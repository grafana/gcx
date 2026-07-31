package testutils

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// CaptureWriters walks the repository rooted at root and returns, for each
// named function, the repo-relative paths of the non-test Go files that call
// it as a selector on the package at pkgPath. It parses rather than
// substring-matching: a raw search misses an aliased import (cap
// "…/telemetry/capture"; cap.SetBatch) — exactly the move a writer pin exists
// to block — and fires on any passing mention in a comment. Dot-imports are
// handled by matching the unqualified call.
//
// It exists so every capture-slot writer guard shares one walker. Two copies
// of this logic drift: a fix applied to one (a new generated directory in the
// skip list, an aliasing case) never reaches the other, and the guard that
// missed it silently stops seeing writers it claims to pin.
func CaptureWriters(t *testing.T, root, pkgPath string, funcNames ...string) map[string][]string {
	t.Helper()

	// Collect paths during the walk and read them afterwards: doing the I/O
	// inside the callback walks a path the walker may already have moved past.
	var goFiles []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if name := d.Name(); name == ".git" || name == "vendor" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			goFiles = append(goFiles, path)
		}
		return nil
	})
	require.NoError(t, err)

	writers := make(map[string][]string, len(funcNames))
	for _, path := range goFiles {
		parsed, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseErr != nil {
			continue // not buildable Go; nothing a writer pin can say about it
		}
		called := calledPackageFuncs(parsed, pkgPath, funcNames)
		if len(called) == 0 {
			continue
		}
		rel, relErr := filepath.Rel(root, path)
		require.NoError(t, relErr)
		for _, funcName := range called {
			writers[funcName] = append(writers[funcName], rel)
		}
	}
	return writers
}

// calledPackageFuncs returns which of funcNames the file calls on the package
// at pkgPath, resolving the local name from the import so an alias cannot
// hide it. One AST pass covers every name.
func calledPackageFuncs(file *ast.File, pkgPath string, funcNames []string) []string {
	local := ""
	for _, imp := range file.Imports {
		if imp.Path == nil || strings.Trim(imp.Path.Value, `"`) != pkgPath {
			continue
		}
		local = pkgPath[strings.LastIndex(pkgPath, "/")+1:]
		if imp.Name != nil {
			local = imp.Name.Name
		}
	}
	if local == "" || local == "_" {
		return nil
	}

	wanted := make(map[string]bool, len(funcNames))
	for _, name := range funcNames {
		wanted[name] = true
	}
	found := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.SelectorExpr:
			if !wanted[node.Sel.Name] {
				return true
			}
			if ident, isIdent := node.X.(*ast.Ident); isIdent && ident.Name == local {
				found[node.Sel.Name] = true
			}
		case *ast.CallExpr:
			// Dot-imported: the call appears unqualified.
			if local != "." {
				return true
			}
			if ident, isIdent := node.Fun.(*ast.Ident); isIdent && wanted[ident.Name] {
				found[ident.Name] = true
			}
		}
		return true
	})

	out := make([]string, 0, len(found))
	for name := range found {
		out = append(out, name)
	}
	return out
}
