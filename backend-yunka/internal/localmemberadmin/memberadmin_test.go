package localmemberadmin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/audit"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/humanauthz"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitycore"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localbootstrap"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localcredential"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localoutbox"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localtx"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/framework/core/runtimecontext"
	"github.com/hvritual/yunka.io/framework/operation"
	"github.com/hvritual/yunka.io/gateway/authz"
	_ "modernc.org/sqlite"
)

func TestYU20DurableAdministratorCreatesIndependentMembersResetsAndDisables(t *testing.T) {
	fixture := newMemberAdminFixture(t)
	ctx := fixture.adminContext(t)
	const (
		sharedName     = "Same Member"
		sharedEmail    = "same@example.invalid"
		firstPassword  = "YU20-first-password-sentinel"
		secondPassword = "YU20-second-password-sentinel"
		resetPassword  = "YU20-reset-password-sentinel"
	)
	first, err := fixture.manager.Create(ctx, CreateInput{DisplayName: sharedName, Email: sharedEmail, Password: []byte(firstPassword)})
	if err != nil {
		t.Fatalf("create first member: %v", err)
	}
	second, err := fixture.manager.Create(ctx, CreateInput{DisplayName: sharedName, Email: sharedEmail, Password: []byte(secondPassword)})
	if err != nil {
		t.Fatalf("create second member with same profile: %v", err)
	}
	if first.UserID == second.UserID || first.UserRevision != 1 || second.UserRevision != 1 || first.CredentialRevision != 1 || second.CredentialRevision != 1 {
		t.Fatalf("independent member results first=%#v second=%#v", first, second)
	}
	var profileRows, distinctUsers int
	if err := fixture.database.QueryRow(`SELECT COUNT(*), COUNT(DISTINCT id) FROM users WHERE organization_id = 'org-a' AND display_name = ? AND email = ?`, sharedName, sharedEmail).Scan(&profileRows, &distinctUsers); err != nil {
		t.Fatal(err)
	}
	if profileRows != 2 || distinctUsers != 2 {
		t.Fatalf("same profile rows=%d distinct users=%d, want 2/2", profileRows, distinctUsers)
	}
	reset, err := fixture.manager.ResetCredential(ctx, ResetCredentialInput{
		UserID: first.UserID, ExpectedUserRevision: 1, ExpectedCredentialRevision: 1, Password: []byte(resetPassword),
	})
	if err != nil || reset.UserRevision != 2 || reset.CredentialRevision != 2 {
		t.Fatalf("reset result=%#v error=%v", reset, err)
	}
	oldPassword, err := fixture.credentials.VerifyPassword(t.Context(), "org-a", first.UserID, []byte(firstPassword))
	if err != nil || oldPassword.Match {
		t.Fatalf("old password verification=%#v error=%v", oldPassword, err)
	}
	newPassword, err := fixture.credentials.VerifyPassword(t.Context(), "org-a", first.UserID, []byte(resetPassword))
	if err != nil || !newPassword.Match || newPassword.Revision != 2 {
		t.Fatalf("new password verification=%#v error=%v", newPassword, err)
	}
	disabled, err := fixture.manager.Disable(ctx, DisableInput{UserID: first.UserID, ExpectedRevision: 2})
	if err != nil || disabled.UserRevision != 3 || disabled.CredentialRevision != 2 {
		t.Fatalf("disable result=%#v error=%v", disabled, err)
	}
	status, revision := fixture.userState(t, "org-a", first.UserID)
	if status != "disabled" || revision != 3 {
		t.Fatalf("disabled user state=%q/%d", status, revision)
	}
	if snapshot, err := fixture.outbox.Snapshot(t.Context()); err != nil || snapshot.Pending != 4 {
		t.Fatalf("member admin Outbox=%#v error=%v, want four pending events", snapshot, err)
	}
	fixture.assertNoSensitiveEventData(t, []string{firstPassword, secondPassword, resetPassword, sharedEmail, sharedName})
}

