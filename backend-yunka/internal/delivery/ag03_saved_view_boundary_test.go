package delivery

import (
	"context"
	"errors"
	savedview "github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/application/savedview"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/domain"
	store "github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/infrastructure/persistence/savedview"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/ports"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestAG03SavedViewActualCapabilitySurfaces(t *testing.T) {
	broad := NewMemoryRepository()
	port := newSavedViewRepository(broad)
	assertAG03Methods(t, port, []string{"CreateSavedView", "ListSavedViews"})
	if _, wide := port.(Repository); wide {
		t.Fatal("saved-view repository exposes full delivery Repository")
	}
	assertAG03Methods(t, store.New(func(context.Context) (store.Executor, error) { return nil, errors.New("not called") }), []string{"CreateSavedView", "ListSavedViews"})
	service := NewService(broad, nil)
	app, err := service.savedViewUseCase()
	if err != nil {
		t.Fatal(err)
	}
	assertAG03Methods(t, app, []string{"ListSavedViews", "SaveView"})
	if reflect.TypeOf(app).Elem().Name() != "service" {
		t.Fatal("unexpected handler")
	}
	// A wide repository has extra methods; the built use case gets the separate
	// dynamic wrapper rather than merely that same object assigned to a small interface.
	actual := reflect.ValueOf(app).Elem().FieldByName("repository").Elem().Type()
	if actual.NumMethod() != 2 {
		t.Fatalf("actual use-case repository exposes %d methods, want 2", actual.NumMethod())
	}
	st := reflect.TypeOf(app).Elem()
	for i := 0; i < st.NumField(); i++ {
		f := st.Field(i)
		if f.Anonymous || f.IsExported() {
			t.Fatalf("implementation field leaks: %v", f)
		}
	}
	if f, ok := st.FieldByName("repository"); !ok || f.Type != reflect.TypeOf((*ports.SavedViewRepository)(nil)).Elem() {
		t.Fatalf("wrong use-case repository: %v", f)
	}
	if newSavedViewRepository(nil) != nil {
		t.Fatal("nil repository not preserved")
	}
}
func assertAG03Methods(t *testing.T, value any, want []string) {
	t.Helper()
	typ := reflect.TypeOf(value)
	var got []string
	for i := 0; i < typ.NumMethod(); i++ {
		got = append(got, typ.Method(i).Name)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%v method surface %v want %v", typ, got, want)
	}
}

type ag03TinyRepository struct {
	err   error
	calls int
}

func (r *ag03TinyRepository) CreateSavedView(context.Context, domain.SavedView) error {
	r.calls++
	return r.err
}
func (r *ag03TinyRepository) ListSavedViews(context.Context, string) ([]domain.SavedView, error) {
	r.calls++
	return nil, r.err
}
func TestAG03SavedViewNeedsOnlyTwoRepositoryMethods(t *testing.T) {
	cause := errors.New("storage unavailable")
	repo := &ag03TinyRepository{err: cause}
	app, err := savedview.Build(repo, time.Now, func() uint64 { return 1 })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.SaveView(context.Background(), domain.SavedViewInput{Name: "x"}); !errors.Is(err, ErrCanonicalUserRequired) || repo.calls != 0 {
		t.Fatalf("identity before IO: %v calls=%d", err, repo.calls)
	}
	ctx := identity.WithPrincipal(context.Background(), identity.Principal{Authenticated: true, UserID: "u"})
	if _, err := app.SaveView(ctx, domain.SavedViewInput{Name: "x"}); !errors.Is(err, cause) || err.Error() != "check generated delivery view ID: storage unavailable" {
		t.Fatalf("wrapped read error: %v", err)
	}
	if _, err := app.ListSavedViews(ctx); !errors.Is(err, cause) {
		t.Fatal(err)
	}
	repo.err = nil
	repo.calls = 0
	if view, err := app.SaveView(ctx, domain.SavedViewInput{Name: "x"}); err != nil || view.Owner != "u" || repo.calls != 2 {
		t.Fatalf("independent minimal repository: %+v %v calls=%d", view, err, repo.calls)
	}
	for _, build := range []func() (savedview.Application, error){
		func() (savedview.Application, error) {
			return savedview.Build(nil, time.Now, func() uint64 { return 1 })
		},
		func() (savedview.Application, error) { return savedview.Build(repo, nil, func() uint64 { return 1 }) },
		func() (savedview.Application, error) { return savedview.Build(repo, time.Now, nil) },
	} {
		if _, err := build(); err == nil {
			t.Fatal("incomplete construction accepted")
		}
	}
}

func TestAG03SavedViewLayerDependencies(t *testing.T) {
	const parent = "github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	policy := map[string]map[string]bool{
		"domain":                               {"errors": true, "strings": true, "time": true},
		"ports":                                {"context": true, parent + "/domain": true},
		"application/savedview":                {"context": true, "errors": true, "fmt": true, "strings": true, "time": true, parent + "/domain": true, parent + "/ports": true, parent + "/application/savedview/internal/usecase": true, "github.com/hvritual/yunka.io/framework/core/identity": true},
		"infrastructure/persistence/savedview": {"context": true, "database/sql": true, "encoding/json": true, "errors": true, "fmt": true, "strings": true, parent + "/domain": true, parent + "/ports": true},
	}
	for root, allowed := range policy {
		count := 0
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			count++
			f, e := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if e != nil {
				return e
			}
			for _, imp := range f.Imports {
				v, e := strconv.Unquote(imp.Path.Value)
				if e != nil {
					return e
				}
				if !allowed[v] {
					t.Errorf("%s imports forbidden dependency %s", path, v)
				}
			}
			return nil
		})
		if err != nil || count == 0 {
			t.Fatalf("coverage %s: %d %v", root, count, err)
		}
	}
	// No business rules may migrate back into the monolithic service facade.
	f, err := parser.ParseFile(token.NewFileSet(), "service.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range f.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && (fn.Name.Name == "SaveView" || fn.Name.Name == "ListSavedViews" || fn.Name.Name == "nextSavedViewID") {
			t.Fatalf("saved-view implementation returned to monolith: %s", fn.Name.Name)
		}
	}
}
