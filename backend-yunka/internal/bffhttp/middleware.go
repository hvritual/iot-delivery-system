// Package bffhttp adapts the local BFF trust contract to Yunka's execution
// context. The browser is never an identity authority at this boundary.
package bffhttp

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/audit"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/bffassertion"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitybinding"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localauth"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/oidcverify"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/framework/core/runtimecontext"
)

const (
	maxRequestBodyBytes   = 1 << 20
	maxErrorResponseBytes = 64 << 10
)

type Config struct {
	Authenticator       *localauth.Authenticator
	AuditRecorder       *audit.SecurityRecorder
	Verifier            *bffassertion.Verifier
	Resolver            *identitybinding.Resolver
	OrganizationID      string
	AllowLegacyFallback bool
}

type Middleware struct {
	authenticator       *localauth.Authenticator
	auditRecorder       *audit.SecurityRecorder
	verifier            *bffassertion.Verifier
	resolver            *identitybinding.Resolver
	organizationID      string
	allowLegacyFallback bool
}

func NewMiddleware(config Config) (*Middleware, error) {
	if config.Verifier == nil || config.Resolver == nil || strings.TrimSpace(config.OrganizationID) == "" {
		return nil, errors.New("BFF HTTP middleware is not configured")
	}
	if config.AllowLegacyFallback && config.Authenticator == nil {
		return nil, errors.New("BFF legacy fallback authenticator is not configured")
	}
	return &Middleware{
		authenticator:       config.Authenticator,
		auditRecorder:       config.AuditRecorder,
		verifier:            config.Verifier,
		resolver:            config.Resolver,
		organizationID:      strings.TrimSpace(config.OrganizationID),
		allowLegacyFallback: config.AllowLegacyFallback,
	}, nil
}

