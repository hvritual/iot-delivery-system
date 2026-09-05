package localbffhttp

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"testing"
)

func TestYU26BFFAdapterDoesNotWriteSQLiteDirectly(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate local BFF source")
	}
	source, err := parser.ParseFile(token.NewFileSet(), filepath.Join(filepath.Dir(testFile), "handler.go"), nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse local BFF source: %v", err)
	}
	for _, imported := range source.Imports {
		if imported.Path != nil && imported.Path.Value == `"database/sql"` {
			t.Fatal("local BFF adapter must not import database/sql")
		}
	}
	for _, forbidden := range []string{"Exec", "ExecContext", "Query", "QueryContext", "QueryRow", "QueryRowContext"} {
		if countSelectorCalls(source, forbidden) != 0 {
			t.Fatalf("local BFF adapter contains forbidden persistence call %s", forbidden)
		}
	}
	for _, required := range []string{"Login", "CurrentMemberFromSessionToken", "IssueAccessTokenFromSession", "Logout", "ChangePassword", "Create", "Disable", "ResetCredential", "Assign", "Revoke"} {
		if countSelectorCalls(source, required) == 0 {
			t.Fatalf("local BFF adapter does not delegate through required application port %s", required)
		}
	}
}

func countSelectorCalls(node ast.Node, name string) int {
	count := 0
	ast.Inspect(node, func(current ast.Node) bool {
		call, ok := current.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if ok && selector.Sel.Name == name {
			count++
		}
		return true
	})
	return count
}
