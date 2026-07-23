package mcpservers //nolint:testpackage

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/assistant/mcpserver"
	assistantmcp "github.com/grafana/gcx/internal/assistant/mcpservers"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

// Agent-output-contract tests for the mcp-servers command family: envelope
// parity for the agents format, mutation-result ordering when the post-write
// OAuth check fails, browser gating in agent mode, and the cancelled-exit
// convention for a declined delete prompt.

const contractBasePath = "/api/plugins/grafana-assistant-app/resources/api/v1"

// setAgentModeMCP forces agent mode on or off for the duration of the test.
// Must be called BEFORE opts.setup: the agents default-format override is
// resolved in Options.BindFlags at flag-binding time.
func setAgentModeMCP(t *testing.T, enabled bool) {
	t.Helper()
	agent.SetFlag(enabled)
	t.Cleanup(agent.ResetForTesting)
}

// newRoutedClient serves the given mux and returns a client pointed at it.
func newRoutedClient(t *testing.T, mux *http.ServeMux) *assistantmcp.Client {
	t.Helper()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	cfg := config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}, Namespace: "default"}
	base, err := assistanthttp.NewClient(cfg)
	require.NoError(t, err)
	return assistantmcp.NewClient(base)
}

// TestAgentsFormatIsEnvelopeFormat pins the isEnvelopeFormat fix: agents is a
// JSON-family format and must resolve to the resources-parity envelope, and
// it is exactly what the agent-mode default resolves to.
func TestAgentsFormatIsEnvelopeFormat(t *testing.T) {
	assert.True(t, isEnvelopeFormat("json"))
	assert.True(t, isEnvelopeFormat("yaml"))
	assert.True(t, isEnvelopeFormat("agents"))
	assert.False(t, isEnvelopeFormat("text"))
	assert.False(t, isEnvelopeFormat("table"))
	assert.False(t, isEnvelopeFormat("wide"))

	setAgentModeMCP(t, true)
	listOptions := &listOpts{}
	listOptions.setup(pflag.NewFlagSet("list", pflag.ContinueOnError))
	assert.Equal(t, "agents", listOptions.IO.OutputFormat, "agent-mode default must resolve to agents")
	assert.True(t, isEnvelopeFormat(listOptions.IO.OutputFormat))

	getOptions := &getOpts{}
	getOptions.setup(pflag.NewFlagSet("get", pflag.ContinueOnError))
	assert.Equal(t, "agents", getOptions.IO.OutputFormat)
}

// TestListAgentsOutputMatchesJSONEnvelope: the agent-mode default (agents
// codec) must emit the SAME {items: [envelope]} value as explicit -o json —
// one JSON value on stdout, resources-parity shape, not the flat Server view.
func TestListAgentsOutputMatchesJSONEnvelope(t *testing.T) {
	setAgentModeMCP(t, true)
	integrations := []map[string]any{
		{"id": "srv-user1", "name": "My Server", "type": "mcp", "enabled": true, "scope": "user",
			"configuration": map[string]any{"url": "https://mcp.example.com/user"}},
		{"id": "srv-abc123", "name": "GitHub", "type": "mcp", "enabled": true, "scope": "tenant",
			"configuration":  map[string]any{"url": "https://api.githubcopilot.com/mcp/"},
			"custom_headers": []map[string]any{{"name": "Authorization"}}},
	}

	encode := func(outputFormat string) string {
		client := newExistingResultTestClient(t, integrations)
		opts := &listOpts{}
		opts.setup(pflag.NewFlagSet("list", pflag.ContinueOnError))
		opts.IO.OutputFormat = outputFormat
		opts.Limit = 50

		cmd := &cobra.Command{}
		cmd.SetContext(t.Context())
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&bytes.Buffer{})
		require.NoError(t, runList(cmd, client, "default", opts))
		return out.String()
	}

	agentsOut := encode("agents")
	jsonOut := encode("json")

	// Exactly one JSON value on stdout for the agents format.
	dec := json.NewDecoder(strings.NewReader(agentsOut))
	var agentsDoc map[string]any
	require.NoError(t, dec.Decode(&agentsDoc))
	assert.False(t, dec.More(), "agents output must be exactly one JSON value")

	var jsonDoc map[string]any
	require.NoError(t, json.Unmarshal([]byte(jsonOut), &jsonDoc))
	assert.Equal(t, jsonDoc, agentsDoc, "agents format must carry the same envelope value as -o json")

	items, ok := agentsDoc["items"].([]any)
	require.True(t, ok, "agents output must be the {items: [...]} envelope, got: %s", agentsOut)
	require.Len(t, items, 2)
	first, ok := items[0].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, first, "apiVersion")
	assert.Contains(t, first, "kind")
	assert.Contains(t, first, "metadata")
	assert.Contains(t, first, "spec")
}

