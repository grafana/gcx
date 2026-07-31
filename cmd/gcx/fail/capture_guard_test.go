package fail_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const captureSignalPackagePath = "github.com/grafana/gcx/internal/telemetry/capture"

// The capture package's doc invites writes from anywhere, so the discipline of
// who actually writes each slot lives in call-site pins, not in the package.
// The existing batch guard (cmd/gcx/resources/batchvolume_test.go) is keyed on
// the literal name SetBatch and cannot see these setters, so the error-signal
// and auth-method slots get their own: http status and k8s reason are written
// only by CaptureErrorSignals in this package; the auth method only by the
// config selector, with login holding the one forcing override. A writer
// anywhere else bypasses the semantics those sites encode — raw-error timing,
// decided-before-validation, probe-outranks-load — and this guard makes that
// a test failure instead of a review hazard.
func TestCaptureSignalWritersArePinned(t *testing.T) {
	root, err := filepath.Abs("../../..")
	require.NoError(t, err)

	// Collect paths during the walk and read them afterwards: doing the I/O
	// inside the callback walks a path the walker may already have moved past.
	var goFiles []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
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

	wantWriters := map[string][]string{
		"SetHTTPStatus":          {filepath.Join("cmd", "gcx", "fail", "capture.go")},
		"SetK8sReason":           {filepath.Join("cmd", "gcx", "fail", "capture.go")},
		"SetGrafanaAuthMethod":   {filepath.Join("internal", "config", "grafana_auth.go")},
		"ForceGrafanaAuthMethod": {filepath.Join("cmd", "gcx", "login", "command.go")},
	}
	gotWriters := make(map[string][]string, len(wantWriters))

	for _, path := range goFiles {
		// Parse rather than substring-match: a raw search misses an aliased
		// import and fires on passing mentions in comments.
		parsed, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseErr != nil {
			continue // not buildable Go; nothing this guard can say about it
		}
		rel, relErr := filepath.Rel(root, path)
		require.NoError(t, relErr)
		for funcName := range wantWriters {
			if callsCaptureFunc(parsed, funcName) {
				gotWriters[funcName] = append(gotWriters[funcName], rel)
			}
		}
	}

	for funcName, want := range wantWriters {
		assert.ElementsMatch(t, want, gotWriters[funcName],
			"capture.%s has a pinned writer set; a new writer needs the same semantics its comment documents",
			funcName)
	}
}

// callsCaptureFunc reports whether the file calls the named function on the
// capture package, resolving the local name from the import so an alias
// cannot hide it.
func callsCaptureFunc(file *ast.File, funcName string) bool {
	local := ""
	for _, imp := range file.Imports {
		if imp.Path == nil || strings.Trim(imp.Path.Value, `"`) != captureSignalPackagePath {
			continue
		}
		local = "capture"
		if imp.Name != nil {
			local = imp.Name.Name
		}
	}
	if local == "" || local == "_" {
		return false
	}

	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != funcName {
			return true
		}
		if ident, isIdent := sel.X.(*ast.Ident); isIdent && ident.Name == local {
			found = true
			return false
		}
		return true
	})
	if found {
		return true
	}

	// Dot-imported: the call appears unqualified.
	if local != "." {
		return false
	}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if ident, isIdent := call.Fun.(*ast.Ident); isIdent && ident.Name == funcName {
			found = true
			return false
		}
		return true
	})
	return found
}
