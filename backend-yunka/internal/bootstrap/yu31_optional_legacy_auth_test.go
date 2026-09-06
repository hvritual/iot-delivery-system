package bootstrap_test

import (
	"context"
	"encoding/base64"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/bootstrap"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localauth"
)

func TestYU31LocalMemberRuntimeDoesNotRequireDevelopmentAPIKey(t *testing.T) {
	for _, name := range []string{localauth.APIKeyEnvironment, localauth.ViewerAPIKeyEnvironment, localauth.ContributorAPIKeyEnvironment, localauth.ReleaseManagerAPIKeyEnvironment} {
		t.Setenv(name, "")
	}
	config := bootstrap.Config{
		RuntimeEnvironment: bootstrap.RuntimeEnvironmentDevelopment,
		BootstrapMode:      bootstrap.BootstrapModeDisabled,
		HTTPAddress:        "127.0.0.1:0", GRPCAddress: "127.0.0.1:0",
		DatabasePath: filepath.Join(t.TempDir(), "yu31.db"), ObsidianVault: t.TempDir(),
		BFFOrganizationID:      "yu31-org",
		BFFAssertionKey:        base64.RawURLEncoding.EncodeToString([]byte("01234567890123456789012345678901")),
		LocalAuthJWTSigningKey: base64.RawURLEncoding.EncodeToString([]byte("abcdefghijabcdefghijabcdefghijab")),
	}
	app, err := bootstrap.New(t.Context(), config)
	if err != nil {
		t.Fatalf("start local-member runtime without legacy key: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := app.Close(ctx); err != nil {
			t.Errorf("close runtime: %v", err)
		}
	})
	if app.LocalAuthentication() == nil {
		t.Fatal("local authentication is missing")
	}
	// Startup readiness is not an anonymous authentication bypass. Neither a
	// missing credential nor an unconfigured development key may reach the API.
	for _, key := range []string{"", "unconfigured-development-key"} {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+app.HTTPAddress()+"/api/projects", nil)
		if err != nil {
			t.Fatal(err)
		}
		if key != "" {
			req.Header.Set(localauth.APIKeyHeader, key)
		}
		response, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("unverified request status=%d", response.StatusCode)
		}
	}
	// A configured but invalid compatibility credential must not be silently
	// discarded in favor of another authentication family.
	t.Setenv(localauth.APIKeyEnvironment, "svc.invalid-legacy-namespace")
	config.DatabasePath = filepath.Join(t.TempDir(), "invalid-legacy.db")
	invalid, err := bootstrap.New(t.Context(), config)
	if invalid != nil {
		_ = invalid.Close(context.Background())
	}
	if err == nil {
		t.Fatal("malformed explicit legacy credential was ignored")
	}
}
