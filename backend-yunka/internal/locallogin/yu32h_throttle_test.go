package locallogin

import (
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/audit"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localcredential"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localtx"
	"github.com/hvritual/yunka.io/framework/operation"
)

func TestYU32HCooldownPersistsDoesNotExtendAndDoesNotInvalidateSessions(t *testing.T) {
	f := newLoginFixture(t, false)
	input := LoginInput{OrganizationID: "org-a", UserID: "user-a", Password: []byte("YU21-password-secret")}
	login, err := f.manager.Login(f.context(t), input)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < 10; i++ {
		if err := f.manager.reservePasswordAttempt(f.context(t), "org-a", "user-a"); err != nil {
			t.Fatal(err)
		}
	}
	_, err = f.manager.Login(f.context(t), input)
	var limited *ThrottleError
	if !errors.As(err, &limited) || limited.RetryAfter != 15*time.Minute {
		t.Fatalf("initial cooldown: %v", err)
	}
	// A second DB handle and a reconstructed Manager use the same durable budget.
	reopened := newYU32HManagerOnSecondConnection(t, f)
	f.now = f.now.Add(time.Minute)
	_, err = reopened.Login(f.context(t), input)
	if !errors.As(err, &limited) || limited.RetryAfter != 14*time.Minute {
		t.Fatalf("cooldown was reset/extended: %v", err)
	}
	if _, err := reopened.VerifySessionToken(t.Context(), login.SessionToken); err != nil {
		t.Fatal("throttle invalidated an established session", err)
	}
	f.now = f.now.Add(14 * time.Minute)
	if _, err := reopened.Login(f.context(t), input); err != nil {
		t.Fatalf("automatic cooldown recovery: %v", err)
	}
}

