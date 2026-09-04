package bootstrap_test

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/bootstrap"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localauth"
	"github.com/hvritual/yunka.io/framework/core"
)

func TestApplicationUsesPlatformCapabilityHealthAndCompleteHostedInventory(t *testing.T) {
	t.Setenv(localauth.APIKeyEnvironment, "runtime-binding-diagnostics-key")
	application, err := bootstrap.New(t.Context(), bootstrap.Config{
		HTTPAddress:        "127.0.0.1:0",
		GRPCAddress:        "127.0.0.1:0",
		DatabasePath:       filepath.Join(t.TempDir(), "runtime-binding.db"),
		ObsidianVault:      t.TempDir(),
		RuntimeEnvironment: bootstrap.RuntimeEnvironmentDevelopment,
	})
	if err != nil {
		t.Fatalf("bootstrap capability-bound application: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := application.Close(ctx); err != nil {
			t.Errorf("close capability-bound application: %v", err)
		}
	})

	response := get(t, "http://"+application.HTTPAddress()+"/__yunka/diagnostics")
	var report struct {
		Core core.DiagnosticsReport `json:"core"`
	}
	if err := json.Unmarshal([]byte(response.Body), &report); err != nil {
		t.Fatalf("decode diagnostics: %v", err)
	}
	checks := make(map[string]core.HealthStatus, len(report.Core.Health.Checks))
	for _, check := range report.Core.Health.Checks {
		checks[check.Name] = check.Status
	}
	for _, name := range []string{
		"composition.capabilities",
		"module.delivery-event-runtime",
		"runtime.grpc-server",
		"runtime.http-server",
	} {
		if checks[name] != core.HealthStatusHealthy {
			t.Fatalf("diagnostics health checks = %#v, want healthy %q", checks, name)
		}
	}
	if report.Core.State != "ready" || !report.Core.Health.Ready || report.Core.Runtime.RPCServerCount != 1 {
		t.Fatalf("diagnostics core = %#v, want ready App with one RPC server", report.Core)
	}
	componentNames := make([]string, 0, len(report.Core.Components))
	for _, component := range report.Core.Components {
		componentNames = append(componentNames, component.Name)
		if !component.Startable || !component.Shutdownable || !component.HealthChecked {
			t.Fatalf("host component = %#v, want complete lifecycle", component)
		}
	}
	if want := []string{"grpc-server", "http-server"}; !reflect.DeepEqual(componentNames, want) {
		t.Fatalf("host components = %#v, want %#v", componentNames, want)
	}
}

func TestApplicationCloseReleasesHostedListenersAndIsIdempotent(t *testing.T) {
	t.Setenv(localauth.APIKeyEnvironment, "runtime-close-key")
	application, err := bootstrap.New(t.Context(), bootstrap.Config{
		HTTPAddress:        "127.0.0.1:0",
		GRPCAddress:        "127.0.0.1:0",
		DatabasePath:       filepath.Join(t.TempDir(), "runtime-close.db"),
		ObsidianVault:      t.TempDir(),
		RuntimeEnvironment: bootstrap.RuntimeEnvironmentDevelopment,
	})
	if err != nil {
		t.Fatalf("bootstrap hosted application: %v", err)
	}
	httpAddress, grpcAddress := application.HTTPAddress(), application.GRPCAddress()
	for index := 0; index < 2; index++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := application.Close(ctx)
		cancel()
		if err != nil {
			t.Fatalf("close hosted application attempt %d: %v", index+1, err)
		}
	}
	for _, address := range []string{httpAddress, grpcAddress} {
		listener, err := net.Listen("tcp", address)
		if err != nil {
			t.Fatalf("hosted listener %s was not released: %v", address, err)
		}
		if err := listener.Close(); err != nil {
			t.Fatalf("close rebound listener %s: %v", address, err)
		}
	}
}

func TestRuntimeBinderFailureClosesPreparedResourcesBeforeServing(t *testing.T) {
	t.Setenv(localauth.APIKeyEnvironment, "runtime-bind-failure-key")
	httpAddress, grpcAddress := freeLoopbackAddress(t), freeLoopbackAddress(t)
	application, err := bootstrap.New(t.Context(), bootstrap.Config{
		HTTPAddress:        httpAddress,
		GRPCAddress:        grpcAddress,
		DatabasePath:       filepath.Join(t.TempDir(), "runtime-bind-failure.db"),
		ObsidianVault:      t.TempDir(),
		RuntimeEnvironment: bootstrap.RuntimeEnvironmentDevelopment,
		DueReminder:        delivery.DueReminderConfig{LeadDays: -1},
	})
	if err == nil || application != nil {
		if application != nil {
			_ = application.Close(t.Context())
		}
		t.Fatalf("runtime binder accepted invalid reminder configuration: application=%T err=%v", application, err)
	}
	for _, address := range []string{httpAddress, grpcAddress} {
		listener, listenErr := net.Listen("tcp", address)
		if listenErr != nil {
			t.Fatalf("failed bootstrap retained listener %s: %v", address, listenErr)
		}
		if closeErr := listener.Close(); closeErr != nil {
			t.Fatalf("close rebound listener %s: %v", address, closeErr)
		}
	}
}
