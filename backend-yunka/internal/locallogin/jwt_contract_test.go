package locallogin

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

func TestYU21JWTRejectsWrongKeyIDEvenWithCorrectSigningKey(t *testing.T) {
	config := DefaultConfig(bytes.Repeat([]byte{0x21}, 32))
	wrong := config
	wrong.KeyID = "local-auth-v2"
	token, _, err := signAccessToken(wrong, "org-a", "user-a", "session-a", time.Date(2026, 9, 5, 7, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifyAccessTokenSignature(config, token, time.Date(2026, 9, 5, 7, 1, 0, 0, time.UTC)); !errors.Is(err, ErrAccessTokenInvalid) {
		t.Fatalf("wrong kid token error=%v", err)
	}
}

func TestYU21JWTRejectsAlgorithmNoneAndUnknownClaims(t *testing.T) {
	config := DefaultConfig(bytes.Repeat([]byte{0x21}, 32))
	now := time.Date(2026, 9, 5, 7, 0, 0, 0, time.UTC)
	claims := `{"iss":"iot-delivery.local","aud":"iot-delivery.internal","sub":"user-a","tid":"org-a","sid":"session-a","iat":1788591600,"exp":1788591900,"ver":1}`
	for name, testCase := range map[string][2]string{
		"algorithm none": {`{"alg":"none","typ":"JWT","kid":"local-auth-v1"}`, claims},
		"unknown claim": {`{"alg":"HS256","typ":"JWT","kid":"local-auth-v1"}`, `{"iss":"iot-delivery.local","aud":"iot-delivery.internal","sub":"user-a","tid":"org-a","sid":"session-a","iat":1788591600,"exp":1788591900,"ver":1,"role":"system-administrator"}`},
	} {
		t.Run(name, func(t *testing.T) {
			headerPart := base64.RawURLEncoding.EncodeToString([]byte(testCase[0]))
			payloadPart := base64.RawURLEncoding.EncodeToString([]byte(testCase[1]))
			input := headerPart + "." + payloadPart
			mac := hmac.New(sha256.New, config.SigningKey)
			_, _ = mac.Write([]byte(input))
			token := input + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
			if _, err := verifyAccessTokenSignature(config, token, now); !errors.Is(err, ErrAccessTokenInvalid) {
				t.Fatalf("drifted token error=%v", err)
			}
		})
	}
}
