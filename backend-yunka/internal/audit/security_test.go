package audit

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"yunka.io/framework/core/identity"
	"yunka.io/framework/core/runtimecontext"

	_ "modernc.org/sqlite"
)

func TestSecurityRecorderPersistsOnlyWhitelistedCorrelationID(t *testing.T) {
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "security-correlation.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteStore(database)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := NewSecurityRecorder(store)
	if err != nil {
		t.Fatal(err)
	}
	ctx := identity.WithPrincipal(t.Context(), identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodJWT, TenantID: "org-a", UserID: "user-a"})
	ctx = runtimecontext.WithMetadata(ctx, runtimecontext.Metadata{Transport: "http", Attributes: map[string]string{
		"correlation_id": "correlation-a", "authorization": "Bearer secret", "oidc_state": "private-state",
	}})
	if err := recorder.RecordAuthenticationAccepted(ctx, "authentication.bff_assertion.accept"); err != nil {
		t.Fatal(err)
	}
	var correlationID, metadata string
	if err := database.QueryRow(`SELECT correlation_id, metadata FROM iotd_audit_entries`).Scan(&correlationID, &metadata); err != nil {
		t.Fatal(err)
	}
	if correlationID != "correlation-a" {
		t.Fatalf("correlation ID = %q, want whitelist value", correlationID)
	}
	if strings.Contains(metadata, "secret") || strings.Contains(metadata, "private-state") {
		t.Fatalf("metadata leaked runtime attribute: %s", metadata)
	}
}

func TestTrustedSecurityActorRejectsNonCanonicalPrincipalValues(t *testing.T) {
	ctx := identity.WithPrincipal(t.Context(), identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodJWT, TenantID: "org a", UserID: "user-a"})
	if _, _, _, err := trustedSecurityActor(ctx); err == nil {
		t.Fatal("non-canonical tenant was accepted")
	}
}
