// Package identitybinding resolves verified external OIDC identities to stable
// internal users. It owns no token verification, authorization, or transport.
package identitybinding

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitycore"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/oidcverify"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

var (
	ErrInvalidBindingInput                  = errors.New("identity binding input is invalid")
	ErrOrganizationNotFound                 = errors.New("identity binding organization was not found")
	ErrUserNotFound                         = errors.New("identity binding user was not found")
	ErrExternalIdentityNotFound             = errors.New("identity binding external identity was not found")
	ErrExternalIdentityOrganizationMismatch = errors.New("identity binding external identity belongs to another organization")
	ErrDisabled                             = errors.New("identity binding record is disabled")
)

const defaultDisplayName = "Unnamed user"

// Config supplies test seams for identifiers and timestamps. In production
// both defaults are cryptographically random identifiers and the current UTC
// time.
type Config struct {
	NewID func() (string, error)
	Now   func() time.Time
}

// Resolver persists the one-to-one mapping between a verified external key and
// an internal user.
type Resolver struct {
	database *sql.DB
	newID    func() (string, error)
	now      func() time.Time
}

// NewSQLiteResolver constructs a resolver over a database already migrated by
// identitycore.ApplyMigrations.
func NewSQLiteResolver(database *sql.DB, config Config) (*Resolver, error) {
	if database == nil {
		return nil, errors.New("identity binding SQLite database is required")
	}
	if config.NewID == nil {
		config.NewID = randomID
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Resolver{database: database, newID: config.NewID, now: config.Now}, nil
}

// ResolveOrProvision returns the stable user for claims already verified by
// oidcverify. A new user receives no roles or permissions.
func (resolver *Resolver) ResolveOrProvision(ctx context.Context, organizationID string, claims oidcverify.VerifiedClaims) (identitycore.User, error) {
	if err := validateBindingInput(organizationID, claims); err != nil {
		return identitycore.User{}, err
	}
	if resolver == nil || resolver.database == nil || resolver.newID == nil || resolver.now == nil {
		return identitycore.User{}, errors.New("identity binding resolver is not configured")
	}

	user, _, err := resolver.resolveInTransaction(ctx, organizationID, claims, true)
	if err == nil || !isUniqueConstraint(err) {
		return user, err
	}
	return resolver.resolveAfterUniqueConstraint(ctx, organizationID, claims, err)
}

func (resolver *Resolver) resolveAfterUniqueConstraint(ctx context.Context, organizationID string, claims oidcverify.VerifiedClaims, original error) (identitycore.User, error) {
	// A concurrent resolver may have completed the same external-key binding.
	// Re-read it once; do not retry provisioning or create another user.
	user, found, readErr := resolver.resolveInTransaction(ctx, organizationID, claims, false)
	if readErr != nil {
		return identitycore.User{}, readErr
	}
	if found {
		return user, nil
	}
	return identitycore.User{}, original
}

func (resolver *Resolver) resolveInTransaction(ctx context.Context, organizationID string, claims oidcverify.VerifiedClaims, provision bool) (identitycore.User, bool, error) {
	transaction, err := resolver.database.BeginTx(ctx, nil)
	if err != nil {
		return identitycore.User{}, false, fmt.Errorf("begin identity binding transaction: %w", err)
	}
	defer transaction.Rollback()

	if err := requireActiveOrganization(ctx, transaction, organizationID); err != nil {
		return identitycore.User{}, false, err
	}
	external, found, err := findExternalIdentity(ctx, transaction, claims.Issuer, claims.Subject)
	if err != nil {
		return identitycore.User{}, false, err
	}
	if found {
		if external.OrganizationID != organizationID {
			return identitycore.User{}, true, ErrExternalIdentityOrganizationMismatch
		}
		if external.Status != identitycore.StatusActive {
			return identitycore.User{}, true, ErrDisabled
		}
		user, err := findUser(ctx, transaction, external.UserID)
		if err != nil {
			return identitycore.User{}, true, err
		}
		if user.OrganizationID != organizationID {
			return identitycore.User{}, true, ErrExternalIdentityOrganizationMismatch
		}
		if user.Status != identitycore.StatusActive {
			return identitycore.User{}, true, ErrDisabled
		}
		updated, err := resolver.updateExisting(ctx, transaction, user, external, claims)
		if err != nil {
			return identitycore.User{}, true, err
		}
		if err := transaction.Commit(); err != nil {
			return identitycore.User{}, true, fmt.Errorf("commit existing identity binding: %w", err)
		}
		return updated, true, nil
	}
	if !provision {
		if err := transaction.Commit(); err != nil {
			return identitycore.User{}, false, fmt.Errorf("commit absent identity binding read: %w", err)
		}
		return identitycore.User{}, false, nil
	}

	userID, err := resolver.nextID()
	if err != nil {
		return identitycore.User{}, false, err
	}
	externalID, err := resolver.nextID()
	if err != nil {
		return identitycore.User{}, false, err
	}
	now := resolver.now().UTC()
	user := identitycore.User{
		ID:             userID,
		OrganizationID: organizationID,
		DisplayName:    profileValue(claims.DisplayName, defaultDisplayName),
		Email:          profileValue(claims.Email, ""),
		Status:         identitycore.StatusActive,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO users (id, organization_id, display_name, email, status, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`, user.ID, user.OrganizationID, user.DisplayName, user.Email, user.Status, formatTime(now), formatTime(now)); err != nil {
		return identitycore.User{}, false, fmt.Errorf("insert identity binding user: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO external_identities (
    id, organization_id, user_id, issuer, subject, email_snapshot,
    display_name_snapshot, last_seen_at, status, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		externalID, organizationID, userID, claims.Issuer, claims.Subject,
		profileValue(claims.Email, ""), profileValue(claims.DisplayName, ""), formatTime(now), identitycore.StatusActive, formatTime(now), formatTime(now)); err != nil {
		return identitycore.User{}, false, fmt.Errorf("insert identity binding external identity: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return identitycore.User{}, false, fmt.Errorf("commit new identity binding: %w", err)
	}
	return user, true, nil
}

func (resolver *Resolver) updateExisting(ctx context.Context, transaction *sql.Tx, user identitycore.User, external identitycore.ExternalIdentity, claims oidcverify.VerifiedClaims) (identitycore.User, error) {
	now := resolver.now().UTC()
	nextEmail := profileValue(claims.Email, user.Email)
	nextDisplayName := profileValue(claims.DisplayName, user.DisplayName)
	userProfileChanged := nextEmail != user.Email || nextDisplayName != user.DisplayName
	user.Email = nextEmail
	user.DisplayName = nextDisplayName
	if userProfileChanged {
		user.UpdatedAt = now
	}
	external.EmailSnapshot = profileValue(claims.Email, external.EmailSnapshot)
	external.DisplayNameSnapshot = profileValue(claims.DisplayName, external.DisplayNameSnapshot)
	external.LastSeenAt = &now
	external.UpdatedAt = now
	if userProfileChanged {
		if _, err := transaction.ExecContext(ctx, `UPDATE users SET display_name = ?, email = ?, updated_at = ? WHERE id = ?`, user.DisplayName, user.Email, formatTime(now), user.ID); err != nil {
			return identitycore.User{}, fmt.Errorf("update identity binding user profile: %w", err)
		}
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE external_identities
SET email_snapshot = ?, display_name_snapshot = ?, last_seen_at = ?, updated_at = ?
WHERE id = ?`, external.EmailSnapshot, external.DisplayNameSnapshot, formatTime(now), formatTime(now), external.ID); err != nil {
		return identitycore.User{}, fmt.Errorf("update identity binding external profile: %w", err)
	}
	return user, nil
}

// DisableOrganization preserves the organization while denying all bindings in
// it. The update is idempotent.
func (resolver *Resolver) DisableOrganization(ctx context.Context, organizationID string) error {
	if strings.TrimSpace(organizationID) == "" {
		return ErrInvalidBindingInput
	}
	return resolver.disable(ctx,
		`UPDATE organizations SET status = ?, updated_at = ? WHERE id = ? AND status = ?`,
		`SELECT 1 FROM organizations WHERE id = ?`,
		organizationID,
		ErrOrganizationNotFound)
}

// DisableUser preserves the user while denying every binding to it. The update
// is idempotent.
func (resolver *Resolver) DisableUser(ctx context.Context, userID string) error {
	if strings.TrimSpace(userID) == "" {
		return ErrInvalidBindingInput
	}
	return resolver.disable(ctx,
		`UPDATE users SET status = ?, updated_at = ? WHERE id = ? AND status = ?`,
		`SELECT 1 FROM users WHERE id = ?`,
		userID,
		ErrUserNotFound)
}

// DisableExternalIdentity preserves the external binding and denies resolution
// through that exact issuer and subject. The update is idempotent.
func (resolver *Resolver) DisableExternalIdentity(ctx context.Context, issuer, subject string) error {
	if strings.TrimSpace(issuer) == "" || strings.TrimSpace(subject) == "" {
		return ErrInvalidBindingInput
	}
	if resolver == nil || resolver.database == nil || resolver.now == nil {
		return errors.New("identity binding resolver is not configured")
	}
	result, err := resolver.database.ExecContext(ctx, `UPDATE external_identities SET status = ?, updated_at = ? WHERE issuer = ? AND subject = ? AND status = ?`, identitycore.StatusDisabled, formatTime(resolver.now().UTC()), issuer, subject, identitycore.StatusActive)
	if err != nil {
		return fmt.Errorf("disable identity binding external identity: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check disabled identity binding external identity: %w", err)
	}
	if changed == 0 {
		var exists int
		err := resolver.database.QueryRowContext(ctx, `SELECT 1 FROM external_identities WHERE issuer = ? AND subject = ?`, issuer, subject).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrExternalIdentityNotFound
		}
		if err != nil {
			return fmt.Errorf("read disabled identity binding external identity: %w", err)
		}
	}
	return nil
}

func (resolver *Resolver) disable(ctx context.Context, statement, existsStatement, id string, notFound error) error {
	if resolver == nil || resolver.database == nil || resolver.now == nil {
		return errors.New("identity binding resolver is not configured")
	}
	result, err := resolver.database.ExecContext(ctx, statement, identitycore.StatusDisabled, formatTime(resolver.now().UTC()), id, identitycore.StatusActive)
	if err != nil {
		return fmt.Errorf("disable identity binding record: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check disabled identity binding record: %w", err)
	}
	if changed == 0 {
		var exists int
		err := resolver.database.QueryRowContext(ctx, existsStatement, id).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return notFound
		}
		if err != nil {
			return fmt.Errorf("read disabled identity binding record: %w", err)
		}
	}
	return nil
}

