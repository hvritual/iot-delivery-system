package main

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/bootstrap"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localauth"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localtransportauth"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/mcpserver"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	mcpAPIKeyEnvironment       = "IOT_DELIVERY_MCP_API_KEY"
	mcpAccessTokenEnvironment  = "IOT_DELIVERY_MCP_ACCESS_TOKEN"
	mcpSessionTokenEnvironment = "IOT_DELIVERY_MCP_SESSION_TOKEN"
	localAuthKeyEnvironment    = "IOT_DELIVERY_LOCAL_AUTH_JWT_KEY"
)

type mcpCredentialKind string

const (
	mcpCredentialAPIKey  mcpCredentialKind = "api-key"
	mcpCredentialAccess  mcpCredentialKind = "access-token"
	mcpCredentialSession mcpCredentialKind = "session-token"
)

type mcpCredential struct {
	kind  mcpCredentialKind
	value string
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	configuration, err := configurationFromEnv()
	if err != nil {
		log.Fatalf("configure local delivery MCP runtime: %v", err)
	}
	credential, err := mcpCredentialFromEnv()
	if err != nil {
		log.Fatalf("configure local MCP authentication: %v", err)
	}
	application, err := bootstrap.New(ctx, configuration)
	if err != nil {
		log.Fatalf("configure local delivery MCP runtime: %v", err)
	}
	defer func() {
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := application.Close(shutdownContext); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("shutdown local delivery MCP runtime: %v", err)
		}
	}()

	server, err := configuredMCPServer(application, credential)
	if err != nil {
		log.Fatalf("configure local MCP principal: %v", err)
	}
	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
		log.Printf("local delivery MCP server stopped: %v", err)
	}
}

func configuredMCPServer(application *bootstrap.Application, credential mcpCredential) (*mcp.Server, error) {
	if application == nil || application.Operations() == nil {
		return nil, errors.New("local MCP application is not configured")
	}
	switch credential.kind {
	case mcpCredentialAccess, mcpCredentialSession:
		verifier, err := localtransportauth.New(application.LocalAuthentication(), nil)
		if err != nil {
			return nil, errors.New("local member authentication is not configured")
		}
		resolver := func(ctx context.Context) (identity.Principal, error) {
			if credential.kind == mcpCredentialAccess {
				return verifier.VerifyAccessToken(ctx, credential.value)
			}
			return verifier.VerifySessionToken(ctx, credential.value)
		}
		return mcpserver.NewWithPrincipalResolver(application.Operations(), resolver), nil
	case mcpCredentialAPIKey:
		authenticator, err := localauth.FromEnvironment()
		if err != nil {
			return nil, errors.New("development API-key authentication is not configured")
		}
		principal, err := authenticator.AuthenticateAPIKey(credential.value)
		if err != nil {
			return nil, errors.New("development API-key authentication failed")
		}
		return mcpserver.New(application.Operations(), principal), nil
	default:
		return nil, errors.New("local MCP credential mode is invalid")
	}
}

func configurationFromEnv() (bootstrap.Config, error) {
	startupPolicy, err := bootstrap.StartupPolicyFromEnvironment(os.Getenv)
	if err != nil {
		return bootstrap.Config{}, err
	}
	if err := startupPolicy.ValidateLocalStdio(); err != nil {
		return bootstrap.Config{}, err
	}
	credential, credentialErr := mcpCredentialFromEnv()
	if credentialErr != nil {
		return bootstrap.Config{}, credentialErr
	}
	localAuthKey := strings.TrimSpace(os.Getenv(localAuthKeyEnvironment))
	if (credential.kind == mcpCredentialAccess || credential.kind == mcpCredentialSession) && localAuthKey == "" {
		return bootstrap.Config{}, errors.New("local member MCP requires the local auth JWT signing key")
	}
	return startupPolicy.Apply(bootstrap.Config{
		HTTPAddress:            valueOr("IOT_DELIVERY_MCP_HTTP_ADDR", "127.0.0.1:0"),
		GRPCAddress:            valueOr("IOT_DELIVERY_MCP_GRPC_ADDR", "127.0.0.1:0"),
		DatabasePath:           valueOr("IOT_DELIVERY_YUNKA_DB", "data/iot-delivery-yunka.db"),
		ObsidianVault:          valueOr("IOT_DELIVERY_YUNKA_OBSIDIAN_VAULT", "runtime-vault"),
		BFFOrganizationID:      strings.TrimSpace(os.Getenv("IOT_DELIVERY_BFF_ORGANIZATION_ID")),
		BFFAssertionKey:        strings.TrimSpace(os.Getenv("IOT_DELIVERY_BFF_ASSERTION_KEY")),
		LocalAuthJWTSigningKey: localAuthKey,
	}), nil
}

func mcpCredentialFromEnv() (mcpCredential, error) {
	access := strings.TrimSpace(os.Getenv(mcpAccessTokenEnvironment))
	session := strings.TrimSpace(os.Getenv(mcpSessionTokenEnvironment))
	explicitAPIKey := strings.TrimSpace(os.Getenv(mcpAPIKeyEnvironment))
	configured := 0
	if access != "" {
		configured++
	}
	if session != "" {
		configured++
	}
	if explicitAPIKey != "" {
		configured++
	}
	if configured > 1 {
		return mcpCredential{}, errors.New("exactly one MCP credential family must be configured")
	}
	if access != "" {
		return mcpCredential{kind: mcpCredentialAccess, value: access}, nil
	}
	if session != "" {
		return mcpCredential{kind: mcpCredentialSession, value: session}, nil
	}
	if explicitAPIKey != "" {
		return mcpCredential{kind: mcpCredentialAPIKey, value: explicitAPIKey}, nil
	}
	legacyAPIKey := strings.TrimSpace(os.Getenv(localauth.APIKeyEnvironment))
	if legacyAPIKey == "" {
		return mcpCredential{}, errors.New("exactly one MCP credential family must be configured")
	}
	return mcpCredential{kind: mcpCredentialAPIKey, value: legacyAPIKey}, nil
}

func valueOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
