package bootstrap

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"testing"
)

func TestYU26LocalBFFRegistersOnceInsideSharedRuntimeBinder(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate YU-26 bootstrap test source")
	}
	bindingSource, err := parser.ParseFile(token.NewFileSet(), filepath.Join(filepath.Dir(testFile), "runtime_binding.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if callsTo(bindingSource, "", "registerLocalAuthBFF") != 0 {
		// callsTo only handles selector calls, so a direct consumer helper is
		// checked below with a tiny AST walk in the shared helper test file.
		t.Fatal("unexpected selector registration")
	}
	sourceText, err := parser.ParseFile(token.NewFileSet(), filepath.Join(filepath.Dir(testFile), "runtime_binding.go"), nil, parser.SkipObjectResolution)
	if err != nil || sourceText == nil {
		t.Fatalf("parse runtime binding: %v", err)
	}
	count := directCallsTo(sourceText, "registerLocalAuthBFF")
	if count != 1 {
		t.Fatalf("local BFF registration count=%d, want 1", count)
	}
}
