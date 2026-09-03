package oidcverify

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestVerifierAcceptsTrustedSignedTokenAndReturnsNormalizedClaims(t *testing.T) {
	issuer := newTestIssuer(t)
	verifier, err := New(t.Context(), Config{
		Issuer:                    issuer.URL(),
		Audience:                  "iot-delivery-api",
		DiscoveryURLForTests:      issuer.DiscoveryURL(),
		AllowInsecureHTTPForTests: true,
		Now:                       func() time.Time { return issuer.now },
	})
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}

	for _, audience := range []any{"iot-delivery-api", []string{"other-client", "iot-delivery-api"}} {
		t.Run("audience", func(t *testing.T) {
			claims, err := verifier.Verify(t.Context(), issuer.token(t, issuer.privateKey, "test-key", "RS256", map[string]any{
				"iss":          issuer.URL(),
				"sub":          "user-123",
				"aud":          audience,
				"exp":          issuer.now.Add(time.Hour).Unix(),
				"email":        "user@example.test",
				"name":         "Test User",
				"internalOnly": "must-not-escape",
			}))
			if err != nil {
				t.Fatalf("verify: %v", err)
			}
			if claims != (VerifiedClaims{Issuer: issuer.URL(), Subject: "user-123", Email: "user@example.test", DisplayName: "Test User"}) {
				t.Fatalf("verified claims = %#v", claims)
			}
		})
	}
}

func TestVerifierFailsClosedForInvalidTokens(t *testing.T) {
	issuer := newTestIssuer(t)
	verifier, err := New(t.Context(), Config{
		Issuer:                    issuer.URL(),
		Audience:                  "iot-delivery-api",
		DiscoveryURLForTests:      issuer.DiscoveryURL(),
		AllowInsecureHTTPForTests: true,
		Now:                       func() time.Time { return issuer.now },
	})
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}

	unknownKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate unknown signing key: %v", err)
	}
	validClaims := map[string]any{
		"iss": issuer.URL(),
		"sub": "user-123",
		"aud": "iot-delivery-api",
		"exp": issuer.now.Add(time.Hour).Unix(),
	}
	cases := map[string]string{
		"issuer requires exact match": issuer.token(t, issuer.privateKey, "test-key", "RS256", withClaim(validClaims, "iss", issuer.URL()+"/different")),
		"audience mismatch":           issuer.token(t, issuer.privateKey, "test-key", "RS256", withClaim(validClaims, "aud", "other-client")),
		"audience missing":            issuer.token(t, issuer.privateKey, "test-key", "RS256", withoutClaim(validClaims, "aud")),
		"expiration missing":          issuer.token(t, issuer.privateKey, "test-key", "RS256", withoutClaim(validClaims, "exp")),
		"expiration elapsed":          issuer.token(t, issuer.privateKey, "test-key", "RS256", withClaim(validClaims, "exp", issuer.now.Add(-time.Second).Unix())),
		"unknown signing key":         issuer.token(t, unknownKey, "unknown-key", "RS256", validClaims),
		"algorithm is not allowed":    issuer.token(t, issuer.privateKey, "test-key", "RS512", validClaims),
		"signature is tampered":       tamperSignature(issuer.token(t, issuer.privateKey, "test-key", "RS256", validClaims)),
	}
	for name, rawIDToken := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := verifier.Verify(t.Context(), rawIDToken); err == nil {
				t.Fatal("verification succeeded, want failure")
			}
		})
	}
}

func TestNewRejectsInsecureAndTestOnlyConfigurationByDefault(t *testing.T) {
	issuer := newTestIssuer(t)
	for name, config := range map[string]Config{
		"http issuer": {
			Issuer:   issuer.URL(),
			Audience: "iot-delivery-api",
		},
		"test discovery endpoint": {
			Issuer:               "https://issuer.example.test",
			Audience:             "iot-delivery-api",
			DiscoveryURLForTests: issuer.DiscoveryURL(),
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := New(t.Context(), config); err == nil {
				t.Fatal("new verifier succeeded, want failed-closed configuration error")
			}
		})
	}
}

func TestNewRejectsRedirectedDiscoveryDocument(t *testing.T) {
	issuer := newTestIssuer(t)
	if _, err := New(t.Context(), Config{
		Issuer:                    issuer.URL(),
		Audience:                  "iot-delivery-api",
		DiscoveryURLForTests:      issuer.server.URL + "/redirect-discovery",
		AllowInsecureHTTPForTests: true,
	}); err == nil {
		t.Fatal("new verifier succeeded through a discovery redirect, want failure")
	}
}

