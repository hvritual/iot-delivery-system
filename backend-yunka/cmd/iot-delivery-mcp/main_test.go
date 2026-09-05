package main

import (
	"encoding/base64"
	"strings"
	"testing"
)

func clearMCPAuthEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		mcpAPIKeyEnvironment,
		mcpAccessTokenEnvironment,
		mcpSessionTokenEnvironment,
		localAuthKeyEnvironment,
		"IOT_DELIVERY_LOCAL_API_KEY",
		"IOT_DELIVERY_LOCAL_VIEWER_API_KEY",
		"IOT_DELIVERY_LOCAL_CONTRIBUTOR_API_KEY",
		"IOT_DELIVERY_LOCAL_RELEASE_MANAGER_API_KEY",
		"IOT_DELIVERY_BFF_ORGANIZATION_ID",
		"IOT_DELIVERY_BFF_ASSERTION_KEY",
	} {
		t.Setenv(name, "")
	}
}

func TestConfigurationRejectsProductionLegacyLocalAPIKey(t *testing.T) {
	clearMCPAuthEnvironment(t)
	const sentinelCredential = "S0_02_08_MCP_SENTINEL_DO_NOT_LOG"
	t.Setenv("IOT_DELIVERY_RUNTIME_ENVIRONMENT", "production")
	t.Setenv("IOT_DELIVERY_BOOTSTRAP_MODE", "disabled")
	t.Setenv("IOT_DELIVERY_LOCAL_API_KEY", sentinelCredential)

	_, err := configurationFromEnv()
	if err == nil || !strings.Contains(err.Error(), "legacy local API-key") {
		t.Fatalf("production MCP legacy API-key configuration error = %v, want generic legacy API-key rejection", err)
	}
	if strings.Contains(err.Error(), sentinelCredential) {
		t.Fatalf("production MCP configuration error leaked sentinel credential: %q", err)
	}
}

func TestConfigurationRejectsProductionInsecureServiceCredentialFlag(t *testing.T) {
	clearMCPAuthEnvironment(t)
	t.Setenv("IOT_DELIVERY_RUNTIME_ENVIRONMENT", "production")
	t.Setenv("IOT_DELIVERY_BOOTSTRAP_MODE", "disabled")
	t.Setenv("IOT_DELIVERY_ALLOW_INSECURE_SERVICE_CREDENTIALS_FOR_DEVELOPMENT", "true")
	t.Setenv(mcpAccessTokenEnvironment, "access-token-sentinel")
	t.Setenv(localAuthKeyEnvironment, base64.RawURLEncoding.EncodeToString(make([]byte, 32)))

	_, err := configurationFromEnv()
	if err == nil || !strings.Contains(err.Error(), "insecure service credentials") {
		t.Fatalf("production MCP insecure service credential configuration error = %v, want generic insecure credential rejection", err)
	}
}

func TestYU23DevelopmentMCPAcceptsLocalAccessOrSessionCredentialMode(t *testing.T) {
	for _, scenario := range []struct {
		name string
		env  string
		kind mcpCredentialKind
	}{
		{name: "access JWT", env: mcpAccessTokenEnvironment, kind: mcpCredentialAccess},
		{name: "opaque session", env: mcpSessionTokenEnvironment, kind: mcpCredentialSession},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			clearMCPAuthEnvironment(t)
			t.Setenv("IOT_DELIVERY_RUNTIME_ENVIRONMENT", "development")
			t.Setenv("IOT_DELIVERY_BOOTSTRAP_MODE", "disabled")
			t.Setenv("IOT_DELIVERY_LOCAL_API_KEY", "YU23-runtime-compatibility-api-key")
			t.Setenv(scenario.env, "YU23-local-member-credential-sentinel")
			key := base64.RawURLEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
			t.Setenv(localAuthKeyEnvironment, key)
			configuration, err := configurationFromEnv()
			if err != nil {
				t.Fatalf("development local-member MCP configuration error=%v", err)
			}
			credential, err := mcpCredentialFromEnv()
			if err != nil || credential.kind != scenario.kind || configuration.RuntimeEnvironment != "development" || configuration.LocalAuthJWTSigningKey != key || !configuration.LegacyLocalAPIKeyEnabled {
				t.Fatalf("credential=%#v configuration=%#v error=%v", credential, configuration, err)
			}
		})
	}
}