func requireActiveOrganization(ctx context.Context, transaction *sql.Tx, organizationID string) error {
	var status string
	err := transaction.QueryRowContext(ctx, `SELECT status FROM organizations WHERE id = ?`, organizationID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrOrganizationNotFound
	}
	if err != nil {
		return fmt.Errorf("read identity binding organization: %w", err)
	}
	if identitycore.Status(status) != identitycore.StatusActive {
		return ErrDisabled
	}
	return nil
}

func findExternalIdentity(ctx context.Context, transaction *sql.Tx, issuer, subject string) (identitycore.ExternalIdentity, bool, error) {
	var identity identitycore.ExternalIdentity
	var status string
	var lastSeen sql.NullString
	var createdAt, updatedAt string
	err := transaction.QueryRowContext(ctx, `
SELECT id, organization_id, user_id, issuer, subject, COALESCE(email_snapshot, ''),
       COALESCE(display_name_snapshot, ''), last_seen_at, status, created_at, updated_at
FROM external_identities WHERE issuer = ? AND subject = ?`, issuer, subject).Scan(
		&identity.ID, &identity.OrganizationID, &identity.UserID, &identity.Issuer, &identity.Subject,
		&identity.EmailSnapshot, &identity.DisplayNameSnapshot, &lastSeen, &status, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return identitycore.ExternalIdentity{}, false, nil
	}
	if err != nil {
		return identitycore.ExternalIdentity{}, false, fmt.Errorf("read identity binding external identity: %w", err)
	}
	identity.Status = identitycore.Status(status)
	identity.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return identitycore.ExternalIdentity{}, false, err
	}
	identity.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return identitycore.ExternalIdentity{}, false, err
	}
	if lastSeen.Valid {
		value, parseErr := parseTime(lastSeen.String)
		if parseErr != nil {
			return identitycore.ExternalIdentity{}, false, parseErr
		}
		identity.LastSeenAt = &value
	}
	return identity, true, nil
}