func TestNewUsesIssuerPathForStandardDiscovery(t *testing.T) {
	issuer := newTestIssuer(t)
	issuer.metadataIssuer = issuer.URL() + "/realms/iot"
	if _, err := New(t.Context(), Config{
		Issuer:                    issuer.metadataIssuer,
		Audience:                  "iot-delivery-api",
		AllowInsecureHTTPForTests: true,
	}); err != nil {
		t.Fatalf("new verifier using /realms/iot/.well-known/openid-configuration: %v", err)
	}
}

func TestNewConfiguresFiniteTimeoutForDiscoveryAndJWKS(t *testing.T) {
	issuer := newTestIssuer(t)
	verifier, err := New(t.Context(), Config{
		Issuer:                    issuer.URL(),
		Audience:                  "iot-delivery-api",
		DiscoveryURLForTests:      issuer.DiscoveryURL(),
		AllowInsecureHTTPForTests: true,
	})
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}
	if verifier.httpClient.Timeout <= 0 || verifier.httpClient.Timeout > 5*time.Second {
		t.Fatalf("OIDC HTTP timeout = %s, want a finite timeout no greater than 5s", verifier.httpClient.Timeout)
	}
}

func TestNewRejectsInsecureTestEndpointsOutsideLoopback(t *testing.T) {
	issuer := newTestIssuer(t)
	for name, config := range map[string]Config{
		"issuer": {
			Issuer:                    "http://issuer.example.test",
			Audience:                  "iot-delivery-api",
			AllowInsecureHTTPForTests: true,
		},
		"discovery": {
			Issuer:                    issuer.URL(),
			Audience:                  "iot-delivery-api",
			DiscoveryURLForTests:      "http://issuer.example.test/.well-known/openid-configuration",
			AllowInsecureHTTPForTests: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := New(t.Context(), config); err == nil {
				t.Fatal("new verifier succeeded for a non-loopback HTTP endpoint")
			}
		})
	}

	issuer.jwksURI = "http://issuer.example.test/keys"
	if _, err := New(t.Context(), Config{
		Issuer:                    issuer.URL(),
		Audience:                  "iot-delivery-api",
		DiscoveryURLForTests:      issuer.DiscoveryURL(),
		AllowInsecureHTTPForTests: true,
	}); err == nil {
		t.Fatal("new verifier succeeded for a non-loopback HTTP JWKS endpoint")
	}
}

func TestVerifierAcceptsJWKSURIWithQuery(t *testing.T) {
	issuer := newTestIssuer(t)
	issuer.jwksURI = issuer.server.URL + "/keys?set=current"
	verifier, err := New(t.Context(), Config{
		Issuer:                    issuer.URL(),
		Audience:                  "iot-delivery-api",
		DiscoveryURLForTests:      issuer.DiscoveryURL(),
		AllowInsecureHTTPForTests: true,
		Now:                       func() time.Time { return issuer.now },
	})
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}
	if _, err := verifier.Verify(t.Context(), issuer.token(t, issuer.privateKey, "test-key", "RS256", map[string]any{
		"iss": issuer.URL(),
		"sub": "user-123",
		"aud": "iot-delivery-api",
		"exp": issuer.now.Add(time.Hour).Unix(),
	})); err != nil {
		t.Fatalf("verify using JWKS URI with query: %v", err)
	}
}

func TestNewRejectsDiscoveryIssuerMismatch(t *testing.T) {
	issuer := newTestIssuer(t)
	issuer.metadataIssuer = issuer.URL() + "/different"
	if _, err := New(t.Context(), Config{
		Issuer:                    issuer.URL(),
		Audience:                  "iot-delivery-api",
		DiscoveryURLForTests:      issuer.DiscoveryURL(),
		AllowInsecureHTTPForTests: true,
	}); err == nil {
		t.Fatal("new verifier accepted a non-exact discovery issuer")
	}
}

