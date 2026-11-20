package changereview //nolint:testpackage // exercises unexported window resolution.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	client "github.com/grafana/gcx/internal/assistant/changereview"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const checksRoute = "/api/plugins/grafana-assistant-app/resources/api/v1/github/review-checks"

// fakeChecks serves the checks API: a GET returns the stored checks and a PUT
// replaces them when its expected revision is current.
type fakeChecks struct {
	mu        sync.Mutex
	revision  int
	checks    []client.Check
	conflicts int // PUTs to refuse with 409 before accepting one
	puts      []map[string]any
	requests  []string
}

func (f *fakeChecks) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	if r.URL.Path != checksRoute {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeData(w, client.Checks{Revision: strconv.Itoa(f.revision), Checks: f.checks})
	case http.MethodPut:
		var body struct {
			ExpectedRevision string         `json:"expectedRevision"`
			Checks           []client.Check `json:"checks"`
		}
		raw := map[string]any{}
		data := new(bytes.Buffer)
		_, _ = data.ReadFrom(r.Body)
		_ = json.Unmarshal(data.Bytes(), &raw)
		_ = json.Unmarshal(data.Bytes(), &body)
		f.puts = append(f.puts, raw)
		if f.conflicts > 0 || body.ExpectedRevision != strconv.Itoa(f.revision) {
			f.conflicts--
			f.revision++ // someone else saved
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"message":"Change review checks changed; reload and try again"}`))
			return
		}
		for i := range body.Checks {
			if body.Checks[i].ID == "" {
				body.Checks[i].ID = strings.ToLower(strings.ReplaceAll(body.Checks[i].Name, " ", "-"))
			}
		}
		f.checks = body.Checks
		f.revision++
		writeData(w, client.Checks{Revision: strconv.Itoa(f.revision), Checks: f.checks})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func writeData(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": v}) //nolint:errchkjson // test fixture
}

func builtIns() []client.Check {
	return []client.Check{
		{ID: "security", BuiltIn: true, Name: "Security", Description: "Finds security issues", Enabled: true, Blocking: true, Paths: []string{}},
		{ID: "performance", BuiltIn: true, Name: "Performance", Enabled: true, Paths: []string{}},
	}
}

func loaderFor(t *testing.T, server *httptest.Server) *providers.ConfigLoader {
	t.Helper()
	for _, key := range []string{"GRAFANA_SERVER", "GRAFANA_TOKEN", "GRAFANA_USER", "GRAFANA_PASSWORD", "GRAFANA_ORG_ID", "GRAFANA_STACK_ID"} {
		t.Setenv(key, "")
		require.NoError(t, os.Unsetenv(key))
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	contents := fmt.Sprintf(`version: 1
stacks:
  test:
    grafana:
      server: %s
      token: test-token
      stack-id: 12345
contexts:
  test:
    stack: test
current-context: test
`, server.URL)
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(path)
	return loader
}

func run(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(t.Context())
	return out.String(), err
}

func TestListShowsBuiltInAndCustomChecks(t *testing.T) {
	fake := &fakeChecks{checks: append(builtIns(), client.Check{ID: "migrations", Name: "Migrations", Instructions: "x", Enabled: false, Paths: []string{"db/**"}})}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)

	out, err := run(t, newListCommand(loaderFor(t, server)), "-o", "text")
	require.NoError(t, err)
	assert.Contains(t, out, "security")
	assert.Contains(t, out, "built-in")
	assert.Contains(t, out, "migrations")
	assert.Contains(t, out, "custom")
	assert.Contains(t, out, "db/**")

	out, err = run(t, newListCommand(loaderFor(t, server)), "-o", "json")
	require.NoError(t, err)
	var list checkList
	require.NoError(t, json.Unmarshal([]byte(out), &list))
	assert.Len(t, list.Checks, 3)
}

func TestUpdateChangesOnlySetFieldsAndSendsRevision(t *testing.T) {
	fake := &fakeChecks{revision: 7, checks: builtIns()}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)

	out, err := run(t, newUpdateCommand(loaderFor(t, server)), "performance", "--blocking", "--path", "api/**", "-o", "json")
	require.NoError(t, err)

	var result mutationResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	assert.Equal(t, "updated", result.Action)
	assert.True(t, result.Check.Blocking)
	assert.True(t, result.Check.Enabled, "enabled was not set and must keep its value")
	assert.Equal(t, []string{"api/**"}, result.Check.Paths)

	require.Len(t, fake.puts, 1)
	assert.Equal(t, "7", fake.puts[0]["expectedRevision"])
	sent, ok := fake.puts[0]["checks"].([]any)
	require.True(t, ok)
	require.Len(t, sent, 2, "a save sends every check so none is dropped")
	assert.NotContains(t, sent[0], "description", "description is read-only")
	assert.True(t, fake.checks[0].Blocking, "security was untouched")
}

func TestUpdateRetriesOnceAfterConcurrentSave(t *testing.T) {
	fake := &fakeChecks{checks: builtIns(), conflicts: 1}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)

	_, err := run(t, newUpdateCommand(loaderFor(t, server)), "performance", "--enabled=false")
	require.NoError(t, err)
	assert.Len(t, fake.puts, 2)
	assert.False(t, fake.checks[1].Enabled)
}

func TestUpdateReportsRepeatedConflict(t *testing.T) {
	fake := &fakeChecks{checks: builtIns(), conflicts: 2}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)

	_, err := run(t, newUpdateCommand(loaderFor(t, server)), "performance", "--enabled=false")
	require.ErrorIs(t, err, client.ErrConflict)
	assert.True(t, fake.checks[1].Enabled, "nothing was saved")
}

func TestUpdateRejectsBuiltInRenameWithoutSaving(t *testing.T) {
	fake := &fakeChecks{checks: builtIns()}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)

	_, err := run(t, newUpdateCommand(loaderFor(t, server)), "security", "--name", "Sec")
	require.ErrorContains(t, err, "keeps its own name")
	assert.Empty(t, fake.puts)

	_, err = run(t, newUpdateCommand(loaderFor(t, server)), "missing", "--blocking")
	require.ErrorContains(t, err, "not found")
	assert.Empty(t, fake.puts)
}

func TestUpdateFromGetOutputRoundTrips(t *testing.T) {
	fake := &fakeChecks{checks: append(builtIns(), client.Check{ID: "migrations", Name: "Migrations", Instructions: "old", Enabled: true, Paths: []string{}})}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)

	out, err := run(t, newGetCommand(loaderFor(t, server)), "migrations", "-o", "yaml")
	require.NoError(t, err)
	file := filepath.Join(t.TempDir(), "check.yaml")
	require.NoError(t, os.WriteFile(file, []byte(strings.Replace(out, "instructions: old", "instructions: new", 1)), 0o600))

	_, err = run(t, newUpdateCommand(loaderFor(t, server)), "migrations", "-f", file, "--blocking")
	require.NoError(t, err)
	assert.Equal(t, "new", fake.checks[2].Instructions)
	assert.True(t, fake.checks[2].Blocking, "flags apply on top of the file")

	_, err = run(t, newUpdateCommand(loaderFor(t, server)), "security", "-f", file)
	require.ErrorContains(t, err, "describes check \"migrations\"")
}

func TestCreateAppendsCustomCheck(t *testing.T) {
	fake := &fakeChecks{checks: builtIns()}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)

	out, err := run(t, newCreateCommand(loaderFor(t, server)), "--name", "Migration safety", "--instructions", "Flag locking migrations.", "-o", "json")
	require.NoError(t, err)
	var result mutationResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	assert.Equal(t, "created", result.Action)
	assert.Equal(t, "gcx.mutation", result.Type)
	assert.Equal(t, "1", result.SchemaVersion)
	assert.Equal(t, "migration-safety", result.Target.ID)
	assert.Contains(t, out, `"type": "gcx.mutation"`, "discriminators are top-level fields")
	assert.Equal(t, "migration-safety", result.Check.ID)
	assert.True(t, result.Check.Enabled, "new checks are enabled by default")
	assert.Len(t, fake.checks, 3)

	_, err = run(t, newCreateCommand(loaderFor(t, server)), "--name", "No instructions")
	require.ErrorContains(t, err, "needs --name and --instructions")

	_, err = run(t, newCreateCommand(loaderFor(t, server)), "--name", "x", "--instructions", "y", "--applies-to", "deploy")
	require.ErrorContains(t, err, "invalid --applies-to \"deploy\"")
}

func TestDeleteRemovesOnlyCustomChecks(t *testing.T) {
	fake := &fakeChecks{checks: append(builtIns(), client.Check{ID: "migrations", Name: "Migrations", Instructions: "x", Enabled: true})}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)

	_, err := run(t, newDeleteCommand(loaderFor(t, server)), "security", "--force")
	require.ErrorContains(t, err, "Built-in checks cannot be deleted")
	assert.Empty(t, fake.puts)

	_, err = run(t, newDeleteCommand(loaderFor(t, server)), "migrations", "--force")
	require.NoError(t, err)
	assert.Len(t, fake.checks, 2)
}

func TestStatsSendsWindowAndRepository(t *testing.T) {
	var query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		writeData(w, client.Stats{
			Window: client.Window{From: time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)},
			Totals: client.Totals{Reviews: 3, PullRequests: 2, Confirmed: 4, Fixed: 1},
			Checks: []client.CheckStats{{ID: "security", Title: "Security", Confirmed: 4, Fixed: 1}},
		})
	}))
	t.Cleanup(server.Close)

	out, err := run(t, newStatsCommand(loaderFor(t, server)), "--since", "7d", "--repository", "grafana/gcx", "-o", "text")
	require.NoError(t, err)
	assert.Contains(t, query, "repository=grafana%2Fgcx")
	assert.Contains(t, query, "from=")
	assert.Contains(t, out, "Reviews: 3 on 2 pull requests")
	assert.Contains(t, out, "security")

	out, err = run(t, newStatsCommand(loaderFor(t, server)), "-o", "json")
	require.NoError(t, err)
	assert.Contains(t, out, `"repositories": []`, "empty arrays stay arrays")
}

func TestStatsNotEnabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	_, err := run(t, newStatsCommand(loaderFor(t, server)))
	require.ErrorContains(t, err, "not enabled")
}

func TestWindowResolve(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		args     []string
		wantErr  string
		wantFrom time.Time
		wantTo   time.Time
	}{
		{name: "default 30d", wantFrom: now.Add(-30 * 24 * time.Hour), wantTo: now},
		{name: "since", args: []string{"--since", "7d"}, wantFrom: now.Add(-7 * 24 * time.Hour), wantTo: now},
		{name: "since over 90d", args: []string{"--since", "91d"}, wantErr: "at most 90d"},
		{name: "since and from", args: []string{"--since", "7d", "--from", "now-1d"}, wantErr: "cannot be used together"},
		{name: "explicit empty from", args: []string{"--from", ""}, wantErr: "--from cannot be empty"},
		{name: "from after to", args: []string{"--from", "now-1d", "--to", "now-2d"}, wantErr: "before --to"},
		{name: "window over 90d", args: []string{"--from", "now-100d", "--to", "now"}, wantErr: "at most 90 days"},
		{name: "only to", args: []string{"--to", "now-1d"}, wantFrom: now.Add(-31 * 24 * time.Hour), wantTo: now.Add(-24 * time.Hour)},
		{name: "only from defaults to now", args: []string{"--from", "now-7d"}, wantFrom: now.Add(-7 * 24 * time.Hour), wantTo: now},
		{name: "only from over 90d", args: []string{"--from", "2024-01-01T00:00:00Z"}, wantErr: "at most 90 days"},
		{name: "empty repository", args: []string{"--repository", ""}, wantErr: "invalid --repository"},
		{name: "repository without owner", args: []string{"--repository", "gcx"}, wantErr: "invalid --repository"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var w windowFlags
			cmd := &cobra.Command{Use: "x"}
			w.bind(cmd.Flags())
			require.NoError(t, cmd.Flags().Parse(tc.args))
			q, err := w.resolve(cmd, now)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantFrom, q.From)
			assert.Equal(t, tc.wantTo, q.To)
		})
	}
}

func TestInvalidInputIsUsageError(t *testing.T) {
	fake := &fakeChecks{checks: builtIns()}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	loader := loaderFor(t, server)

	tests := []struct {
		name string
		cmd  *cobra.Command
		args []string
	}{
		{name: "since over 90d", cmd: newStatsCommand(loader), args: []string{"--since", "91d"}},
		{name: "bad repository", cmd: newListPullRequestsCommand(loader), args: []string{"security", "--repository", "gcx"}},
		{name: "bad applies-to", cmd: newCreateCommand(loader), args: []string{"--name", "x", "--instructions", "y", "--applies-to", "deploy"}},
		{name: "missing instructions", cmd: newCreateCommand(loader), args: []string{"--name", "x"}},
		{name: "built-in rename", cmd: newUpdateCommand(loader), args: []string{"security", "--name", "Sec"}},
		{name: "empty check ID", cmd: newGetCommand(loader), args: []string{" "}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := run(t, tc.cmd, tc.args...)
			var usage *gcxerrors.UsageError
			require.ErrorAs(t, err, &usage)
		})
	}
	assert.Empty(t, fake.puts)
}

func TestMutationYAMLCarriesDiscriminators(t *testing.T) {
	fake := &fakeChecks{checks: append(builtIns(), client.Check{ID: "migrations", Name: "Migrations", Instructions: "x", Enabled: true})}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)

	out, err := run(t, newDeleteCommand(loaderFor(t, server)), "migrations", "--force", "-o", "yaml")
	require.NoError(t, err)
	assert.Contains(t, out, "type: gcx.mutation\n")
	assert.Contains(t, out, "schema_version: \"1\"\n")
	assert.Contains(t, out, "action: deleted\n")
	assert.NotContains(t, out, "SingleMutation")
}

func TestListPullRequestsSendsCheckAndWindow(t *testing.T) {
	var path, query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, query = r.URL.Path, r.URL.RawQuery
		writeData(w, client.PullRequests{PullRequests: []client.PullRequest{{Repository: "grafana/gcx", PullNumber: 42, Reviews: 2, Confirmed: 1}}})
	}))
	t.Cleanup(server.Close)

	out, err := run(t, newListPullRequestsCommand(loaderFor(t, server)), " security ", "--since", "7d", "--repository", "grafana/gcx", "-o", "text")
	require.NoError(t, err)
	assert.Equal(t, "/api/plugins/grafana-assistant-app/resources/api/v1/github/review-analytics/pull-requests", path)
	assert.Contains(t, query, "check=security&", "the check ID is trimmed and sent as check")
	assert.Contains(t, query, "repository=grafana%2Fgcx")
	assert.Contains(t, query, "from=")
	assert.Contains(t, query, "to=")
	assert.Contains(t, out, "#42")
}

func TestGetTrimsCheckID(t *testing.T) {
	fake := &fakeChecks{checks: builtIns()}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)

	out, err := run(t, newGetCommand(loaderFor(t, server)), " security ")
	require.NoError(t, err)
	assert.Contains(t, out, "id: security")
}

func TestUpdateRejectsEmptyFilename(t *testing.T) {
	fake := &fakeChecks{checks: builtIns()}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)

	_, err := run(t, newUpdateCommand(loaderFor(t, server)), "security", "-f", "", "--blocking")
	var usage *gcxerrors.UsageError
	require.ErrorAs(t, err, &usage)
	assert.Contains(t, err.Error(), "--filename cannot be empty")
	assert.Empty(t, fake.puts, "the flag alone must not be saved")
}

func TestListPullRequestsAttachesListMetaWhenTruncated(t *testing.T) {
	tests := []struct {
		name    string
		count   int
		wantCap int
	}{
		{name: "pull request cap", count: 100, wantCap: 100},
		{name: "review cap", count: 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pulls := make([]client.PullRequest, tc.count)
			for i := range pulls {
				pulls[i] = client.PullRequest{Repository: "grafana/gcx", PullNumber: i + 1}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeData(w, client.PullRequests{Truncated: true, PullRequests: pulls})
			}))
			t.Cleanup(server.Close)

			out, err := run(t, newListPullRequestsCommand(loaderFor(t, server)), "security", "-o", "json")
			require.NoError(t, err)
			var got struct {
				Truncated    bool                 `json:"truncated"`
				PullRequests []client.PullRequest `json:"pullRequests"`
				ListMeta     *struct {
					Truncated bool `json:"truncated"`
					Returned  int  `json:"returned"`
					Cap       int  `json:"cap"`
				} `json:"list_meta"`
			}
			require.NoError(t, json.Unmarshal([]byte(out), &got))
			require.NotNil(t, got.ListMeta)
			assert.True(t, got.ListMeta.Truncated)
			assert.Equal(t, tc.count, got.ListMeta.Returned)
			assert.Equal(t, tc.wantCap, got.ListMeta.Cap)
			assert.Len(t, got.PullRequests, tc.count, "the list keeps its own fields")
		})
	}
}

func TestListPullRequestsCompleteHasNoListMeta(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeData(w, client.PullRequests{PullRequests: []client.PullRequest{}})
	}))
	t.Cleanup(server.Close)

	out, err := run(t, newListPullRequestsCommand(loaderFor(t, server)), "security", "-o", "json")
	require.NoError(t, err)
	assert.NotContains(t, out, "list_meta")
	assert.Contains(t, out, `"pullRequests": []`)
}