func newYU32HManagerOnSecondConnection(t *testing.T, f *loginFixture) *Manager {
	t.Helper()
	var seq int
	var name, file string
	if err := f.database.QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &file); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", file+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	credentials, err := localcredential.NewSQLiteRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	store, err := audit.NewSQLiteStore(db)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(db, credentials, store, operation.NewExecutorWithOptions(nil, operation.ExecutorOptions{Transactions: localtx.NewSQLiteFactory(db)}), f.config, WithClock(func() time.Time { return f.now }))
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func TestYU32HConcurrentIndependentConnectionsCannotOversubscribe(t *testing.T) {
	f := newLoginFixture(t, false)
	if _, err := f.database.Exec(`PRAGMA busy_timeout=5000`); err != nil {
		t.Fatal(err)
	}
	other := newYU32HManagerOnSecondConnection(t, f)
	managers := []*Manager{f.manager, other}
	var allowed, denied atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := managers[i%2].reservePasswordAttempt(t.Context(), "org-a", "user-a")
			switch {
			case err == nil:
				allowed.Add(1)
			case errors.Is(err, ErrLoginThrottled):
				denied.Add(1)
			default:
				t.Errorf("reservation failed: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if allowed.Load() != 10 || denied.Load() != 22 {
		t.Fatalf("oversubscription: allowed=%d denied=%d", allowed.Load(), denied.Load())
	}
}

func TestYU32HUnknownAccountsAndKnownAccountsShareLimits(t *testing.T) {
	for _, user := range []string{"user-a", "absent-user"} {
		t.Run(user, func(t *testing.T) {
			f := newLoginFixture(t, false)
			for i := 0; i < 10; i++ {
				if _, err := f.manager.Login(f.context(t), LoginInput{OrganizationID: "org-a", UserID: user, Password: []byte("wrong-password")}); !errors.Is(err, ErrAuthenticationFailed) {
					t.Fatal(err)
				}
			}
			if _, err := f.manager.Login(f.context(t), LoginInput{OrganizationID: "org-a", UserID: user, Password: []byte("wrong-password")}); !errors.Is(err, ErrLoginThrottled) {
				t.Fatal("account existence changed throttle response", err)
			}
			if f.sessionCount(t) != 0 {
				t.Fatal("failed attempt created session")
			}
		})
	}
}

func TestYU32HSourceBudgetAndCanonicalPeerIdentity(t *testing.T) {
	f := newLoginFixture(t, false)
	f.manager.throttle.Source.Attempts = 2
	for i, peer := range []string{"192.0.2.8:1234", "[::ffff:192.0.2.8]:2345"} {
		if err := f.manager.reservePasswordAttempt(WithPeerAddress(t.Context(), peer), "org-a", fmt.Sprint("user-", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.manager.reservePasswordAttempt(WithPeerAddress(t.Context(), "192.0.2.8:9999"), "org-a", "third"); !errors.Is(err, ErrLoginThrottled) {
		t.Fatal("source limit bypass", err)
	}
	if err := f.manager.reservePasswordAttempt(WithPeerAddress(t.Context(), "192.0.2.9:9999"), "org-a", "third"); err != nil {
		t.Fatal("unrelated source blocked", err)
	}
	if throttleKey("a\x00b", "c") == throttleKey("a", "b\x00c") {
		t.Fatal("key tuple collision")
	}
}

func TestYU32HStorageFailureAndCorruptMigrationFailClosed(t *testing.T) {
	f := newLoginFixture(t, false)
	if err := ApplyMigrations(t.Context(), f.database); err != nil {
		t.Fatal("migration not repeatable", err)
	}
	if _, err := f.database.Exec(`DROP TABLE iotd_local_password_attempts`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.manager.Login(f.context(t), LoginInput{OrganizationID: "org-a", UserID: "user-a", Password: []byte("YU21-password-secret")}); !errors.Is(err, ErrThrottleUnavailable) {
		t.Fatal("limiter outage failed open", err)
	}
	if f.sessionCount(t) != 0 {
		t.Fatal("limiter outage created session")
	}
	if _, err := f.database.Exec(`CREATE TABLE iotd_local_password_attempts (bucket TEXT PRIMARY KEY, attempts INTEGER, reset_at INTEGER, blocked_until INTEGER, expires_at INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(t.Context(), f.database); err == nil {
		t.Fatal("forged ledger/corrupt schema accepted")
	}
}

func TestYU32HBoundedRetentionFailsClosedAtCapacityAndRecovers(t *testing.T) {
	f := newLoginFixture(t, false)
	_, err := f.database.Exec(`WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x < ?) INSERT INTO iotd_local_password_attempts SELECT printf('%064x',x),1,?,0,? FROM n`, maxThrottleBuckets, f.now.Unix()+1, f.now.Unix()+1)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.manager.reservePasswordAttempt(t.Context(), "org-a", "user-a"); !errors.Is(err, ErrThrottleUnavailable) {
		t.Fatal("capacity failed open", err)
	}
	f.now = f.now.Add(time.Second)
	if err := f.manager.reservePasswordAttempt(t.Context(), "org-a", "user-a"); err != nil {
		t.Fatal("expired records not reclaimed", err)
	}
	var count int
	if err := f.database.QueryRow(`SELECT count(*) FROM iotd_local_password_attempts`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("retention count=%d err=%v", count, err)
	}
}

func TestYU32HPasswordChangeCannotBypassLoginBudget(t *testing.T) {
	f := newLoginFixture(t, false)
	login, err := f.manager.Login(f.context(t), LoginInput{OrganizationID: "org-a", UserID: "user-a", Password: []byte("YU21-password-secret")})
	if err != nil {
		t.Fatal(err)
	}
	input := ChangePasswordInput{SessionToken: login.SessionToken, ExpectedSessionRevision: 1, ExpectedUserRevision: 1, ExpectedCredentialRevision: 1, CurrentPassword: []byte("wrong-current-password"), NewPassword: []byte("new-strong-password")}
	for i := 1; i < 10; i++ {
		if _, err := f.manager.ChangePassword(f.context(t), input); !errors.Is(err, ErrCurrentPasswordInvalid) {
			t.Fatal(err)
		}
	}
	if _, err := f.manager.ChangePassword(f.context(t), input); !errors.Is(err, ErrLoginThrottled) {
		t.Fatal("change-password bypassed login budget", err)
	}
	m, err := f.credentials.Metadata(t.Context(), "org-a", "user-a")
	if err != nil || m.Revision != 1 {
		t.Fatal("failed current password changed credential")
	}
}
