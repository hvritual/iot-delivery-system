package runtime_test

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/hvritual/iot-delivery-system/backend/internal/runtime"
)

func TestServerIsManagedByYunkaLifecycle(t *testing.T) {
	t.Parallel()

	server, err := runtime.NewServer("127.0.0.1:0", http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/health" {
			http.NotFound(writer, request)
			return
		}
		_, _ = writer.Write([]byte("ok"))
	}))
	if err != nil {
		t.Fatalf("construct managed server: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := server.Start(ctx); err != nil {
		t.Fatalf("start managed server: %v", err)
	}
	t.Cleanup(func() { _ = server.Shutdown(context.Background()) })
	if !server.Health(ctx).Ready {
		t.Fatal("server is not ready after lifecycle start")
	}

	response, err := (&http.Client{Timeout: time.Second}).Get("http://" + server.Address() + "/health")
	if err != nil {
		t.Fatalf("request managed server: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if response.StatusCode != http.StatusOK || string(body) != "ok" {
		t.Fatalf("health response = %d %q, want 200 ok", response.StatusCode, body)
	}

	if err := server.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown managed server: %v", err)
	}
	if server.Health(ctx).Ready {
		t.Fatal("server remains ready after lifecycle shutdown")
	}
}
