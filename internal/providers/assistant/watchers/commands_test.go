package watchers //nolint:testpackage // Verify command wiring and codec parity.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/assistant/watcher"
	clientwatchers "github.com/grafana/gcx/internal/assistant/watchers"
	"github.com/grafana/gcx/internal/config"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

func fixtureClient(t *testing.T, item clientwatchers.Watcher, calibrationCode int) *clientwatchers.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "application/json")
		var result any
		switch {
		case strings.HasSuffix(r.URL.Path, "/initial-calibration"):
			if calibrationCode != http.StatusOK {
				w.WriteHeader(calibrationCode)
				assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"message": "raw-secret-value\nSuggestions: ╭diagnostic-box╮"}))
				return
			}
			result = clientwatchers.Calibration{Status: "needs_input", Message: "More context required"}
		case strings.HasSuffix(r.URL.Path, "/auto-calibration"):
			result = clientwatchers.Enrollment{Enabled: true}
		case strings.HasSuffix(r.URL.Path, "/"+item.ID):
			result = item
		default:
			rows := []clientwatchers.Watcher{}
			if r.URL.Query().Get("archived") != "true" {
				rows = append(rows, item)
			}
			result = map[string]any{"agents": rows}
		}
		assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"data": result}))
	}))
	t.Cleanup(server.Close)
	base, err := assistanthttp.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}, Namespace: "default"})
	require.NoError(t, err)
	return clientwatchers.NewClient(base)
}

func fixture() clientwatchers.Watcher {
	return clientwatchers.Watcher{ID: "11111111-1111-4111-8111-111111111111", Name: "Checkout health", Prompt: "Watch errors", Status: "paused", TriggerIntervalSeconds: 900, Sensitivity: "balanced", LastRunAssessment: "warning", DefinitionVersion: 2, Actions: clientwatchers.Actions{Webhook: &clientwatchers.WebhookAction{Enabled: true, MinSeverity: "critical", SecureFields: &clientwatchers.SecureFields{URL: true, HMACSecret: true, AuthorizationCredentials: true}}}}
}
func outputCommand(t *testing.T) (*cobra.Command, *bytes.Buffer) {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	return cmd, out
}

func TestGetParity(t *testing.T) {
	for _, f := range []string{"json", "yaml", "agents"} {
		t.Run(f, func(t *testing.T) {
			client := fixtureClient(t, fixture(), http.StatusOK)
			cmd, out := outputCommand(t)
			opts := &readOpts{IO: cmdio.Options{OutputFormat: f}, Ref: "checkout-health"}
			require.NoError(t, runGet(cmd, client, "default", opts))
			crud := watcher.NewTypedCRUDForClient(client, "default")
			expected, err := crud.AsAdapter().Get(t.Context(), opts.Ref, metav1.GetOptions{})
			require.NoError(t, err)
			var want bytes.Buffer
			require.NoError(t, opts.IO.Encode(&want, expected.Object))
			require.Equal(t, want.String(), out.String())
			require.Contains(t, out.String(), "preserve")
			require.Contains(t, out.String(), fixture().ID)
			require.NotContains(t, out.String(), "lastRun")
			require.NotContains(t, out.String(), "queries")
		})
	}
}

func TestListArchiveCoverage(t *testing.T) {
	for _, archived := range []bool{false, true} {
		t.Run(strconv.FormatBool(archived), func(t *testing.T) {
			cmd, out := outputCommand(t)
			require.NoError(t, runList(cmd, fixtureClient(t, fixture(), http.StatusOK), "default", &listOpts{IO: cmdio.Options{OutputFormat: "json"}, Archived: archived}))
			var got listResult
			require.NoError(t, json.Unmarshal(out.Bytes(), &got))
			require.NotNil(t, got.Items)
			require.Equal(t, archived, got.Coverage.Archived)
			require.True(t, got.Coverage.Complete)
			require.False(t, got.Coverage.Snapshot)
			if archived {
				require.Empty(t, got.Items)
			} else {
				require.Len(t, got.Items, 1)
			}
		})
	}
}

