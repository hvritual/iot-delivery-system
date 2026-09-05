package main

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestYU21ConfigurationLoadsDedicatedLocalAuthJWTKeyWithoutReusingBFFKey(t *testing.T) {
	t.Setenv("IOT_DELIVERY_RUNTIME_ENVIRONMENT", "development")
	t.Setenv("IOT_DELIVERY_BOOTSTRAP_MODE", "disabled")
	localKey := base64.RawURLEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	bffKey := base64.RawURLEncoding.EncodeToString([]byte("abcdef0123456789abcdef0123456789"))
	t.Setenv("IOT_DELIVERY_LOCAL_AUTH_JWT_KEY", localKey)
	t.Setenv("IOT_DELIVERY_BFF_ORGANIZATION_ID", "org-config")
	t.Setenv("IOT_DELIVERY_BFF_ASSERTION_KEY", bffKey)
	configuration, err := configurationFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if configuration.LocalAuthJWTSigningKey != localKey || configuration.BFFAssertionKey != bffKey || configuration.LocalAuthJWTSigningKey == configuration.BFFAssertionKey {
		t.Fatalf("local/BFF key configuration drift: local=%q bff=%q", configuration.LocalAuthJWTSigningKey, configuration.BFFAssertionKey)
	}
}

func TestYU21ConfigurationDoesNotLeakLocalAuthJWTKeyOnOtherStartupErrors(t *testing.T) {
	const sentinel = "YU21_SIGNING_SECRET_MUST_NOT_LEAK"
	t.Setenv("IOT_DELIVERY_RUNTIME_ENVIRONMENT", "invalid-environment")
	t.Setenv("IOT_DELIVERY_BOOTSTRAP_MODE", "disabled")
	t.Setenv("IOT_DELIVERY_LOCAL_AUTH_JWT_KEY", sentinel)
	_, err := configurationFromEnv()
	if err == nil {
		t.Fatal("invalid runtime environment unexpectedly passed")
	}
	if strings.Contains(err.Error(), sentinel) {
		t.Fatalf("startup error leaked local auth signing key: %q", err)
	}
}
