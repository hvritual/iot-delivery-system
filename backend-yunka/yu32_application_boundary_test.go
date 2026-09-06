package main

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestYU32ApplicationPackagesDoNotImportGatewayAuthorization(t *testing.T) {
	files, err := filepath.Glob("internal/*/application/*.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no application source files found")
	}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, spec := range file.Imports {
			value, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatalf("decode import in %s: %v", path, err)
			}
			if value == "github.com/hvritual/yunka.io/gateway/authz" || value == "yunka.io/gateway/authz" {
				t.Fatalf("%s directly imports %s; authorization implementation belongs at the root security boundary", path, value)
			}
		}
	}
}