func TestYU20RequiresDurableSystemAdministratorAndEnforcesTenantBoundary(t *testing.T) {
	fixture := newMemberAdminFixture(t)
	member, err := fixture.manager.Create(fixture.adminContext(t), CreateInput{DisplayName: "Regular", Password: []byte("regular-password")})
	if err != nil {
		t.Fatal(err)
	}
	beforeUsers, beforeOutbox := fixture.userCount(t), fixture.outboxCount(t)
	forged := fixture.humanContext(t, "org-a", member.UserID, []string{"system-administrator", "forged-admin"})
	if _, err := fixture.manager.Create(forged, CreateInput{DisplayName: "Forbidden", Password: []byte("forbidden-password")}); !authz.IsDenied(err) {
		t.Fatalf("forged Principal.Roles result=%v, want authorization denied", err)
	}
	if fixture.userCount(t) != beforeUsers || fixture.outboxCount(t) != beforeOutbox {
		t.Fatal("authorization denial changed users or Outbox")
	}
	entry := fixture.latestAudit(t, OperationCreateMember)
	if entry.EventCategory != audit.EventCategoryAuthorization || entry.ActorID != member.UserID || entry.Result != audit.ResultDenied || entry.ReasonCode != "authorization.denied" {
		t.Fatalf("authorization denial audit=%#v", entry)
	}
	if _, err := fixture.database.Exec(`INSERT INTO users (id, organization_id, display_name, status) VALUES ('foreign-user', 'org-b', 'Foreign', 'active')`); err != nil {
		t.Fatal(err)
	}
	beforeOutbox = fixture.outboxCount(t)
	if _, err := fixture.manager.Disable(fixture.adminContext(t), DisableInput{UserID: "foreign-user", ExpectedRevision: 1}); !errors.Is(err, ErrMemberNotFound) {
		t.Fatalf("cross-tenant disable error=%v, want ErrMemberNotFound", err)
	}
	status, revision := fixture.userState(t, "org-b", "foreign-user")
	if status != "active" || revision != 1 || fixture.outboxCount(t) != beforeOutbox {
		t.Fatalf("cross-tenant target changed status=%q revision=%d outbox=%d", status, revision, fixture.outboxCount(t))
	}
}

func TestYU20CASConflictsRollbackUserAndCredentialTogether(t *testing.T) {
	fixture := newMemberAdminFixture(t)
	member, err := fixture.manager.Create(fixture.adminContext(t), CreateInput{DisplayName: "CAS", Password: []byte("initial-password")})
	if err != nil {
		t.Fatal(err)
	}
	beforeOutbox := fixture.outboxCount(t)
	if _, err := fixture.manager.ResetCredential(fixture.adminContext(t), ResetCredentialInput{
		UserID: member.UserID, ExpectedUserRevision: 7, ExpectedCredentialRevision: 1, Password: []byte("stale-user-password"),
	}); !errors.Is(err, ErrMemberRevisionConflict) {
		t.Fatalf("stale user revision error=%v, want ErrMemberRevisionConflict", err)
	}
	assertMemberVersion(t, fixture, member.UserID, 1, 1)
	if fixture.outboxCount(t) != beforeOutbox {
		t.Fatal("stale user revision staged Outbox event")
	}
	if _, err := fixture.manager.ResetCredential(fixture.adminContext(t), ResetCredentialInput{
		UserID: member.UserID, ExpectedUserRevision: 1, ExpectedCredentialRevision: 0, Password: []byte("stale-credential-password"),
	}); !errors.Is(err, localcredential.ErrRevisionConflict) {
		t.Fatalf("stale credential revision error=%v, want local credential conflict", err)
	}
	assertMemberVersion(t, fixture, member.UserID, 1, 1)
	if fixture.outboxCount(t) != beforeOutbox {
		t.Fatal("credential CAS conflict staged Outbox event")
	}
	verification, err := fixture.credentials.VerifyPassword(t.Context(), "org-a", member.UserID, []byte("initial-password"))
	if err != nil || !verification.Match {
		t.Fatalf("credential conflict changed password verification=%#v error=%v", verification, err)
	}
	for _, sentinel := range []string{"stale-user-password", "stale-credential-password"} {
		fixture.assertNoSensitiveEventData(t, []string{sentinel})
	}
}

