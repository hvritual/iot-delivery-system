package localtransportauth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localauth"
)

func TestYU26LocalBFFNamespaceBypassesBearerOnlyAndRejectsCredentialMixing(t *testing.T) {
	verifier := &Verifier{}
	fallbackCalls := 0
	fallback := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			fallbackCalls++
			writer.WriteHeader(http.StatusUnauthorized)
		})
	}
	nextCalls := 0
	wrapped := verifier.HTTPMiddleware(fallback)(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		nextCalls++
		writer.WriteHeader(http.StatusNoContent)
	}))

	local := httptest.NewRequest(http.MethodPost, "https://example.test/auth/local/login", nil)
	localResponse := httptest.NewRecorder()
	wrapped.ServeHTTP(localResponse, local)
	if localResponse.Code != http.StatusNoContent || nextCalls != 1 || fallbackCalls != 0 {
		t.Fatalf("local namespace status=%d next=%d fallback=%d", localResponse.Code, nextCalls, fallbackCalls)
	}

	protected := httptest.NewRequest(http.MethodGet, "https://example.test/api/items", nil)
	protectedResponse := httptest.NewRecorder()
	wrapped.ServeHTTP(protectedResponse, protected)
	if protectedResponse.Code != http.StatusUnauthorized || nextCalls != 1 || fallbackCalls != 1 {
		t.Fatalf("protected route status=%d next=%d fallback=%d", protectedResponse.Code, nextCalls, fallbackCalls)
	}

	mixedBearer := httptest.NewRequest(http.MethodPost, "https://example.test/auth/local/login", nil)
	mixedBearer.Header.Set("Authorization", "Bearer must-not-be-ignored")
	mixedBearerResponse := httptest.NewRecorder()
	wrapped.ServeHTTP(mixedBearerResponse, mixedBearer)
	if mixedBearerResponse.Code != http.StatusUnauthorized || nextCalls != 1 || fallbackCalls != 1 {
		t.Fatalf("mixed bearer status=%d next=%d fallback=%d", mixedBearerResponse.Code, nextCalls, fallbackCalls)
	}

	mixedAPIKey := httptest.NewRequest(http.MethodPost, "https://example.test/auth/local/login", nil)
	mixedAPIKey.Header.Set(localauth.APIKeyHeader, "must-not-be-ignored")
	mixedAPIKeyResponse := httptest.NewRecorder()
	wrapped.ServeHTTP(mixedAPIKeyResponse, mixedAPIKey)
	if mixedAPIKeyResponse.Code != http.StatusUnauthorized || nextCalls != 1 || fallbackCalls != 1 {
		t.Fatalf("mixed API key status=%d next=%d fallback=%d", mixedAPIKeyResponse.Code, nextCalls, fallbackCalls)
	}
}
