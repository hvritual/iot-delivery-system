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
var implementationAllowlist = map[string]bool{"internal/delivery/service.go": true, "internal/delivery/transactional_service.go": true, "internal/delivery/repository.go": true, "internal/delivery/sqlite_repository.go": true, "internal/delivery/application/adapter.go": true, "internal/delivery/application/audited.go": true, "internal/delivery/application/operations.go": true}
var bootstrapServiceConstructionAllowlist = map[string]bool{"internal/bootstrap/runtime_binding.go": true}

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

func TestDeliveryServiceConstructionIsConfinedToRuntimeBinder(t *testing.T) {
	source := []byte(`package bootstrap
import "github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
func construct() { _ = delivery.NewService(nil, nil) }`)
	allowed, err := scanProductionSource("internal/bootstrap/runtime_binding.go", source)
	if err != nil {
		t.Fatal(err)
	}
	if containsViolation(allowed, "constructs delivery.Service") {
		t.Fatalf("runtime binder was rejected as bootstrap assembly: %v", allowed)
	}
	legacy, err := scanProductionSource("internal/bootstrap/application.go", source)
	if err != nil {
		t.Fatal(err)
	}
	if !containsViolation(legacy, "constructs delivery.Service outside bootstrap assembly") {
		t.Fatalf("legacy prebuilt bootstrap path was not rejected: %v", legacy)
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
			if (selector.Sel.Name == "NewService" || selector.Sel.Name == "NewRootTransactionalService") && selectorReceiverName(selector.X) == "delivery" && !bootstrapServiceConstructionAllowlist[name] {
				violations = append(violations, position(fset, value)+" constructs delivery.Service outside bootstrap assembly")
			}
			if selector.Sel.Name == "Sync" && name != "internal/obsidian/projection_consumer.go" {
				violations = append(violations, position(fset, value)+" directly calls Sync")
			}
			if guardedMutations[selector.Sel.Name] && !trustedMemberCreate(file, value) && !operationsReceiver(selector.X) && ((selector.Sel.Name != "Close" && selector.Sel.Name != "Save") || serviceReceiver(selector.X)) {
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

// Create also names the member-admin application port. Accept that port only
// when the AST proves the canonical import, input type and concrete receiver
// declaration; an identifier named "members" is not an authorization seam.
func trustedMemberCreate(file *ast.File, call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Create" || len(call.Args) != 2 {
		return false
	}
	input, ok := call.Args[1].(*ast.CompositeLit)
	if !ok || !canonicalImportedType(file, input.Type, "internal/localmemberadmin", "CreateInput") {
		return false
	}
	if access, ok := selector.X.(*ast.SelectorExpr); ok {
		receiver, ok := access.X.(*ast.Ident)
		if !ok || receiver.Obj == nil {
			return false
		}
		declaration, ok := receiver.Obj.Decl.(*ast.Field)
		if !ok {
			return false
		}
		pointer, ok := declaration.Type.(*ast.StarExpr)
		if !ok {
			return false
		}
		structName, ok := pointer.X.(*ast.Ident)
		if !ok || structName.Obj == nil {
			return false
		}
		typeSpec, ok := structName.Obj.Decl.(*ast.TypeSpec)
		if !ok {
			return false
		}
		structure, ok := typeSpec.Type.(*ast.StructType)
		if !ok {
			return false
		}
		for _, field := range structure.Fields.List {
			for _, name := range field.Names {
				if name.Name == access.Sel.Name {
					fieldPointer, ok := field.Type.(*ast.StarExpr)
					return ok && canonicalImportedType(file, fieldPointer.X, "internal/localmemberadmin", "Manager")
				}
			}
		}
	}
	// The E2E fixture obtains the exact application port from bootstrap.New.
	if accessor, ok := selector.X.(*ast.CallExpr); ok && len(accessor.Args) == 0 {
		method, ok := accessor.Fun.(*ast.SelectorExpr)
		if !ok || method.Sel.Name != "MemberAdministration" {
			return false
		}
		application, ok := method.X.(*ast.Ident)
		if !ok || application.Obj == nil {
			return false
		}
		assignment, ok := application.Obj.Decl.(*ast.AssignStmt)
		if !ok || len(assignment.Rhs) != 1 || len(assignment.Lhs) == 0 {
			return false
		}
		first, ok := assignment.Lhs[0].(*ast.Ident)
		if !ok || first.Name != application.Name {
			return false
		}
		constructor, ok := assignment.Rhs[0].(*ast.CallExpr)
		return ok && canonicalImportedType(file, constructor.Fun, "internal/bootstrap", "New")
	}
	return false
}

func canonicalImportedType(file *ast.File, expression ast.Expr, suffix, typeName string) bool {
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != typeName {
		return false
	}
	alias, ok := selector.X.(*ast.Ident)
	if !ok {
		return false
	}
	for _, imported := range file.Imports {
		if imported.Path.Value != `"github.com/hvritual/iot-delivery-system/backend-yunka/`+suffix+`"` {
			continue
		}
		name := filepath.Base(suffix)
		if imported.Name != nil {
			name = imported.Name.Name
		}
		return alias.Name == name
	}
	return false
}

func TestYU30MemberCreatePortDoesNotHideDeliveryBypasses(t *testing.T) {
	source := []byte(`package probe
import (
 "github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
 ma "github.com/hvritual/iot-delivery-system/backend-yunka/internal/localmemberadmin"
)
type Handler struct { members *ma.Manager; repo *delivery.Service }
func (handler *Handler) create() {
 handler.members.Create(nil, ma.CreateInput{})
 handler.repo.Create(nil, delivery.CreateInput{})
}
func misleading(members *delivery.Service) { members.Create(nil, delivery.CreateInput{}) }
func constructorBypass() { _ = delivery.NewRootTransactionalService(nil,nil,nil) }
`)
	violations, err := scanProductionSource("internal/localbffhttp/probe.go", source)
	if err != nil {
		t.Fatal(err)
	}
	createCount := 0
	for _, violation := range violations {
		if strings.Contains(violation, "calls Create outside Operations") {
			createCount++
		}
	}
	if createCount != 2 || !containsViolation(violations, "constructs delivery.Service outside bootstrap assembly") {
		t.Fatalf("typed member port must be allowed but both delivery bypasses and root constructor rejected: %v", violations)
	}
}