func TestStatusObservations(t *testing.T) {
	for _, test := range []struct {
		name         string
		code         int
		availability string
	}{{"progress", 200, ""}, {"unavailable", 501, "unavailable"}, {"denied", 403, "access-denied"}, {"failure", 500, "failed"}} {
		t.Run(test.name, func(t *testing.T) {
			item := fixture()
			now := time.Now().UTC()
			item.LastRunAt = &now
			item.CreatedAt = now
			item.TokenConsumption = &clientwatchers.TokenConsumption{AveragePerRun: 120, EstimatedPerHour: 480, SampleSize: 3}
			item.Queries = []map[string]any{{"id": "check-one", "type": "new-kind", "expr": "example", "futureOption": true}}
			cmd, out := outputCommand(t)
			require.NoError(t, runStatus(cmd, fixtureClient(t, item, test.code), &readOpts{IO: cmdio.Options{OutputFormat: "json"}, Ref: item.ID}))
			var got statusResult
			require.NoError(t, json.Unmarshal(out.Bytes(), &got))
			require.Equal(t, "paused", got.Lifecycle)
			require.Equal(t, "warning", got.Assessment.Severity)
			require.Equal(t, "2", got.Version.ID)
			require.Equal(t, 120, int(got.Usage.EstimatedTokensPerRun))
			require.Equal(t, 3, got.Usage.SampleSize)
			require.Equal(t, test.availability, got.Calibration.Availability)
			require.Len(t, got.Calibration.Checks, 1)
			require.Contains(t, out.String(), "unsupported")
			require.NotContains(t, out.String(), "lastCompletedAt")
			require.NotContains(t, out.String(), "observedAt")
			if test.code == 200 {
				require.Equal(t, "needs_input", got.Calibration.State)
				require.Contains(t, got.Calibration.Message, "Grafana")
			} else {
				require.Equal(t, "unknown", got.Calibration.State)
				assertPlainListMessage(t, got.Calibration.Message)
			}
		})
	}
}

func TestCommandValidationAndExperimental(t *testing.T) {
	for _, args := range [][]string{{"list", "stray"}, {"get"}, {"get", " "}, {"status", " "}, {"status", "one", "two"}, {"list", "--archived="}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			cmd := Commands(&providers.ConfigLoader{})
			cmd.SetArgs(args)
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			require.Error(t, cmd.ExecuteContext(t.Context()))
		})
	}
	root := Commands(&providers.ConfigLoader{})
	list, _, err := root.Find([]string{"list"})
	require.NoError(t, err)
	require.Contains(t, list.Long, "Partial reads")
	require.Contains(t, list.Long, "failed or skipped")
	for _, cmd := range append([]*cobra.Command{root}, root.Commands()...) {
		require.True(t, strings.HasPrefix(cmd.Short, "[experimental]"))
		require.True(t, strings.HasPrefix(cmd.Long, experimental))
		require.Equal(t, agent.StabilityExperimental, cmd.Annotations[agent.AnnotationStability])
	}
}

func TestHumanCodecs(t *testing.T) {
	for _, f := range []string{"text", "table", "wide"} {
		t.Run(f, func(t *testing.T) {
			opts := &readOpts{Ref: fixture().ID}
			opts.setup((&cobra.Command{}).Flags())
			opts.IO.OutputFormat = f
			cmd, out := outputCommand(t)
			require.NoError(t, runGet(cmd, fixtureClient(t, fixture(), 200), "default", opts))
			require.Contains(t, out.String(), "checkout-health")
			require.Contains(t, out.String(), fixture().ID)
			out.Reset()
			require.NoError(t, runStatus(cmd, fixtureClient(t, fixture(), 200), opts))
			require.Contains(t, out.String(), "calibration")
			require.Contains(t, out.String(), "paused")
		})
	}
}

func TestPublicCommandArchiveFlag(t *testing.T) {
	for _, archived := range []bool{false, true} {
		t.Run(strconv.FormatBool(archived), func(t *testing.T) {
			var selected []bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				selected = append(selected, r.URL.Query().Get("archived") == "true")
				w.Header().Set("Content-Type", "application/json")
				assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"agents": []any{}}}))
			}))
			t.Cleanup(server.Close)
			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte("current-context: test\ncontexts:\n  test:\n    grafana:\n      server: "+server.URL+"\n      token: fixture-token\n      stack-id: 12345\n"), 0600))
			loader := &providers.ConfigLoader{}
			loader.SetConfigFile(path)
			cmd := Commands(loader)
			out := &bytes.Buffer{}
			cmd.SetOut(out)
			cmd.SetErr(&bytes.Buffer{})
			args := []string{"list", "-o", "json"}
			if archived {
				args = append(args, "--archived")
			}
			cmd.SetArgs(args)
			require.NoError(t, cmd.ExecuteContext(t.Context()))
			require.Equal(t, []bool{archived}, selected)
			var got listResult
			require.NoError(t, json.Unmarshal(out.Bytes(), &got))
			require.Equal(t, archived, got.Coverage.Archived)
		})
	}
}
