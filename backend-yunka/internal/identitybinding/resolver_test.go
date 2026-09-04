package identitybinding

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitycore"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/oidcverify"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

func TestResolveOrProvisionCreatesExactlyOneUserAndExternalIdentity(t *testing.T) {
	database, resolver := newResolver(t, "user-1", "external-1")
	createOrganization(t, database, "org-1")

	resolved, err := resolver.ResolveOrProvision(t.Context(), "org-1", claims("issuer-a", "subject-a", "first@example.test", "First Name"))
	if err != nil {
		t.Fatalf("resolve or provision: %v", err)
	}
	if resolved.ID != "user-1" || resolved.OrganizationID != "org-1" || resolved.Email != "first@example.test" || resolved.DisplayName != "First Name" || resolved.Status != identitycore.StatusActive {
		t.Fatalf("resolved user = %#v", resolved)
	}
	if got := count(t, database, "users"); got != 1 {
		t.Fatalf("user count = %d, want 1", got)
	}
	if got := count(t, database, "external_identities"); got != 1 {
		t.Fatalf("external identity count = %d, want 1", got)
	}
	identity := readExternalIdentity(t, database, "issuer-a", "subject-a")
	if identity.ID != "external-1" || identity.UserID != "user-1" || identity.OrganizationID != "org-1" || identity.EmailSnapshot != "first@example.test" || identity.DisplayNameSnapshot != "First Name" || identity.LastSeenAt == nil || identity.Status != identitycore.StatusActive {
		t.Fatalf("external identity = %#v", identity)
	}
	t.Logf("SQLite readback: users=%d external_identities=%d user_id=%s external_identity_id=%s status=%s", count(t, database, "users"), count(t, database, "external_identities"), resolved.ID, identity.ID, identity.Status)
}

