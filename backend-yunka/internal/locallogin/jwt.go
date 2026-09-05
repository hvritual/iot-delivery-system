package locallogin

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hvritual/yunka.io/framework/core/identity"
)

const (
	JWTVersion             = 2
	JWTAlgorithm           = "HS256"
	JWTType                = "JWT"
	DefaultIssuer          = "iot-delivery.local"
	DefaultAudience        = "iot-delivery.internal"
	DefaultKeyID           = "local-auth-v1"
	DefaultSessionTTL      = 12 * time.Hour
	DefaultAccessTTL       = 5 * time.Minute
	minimumSigningKeyBytes = 32
	maximumJWTBytes        = 8192
	allowedClockSkew       = 30 * time.Second
)

var ErrAccessTokenInvalid = errors.New("local access token is invalid")

type Config struct {
	Issuer     string
	Audience   string
	KeyID      string
	SigningKey []byte
	SessionTTL time.Duration
	AccessTTL  time.Duration
}

func DefaultConfig(signingKey []byte) Config {
	return Config{
		Issuer:     DefaultIssuer,
		Audience:   DefaultAudience,
		KeyID:      DefaultKeyID,
		SigningKey: append([]byte(nil), signingKey...),
		SessionTTL: DefaultSessionTTL,
		AccessTTL:  DefaultAccessTTL,
	}
}

func (config Config) validate() error {
	if !canonicalJWTIdentifier(config.Issuer, 255) || !canonicalJWTIdentifier(config.Audience, 255) || !canonicalJWTIdentifier(config.KeyID, 128) {
		return errors.New("local JWT issuer, audience, and key ID are required")
	}
	if len(config.SigningKey) < minimumSigningKeyBytes {
		return errors.New("local JWT signing key must be at least 32 bytes")
	}
	if config.SessionTTL <= 0 || config.AccessTTL <= 0 || config.AccessTTL > config.SessionTTL || config.AccessTTL > 15*time.Minute || config.SessionTTL > 30*24*time.Hour {
		return errors.New("local JWT/session TTL contract is invalid")
	}
	if config.SessionTTL%time.Second != 0 || config.AccessTTL%time.Second != 0 {
		return errors.New("local JWT/session TTLs must use whole seconds")
	}
	return nil
}

type jwtHeader struct {
	Algorithm string `json:"alg"`
	Type      string `json:"typ"`
	KeyID     string `json:"kid"`
}

type jwtClaims struct {
	Issuer          string `json:"iss"`
	Audience        string `json:"aud"`
	Subject         string `json:"sub"`
	TenantID        string `json:"tid"`
	SessionID       string `json:"sid"`
	SessionRevision int64  `json:"sv"`
	IssuedAt        int64  `json:"iat"`
	ExpiresAt       int64  `json:"exp"`
	Version         int    `json:"ver"`
}

// signAccessToken is retained for YU-21 compatibility tests and signs the
// initial session revision. Runtime renewal uses signAccessTokenForSession.
func signAccessToken(config Config, organizationID, userID, sessionID string, issuedAt time.Time) (string, time.Time, error) {
	return signAccessTokenForSession(config, organizationID, userID, sessionID, 1, issuedAt)
}

func signAccessTokenForSession(config Config, organizationID, userID, sessionID string, sessionRevision int64, issuedAt time.Time) (string, time.Time, error) {
	if err := config.validate(); err != nil {
		return "", time.Time{}, err
	}
	if !canonicalIdentifier(organizationID) || !canonicalIdentifier(userID) || !canonicalIdentifier(sessionID) || sessionRevision < 1 {
		return "", time.Time{}, ErrAccessTokenInvalid
	}
	issuedAt = issuedAt.UTC().Truncate(time.Second)
	if issuedAt.IsZero() {
		return "", time.Time{}, ErrAccessTokenInvalid
	}
	expiresAt := issuedAt.Add(config.AccessTTL)
	header := jwtHeader{Algorithm: JWTAlgorithm, Type: JWTType, KeyID: config.KeyID}
	claims := jwtClaims{
		Issuer: config.Issuer, Audience: config.Audience,
		Subject: userID, TenantID: organizationID, SessionID: sessionID, SessionRevision: sessionRevision,
		IssuedAt: issuedAt.Unix(), ExpiresAt: expiresAt.Unix(), Version: JWTVersion,
	}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", time.Time{}, errors.New("encode local JWT header")
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", time.Time{}, errors.New("encode local JWT claims")
	}
	encodedHeader := base64.RawURLEncoding.EncodeToString(headerJSON)
	encodedClaims := base64.RawURLEncoding.EncodeToString(claimsJSON)
	signingInput := encodedHeader + "." + encodedClaims
	signature := hmac.New(sha256.New, config.SigningKey)
	_, _ = signature.Write([]byte(signingInput))
	encodedSignature := base64.RawURLEncoding.EncodeToString(signature.Sum(nil))
	return signingInput + "." + encodedSignature, expiresAt, nil
}