func TestYU23ProductionLocalMemberMCPRemainsDevelopmentOnly(t *testing.T) {
	clearMCPAuthEnvironment(t)
	t.Setenv("IOT_DELIVERY_RUNTIME_ENVIRONMENT", "production")
	t.Setenv("IOT_DELIVERY_BOOTSTRAP_MODE", "disabled")
	t.Setenv(mcpAccessTokenEnvironment, "YU23-production-access-token")
	t.Setenv(localAuthKeyEnvironment, base64.RawURLEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	_, err := configurationFromEnv()
	if err == nil || !strings.Contains(err.Error(), "development-only") {
		t.Fatalf("production local-member MCP error=%v, want development-only", err)
	}
}

func TestYU23MCPRejectsMissingOrMixedCredentialFamiliesWithoutLeakingValues(t *testing.T) {
	clearMCPAuthEnvironment(t)
	t.Setenv("IOT_DELIVERY_RUNTIME_ENVIRONMENT", "development")
	t.Setenv("IOT_DELIVERY_BOOTSTRAP_MODE", "disabled")
	if _, err := configurationFromEnv(); err == nil || !strings.Contains(err.Error(), "exactly one MCP credential family") {
		t.Fatalf("missing MCP credential error=%v", err)
	}
	clearMCPAuthEnvironment(t)
	t.Setenv("IOT_DELIVERY_RUNTIME_ENVIRONMENT", "development")
	t.Setenv("IOT_DELIVERY_BOOTSTRAP_MODE", "disabled")
	const accessSentinel = "YU23-access-secret-must-not-leak"
	const sessionSentinel = "YU23-session-secret-must-not-leak"
	t.Setenv(mcpAccessTokenEnvironment, accessSentinel)
	t.Setenv(mcpSessionTokenEnvironment, sessionSentinel)
	_, err := configurationFromEnv()
	if err == nil || !strings.Contains(err.Error(), "exactly one MCP credential family") {
		t.Fatalf("mixed MCP credential error=%v", err)
	}
	if strings.Contains(err.Error(), accessSentinel) || strings.Contains(err.Error(), sessionSentinel) {
		t.Fatalf("mixed MCP credential error leaked secret: %q", err)
	}
}

func TestYU23MCPExplicitLocalMemberCredentialIgnoresGlobalDevelopmentAPIKey(t *testing.T) {
	clearMCPAuthEnvironment(t)
	t.Setenv("IOT_DELIVERY_RUNTIME_ENVIRONMENT", "development")
	t.Setenv("IOT_DELIVERY_BOOTSTRAP_MODE", "disabled")
	t.Setenv(mcpAccessTokenEnvironment, "YU23-explicit-access-token")
	t.Setenv("IOT_DELIVERY_LOCAL_API_KEY", "YU23-global-development-api-key")
	key := base64.RawURLEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	t.Setenv(localAuthKeyEnvironment, key)

	credential, err := mcpCredentialFromEnv()
	if err != nil || credential.kind != mcpCredentialAccess || credential.value != "YU23-explicit-access-token" {
		t.Fatalf("explicit local-member credential=%#v error=%v", credential, err)
	}
	configuration, err := configurationFromEnv()
	if err != nil || configuration.LocalAuthJWTSigningKey != key || !configuration.LegacyLocalAPIKeyEnabled {
		t.Fatalf("development local-member configuration=%#v error=%v", configuration, err)
	}
}

func TestYU23MCPFallsBackToLegacyGlobalAPIKeyOnlyWhenNoExplicitCredentialExists(t *testing.T) {
	clearMCPAuthEnvironment(t)
	t.Setenv("IOT_DELIVERY_RUNTIME_ENVIRONMENT", "development")
	t.Setenv("IOT_DELIVERY_BOOTSTRAP_MODE", "disabled")
	t.Setenv("IOT_DELIVERY_LOCAL_API_KEY", "YU23-legacy-mcp-api-key")
	credential, err := mcpCredentialFromEnv()
	if err != nil || credential.kind != mcpCredentialAPIKey || credential.value != "YU23-legacy-mcp-api-key" {
		t.Fatalf("legacy MCP credential=%#v error=%v", credential, err)
	}

	t.Setenv(mcpAPIKeyEnvironment, "YU23-explicit-mcp-api-key")
	credential, err = mcpCredentialFromEnv()
	if err != nil || credential.kind != mcpCredentialAPIKey || credential.value != "YU23-explicit-mcp-api-key" {
		t.Fatalf("explicit MCP API-key credential=%#v error=%v", credential, err)
	}
}
