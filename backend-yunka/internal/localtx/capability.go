package localtx

import (
	"errors"

	"github.com/hvritual/yunka.io/framework/core/modulecatalog"
	"github.com/hvritual/yunka.io/framework/execution"
)

const (
	CapabilityName = "sqlite.transaction-factory"
	providerName   = "sqlite-transaction"
)

// Factory is the typed local SQLite UnitOfWork factory contract exported to
// generated Application construction. The root Executor remains the sole
// transaction owner; Applications receive this process-scoped value only as a
// constructor dependency and never as a runtime locator.
type Factory interface {
	execution.TransactionFactory
}

var TransactionFactoryCapability = modulecatalog.MustCapabilityKey[Factory](
	CapabilityName,
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localtx",
	"Factory",
)

// CapabilityDescriptor composes the handwritten Provides declaration that the
// current module manifest schema cannot generate. The descriptor captures an
// already configured factory and performs no I/O or runtime discovery.
func CapabilityDescriptor(factory Factory) (modulecatalog.Descriptor, error) {
	if factory == nil {
		return modulecatalog.Descriptor{}, errors.New("SQLite transaction factory capability is required")
	}
	return modulecatalog.Descriptor{
		Name:     providerName,
		Version:  "v0.1.0",
		Provides: []modulecatalog.CapabilityContract{TransactionFactoryCapability.Contract()},
		Build: func(modulecatalog.BuildContext) (modulecatalog.Instance, error) {
			return &capabilityProvider{factory: factory}, nil
		},
	}, nil
}

type capabilityProvider struct {
	factory Factory
}

func (*capabilityProvider) Name() string { return providerName }

func (provider *capabilityProvider) ExportCapabilities() []modulecatalog.CapabilityExport {
	return []modulecatalog.CapabilityExport{
		TransactionFactoryCapability.Export(provider.factory),
	}
}
