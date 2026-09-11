package backendyunka

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// The project profile, configured contract source root, dev manifest and
// bootstrap executable are the minimum reproducible Yunka scaffold contract.
// A bootstrap proto is only an initialization placeholder and is deliberately
// not a permanent project invariant once real business contracts exist.
func TestYunkaScaffoldMaterializesMVPProject(t *testing.T) {
	t.Parallel()

	for _, relativePath := range []string{
		".yunka/project.json",
		".yunka/dev.json",
		"cmd/yunka-bootstrap/main.go",
	} {
		if _, err := os.Stat(relativePath); err != nil {
			t.Fatalf("Yunka scaffold must contain %s: %v", relativePath, err)
		}
	}

	profileBytes, err := os.ReadFile(".yunka/project.json")
	if err != nil {
		t.Fatalf("read Yunka project profile: %v", err)
	}
	var profile struct {
		Workflow struct {
			Contract struct {
				ProtoRoot string `json:"protoRoot"`
			} `json:"contract"`
		} `json:"workflow"`
	}
	if err := json.Unmarshal(profileBytes, &profile); err != nil {
		t.Fatalf("decode Yunka project profile: %v", err)
	}
	if profile.Workflow.Contract.ProtoRoot == "" {
		t.Fatal("Yunka project profile must declare workflow.contract.protoRoot")
	}

	protoFiles := 0
	if err := filepath.WalkDir(profile.Workflow.Contract.ProtoRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && filepath.Ext(path) == ".proto" {
			protoFiles++
		}
		return nil
	}); err != nil {
		t.Fatalf("walk configured proto root %q: %v", profile.Workflow.Contract.ProtoRoot, err)
	}
	if protoFiles == 0 {
		t.Fatalf("configured proto root %q contains no protobuf contracts", profile.Workflow.Contract.ProtoRoot)
	}
}
