package bffassertion

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestVerifierRejectsForgeryTamperingAndReplayBeforeInvocation(t *testing.T) {
	t.Parallel()
	key := []byte("01234567890123456789012345678901")
	now := time.Date(2026, 9, 3, 8, 0, 0, 0, time.UTC)
	verifier, err := NewVerifier(Config{Key: key, Now: func() time.Time { return now }, ReplayCapacity: 4})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	body := []byte(`{"title":"bound body"}`)
	request := signedRequest(t, key, now, http.MethodPost, "/api/items?projectId=P-1", body, "00000000000000000000000000000001", "nonce-001")
	claims, err := verifier.Verify(request, body)
	if err != nil {
		t.Fatalf("Verify valid request: %v", err)
	}
	if claims.Subject != "external-user-1" || claims.TraceID != "00000000000000000000000000000001" {
		t.Fatalf("claims = %#v", claims)
	}
	if _, err := verifier.Verify(request, body); err == nil {
		t.Fatal("replayed assertion unexpectedly accepted")
	}

	methodTampered := signedRequest(t, key, now, http.MethodPost, "/api/items?projectId=P-1", body, "00000000000000000000000000000002", "nonce-002")
	methodTampered.Method = http.MethodGet
	pathTampered := signedRequest(t, key, now, http.MethodPost, "/api/items?projectId=P-1", body, "00000000000000000000000000000003", "nonce-003")
	pathTampered.URL.RawQuery = "projectId=P-2"
	for _, tampered := range []struct {
		name    string
		request *http.Request
		body    []byte
	}{
		{"method", methodTampered, body},
		{"path", pathTampered, body},
		{"body", signedRequest(t, key, now, http.MethodPost, "/api/items?projectId=P-1", []byte(`{"title":"original"}`), "00000000000000000000000000000004", "nonce-004"), body},
	} {
		t.Run(tampered.name, func(t *testing.T) {
			if _, err := verifier.Verify(tampered.request, tampered.body); err == nil {
				t.Fatal("tampered assertion unexpectedly accepted")
			}
		})
	}
}

func TestVerifierRejectsInvalidHeadersSignaturesAndExpiry(t *testing.T) {
	t.Parallel()
	key := []byte("01234567890123456789012345678901")
	now := time.Date(2026, 9, 3, 8, 0, 0, 0, time.UTC)
	verifier, err := NewVerifier(Config{Key: key, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	body := []byte(`{}`)
	request := signedRequest(t, key, now.Add(-2*time.Minute), http.MethodPost, "/api/items", body, "00000000000000000000000000000011", "nonce-expired-01")
	if _, err := verifier.Verify(request, body); err == nil {
		t.Fatal("expired assertion unexpectedly accepted")
	}

	request = signedRequest(t, key, now, http.MethodPost, "/api/items", body, "00000000000000000000000000000012", "nonce-bad-signature")
	request.Header.Set(SignatureHeader, "invalid")
	if _, err := verifier.Verify(request, body); err == nil {
		t.Fatal("invalid signature unexpectedly accepted")
	}
	request = signedRequest(t, key, now, http.MethodPost, "/api/items", body, "00000000000000000000000000000013", "nonce-multiple-01")
	request.Header.Add(AssertionHeader, "another-value")
	if _, err := verifier.Verify(request, body); err == nil {
		t.Fatal("multiple assertion header values unexpectedly accepted")
	}
}

func TestVerifierRejectsControlCharactersInIdentityFields(t *testing.T) {
	t.Parallel()
	key := []byte("01234567890123456789012345678901")
	now := time.Date(2026, 9, 3, 8, 0, 0, 0, time.UTC)
	verifier, err := NewVerifier(Config{Key: key, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	request := signedRequest(t, key, now, http.MethodPost, "/api/items", []byte(`{}`), "00000000000000000000000000000015", "nonce-control-001")
	payload, _ := base64.RawURLEncoding.DecodeString(request.Header.Get(AssertionHeader))
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatal(err)
	}
	claims.DisplayName = "unsafe\nname"
	replaced, _ := json.Marshal(claims)
	encoded := base64.RawURLEncoding.EncodeToString(replaced)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(encoded))
	request.Header.Set(AssertionHeader, encoded)
	request.Header.Set(SignatureHeader, base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))
	if _, err := verifier.Verify(request, []byte(`{}`)); err == nil {
		t.Fatal("control character identity unexpectedly accepted")
	}
}

func TestVerifierAcceptsTypeScriptV1Vector(t *testing.T) {
	t.Parallel()
	key := []byte("01234567890123456789012345678901")
	now := time.Unix(1_788_422_400, 0).UTC()
	verifier, err := NewVerifier(Config{Key: key, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, "http://runtime.example/api/items?projectId=P-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set(AssertionHeader, "eyJ2IjoxLCJpc3N1ZXIiOiJodHRwczovL2lzc3Vlci5leGFtcGxlL3RlbmFudCIsInN1YmplY3QiOiJ1c2VyLTEiLCJlbWFpbCI6InVzZXJAZXhhbXBsZS50ZXN0IiwiZGlzcGxheU5hbWUiOiJVc2VyIE9uZSIsIm5vbmNlIjoibm9uY2UtdmVjdG9yLTAwMDEiLCJ0cmFjZUlkIjoiMTExMTExMTExMTExMTExMTExMTExMTExMTExMTExMTEiLCJtZXRob2QiOiJQT1NUIiwicGF0aCI6Ii9hcGkvaXRlbXM_cHJvamVjdElkPVAtMSIsImJvZHlTaGEyNTYiOiJmMjcxODNiY2Y2Nzg2YjgxOTMxMTJiNWRkMDQ2ZDUxZGI5ZmQ1ZDE1OTA3NjkxN2RjODUwYTc2MmE1Y2UyZTkxIiwiaWF0IjoxNzg4NDIyNDAwLCJleHAiOjE3ODg0MjI0NjB9")
	request.Header.Set(SignatureHeader, "L8v1Jb1YMnmCUoCy-0UN6Hy7jx1SPFoRHGA457yuO7k")
	request.Header.Set(TraceHeader, "11111111111111111111111111111111")
	claims, err := verifier.Verify(request, []byte(`{"title":"vector"}`))
	if err != nil {
		t.Fatalf("TypeScript v1 vector rejected: %v", err)
	}
	if claims.Subject != "user-1" || claims.Nonce != "nonce-vector-0001" {
		t.Fatalf("claims = %#v", claims)
	}
}

func signedRequest(t *testing.T, key []byte, now time.Time, method, path string, body []byte, traceID, nonce string) *http.Request {
	t.Helper()
	digest := sha256.Sum256(body)
	claims := Claims{
		Version: 1, Issuer: "https://issuer.example/tenant", Subject: "external-user-1", Email: "person@example.test", DisplayName: "Person One",
		Nonce: nonce, TraceID: traceID, Method: method, Path: path, BodySHA256: hex.EncodeToString(digest[:]), Iat: now.Unix(), Exp: now.Add(time.Minute).Unix(),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(encoded))
	request, err := http.NewRequest(method, "http://runtime.example"+path, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	request.Header.Set(AssertionHeader, encoded)
	request.Header.Set(SignatureHeader, base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))
	request.Header.Set(TraceHeader, traceID)
	return request
}
