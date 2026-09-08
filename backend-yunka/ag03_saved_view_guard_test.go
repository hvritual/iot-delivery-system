package backendyunka

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// A bounded addition to the existing consumer guard, not a general type/dataflow
// analyzer. No new file is excluded. Only the two declared implementation call
// sites may use their canonical narrow contracts; other callers remain blocked.
func trustedSavedViewWrite(path string, f *ast.File, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if path == "internal/delivery/saved_view_service.go" && sel.Sel.Name == "SaveView" {
		id, ok := sel.X.(*ast.Ident)
		if !ok || id.Obj == nil {
			return false
		}
		spec, ok := id.Obj.Decl.(*ast.ValueSpec)
		return ok && canonicalImportedType(f, spec.Type, "internal/delivery/application/savedview", "Application")
	}
	if path != "internal/delivery/application/savedview/internal/usecase/service.go" || sel.Sel.Name != "CreateSavedView" {
		return false
	}
	field, ok := sel.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	receiver, ok := field.X.(*ast.Ident)
	if !ok || receiver.Obj == nil {
		return false
	}
	declaration, ok := receiver.Obj.Decl.(*ast.Field)
	if !ok {
		return false
	}
	ptr, ok := declaration.Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	typ, ok := ptr.X.(*ast.Ident)
	if !ok || typ.Obj == nil {
		return false
	}
	spec, ok := typ.Obj.Decl.(*ast.TypeSpec)
	if !ok {
		return false
	}
	structure, ok := spec.Type.(*ast.StructType)
	if !ok {
		return false
	}
	for _, member := range structure.Fields.List {
		for _, name := range member.Names {
			if name.Name == field.Sel.Name {
				return canonicalImportedType(f, member.Type, "internal/delivery/ports", "SavedViewRepository")
			}
		}
	}
	return false
}

func TestAG03SavedViewGuardRejectsMasqueradingCallers(t *testing.T) {
	facade := `package delivery
 import sv "github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/application/savedview"
 func f(){var arbitrary sv.Application;arbitrary.SaveView(nil, struct{}{})}`
	usecase := `package usecase
 import p "github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/ports"
 type service struct{ storage p.SavedViewRepository }
 func(s *service)f(){s.storage.CreateSavedView(nil, struct{}{})}`
	for _, tc := range []struct {
		name, path, source string
		allowed            bool
	}{
		{"typed_facade", "internal/delivery/saved_view_service.go", facade, true},
		{"same_type_wrong_caller", "internal/httpapi/x.go", facade, false},
		{"typed_repository", "internal/delivery/application/savedview/internal/usecase/service.go", usecase, true},
		{"same_repo_wrong_caller", "internal/httpapi/x.go", usecase, false},
		{"wide_repository", "internal/delivery/application/savedview/internal/usecase/service.go", `package usecase;import p "github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery";type service struct{storage p.Repository};func(s *service)f(){s.storage.CreateSavedView(nil,struct{}{})}`, false},
		{"fake_package", "internal/delivery/saved_view_service.go", `package delivery;import sv "example.com/other/savedview";func f(){var arbitrary sv.Application;arbitrary.SaveView(nil,struct{}{})}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, e := parser.ParseFile(token.NewFileSet(), "probe.go", tc.source, 0)
			if e != nil {
				t.Fatal(e)
			}
			calls := 0
			ast.Inspect(f, func(n ast.Node) bool {
				c, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				calls++
				if got := trustedSavedViewWrite(tc.path, f, c); got != tc.allowed {
					t.Fatalf("allow=%v want=%v", got, tc.allowed)
				}
				return true
			})
			if calls != 1 {
				t.Fatalf("test inventory: %d calls", calls)
			}
			violations, e := scanProductionSource(tc.path, []byte(tc.source))
			if e != nil {
				t.Fatal(e)
			}
			if (len(violations) == 0) != tc.allowed {
				t.Fatalf("guard decisions: %v", violations)
			}
		})
	}
}
