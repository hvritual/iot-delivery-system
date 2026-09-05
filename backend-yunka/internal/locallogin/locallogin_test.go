package locallogin

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/audit"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitycore"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localcredential"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localmemberadmin"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localtx"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/framework/core/runtimecontext"
	"github.com/hvritual/yunka.io/framework/operation"
	_ "modernc.org/sqlite"
)

func TestYU21LoginCreatesOpaqueSessionAndOnlyVerifiedJWTBuildsPrincipal(t *testing.T) {
	fixture := newLoginFixture(t, false)
	result, err := fixture.manager.Login(fixture.context(t), LoginInput{
		OrganizationID: "org-a", UserID: "user-a", Password: []byte("YU21-password-secret"),
	})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if result.SessionID == "" || result.SessionToken == "" || result.AccessToken == "" || result.UserID != "user-a" || result.OrganizationID != "org-a" {
		t.Fatalf("login result=%#v", result)
	}
	rawSession, err := base64.RawURLEncoding.DecodeString(result.SessionToken)
	if err != nil || len(rawSession) != sessionSecretBytes || base64.RawURLEncoding.EncodeToString(rawSession) != result.SessionToken {
		t.Fatalf("opaque session token length/encoding invalid: bytes=%d error=%v", len(rawSession), err)
	}
	var storedDigest []byte
	var storedTokenText string
	if err := fixture.database.QueryRow(`SELECT secret_digest, CAST(secret_digest AS TEXT) FROM iotd_local_sessions WHERE id = ?`, result.SessionID).Scan(&storedDigest, &storedTokenText); err != nil {
		t.Fatal(err)
	}
	wantDigest := sha256.Sum256(rawSession)
	if !bytes.Equal(storedDigest, wantDigest[:]) || bytes.Equal(storedDigest, rawSession) || storedTokenText == result.SessionToken {
		t.Fatal("session persistence did not store only the one-way digest")
	}
	principal, err := fixture.manager.VerifyAccessToken(t.Context(), result.AccessToken)
	if err != nil {
		t.Fatalf("verify access token: %v", err)
	}
	if !principal.Authenticated || principal.AuthMethod != identity.AuthMethodJWT || principal.UserID != "user-a" || principal.TenantID != "org-a" || principal.Subject != "local-user/user-a" || len(principal.Roles) != 0 {
		t.Fatalf("verified principal=%#v", principal)
	}
	parts := strings.Split(result.AccessToken, ".")
	parts[2] = strings.Repeat("A", len(parts[2]))
	if principal, err := fixture.manager.VerifyAccessToken(t.Context(), strings.Join(parts, ".")); !errors.Is(err, ErrAccessTokenInvalid) || principal.Authenticated {
		t.Fatalf("tampered token principal=%#v error=%v", principal, err)
	}
	wrongIssuer := fixture.config
	wrongIssuer.Issuer = "wrong.local"
	badIssuerToken, _, err := signAccessToken(wrongIssuer, "org-a", "user-a", result.SessionID, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	if principal, err := fixture.manager.VerifyAccessToken(t.Context(), badIssuerToken); !errors.Is(err, ErrAccessTokenInvalid) || principal.Authenticated {
		t.Fatalf("wrong issuer principal=%#v error=%v", principal, err)
	}
	wrongAudience := fixture.config
	wrongAudience.Audience = "wrong-audience"
	badAudienceToken, _, err := signAccessToken(wrongAudience, "org-a", "user-a", result.SessionID, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.manager.VerifyAccessToken(t.Context(), badAudienceToken); !errors.Is(err, ErrAccessTokenInvalid) {
		t.Fatalf("wrong audience token error=%v", err)
	}
	wrongKey := fixture.config
	wrongKey.SigningKey = bytes.Repeat([]byte{0x44}, 32)
	badKeyToken, _, err := signAccessToken(wrongKey, "org-a", "user-a", result.SessionID, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.manager.VerifyAccessToken(t.Context(), badKeyToken); !errors.Is(err, ErrAccessTokenInvalid) {
		t.Fatalf("wrong signing key token error=%v", err)
	}
	badVersion := signRawJWT(t, fixture.config, jwtClaims{
		Issuer: fixture.config.Issuer, Audience: fixture.config.Audience, Subject: "user-a", TenantID: "org-a", SessionID: result.SessionID,
		IssuedAt: fixture.now.Unix(), ExpiresAt: fixture.now.Add(fixture.config.AccessTTL).Unix(), Version: JWTVersion + 1,
	})
	if _, err := fixture.manager.VerifyAccessToken(t.Context(), badVersion); !errors.Is(err, ErrAccessTokenInvalid) {
		t.Fatalf("wrong JWT version error=%v", err)
	}
	if _, err := fixture.database.Exec(`DELETE FROM iotd_local_sessions WHERE id = ?`, result.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.manager.VerifyAccessToken(t.Context(), result.AccessToken); !errors.Is(err, ErrAccessTokenInvalid) {
		t.Fatalf("JWT without server session error=%v", err)
	}
	fixture.assertSecretsAbsent(t, []string{"YU21-password-secret", result.SessionToken, result.AccessToken, string(fixture.config.SigningKey)})
}

func TestYU21UnknownDisabledCrossTenantAndWrongPasswordShareFailureCategory(t *testing.T) {
	fixture := newLoginFixture(t, true)
	cases := []LoginInput{
		{OrganizationID: "org-a", UserID: "missing-user", Password: []byte("YU21-missing-secret")},
		{OrganizationID: "org-a", UserID: "disabled-user", Password: []byte("YU21-disabled-secret")},
		{OrganizationID: "org-b", UserID: "user-a", Password: []byte("YU21-cross-tenant-secret")},
		{OrganizationID: "org-a", UserID: "user-a", Password: []byte("YU21-wrong-secret")},
		{OrganizationID: "org-a", UserID: "no-credential", Password: []byte("YU21-no-credential-secret")},
	}
	for _, input := range cases {
		beforeSessions := fixture.sessionCount(t)
		if _, err := fixture.manager.Login(fixture.context(t), input); !errors.Is(err, ErrAuthenticationFailed) || err.Error() != ErrAuthenticationFailed.Error() {
			t.Fatalf("login failure org=%q user=%q error=%v", input.OrganizationID, input.UserID, err)
		}
		if fixture.sessionCount(t) != beforeSessions {
			t.Fatalf("failed login org=%q user=%q created a session", input.OrganizationID, input.UserID)
		}
	}
	if fixture.synthetic.Count() == 0 {
		t.Fatal("negative User/credential paths did not consume synthetic password verification work")
	}
	page, err := fixture.auditStore.Query(t.Context(), audit.Query{SystemScope: true, Operation: OperationLogin})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != len(cases) {
		t.Fatalf("anonymous failure audit count=%d, want %d", len(page.Entries), len(cases))
	}
	for _, entry := range page.Entries {
		if entry.EventCategory != audit.EventCategoryAuthentication || entry.ActorType != audit.ActorAnonymous || entry.Result != audit.ResultFailure || entry.ReasonCode != "authentication.local_login_failed" || entry.OrganizationID != "" || entry.TargetID != "" {
			t.Fatalf("failure audit leaked identity classification: %#v", entry)
		}
	}
	fixture.assertSecretsAbsent(t, []string{"YU21-missing-secret", "YU21-disabled-secret", "YU21-cross-tenant-secret", "YU21-wrong-secret", "YU21-no-credential-secret"})
}

func TestYU21SuccessfulLoginRehashesAtomicallyWithSessionAndAudit(t *testing.T) {
	fixture := newLoginFixture(t, false)
	oldMetadata, err := fixture.credentials.Metadata(t.Context(), "org-a", "user-a")
	if err != nil {
		t.Fatal(err)
	}
	if oldMetadata.PolicyVersion != 1 || oldMetadata.Revision != 1 {
		t.Fatalf("initial credential=%#v", oldMetadata)
	}
	policyV1 := localcredential.DefaultPolicy()
	policyV2 := policyV1
	policyV2.PolicyVersion = 2
	policies, err := localcredential.NewPolicySet(2, policyV1, policyV2)
	if err != nil {
		t.Fatal(err)
	}
	upgradingRepo, err := localcredential.NewSQLiteRepository(fixture.database, localcredential.WithPolicySet(policies), localcredential.WithClock(func() time.Time { return fixture.now }))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(
		fixture.database, upgradingRepo, fixture.auditStore,
		operation.NewExecutorWithOptions(nil, operation.ExecutorOptions{Transactions: localtx.NewSQLiteFactory(fixture.database)}),
		fixture.config,
		WithClock(func() time.Time { return fixture.now }), WithIDGenerator(sequenceID("rehash")), WithRandomSource(bytes.NewReader(bytes.Repeat([]byte{0x7a}, 256))),
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.Login(fixture.context(t), LoginInput{OrganizationID: "org-a", UserID: "user-a", Password: []byte("YU21-password-secret")})
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := upgradingRepo.Metadata(t.Context(), "org-a", "user-a")
	if err != nil {
		t.Fatal(err)
	}
	if metadata.PolicyVersion != 2 || metadata.Revision != 2 {
		t.Fatalf("rehash metadata=%#v", metadata)
	}
	var sessionRevision int64
	if err := fixture.database.QueryRow(`SELECT credential_revision FROM iotd_local_sessions WHERE id = ?`, result.SessionID).Scan(&sessionRevision); err != nil || sessionRevision != 2 {
		t.Fatalf("session credential revision=%d error=%v", sessionRevision, err)
	}
	page, err := fixture.auditStore.Query(t.Context(), audit.Query{OrganizationID: "org-a", Operation: OperationLogin})
	if err != nil || len(page.Entries) != 1 || !strings.Contains(page.Entries[0].Metadata, `"credential_rehashed":true`) {
		t.Fatalf("rehash success audit=%#v error=%v", page, err)
	}
}

func TestYU21SuccessAuditFailureRollsBackSessionAndCredentialRehash(t *testing.T) {
	fixture := newLoginFixture(t, false)
	policyV1 := localcredential.DefaultPolicy()
	policyV2 := policyV1
	policyV2.PolicyVersion = 2
	policies, err := localcredential.NewPolicySet(2, policyV1, policyV2)
	if err != nil {
		t.Fatal(err)
	}
	upgradingRepo, err := localcredential.NewSQLiteRepository(fixture.database, localcredential.WithPolicySet(policies), localcredential.WithClock(func() time.Time { return fixture.now }))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(
		fixture.database, upgradingRepo, fixture.auditStore,
		operation.NewExecutorWithOptions(nil, operation.ExecutorOptions{Transactions: localtx.NewSQLiteFactory(fixture.database)}),
		fixture.config,
		WithClock(func() time.Time { return fixture.now }), WithIDGenerator(sequenceID("rollback")), WithRandomSource(bytes.NewReader(bytes.Repeat([]byte{0x3c}, 256))),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.Exec(`CREATE TRIGGER yu21_fail_login_success_audit BEFORE INSERT ON iotd_audit_entries
WHEN NEW.operation = 'identity.local-login.authenticate' AND NEW.result = 'success'
BEGIN SELECT RAISE(ABORT, 'forced YU21 login success audit failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Login(fixture.context(t), LoginInput{OrganizationID: "org-a", UserID: "user-a", Password: []byte("YU21-password-secret")}); err == nil || errors.Is(err, ErrAuthenticationFailed) {
		t.Fatalf("forced success-audit failure error=%v", err)
	}
	if fixture.sessionCount(t) != 0 {
		t.Fatal("success-audit failure left a session")
	}
	metadata, err := upgradingRepo.Metadata(t.Context(), "org-a", "user-a")
	if err != nil {
		t.Fatal(err)
	}
	if metadata.PolicyVersion != 1 || metadata.Revision != 1 {
		t.Fatalf("success-audit failure left credential rehash=%#v", metadata)
	}
}

func TestYU21ExpiredTokenCannotCreatePrincipal(t *testing.T) {
	fixture := newLoginFixture(t, false)
	result, err := fixture.manager.Login(fixture.context(t), LoginInput{OrganizationID: "org-a", UserID: "user-a", Password: []byte("YU21-password-secret")})
	if err != nil {
		t.Fatal(err)
	}
	fixture.now = result.AccessExpiresAt
	if principal, err := fixture.manager.VerifyAccessToken(t.Context(), result.AccessToken); !errors.Is(err, ErrAccessTokenInvalid) || principal.Authenticated {
		t.Fatalf("expired token principal=%#v error=%v", principal, err)
	}
}

type countingReader struct {
	data  []byte
	count int
}

func (reader *countingReader) Read(buffer []byte) (int, error) {
	if len(reader.data) == 0 {
		return 0, errors.New("counting random exhausted")
	}
	for index := range buffer {
		buffer[index] = reader.data[(reader.count+index)%len(reader.data)]
	}
	reader.count += len(buffer)
	return len(buffer), nil
}

func (reader *countingReader) Count() int { return reader.count }
func (reader *countingReader) Reset()     { reader.count = 0 }

type loginFixture struct {
	database    *sql.DB
	credentials *localcredential.SQLiteRepository
	auditStore  *audit.SQLiteStore
	manager     *Manager
	config      Config
	synthetic   *countingReader
	now         time.Time
}

func newLoginFixture(t *testing.T, extraUsers bool) *loginFixture {
	t.Helper()
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "yu21.db"))
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	if err := identitycore.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if err := localcredential.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if err := localmemberadmin.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if err := audit.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO organizations (id, slug, name, status) VALUES ('org-a', 'org-a', 'Organization A', 'active')`,
		`INSERT INTO organizations (id, slug, name, status) VALUES ('org-b', 'org-b', 'Organization B', 'active')`,
		`INSERT INTO users (id, organization_id, display_name, status, revision) VALUES ('user-a', 'org-a', 'User A', 'active', 1)`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if extraUsers {
		for _, statement := range []string{
			`INSERT INTO users (id, organization_id, display_name, status, revision) VALUES ('disabled-user', 'org-a', 'Disabled', 'active', 1)`,
			`INSERT INTO users (id, organization_id, display_name, status, revision) VALUES ('no-credential', 'org-a', 'No Credential', 'active', 1)`,
		} {
			if _, err := database.Exec(statement); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := database.Exec(`UPDATE users SET status = 'disabled', revision = revision + 1 WHERE id = 'disabled-user'`); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 9, 5, 7, 0, 0, 0, time.UTC)
	synthetic := &countingReader{data: bytes.Repeat([]byte{0x6b}, 64)}
	credentials, err := localcredential.NewSQLiteRepository(database, localcredential.WithRandomSource(synthetic), localcredential.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := credentials.SetPassword(t.Context(), "org-a", "user-a", []byte("YU21-password-secret"), 0); err != nil {
		t.Fatal(err)
	}
	synthetic.Reset()
	auditStore, err := audit.NewSQLiteStore(database, audit.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig(bytes.Repeat([]byte{0x21}, 32))
	fixture := &loginFixture{database: database, credentials: credentials, auditStore: auditStore, config: config, synthetic: synthetic, now: now}
	manager, err := NewManager(
		database, credentials, auditStore,
		operation.NewExecutorWithOptions(nil, operation.ExecutorOptions{Transactions: localtx.NewSQLiteFactory(database)}),
		config,
		WithClock(func() time.Time { return fixture.now }), WithIDGenerator(sequenceID("login")), WithRandomSource(bytes.NewReader(bytes.Repeat([]byte{0x5a}, 4096))),
	)
	if err != nil {
		t.Fatal(err)
	}
	fixture.manager = manager
	return fixture
}

func (fixture *loginFixture) context(t *testing.T) context.Context {
	t.Helper()
	ctx := runtimecontext.WithTraceID(t.Context(), "0123456789abcdef0123456789abcdef")
	return runtimecontext.WithMetadata(ctx, runtimecontext.Metadata{
		Transport: "internal-test", Protocol: "internal", RequestID: "request-yu21",
		Attributes: map[string]string{
			"authorization": "Bearer YU21-authorization-secret",
			"session": "YU21-session-metadata-secret",
			"csrf": "YU21-csrf-secret",
		},
	})
}

func (fixture *loginFixture) sessionCount(t *testing.T) int {
	t.Helper()
	var count int
	if err := fixture.database.QueryRow(`SELECT COUNT(*) FROM iotd_local_sessions`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func (fixture *loginFixture) assertSecretsAbsent(t *testing.T, sentinels []string) {
	t.Helper()
	var auditText strings.Builder
	rows, err := fixture.database.Query(`SELECT COALESCE(actor_id,''), COALESCE(target_id,''), COALESCE(reason_code,''), COALESCE(diff_summary,''), COALESCE(metadata,'') FROM iotd_audit_entries`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var values [5]string
		if err := rows.Scan(&values[0], &values[1], &values[2], &values[3], &values[4]); err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		auditText.WriteString(strings.Join(values[:], "|"))
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	var sessionText strings.Builder
	sessionRows, err := fixture.database.Query(`SELECT id, organization_id, user_id, hex(secret_digest), status, credential_revision, created_at, expires_at, COALESCE(revoked_at,'') FROM iotd_local_sessions`)
	if err != nil {
		t.Fatal(err)
	}
	for sessionRows.Next() {
		var values [9]string
		var revision int64
		if err := sessionRows.Scan(&values[0], &values[1], &values[2], &values[3], &values[4], &revision, &values[6], &values[7], &values[8]); err != nil {
			_ = sessionRows.Close()
			t.Fatal(err)
		}
		values[5] = fmt.Sprint(revision)
		sessionText.WriteString(strings.Join(values[:], "|"))
	}
	if err := sessionRows.Close(); err != nil {
		t.Fatal(err)
	}
	persisted := auditText.String() + sessionText.String()
	for _, sentinel := range append(sentinels, "YU21-authorization-secret", "YU21-session-metadata-secret", "YU21-csrf-secret") {
		if sentinel != "" && strings.Contains(persisted, sentinel) {
			t.Fatalf("secret %q leaked into audit/session persistence: %q", sentinel, persisted)
		}
	}
}

func sequenceID(prefix string) func() (string, error) {
	sequence := 0
	return func() (string, error) {
		sequence++
		return fmt.Sprintf("%s-%d", prefix, sequence), nil
	}
}

func signRawJWT(t *testing.T, config Config, claims jwtClaims) string {
	t.Helper()
	headerJSON, err := json.Marshal(jwtHeader{Algorithm: JWTAlgorithm, Type: JWTType, KeyID: config.KeyID})
	if err != nil {
		t.Fatal(err)
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	header := base64.RawURLEncoding.EncodeToString(headerJSON)
	payload := base64.RawURLEncoding.EncodeToString(claimsJSON)
	input := header + "." + payload
	mac := hmac.New(sha256.New, config.SigningKey)
	_, _ = mac.Write([]byte(input))
	return input + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
