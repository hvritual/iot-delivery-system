package localbootstrap

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/audit"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitycore"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localcredential"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localtx"
	"github.com/hvritual/yunka.io/framework/operation"
	_ "modernc.org/sqlite"
)

func TestYU19BootstrapAtomicallyCreatesAdministratorCredentialLatchAndAudit(t *testing.T) {
	fixture := newFixture(t)
	const password = "YU19-bootstrap-password-sentinel"
	result, err := fixture.manager.Initialize(t.Context(), InitializeInput{
		OrganizationID: "org-a",
		DisplayName:    "Initial Administrator",
		Email:          "admin@example.test",
		Password:       []byte(password),
	})
	if err != nil {
		t.Fatalf("initialize administrator: %v", err)
	}
	if result.OrganizationID != "org-a" || result.UserID == "" || result.RoleBindingID == "" || result.CredentialRevision != 1 {
		t.Fatalf("bootstrap result = %#v", result)
	}
	var userStatus, displayName, email string
	if err := fixture.database.QueryRow(`SELECT status, display_name, COALESCE(email, '') FROM users WHERE id = ? AND organization_id = ?`, result.UserID, result.OrganizationID).Scan(&userStatus, &displayName, &email); err != nil {
		t.Fatalf("read bootstrap user: %v", err)
	}
	if userStatus != "active" || displayName != "Initial Administrator" || email != "admin@example.test" {
		t.Fatalf("bootstrap user = %q/%q/%q", userStatus, displayName, email)
	}
	var roleID, scopeType, scopeID, bindingStatus string
	if err := fixture.database.QueryRow(`SELECT role_id, scope_type, scope_id, status FROM role_bindings WHERE id = ? AND user_id = ?`, result.RoleBindingID, result.UserID).Scan(&roleID, &scopeType, &scopeID, &bindingStatus); err != nil {
		t.Fatalf("read bootstrap role binding: %v", err)
	}
	if roleID != administratorRoleID || scopeType != "organization" || scopeID != "org-a" || bindingStatus != "active" {
		t.Fatalf("bootstrap binding = %q/%q/%q/%q", roleID, scopeType, scopeID, bindingStatus)
	}
	verification, err := fixture.credentials.VerifyPassword(t.Context(), "org-a", result.UserID, []byte(password))
	if err != nil || !verification.Match || verification.Revision != 1 {
		t.Fatalf("bootstrap credential verification=%#v err=%v", verification, err)
	}
	var state, reason, organizationID, initializedUserID string
	if err := fixture.database.QueryRow(`SELECT state, close_reason, COALESCE(organization_id, ''), COALESCE(initialized_user_id, '') FROM iotd_local_admin_bootstrap_state WHERE id = ?`, stateID).Scan(&state, &reason, &organizationID, &initializedUserID); err != nil {
		t.Fatalf("read bootstrap state: %v", err)
	}
	if state != "closed" || reason != "initialized" || organizationID != "org-a" || initializedUserID != result.UserID {
		t.Fatalf("bootstrap state = %q/%q/%q/%q", state, reason, organizationID, initializedUserID)
	}
	var category, actorType, actorID, operationID, auditResult, reasonCode, targetType, targetID, metadata string
	if err := fixture.database.QueryRow(`SELECT event_category, actor_type, COALESCE(actor_id, ''), operation, result, reason_code, COALESCE(target_type, ''), COALESCE(target_id, ''), metadata FROM iotd_audit_entries`).Scan(&category, &actorType, &actorID, &operationID, &auditResult, &reasonCode, &targetType, &targetID, &metadata); err != nil {
		t.Fatalf("read bootstrap audit: %v", err)
	}
	if category != "system" || actorType != "system" || actorID != bootstrapActorID || operationID != initializeOperationID || auditResult != "success" || reasonCode != "bootstrap.initialized" || targetType != "identity.user" || targetID != result.UserID || !strings.Contains(metadata, `"bootstrap_state":"closed"`) || strings.Contains(metadata, password) {
		t.Fatalf("bootstrap audit = %q/%q/%q/%q/%q/%q/%q/%q metadata=%q", category, actorType, actorID, operationID, auditResult, reasonCode, targetType, targetID, metadata)
	}
	assertSecretAbsent(t, fixture.database, password)
}

