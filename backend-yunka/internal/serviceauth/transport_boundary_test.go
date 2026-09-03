package serviceauth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServiceCredentialManagementHasNoRemoteWriteContract(t *testing.T) {
	proto, err := os.ReadFile(filepath.Join("..", "..", "contracts", "proto", "iot_delivery.proto"))
	if err != nil {
		t.Fatalf("read canonical delivery contract: %v", err)
	}
	plans, err := os.ReadFile(filepath.Join("..", "..", "contracts", "generated", "operation-plans.json"))
	if err != nil {
		t.Fatalf("read generated operation plans: %v", err)
	}
	for _, forbidden := range []string{"IssueServiceCredential", "RotateServiceCredential", "RevokeServiceCredential", "service.credentials."} {
		if strings.Contains(string(proto), forbidden) || strings.Contains(string(plans), forbidden) {
			t.Fatalf("S0-02-07 must not expose service credential management through a remote contract: found %q", forbidden)
		}
	}
}

func TestServiceCredentialImplementationDoesNotAddHTTPManagementTransport(t *testing.T) {
	for _, name := range []string{"serviceauth.go", "grpc.go"} {
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read service credential source %s: %v", name, err)
		}
		for _, forbidden := range []string{"net/http", "http.Handle", "http.Handler"} {
			if strings.Contains(string(source), forbidden) {
				t.Fatalf("service credential source %s must not expose HTTP management transport %q", name, forbidden)
			}
		}
	}
	grpcSource, err := os.ReadFile("grpc.go")
	if err != nil {
		t.Fatalf("read gRPC service credential adapter: %v", err)
	}
	for _, required := range []string{"AuthenticatedUnaryServerInterceptor", "ServiceAuthorizationMetadata", "CredentialVerifier"} {
		if !strings.Contains(string(grpcSource), required) {
			t.Fatalf("service credential gRPC adapter must reuse Yunka %q", required)
		}
	}
	if strings.Contains(string(grpcSource), "x-iot-service-credential") {
		t.Fatal("service credential gRPC adapter must not invent a parallel metadata header")
	}
}
