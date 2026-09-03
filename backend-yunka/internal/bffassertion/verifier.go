// Package bffassertion verifies the short-lived, request-bound assertion
// accepted only from the local BFF channel. It deliberately has no transport
// or authorization dependency so failed assertions cannot invoke a use case.
package bffassertion

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	AssertionHeader = "X-IoT-Delivery-Assertion"
	SignatureHeader = "X-IoT-Delivery-Assertion-Signature"
	TraceHeader     = "X-Trace-ID"

	maxAssertionLength = 8192
	maxSignatureLength = 128
	defaultReplayLimit = 10_000
	maxAssertionAge    = 90 * time.Second
	allowedClockSkew   = 30 * time.Second
)

var (
	traceIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
	noncePattern   = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)
	digestPattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)

	ErrInvalidAssertion = errors.New("invalid BFF assertion")
)

// Claims is the versioned, signed BFF assertion contract.
type Claims struct {
	Version     int    `json:"v"`
	Issuer      string `json:"issuer"`
	Subject     string `json:"subject"`
	Email       string `json:"email,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	Nonce       string `json:"nonce"`
	TraceID     string `json:"traceId"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	BodySHA256  string `json:"bodySha256"`
	Iat         int64  `json:"iat"`
	Exp         int64  `json:"exp"`
}

// Config contains only the BFF assertion trust material and test seams.
type Config struct {
	Key            []byte
	Now            func() time.Time
	ReplayCapacity int
}

// Verifier keeps consumed nonces only until their assertion expiry.
type Verifier struct {
	key            []byte
	now            func() time.Time
	replayCapacity int

	mu       sync.Mutex
	consumed map[string]time.Time
}

func NewVerifier(config Config) (*Verifier, error) {
	if len(config.Key) < 32 {
		return nil, ErrInvalidAssertion
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.ReplayCapacity == 0 {
		config.ReplayCapacity = defaultReplayLimit
	}
	if config.ReplayCapacity < 1 {
		return nil, ErrInvalidAssertion
	}
	return &Verifier{
		key:            append([]byte(nil), config.Key...),
		now:            config.Now,
		replayCapacity: config.ReplayCapacity,
		consumed:       make(map[string]time.Time),
	}, nil
}

// Verify authenticates an assertion against the exact received request and
// body. It consumes a nonce only after every validation succeeds.
func (verifier *Verifier) Verify(request *http.Request, body []byte) (Claims, error) {
	if verifier == nil || len(verifier.key) < 32 || verifier.now == nil || request == nil {
		return Claims{}, ErrInvalidAssertion
	}
	payload, signature, traceID, err := assertionHeaders(request.Header)
	if err != nil {
		return Claims{}, err
	}
	if !verifySignature(verifier.key, payload, signature) {
		return Claims{}, ErrInvalidAssertion
	}
	claims, err := parseClaims(payload)
	if err != nil {
		return Claims{}, err
	}
	if !validClaims(claims, request, body, traceID, verifier.now().UTC()) {
		return Claims{}, ErrInvalidAssertion
	}
	if !verifier.consume(claims.Nonce, time.Unix(claims.Exp, 0).UTC()) {
		return Claims{}, ErrInvalidAssertion
	}
	return claims, nil
}

func assertionHeaders(headers http.Header) (string, string, string, error) {
	payload, ok := singleHeader(headers, AssertionHeader, maxAssertionLength)
	if !ok {
		return "", "", "", ErrInvalidAssertion
	}
	signature, ok := singleHeader(headers, SignatureHeader, maxSignatureLength)
	if !ok {
		return "", "", "", ErrInvalidAssertion
	}
	traceID, ok := singleHeader(headers, TraceHeader, 32)
	if !ok || !traceIDPattern.MatchString(traceID) {
		return "", "", "", ErrInvalidAssertion
	}
	return payload, signature, traceID, nil
}

func singleHeader(headers http.Header, name string, limit int) (string, bool) {
	values := headers.Values(name)
	if len(values) != 1 || len(values[0]) == 0 || len(values[0]) > limit {
		return "", false
	}
	return values[0], true
}

func verifySignature(key []byte, payload, encodedSignature string) bool {
	signature, err := base64.RawURLEncoding.DecodeString(encodedSignature)
	if err != nil || len(signature) != sha256.Size {
		return false
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(payload))
	return hmac.Equal(mac.Sum(nil), signature)
}

func parseClaims(payload string) (Claims, error) {
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil || len(raw) == 0 || len(raw) > maxAssertionLength {
		return Claims{}, ErrInvalidAssertion
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var claims Claims
	if err := decoder.Decode(&claims); err != nil {
		return Claims{}, ErrInvalidAssertion
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Claims{}, ErrInvalidAssertion
	}
	return claims, nil
}

func validClaims(claims Claims, request *http.Request, body []byte, traceID string, now time.Time) bool {
	if claims.Version != 1 || !validIdentityValue(claims.Issuer) || !validIdentityValue(claims.Subject) ||
		(claims.Email != "" && !validIdentityValue(claims.Email)) || (claims.DisplayName != "" && !validIdentityValue(claims.DisplayName)) ||
		!noncePattern.MatchString(claims.Nonce) || !traceIDPattern.MatchString(claims.TraceID) || !digestPattern.MatchString(claims.BodySHA256) {
		return false
	}
	if !hmac.Equal([]byte(claims.TraceID), []byte(traceID)) || !hmac.Equal([]byte(claims.Method), []byte(request.Method)) || !hmac.Equal([]byte(claims.Path), []byte(requestPath(request))) {
		return false
	}
	digest := sha256.Sum256(body)
	if !hmac.Equal([]byte(claims.BodySHA256), []byte(hex.EncodeToString(digest[:]))) {
		return false
	}
	iat, exp := time.Unix(claims.Iat, 0).UTC(), time.Unix(claims.Exp, 0).UTC()
	return exp.After(now) && !iat.After(now.Add(allowedClockSkew)) && !now.After(exp.Add(allowedClockSkew)) && exp.Sub(iat) > 0 && exp.Sub(iat) <= maxAssertionAge
}

func validIdentityValue(value string) bool {
	if strings.TrimSpace(value) != value || len(value) == 0 || len(value) > 4096 {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func requestPath(request *http.Request) string {
	path := request.URL.EscapedPath()
	if path == "" {
		path = "/"
	}
	if request.URL.ForceQuery || request.URL.RawQuery != "" {
		return path + "?" + request.URL.RawQuery
	}
	return path
}

func (verifier *Verifier) consume(nonce string, expiry time.Time) bool {
	now := verifier.now().UTC()
	verifier.mu.Lock()
	defer verifier.mu.Unlock()
	for value, expiresAt := range verifier.consumed {
		if !expiresAt.After(now) {
			delete(verifier.consumed, value)
		}
	}
	if _, exists := verifier.consumed[nonce]; exists || len(verifier.consumed) >= verifier.replayCapacity {
		return false
	}
	verifier.consumed[nonce] = expiry
	return true
}