func verifyAccessTokenSignature(config Config, token string, now time.Time) (jwtClaims, error) {
	if err := config.validate(); err != nil {
		return jwtClaims{}, err
	}
	if token == "" || token != strings.TrimSpace(token) || len(token) > maximumJWTBytes || strings.Count(token, ".") != 2 {
		return jwtClaims{}, ErrAccessTokenInvalid
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return jwtClaims{}, ErrAccessTokenInvalid
	}
	headerJSON, err := decodeJWTPart(parts[0])
	if err != nil {
		return jwtClaims{}, ErrAccessTokenInvalid
	}
	claimsJSON, err := decodeJWTPart(parts[1])
	if err != nil {
		return jwtClaims{}, ErrAccessTokenInvalid
	}
	signature, err := decodeJWTPart(parts[2])
	if err != nil || len(signature) != sha256.Size {
		return jwtClaims{}, ErrAccessTokenInvalid
	}
	mac := hmac.New(sha256.New, config.SigningKey)
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return jwtClaims{}, ErrAccessTokenInvalid
	}
	var header jwtHeader
	if err := decodeCanonicalJSON(headerJSON, &header); err != nil || header != (jwtHeader{Algorithm: JWTAlgorithm, Type: JWTType, KeyID: config.KeyID}) {
		return jwtClaims{}, ErrAccessTokenInvalid
	}
	var claims jwtClaims
	if err := decodeCanonicalJSON(claimsJSON, &claims); err != nil {
		return jwtClaims{}, ErrAccessTokenInvalid
	}
	if claims.Issuer != config.Issuer || claims.Audience != config.Audience || claims.Version != JWTVersion ||
		!canonicalIdentifier(claims.Subject) || !canonicalIdentifier(claims.TenantID) || !canonicalIdentifier(claims.SessionID) || claims.SessionRevision < 1 ||
		claims.IssuedAt <= 0 || claims.ExpiresAt <= claims.IssuedAt || claims.ExpiresAt-claims.IssuedAt != int64(config.AccessTTL/time.Second) {
		return jwtClaims{}, ErrAccessTokenInvalid
	}
	now = now.UTC()
	if now.IsZero() || claims.ExpiresAt <= now.Unix() || claims.IssuedAt > now.Add(allowedClockSkew).Unix() {
		return jwtClaims{}, ErrAccessTokenInvalid
	}
	return claims, nil
}

func principalFromVerifiedClaims(claims jwtClaims) identity.Principal {
	return identity.Principal{
		Subject:       "local-user/" + claims.Subject,
		UserID:        claims.Subject,
		TenantID:      claims.TenantID,
		AuthMethod:    identity.AuthMethodJWT,
		Authenticated: true,
	}
}

func decodeJWTPart(value string) ([]byte, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || base64.RawURLEncoding.EncodeToString(decoded) != value {
		return nil, ErrAccessTokenInvalid
	}
	return decoded, nil
}

func decodeCanonicalJSON(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) == nil {
		return errors.New("local JWT contains trailing JSON")
	}
	canonical, err := json.Marshal(destination)
	if err != nil || !bytes.Equal(canonical, data) {
		return fmt.Errorf("local JWT JSON is not canonical")
	}
	return nil
}

func canonicalJWTIdentifier(value string, max int) bool {
	return value != "" && value == strings.TrimSpace(value) && len(value) <= max && !strings.ContainsAny(value, "\r\n\t")
}
