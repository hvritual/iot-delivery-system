package bootstrap

import (
	"encoding/base64"
	"testing"
)

func TestYU21LocalAuthJWTKeyIsOptionalUntilRouteExposureButValidatedWhenPresent(t *testing.T) {
	if err := validateLocalAuthConfiguration(Config{}); err != nil {
		t.Fatalf("absent YU-21 key should leave optional in-process capability disabled: %v", err)
	}
	if err := validateLocalAuthConfiguration(Config{LocalAuthJWTSigningKey: "not-base64url***"}); err == nil {
		t.Fatal("malformed local auth JWT key passed validation")
	}
	short := base64.RawURLEncoding.EncodeToString([]byte("too-short"))
	if err := validateLocalAuthConfiguration(Config{LocalAuthJWTSigningKey: short}); err == nil {
		t.Fatal("short local auth JWT key passed validation")
	}
	valid := base64.RawURLEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	if err := validateLocalAuthConfiguration(Config{LocalAuthJWTSigningKey: valid}); err != nil {
		t.Fatalf("valid local auth JWT key rejected: %v", err)
	}
}
