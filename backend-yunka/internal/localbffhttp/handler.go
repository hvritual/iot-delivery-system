// Package localbffhttp exposes browser-facing local-auth BFF routes while
// delegating identity, credential, session, member and RoleBinding semantics to
// the existing YU-20..YU-25 application capabilities.
package localbffhttp

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/bffassertion"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localcredential"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/locallogin"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localmemberadmin"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localprojectroleadmin"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localtransportauth"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/framework/core/runtimecontext"
	"github.com/hvritual/yunka.io/gateway/authz"
)

const (
	RoutePrefix        = "/auth/local/"
	SessionCookieName  = "__Host-iotd_local_session"
	CSRFCookieName     = "__Host-iotd_local_csrf"
	CSRFHeader         = "X-CSRF-Token"
	maxRequestBodySize = 64 << 10
	opaqueValueBytes   = 32
)

type Config struct {
	Login         *locallogin.Manager
	Verifier      *localtransportauth.Verifier
	Members       *localmemberadmin.Manager
	ProjectRoles  *localprojectroleadmin.Manager
	TrustedOrigin string
	Clock         func() time.Time
	Random        io.Reader
}

type Handler struct {
	login         *locallogin.Manager
	verifier      *localtransportauth.Verifier
	members       *localmemberadmin.Manager
	projectRoles  *localprojectroleadmin.Manager
	trustedOrigin string
	clock         func() time.Time
	random        io.Reader
}

func New(config Config) (*Handler, error) {
	if config.Login == nil || config.Verifier == nil || config.Members == nil || config.ProjectRoles == nil {
		return nil, errors.New("local auth BFF dependencies are required")
	}
	origin := ""
	if strings.TrimSpace(config.TrustedOrigin) != "" {
		var err error
		origin, err = CanonicalOrigin(config.TrustedOrigin)
		if err != nil {
			return nil, err
		}
	}
	clock := config.Clock
	if clock == nil {
		clock = time.Now
	}
	random := config.Random
	if random == nil {
		random = rand.Reader
	}
	return &Handler{
		login: config.Login, verifier: config.Verifier, members: config.Members,
		projectRoles: config.ProjectRoles, trustedOrigin: origin, clock: clock, random: random,
	}, nil
}

// CanonicalOrigin accepts only an absolute HTTP(S) origin with no path, query,
// fragment or userinfo. When no explicit origin is configured, unsafe routes
// use the request Host as the same-origin authority and permit plaintext only
// for loopback development hosts.
func CanonicalOrigin(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("local auth BFF trusted origin is required")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed == nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", errors.New("local auth BFF trusted origin must be a canonical HTTP(S) origin")
	}
	canonical := parsed.Scheme + "://" + parsed.Host
	if value != canonical {
		return "", errors.New("local auth BFF trusted origin must not contain a trailing slash or path")
	}
	return canonical, nil
}

func IsRoute(path string) bool {
	return strings.HasPrefix(path, RoutePrefix)
}

// BypassProtectedMiddleware leaves only the dedicated local-auth namespace to
// this handler. Every other runtime route still traverses the existing BFF /
// local-JWT / service authentication chain.
func BypassProtectedMiddleware(protected func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		var protectedHandler http.Handler
		if protected != nil {
			protectedHandler = protected(next)
		}
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if IsRoute(request.URL.Path) {
				next.ServeHTTP(writer, request)
				return
			}
			if protectedHandler == nil {
				writeError(writer, http.StatusServiceUnavailable, "service_unavailable", newTraceID())
				return
			}
			protectedHandler.ServeHTTP(writer, request)
		})
	}
}

func (handler *Handler) Register(mux *http.ServeMux) error {
	if handler == nil || mux == nil {
		return errors.New("local auth BFF handler and HTTP mux are required")
	}
	mux.HandleFunc("POST /auth/local/login", handler.handleLogin)
	mux.HandleFunc("GET /auth/local/current", handler.handleCurrent)
	mux.HandleFunc("POST /auth/local/logout", handler.handleLogout)
	mux.HandleFunc("POST /auth/local/change-password", handler.handleChangePassword)
	mux.HandleFunc("POST /auth/local/admin/members", handler.handleCreateMember)
	mux.HandleFunc("POST /auth/local/admin/members/{userID}/disable", handler.handleDisableMember)
	mux.HandleFunc("POST /auth/local/admin/members/{userID}/reset-credential", handler.handleResetCredential)
	mux.HandleFunc("POST /auth/local/admin/project-role-bindings", handler.handleAssignProjectRole)
	mux.HandleFunc("POST /auth/local/admin/project-role-bindings/{bindingID}/revoke", handler.handleRevokeProjectRole)
	return nil
}

