package loki_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	dsloki "github.com/grafana/gcx/internal/datasources/loki"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// execGuardedCmd runs a Loki command against the fake Grafana with the test-only
// policy override pointing at a policy server serving policyJSON. It returns
// stdout, stderr, the command error, and how many real (non-stats) query
// requests reached Grafana.
func execGuardedCmd(t *testing.T, agentMode bool, policyJSON string, newCmd func(*providers.ConfigLoader) *cobra.Command, use string, args ...string) (string, string, int, error) {
	t.Helper()

	policySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(policyJSON))
	}))
	defer policySrv.Close()
	dsloki.SetPolicyURLOverride(t, policySrv.URL)

	prev := agent.IsAgentMode()
	agent.SetFlag(agentMode)
	t.Cleanup(func() { agent.SetFlag(prev) })

	var indexStatsRequests, queryRequests int
	srv := newGuardTestServer(t, &indexStatsRequests, &queryRequests)
	defer srv.Close()

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(writeGuardTestConfig(t, srv.URL))

	root := &cobra.Command{Use: "test", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(newCmd(loader))
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{use}, args...))

	err := root.Execute()
	return stdout.String(), stderr.String(), queryRequests, err
}

const (
	enforcePolicy = `{"status":"success","data":{"mode":"enforce","maxBytes":1073741824,"maxRangeSeconds":86400,"source":"tenant"}}`
	shadowPolicy  = `{"status":"success","data":{"mode":"shadow","maxBytes":1073741824,"maxRangeSeconds":86400,"source":"tenant"}}`
)

func TestGuardedCmds_BlockOverBudgetBeforeTheQueryRuns(t *testing.T) {
	for name, tc := range map[string]struct {
		newCmd func(*providers.ConfigLoader) *cobra.Command
		use    string
		expr   string
	}{
		"query":   {dsloki.QueryCmd, "query", `{app="foo"}`},
		"metrics": {dsloki.MetricsCmd, "metrics", `rate({app="foo"}[5m])`},
	} {
		t.Run(name, func(t *testing.T) {
			stdout, _, queries, err := execGuardedCmd(t, true, enforcePolicy, tc.newCmd, tc.use, "-d", "loki-uid", tc.expr, "--since", "1h")

			var de gcxerrors.DetailedError
			require.ErrorAs(t, err, &de)
			assert.Equal(t, gcxerrors.SummaryQueryBlocked, de.Summary)
			assert.Contains(t, de.Details, "5.6 GiB")
			assert.Empty(t, stdout, "a blocked query must not write a result to stdout")
			assert.Zero(t, queries, "the real query must not run")
		})
	}
}

func TestGuardedCmd_ShadowModeRunsTheQuery(t *testing.T) {
	stdout, _, queries, err := execGuardedCmd(t, true, shadowPolicy, dsloki.QueryCmd, "query", "-d", "loki-uid", `{app="foo"}`, "--since", "1h")
	require.NoError(t, err)
	assert.NotEmpty(t, stdout)
	assert.Positive(t, queries)
}

func TestGuardedCmd_WithinBudgetRunsAndKeepsStderrQuiet(t *testing.T) {
	policy := `{"status":"success","data":{"mode":"enforce","maxBytes":10737418240,"maxRangeSeconds":86400}}`
	stdout, stderr, queries, err := execGuardedCmd(t, true, policy, dsloki.QueryCmd, "query", "-d", "loki-uid", `{app="foo"}`, "--since", "1h")
	require.NoError(t, err)
	assert.NotEmpty(t, stdout)
	assert.Empty(t, stderr, "an allowed query adds no output")
	assert.Positive(t, queries)
}

func TestGuardedCmd_UnparseableQueryIsLeftForLokiToReject(t *testing.T) {
	for name, agentMode := range map[string]bool{"terminal": false, "agent mode": true} {
		t.Run(name, func(t *testing.T) {
			_, stderr, queries, err := execGuardedCmd(t, agentMode, enforcePolicy, dsloki.QueryCmd, "query", "-d", "loki-uid", `not logql`, "--since", "1h")
			require.NoError(t, err)
			assert.NotContains(t, stderr, "could not estimate")
			assert.Positive(t, queries, "the query must reach Loki")
		})
	}
}

func TestGuardedCmds_ForceBypassesTheGuard(t *testing.T) {
	for name, tc := range map[string]struct {
		newCmd func(*providers.ConfigLoader) *cobra.Command
		use    string
		expr   string
	}{
		"query":   {dsloki.QueryCmd, "query", `{app="foo"}`},
		"metrics": {dsloki.MetricsCmd, "metrics", `rate({app="foo"}[5m])`},
	} {
		t.Run(name, func(t *testing.T) {
			// Control: the same invocation without --force is blocked.
			_, _, queries, err := execGuardedCmd(t, true, enforcePolicy, tc.newCmd, tc.use, "-d", "loki-uid", tc.expr, "--since", "1h")
			require.Error(t, err)
			assert.Zero(t, queries)

			stdout, _, queries, err := execGuardedCmd(t, true, enforcePolicy, tc.newCmd, tc.use, "-d", "loki-uid", tc.expr, "--since", "1h", "--force")
			require.NoError(t, err)
			assert.NotEmpty(t, stdout)
			assert.Positive(t, queries, "--force must let the real query run")
		})
	}
}

func TestGuardedCmd_AutoApproveBypassesTheGuard(t *testing.T) {
	t.Setenv("GCX_AUTO_APPROVE", "true")
	stdout, _, queries, err := execGuardedCmd(t, true, enforcePolicy, dsloki.QueryCmd, "query", "-d", "loki-uid", `{app="foo"}`, "--since", "1h")
	require.NoError(t, err)
	assert.NotEmpty(t, stdout)
	assert.Positive(t, queries)
}

func TestGuardedCmd_InvalidAutoApproveValueFails(t *testing.T) {
	t.Setenv("GCX_AUTO_APPROVE", "on")
	_, _, queries, err := execGuardedCmd(t, true, enforcePolicy, dsloki.QueryCmd, "query", "-d", "loki-uid", `{app="foo"}`, "--since", "1h")
	require.Error(t, err)
	assert.Zero(t, queries)
}
