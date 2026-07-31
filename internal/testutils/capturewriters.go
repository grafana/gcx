package testutils

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
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
			if path != root && skipWalkDir(path, d.Name()) {
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

// skipWalkDir reports whether the writer walk must not descend into a
// directory. It takes the path as well as the name so a nested module is
// recognised by its own go.mod rather than by a name this repository would have
// to keep a list of.
//
// The dot-prefix rule is the one that matters in practice. This repository puts
// git worktrees under .claude/worktrees, and a worktree holds a second copy of
// every file in the tree — including the one legitimate writer. Walking into
// one makes a guard report the same file twice and fail with a message about "a
// writer elsewhere" that names the file it was already expecting. CI has no
// worktrees, so only the local pre-commit gate breaks, which is the worst place
// for a false alarm. Skipping dot- and underscore-prefixed directories matches
// what the go tool itself ignores, and dropping the nested modules takes the
// walk from about 7000 files back to the module's own 1200.
func skipWalkDir(path, name string) bool {
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
		return true
	}
	if name == "vendor" || name == "node_modules" || name == "testdata" {
		return true
	}
	// A directory with its own go.mod is a separate module: its files are not
	// this repository's code, and a checkout of this repository placed inside
	// the tree would otherwise duplicate every writer.
	_, err := os.Stat(filepath.Join(path, "go.mod"))

	return err == nil
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
