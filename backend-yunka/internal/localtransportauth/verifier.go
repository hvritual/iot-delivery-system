// Package localtransportauth adapts YU-22 verified local-member credentials to
// transport Principals. It never resolves authorization grants and it never
// trusts caller-supplied tenant, user, or role fields.
package localtransportauth

import (
	"context"
	"errors"
	"strings"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/audit"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/locallogin"
	"github.com/hvritual/yunka.io/framework/core/identity"
)

var ErrUnauthenticated = errors.New("local member authentication failed")

type Verifier struct {
	login    *locallogin.Manager
	recorder *audit.SecurityRecorder
}

func New(login *locallogin.Manager, recorder *audit.SecurityRecorder) (*Verifier, error) {
	if login == nil {
		return nil, errors.New("local member login verifier is required")
	}
	return &Verifier{login: login, recorder: recorder}, nil
}

// VerifyAccessToken returns only the Principal produced by YU-22's strict JWT
// signature/claim/session verification. Roles are deliberately absent.
func (verifier *Verifier) VerifyAccessToken(ctx context.Context, token string) (identity.Principal, error) {
	if verifier == nil || verifier.login == nil || token == "" || token != strings.TrimSpace(token) {
		return identity.Principal{}, ErrUnauthenticated
	}
	principal, err := verifier.login.VerifyAccessToken(ctx, token)
	if err != nil || !validLocalHuman(principal) {
		return identity.Principal{}, ErrUnauthenticated
	}
	principal.Roles = nil
	return principal, nil
}

// VerifySessionToken maps only a verified server-side opaque session to the
// same local-human Principal shape used by verified access JWTs. It is used by
// stdio MCP so revocation is re-read for every tool invocation.
func (verifier *Verifier) VerifySessionToken(ctx context.Context, token string) (identity.Principal, error) {
	if verifier == nil || verifier.login == nil || token == "" || token != strings.TrimSpace(token) {
		return identity.Principal{}, ErrUnauthenticated
	}
	session, err := verifier.login.VerifySessionToken(ctx, token)
	if err != nil || !canonicalIdentifier(session.OrganizationID) || !canonicalIdentifier(session.UserID) || session.Revision < 1 {
		return identity.Principal{}, ErrUnauthenticated
	}
	return identity.Principal{
		Subject:       "local-user/" + session.UserID,
		TenantID:      session.OrganizationID,
		UserID:        session.UserID,
		AuthMethod:    identity.AuthMethodJWT,
		Authenticated: true,
	}, nil
}

func validLocalHuman(principal identity.Principal) bool {
	return principal.Authenticated && principal.AuthMethod == identity.AuthMethodJWT &&
		canonicalIdentifier(principal.TenantID) && canonicalIdentifier(principal.UserID) &&
		principal.Subject == "local-user/"+principal.UserID
}

func canonicalIdentifier(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && len(value) <= 255
}