func TestYU19BootstrapNeverReopensAfterDisableOrDelete(t *testing.T) {
	fixture := newFixture(t)
	result, err := fixture.manager.Initialize(t.Context(), InitializeInput{OrganizationID: "org-a", DisplayName: "Administrator", Password: []byte("first-password")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.Exec(`UPDATE users SET status = 'disabled' WHERE id = ?`, result.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.Exec(`UPDATE role_bindings SET status = 'disabled' WHERE id = ?`, result.RoleBindingID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.manager.Initialize(t.Context(), InitializeInput{OrganizationID: "org-a", DisplayName: "Second", Password: []byte("second-password")}); !errors.Is(err, ErrAlreadyInitialized) {
		t.Fatalf("disabled administrator reopened bootstrap: %v", err)
	}
	if _, err := fixture.database.Exec(`DELETE FROM role_bindings WHERE id = ?`, result.RoleBindingID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.Exec(`DELETE FROM iotd_local_user_credentials WHERE organization_id = ? AND user_id = ?`, result.OrganizationID, result.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.Exec(`DELETE FROM users WHERE id = ?`, result.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.manager.Initialize(t.Context(), InitializeInput{OrganizationID: "org-a", DisplayName: "Third", Password: []byte("third-password")}); !errors.Is(err, ErrAlreadyInitialized) {
		t.Fatalf("deleted administrator reopened bootstrap: %v", err)
	}
	if _, err := fixture.database.Exec(`UPDATE iotd_local_admin_bootstrap_state SET state = 'closed' WHERE id = ?`, stateID); err == nil {
		t.Fatal("immutable bootstrap state accepted update")
	}
	if _, err := fixture.database.Exec(`DELETE FROM iotd_local_admin_bootstrap_state WHERE id = ?`, stateID); err == nil {
		t.Fatal("immutable bootstrap state accepted delete")
	}
	assertCounts(t, fixture.database, 0, 0, 0, 1, 1)
}

func TestYU19PreexistingIdentityPermanentlyClosesAnonymousBootstrap(t *testing.T) {
	database := openDatabase(t, 1)
	if err := identitycore.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if err := localcredential.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	seedOrganization(t, database, "org-a")
	if _, err := database.Exec(`INSERT INTO users (id, organization_id, display_name) VALUES ('existing-user', 'org-a', 'Existing User')`); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if err := audit.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	credentials, err := localcredential.NewSQLiteRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	auditStore, err := audit.NewSQLiteStore(database)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(database, credentials, auditStore, operation.NewExecutorWithOptions(nil, operation.ExecutorOptions{Transactions: localtx.NewSQLiteFactory(database)}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Initialize(t.Context(), InitializeInput{OrganizationID: "org-a", DisplayName: "Anonymous Escalation", Password: []byte("must-not-create")}); !errors.Is(err, ErrPreexistingIdentity) {
		t.Fatalf("preexisting identity bootstrap error=%v, want ErrPreexistingIdentity", err)
	}
	var reason string
	if err := database.QueryRow(`SELECT close_reason FROM iotd_local_admin_bootstrap_state WHERE id = ?`, stateID).Scan(&reason); err != nil || reason != "preexisting_identity" {
		t.Fatalf("preexisting identity close reason=%q err=%v", reason, err)
	}
	assertCounts(t, database, 1, 0, 0, 1, 0)
}

func TestYU19BootstrapFailuresLeaveZeroPartialState(t *testing.T) {
	t.Run("credential", func(t *testing.T) {
		fixture := newFixture(t)
		credentials, err := localcredential.NewSQLiteRepository(fixture.database, localcredential.WithRandomSource(errorReader{}))
		if err != nil {
			t.Fatal(err)
		}
		manager, err := NewManager(fixture.database, credentials, fixture.audit, operation.NewExecutorWithOptions(nil, operation.ExecutorOptions{Transactions: localtx.NewSQLiteFactory(fixture.database)}), WithClock(fixture.clock))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := manager.Initialize(t.Context(), InitializeInput{OrganizationID: "org-a", DisplayName: "Administrator", Password: []byte("credential-failure-password")}); err == nil {
			t.Fatal("credential failure unexpectedly initialized administrator")
		}
		assertCounts(t, fixture.database, 0, 0, 0, 0, 0)
		assertSecretAbsent(t, fixture.database, "credential-failure-password")
	})

	t.Run("audit", func(t *testing.T) {
		fixture := newFixture(t)
		if _, err := fixture.database.Exec(`CREATE TRIGGER yu19_force_audit_failure BEFORE INSERT ON iotd_audit_entries BEGIN SELECT RAISE(ABORT, 'forced audit failure'); END;`); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.manager.Initialize(t.Context(), InitializeInput{OrganizationID: "org-a", DisplayName: "Administrator", Password: []byte("audit-failure-password")}); err == nil {
			t.Fatal("audit failure unexpectedly initialized administrator")
		}
		assertCounts(t, fixture.database, 0, 0, 0, 0, 0)
		assertSecretAbsent(t, fixture.database, "audit-failure-password")
	})
}

func TestYU19ConcurrentBootstrapHasAtMostOneWinnerAndOneDurableAdministrator(t *testing.T) {
	fixture := newFixtureWithConnections(t, 4)
	input := InitializeInput{OrganizationID: "org-a", DisplayName: "Concurrent Administrator", Password: []byte("concurrent-password")}
	errorsByCall := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := fixture.manager.Initialize(context.Background(), input)
			errorsByCall <- err
		}()
	}
	group.Wait()
	close(errorsByCall)
	successes := 0
	for err := range errorsByCall {
		if err == nil {
			successes++
		}
	}
	if successes == 0 {
		if _, err := fixture.manager.Initialize(t.Context(), input); err != nil {
			t.Fatalf("concurrent attempts left an empty system that could not initialize: %v", err)
		}
		successes = 1
	}
	if successes != 1 {
		t.Fatalf("concurrent bootstrap successes=%d, want exactly one durable winner", successes)
	}
	assertCounts(t, fixture.database, 1, 1, 1, 1, 1)
	if _, err := fixture.manager.Initialize(t.Context(), input); !errors.Is(err, ErrAlreadyInitialized) {
		t.Fatalf("post-concurrency retry=%v, want ErrAlreadyInitialized", err)
	}
}

func TestYU19BootstrapOperationIsInternalUnprotectedAndTransactional(t *testing.T) {
	plan := OperationPlan()
	if plan.OperationID != initializeOperationID || plan.Domain != "identity" || plan.Application != "local-admin-bootstrap" || plan.Execution.Transaction != "local" || plan.Execution.Idempotency != "none" || operation.Protected(plan) || len(plan.Security.Authentication) != 0 || len(plan.Security.Permissions) != 0 || plan.Security.TenantRequired || plan.Bindings.RPC != "" || len(plan.Bindings.HTTP) != 0 || plan.Composition.Boundary != "local" {
		t.Fatalf("bootstrap operation contract drifted: %#v", plan)
	}
}

type fixture struct {
	database    *sql.DB
	credentials *localcredential.SQLiteRepository
	audit       *audit.SQLiteStore
	manager     *Manager
	clock       func() time.Time
}

func newFixture(t *testing.T) *fixture {
	return newFixtureWithConnections(t, 1)
}

func newFixtureWithConnections(t *testing.T, connections int) *fixture {
	t.Helper()
	database := openDatabase(t, connections)
	if err := identitycore.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatalf("apply identity migrations: %v", err)
	}
	if err := localcredential.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatalf("apply local credential migrations: %v", err)
	}
	if err := ApplyMigrations(t.Context(), database); err != nil {
		t.Fatalf("apply local bootstrap migrations: %v", err)
	}
	if err := audit.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatalf("apply audit migrations: %v", err)
	}
	seedOrganization(t, database, "org-a")
	clock := func() time.Time { return time.Date(2026, 9, 5, 6, 30, 0, 0, time.UTC) }
	credentials, err := localcredential.NewSQLiteRepository(database, localcredential.WithClock(clock))
	if err != nil {
		t.Fatal(err)
	}
	auditStore, err := audit.NewSQLiteStore(database, audit.WithClock(clock))
	if err != nil {
		t.Fatal(err)
	}
	executor := operation.NewExecutorWithOptions(nil, operation.ExecutorOptions{Transactions: localtx.NewSQLiteFactory(database)})
	manager, err := NewManager(database, credentials, auditStore, executor, WithClock(clock))
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{database: database, credentials: credentials, audit: auditStore, manager: manager, clock: clock}
}

func openDatabase(t *testing.T, connections int) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "bootstrap.db"))
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(connections)
	database.SetMaxIdleConns(connections)
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func seedOrganization(t *testing.T, database *sql.DB, organizationID string) {
	t.Helper()
	if _, err := database.Exec(`INSERT INTO organizations (id, slug, name, status) VALUES (?, ?, ?, 'active')`, organizationID, organizationID, organizationID); err != nil {
		t.Fatal(err)
	}
}

func assertCounts(t *testing.T, database *sql.DB, users, bindings, credentials, states, audits int) {
	t.Helper()
	for name, check := range map[string]struct {
		query string
		want  int
	}{
		"users":       {query: `SELECT COUNT(*) FROM users`, want: users},
		"bindings":    {query: `SELECT COUNT(*) FROM role_bindings`, want: bindings},
		"credentials": {query: `SELECT COUNT(*) FROM iotd_local_user_credentials`, want: credentials},
		"states":      {query: `SELECT COUNT(*) FROM iotd_local_admin_bootstrap_state`, want: states},
		"audits":      {query: `SELECT COUNT(*) FROM iotd_audit_entries`, want: audits},
	} {
		var got int
		if err := database.QueryRow(check.query).Scan(&got); err != nil || got != check.want {
			t.Fatalf("%s count=%d err=%v, want %d", name, got, err, check.want)
		}
	}
}

func assertSecretAbsent(t *testing.T, database *sql.DB, sentinel string) {
	t.Helper()
	queries := []string{
		`SELECT COUNT(*) FROM iotd_local_user_credentials WHERE instr(CAST(salt AS TEXT), ?) > 0 OR instr(CAST(password_hash AS TEXT), ?) > 0`,
		`SELECT COUNT(*) FROM iotd_local_admin_bootstrap_state WHERE instr(COALESCE(organization_id, ''), ?) > 0 OR instr(COALESCE(initialized_user_id, ''), ?) > 0`,
		`SELECT COUNT(*) FROM iotd_audit_entries WHERE instr(COALESCE(diff_summary, ''), ?) > 0 OR instr(COALESCE(metadata, ''), ?) > 0`,
	}
	for _, query := range queries {
		var leaked int
		if err := database.QueryRow(query, sentinel, sentinel).Scan(&leaked); err != nil {
			t.Fatal(err)
		}
		if leaked != 0 {
			t.Fatalf("secret sentinel %q leaked into durable bootstrap state", sentinel)
		}
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("random unavailable") }
