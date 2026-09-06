package locallogin

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net"
	"net/netip"
	"time"

	"github.com/hvritual/yunka.io/framework/execution"
)

var ErrLoginThrottled = errors.New("local password attempts are temporarily limited")
var ErrThrottleUnavailable = errors.New("local password attempt protection is unavailable")

// ThrottleError has no account-existence, password or network-address details.
type ThrottleError struct{ RetryAfter time.Duration }

func (e *ThrottleError) Error() string { return ErrLoginThrottled.Error() }
func (e *ThrottleError) Unwrap() error { return ErrLoginThrottled }

type AttemptLimit struct {
	Attempts int
	Window   time.Duration
	Cooldown time.Duration
}

type ThrottlePolicy struct {
	Account AttemptLimit
	Source  AttemptLimit
}

// Budgets include successful attempts. Never clearing reservations on success
// prevents an overlapping success from erasing another request's reservation.
func DefaultThrottlePolicy() ThrottlePolicy {
	return ThrottlePolicy{
		Account: AttemptLimit{Attempts: 10, Window: 5 * time.Minute, Cooldown: 15 * time.Minute},
		Source:  AttemptLimit{Attempts: 120, Window: time.Minute, Cooldown: time.Minute},
	}
}

func (p ThrottlePolicy) validate() error {
	for _, rule := range []AttemptLimit{p.Account, p.Source} {
		if rule.Attempts < 1 || rule.Attempts > 10000 || rule.Window < time.Second || rule.Window > 24*time.Hour || rule.Cooldown < time.Second || rule.Cooldown > 24*time.Hour || rule.Window%time.Second != 0 || rule.Cooldown%time.Second != 0 {
			return ErrThrottleUnavailable
		}
	}
	if p.Account.Attempts > 100 {
		return ErrThrottleUnavailable
	}
	return nil
}

func WithThrottlePolicy(policy ThrottlePolicy) Option {
	return func(manager *Manager) error {
		if err := policy.validate(); err != nil {
			return err
		}
		manager.throttle = policy
		return nil
	}
}

type peerAddressKey struct{}

// WithPeerAddress accepts the actual connection peer, never Forwarded,
// X-Forwarded-For or X-Real-IP. A BFF therefore shares a conservative source
// budget across its clients; it must not invent a browser IP from headers.
func WithPeerAddress(ctx context.Context, remoteAddr string) context.Context {
	source := "unknown-peer"
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		if address, err := netip.ParseAddr(host); err == nil && address.Zone() == "" {
			address = address.Unmap()
			if address.Is6() {
				source = netip.PrefixFrom(address, 64).Masked().String()
			} else {
				source = address.String()
			}
		}
	}
	return context.WithValue(ctx, peerAddressKey{}, source)
}

func throttleKey(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(part)))
		hash.Write(size[:])
		hash.Write([]byte(part))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

const maxThrottleBuckets = 4096

// reservePasswordAttempt commits a security-control reservation BEFORE the
// business root transaction and before Argon2 work. Failed authentication,
// audit rollback, process restart and concurrent Manager instances cannot
// reset that reservation. It never joins a rollback-prone ExecutionScope.
func (manager *Manager) reservePasswordAttempt(ctx context.Context, organizationID, userID string) error {
	if _, active := execution.Current(ctx); active {
		return ErrThrottleUnavailable
	}
	if err := manager.throttle.validate(); err != nil {
		return err
	}
	now := manager.clock().UTC().Unix()
	if now <= 0 {
		return ErrThrottleUnavailable
	}
	source, ok := ctx.Value(peerAddressKey{}).(string)
	if !ok {
		source = "in-process"
	}
	if !canonicalIdentifier(organizationID) || !canonicalIdentifier(userID) {
		organizationID, userID = "invalid", "invalid"
	}
	tx, err := manager.database.BeginTx(ctx, nil)
	if err != nil {
		return ErrThrottleUnavailable
	}
	defer tx.Rollback()
	// The first statement acquires SQLite's writer lock before any read.
	// No auth hash work or business callback runs while this lock is held.
	if _, err := tx.ExecContext(ctx, `DELETE FROM iotd_local_password_attempts WHERE expires_at <= ?`, now); err != nil {
		return ErrThrottleUnavailable
	}
	var denied *ThrottleError
	transition := false
	for _, bucket := range []struct {
		key  string
		rule AttemptLimit
	}{
		{throttleKey("source", source), manager.throttle.Source},
		{throttleKey("account", organizationID, userID), manager.throttle.Account},
	} {
		var changed bool
		denied, changed, err = takePasswordBudget(ctx, tx, bucket.key, bucket.rule, now)
		transition = transition || changed
		if err != nil {
			return ErrThrottleUnavailable
		}
		if denied != nil {
			break
		}
	}
	if err := tx.Commit(); err != nil {
		return ErrThrottleUnavailable
	}
	if denied != nil {
		// Record lock transitions rather than flooding audit storage per denied
		// probe. No supplied identity, source or secret is copied into the audit.
		if transition {
			_ = manager.recordThrottle(ctx)
		}
		return denied
	}
	return nil
}

func takePasswordBudget(ctx context.Context, tx *sql.Tx, key string, rule AttemptLimit, now int64) (*ThrottleError, bool, error) {
	var attempts int
	var resetAt, blockedUntil int64
	err := tx.QueryRowContext(ctx, `SELECT attempts, reset_at, blocked_until FROM iotd_local_password_attempts WHERE bucket = ?`, key).Scan(&attempts, &resetAt, &blockedUntil)
	if errors.Is(err, sql.ErrNoRows) {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM iotd_local_password_attempts`).Scan(&count); err != nil {
			return nil, false, err
		}
		if count >= maxThrottleBuckets {
			return nil, false, ErrThrottleUnavailable
		}
		resetAt = now + int64(rule.Window/time.Second)
		_, err = tx.ExecContext(ctx, `INSERT INTO iotd_local_password_attempts (bucket, attempts, reset_at, blocked_until, expires_at) VALUES (?, 1, ?, 0, ?)`, key, resetAt, resetAt)
		return nil, false, err
	}
	if err != nil {
		return nil, false, err
	}
	if blockedUntil > now {
		// Rejected traffic must not renew or lengthen the existing lock.
		return &ThrottleError{RetryAfter: time.Duration(blockedUntil-now) * time.Second}, false, nil
	}
	if attempts >= rule.Attempts {
		blockedUntil = now + int64(rule.Cooldown/time.Second)
		_, err := tx.ExecContext(ctx, `UPDATE iotd_local_password_attempts SET blocked_until = ?, expires_at = ? WHERE bucket = ?`, blockedUntil, blockedUntil, key)
		return &ThrottleError{RetryAfter: rule.Cooldown}, true, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE iotd_local_password_attempts SET attempts = attempts + 1 WHERE bucket = ?`, key)
	return nil, false, err
}
