package localtransportauth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/bffassertion"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localauth"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/framework/core/runtimecontext"
)

const (
	authorizationHeader = "Authorization"
	localBFFRoutePrefix = "/auth/local/"
)

// HTTPMiddleware accepts verified local-member JWTs for protected runtime
// application routes. The dedicated /auth/local/ BFF namespace owns its own
// opaque-session, Origin and CSRF checks and is therefore passed directly to
// the registered BFF handler instead of being forced through bearer/BFF
// assertion authentication first. Competing bearer/API-key/BFF assertion
// credentials are rejected at the namespace boundary rather than ignored.
func (verifier *Verifier) HTTPMiddleware(fallback func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		var fallbackHandler http.Handler
		if fallback != nil {
			fallbackHandler = fallback(next)
		}
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if strings.HasPrefix(request.URL.Path, localBFFRoutePrefix) {
				if len(request.Header.Values(authorizationHeader)) > 0 || hasCompetingHTTPCredential(request.Header) {
					writeHTTPUnauthorized(writer, transportTraceID())
					return
				}
				next.ServeHTTP(writer, request)
				return
			}
			values := request.Header.Values(authorizationHeader)
			if len(values) == 0 {
				if fallbackHandler != nil {
					fallbackHandler.ServeHTTP(writer, request)
					return
				}
				traceID := transportTraceID()
				verifier.recordFailure(request, traceID, "http", "authentication.missing_credential")
				writeHTTPUnauthorized(writer, traceID)
				return
			}
			traceID := transportTraceID()
			if len(values) != 1 || hasCompetingHTTPCredential(request.Header) {
				verifier.recordFailure(request, traceID, "http", "authentication.mixed_credentials")
				writeHTTPUnauthorized(writer, traceID)
				return
			}
			token, ok := parseBearer(values[0])
			if !ok {
				verifier.recordFailure(request, traceID, "http", "authentication.invalid_credential")
				writeHTTPUnauthorized(writer, traceID)
				return
			}
			principal, err := verifier.VerifyAccessToken(request.Context(), token)
			if err != nil {
				verifier.recordFailure(request, traceID, "http", "authentication.invalid_credential")
				writeHTTPUnauthorized(writer, traceID)
				return
			}
			ctx := identity.WithPrincipal(request.Context(), principal)
			ctx = runtimecontext.WithTraceID(ctx, traceID)
			ctx = runtimecontext.WithMetadata(ctx, runtimecontext.Metadata{
				Transport: "http", Protocol: "http", Method: request.Method,
				Route: request.URL.EscapedPath(), RequestID: traceID,
			})
			if verifier != nil && verifier.recorder != nil {
				if err := verifier.recorder.RecordLocalAccessAuthenticationAccepted(ctx, "authentication.local_access_token", "http"); err != nil {
					writeHTTPServiceUnavailable(writer, traceID)
					return
				}
			}
			writer.Header().Set(bffassertion.TraceHeader, traceID)
			next.ServeHTTP(writer, request.WithContext(ctx))
		})
	}
}

func (verifier *Verifier) recordFailure(request *http.Request, traceID, transport, reason string) {
	if verifier == nil || verifier.recorder == nil {
		return
	}
	ctx := runtimecontext.WithTraceID(request.Context(), traceID)
	ctx = runtimecontext.WithMetadata(ctx, runtimecontext.Metadata{
		Transport: transport, Protocol: transport, Method: request.Method,
		Route: request.URL.EscapedPath(), RequestID: traceID,
	})
	_ = verifier.recorder.RecordAuthenticationFailure(ctx, "authentication.local_access_token", transport, reason)
}

func hasCompetingHTTPCredential(header http.Header) bool {
	return len(header.Values(localauth.APIKeyHeader)) > 0 ||
		len(header.Values(bffassertion.AssertionHeader)) > 0 ||
		len(header.Values(bffassertion.SignatureHeader)) > 0
}

func parseBearer(value string) (string, bool) {
	if value == "" || value != strings.TrimSpace(value) {
		return "", false
	}
	parts := strings.Fields(value)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", false
	}
	return parts[1], true
}

func transportTraceID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "00000000000000000000000000000000"
	}
	return hex.EncodeToString(raw[:])
}

func writeHTTPUnauthorized(writer http.ResponseWriter, traceID string) {
	writeHTTPError(writer, http.StatusUnauthorized, "unauthorized", traceID)
}

func writeHTTPServiceUnavailable(writer http.ResponseWriter, traceID string) {
	writeHTTPError(writer, http.StatusServiceUnavailable, "service_unavailable", traceID)
}

func writeHTTPError(writer http.ResponseWriter, status int, code, traceID string) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set(bffassertion.TraceHeader, traceID)
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(map[string]string{"error": code, "traceId": traceID})
}
