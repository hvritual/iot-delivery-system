package main

import (
	"strings"
	"testing"
)

func TestConfigurationRejectsProductionLegacyLocalAPIKey(t *testing.T) {
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
	t.Setenv("IOT_DELIVERY_RUNTIME_ENVIRONMENT", "production")
	t.Setenv("IOT_DELIVERY_BOOTSTRAP_MODE", "disabled")
	t.Setenv("IOT_DELIVERY_ALLOW_INSECURE_SERVICE_CREDENTIALS_FOR_DEVELOPMENT", "true")

	_, err := configurationFromEnv()
	if err == nil || !strings.Contains(err.Error(), "insecure service credentials") {
		t.Fatalf("production MCP insecure service credential configuration error = %v, want generic insecure credential rejection", err)
	}
}

func TestConfigurationRejectsProductionBeforeLocalMCPAuthentication(t *testing.T) {
	t.Setenv("IOT_DELIVERY_RUNTIME_ENVIRONMENT", "production")
	t.Setenv("IOT_DELIVERY_BOOTSTRAP_MODE", "disabled")
	t.Setenv("IOT_DELIVERY_LOCAL_API_KEY", "")

	_, err := configurationFromEnv()
	if err == nil || !strings.Contains(err.Error(), "development-only") {
		t.Fatalf("production MCP configuration error = %v, want explicit development-only rejection", err)
	}
}