func TestVerifierRejectsEmptySubject(t *testing.T) {
	issuer := newTestIssuer(t)
	verifier, err := New(t.Context(), Config{
		Issuer:                    issuer.URL(),
		Audience:                  "iot-delivery-api",
		DiscoveryURLForTests:      issuer.DiscoveryURL(),
		AllowInsecureHTTPForTests: true,
		Now:                       func() time.Time { return issuer.now },
	})
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}
	if _, err := verifier.Verify(t.Context(), issuer.token(t, issuer.privateKey, "test-key", "RS256", map[string]any{
		"iss": issuer.URL(),
		"aud": "iot-delivery-api",
		"exp": issuer.now.Add(time.Hour).Unix(),
	})); err == nil {
		t.Fatal("verify succeeded without a subject")
	}
}

type testIssuer struct {
	server         *httptest.Server
	privateKey     *rsa.PrivateKey
	now            time.Time
	metadataIssuer string
	jwksURI        string
}

func newTestIssuer(t *testing.T) *testIssuer {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate signing key: %v", err)
	}
	issuer := &testIssuer{privateKey: privateKey, now: time.Date(2026, time.September, 3, 0, 0, 0, 0, time.UTC)}
	issuer.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/redirect-discovery":
			http.Redirect(writer, request, "/.well-known/openid-configuration", http.StatusFound)
		case "/.well-known/openid-configuration", "/realms/iot/.well-known/openid-configuration":
			writeJSON(t, writer, map[string]any{
				"issuer":   issuer.MetadataIssuer(),
				"jwks_uri": issuer.JWKSURI(),
			})
		case "/keys":
			writeJSON(t, writer, map[string]any{"keys": []any{rsaJWK(t, issuer.privateKey, "test-key")}})
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(issuer.server.Close)
	return issuer
}

func (issuer *testIssuer) URL() string { return issuer.server.URL }

func (issuer *testIssuer) DiscoveryURL() string {
	return issuer.server.URL + "/.well-known/openid-configuration"
}

func (issuer *testIssuer) MetadataIssuer() string {
	if issuer.metadataIssuer != "" {
		return issuer.metadataIssuer
	}
	return issuer.URL()
}

func (issuer *testIssuer) JWKSURI() string {
	if issuer.jwksURI != "" {
		return issuer.jwksURI
	}
	return issuer.server.URL + "/keys"
}

func (issuer *testIssuer) token(t *testing.T, privateKey *rsa.PrivateKey, keyID, algorithm string, claims map[string]any) string {
	t.Helper()
	header, err := json.Marshal(map[string]string{"alg": algorithm, "kid": keyID, "typ": "JWT"})
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	signingInput := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	var hash crypto.Hash
	switch algorithm {
	case "RS256":
		hash = crypto.SHA256
	case "RS512":
		hash = crypto.SHA512
	default:
		t.Fatalf("unsupported test algorithm %q", algorithm)
	}
	var digest []byte
	switch hash {
	case crypto.SHA256:
		sum := sha256.Sum256([]byte(signingInput))
		digest = sum[:]
	case crypto.SHA512:
		sum := sha512.Sum512([]byte(signingInput))
		digest = sum[:]
	}
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, hash, digest)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func rsaJWK(t *testing.T, privateKey *rsa.PrivateKey, keyID string) map[string]string {
	t.Helper()
	return map[string]string{
		"kty": "RSA",
		"kid": keyID,
		"alg": "RS256",
		"use": "sig",
		"n":   base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(bigEndianBytes(privateKey.PublicKey.E)),
	}
}

func bigEndianBytes(value int) []byte {
	result := make([]byte, 0, 8)
	for value > 0 {
		result = append([]byte{byte(value)}, result...)
		value >>= 8
	}
	return result
}

func withClaim(claims map[string]any, key string, value any) map[string]any {
	result := make(map[string]any, len(claims))
	for claim, claimValue := range claims {
		result[claim] = claimValue
	}
	result[key] = value
	return result
}

func withoutClaim(claims map[string]any, key string) map[string]any {
	result := withClaim(claims, "", nil)
	delete(result, "")
	delete(result, key)
	return result
}

func tamperSignature(rawIDToken string) string {
	parts := strings.Split(rawIDToken, ".")
	signature := parts[2]
	if signature[0] == 'A' {
		signature = "B" + signature[1:]
	} else {
		signature = "A" + signature[1:]
	}
	parts[2] = signature
	return strings.Join(parts, ".")
}

func writeJSON(t *testing.T, writer http.ResponseWriter, value any) {
	t.Helper()
	writer.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		t.Fatalf("encode test response: %v", err)
	}
}
