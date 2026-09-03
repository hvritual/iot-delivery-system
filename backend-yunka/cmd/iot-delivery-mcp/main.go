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
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const mcpAPIKeyEnvironment = "IOT_DELIVERY_MCP_API_KEY"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	authenticator, err := localauth.FromEnvironment()
	if err != nil {
		log.Fatalf("configure local MCP authentication: %v", err)
	}
	apiKey := strings.TrimSpace(os.Getenv(mcpAPIKeyEnvironment))
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv(localauth.APIKeyEnvironment))
	}
	principal, err := authenticator.AuthenticateAPIKey(apiKey)
	if err != nil {
		log.Fatalf("authenticate local MCP process: %v", err)
	}
	application, err := bootstrap.New(ctx, configurationFromEnv())
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

	server := mcpserver.New(application.Operations(), principal)
	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
		log.Printf("local delivery MCP server stopped: %v", err)
	}
}

func configurationFromEnv() bootstrap.Config {
	return bootstrap.Config{
		HTTPAddress:   valueOr("IOT_DELIVERY_MCP_HTTP_ADDR", "127.0.0.1:0"),
		GRPCAddress:   valueOr("IOT_DELIVERY_MCP_GRPC_ADDR", "127.0.0.1:0"),
		DatabasePath:  valueOr("IOT_DELIVERY_YUNKA_DB", "data/iot-delivery-yunka.db"),
		ObsidianVault: valueOr("IOT_DELIVERY_YUNKA_OBSIDIAN_VAULT", "runtime-vault"),
	}
}

func valueOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
