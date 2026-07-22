//nolint:testpackage // white-box: apiOpts and outputResponse are unexported
package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

// These tests pin the agent output contract for `gcx api` (#387): the
// finite path (JSON-parseable responses) emits exactly one JSON value on
// stdout through the codec system, with the agents codec as the agent-mode
// default; non-JSON bodies keep the declared raw passthrough protocol; and
// the -o usage string advertises the full accepted format menu.

// setAgentMode toggles agent mode for the duration of the test. It must run
// BEFORE flags are bound — the shared output flags resolve their default
// format at BindFlags time.
func setAgentMode(t *testing.T, enabled bool) {
	t.Helper()
	agent.SetFlag(enabled)
	t.Cleanup(func() { agent.SetFlag(false) })
}

// boundAPIOpts builds apiOpts with flags bound under the current agent mode,
// mirroring Command()'s setup.
func boundAPIOpts(t *testing.T, args ...string) *apiOpts {
	t.Helper()
	opts := &apiOpts{}
	flags := pflag.NewFlagSet("api", pflag.ContinueOnError)
	opts.setup(flags)
	require.NoError(t, flags.Parse(args))
	require.NoError(t, opts.Validate())
	return opts
}

func TestOutputFlagUsage_AdvertisesFullFormatMenu(t *testing.T) {
	setAgentMode(t, false)

	cmd := Command()
	flag := cmd.Flags().Lookup("output")
	require.NotNil(t, flag)

	// The usage must clarify the JSON-responses scope without understating
	// the accepted codec registry (agents is a valid, working -o value).
	require.Equal(t, "Output format for JSON responses. One of: agents, json, yaml", flag.Usage)
}

func TestOutputResponse_AgentModeSingleJSONDocument(t *testing.T) {
	setAgentMode(t, true)
	opts := boundAPIOpts(t)
	require.Equal(t, "agents", opts.IO.OutputFormat)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"status":"ok","version":"12.0.0"}`)),
	}

	var stdout bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&stdout)
	cmd.SetErr(io.Discard)
	opts.IO.ErrWriter = io.Discard

	require.NoError(t, outputResponse(cmd, opts, resp))

	dec := json.NewDecoder(strings.NewReader(stdout.String()))
	var first any
	require.NoError(t, dec.Decode(&first), "stdout is not valid JSON:\n%s", stdout.String())
	var second any
	require.ErrorIs(t, dec.Decode(&second), io.EOF,
		"stdout must contain exactly one JSON value:\n%s", stdout.String())

	obj, ok := first.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "ok", obj["status"])
}

func TestOutputResponse_ExplicitOutputOverridesAgentDefault(t *testing.T) {
	setAgentMode(t, true)
	opts := boundAPIOpts(t, "-o", "yaml")
	require.Equal(t, "yaml", opts.IO.OutputFormat)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"status":"ok"}`)),
	}

	var stdout bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&stdout)
	cmd.SetErr(io.Discard)

	require.NoError(t, outputResponse(cmd, opts, resp))
	require.Equal(t, "status: ok\n", stdout.String())
}

func TestOutputResponse_RawPassthroughUnchangedInAgentMode(t *testing.T) {
	// Declared protocol: non-JSON bodies pass through verbatim, even in
	// agent mode — `gcx api` is a raw passthrough for those responses.
	setAgentMode(t, true)
	opts := boundAPIOpts(t)

	raw := "# HELP up Whether the scrape target is healthy\nup 1\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(raw)),
	}

	var stdout bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&stdout)
	cmd.SetErr(io.Discard)
	opts.IO.ErrWriter = io.Discard

	require.NoError(t, outputResponse(cmd, opts, resp))
	require.Equal(t, raw, stdout.String())
}