func TestResolveOrProvisionIsIdempotentForAnExternalKey(t *testing.T) {
	database, resolver := newResolver(t, "user-1", "external-1", "unexpected-user", "unexpected-external")
	createOrganization(t, database, "org-1")
	first, err := resolver.ResolveOrProvision(t.Context(), "org-1", claims("issuer-a", "subject-a", "first@example.test", "First Name"))
	if err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	second, err := resolver.ResolveOrProvision(t.Context(), "org-1", claims("issuer-a", "subject-a", "first@example.test", "First Name"))
	if err != nil {
		t.Fatalf("second resolve: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("second user ID = %q, want %q", second.ID, first.ID)
	}
	if got := count(t, database, "users"); got != 1 {
		t.Fatalf("user count = %d, want 1", got)
	}
	if got := count(t, database, "external_identities"); got != 1 {
		t.Fatalf("external identity count = %d, want 1", got)
	}
}

func TestResolveOrProvisionDoesNotMergeMatchingEmailAcrossExternalKeys(t *testing.T) {
	database, resolver := newResolver(t, "user-1", "external-1", "user-2", "external-2", "user-3", "external-3")
	createOrganization(t, database, "org-1")
	first, err := resolver.ResolveOrProvision(t.Context(), "org-1", claims("issuer-a", "subject-a", "shared@example.test", "First"))
	if err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	second, err := resolver.ResolveOrProvision(t.Context(), "org-1", claims("issuer-b", "subject-a", "shared@example.test", "Second"))
	if err != nil {
		t.Fatalf("second resolve: %v", err)
	}
	third, err := resolver.ResolveOrProvision(t.Context(), "org-1", claims("issuer-a", "subject-b", "shared@example.test", "Third"))
	if err != nil {
		t.Fatalf("third resolve: %v", err)
	}
	if first.ID == second.ID || first.ID == third.ID || second.ID == third.ID {
		t.Fatalf("distinct external keys merged: first=%q second=%q third=%q", first.ID, second.ID, third.ID)
	}
	if got := count(t, database, "users"); got != 3 {
		t.Fatalf("user count = %d, want 3", got)
	}
}

func TestResolveOrProvisionUpdatesNonEmptyProfileWithoutRebinding(t *testing.T) {
	clock := newTestClock(time.Date(2026, time.September, 3, 12, 0, 0, 0, time.UTC))
	database, resolver := newResolverWithClock(t, clock, "user-1", "external-1")
	createOrganization(t, database, "org-1")
	first, err := resolver.ResolveOrProvision(t.Context(), "org-1", claims("issuer-a", "subject-a", "first@example.test", "First Name"))
	if err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	firstIdentity := readExternalIdentity(t, database, "issuer-a", "subject-a")
	assertTime(t, first.UpdatedAt, clock.Now())
	assertTime(t, *firstIdentity.LastSeenAt, clock.Now())
	assertTime(t, firstIdentity.UpdatedAt, clock.Now())

	clock.Advance(time.Minute)
	same, err := resolver.ResolveOrProvision(t.Context(), "org-1", claims("issuer-a", "subject-a", "first@example.test", "First Name"))
	if err != nil {
		t.Fatalf("same-profile resolve: %v", err)
	}
	sameIdentity := readExternalIdentity(t, database, "issuer-a", "subject-a")
	assertTime(t, same.UpdatedAt, first.UpdatedAt)
	assertTime(t, *sameIdentity.LastSeenAt, clock.Now())
	assertTime(t, sameIdentity.UpdatedAt, clock.Now())

	clock.Advance(time.Minute)
	second, err := resolver.ResolveOrProvision(t.Context(), "org-1", claims("issuer-a", "subject-a", "changed@example.test", "Changed Name"))
	if err != nil {
		t.Fatalf("second resolve: %v", err)
	}
	if second.ID != first.ID || second.OrganizationID != first.OrganizationID || second.Email != "changed@example.test" || second.DisplayName != "Changed Name" {
		t.Fatalf("updated user = %#v", second)
	}
	identity := readExternalIdentity(t, database, "issuer-a", "subject-a")
	if identity.UserID != first.ID || identity.EmailSnapshot != "changed@example.test" || identity.DisplayNameSnapshot != "Changed Name" || identity.LastSeenAt == nil {
		t.Fatalf("updated external identity = %#v", identity)
	}
	assertTime(t, second.UpdatedAt, clock.Now())
	assertTime(t, *identity.LastSeenAt, clock.Now())
	assertTime(t, identity.UpdatedAt, clock.Now())

	clock.Advance(time.Minute)
	_, err = resolver.ResolveOrProvision(t.Context(), "org-1", claims("issuer-a", "subject-a", "", ""))
	if err != nil {
		t.Fatalf("resolve without optional profile: %v", err)
	}
	unchanged := readUser(t, database, first.ID)
	unchangedIdentity := readExternalIdentity(t, database, "issuer-a", "subject-a")
	if unchanged.Email != "changed@example.test" || unchanged.DisplayName != "Changed Name" || unchangedIdentity.EmailSnapshot != "changed@example.test" || unchangedIdentity.DisplayNameSnapshot != "Changed Name" {
		t.Fatalf("missing optional profile erased user data: %#v", unchanged)
	}
	assertTime(t, unchanged.UpdatedAt, second.UpdatedAt)
	assertTime(t, *unchangedIdentity.LastSeenAt, clock.Now())
	assertTime(t, unchangedIdentity.UpdatedAt, clock.Now())
}

func TestResolveOrProvisionRejectsDisabledIdentityRecordsWithoutSideEffects(t *testing.T) {
	for name, disable := range map[string]func(*testing.T, *Resolver, *sql.DB){
		"organization": func(t *testing.T, resolver *Resolver, _ *sql.DB) {
			t.Helper()
			if err := resolver.DisableOrganization(t.Context(), "org-1"); err != nil {
				t.Fatalf("disable organization: %v", err)
			}
		},
		"user": func(t *testing.T, resolver *Resolver, _ *sql.DB) {
			t.Helper()
			if err := resolver.DisableUser(t.Context(), "user-1"); err != nil {
				t.Fatalf("disable user: %v", err)
			}
		},
		"external identity": func(t *testing.T, resolver *Resolver, _ *sql.DB) {
			t.Helper()
			if err := resolver.DisableExternalIdentity(t.Context(), "issuer-a", "subject-a"); err != nil {
				t.Fatalf("disable external identity: %v", err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			clock := newTestClock(time.Date(2026, time.September, 3, 12, 0, 0, 0, time.UTC))
			database, resolver := newResolverWithClock(t, clock, "user-1", "external-1")
			createOrganization(t, database, "org-1")
			if _, err := resolver.ResolveOrProvision(t.Context(), "org-1", claims("issuer-a", "subject-a", "before@example.test", "Before")); err != nil {
				t.Fatalf("initial resolve: %v", err)
			}
			clock.Advance(time.Minute)
			disable(t, resolver, database)
			beforeUser := readUser(t, database, "user-1")
			beforeIdentity := readExternalIdentity(t, database, "issuer-a", "subject-a")
			clock.Advance(time.Minute)

			_, err := resolver.ResolveOrProvision(t.Context(), "org-1", claims("issuer-a", "subject-a", "after@example.test", "After"))
			if !errors.Is(err, ErrDisabled) {
				t.Fatalf("resolve error = %v, want ErrDisabled", err)
			}
			afterUser := readUser(t, database, "user-1")
			afterIdentity := readExternalIdentity(t, database, "issuer-a", "subject-a")
			if afterUser.Email != beforeUser.Email || afterUser.DisplayName != beforeUser.DisplayName || !afterUser.UpdatedAt.Equal(beforeUser.UpdatedAt) || !sameTime(afterIdentity.LastSeenAt, beforeIdentity.LastSeenAt) || !afterIdentity.UpdatedAt.Equal(beforeIdentity.UpdatedAt) {
				t.Fatalf("disabled resolve changed records: user=%#v identity=%#v", afterUser, afterIdentity)
			}
		})
	}
}

func TestDisableIdentityRecordsIsIdempotentWithoutRefreshingTimestamp(t *testing.T) {
	for name, disable := range map[string]struct {
		read    func(*testing.T, *sql.DB) (identitycore.Status, time.Time)
		disable func(*testing.T, *Resolver)
	}{
		"organization": {
			read: func(t *testing.T, database *sql.DB) (identitycore.Status, time.Time) {
				organization := readOrganization(t, database, "org-1")
				return organization.Status, organization.UpdatedAt
			},
			disable: func(t *testing.T, resolver *Resolver) {
				t.Helper()
				if err := resolver.DisableOrganization(t.Context(), "org-1"); err != nil {
					t.Fatalf("disable organization: %v", err)
				}
			},
		},
		"user": {
			read: func(t *testing.T, database *sql.DB) (identitycore.Status, time.Time) {
				user := readUser(t, database, "user-1")
				return user.Status, user.UpdatedAt
			},
			disable: func(t *testing.T, resolver *Resolver) {
				t.Helper()
				if err := resolver.DisableUser(t.Context(), "user-1"); err != nil {
					t.Fatalf("disable user: %v", err)
				}
			},
		},
		"external identity": {
			read: func(t *testing.T, database *sql.DB) (identitycore.Status, time.Time) {
				identity := readExternalIdentity(t, database, "issuer-a", "subject-a")
				return identity.Status, identity.UpdatedAt
			},
			disable: func(t *testing.T, resolver *Resolver) {
				t.Helper()
				if err := resolver.DisableExternalIdentity(t.Context(), "issuer-a", "subject-a"); err != nil {
					t.Fatalf("disable external identity: %v", err)
				}
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			clock := newTestClock(time.Date(2026, time.September, 3, 12, 0, 0, 0, time.UTC))
			database, resolver := newResolverWithClock(t, clock, "user-1", "external-1")
			createOrganization(t, database, "org-1")
			if _, err := resolver.ResolveOrProvision(t.Context(), "org-1", claims("issuer-a", "subject-a", "user@example.test", "User")); err != nil {
				t.Fatalf("initial resolve: %v", err)
			}
			clock.Advance(time.Minute)
			disable.disable(t, resolver)
			status, firstDisabledAt := disable.read(t, database)
			if status != identitycore.StatusDisabled {
				t.Fatalf("status = %q, want %q", status, identitycore.StatusDisabled)
			}
			assertTime(t, firstDisabledAt, clock.Now())
			clock.Advance(time.Minute)
			disable.disable(t, resolver)
			status, secondDisabledAt := disable.read(t, database)
			if status != identitycore.StatusDisabled {
				t.Fatalf("status after repeated disable = %q, want %q", status, identitycore.StatusDisabled)
			}
			assertTime(t, secondDisabledAt, firstDisabledAt)
		})
	}
}

func TestSQLiteConstraintClassificationOnlyAcceptsExternalIdentityUniqueCode(t *testing.T) {
	database, _ := newResolver(t, "unused-user", "unused-external")
	if _, err := database.Exec(`CREATE TABLE constraint_probe (id TEXT PRIMARY KEY, value TEXT UNIQUE)`); err != nil {
		t.Fatalf("create probe table: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO constraint_probe (id, value) VALUES ('one', 'same')`); err != nil {
		t.Fatalf("insert probe row: %v", err)
	}
	_, uniqueErr := database.Exec(`INSERT INTO constraint_probe (id, value) VALUES ('two', 'same')`)
	assertSQLiteConstraintCode(t, uniqueErr, sqlite3.SQLITE_CONSTRAINT_UNIQUE)
	if !isUniqueConstraint(uniqueErr) {
		t.Fatal("UNIQUE constraint was not classified for convergence")
	}
	_, primaryKeyErr := database.Exec(`INSERT INTO constraint_probe (id, value) VALUES ('one', 'different')`)
	assertSQLiteConstraintCode(t, primaryKeyErr, sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY)
	if isUniqueConstraint(primaryKeyErr) {
		t.Fatal("PRIMARY KEY constraint must not be classified for convergence")
	}
}

func TestResolveAfterUniqueConstraintReturnsLatestBusinessError(t *testing.T) {
	for name, testcase := range map[string]struct {
		organizationID string
		prepare        func(*testing.T, *Resolver, *sql.DB)
		want           error
	}{
		"disabled external identity": {
			organizationID: "org-1",
			prepare: func(t *testing.T, resolver *Resolver, _ *sql.DB) {
				t.Helper()
				if err := resolver.DisableExternalIdentity(t.Context(), "issuer-a", "subject-a"); err != nil {
					t.Fatalf("disable external identity: %v", err)
				}
			},
			want: ErrDisabled,
		},
		"another organization": {
			organizationID: "org-2",
			prepare: func(t *testing.T, _ *Resolver, database *sql.DB) {
				t.Helper()
				createOrganization(t, database, "org-2")
			},
			want: ErrExternalIdentityOrganizationMismatch,
		},
	} {
		t.Run(name, func(t *testing.T) {
			database, resolver := newResolver(t, "user-1", "external-1")
			createOrganization(t, database, "org-1")
			if _, err := resolver.ResolveOrProvision(t.Context(), "org-1", claims("issuer-a", "subject-a", "user@example.test", "User")); err != nil {
				t.Fatalf("initial resolve: %v", err)
			}
			testcase.prepare(t, resolver, database)
			_, err := resolver.resolveAfterUniqueConstraint(t.Context(), testcase.organizationID, claims("issuer-a", "subject-a", "user@example.test", "User"), errors.New("original unique error"))
			if !errors.Is(err, testcase.want) {
				t.Fatalf("convergence error = %v, want %v", err, testcase.want)
			}
		})
	}
}

func TestResolveOrProvisionRejectsAnExternalKeyBoundToAnotherOrganization(t *testing.T) {
	database, resolver := newResolver(t, "user-1", "external-1")
	createOrganization(t, database, "org-1")
	createOrganization(t, database, "org-2")
	if _, err := resolver.ResolveOrProvision(t.Context(), "org-1", claims("issuer-a", "subject-a", "user@example.test", "User")); err != nil {
		t.Fatalf("initial resolve: %v", err)
	}
	_, err := resolver.ResolveOrProvision(t.Context(), "org-2", claims("issuer-a", "subject-a", "changed@example.test", "Changed"))
	if !errors.Is(err, ErrExternalIdentityOrganizationMismatch) {
		t.Fatalf("cross-organization resolve error = %v, want ErrExternalIdentityOrganizationMismatch", err)
	}
	if got := count(t, database, "users"); got != 1 {
		t.Fatalf("user count = %d, want 1", got)
	}
}

func TestResolveOrProvisionRollsBackUserWhenExternalIdentityInsertFails(t *testing.T) {
	database, resolver := newResolver(t, "new-user", "external-duplicate")
	createOrganization(t, database, "org-1")
	insertUser(t, database, "existing-user", "org-1", "Existing", "existing@example.test")
	insertExternalIdentity(t, database, "external-duplicate", "org-1", "existing-user", "issuer-old", "subject-old")

	_, err := resolver.ResolveOrProvision(t.Context(), "org-1", claims("issuer-new", "subject-new", "new@example.test", "New"))
	if err == nil {
		t.Fatal("resolve unexpectedly succeeded")
	}
	if got := count(t, database, "users"); got != 1 {
		t.Fatalf("user count after failed transaction = %d, want 1", got)
	}
	if got := count(t, database, "external_identities"); got != 1 {
		t.Fatalf("external identity count after failed transaction = %d, want 1", got)
	}
}

func TestResolveOrProvisionRejectsBlankBindingInputs(t *testing.T) {
	database, resolver := newResolver(t, "user-1", "external-1")
	createOrganization(t, database, "org-1")
	for name, testcase := range map[string]struct {
		organizationID string
		verified       oidcverify.VerifiedClaims
	}{
		"organization": {organizationID: " \t", verified: claims("issuer-a", "subject-a", "", "")},
		"issuer":       {organizationID: "org-1", verified: claims(" \n", "subject-a", "", "")},
		"subject":      {organizationID: "org-1", verified: claims("issuer-a", " \r", "", "")},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := resolver.ResolveOrProvision(t.Context(), testcase.organizationID, testcase.verified)
			if !errors.Is(err, ErrInvalidBindingInput) {
				t.Fatalf("resolve error = %v, want ErrInvalidBindingInput", err)
			}
		})
	}
	if got := count(t, database, "users"); got != 0 {
		t.Fatalf("user count = %d, want 0", got)
	}
}

func newResolver(t *testing.T, ids ...string) (*sql.DB, *Resolver) {
	t.Helper()
	database, resolver := newResolverWithClock(t, newTestClock(time.Date(2026, time.September, 3, 12, 0, 0, 0, time.UTC)), ids...)
	return database, resolver
}

func newResolverWithClock(t *testing.T, clock *testClock, ids ...string) (*sql.DB, *Resolver) {
	t.Helper()
	database, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_")))
	if err != nil {
		t.Fatalf("open temporary SQLite database: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	if err := identitycore.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatalf("apply identity migrations: %v", err)
	}
	next := 0
	resolver, err := NewSQLiteResolver(database, Config{
		NewID: func() (string, error) {
			if next >= len(ids) {
				return "", errors.New("test ID sequence exhausted")
			}
			id := ids[next]
			next++
			return id, nil
		},
		Now: clock.Now,
	})
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}
	return database, resolver
}

type testClock struct{ now time.Time }

func newTestClock(now time.Time) *testClock { return &testClock{now: now.UTC()} }

func (clock *testClock) Now() time.Time { return clock.now }

func (clock *testClock) Advance(duration time.Duration) { clock.now = clock.now.Add(duration) }

func claims(issuer, subject, email, displayName string) oidcverify.VerifiedClaims {
	return oidcverify.VerifiedClaims{Issuer: issuer, Subject: subject, Email: email, DisplayName: displayName}
}

func createOrganization(t *testing.T, database *sql.DB, id string) {
	t.Helper()
	if _, err := database.Exec(`INSERT INTO organizations (id, slug, name) VALUES (?, ?, ?)`, id, id, id); err != nil {
		t.Fatalf("create organization: %v", err)
	}
}

func insertUser(t *testing.T, database *sql.DB, id, organizationID, displayName, email string) {
	t.Helper()
	if _, err := database.Exec(`INSERT INTO users (id, organization_id, display_name, email) VALUES (?, ?, ?, ?)`, id, organizationID, displayName, email); err != nil {
		t.Fatalf("insert user: %v", err)
	}
}

func insertExternalIdentity(t *testing.T, database *sql.DB, id, organizationID, userID, issuer, subject string) {
	t.Helper()
	if _, err := database.Exec(`INSERT INTO external_identities (id, organization_id, user_id, issuer, subject) VALUES (?, ?, ?, ?, ?)`, id, organizationID, userID, issuer, subject); err != nil {
		t.Fatalf("insert external identity: %v", err)
	}
}

func count(t *testing.T, database *sql.DB, table string) int {
	t.Helper()
	var value int
	if err := database.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&value); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return value
}

