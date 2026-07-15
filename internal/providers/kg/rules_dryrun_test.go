package kg_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/kg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rulesDryRunHandler routes the three endpoints a rules dry-run touches:
// POST <validateSuffix> (validate-only), GET <getSubstr> (fetch current config,
// remoteBody is an empty object when it does not exist), and PUT <writeSuffix>
// (the real write — recorded in writeHit so tests can assert dry-run never
// writes).
func rulesDryRunHandler(t *testing.T, validateSuffix, getSubstr, writeSuffix string, validateStatus int, validateBody, remoteBody string, writeHit *bool) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, validateSuffix):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(validateStatus)
			if validateBody != "" {
				_, _ = w.Write([]byte(validateBody))
			}
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, getSubstr):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(remoteBody))
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, writeSuffix):
			*writeHit = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func modelRulesDryRunHandler(t *testing.T, validateStatus int, validateBody, remoteBody string, writeHit *bool) http.HandlerFunc {
	t.Helper()
	return rulesDryRunHandler(t, "model-rules-validate", "/model-rules/", "model-rules", validateStatus, validateBody, remoteBody, writeHit)
}

func promRulesDryRunHandler(t *testing.T, validateStatus int, validateBody, remoteBody string, writeHit *bool) http.HandlerFunc {
	t.Helper()
	return rulesDryRunHandler(t, "prom-rules-validate-sync", "/prom-rules/", "prom-rules", validateStatus, validateBody, remoteBody, writeHit)
}

const localModelRulesYAML = `name: my-model
entities:
- name: Service
`

// --- model-rules -----------------------------------------------------------

func TestModelRulesCreate_DryRun_Invalid(t *testing.T) {
	writeHit := false
	body := `{"message":"Invalid model rules configuration","subErrors":[` +
		`{"field":"entities[0].name","message":"Missing Entity Name"}]}`
	server := httptest.NewServer(modelRulesDryRunHandler(t, http.StatusUnprocessableEntity, body, "{}", &writeHit))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	cmd := kg.NewModelRulesCommand(writeLoaderFor(server))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"upsert", "-f", "-", "--dry-run"})
	cmd.SetIn(bytes.NewBufferString(localModelRulesYAML))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Missing Entity Name")
	assert.Empty(t, stdout.String(), "no diff on stdout for invalid input")
	assert.False(t, writeHit, "dry-run must not write")
}

func TestModelRulesCreate_DryRun_Add(t *testing.T) {
	writeHit := false
	// Empty object => config does not exist remotely => "add".
	server := httptest.NewServer(modelRulesDryRunHandler(t, http.StatusOK, "", "{}", &writeHit))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	cmd := kg.NewModelRulesCommand(writeLoaderFor(server))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"upsert", "-f", "-", "--dry-run", "-o", "json"})
	cmd.SetIn(bytes.NewBufferString(localModelRulesYAML))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	require.NoError(t, cmd.Execute())

	var got kg.RulesDryRunResult
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &got))
	assert.True(t, got.Valid)
	assert.True(t, got.Changed)
	assert.Equal(t, "add", got.Action)
	assert.Equal(t, "my-model", got.Name)
	assert.Contains(t, got.Diff, "+++ local")
	assert.Contains(t, got.Diff, "Service")
	assert.Contains(t, stderr.String(), "[dry-run]")
	assert.False(t, writeHit, "dry-run must not write")
}

func TestModelRulesCreate_DryRun_Modify(t *testing.T) {
	writeHit := false
	remote := kg.ModelRules{Name: "my-model", Entities: json.RawMessage(`[{"name":"Database"}]`)}
	remoteBody, err := json.Marshal(remote)
	require.NoError(t, err)
	server := httptest.NewServer(modelRulesDryRunHandler(t, http.StatusOK, "", string(remoteBody), &writeHit))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	cmd := kg.NewModelRulesCommand(writeLoaderFor(server))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"upsert", "-f", "-", "--dry-run", "-o", "text"})
	cmd.SetIn(bytes.NewBufferString(localModelRulesYAML))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	require.NoError(t, cmd.Execute())
	assert.Contains(t, stderr.String(), "[dry-run]")
	assert.Contains(t, stderr.String(), "modify")
	assert.Contains(t, stdout.String(), "--- remote")
	assert.Contains(t, stdout.String(), "+++ local")
	assert.Contains(t, stdout.String(), "Service")
	assert.Contains(t, stdout.String(), "Database")
	assert.False(t, writeHit, "dry-run must not write")
}

func TestModelRulesCreate_DryRun_NoChanges(t *testing.T) {
	writeHit := false
	remote := kg.ModelRules{Name: "my-model", Entities: json.RawMessage(`[{"name":"Service"}]`)}
	remoteBody, err := json.Marshal(remote)
	require.NoError(t, err)
	server := httptest.NewServer(modelRulesDryRunHandler(t, http.StatusOK, "", string(remoteBody), &writeHit))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	cmd := kg.NewModelRulesCommand(writeLoaderFor(server))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"upsert", "-f", "-", "--dry-run", "-o", "text"})
	cmd.SetIn(bytes.NewBufferString(localModelRulesYAML))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	require.NoError(t, cmd.Execute())
	assert.Contains(t, stderr.String(), "no changes")
	assert.Empty(t, stdout.String(), "no diff on stdout when there are no changes")
	assert.False(t, writeHit, "dry-run must not write")
}

