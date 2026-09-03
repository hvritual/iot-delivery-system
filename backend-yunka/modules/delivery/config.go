package delivery

// Config is populated through the descriptor-declared configuration key.
type Config struct{}

func DefaultConfig() Config    { return Config{} }
func (Config) Validate() error { return nil }