type loginRequest struct {
	OrganizationID string `json:"organizationId"`
	UserID         string `json:"userId"`
	Password       string `json:"password"`
}

func (handler *Handler) handleLogin(writer http.ResponseWriter, request *http.Request) {
	traceID := prepareResponse(writer)
	if !handler.validOrigin(request) {
		writeError(writer, http.StatusForbidden, "forbidden", traceID)
		return
	}
	var body loginRequest
	if err := decodeJSON(request, &body); err != nil || body.Password == "" {
		writeError(writer, http.StatusBadRequest, "invalid_request", traceID)
		return
	}
	password := []byte(body.Password)
	body.Password = ""
	defer zeroBytes(password)
	ctx := requestContext(request.Context(), request, traceID)
	result, err := handler.login.Login(ctx, locallogin.LoginInput{
		OrganizationID: body.OrganizationID,
		UserID:         body.UserID,
		Password:       password,
	})
	if err != nil {
		if writeThrottleError(writer, err, traceID) {
			return
		}
		if errors.Is(err, locallogin.ErrAuthenticationFailed) {
			writeError(writer, http.StatusUnauthorized, "unauthenticated", traceID)
			return
		}
		writeError(writer, http.StatusServiceUnavailable, "service_unavailable", traceID)
		return
	}
	csrf, err := handler.newOpaqueValue()
	if err != nil || handler.setSessionCookies(writer, result.SessionToken, csrf, result.SessionExpiresAt) != nil {
		// The session exists server-side but cannot be safely delivered to the
		// browser. Best-effort revoke it rather than return an orphaned bearer.
		_, _ = handler.login.Logout(ctx, locallogin.LogoutInput{SessionToken: result.SessionToken, ExpectedSessionRevision: result.SessionRevision})
		writeError(writer, http.StatusServiceUnavailable, "service_unavailable", traceID)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"authenticated":    true,
		"organizationId":   result.OrganizationID,
		"userId":           result.UserID,
		"sessionExpiresAt": result.SessionExpiresAt,
		"accessToken":      result.AccessToken,
		"accessExpiresAt":  result.AccessExpiresAt,
		"csrfToken":        csrf,
		"traceId":          traceID,
	})
}

func (handler *Handler) handleCurrent(writer http.ResponseWriter, request *http.Request) {
	traceID := prepareResponse(writer)
	token, ok := exactOpaqueCookie(request, SessionCookieName)
	if !ok {
		handler.clearCookies(writer)
		writeError(writer, http.StatusUnauthorized, "unauthenticated", traceID)
		return
	}
	ctx := requestContext(request.Context(), request, traceID)
	member, err := handler.login.CurrentMemberFromSessionToken(ctx, token)
	if err != nil {
		handler.clearCookies(writer)
		writeError(writer, http.StatusUnauthorized, "unauthenticated", traceID)
		return
	}
	access, err := handler.login.IssueAccessTokenFromSession(ctx, token)
	if err != nil {
		handler.clearCookies(writer)
		writeError(writer, http.StatusUnauthorized, "unauthenticated", traceID)
		return
	}
	csrf, ok := exactOpaqueCookie(request, CSRFCookieName)
	if !ok {
		csrf, err = handler.newOpaqueValue()
		if err != nil || handler.setCSRFCookie(writer, csrf, member.SessionExpiresAt) != nil {
			writeError(writer, http.StatusServiceUnavailable, "service_unavailable", traceID)
			return
		}
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"authenticated":    true,
		"organizationId":   member.OrganizationID,
		"userId":           member.UserID,
		"displayName":      member.DisplayName,
		"email":            member.Email,
		"userRevision":     member.UserRevision,
		"sessionRevision":  member.SessionRevision,
		"sessionExpiresAt": member.SessionExpiresAt,
		"accessToken":      access.AccessToken,
		"accessExpiresAt":  access.AccessExpiresAt,
		"csrfToken":        csrf,
		"traceId":          traceID,
	})
}

