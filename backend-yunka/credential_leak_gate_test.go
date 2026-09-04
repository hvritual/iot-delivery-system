package backendyunka

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const s00208LeakageGateReport = "../docs/target/S0-02-08-BOOTSTRAP-FAIL-CLOSED-LEAKAGE-GATE.md"

var highConfidenceCredentialPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9_]{20,}\b`),
	regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}\b`),
	regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{20,}\b`),
	regexp.MustCompile(`(?i)\b(?:api[_-]?key|access[_-]?token|secret|password)\s*[:=]\s*["']?[A-Za-z0-9_+/=-]{20,}`),
}

func TestS00208CredentialLeakageGate(t *testing.T) {
	report, err := os.ReadFile(s00208LeakageGateReport)
	if err != nil {
		t.Fatalf("read S0-02-08 leakage gate report: %v", err)
	}
	for _, required := range []string{"扫描边界", "规则", "allowlist", "未覆盖"} {
		if !strings.Contains(string(report), required) {
			t.Fatalf("S0-02-08 leakage gate report is missing %q", required)
		}
	}

	for _, name := range trackedLeakageGateFiles(t) {
		contents, readErr := os.ReadFile(filepath.Join("..", filepath.FromSlash(name)))
		if readErr != nil {
			t.Fatalf("read leakage gate target %s: %v", name, readErr)
		}
		if isLeakageGateBinary(contents) {
			continue
		}
		if scanErr := scanLeakageContents(name, contents); scanErr != nil {
			t.Fatal(scanErr)
		}
	}
}

func scanLeakageContents(name string, contents []byte) error {
	for _, pattern := range highConfidenceCredentialPatterns {
		for _, match := range pattern.FindAll(contents, -1) {
			if leakageGateAllowlisted(name, match) {
				continue
			}
			return fmt.Errorf("possible credential in %s matches %q", name, pattern.String())
		}
	}
	return nil
}

func TestS00208CredentialLeakageGateCoversFirstPartySecuritySurfaces(t *testing.T) {
	files := make(map[string]struct{})
	for _, name := range trackedLeakageGateFiles(t) {
		files[name] = struct{}{}
	}
	for _, required := range []string{
		"README.md",
		"backend/internal/runtime/server.go",
		"backend-yunka/internal/bootstrap/application.go",
		"web/lib/server/session.ts",
		"docs/target/S0-02-04-OIDC-BFF.md",
	} {
		if _, ok := files[required]; !ok {
			t.Fatalf("leakage gate does not scan required first-party surface %q", required)
		}
	}
}

func TestS00208LeakageGateRejectsSecondCredentialAfterAllowlistedFixture(t *testing.T) {
	const allowlistedFixture = "ghp_abcdefghijklmnopqrstuvwxyz123456"
	const allowlistedPath = "backend-yunka/credential_leak_gate_test.go"
	unallowlistedFixture := "ghp_" + strings.Repeat("z", 24)
	if err := scanLeakageContents(allowlistedPath, []byte(allowlistedFixture)); err != nil {
		t.Fatalf("scanner rejected its exact allowlisted fixture: %v", err)
	}
	err := scanLeakageContents(allowlistedPath, []byte(allowlistedFixture+"\n"+unallowlistedFixture))
	if err == nil {
		t.Fatal("scanner accepted an unallowlisted credential after an allowlisted fixture")
	}
	if strings.Contains(err.Error(), unallowlistedFixture) {
		t.Fatalf("scanner error leaked credential fixture: %q", err)
	}
}

func trackedLeakageGateFiles(t *testing.T) []string {
	t.Helper()
	command := exec.Command("git", "-C", "..", "ls-files", "-s", "-z")
	output, err := command.Output()
	if err != nil {
		t.Fatalf("list version-controlled leakage gate files: %v", err)
	}
	files := make([]string, 0)
	for _, entry := range strings.FieldsFunc(string(output), func(r rune) bool { return r == 0 }) {
		metadata, name, ok := strings.Cut(entry, "\t")
		if !ok {
			t.Fatalf("parse version-controlled leakage gate entry %q", entry)
		}
		fields := strings.Fields(metadata)
		if len(fields) != 3 {
			t.Fatalf("parse version-controlled leakage gate metadata %q", metadata)
		}
		if fields[0] == "160000" {
			continue
		}
		files = append(files, name)
	}
	return files
}

func leakageGateAllowlisted(name string, match []byte) bool {
	return name == "backend-yunka/credential_leak_gate_test.go" && string(match) == "ghp_abcdefghijklmnopqrstuvwxyz123456"
}

func isLeakageGateBinary(contents []byte) bool {
	return bytes.IndexByte(contents, 0) >= 0
}

func TestS00208LeakageGateDetectsHighConfidenceCredential(t *testing.T) {
	for _, pattern := range highConfidenceCredentialPatterns {
		if len(pattern.Find([]byte("ghp_abcdefghijklmnopqrstuvwxyz123456"))) > 0 {
			return
		}
	}
	t.Fatal("high-confidence credential patterns must detect a GitHub token-shaped value")
}