func TestYU20AuditAndOutboxFailuresRollbackBusinessStateBeforeFailureAudit(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		install func(*testing.T, *memberAdminFixture)
	}{
		{
			name: "success audit failure",
			install: func(t *testing.T, fixture *memberAdminFixture) {
				t.Helper()
				if _, err := fixture.database.Exec(`CREATE TRIGGER yu20_fail_success_audit BEFORE INSERT ON iotd_audit_entries
WHEN NEW.operation = 'identity.members.create' AND NEW.result = 'success'
BEGIN SELECT RAISE(ABORT, 'forced YU20 success audit failure'); END`); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "outbox failure",
			install: func(t *testing.T, fixture *memberAdminFixture) {
				t.Helper()
				if _, err := fixture.database.Exec(`CREATE TRIGGER yu20_fail_outbox BEFORE INSERT ON iotd_outbox
BEGIN SELECT RAISE(ABORT, 'forced YU20 Outbox failure'); END`); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			fixture := newMemberAdminFixture(t)
			scenario.install(t, fixture)
			beforeUsers, beforeCredentials, beforeOutbox := fixture.userCount(t), fixture.credentialCount(t), fixture.outboxCount(t)
			if _, err := fixture.manager.Create(fixture.adminContext(t), CreateInput{DisplayName: "Rollback", Password: []byte("YU20-rollback-secret")}); err == nil {
				t.Fatal("forced persistence failure unexpectedly committed member")
			}
			if fixture.userCount(t) != beforeUsers || fixture.credentialCount(t) != beforeCredentials || fixture.outboxCount(t) != beforeOutbox {
				t.Fatal("forced persistence failure left user, credential, or Outbox residue")
			}
			entry := fixture.latestAudit(t, OperationCreateMember)
			if entry.EventCategory != audit.EventCategoryConfiguration || entry.Result != audit.ResultFailure || entry.ReasonCode != "identity.transaction_rolled_back" || entry.ActorID != fixture.adminID {
				t.Fatalf("post-rollback audit=%#v", entry)
			}
			fixture.assertNoSensitiveEventData(t, []string{"YU20-rollback-secret"})
		})
	}
}

func TestYU20CannotDisableLastSystemAdministratorEvenThroughDirectSQL(t *testing.T) {
	fixture := newMemberAdminFixture(t)
	beforeOutbox := fixture.outboxCount(t)
	if _, err := fixture.manager.Disable(fixture.adminContext(t), DisableInput{UserID: fixture.adminID, ExpectedRevision: 1}); !errors.Is(err, ErrLastAdministrator) {
		t.Fatalf("last administrator disable error=%v, want ErrLastAdministrator", err)
	}
	status, revision := fixture.userState(t, "org-a", fixture.adminID)
	if status != "active" || revision != 1 || fixture.outboxCount(t) != beforeOutbox {
		t.Fatalf("last administrator changed through manager status=%q revision=%d outbox=%d", status, revision, fixture.outboxCount(t))
	}
	if _, err := fixture.database.Exec(`UPDATE users SET status = 'disabled' WHERE organization_id = 'org-a' AND id = ?`, fixture.adminID); err == nil || !strings.Contains(strings.ToLower(err.Error()), lastAdministratorAbort) {
		t.Fatalf("direct SQL last administrator disable error=%v", err)
	}
	status, revision = fixture.userState(t, "org-a", fixture.adminID)
	if status != "active" || revision != 1 {
		t.Fatalf("last administrator changed through direct SQL status=%q revision=%d", status, revision)
	}
}

type memberAdminFixture struct {
	database    *sql.DB
	credentials *localcredential.SQLiteRepository
	auditStore  *audit.SQLiteStore
	outbox      *localoutbox.SQLiteStore
	manager     *Manager
	adminID     string
}