func (handler *Handler) handleLogout(writer http.ResponseWriter, request *http.Request) {
	traceID := prepareResponse(writer)
	token, session, ok := handler.requireUnsafeSession(writer, request, traceID)
	if !ok {
		return
	}
	ctx := requestContext(request.Context(), request, traceID)
	if _, err := handler.login.Logout(ctx, locallogin.LogoutInput{SessionToken: token, ExpectedSessionRevision: session.Revision}); err != nil {
		handler.clearCookies(writer)
		writeError(writer, http.StatusUnauthorized, "unauthenticated", traceID)
		return
	}
	handler.clearCookies(writer)
	writer.WriteHeader(http.StatusNoContent)
}

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

func (handler *Handler) handleChangePassword(writer http.ResponseWriter, request *http.Request) {
	traceID := prepareResponse(writer)
	token, session, ok := handler.requireUnsafeSession(writer, request, traceID)
	if !ok {
		return
	}
	var body changePasswordRequest
	if err := decodeJSON(request, &body); err != nil || body.CurrentPassword == "" || body.NewPassword == "" {
		writeError(writer, http.StatusBadRequest, "invalid_request", traceID)
		return
	}
	currentPassword := []byte(body.CurrentPassword)
	newPassword := []byte(body.NewPassword)
	body.CurrentPassword, body.NewPassword = "", ""
	defer zeroBytes(currentPassword)
	defer zeroBytes(newPassword)
	ctx := requestContext(request.Context(), request, traceID)
	member, err := handler.login.CurrentMemberFromSessionToken(ctx, token)
	if err != nil {
		handler.clearCookies(writer)
		writeError(writer, http.StatusUnauthorized, "unauthenticated", traceID)
		return
	}
	result, err := handler.login.ChangePassword(ctx, locallogin.ChangePasswordInput{
		SessionToken:               token,
		ExpectedSessionRevision:    session.Revision,
		ExpectedUserRevision:       member.UserRevision,
		ExpectedCredentialRevision: session.CredentialRevision,
		CurrentPassword:            currentPassword,
		NewPassword:                newPassword,
	})
	if err != nil {
		if writeThrottleError(writer, err, traceID) {
			return
		}
		switch {
		case errors.Is(err, locallogin.ErrCurrentPasswordInvalid), errors.Is(err, localcredential.ErrInvalidPassword):
			writeError(writer, http.StatusBadRequest, "invalid_request", traceID)
		case errors.Is(err, locallogin.ErrSessionInvalid), errors.Is(err, locallogin.ErrSessionRevisionConflict):
			handler.clearCookies(writer)
			writeError(writer, http.StatusUnauthorized, "unauthenticated", traceID)
		case errors.Is(err, locallogin.ErrUserRevisionConflict), errors.Is(err, localcredential.ErrRevisionConflict):
			writeError(writer, http.StatusConflict, "conflict", traceID)
		default:
			writeError(writer, http.StatusServiceUnavailable, "service_unavailable", traceID)
		}
		return
	}
	handler.clearCookies(writer)
	writeJSON(writer, http.StatusOK, map[string]any{
		"changed":            true,
		"userRevision":       result.UserRevision,
		"credentialRevision": result.CredentialRevision,
		"traceId":            traceID,
	})
}

type createMemberRequest struct {
	DisplayName string `json:"displayName"`
	Email       string `json:"email"`
	Password    string `json:"password"`
}

func (handler *Handler) handleCreateMember(writer http.ResponseWriter, request *http.Request) {
	traceID := prepareResponse(writer)
	ctx, ok := handler.requireAdminContext(writer, request, traceID)
	if !ok {
		return
	}
	var body createMemberRequest
	if err := decodeJSON(request, &body); err != nil || body.Password == "" {
		writeError(writer, http.StatusBadRequest, "invalid_request", traceID)
		return
	}
	password := []byte(body.Password)
	body.Password = ""
	defer zeroBytes(password)
	result, err := handler.members.Create(ctx, localmemberadmin.CreateInput{DisplayName: body.DisplayName, Email: body.Email, Password: password})
	if err != nil {
		handler.writeManagerError(writer, err, traceID)
		return
	}
	writeJSON(writer, http.StatusCreated, result)
}

