package backendyunka

import (
	"os"
	"testing"
)

// The project profile and bootstrap are the minimum reproducible Yunka
// scaffold contract. Business code is intentionally absent at this RED stage.
func TestYunkaScaffoldMaterializesMVPProject(t *testing.T) {
	t.Parallel()

	for _, relativePath := range []string{
		".yunka/project.json",
		".yunka/dev.json",
		"contracts/proto/yunka_bootstrap.proto",
		"cmd/yunka-bootstrap/main.go",
	} {
		if _, err := os.Stat(relativePath); err != nil {
			t.Fatalf("Yunka scaffold must create %s: %v", relativePath, err)
		}
	}
}