func readUser(t *testing.T, database *sql.DB, id string) identitycore.User {
	t.Helper()
	var user identitycore.User
	var status string
	var createdAt, updatedAt string
	if err := database.QueryRow(`SELECT id, organization_id, display_name, email, status, created_at, updated_at FROM users WHERE id = ?`, id).Scan(&user.ID, &user.OrganizationID, &user.DisplayName, &user.Email, &status, &createdAt, &updatedAt); err != nil {
		t.Fatalf("read user: %v", err)
	}
	user.Status = identitycore.Status(status)
	user.CreatedAt = parseTestTime(t, createdAt)
	user.UpdatedAt = parseTestTime(t, updatedAt)
	return user
}

func readOrganization(t *testing.T, database *sql.DB, id string) identitycore.Organization {
	t.Helper()
	var organization identitycore.Organization
	var status string
	var createdAt, updatedAt string
	if err := database.QueryRow(`SELECT id, slug, name, status, created_at, updated_at FROM organizations WHERE id = ?`, id).Scan(&organization.ID, &organization.Slug, &organization.Name, &status, &createdAt, &updatedAt); err != nil {
		t.Fatalf("read organization: %v", err)
	}
	organization.Status = identitycore.Status(status)
	organization.CreatedAt = parseTestTime(t, createdAt)
	organization.UpdatedAt = parseTestTime(t, updatedAt)
	return organization
}

