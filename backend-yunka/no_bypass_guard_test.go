package backendyunka

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var guardedMutations = map[string]bool{"Create": true, "CreateProject": true, "CreateRelease": true, "CreateSprint": true, "CreateMilestone": true, "UpdateWorkItem": true, "AddComment": true, "UpdateContext": true, "AdvanceGate": true, "Close": true, "SaveView": true, "Save": true, "SaveProject": true, "SaveRelease": true, "SaveSprint": true, "SaveMilestone": true, "CreateSavedView": true}
var implementationAllowlist = map[string]bool{"internal/delivery/service.go": true, "internal/delivery/repository.go": true, "internal/delivery/sqlite_repository.go": true, "internal/delivery/application/adapter.go": true, "internal/delivery/application/operations.go": true}

func TestProductionWriteCallersUseOperationsBoundary(t *testing.T) {
	var files []string
	if err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(path, ".go") {
			files = append(files, path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var violations []string
	for _, path := range files {
		name := strings.TrimPrefix(filepath.ToSlash(path), "./")
		if excludedProductionFile(name) {
			continue
		}
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		found, err := scanProductionSource(name, source)
		if err != nil {
			t.Fatal(err)
		}
		violations = append(violations, found...)
	}
	if len(violations) > 0 {
		t.Fatalf("production write-boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestProductionWriteBoundaryScannerDetectsRenamedBypasses(t *testing.T) {
	violations, err := scanProductionSource("internal/httpapi/probe.go", []byte(`package probe
import "github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
func RawService() *delivery.Service { return nil }
func bad(repo *delivery.SQLiteRepository) { svc := delivery.NewService(nil,nil); svc.Create(nil, delivery.CreateInput{}); svc.Close(nil,"x"); repo.Save(nil, delivery.WorkItem{}) }`))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"exposes or injects *delivery.Service", "constructs delivery.Service", "calls Create outside Operations", "calls Close outside Operations", "calls Save outside Operations"} {
		if !containsViolation(violations, want) {
			t.Fatalf("scanner violations=%v, missing %q", violations, want)
		}
	}
}

func scanProductionSource(name string, source []byte) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, source, 0)
	if err != nil {
		return nil, err
	}
	var violations []string
	ast.Inspect(file, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.FuncDecl:
			if ast.IsExported(value.Name.Name) && (fieldListUsesDeliveryService(value.Type.Params) || fieldListUsesDeliveryService(value.Type.Results)) {
				violations = append(violations, position(fset, value)+" exposes or injects *delivery.Service")
			}
		case *ast.CallExpr:
			selector, ok := value.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if selector.Sel.Name == "NewService" && selectorReceiverName(selector.X) == "delivery" && name != "internal/bootstrap/application.go" {
				violations = append(violations, position(fset, value)+" constructs delivery.Service outside bootstrap assembly")
			}
			if selector.Sel.Name == "Sync" && name != "internal/obsidian/projection_consumer.go" {
				violations = append(violations, position(fset, value)+" directly calls Sync")
			}
			if guardedMutations[selector.Sel.Name] && !operationsReceiver(selector.X) && ((selector.Sel.Name != "Close" && selector.Sel.Name != "Save") || serviceReceiver(selector.X)) {
				violations = append(violations, position(fset, value)+" calls "+selector.Sel.Name+" outside Operations")
			}
		}
		return true
	})
	return violations, nil
}
func excludedProductionFile(name string) bool {
	return strings.HasSuffix(name, "_test.go") || strings.HasPrefix(name, "contracts/") || implementationAllowlist[name] || strings.Contains(name, "/zz_") || strings.HasSuffix(name, ".pb.go")
}
func fieldListUsesDeliveryService(fields *ast.FieldList) bool {
	if fields == nil {
		return false
	}
	for _, field := range fields.List {
		pointer, ok := field.Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		selector, ok := pointer.X.(*ast.SelectorExpr)
		if ok && selectorReceiverName(selector.X) == "delivery" && selector.Sel.Name == "Service" {
			return true
		}
	}
	return false
}
func position(fset *token.FileSet, node ast.Node) string { return fset.Position(node.Pos()).String() }
func selectorReceiverName(expression ast.Expr) string {
	if value, ok := expression.(*ast.Ident); ok {
		return value.Name
	}
	return ""
}
func containsViolation(values []string, want string) bool {
	for _, value := range values {
		if strings.Contains(value, want) {
			return true
		}
	}
	return false
}
func operationsReceiver(expression ast.Expr) bool {
	switch value := expression.(type) {
	case *ast.Ident:
		return value.Name == "operations"
	case *ast.SelectorExpr:
		return value.Sel.Name == "operations"
	}
	return false
}
func serviceReceiver(expression ast.Expr) bool {
	switch value := expression.(type) {
	case *ast.Ident:
		return value.Name == "svc" || value.Name == "repo" || strings.Contains(strings.ToLower(value.Name), "service")
	case *ast.SelectorExpr:
		return value.Sel.Name == "service"
	}
	return false
}