func newMemberAdminFixture(t *testing.T) *memberAdminFixture {
	t.Helper()
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "yu20.db"))
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = database.Close() })
	if err := identitycore.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatalf("apply identity migrations: %v", err)
	}
	if err := localcredential.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatalf("apply local credential migrations: %v", err)
	}
	if err := localbootstrap.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatalf("apply bootstrap migrations: %v", err)
	}
	if err := ApplyMigrations(t.Context(), database); err != nil {
		t.Fatalf("apply member admin migrations: %v", err)
	}
	if err := audit.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatalf("apply audit migrations: %v", err)
	}
	for _, statement := range []string{
		`INSERT INTO organizations (id, slug, name, status) VALUES ('org-a', 'org-a', 'Organization A', 'active')`,
		`INSERT INTO organizations (id, slug, name, status) VALUES ('org-b', 'org-b', 'Organization B', 'active')`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	clock := func() time.Time { return time.Date(2026, 9, 5, 6, 30, 0, 0, time.UTC) }
	credentials, err := localcredential.NewSQLiteRepository(database, localcredential.WithClock(clock))
	if err != nil {
		t.Fatal(err)
	}
	auditStore, err := audit.NewSQLiteStore(database, audit.WithClock(clock))
	if err != nil {
		t.Fatal(err)
	}
	bootstrap, err := localbootstrap.NewManager(
		database,
		credentials,
		auditStore,
		operation.NewExecutorWithOptions(nil, operation.ExecutorOptions{Transactions: localtx.NewSQLiteFactory(database)}),
		localbootstrap.WithClock(clock),
		localbootstrap.WithIDGenerator(sequenceID("bootstrap")),
	)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := bootstrap.Initialize(t.Context(), localbootstrap.InitializeInput{
		OrganizationID: "org-a", DisplayName: "Administrator", Email: "admin@example.invalid", Password: []byte("bootstrap-admin-password"),
	})
	if err != nil {
		t.Fatalf("initialize first administrator: %v", err)
	}
	humanResolver, err := humanauthz.NewGrantResolver(database)
	if err != nil {
		t.Fatal(err)
	}
	authorizer, err := authz.NewGrantAuthorizerWithResolver(humanResolver)
	if err != nil {
		t.Fatal(err)
	}
	guard, err := NewOperationGuard(database)
	if err != nil {
		t.Fatal(err)
	}
	security, err := authz.NewExecutionSecurity(authorizer, guard.GuardResolver())
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := audit.NewSecurityRecorder(auditStore)
	if err != nil {
		t.Fatal(err)
	}
	executor, err := audit.NewRecordingExecutor(operation.NewExecutorWithOptions(security, operation.ExecutorOptions{Transactions: localtx.NewSQLiteFactory(database)}), recorder)
	if err != nil {
		t.Fatal(err)
	}
	outboxStore, err := localoutbox.NewSQLiteStore(database)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(database, credentials, auditStore, outboxStore, executor, WithClock(clock), WithIDGenerator(sequenceID("member")))
	if err != nil {
		t.Fatal(err)
	}
	return &memberAdminFixture{database: database, credentials: credentials, auditStore: auditStore, outbox: outboxStore, manager: manager, adminID: admin.UserID}
}

func sequenceID(prefix string) func() (string, error) {
	n := 0
	return func() (string, error) {
		n++
		return fmt.Sprintf("%s-%d", prefix, n), nil
	}
}

func (fixture *memberAdminFixture) adminContext(t *testing.T) context.Context {
	t.Helper()
	return fixture.humanContext(t, "org-a", fixture.adminID, []string{"forged-role-must-not-authorize"})
}

