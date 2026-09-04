package bootstrap

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/hvritual/yunka.io/framework/core/modulecatalog"
)

func TestBootstrapUsesGeneratedCapabilityBinderForAllTransportRegistration(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate runtime binding test source")
	}
	sourcePath := filepath.Join(filepath.Dir(testFile), "application.go")
	source, err := parser.ParseFile(token.NewFileSet(), sourcePath, nil, 0)
	if err != nil {
		t.Fatalf("parse bootstrap source: %v", err)
	}

	var options *ast.CompositeLit
	ast.Inspect(source, func(node ast.Node) bool {
		literal, ok := node.(*ast.CompositeLit)
		if !ok || !isGeneratedBootstrapOptions(literal.Type) {
			return true
		}
		options = literal
		return false
	})
	if options == nil {
		t.Fatal("bootstrap source has no generated Assembly BootstrapOptions")
	}

	fields := keyedFields(options)
	if fields["Factories"] != nil || fields["Executor"] != nil {
		t.Fatal("bootstrap still supplies prebuilt Factories/Executor outside the generated runtime binder")
	}
	binder, ok := fields["BindRuntimeWithCapabilities"].(*ast.FuncLit)
	if !ok {
		t.Fatal("bootstrap does not use generated BindRuntimeWithCapabilities")
	}
	if callsTo(binder, "binder", "Bind") != 1 {
		t.Fatal("generated capability binder does not delegate exactly once to consumer runtime construction")
	}
	if callsTo(source, "httpapi", "Register") != 0 {
		t.Fatal("HTTP compatibility registration must not retain a second post-start path")
	}
	bindingSource, err := parser.ParseFile(token.NewFileSet(), filepath.Join(filepath.Dir(testFile), "runtime_binding.go"), nil, 0)
	if err != nil {
		t.Fatalf("parse runtime binding source: %v", err)
	}
	if callsTo(bindingSource, "httpapi", "Register") != 1 {
		t.Fatal("handwritten HTTP compatibility routes must register exactly once inside the generated binder before App.Start")
	}
}

func TestRuntimeBinderDoesNotRetainRequestContextOrCapabilitySet(t *testing.T) {
	contextType := reflect.TypeOf((*context.Context)(nil)).Elem()
	capabilitySetType := reflect.TypeOf(modulecatalog.EmptyCapabilitySet())
	binderType := reflect.TypeOf(applicationRuntimeBinder{})
	for index := 0; index < binderType.NumField(); index++ {
		field := binderType.Field(index)
		if field.Type.Implements(contextType) || field.Type == capabilitySetType {
			t.Fatalf("runtime binder field %s retains forbidden state %s", field.Name, field.Type)
		}
	}
}

func isGeneratedBootstrapOptions(expression ast.Expr) bool {
	index, ok := expression.(*ast.IndexExpr)
	if ok {
		expression = index.X
	}
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "BootstrapOptions" {
		return false
	}
	identifier, ok := selector.X.(*ast.Ident)
	return ok && identifier.Name == "generatedassembly"
}

func keyedFields(literal *ast.CompositeLit) map[string]ast.Expr {
	fields := make(map[string]ast.Expr)
	for _, element := range literal.Elts {
		field, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		identifier, ok := field.Key.(*ast.Ident)
		if ok {
			fields[identifier.Name] = field.Value
		}
	}
	return fields
}

func callsTo(node ast.Node, packageName, functionName string) int {
	count := 0
	ast.Inspect(node, func(current ast.Node) bool {
		call, ok := current.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != functionName {
			return true
		}
		identifier, ok := selector.X.(*ast.Ident)
		if ok && identifier.Name == packageName {
			count++
		}
		return true
	})
	return count
}
