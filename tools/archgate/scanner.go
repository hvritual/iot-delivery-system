package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
)

// Package ownership is exact, not a glob: newly created packages fail closed.
type Owner struct {
	Module      string `json:"module"`
	Role        string `json:"role"`
	Remediation string `json:"remediation"`
}
type Policy struct {
	Version   int              `json:"version"`
	Baseline  string           `json:"baseline"`
	Module    string           `json:"module"`
	Framework string           `json:"framework"`
	Owners    map[string]Owner `json:"packages"`
}
type Source struct {
	Path, Blob string
	Content    []byte
}
type Finding struct {
	Rule        string `json:"rule"`
	File        string `json:"file"`
	Target      string `json:"target"`
	Line        int    `json:"line"`
	Remediation string `json:"remediation,omitempty"`
}
type Inventory struct {
	Package   string   `json:"package"`
	Module    string   `json:"module"`
	Role      string   `json:"role"`
	Files     int      `json:"production_files"`
	Generated int      `json:"generated_files"`
	Imports   []string `json:"imports"`
}
type Report struct {
	Schema        int         `json:"schema"`
	Head          string      `json:"head"`
	Baseline      string      `json:"baseline"`
	PolicySHA256  string      `json:"policy_sha256"`
	SourceTree    string      `json:"source_tree"`
	TypedPlatform string      `json:"typed_platform"`
	Inventory     []Inventory `json:"inventory"`
	Retained      []Finding   `json:"retained_frozen_debt"`
	Blocking      []Finding   `json:"blocking"`
}

func decodePolicy(data []byte) (Policy, error) {
	var p Policy
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&p); err != nil {
		return p, err
	}
	if d.Decode(new(any)) != io.EOF {
		return p, fmt.Errorf("policy has trailing data")
	}
	if p.Version != 1 || !isSHA(p.Baseline) || !isSHA(p.Framework) || !strings.HasPrefix(p.Module, "github.com/") || len(p.Owners) == 0 {
		return p, fmt.Errorf("invalid policy header")
	}
	valid := map[string]bool{"domain": true, "application": true, "infrastructure": true, "security": true, "transport": true, "composition": true, "contract": true, "policy": true}
	for dir, o := range p.Owners {
		if path.Clean(dir) != dir || strings.HasPrefix(dir, "/") || strings.Contains(dir, "\\") || strings.ContainsAny(dir, "*?[]") || strings.Contains(dir, "..") || !valid[o.Role] || o.Module == "" || o.Remediation == "" {
			return p, fmt.Errorf("invalid exact package ownership: %s", dir)
		}
	}
	return p, nil
}
func isSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}
func hash(b []byte) string        { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func under(s, prefix string) bool { return s == prefix || strings.HasPrefix(s, prefix+"/") }
func blockedImport(p Policy, o Owner, imp string) string {
	storage := imp == "database/sql" || under(imp, "gorm.io") || under(imp, "modernc.org/sqlite") || under(imp, "github.com/go-sql-driver") || under(imp, "github.com/jackc/pgx")
	if (o.Role == "domain" || o.Role == "application" || o.Role == "transport") && storage {
		return "ARCH-STORAGE-001"
	}
	if o.Role == "domain" && (imp == "net/http" || under(imp, "google.golang.org/grpc")) {
		return "ARCH-DOMAIN-001"
	}
	if o.Role == "application" && (under(imp, "github.com/hvritual/yunka.io/gateway/authz") || under(imp, "yunka.io/gateway/authz") || under(imp, "github.com/hvritual/yunka.io/framework/platform")) {
		return "ARCH-APPLICATION-001"
	}
	if strings.HasPrefix(imp, p.Module+"/") {
		target, ok := p.Owners[strings.TrimPrefix(imp, p.Module+"/")]
		if !ok {
			return "ARCH-OWNERSHIP-002"
		}
		if o.Role == "domain" && (target.Role == "application" || target.Role == "transport" || target.Role == "infrastructure" || target.Role == "composition") {
			return "ARCH-LAYER-001"
		}
		if o.Role == "transport" && target.Role == "infrastructure" {
			return "ARCH-LAYER-002"
		}
		if o.Role == "infrastructure" && (target.Role == "application" || target.Role == "transport") {
			return "ARCH-LAYER-003"
		}
	}
	return ""
}

// Inspect all committed source, including inactive build-tag files and generated
// files. Only _test.go is excluded from the production dependency inventory.
func scan(p Policy, files []Source) ([]Inventory, []Finding, error) {
	var findings []Finding
	inventories := map[string]*Inventory{}
	imports := map[string]map[string]bool{}
	fset := token.NewFileSet()
	for _, s := range files {
		if !strings.HasSuffix(s.Path, ".go") || strings.HasSuffix(s.Path, "_test.go") {
			continue
		}
		dir := path.Dir(s.Path)
		o, ok := p.Owners[dir]
		if !ok {
			findings = append(findings, Finding{Rule: "ARCH-OWNERSHIP-001", File: s.Path, Target: dir})
			continue
		}
		f, err := parser.ParseFile(fset, s.Path, s.Content, parser.ParseComments)
		if err != nil {
			return nil, nil, fmt.Errorf("parse %s: %w", s.Path, err)
		}
		inv := inventories[dir]
		if inv == nil {
			inv = &Inventory{Package: dir, Module: o.Module, Role: o.Role, Imports: []string{}}
			inventories[dir] = inv
			imports[dir] = map[string]bool{}
		}
		inv.Files++
		if ast.IsGenerated(f) {
			inv.Generated++
		}
		for _, spec := range f.Imports {
			imp, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return nil, nil, err
			}
			imports[dir][imp] = true
			if rule := blockedImport(p, o, imp); rule != "" {
				findings = append(findings, Finding{Rule: rule, File: s.Path, Target: imp, Line: fset.Position(spec.Pos()).Line, Remediation: o.Remediation})
			}
		}
	}
	result := []Inventory{}
	for dir, inv := range inventories {
		for imp := range imports[dir] {
			inv.Imports = append(inv.Imports, imp)
		}
		sort.Strings(inv.Imports)
		result = append(result, *inv)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Package < result[j].Package })
	sortFindings(findings)
	if len(result) == 0 {
		return nil, nil, fmt.Errorf("empty production inventory is not PASS")
	}
	return result, findings, nil
}
func sortFindings(f []Finding) {
	sort.Slice(f, func(i, j int) bool {
		a, b := f[i], f[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Rule != b.Rule {
			return a.Rule < b.Rule
		}
		return a.Target < b.Target
	})
}

// A legacy finding is retained only while its entire source file is byte-identical
// to the immutable baseline. No count budget, line-number exemption or update-
// baseline command can silently bless new uses of an already imported dependency.
func partition(findings []Finding, current, baseline []Source) ([]Finding, []Finding) {
	old := map[string]string{}
	now := map[string]string{}
	for _, s := range baseline {
		old[s.Path] = hash(s.Content)
	}
	for _, s := range current {
		now[s.Path] = hash(s.Content)
	}
	retained := []Finding{}
	blocking := []Finding{}
	for _, f := range findings {
		if !strings.HasPrefix(f.Rule, "ARCH-OWNERSHIP-") && old[f.File] != "" && old[f.File] == now[f.File] {
			retained = append(retained, f)
		} else {
			blocking = append(blocking, f)
		}
	}
	sortFindings(retained)
	sortFindings(blocking)
	return retained, blocking
}
