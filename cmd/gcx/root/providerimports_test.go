package root_test

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

// TestProviderPackagesAreBlankImported fails when a package under
// internal/providers/ registers a provider but command.go does not blank-import
// it. Without the import the provider compiles and its own tests pass, but it
// is missing from the binary and from every test that walks the root command
// tree.
func TestProviderPackagesAreBlankImported(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "command.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse command.go: %v", err)
	}
	imported := map[string]bool{}
	for _, spec := range file.Imports {
		if spec.Name == nil || spec.Name.Name != "_" {
			continue
		}
		p, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatalf("unquote import %s: %v", spec.Path.Value, err)
		}
		imported[p] = true
	}

	providersDir := filepath.Join("..", "..", "..", "internal", "providers")
	registering := map[string]bool{}
	err = filepath.WalkDir(providersDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		dir := filepath.Dir(path)
		// providers.Register lives in the providers package itself.
		if dir == providersDir || registering[dir] {
			return nil
		}
		if callsProvidersRegister(t, fset, path) {
			registering[dir] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", providersDir, err)
	}
	if len(registering) == 0 {
		t.Fatal("found no provider packages calling providers.Register; the scan is broken")
	}

	for dir := range registering {
		rel, err := filepath.Rel(providersDir, dir)
		if err != nil {
			t.Fatalf("relative path for %s: %v", dir, err)
		}
		importPath := "github.com/grafana/gcx/internal/providers/" + filepath.ToSlash(rel)
		if !imported[importPath] {
			t.Errorf("%s calls providers.Register but cmd/gcx/root/command.go does not blank-import %q", importPath, importPath)
		}
	}
}

// callsProvidersRegister reports whether the Go file at path contains a call
// expression providers.Register(...). Comments and string literals do not
// count.
func callsProvidersRegister(t *testing.T, fset *token.FileSet, path string) bool {
	t.Helper()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || found {
			return !found
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Register" {
			return true
		}
		if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "providers" {
			found = true
		}
		return !found
	})
	return found
}