func (fixture *memberAdminFixture) humanContext(t *testing.T, organizationID, userID string, roles []string) context.Context {
	t.Helper()
	ctx := identity.WithPrincipal(t.Context(), identity.Principal{
		Authenticated: true,
		AuthMethod:    identity.AuthMethodJWT,
		TenantID:      organizationID,
		UserID:        userID,
		Roles:         roles,
	})
	ctx = runtimecontext.WithTraceID(ctx, "0123456789abcdef0123456789abcdef")
	return runtimecontext.WithMetadata(ctx, runtimecontext.Metadata{
		Transport: "internal-test",
		Protocol:  "internal",
		RequestID: "request-yu20",
		Attributes: map[string]string{
			"correlation_id": "correlation-yu20",
			"authorization":  "Bearer YU20-authorization-secret",
			"session":        "YU20-session-secret",
			"csrf":           "YU20-csrf-secret",
		},
	})
}

func (fixture *memberAdminFixture) userState(t *testing.T, organizationID, userID string) (string, int64) {
	t.Helper()
	var status string
	var revision int64
	if err := fixture.database.QueryRow(`SELECT status, revision FROM users WHERE organization_id = ? AND id = ?`, organizationID, userID).Scan(&status, &revision); err != nil {
		t.Fatal(err)
	}
	return status, revision
}

func (fixture *memberAdminFixture) userCount(t *testing.T) int {
	t.Helper()
	var count int
	if err := fixture.database.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func (fixture *memberAdminFixture) credentialCount(t *testing.T) int {
	t.Helper()
	var count int
	if err := fixture.database.QueryRow(`SELECT COUNT(*) FROM iotd_local_user_credentials`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func (fixture *memberAdminFixture) outboxCount(t *testing.T) int {
	t.Helper()
	var count int
	if err := fixture.database.QueryRow(`SELECT COUNT(*) FROM iotd_outbox`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func (fixture *memberAdminFixture) latestAudit(t *testing.T, operationID string) audit.Entry {
	t.Helper()
	page, err := fixture.auditStore.Query(t.Context(), audit.Query{OrganizationID: "org-a", Operation: operationID})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) == 0 {
		t.Fatalf("no audit entry for %s", operationID)
	}
	return page.Entries[len(page.Entries)-1]
}

func (fixture *memberAdminFixture) assertNoSensitiveEventData(t *testing.T, sentinels []string) {
	t.Helper()
	var auditText, outboxText strings.Builder
	rows, err := fixture.database.Query(`SELECT COALESCE(diff_summary, ''), COALESCE(metadata, ''), COALESCE(reason_code, ''), COALESCE(target_id, '') FROM iotd_audit_entries`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var values [4]string
		if err := rows.Scan(&values[0], &values[1], &values[2], &values[3]); err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		auditText.WriteString(strings.Join(values[:], "|"))
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	outboxRows, err := fixture.database.Query(`SELECT envelope_json FROM iotd_outbox`)
	if err != nil {
		t.Fatal(err)
	}
	for outboxRows.Next() {
		var value string
		if err := outboxRows.Scan(&value); err != nil {
			_ = outboxRows.Close()
			t.Fatal(err)
		}
		outboxText.WriteString(value)
	}
	if err := outboxRows.Close(); err != nil {
		t.Fatal(err)
	}
	persisted := auditText.String() + outboxText.String()
	for _, sentinel := range append(sentinels, "YU20-authorization-secret", "YU20-session-secret", "YU20-csrf-secret") {
		if strings.Contains(persisted, sentinel) {
			t.Fatalf("sensitive value %q leaked into audit/Outbox: %q", sentinel, persisted)
		}
	}
}

func assertMemberVersion(t *testing.T, fixture *memberAdminFixture, userID string, wantUser, wantCredential int64) {
	t.Helper()
	status, revision := fixture.userState(t, "org-a", userID)
	metadata, err := fixture.credentials.Metadata(t.Context(), "org-a", userID)
	if err != nil {
		t.Fatal(err)
	}
	if status != "active" || revision != wantUser || metadata.Revision != wantCredential {
		t.Fatalf("member version status=%q user=%d credential=%d, want active/%d/%d", status, revision, metadata.Revision, wantUser, wantCredential)
	}
}
