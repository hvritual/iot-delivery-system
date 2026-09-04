package autoload

import (
	module "github.com/hvritual/iot-delivery-system/backend-yunka/modules/delivery"
	"yunka.io/framework/core/modulecatalog"
)

func init() { modulecatalog.MustRegister(module.GeneratedDescriptor()) }