// TestGetAgentsOutputMatchesJSONEnvelope: same parity fix for get — the
// agents format carries the {apiVersion, kind, metadata, spec} envelope.
func TestGetAgentsOutputMatchesJSONEnvelope(t *testing.T) {
	setAgentModeMCP(t, true)
	integrations := []map[string]any{
		{"id": "srv-abc123", "name": "GitHub", "type": "mcp", "enabled": true, "scope": "tenant",
			"configuration":  map[string]any{"url": "https://api.githubcopilot.com/mcp/"},
			"custom_headers": []map[string]any{{"name": "Authorization"}}},
	}

	encode := func(outputFormat string) string {
		client := newParityTestClient(t, integrations)
		opts := &getOpts{}
		opts.setup(pflag.NewFlagSet("get", pflag.ContinueOnError))
		opts.IO.OutputFormat = outputFormat

		cmd := &cobra.Command{}
		cmd.SetContext(t.Context())
		var out bytes.Buffer
		cmd.SetOut(&out)
		require.NoError(t, runGet(cmd, client, "default", opts, "GitHub"))
		return out.String()
	}

	var agentsDoc, jsonDoc map[string]any
	require.NoError(t, json.Unmarshal([]byte(encode("agents")), &agentsDoc))
	require.NoError(t, json.Unmarshal([]byte(encode("json")), &jsonDoc))
	assert.Equal(t, jsonDoc, agentsDoc)
	assert.Contains(t, agentsDoc, "apiVersion")
	assert.Contains(t, agentsDoc, "spec")
}

// newValidateFixtureClient serves a create/update-shaped backend whose
// /validate endpoint behaves per validateHandler. Recording browser opens
// (via the openURL seam) is left to the caller.
func newValidateFixtureClient(t *testing.T, validateHandler http.HandlerFunc) *assistantmcp.Client {
	t.Helper()
	created := map[string]any{
		"id": "srv-new", "name": "GitHub", "type": "mcp", "enabled": true, "scope": "user",
		"configuration": map[string]any{"url": "https://api.githubcopilot.com/mcp"},
	}
	encode := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(v); err != nil {
			t.Errorf("failed to encode fixture response: %v", err)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc(contractBasePath+"/integrations", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			encode(w, map[string]any{"data": map[string]any{"integration": created}})
			return
		}
		encode(w, map[string]any{"data": map[string]any{"integrations": []map[string]any{}}})
	})
	mux.HandleFunc(contractBasePath+"/integrations/srv-new/validate", validateHandler)
	mux.HandleFunc(contractBasePath+"/integrations/oauth/initiate", func(w http.ResponseWriter, _ *http.Request) {
		encode(w, map[string]any{"data": map[string]any{"auth_url": "https://example.com/oauth", "state": "s"}})
	})
	return newRoutedClient(t, mux)
}

