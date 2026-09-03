package delivery

const ModuleName = "delivery"

type Module struct {
	dependencies Dependencies
}

func NewModule(dependencies Dependencies) (*Module, error) {
	return &Module{dependencies: dependencies}, nil
}

func (*Module) Name() string { return ModuleName }