// HTTPMiddleware accepts a signed BFF assertion. Development may explicitly
// retain the historical local API-key fallback and BFF channel requirement.
func (middleware *Middleware) HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		traceID := newTraceID()
		if middleware == nil {
			writeError(writer, http.StatusServiceUnavailable, "service_unavailable", traceID)
			return
		}
		if hasAssertionHeaders(request.Header) {
			var legacyRoles []string
			if middleware.allowLegacyFallback {
				if middleware.authenticator == nil {
					writeError(writer, http.StatusServiceUnavailable, "service_unavailable", traceID)
					return
				}
				channelPrincipal, err := middleware.authenticator.AuthenticateAPIKey(request.Header.Get(localauth.APIKeyHeader))
				if err != nil {
					auditAuthenticationFailure(request.Context(), middleware.auditRecorder, traceID, "authentication.bff_channel", "authentication.invalid_credential")
					writeError(writer, http.StatusUnauthorized, "unauthorized", traceID)
					return
				}
				legacyRoles = append([]string(nil), channelPrincipal.Roles...)
			}
			if middleware.verifier == nil || middleware.resolver == nil || middleware.organizationID == "" {
				writeError(writer, http.StatusServiceUnavailable, "service_unavailable", traceID)
				return
			}
			body, readErr := readAndRestoreBody(request)
			if readErr != nil {
				writeError(writer, http.StatusBadRequest, "invalid_request", traceID)
				return
			}
			claims, verifyErr := middleware.verifier.Verify(request, body)
			if verifyErr != nil {
				auditAuthenticationFailure(request.Context(), middleware.auditRecorder, traceID, "authentication.bff_assertion", "authentication.assertion_rejected")
				writeError(writer, http.StatusUnauthorized, "unauthorized", traceID)
				return
			}
			traceID = claims.TraceID
			user, resolveErr := middleware.resolver.ResolveOrProvision(request.Context(), middleware.organizationID, oidcverify.VerifiedClaims{
				Issuer: claims.Issuer, Subject: claims.Subject, Email: claims.Email, DisplayName: claims.DisplayName,
			})
			if resolveErr != nil {
				auditAuthenticationFailure(request.Context(), middleware.auditRecorder, traceID, "authentication.bff_identity_binding", "authentication.identity_binding_rejected")
				writeError(writer, http.StatusForbidden, "forbidden", traceID)
				return
			}
			principal := identity.Principal{
				Subject:       "oidc-bff/" + user.ID,
				TenantID:      user.OrganizationID,
				UserID:        user.ID,
				Roles:         legacyRoles,
				AuthMethod:    identity.AuthMethodJWT,
				Authenticated: true,
			}
			ctx := identity.WithPrincipal(request.Context(), principal)
			ctx = runtimecontext.WithTraceID(ctx, traceID)
			ctx = runtimecontext.WithMetadata(ctx, runtimecontext.Metadata{Transport: "http", Protocol: "http", Method: request.Method, Route: request.URL.EscapedPath(), RequestID: traceID})
			if middleware.auditRecorder != nil {
				if err := middleware.auditRecorder.RecordAuthenticationAccepted(ctx, "authentication.bff_assertion"); err != nil {
					writeError(writer, http.StatusServiceUnavailable, "service_unavailable", traceID)
					return
				}
			}
			traced := newTraceResponseWriter(writer, traceID)
			next.ServeHTTP(traced, request.WithContext(ctx))
			traced.commit()
			return
		}
		if !middleware.allowLegacyFallback || middleware.authenticator == nil {
			auditAuthenticationFailure(request.Context(), middleware.auditRecorder, traceID, "authentication.bff_assertion", "authentication.assertion_missing")
			writeError(writer, http.StatusUnauthorized, "unauthorized", traceID)
			return
		}
		principal, err := middleware.authenticator.AuthenticateAPIKey(request.Header.Get(localauth.APIKeyHeader))
		if err != nil {
			auditAuthenticationFailure(request.Context(), middleware.auditRecorder, traceID, "authentication.development_api_key", "authentication.invalid_credential")
			writeError(writer, http.StatusUnauthorized, "unauthorized", traceID)
			return
		}
		ctx := identity.WithPrincipal(request.Context(), principal)
		ctx = runtimecontext.WithTraceID(ctx, traceID)
		ctx = runtimecontext.WithMetadata(ctx, runtimecontext.Metadata{Transport: "http", Protocol: "http", Method: request.Method, Route: request.URL.EscapedPath(), RequestID: traceID})
		traced := newTraceResponseWriter(writer, traceID)
		next.ServeHTTP(traced, request.WithContext(ctx))
		traced.commit()
	})
}

// APIKeyTraceMiddleware keeps the explicit legacy/bootstrap route available
// while giving every response the same server-generated trace contract.
func APIKeyTraceMiddleware(authenticator *localauth.Authenticator, recorders ...*audit.SecurityRecorder) func(http.Handler) http.Handler {
	var recorder *audit.SecurityRecorder
	if len(recorders) > 0 {
		recorder = recorders[0]
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			traceID := newTraceID()
			if authenticator == nil {
				writeError(writer, http.StatusServiceUnavailable, "service_unavailable", traceID)
				return
			}
			principal, err := authenticator.AuthenticateAPIKey(request.Header.Get(localauth.APIKeyHeader))
			if err != nil {
				auditAuthenticationFailure(request.Context(), recorder, traceID, "authentication.development_api_key", "authentication.invalid_credential")
				writeError(writer, http.StatusUnauthorized, "unauthorized", traceID)
				return
			}
			if hasAssertionHeaders(request.Header) {
				auditAuthenticationFailure(request.Context(), recorder, traceID, "authentication.development_api_key", "authentication.mixed_credentials")
				writeError(writer, http.StatusUnauthorized, "unauthorized", traceID)
				return
			}
			ctx := identity.WithPrincipal(request.Context(), principal)
			ctx = runtimecontext.WithTraceID(ctx, traceID)
			ctx = runtimecontext.WithMetadata(ctx, runtimecontext.Metadata{Transport: "http", Protocol: "http", Method: request.Method, Route: request.URL.EscapedPath(), RequestID: traceID})
			traced := newTraceResponseWriter(writer, traceID)
			next.ServeHTTP(traced, request.WithContext(ctx))
			traced.commit()
		})
	}
}