type revisionRequest struct {
	ExpectedRevision int64 `json:"expectedRevision"`
}

func (handler *Handler) handleDisableMember(writer http.ResponseWriter, request *http.Request) {
	traceID := prepareResponse(writer)
	ctx, ok := handler.requireAdminContext(writer, request, traceID)
	if !ok {
		return
	}
	var body revisionRequest
	if err := decodeJSON(request, &body); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", traceID)
		return
	}
	result, err := handler.members.Disable(ctx, localmemberadmin.DisableInput{UserID: request.PathValue("userID"), ExpectedRevision: body.ExpectedRevision})
	if err != nil {
		handler.writeManagerError(writer, err, traceID)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

type resetCredentialRequest struct {
	ExpectedUserRevision       int64  `json:"expectedUserRevision"`
	ExpectedCredentialRevision int64  `json:"expectedCredentialRevision"`
	Password                   string `json:"password"`
}

func (handler *Handler) handleResetCredential(writer http.ResponseWriter, request *http.Request) {
	traceID := prepareResponse(writer)
	ctx, ok := handler.requireAdminContext(writer, request, traceID)
	if !ok {
		return
	}
	var body resetCredentialRequest
	if err := decodeJSON(request, &body); err != nil || body.Password == "" {
		writeError(writer, http.StatusBadRequest, "invalid_request", traceID)
		return
	}
	password := []byte(body.Password)
	body.Password = ""
	defer zeroBytes(password)
	result, err := handler.members.ResetCredential(ctx, localmemberadmin.ResetCredentialInput{
		UserID:                     request.PathValue("userID"),
		ExpectedUserRevision:       body.ExpectedUserRevision,
		ExpectedCredentialRevision: body.ExpectedCredentialRevision,
		Password:                   password,
	})
	if err != nil {
		handler.writeManagerError(writer, err, traceID)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

type assignProjectRoleRequest struct {
	ProjectID string `json:"projectId"`
	UserID    string `json:"userId"`
	RoleID    string `json:"roleId"`
}

func (handler *Handler) handleAssignProjectRole(writer http.ResponseWriter, request *http.Request) {
	traceID := prepareResponse(writer)
	ctx, ok := handler.requireAdminContext(writer, request, traceID)
	if !ok {
		return
	}
	var body assignProjectRoleRequest
	if err := decodeJSON(request, &body); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", traceID)
		return
	}
	result, err := handler.projectRoles.Assign(ctx, localprojectroleadmin.AssignInput{ProjectID: body.ProjectID, UserID: body.UserID, RoleID: body.RoleID})
	if err != nil {
		handler.writeManagerError(writer, err, traceID)
		return
	}
	writeJSON(writer, http.StatusCreated, result)
}

func (handler *Handler) handleRevokeProjectRole(writer http.ResponseWriter, request *http.Request) {
	traceID := prepareResponse(writer)
	ctx, ok := handler.requireAdminContext(writer, request, traceID)
	if !ok {
		return
	}
	var body revisionRequest
	if err := decodeJSON(request, &body); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", traceID)
		return
	}
	result, err := handler.projectRoles.Revoke(ctx, localprojectroleadmin.RevokeInput{BindingID: request.PathValue("bindingID"), ExpectedRevision: body.ExpectedRevision})
	if err != nil {
		handler.writeManagerError(writer, err, traceID)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (handler *Handler) requireAdminContext(writer http.ResponseWriter, request *http.Request, traceID string) (context.Context, bool) {
	if !handler.validOrigin(request) || !validCSRF(request) {
		writeError(writer, http.StatusForbidden, "forbidden", traceID)
		return nil, false
	}
	token, ok := exactOpaqueCookie(request, SessionCookieName)
	if !ok {
		handler.clearCookies(writer)
		writeError(writer, http.StatusUnauthorized, "unauthenticated", traceID)
		return nil, false
	}
	ctx := requestContext(request.Context(), request, traceID)
	principal, err := handler.verifier.VerifySessionToken(ctx, token)
	if err != nil || !principal.Authenticated || principal.AuthMethod != identity.AuthMethodJWT || len(principal.Roles) != 0 {
		handler.clearCookies(writer)
		writeError(writer, http.StatusUnauthorized, "unauthenticated", traceID)
		return nil, false
	}
	return identity.WithPrincipal(ctx, principal), true
}

func (handler *Handler) requireUnsafeSession(writer http.ResponseWriter, request *http.Request, traceID string) (string, locallogin.SessionIdentity, bool) {
	if !handler.validOrigin(request) || !validCSRF(request) {
		writeError(writer, http.StatusForbidden, "forbidden", traceID)
		return "", locallogin.SessionIdentity{}, false
	}
	token, ok := exactOpaqueCookie(request, SessionCookieName)
	if !ok {
		handler.clearCookies(writer)
		writeError(writer, http.StatusUnauthorized, "unauthenticated", traceID)
		return "", locallogin.SessionIdentity{}, false
	}
	session, err := handler.login.VerifySessionToken(request.Context(), token)
	if err != nil {
		handler.clearCookies(writer)
		writeError(writer, http.StatusUnauthorized, "unauthenticated", traceID)
		return "", locallogin.SessionIdentity{}, false
	}
	return token, session, true
}

func (handler *Handler) writeManagerError(writer http.ResponseWriter, err error, traceID string) {
	if writeThrottleError(writer, err, traceID) {
		return
	}
	switch {
	case authz.IsDenied(err):
		writeError(writer, http.StatusForbidden, "forbidden", traceID)
	case errors.Is(err, localcredential.ErrInvalidPassword), errors.Is(err, localmemberadmin.ErrInvalidInput), errors.Is(err, localprojectroleadmin.ErrInvalidInput), errors.Is(err, localprojectroleadmin.ErrRoleNotAssignable):
		writeError(writer, http.StatusBadRequest, "invalid_request", traceID)
	case errors.Is(err, localmemberadmin.ErrMemberNotFound), errors.Is(err, localprojectroleadmin.ErrMemberNotFound), errors.Is(err, localprojectroleadmin.ErrProjectNotFound), errors.Is(err, localprojectroleadmin.ErrBindingNotFound):
		writeError(writer, http.StatusNotFound, "not_found", traceID)
	case errors.Is(err, localmemberadmin.ErrMemberRevisionConflict), errors.Is(err, localmemberadmin.ErrMemberDisabled), errors.Is(err, localmemberadmin.ErrLastAdministrator),
		errors.Is(err, localprojectroleadmin.ErrMemberDisabled), errors.Is(err, localprojectroleadmin.ErrBindingAlreadyActive), errors.Is(err, localprojectroleadmin.ErrBindingRevisionConflict), errors.Is(err, localprojectroleadmin.ErrBindingRevoked):
		writeError(writer, http.StatusConflict, "conflict", traceID)
	default:
		writeError(writer, http.StatusServiceUnavailable, "service_unavailable", traceID)
	}
}

func (handler *Handler) validOrigin(request *http.Request) bool {
	if handler == nil || request == nil {
		return false
	}
	values := request.Header.Values("Origin")
	if len(values) != 1 {
		return false
	}
	if handler.trustedOrigin != "" {
		return values[0] == handler.trustedOrigin
	}
	origin, err := CanonicalOrigin(values[0])
	if err != nil {
		return false
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host != request.Host {
		return false
	}
	if parsed.Scheme == "https" {
		return true
	}
	return parsed.Scheme == "http" && loopbackHost(request.Host)
}

func loopbackHost(hostPort string) bool {
	host := hostPort
	if parsedHost, _, err := net.SplitHostPort(hostPort); err == nil {
		host = parsedHost
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func validCSRF(request *http.Request) bool {
	if request == nil {
		return false
	}
	cookie, ok := exactOpaqueCookie(request, CSRFCookieName)
	if !ok {
		return false
	}
	values := request.Header.Values(CSRFHeader)
	if len(values) != 1 || values[0] == "" || values[0] != strings.TrimSpace(values[0]) {
		return false
	}
	left := []byte(cookie)
	right := []byte(values[0])
	return len(left) == len(right) && subtle.ConstantTimeCompare(left, right) == 1
}

func exactOpaqueCookie(request *http.Request, name string) (string, bool) {
	if request == nil {
		return "", false
	}
	var found string
	count := 0
	for _, cookie := range request.Cookies() {
		if cookie.Name != name {
			continue
		}
		count++
		found = cookie.Value
	}
	if count != 1 || !canonicalOpaqueValue(found) {
		return "", false
	}
	return found, true
}

func canonicalOpaqueValue(value string) bool {
	if value == "" || value != strings.TrimSpace(value) {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) != opaqueValueBytes || base64.RawURLEncoding.EncodeToString(decoded) != value {
		return false
	}
	return true
}

func (handler *Handler) newOpaqueValue() (string, error) {
	raw := make([]byte, opaqueValueBytes)
	if _, err := io.ReadFull(handler.random, raw); err != nil {
		return "", errors.New("generate local BFF CSRF token")
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (handler *Handler) setSessionCookies(writer http.ResponseWriter, sessionToken, csrf string, expiresAt time.Time) error {
	if !canonicalOpaqueValue(sessionToken) || !canonicalOpaqueValue(csrf) {
		return errors.New("local BFF cookie value is invalid")
	}
	if err := handler.setCookie(writer, SessionCookieName, sessionToken, expiresAt, true); err != nil {
		return err
	}
	return handler.setCookie(writer, CSRFCookieName, csrf, expiresAt, false)
}

func (handler *Handler) setCSRFCookie(writer http.ResponseWriter, csrf string, expiresAt time.Time) error {
	if !canonicalOpaqueValue(csrf) {
		return errors.New("local BFF CSRF cookie value is invalid")
	}
	return handler.setCookie(writer, CSRFCookieName, csrf, expiresAt, false)
}

func (handler *Handler) setCookie(writer http.ResponseWriter, name, value string, expiresAt time.Time, httpOnly bool) error {
	now := handler.clock().UTC()
	expiresAt = expiresAt.UTC()
	if now.IsZero() || !expiresAt.After(now) {
		return errors.New("local BFF cookie expiry is invalid")
	}
	maxAge := int(expiresAt.Sub(now) / time.Second)
	if maxAge < 1 {
		return errors.New("local BFF cookie lifetime is invalid")
	}
	http.SetCookie(writer, &http.Cookie{
		Name: name, Value: value, Path: "/", Expires: expiresAt, MaxAge: maxAge,
		Secure: true, HttpOnly: httpOnly, SameSite: http.SameSiteStrictMode,
	})
	return nil
}

func (handler *Handler) clearCookies(writer http.ResponseWriter) {
	for _, cookie := range []*http.Cookie{
		{Name: SessionCookieName, Path: "/", MaxAge: -1, Expires: time.Unix(1, 0).UTC(), Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode},
		{Name: CSRFCookieName, Path: "/", MaxAge: -1, Expires: time.Unix(1, 0).UTC(), Secure: true, HttpOnly: false, SameSite: http.SameSiteStrictMode},
	} {
		http.SetCookie(writer, cookie)
	}
}

func decodeJSON(request *http.Request, destination any) error {
	if request == nil || request.Body == nil {
		return errors.New("request body is required")
	}
	decoder := json.NewDecoder(http.MaxBytesReader(nil, request.Body, maxRequestBodySize))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) == nil {
		return errors.New("request body contains trailing JSON")
	}
	return nil
}

func requestContext(ctx context.Context, request *http.Request, traceID string) context.Context {
	ctx = locallogin.WithPeerAddress(ctx, request.RemoteAddr)
	ctx = runtimecontext.WithTraceID(ctx, traceID)
	return runtimecontext.WithMetadata(ctx, runtimecontext.Metadata{
		Transport: "http", Protocol: "http", Method: request.Method,
		Route: request.URL.EscapedPath(), RequestID: traceID,
	})
}

func prepareResponse(writer http.ResponseWriter) string {
	traceID := newTraceID()
	writer.Header().Set("Cache-Control", "no-store, max-age=0")
	writer.Header().Set("Pragma", "no-cache")
	writer.Header().Set("Vary", "Cookie, Origin")
	writer.Header().Set(bffassertion.TraceHeader, traceID)
	return traceID
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeError(writer http.ResponseWriter, status int, code, traceID string) {
	writer.Header().Set("Cache-Control", "no-store, max-age=0")
	writer.Header().Set("Pragma", "no-cache")
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set(bffassertion.TraceHeader, traceID)
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(map[string]string{"error": code, "traceId": traceID})
}

func newTraceID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "00000000000000000000000000000000"
	}
	return hex.EncodeToString(raw[:])
}

func zeroBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
