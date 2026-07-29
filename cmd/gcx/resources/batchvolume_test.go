package resources //nolint:testpackage // exercises the unexported capture helper and its call sites

import (
	"go/ast"
	"go/parser"
	"go/token"
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

func TestCaptureBatchVolumeCallSitesArePinned(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)

	var callers []string
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue // tests call the helper directly; only production callers are pinned
		}

		fset := token.NewFileSet()
		parsed, parseErr := parser.ParseFile(fset, path, nil, 0)
		require.NoError(t, parseErr, path)

		// Walk each top-level func and record its name if its body mentions
		// captureBatchVolume anywhere, including inside a nested closure such
		// as a cobra RunE.
		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, isCall := n.(*ast.CallExpr)
				if !isCall {
					return true
				}
				if ident, isIdent := call.Fun.(*ast.Ident); isIdent && ident.Name == "captureBatchVolume" {
					if !slices.Contains(callers, fn.Name.Name) {
						callers = append(callers, fn.Name.Name)
					}
				}
				return true
			})
		}
	}

	assert.ElementsMatch(t, capturedCallSites(), callers,
		"captureBatchVolume call sites changed: get must never report batch volume, so a new "+
			"caller (or one moved into the shared fetch/pull path) needs a deliberate decision")
}
