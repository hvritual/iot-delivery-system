package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func git(root string, args ...string) ([]byte, error) {
	c := exec.Command("git", append([]string{"-C", root}, args...)...)
	var stderr bytes.Buffer
	c.Stderr = &stderr
	b, e := c.Output()
	if e != nil {
		return nil, fmt.Errorf("git %v: %w: %s", args, e, stderr.String())
	}
	return b, nil
}
func revision(root, ref string) (string, error) {
	b, e := git(root, "rev-parse", "--verify", ref+"^{commit}")
	if e != nil {
		return "", e
	}
	s := strings.TrimSpace(string(b))
	if !isSHA(s) {
		return "", fmt.Errorf("invalid commit identity")
	}
	return s, nil
}
func snapshot(root, ref string) ([]Source, error) {
	if !isSHA(ref) {
		return nil, fmt.Errorf("snapshot requires immutable SHA")
	}
	data, err := git(root, "ls-tree", "-r", "-z", ref, "--", "backend-yunka")
	if err != nil {
		return nil, err
	}
	result := []Source{}
	for _, entry := range bytes.Split(data, []byte{0}) {
		if len(entry) == 0 {
			continue
		}
		parts := bytes.SplitN(entry, []byte{'\t'}, 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid git tree entry")
		}
		name := string(parts[1])
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		meta := strings.Fields(string(parts[0]))
		if len(meta) != 3 || meta[1] != "blob" || (meta[0] != "100644" && meta[0] != "100755") {
			return nil, fmt.Errorf("unsupported source mode: %s", name)
		}
		content, err := git(root, "cat-file", "blob", meta[2])
		if err != nil {
			return nil, err
		}
		result = append(result, Source{Path: strings.TrimPrefix(name, "backend-yunka/"), Blob: meta[2], Content: content})
	}
	return result, nil
}
func run() error {
	root := flag.String("root", ".", "repository root")
	flag.Parse()
	absolute, err := filepath.Abs(*root)
	if err != nil {
		return err
	}
	if !sameNativePlatform() {
		return fmt.Errorf("custom GOOS/GOARCH/build tags require a separately certified profile")
	}
	head, err := revision(absolute, "HEAD")
	if err != nil {
		return err
	}
	dirty, err := git(absolute, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return err
	}
	if len(dirty) > 0 {
		return fmt.Errorf("architecture evidence requires a clean committed worktree")
	}
	policyData, err := git(absolute, "show", head+":.architecture/policy.json")
	if err != nil {
		return err
	}
	p, err := decodePolicy(policyData)
	if err != nil {
		return err
	}
	if _, err := git(absolute, "merge-base", "--is-ancestor", p.Baseline, head); err != nil {
		return fmt.Errorf("baseline is not an ancestor: %w", err)
	}
	pin, err := git(absolute, "ls-tree", head, "--", "third_party/yunka")
	if err != nil {
		return err
	}
	parts := strings.Fields(string(pin))
	if len(parts) != 4 || parts[0] != "160000" || parts[2] != p.Framework {
		return fmt.Errorf("framework gitlink mismatch")
	}
	sub, err := revision(filepath.Join(absolute, "third_party/yunka"), "HEAD")
	if err != nil || sub != p.Framework {
		return fmt.Errorf("framework checkout missing or mismatched")
	}
	subdirty, err := git(filepath.Join(absolute, "third_party/yunka"), "status", "--porcelain", "--untracked-files=all")
	if err != nil || len(subdirty) > 0 {
		return fmt.Errorf("framework worktree is dirty or unreadable")
	}
	files, err := snapshot(absolute, head)
	if err != nil {
		return err
	}
	base, err := snapshot(absolute, p.Baseline)
	if err != nil {
		return err
	}
	inventory, findings, err := scan(p, files)
	if err != nil {
		return err
	}
	typed, err := typedScan(filepath.Join(absolute, "backend-yunka"), p)
	if err != nil {
		return err
	}
	retained, blocking := partition(findings, files, base)
	// Typed write-boundary violations are never grandfathered: a dependency
	// change can create a new semantic bypass in an unchanged caller.
	blocking = append(blocking, typed...)
	sortFindings(blocking)
	tree, err := git(absolute, "rev-parse", head+"^{tree}")
	if err != nil {
		return err
	}
	report := Report{Schema: 1, Head: head, Baseline: p.Baseline, PolicySHA256: hash(policyData), SourceTree: strings.TrimSpace(string(tree)), TypedPlatform: runtime.GOOS + "/" + runtime.GOARCH, Inventory: inventory, Retained: retained, Blocking: blocking}
	e := json.NewEncoder(os.Stdout)
	e.SetIndent("", "  ")
	if err := e.Encode(report); err != nil {
		return err
	}
	if len(blocking) > 0 {
		return fmt.Errorf("%d blocking architecture findings; frozen debt is reported separately", len(blocking))
	}
	return nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "ARCHITECTURE GATE BLOCKED:", err)
		os.Exit(1)
	}
}