func readExternalIdentity(t *testing.T, database *sql.DB, issuer, subject string) identitycore.ExternalIdentity {
	t.Helper()
	var identity identitycore.ExternalIdentity
	var status string
	var lastSeen sql.NullString
	var createdAt, updatedAt string
	if err := database.QueryRow(`SELECT id, organization_id, user_id, issuer, subject, email_snapshot, display_name_snapshot, last_seen_at, status, created_at, updated_at FROM external_identities WHERE issuer = ? AND subject = ?`, issuer, subject).Scan(&identity.ID, &identity.OrganizationID, &identity.UserID, &identity.Issuer, &identity.Subject, &identity.EmailSnapshot, &identity.DisplayNameSnapshot, &lastSeen, &status, &createdAt, &updatedAt); err != nil {
		t.Fatalf("read external identity: %v", err)
	}
	identity.Status = identitycore.Status(status)
	identity.CreatedAt = parseTestTime(t, createdAt)
	identity.UpdatedAt = parseTestTime(t, updatedAt)
	if lastSeen.Valid {
		value := parseTestTime(t, lastSeen.String)
		identity.LastSeenAt = &value
	}
	return identity
}

func parseTestTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		t.Fatalf("parse SQLite timestamp %q: %v", value, err)
	}
	return parsed
}

func sameTime(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.Equal(*right)
}

func assertTime(t *testing.T, actual, want time.Time) {
	t.Helper()
	if !actual.Equal(want) {
		t.Fatalf("timestamp = %s, want %s", actual.Format(time.RFC3339Nano), want.Format(time.RFC3339Nano))
	}
}

func assertSQLiteConstraintCode(t *testing.T, err error, want int) {
	t.Helper()
	if err == nil {
		t.Fatal("SQLite constraint unexpectedly succeeded")
	}
	var sqliteError *sqlite.Error
	if !errors.As(err, &sqliteError) {
		t.Fatalf("error type = %T, want *sqlite.Error", err)
	}
	if got := sqliteError.Code(); got != want {
		t.Fatalf("SQLite extended code = %d, want %d", got, want)
	}
}
