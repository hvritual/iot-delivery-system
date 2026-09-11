package serviceauth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/hvritual/iot-delivery-system/backend-yunka/contracts/delivery/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

func TestServiceCredentialManagementHasNoRemoteWriteContract(t *testing.T) {
	descriptor, err := protoregistry.GlobalFiles.FindDescriptorByName("iot.delivery.v1.DeliveryService")
	if err != nil {
		t.Fatalf("find canonical DeliveryService descriptor: %v", err)
	}
	service, ok := descriptor.(protoreflect.ServiceDescriptor)
	if !ok {
		t.Fatalf("DeliveryService descriptor is %T, want protoreflect.ServiceDescriptor", descriptor)
	}
	for _, forbiddenMethod := range []protoreflect.Name{"IssueServiceCredential", "RotateServiceCredential", "RevokeServiceCredential"} {
		if method := service.Methods().ByName(forbiddenMethod); method != nil {
			t.Fatalf("S0-02-07 must not expose service credential management through a remote contract: found RPC %q", forbiddenMethod)
		}
	}

	plans, err := os.ReadFile(filepath.Join("..", "..", "contracts", "generated", "operation-plans.json"))
	if err != nil {
		t.Fatalf("read generated operation plans: %v", err)
	}
	if strings.Contains(string(plans), "service.credentials.") {
		t.Fatal("S0-02-07 must not expose service credential management through a canonical operation ID")
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