func auditAuthenticationFailure(ctx context.Context, recorder *audit.SecurityRecorder, traceID, operation, reasonCode string) {
	if recorder == nil {
		return
	}
	ctx = runtimecontext.WithTraceID(ctx, traceID)
	ctx = runtimecontext.WithMetadata(ctx, runtimecontext.Metadata{Transport: "http", Protocol: "http", RequestID: traceID})
	_ = recorder.RecordAuthenticationFailure(ctx, operation, "http", reasonCode)
}

func hasAssertionHeaders(headers http.Header) bool {
	return len(headers.Values(bffassertion.AssertionHeader)) > 0 || len(headers.Values(bffassertion.SignatureHeader)) > 0 || len(headers.Values(bffassertion.TraceHeader)) > 0
}

func readAndRestoreBody(request *http.Request) ([]byte, error) {
	if request.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(http.MaxBytesReader(nil, request.Body, maxRequestBodyBytes))
	if err != nil {
		return nil, err
	}
	request.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

func newTraceID() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "00000000000000000000000000000000"
	}
	return hex.EncodeToString(value)
}

func writeError(writer http.ResponseWriter, status int, code, traceID string) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set(bffassertion.TraceHeader, traceID)
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(map[string]string{"error": code, "traceId": traceID})
}

type traceResponseWriter struct {
	destination http.ResponseWriter
	traceID     string
	header      http.Header
	body        bytes.Buffer
	status      int
	wroteHeader bool
	buffered    bool
}

func newTraceResponseWriter(destination http.ResponseWriter, traceID string) *traceResponseWriter {
	destination.Header().Set(bffassertion.TraceHeader, traceID)
	return &traceResponseWriter{destination: destination, traceID: traceID}
}

func (writer *traceResponseWriter) Header() http.Header { return writer.destination.Header() }

func (writer *traceResponseWriter) WriteHeader(status int) {
	if writer.wroteHeader {
		return
	}
	writer.wroteHeader = true
	writer.status = status
	if status >= http.StatusBadRequest {
		writer.buffered = true
		writer.header = writer.destination.Header().Clone()
		clearHeader(writer.destination.Header())
		return
	}
	writer.destination.WriteHeader(status)
}

func (writer *traceResponseWriter) Write(value []byte) (int, error) {
	if !writer.wroteHeader {
		writer.WriteHeader(http.StatusOK)
	}
	if writer.buffered {
		remaining := maxErrorResponseBytes - writer.body.Len()
		if remaining > 0 {
			_, _ = writer.body.Write(value[:min(len(value), remaining)])
		}
		return len(value), nil
	}
	return writer.destination.Write(value)
}

func (writer *traceResponseWriter) commit() {
	if !writer.buffered {
		return
	}
	body := writer.body.Bytes()
	payload := map[string]any{"error": "request_failed"}
	if writer.status < http.StatusInternalServerError {
		var candidate map[string]any
		if err := json.Unmarshal(body, &candidate); err == nil && candidate != nil {
			payload = candidate
			if _, ok := payload["error"]; !ok {
				payload["error"] = "request_failed"
			}
		}
	} else {
		payload["error"] = "internal_error"
	}
	payload["traceId"] = writer.traceID
	body, _ = json.Marshal(payload)
	body = append(body, '\n')
	writer.header.Set("Content-Type", "application/json; charset=utf-8")
	writer.header.Del("Content-Length")
	for name, values := range writer.header {
		for _, value := range values {
			writer.destination.Header().Add(name, value)
		}
	}
	writer.destination.Header().Set(bffassertion.TraceHeader, writer.traceID)
	writer.destination.WriteHeader(writer.status)
	_, _ = writer.destination.Write(body)
}

func clearHeader(header http.Header) {
	for name := range header {
		delete(header, name)
	}
}
