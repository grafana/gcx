package docs //nolint:testpackage // white-box tests call newDocsCommand without exported test constructors.

import (
	"bytes"
	"os"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/mcp-doc-server/pkg/grafanadocs"
	"github.com/stretchr/testify/require"
)

// disableAgentMode ensures tests run with agent mode off so default output
// format is "text" rather than "agents".
func disableAgentMode(t *testing.T) {
	t.Helper()
	t.Setenv("GCX_AGENT_MODE", "false")
	t.Setenv("CURSOR_AGENT", "")
	t.Setenv("CLAUDECODE", "")
	t.Setenv("CLAUDE_CODE", "")
	agent.ResetForTesting()
}

func enableAgentMode(t *testing.T) {
	t.Helper()
	t.Setenv("GCX_AGENT_MODE", "true")
	t.Setenv("CURSOR_AGENT", "")
	t.Setenv("CLAUDECODE", "")
	t.Setenv("CLAUDE_CODE", "")
	agent.ResetForTesting()
}

func loadTestIndex(t *testing.T) *grafanadocs.Index {
	t.Helper()
	f, err := os.Open("testdata/sample-index.txt")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	idx, err := grafanadocs.LoadIndexFromReader(f)
	require.NoError(t, err)
	return idx
}

// testCommand builds the docs command group and executes it with the given
// args, capturing stdout and stderr separately.
//
// A nil idx uses an uninitialized loader (shorthand resolution unavailable).
// A non-nil idx is pre-loaded so tests never hit the network for the index.
// A nil fetch uses grafanadocs.FetchDoc (for host-rejection guard tests).
func testCommand(t *testing.T, idx *grafanadocs.Index, fetch docFetcher, args ...string) (string, string, error) {
	t.Helper()
	if fetch == nil {
		fetch = grafanadocs.FetchDoc
	}
	loader := &indexLoader{}
	if idx != nil {
		loader.idx = idx
		loader.once.Do(func() {})
	}
	cmd := newDocsCommand(loader, fetch)
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}