// TestRunCreateEmitsResultThenPartialFailureWhenOAuthCheckFails is the
// exit-code-mismatch fix, end to end: the server IS created, the follow-up
// OAuth requirement check fails, and the command must still emit the created
// result document on stdout — now carrying the failure summary in-band on
// the document's `error` member, so a consumer reading only stdout sees why
// the exit code is ExitPartialFailure — then exit via EmittedError (typed
// stderr warning, no second stdout document). Previously it exited non-zero
// with an empty stdout although the create had persisted.
func TestRunCreateEmitsResultThenPartialFailureWhenOAuthCheckFails(t *testing.T) {
	setAgentModeMCP(t, false)
	client := newValidateFixtureClient(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"validation backend unavailable"}`, http.StatusBadGateway)
	})
	crud := mcpserver.NewTypedCRUDForClient(client, "default")

	var opened []string
	origOpenURL := openURL
	openURL = func(u string) (bool, error) { opened = append(opened, u); return true, nil }
	t.Cleanup(func() { openURL = origOpenURL })

	opts := &createOpts{}
	opts.setup(pflag.NewFlagSet("create", pflag.ContinueOnError))
	opts.IO.OutputFormat = "json"

	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	manifest := mcpserver.MCPServer{Name: "GitHub", Scope: "user", URL: "https://api.githubcopilot.com/mcp", Enabled: true}
	err := runCreate(cmd, crud, client, opts, manifest)

	var emitted *gcxerrors.EmittedError
	require.ErrorAs(t, err, &emitted)
	assert.Equal(t, gcxerrors.ExitPartialFailure, emitted.Code)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &doc), "the result document must be on stdout: %s", out.String())
	assert.Equal(t, "created", doc["operation"])
	assert.NotContains(t, doc, "authUrl")

	// The exit-4 reason is in-band: the document's error member carries the
	// OAuth-check failure summary.
	errMsg, ok := doc["error"].(string)
	require.True(t, ok, "the OAuth-check failure must be in-band on the document: %s", out.String())
	assert.Contains(t, errMsg, "MCP server created, but the OAuth requirement check failed")

	assert.Contains(t, errOut.String(), "OAuth requirement check failed")
	assert.Empty(t, opened, "no browser open when the OAuth check failed")
}

// TestFinishMutationAgentModeSkipsBrowserAndEmitsHint is the browser-gate
// contract on the unified path: the gate is the single shared deeplink guard
// (deeplink.OpenWithStatus behind the openURL seam), not a bespoke agent
// branch here. With the REAL guard in place (no stub), agent mode must not
// launch a browser — the guard skips the exec, emits its typed hint on the
// process stderr, and reports (opened=false, nil) — while the auth URL
// reaches the agent in-band (authUrl in the stdout document) and the command
// still succeeds.
func TestFinishMutationAgentModeSkipsBrowserAndEmitsHint(t *testing.T) {
	setAgentModeMCP(t, true)
	client := newValidateFixtureClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"result": map[string]any{"status": assistantmcp.ValidationStatusOAuthRequired}},
		}); err != nil {
			t.Errorf("failed to encode validate response: %v", err)
		}
	})

	// Deliberately no openURL stub: the real deeplink guard is under test.
	// Its typed hint goes to the process stderr, so capture os.Stderr.
	origStderr := os.Stderr
	r, w, pipeErr := os.Pipe()
	require.NoError(t, pipeErr)
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = origStderr })

	ioOpts := cmdio.Options{OutputFormat: "json"}
	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	result := &assistantmcp.MutationResult{
		Operation: "created",
		Server:    &assistantmcp.Server{ID: "srv-new", Name: "GitHub", Scope: "user"},
	}
	finishErr := finishMutation(cmd, client, &ioOpts, result)

	require.NoError(t, w.Close())
	os.Stderr = origStderr
	captured, readErr := io.ReadAll(r)
	require.NoError(t, readErr)

	require.NoError(t, finishErr)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &doc))
	assert.Equal(t, "https://example.com/oauth", doc["authUrl"], "the auth URL must reach the agent in-band")
	assert.NotContains(t, doc, "error", "a successful mutation carries no in-band error")

	// The deeplink guard skipped the launch: its typed hint (only emitted on
	// the skip path — the launch path would have exec'd a browser) is the
	// observable proof, JSONL {"class":"hint"} carrying the URL. Other agent
	// hints (e.g. the codec's --jq hint) may share the stream, so scan lines.
	var hint map[string]any
	for line := range strings.Lines(string(captured)) {
		var doc map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &doc), "agent-mode stderr diagnostics must be JSONL: %s", captured)
		if s, _ := doc["summary"].(string); strings.Contains(s, "browser launch skipped in agent mode") {
			hint = doc
			break
		}
	}
	require.NotNil(t, hint, "the deeplink skip hint must be on stderr: %s", captured)
	assert.Equal(t, "hint", hint["class"])
	assert.Equal(t, "https://example.com/oauth", hint["command"])

	// No human "Opening ..." prose leaks onto the command stderr.
	assert.NotContains(t, errOut.String(), "Opening OAuth authorization URL")
}

// TestFinishMutationHumanModeStillOpensBrowser guards the human default: the
// OAuth URL keeps opening in a browser outside agent mode (via the deeplink
// seam), with the same stderr prose as before.
func TestFinishMutationHumanModeStillOpensBrowser(t *testing.T) {
	setAgentModeMCP(t, false)
	client := newValidateFixtureClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"result": map[string]any{"status": assistantmcp.ValidationStatusOAuthRequired}},
		}); err != nil {
			t.Errorf("failed to encode validate response: %v", err)
		}
	})

	var opened []string
	origOpenURL := openURL
	openURL = func(u string) (bool, error) { opened = append(opened, u); return true, nil }
	t.Cleanup(func() { openURL = origOpenURL })

	ioOpts := cmdio.Options{OutputFormat: "yaml"}
	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	result := &assistantmcp.MutationResult{
		Operation: "updated",
		Server:    &assistantmcp.Server{ID: "srv-new", Name: "GitHub", Scope: "user"},
	}
	require.NoError(t, finishMutation(cmd, client, &ioOpts, result))

	assert.Equal(t, []string{"https://example.com/oauth"}, opened)
	assert.Contains(t, errOut.String(), "Opening OAuth authorization URL")
	assert.Contains(t, out.String(), "operation: updated")
	assert.NotContains(t, out.String(), "error:")
}

// TestFinishMutationUpdatePartialFailure covers the shared tail from the
// update side: an updated result document — carrying the OAuth-check failure
// summary in-band on its `error` member — followed by ExitPartialFailure when
// the OAuth check errors.
func TestFinishMutationUpdatePartialFailure(t *testing.T) {
	setAgentModeMCP(t, false)
	client := newValidateFixtureClient(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"boom"}`, http.StatusInternalServerError)
	})

	ioOpts := cmdio.Options{OutputFormat: "json"}
	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	result := &assistantmcp.MutationResult{
		Operation: "updated",
		Server:    &assistantmcp.Server{ID: "srv-new", Name: "GitHub", Scope: "user"},
	}
	err := finishMutation(cmd, client, &ioOpts, result)

	var emitted *gcxerrors.EmittedError
	require.ErrorAs(t, err, &emitted)
	assert.Equal(t, gcxerrors.ExitPartialFailure, emitted.Code)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &doc))
	assert.Equal(t, "updated", doc["operation"])
	errMsg, ok := doc["error"].(string)
	require.True(t, ok, "the OAuth-check failure must be in-band on the document: %s", out.String())
	assert.Contains(t, errMsg, "MCP server updated, but the OAuth requirement check failed")
	assert.Contains(t, errOut.String(), "MCP server updated, but the OAuth requirement check failed")
}

// TestFinishMutationNoOAuthNeededIsPlainSuccess: the common path — no OAuth
// requirement — stays a single result document and exit 0.
func TestFinishMutationNoOAuthNeededIsPlainSuccess(t *testing.T) {
	setAgentModeMCP(t, false)
	client := newValidateFixtureClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"result": map[string]any{"status": "ok"}},
		}); err != nil {
			t.Errorf("failed to encode validate response: %v", err)
		}
	})

	var opened []string
	origOpenURL := openURL
	openURL = func(u string) (bool, error) { opened = append(opened, u); return true, nil }
	t.Cleanup(func() { openURL = origOpenURL })

	ioOpts := cmdio.Options{OutputFormat: "json"}
	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})

	result := &assistantmcp.MutationResult{
		Operation: "created",
		Server:    &assistantmcp.Server{ID: "srv-new", Name: "GitHub", Scope: "user"},
	}
	require.NoError(t, finishMutation(cmd, client, &ioOpts, result))
	assert.Empty(t, opened)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &doc))
	assert.Equal(t, "created", doc["operation"])
	assert.NotContains(t, doc, "authUrl")
	assert.NotContains(t, doc, "error")
}