func findUser(ctx context.Context, transaction *sql.Tx, userID string) (identitycore.User, error) {
	var user identitycore.User
	var status string
	var createdAt, updatedAt string
	err := transaction.QueryRowContext(ctx, `
SELECT id, organization_id, display_name, COALESCE(email, ''), status, created_at, updated_at
FROM users WHERE id = ?`, userID).Scan(&user.ID, &user.OrganizationID, &user.DisplayName, &user.Email, &status, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return identitycore.User{}, ErrUserNotFound
	}
	if err != nil {
		return identitycore.User{}, fmt.Errorf("read identity binding user: %w", err)
	}
	user.Status = identitycore.Status(status)
	user.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return identitycore.User{}, err
	}
	user.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return identitycore.User{}, err
	}
	return user, nil
}

func validateBindingInput(organizationID string, claims oidcverify.VerifiedClaims) error {
	if strings.TrimSpace(organizationID) == "" || strings.TrimSpace(claims.Issuer) == "" || strings.TrimSpace(claims.Subject) == "" {
		return ErrInvalidBindingInput
	}
	return nil
}

func profileValue(candidate, current string) string {
	if strings.TrimSpace(candidate) == "" {
		return current
	}
	return candidate
}

func (resolver *Resolver) nextID() (string, error) {
	id, err := resolver.newID()
	if err != nil {
		return "", fmt.Errorf("generate identity binding identifier: %w", err)
	}
	if strings.TrimSpace(id) == "" {
		return "", errors.New("generate identity binding identifier: empty identifier")
	}
	return id, nil
}

func randomID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("read random identity binding identifier: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

func formatTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("decode identity binding timestamp: %w", err)
	}
	return parsed, nil
}

func isUniqueConstraint(err error) bool {
	var sqliteError *sqlite.Error
	return errors.As(err, &sqliteError) && sqliteError.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE
}
