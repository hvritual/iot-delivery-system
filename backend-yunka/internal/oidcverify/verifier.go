// Package oidcverify verifies externally issued OIDC ID Tokens at the trust
// boundary and exposes only the normalized identity snapshots needed by a
// later binding layer.
package oidcverify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

const (
	discoveryPath      = "/.well-known/openid-configuration"
	httpRequestTimeout = 5 * time.Second
)

// Config declares exactly one trusted OIDC issuer and audience.
//
// Production configuration accepts only HTTPS issuers and uses their standard
// discovery location. The two ForTests fields exist solely for hermetic tests
// and require an explicit insecure-test opt-in.
type Config struct {
	Issuer   string
	Audience string

	DiscoveryURLForTests      string
	AllowInsecureHTTPForTests bool
	Now                       func() time.Time
}

// VerifiedClaims is the intentionally small, normalized result of a verified
// ID Token. It never retains the raw token or arbitrary provider claims.
type VerifiedClaims struct {
	Issuer      string
	Subject     string
	Email       string
	DisplayName string
}

// Verifier verifies ID Tokens for one configured issuer and audience.
type Verifier struct {
	verifier   *oidc.IDTokenVerifier
	httpClient *http.Client
}

// New constructs a fail-closed verifier after retrieving and checking the
// configured issuer's discovery document.
func New(ctx context.Context, config Config) (*Verifier, error) {
	_, err := validateIssuer(config.Issuer, config.AllowInsecureHTTPForTests)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(config.Audience) == "" || config.Audience != strings.TrimSpace(config.Audience) {
		return nil, errors.New("oidc audience is required and must not contain surrounding whitespace")
	}

	discoveryURL, err := configuredDiscoveryURL(config)
	if err != nil {
		return nil, err
	}
	httpClient := failClosedHTTPClient()
	metadata, err := fetchProviderMetadata(ctx, httpClient, discoveryURL)
	if err != nil {
		return nil, err
	}
	if metadata.Issuer != config.Issuer {
		return nil, errors.New("oidc discovery issuer does not exactly match configured issuer")
	}
	if _, err := validateEndpoint(metadata.JWKSURI, config.AllowInsecureHTTPForTests); err != nil {
		return nil, fmt.Errorf("oidc jwks URI: %w", err)
	}

	keySet := oidc.NewRemoteKeySet(oidc.ClientContext(ctx, httpClient), metadata.JWKSURI)
	return &Verifier{verifier: oidc.NewVerifier(config.Issuer, keySet, &oidc.Config{
		ClientID:             config.Audience,
		SupportedSigningAlgs: []string{oidc.RS256},
		Now:                  config.Now,
	}), httpClient: httpClient}, nil
}

// Verify validates an ID Token's signature, issuer, audience, and expiry, and
// returns only the canonical claims permitted to leave this package.
func (verifier *Verifier) Verify(ctx context.Context, rawIDToken string) (VerifiedClaims, error) {
	if verifier == nil || verifier.verifier == nil {
		return VerifiedClaims{}, errors.New("oidc verifier is not configured")
	}
	token, err := verifier.verifier.Verify(oidc.ClientContext(ctx, verifier.httpClient), rawIDToken)
	if err != nil {
		return VerifiedClaims{}, fmt.Errorf("verify OIDC ID Token: %w", err)
	}
	if token.Subject == "" {
		return VerifiedClaims{}, errors.New("verified OIDC ID Token is missing subject")
	}
	var snapshot struct {
		Email       string `json:"email"`
		DisplayName string `json:"name"`
	}
	if err := token.Claims(&snapshot); err != nil {
		return VerifiedClaims{}, fmt.Errorf("decode verified OIDC ID Token snapshot: %w", err)
	}
	return VerifiedClaims{
		Issuer:      token.Issuer,
		Subject:     token.Subject,
		Email:       snapshot.Email,
		DisplayName: snapshot.DisplayName,
	}, nil
}

type providerMetadata struct {
	Issuer  string `json:"issuer"`
	JWKSURI string `json:"jwks_uri"`
}

func configuredDiscoveryURL(config Config) (string, error) {
	if config.DiscoveryURLForTests != "" {
		if !config.AllowInsecureHTTPForTests {
			return "", errors.New("oidc test discovery URL requires explicit insecure-test opt-in")
		}
		if _, err := validateEndpoint(config.DiscoveryURLForTests, true); err != nil {
			return "", fmt.Errorf("oidc test discovery URL: %w", err)
		}
		return config.DiscoveryURLForTests, nil
	}

	return strings.TrimSuffix(config.Issuer, "/") + discoveryPath, nil
}

func fetchProviderMetadata(ctx context.Context, httpClient *http.Client, discoveryURL string) (providerMetadata, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return providerMetadata{}, fmt.Errorf("create OIDC discovery request: %w", err)
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return providerMetadata{}, fmt.Errorf("retrieve OIDC discovery document: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return providerMetadata{}, fmt.Errorf("retrieve OIDC discovery document: unexpected HTTP status %d", response.StatusCode)
	}
	var metadata providerMetadata
	if err := json.NewDecoder(response.Body).Decode(&metadata); err != nil {
		return providerMetadata{}, fmt.Errorf("decode OIDC discovery document: %w", err)
	}
	if metadata.Issuer == "" || metadata.JWKSURI == "" {
		return providerMetadata{}, errors.New("OIDC discovery document is missing issuer or jwks_uri")
	}
	return metadata, nil
}

func failClosedHTTPClient() *http.Client {
	return &http.Client{
		Timeout: httpRequestTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("OIDC metadata and JWKS redirects are not allowed")
		},
	}
}

func validateIssuer(value string, allowInsecureHTTPForTests bool) (*url.URL, error) {
	if strings.TrimSpace(value) == "" || value != strings.TrimSpace(value) {
		return nil, errors.New("oidc issuer is required and must not contain surrounding whitespace")
	}
	issuerURL, err := url.Parse(value)
	if err != nil || issuerURL.Host == "" || issuerURL.User != nil || issuerURL.RawQuery != "" || issuerURL.Fragment != "" {
		return nil, errors.New("oidc issuer must be an absolute URL without query or fragment")
	}
	if issuerURL.Scheme == "https" {
		return issuerURL, nil
	}
	if issuerURL.Scheme == "http" && allowInsecureHTTPForTests && isLoopbackHost(issuerURL.Hostname()) {
		return issuerURL, nil
	}
	return nil, errors.New("oidc issuer must use HTTPS outside the explicit test boundary")
}

func validateEndpoint(value string, allowInsecureHTTPForTests bool) (*url.URL, error) {
	endpoint, err := url.Parse(value)
	if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.Fragment != "" {
		return nil, errors.New("must be an absolute URL without userinfo or fragment")
	}
	if endpoint.Scheme == "https" || (endpoint.Scheme == "http" && allowInsecureHTTPForTests && isLoopbackHost(endpoint.Hostname())) {
		return endpoint, nil
	}
	return nil, errors.New("must use HTTPS outside the explicit test boundary")
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}
