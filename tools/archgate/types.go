package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

type listedPackage struct {
	ImportPath      string
	Dir             string
	Export          string
	CompiledGoFiles []string
	Error           *struct{ Err string }
}

// go list resolves the module graph and emits canonical export locations. The
// importer never guesses GOPATH or package names. Type checking is performed by
// the same toolchain as the exports. Inactive build variants are covered by scan;
// this pass records and checks the actual native GOOS/GOARCH only.
func typedScan(root string, p Policy) ([]Finding, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "list", "-mod=readonly", "-deps", "-export", "-compiled", "-json", "./...")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local", "GOFLAGS=-mod=readonly")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("type inventory failed (not PASS): %w: %s", err, stderr.String())
	}
	packages := []listedPackage{}
	exports := map[string]string{}
	d := json.NewDecoder(bytes.NewReader(data))
	for {
		var x listedPackage
		err := d.Decode(&x)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if x.Error != nil {
			return nil, fmt.Errorf("load %s: %s", x.ImportPath, x.Error.Err)
		}
		exports[x.ImportPath] = x.Export
		if under(x.ImportPath, p.Module) && len(x.CompiledGoFiles) > 0 {
			packages = append(packages, x)
		}
	}
	if len(packages) == 0 {
		return nil, fmt.Errorf("no type-checked production packages")
	}
	sort.Slice(packages, func(i, j int) bool { return packages[i].ImportPath < packages[j].ImportPath })
	findings := []Finding{}
	fset := token.NewFileSet()
	lookup := func(imp string) (io.ReadCloser, error) {
		name := exports[imp]
		if name == "" {
			return nil, fmt.Errorf("missing canonical export data: %s", imp)
		}
		return os.Open(name)
	}
	resolver := importer.ForCompiler(fset, "gc", lookup)
	for _, pkg := range packages {
		dir, err := filepath.Rel(root, pkg.Dir)
		if err != nil {
			return nil, err
		}
		dir = filepath.ToSlash(dir)
		owner, ok := p.Owners[dir]
		if !ok {
			return nil, fmt.Errorf("unclassified active package: %s", dir)
		}
		files := []*ast.File{}
		for _, name := range pkg.CompiledGoFiles {
			if !filepath.IsAbs(name) {
				name = filepath.Join(pkg.Dir, name)
			}
			f, err := parser.ParseFile(fset, name, nil, 0)
			if err != nil {
				return nil, err
			}
			files = append(files, f)
		}
		info := &types.Info{Uses: map[*ast.Ident]types.Object{}, Selections: map[*ast.SelectorExpr]*types.Selection{}}
		conf := types.Config{Importer: resolver, Sizes: types.SizesFor("gc", runtime.GOARCH)}
		if _, err := conf.Check(pkg.ImportPath, fset, files, info); err != nil {
			return nil, fmt.Errorf("type-check %s: %w", pkg.ImportPath, err)
		}
		// Composition owns construction. The delivery implementation is permitted to
		// reference its own service; transports never gain this privilege by naming
		// a variable 'operations', aliasing an import, or using a method value.
		if owner.Role == "composition" || (owner.Module == "delivery" && (owner.Role == "domain" || owner.Role == "application")) {
			continue
		}
		for id, obj := range info.Uses {
			if obj.Pkg() == nil || obj.Pkg().Path() != p.Module+"/internal/delivery" {
				continue
			}
			prohibited := false
			switch obj.(type) {
			case *types.TypeName:
				prohibited = obj.Name() == "Service" || obj.Name() == "SQLiteRepository" || obj.Name() == "MemoryRepository" || obj.Name() == "Repository"
			case *types.Func:
				prohibited = obj.Name() == "NewService" || obj.Name() == "NewRootTransactionalService" || obj.Name() == "NewSQLiteRepository" || obj.Name() == "NewMemoryRepository"
			}
			if prohibited {
				pos := fset.Position(id.Pos())
				name, err := filepath.Rel(root, pos.Filename)
				if err != nil {
					return nil, err
				}
				findings = append(findings, Finding{Rule: "ARCH-WRITE-001", File: filepath.ToSlash(name), Target: obj.Pkg().Path() + "." + obj.Name(), Line: pos.Line, Remediation: owner.Remediation})
			}
		}
		writes := map[string]bool{"Create": true, "CreateProject": true, "CreateRelease": true, "CreateSprint": true, "CreateMilestone": true, "UpdateWorkItem": true, "AddComment": true, "UpdateContext": true, "AdvanceGate": true, "Close": true, "SaveView": true, "Save": true, "SaveProject": true, "SaveRelease": true, "SaveSprint": true, "SaveMilestone": true, "CreateSavedView": true}
		for expression, selection := range info.Selections {
			obj := selection.Obj()
			if obj.Pkg() == nil || obj.Pkg().Path() != p.Module+"/internal/delivery" || !writes[obj.Name()] {
				continue
			}
			pos := fset.Position(expression.Pos())
			name, err := filepath.Rel(root, pos.Filename)
			if err != nil {
				return nil, err
			}
			findings = append(findings, Finding{Rule: "ARCH-WRITE-002", File: filepath.ToSlash(name), Target: obj.Pkg().Path() + "." + obj.Name(), Line: pos.Line, Remediation: owner.Remediation})
		}

	}
	sortFindings(findings)
	return findings, nil
}

func sameNativePlatform() bool {
	return (os.Getenv("GOOS") == "" || os.Getenv("GOOS") == runtime.GOOS) && (os.Getenv("GOARCH") == "" || os.Getenv("GOARCH") == runtime.GOARCH) && !strings.Contains(os.Getenv("GOFLAGS"), "-tags")
}