// System-managed fields (managedBy) are populated by the backend, not the input
// file, so a config whose user fields match remote must report no change even
// when remote carries a managedBy the file omits.
func TestModelRulesCreate_DryRun_SystemFieldsIgnored(t *testing.T) {
	writeHit := false
	remote := kg.ModelRules{Name: "my-model", Entities: json.RawMessage(`[{"name":"Service"}]`), ManagedBy: "terraform"}
	remoteBody, err := json.Marshal(remote)
	require.NoError(t, err)
	server := httptest.NewServer(modelRulesDryRunHandler(t, http.StatusOK, "", string(remoteBody), &writeHit))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	cmd := kg.NewModelRulesCommand(writeLoaderFor(server))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"upsert", "-f", "-", "--dry-run", "-o", "json"})
	cmd.SetIn(bytes.NewBufferString(localModelRulesYAML))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	require.NoError(t, cmd.Execute())

	var got kg.RulesDryRunResult
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &got))
	assert.False(t, got.Changed, "managedBy is system-managed and must not count as a change")
	assert.Equal(t, "none", got.Action)
	assert.Empty(t, got.Diff)
	assert.False(t, writeHit, "dry-run must not write")
}

// --- prom-rules ------------------------------------------------------------

// localPromRulesYAML uses `for:` — the JSON-tag-only field that yaml.v3 would
// mishandle — so the no-change test doubles as a guard on the sigs.k8s.io/yaml
// parse path.
const localPromRulesYAML = `name: my-rules
groups:
- name: g1
  rules:
  - alert: HighErrors
    expr: rate(errors[5m]) > 0
    for: 5m
`

func TestPromRulesCreate_DryRun_Invalid(t *testing.T) {
	writeHit := false
	body := `{"message":"Invalid prom rules configuration","subErrors":[` +
		`{"field":"groups[0].rules[0].expr","message":"Invalid PromQL"}]}`
	server := httptest.NewServer(promRulesDryRunHandler(t, http.StatusUnprocessableEntity, body, "{}", &writeHit))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	cmd := kg.NewPromRulesCommand(writeLoaderFor(server))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"upsert", "-f", "-", "--dry-run"})
	cmd.SetIn(bytes.NewBufferString(localPromRulesYAML))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid PromQL")
	assert.Empty(t, stdout.String(), "no diff on stdout for invalid input")
	assert.False(t, writeHit, "dry-run must not write")
}

func TestPromRulesCreate_DryRun_Add(t *testing.T) {
	writeHit := false
	// Backend returns an empty object (200) for a missing name => "add".
	server := httptest.NewServer(promRulesDryRunHandler(t, http.StatusOK, "", "{}", &writeHit))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	cmd := kg.NewPromRulesCommand(writeLoaderFor(server))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"upsert", "-f", "-", "--dry-run", "-o", "json"})
	cmd.SetIn(bytes.NewBufferString(localPromRulesYAML))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	require.NoError(t, cmd.Execute())

	var got kg.RulesDryRunResult
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &got))
	assert.True(t, got.Valid)
	assert.True(t, got.Changed)
	assert.Equal(t, "add", got.Action)
	assert.Equal(t, "my-rules", got.Name)
	// `for:` must survive parsing (proves the sigs.k8s.io/yaml path honors JSON tags).
	assert.Contains(t, got.Diff, "for: 5m")
	assert.False(t, writeHit, "dry-run must not write")
}

func TestPromRulesCreate_DryRun_Modify(t *testing.T) {
	writeHit := false
	remote := kg.Rule{Name: "my-rules", Groups: []kg.RuleGroup{{
		Name:  "g1",
		Rules: []kg.PromRule{{Alert: "HighErrors", Expr: "rate(errors[5m]) > 0", Duration: "10m"}},
	}}}
	remoteBody, err := json.Marshal(remote)
	require.NoError(t, err)
	server := httptest.NewServer(promRulesDryRunHandler(t, http.StatusOK, "", string(remoteBody), &writeHit))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	cmd := kg.NewPromRulesCommand(writeLoaderFor(server))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"upsert", "-f", "-", "--dry-run", "-o", "text"})
	cmd.SetIn(bytes.NewBufferString(localPromRulesYAML))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	require.NoError(t, cmd.Execute())
	assert.Contains(t, stderr.String(), "modify")
	assert.Contains(t, stdout.String(), "-    for: 10m")
	assert.Contains(t, stdout.String(), "+    for: 5m")
	assert.False(t, writeHit, "dry-run must not write")
}

func TestPromRulesCreate_DryRun_NoChanges(t *testing.T) {
	writeHit := false
	remote := kg.Rule{Name: "my-rules", Groups: []kg.RuleGroup{{
		Name:  "g1",
		Rules: []kg.PromRule{{Alert: "HighErrors", Expr: "rate(errors[5m]) > 0", Duration: "5m"}},
	}}}
	remoteBody, err := json.Marshal(remote)
	require.NoError(t, err)
	server := httptest.NewServer(promRulesDryRunHandler(t, http.StatusOK, "", string(remoteBody), &writeHit))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	cmd := kg.NewPromRulesCommand(writeLoaderFor(server))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"upsert", "-f", "-", "--dry-run", "-o", "text"})
	cmd.SetIn(bytes.NewBufferString(localPromRulesYAML))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	require.NoError(t, cmd.Execute())
	assert.Contains(t, stderr.String(), "no changes")
	assert.Empty(t, stdout.String(), "identical config (including for:) must produce no diff")
	assert.False(t, writeHit, "dry-run must not write")
}
