package resources //nolint:testpackage // exercises the unexported capture helper and its call sites

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/telemetry/capture"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCaptureBatchVolumeRecordsFinalizedCounts(t *testing.T) {
	capture.Reset()
	t.Cleanup(capture.Reset)

	captureBatchVolume(cmdio.MutationSummary{Succeeded: 12, Failed: 3, Skipped: 4}, true)

	got := capture.CurrentBatch()
	require.NotNil(t, got)
	assert.Equal(t, capture.Batch{Succeeded: 12, Failed: 3, Skipped: 4, DryRun: true}, *got)
}

// A completed operation that matched nothing reports zeroes. That is a distinct
// answer from reporting nothing at all, which means the operation never reached
// a final count.
func TestCaptureBatchVolumeReportsEmptyOperation(t *testing.T) {
	capture.Reset()
	t.Cleanup(capture.Reset)

	captureBatchVolume(cmdio.MutationSummary{}, false)

	got := capture.CurrentBatch()
	require.NotNil(t, got, "an operation that matched nothing is still a completed operation")
	assert.Equal(t, capture.Batch{}, *got)
}

// dry_run must survive as its own value, because it cannot be recovered from the
// recorded flag names: --dry-run=false marks the flag changed too.
func TestCaptureBatchVolumeCarriesDryRunBothWays(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		capture.Reset()
		t.Cleanup(capture.Reset)

		captureBatchVolume(cmdio.MutationSummary{Succeeded: 5}, dryRun)

		got := capture.CurrentBatch()
		require.NotNil(t, got)
		assert.Equal(t, dryRun, got.DryRun)
	}
}

// capturedCallSites are the only functions allowed to record batch volume.
//
// This is the guard for the invariant that `gcx resources get` never reports
// volume. get reaches remote.Puller.Pull through FetchResources, and delete
// fetches before deleting, so moving the capture down into the puller — the
// obvious place, since that is where the summary is produced — would silently
// make get report volume for a read and make delete report its internal fetch
// instead of the deletion. No behavioural test can express "and nothing else
// calls this", so the call sites are pinned by name instead.
func capturedCallSites() []string {
	return []string{"pushCmd", "deleteCmd", "pullCmd", "validateCmd"}
}

// referencesCaptureHelper reports whether n mentions captureBatchVolume in any
// expression position, not just as the function of a call. Matching only
// CallExpr.Fun would miss `f := captureBatchVolume; f(s, false)`, which reaches
// the same global by a name the guard never sees.
func referencesCaptureHelper(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(node ast.Node) bool {
		if ident, ok := node.(*ast.Ident); ok && ident.Name == "captureBatchVolume" {
			found = true
			return false
		}
		return true
	})
	return found
}

func parsePackageFiles(t *testing.T) map[string]*ast.File {
	t.Helper()

	paths, err := filepath.Glob("*.go")
	require.NoError(t, err)

	files := make(map[string]*ast.File, len(paths))
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue // tests call the helper directly; only production callers are pinned
		}
		parsed, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		require.NoError(t, parseErr, path)
		files[path] = parsed
	}
	return files
}

func TestCaptureBatchVolumeCallSitesArePinned(t *testing.T) {
	var callers []string
	for _, parsed := range parsePackageFiles(t) {
		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if referencesCaptureHelper(fn.Body) && !slices.Contains(callers, fn.Name.Name) {
				callers = append(callers, fn.Name.Name)
			}
		}
	}

	assert.ElementsMatch(t, capturedCallSites(), callers,
		"captureBatchVolume call sites changed: get must never report batch volume, so a new "+
			"caller (or one moved into the shared fetch/pull path) needs a deliberate decision")
}

// The helper is only a wrapper; the state itself lives behind the exported
// capture.SetBatch, whose package doc invites writes from anywhere. Pinning the
// wrapper alone would let the very refactor this guard exists to prevent slip
// through by calling capture.SetBatch directly from remote.Puller.Pull, so the
// whole repository is checked for writers outside the telemetry packages.
func TestNothingOutsideResourcesWritesBatchCapture(t *testing.T) {
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

	var writers []string
	for _, path := range goFiles {
		src, readErr := os.ReadFile(path)
		require.NoError(t, readErr, path)
		if !strings.Contains(string(src), "capture.SetBatch") {
			continue
		}
		rel, relErr := filepath.Rel(root, path)
		require.NoError(t, relErr)
		writers = append(writers, rel)
	}

	assert.ElementsMatch(t, []string{filepath.Join("cmd", "gcx", "resources", "dryrun.go")}, writers,
		"capture.SetBatch must only be written through captureBatchVolume; a direct writer elsewhere "+
			"bypasses the call-site pin and can make read-only commands report volume")
}

// Capture must be unreachable when the operation returned an error, so a hard
// abort reports nothing. Placement alone carries that guarantee — there is no
// type or signature enforcing it — and no test executes these commands, so
// moving the call above its error check would otherwise pass the whole suite
// while silently reporting volume for aborted work.
//
// Checked structurally: in the block containing the capture call, some earlier
// statement must be an `if` that returns.
func TestCaptureBatchVolumeIsGuardedByAnErrorReturn(t *testing.T) {
	guarded := 0

	for path, parsed := range parsePackageFiles(t) {
		ast.Inspect(parsed, func(n ast.Node) bool {
			block, ok := n.(*ast.BlockStmt)
			if !ok {
				return true
			}
			for i, stmt := range block.List {
				expr, isExpr := stmt.(*ast.ExprStmt)
				if !isExpr || !referencesCaptureHelper(expr) {
					continue
				}
				guarded++
				assert.True(t, errorCheckedBefore(block.List[:i]),
					"%s: captureBatchVolume must sit after the error check for the operation that "+
						"produced its counts, so an aborted operation reports no volume", path)
			}
			return true
		})
	}

	assert.Len(t, capturedCallSites(), guarded,
		"every pinned call site must be order-checked; a call nested somewhere this walk "+
			"does not reach is a call whose abort behaviour is unverified")
}

// errorCheckedBefore reports whether the most recent error-producing assignment
// before this point was followed by a returning `if`.
//
// Scanning backwards is what makes this meaningful. A RunE body is full of
// earlier `if err != nil { return err }` guards, so merely finding one somewhere
// above is satisfied by any placement, including immediately after the operation
// call and before its own check. Reaching an assignment that binds `err` first
// means the capture sits between an operation and its error check.
func errorCheckedBefore(stmts []ast.Stmt) bool {
	for _, stmt := range slices.Backward(stmts) {
		switch stmt := stmt.(type) {
		case *ast.IfStmt:
			for _, inner := range stmt.Body.List {
				if _, isReturn := inner.(*ast.ReturnStmt); isReturn {
					return true
				}
			}
		case *ast.AssignStmt:
			for _, lhs := range stmt.Lhs {
				if ident, ok := lhs.(*ast.Ident); ok && ident.Name == "err" {
					return false
				}
			}
		}
	}
	return false
}
