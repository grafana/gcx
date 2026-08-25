package resources //nolint:testpackage // exercises the unexported capture helper and its call sites

import (
	"errors"
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

	captureBatchVolume(cmdio.MutationSummary{Succeeded: 12, Failed: 3, Skipped: 4}, true, nil)

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

	captureBatchVolume(cmdio.MutationSummary{}, false, nil)

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

		captureBatchVolume(cmdio.MutationSummary{Succeeded: 5}, dryRun, nil)

		got := capture.CurrentBatch()
		require.NotNil(t, got)
		assert.Equal(t, dryRun, got.DryRun)
	}
}

const capturePackagePath = "github.com/grafana/gcx/internal/telemetry/capture"

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

	var writers []string
	for _, path := range goFiles {
		// Parse rather than substring-match. A raw search for "capture.SetBatch"
		// misses an aliased import (cap "…/telemetry/capture"; cap.SetBatch)
		// — exactly the move this guard exists to block — and fires on any
		// passing mention in a comment.
		parsed, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseErr != nil {
			continue // not buildable Go; nothing this guard can say about it
		}
		if !callsCaptureSetBatch(parsed) {
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

// skipWalkDir reports whether the writer walk must not descend into a
// directory. It takes the path as well as the name so a nested module is
// recognised by its own go.mod rather than by a name this repository would have
// to keep a list of.
//
// The dot-prefix rule is the one that matters in practice. This repository puts
// git worktrees under .claude/worktrees, and a worktree holds a second copy of
// every file in the tree — including the one legitimate writer. Walking into
// one makes the guard report the same file twice and fail with a message about
// "a writer elsewhere" that names the file it was already expecting. CI has no
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

// callsCaptureSetBatch reports whether the file calls SetBatch on the capture
// package, resolving the local name from the import so an alias cannot hide it.
func callsCaptureSetBatch(file *ast.File) bool {
	local := ""
	for _, imp := range file.Imports {
		if imp.Path == nil || strings.Trim(imp.Path.Value, `"`) != capturePackagePath {
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
		if !ok || sel.Sel.Name != "SetBatch" {
			return true
		}
		// A dot-import makes the qualifier absent; that is caught by the
		// local == "." case below rather than here.
		if ident, isIdent := sel.X.(*ast.Ident); isIdent && ident.Name == local {
			found = true
			return false
		}
		return true
	})
	if found {
		return true
	}

	// Dot-imported: SetBatch appears unqualified.
	if local != "." {
		return false
	}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if ident, isIdent := call.Fun.(*ast.Ident); isIdent && ident.Name == "SetBatch" {
			found = true
			return false
		}
		return true
	})
	return found
}

// A failed operation must record nothing, whatever the placement of the call.
//
// This replaces an AST order check that tried to prove the call sat after its
// error guard. That check was defeated twice: first by the many unrelated error
// guards in a RunE body, then by renaming the error variable, since it had to
// recognise the binding by the identifier "err". Enforcing it in the callee
// removes the naming dependency and the ordering assumption together.
func TestCaptureBatchVolumeIgnoresAFailedOperation(t *testing.T) {
	capture.Reset()
	t.Cleanup(capture.Reset)

	captureBatchVolume(cmdio.MutationSummary{Succeeded: 47}, false, errors.New("aborted"))

	assert.Nil(t, capture.CurrentBatch(),
		"an operation that failed must report no volume, even if counts were available")
}

// A prior successful capture must not be erased by a later failed one, so the
// guard cannot turn a reported operation into an unreported one.
func TestCaptureBatchVolumeKeepsEarlierSuccessOnLaterFailure(t *testing.T) {
	capture.Reset()
	t.Cleanup(capture.Reset)

	captureBatchVolume(cmdio.MutationSummary{Succeeded: 5}, false, nil)
	captureBatchVolume(cmdio.MutationSummary{Succeeded: 99}, false, errors.New("aborted"))

	got := capture.CurrentBatch()
	require.NotNil(t, got)
	assert.Equal(t, 5, got.Succeeded)
}

// The callee guard only works if call sites actually pass their operation's
// error, and this checks the weakest necessary condition: that the argument is
// an identifier rather than a nil literal. It cannot tell whether that
// identifier is the right error — a caller passing some other nil-valued error
// variable would pass here and silently restore the dependency on placement.
// Reviewers still own that; this only catches the accidental `nil`.
func TestCaptureBatchVolumeCallSitesPassANonNilIdentifierAsOpErr(t *testing.T) {
	checked := 0

	for path, parsed := range parsePackageFiles(t) {
		ast.Inspect(parsed, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			ident, isIdent := call.Fun.(*ast.Ident)
			if !isIdent || ident.Name != "captureBatchVolume" {
				return true
			}
			checked++
			require.Len(t, call.Args, 3, "%s: captureBatchVolume takes the operation error", path)
			arg, isIdent := call.Args[2].(*ast.Ident)
			assert.True(t, isIdent && arg.Name != "nil",
				"%s: pass the operation's error, not a nil literal, or a reordered call "+
					"would record volume for aborted work", path)
			return true
		})
	}

	assert.Len(t, capturedCallSites(), checked,
		"every pinned call site must be checked for the error argument")
}
