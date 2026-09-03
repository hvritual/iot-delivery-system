package runtime

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"

	"yunka.io/framework/core"
)

// Server hosts the delivery API as a Yunka-owned runtime component. The
// application lifecycle, readiness state, and shutdown ordering come from the
// framework; the HTTP handlers remain explicit application dependencies.
type Server struct {
	application *core.App
	httpServer  *http.Server

	mu       sync.RWMutex
	listener net.Listener
	serveErr error
}

func NewServer(address string, handler http.Handler) (*Server, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return nil, errors.New("server address is required")
	}
	if handler == nil {
		handler = http.NotFoundHandler()
	}
	server := &Server{httpServer: &http.Server{Addr: address, Handler: handler}}
	application, err := core.NewApp(core.AppOptions{
		RuntimeInventory: core.RuntimeInventory{Routes: []string{"/health", "/api/dashboard", "/api/items"}},
		RuntimeComponents: []core.RuntimeComponent{{
			Name:         "delivery-http-api",
			StartFunc:    server.startHTTP,
			HealthFunc:   server.healthHTTP,
			ShutdownFunc: server.shutdownHTTP,
		}},
	})
	if err != nil {
		return nil, fmt.Errorf("build Yunka application: %w", err)
	}
	server.application = application
	return server, nil
}

func (server *Server) Start(ctx context.Context) error {
	if server == nil || server.application == nil {
		return errors.New("managed server is not configured")
	}
	return server.application.Start(ctx)
}

func (server *Server) Shutdown(ctx context.Context) error {
	if server == nil || server.application == nil {
		return nil
	}
	return server.application.Shutdown(ctx)
}

func (server *Server) Health(ctx context.Context) core.HealthReport {
	if server == nil || server.application == nil {
		return core.HealthReport{State: "unconfigured", Live: false, Ready: false}
	}
	return server.application.Health(ctx)
}

func (server *Server) Address() string {
	if server == nil {
		return ""
	}
	server.mu.RLock()
	defer server.mu.RUnlock()
	if server.listener == nil {
		return ""
	}
	return server.listener.Addr().String()
}

func (server *Server) startHTTP(_ context.Context) error {
	listener, err := net.Listen("tcp", server.httpServer.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", server.httpServer.Addr, err)
	}
	server.mu.Lock()
	server.listener = listener
	server.serveErr = nil
	server.mu.Unlock()
	go func() {
		if err := server.httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			server.mu.Lock()
			server.serveErr = err
			server.mu.Unlock()
		}
	}()
	return nil
}

func (server *Server) healthHTTP(_ context.Context) error {
	server.mu.RLock()
	defer server.mu.RUnlock()
	if server.listener == nil {
		return errors.New("HTTP listener is not ready")
	}
	return server.serveErr
}

func (server *Server) shutdownHTTP(ctx context.Context) error {
	server.mu.RLock()
	listener := server.listener
	server.mu.RUnlock()
	if listener == nil {
		return nil
	}
	return server.httpServer.Shutdown(ctx)
}
