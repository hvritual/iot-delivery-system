package bootstrap

import (
	"go/ast"
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
	source, err := parser.ParseFile(token.NewFileSet(), filepath.Join(filepath.Dir(testFile), "runtime_binding.go"), nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse runtime binding: %v", err)
	}
	if count := directCallsTo(source, "registerLocalAuthBFF"); count != 1 {
		t.Fatalf("local BFF registration count=%d, want 1", count)
	}
}

func directCallsTo(node ast.Node, name string) int {
	count := 0
	ast.Inspect(node, func(current ast.Node) bool {
		call, ok := current.(*ast.CallExpr)
		if !ok {
			return true
		}
		identifier, ok := call.Fun.(*ast.Ident)
		if ok && identifier.Name == name {
			count++
		}
		return true
	})
	return count
}
