package backendyunka

import (
	"os"
	"os/exec"
	"testing"
)

// The ordinary backend test entry also invokes the architecture gate. It does
// not generate code, reset a baseline, or reinterpret missing tools as success.
func TestArchitectureContract(t *testing.T) {
	command := exec.Command("bash", "../scripts/check-architecture.sh")
	command.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("architecture gate failed: %v\n%s", err, output)
	}
	t.Log(string(output))
}
