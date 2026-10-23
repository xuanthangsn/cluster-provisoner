package engine_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// These tests hold the engine's two architectural boundaries in place by
// reading the source, so that crossing either one fails the build rather than
// relying on review.

const module = "sim.dev/engine"

// dockerAllowed lists the only packages that may mention Docker: the container
// backend itself, the composition root that constructs it, and the
// build-tagged end-to-end tests that drive it.
var dockerAllowed = []string{"provider/container", "cmd/simctl", "e2e"}

// clockFree lists the packages above the Machine provider. None of them may
// read or wait on the system clock; they take a clock.Clock instead (NFR-10).
var clockFree = []string{".", "spec", "planner", "executor", "taskrunner", "state", "events"}

// TestNothingAboveTheProviderReferencesDocker is NFR-9: a new backend must be
// addable without touching anything above the provider, which is only true if
// nothing above it knows Docker exists.
func TestNothingAboveTheProviderReferencesDocker(t *testing.T) {
	for dir, files := range sourcePackages(t) {
		if within(dir, dockerAllowed) {
			continue
		}
		for _, f := range files {
			for _, imp := range f.ast.Imports {
				path, _ := strconv.Unquote(imp.Path.Value)
				if path == module+"/provider/container" || strings.Contains(strings.ToLower(path), "docker") {
					t.Errorf("%s imports %s: only %v may reference Docker", f.name, path, dockerAllowed)
				}
			}
			ast.Inspect(f.ast, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if ok && lit.Kind == token.STRING && strings.Contains(strings.ToLower(lit.Value), "docker") {
					t.Errorf("%s:%d mentions Docker in %s: that knowledge belongs below the provider",
						f.name, f.fset.Position(lit.Pos()).Line, lit.Value)
				}
				return true
			})
		}
	}
}

// TestNothingAboveTheProviderReadsTheSystemClock is what makes time
// injectable: Probe intervals, timeouts and retry windows can only be tested
// without sleeping if nothing reaches past the clock.Clock it was given.
func TestNothingAboveTheProviderReadsTheSystemClock(t *testing.T) {
	forbidden := map[string]bool{
		"Now": true, "Since": true, "Until": true, "After": true, "AfterFunc": true,
		"Sleep": true, "Tick": true, "NewTimer": true, "NewTicker": true,
	}
	for dir, files := range sourcePackages(t) {
		if !within(dir, clockFree) || (dir != "." && !exactly(dir, clockFree)) {
			continue
		}
		for _, f := range files {
			timeName := importName(f.ast, "time")
			if timeName == "" {
				continue
			}
			ast.Inspect(f.ast, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == timeName && forbidden[sel.Sel.Name] {
					t.Errorf("%s:%d calls time.%s: take a clock.Clock instead",
						f.name, f.fset.Position(sel.Pos()).Line, sel.Sel.Name)
				}
				return true
			})
		}
	}
}

// TestTheBoundariesCanFail guards the two tests above against passing
// vacuously: it proves they see the packages they are meant to police.
func TestTheBoundariesCanFail(t *testing.T) {
	pkgs := sourcePackages(t)
	for _, want := range append([]string{"provider/container"}, clockFree...) {
		if _, ok := pkgs[want]; !ok {
			t.Errorf("the architecture tests do not see package %q", want)
		}
	}
}

type sourceFile struct {
	name string
	fset *token.FileSet
	ast  *ast.File
}

// sourcePackages parses every non-test Go file in the module, by directory
// relative to the module root.
func sourcePackages(t *testing.T) map[string][]sourceFile {
	t.Helper()
	out := map[string][]sourceFile{}
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != "." && (strings.HasPrefix(d.Name(), ".") || d.Name() == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		dir := filepath.ToSlash(filepath.Dir(path))
		out[dir] = append(out[dir], sourceFile{name: path, fset: fset, ast: f})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// within reports whether dir is one of roots or lies beneath one.
func within(dir string, roots []string) bool {
	for _, r := range roots {
		if dir == r || (r != "." && strings.HasPrefix(dir, r+"/")) {
			return true
		}
	}
	return false
}

func exactly(dir string, list []string) bool {
	for _, d := range list {
		if dir == d {
			return true
		}
	}
	return false
}

// importName returns the name a file uses for an import, or "" if it does not
// import it.
func importName(f *ast.File, path string) string {
	for _, imp := range f.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		if p != path {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}
		return path[strings.LastIndex(path, "/")+1:]
	}
	return ""
}
