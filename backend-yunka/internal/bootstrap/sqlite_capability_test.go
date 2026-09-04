package bootstrap

import (
	"strings"
	"testing"

	generatedassembly "github.com/hvritual/iot-delivery-system/backend-yunka/internal/assembly"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	deliveryapplication "github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/application"
	"github.com/hvritual/yunka.io/framework/core/modulecatalog"
)

func TestGeneratedAssemblyRejectsMissingSQLiteTransactionCapability(t *testing.T) {
	adapter := deliveryapplication.NewAdapter(delivery.NewService(delivery.NewMemoryRepository(), nil))

	_, err := generatedassembly.BuildApplicationsWithCapabilities(
		applicationFactories{deliveryManagement: adapter},
		nil,
		modulecatalog.EmptyCapabilitySet(),
	)
	if err == nil {
		t.Fatal("generated assembly accepted missing SQLite transaction capability")
	}
	if !strings.Contains(err.Error(), "capability sqlite.transaction-factory") {
		t.Fatalf("generated assembly error = %q, want missing SQLite capability context", err)
	}
}

func TestApplicationFactoryRejectsMissingSQLiteTransactionCapability(t *testing.T) {
	adapter := deliveryapplication.NewAdapter(delivery.NewService(delivery.NewMemoryRepository(), nil))

	application, err := (applicationFactories{deliveryManagement: adapter}).BuildDeliveryManagement(
		generatedassembly.DeliveryManagementDependencies{},
	)
	if err == nil {
		t.Fatalf("application factory accepted missing SQLite transaction capability: application=%T", application)
	}
}
