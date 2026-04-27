package kg_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/kg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

type testRESTConfigLoader struct {
	cfg config.NamespacedRESTConfig
}

func (l testRESTConfigLoader) LoadGrafanaConfig(_ context.Context) (config.NamespacedRESTConfig, error) {
	return l.cfg, nil
}

func scopesHandler(scopes map[string][]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"scopeValues": scopes})
	}
}

func TestSuppressionsCreate_DryRunShowsDiffWithoutUploading(t *testing.T) {
	var postCalled atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "config/disabled-alerts")
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, map[string]any{
				"disabledAlertConfigs": []map[string]any{{
					"name":        "remote",
					"matchLabels": map[string]string{"env": "prod"},
				}},
			})
		case http.MethodPost:
			postCalled.Store(true)
			t.Fatalf("dry-run must not upload suppressions")
		default:
			t.Fatalf("unexpected method: %s", r.Method)
		}
	}))
	defer server.Close()

	file := writeTempYAML(t, "disabledAlertConfigs:\n- name: local\n  matchLabels:\n    env: prod\n")
	cmd := kg.NewTestSuppressionsCommand(testRESTConfigLoader{
		cfg: config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}},
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"create", "-f", file, "--dry-run"})

	require.NoError(t, cmd.Execute())
	assert.False(t, postCalled.Load())
	assert.Contains(t, out.String(), "[dry-run] Suppressions YAML is valid")
	assert.Contains(t, out.String(), "--- remote")
	assert.Contains(t, out.String(), "+++ local")
	assert.Contains(t, out.String(), "-      name: remote")
	assert.Contains(t, out.String(), "+      name: local")
}

func TestSuppressionsCreate_DryRunNoChanges(t *testing.T) {
	const configYAML = "disabledAlertConfigs:\n- name: same\n  matchLabels:\n    env: prod\n"
	var postCalled atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "config/disabled-alerts")
		_, _ = w.Write([]byte(configYAML))
	}))
	defer server.Close()

	file := writeTempYAML(t, configYAML)
	cmd := kg.NewTestSuppressionsCommand(testRESTConfigLoader{
		cfg: config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}},
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"create", "-f", file, "--dry-run"})

	require.NoError(t, cmd.Execute())
	assert.False(t, postCalled.Load())
	assert.Contains(t, out.String(), "no changes")
	assert.NotContains(t, out.String(), "--- remote")
}

func TestSuppressionsCreate_DryRunRejectsInvalidYAML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("invalid YAML should fail before remote suppressions are fetched")
	}))
	defer server.Close()

	file := writeTempYAML(t, "disabledAlerts:\n- name: [unterminated\n")
	cmd := kg.NewTestSuppressionsCommand(testRESTConfigLoader{
		cfg: config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}},
	})
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"create", "-f", file, "--dry-run"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid local suppressions YAML")
}

func writeTempYAML(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "suppressions.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestScopeFlags_ValidateScopes(t *testing.T) {
	knownScopes := map[string][]string{
		"env":       {"ops-eu-south-0", "ops-eu-north-1", "prod-us-east-1"},
		"site":      {"site-a", "site-b"},
		"namespace": {"default", "monitoring"},
	}

	tests := []struct {
		name         string
		flags        kg.ScopeFlags
		serverScopes map[string][]string
		serverErr    bool
		wantErr      bool
		errContains  string
	}{
		{
			name:         "no scope flags set — skips validation",
			flags:        kg.NewTestScopeFlags("", "", ""),
			serverScopes: knownScopes,
		},
		{
			name:         "exact match — no error",
			flags:        kg.NewTestScopeFlags("ops-eu-south-0", "", ""),
			serverScopes: knownScopes,
		},
		{
			name:         "exact match multiple flags — no error",
			flags:        kg.NewTestScopeFlags("ops-eu-south-0", "", "default"),
			serverScopes: knownScopes,
		},
		{
			name:         "partial match — error with candidates",
			flags:        kg.NewTestScopeFlags("ops", "", ""),
			serverScopes: knownScopes,
			wantErr:      true,
			errContains:  `did you mean one of: ops-eu-north-1, ops-eu-south-0`,
		},
		{
			name:         "no candidates — lists known values",
			flags:        kg.NewTestScopeFlags("totally-unknown", "", ""),
			serverScopes: knownScopes,
			wantErr:      true,
			errContains:  `known env values:`,
		},
		{
			name:  "known values truncated at 10 with hint",
			flags: kg.NewTestScopeFlags("zzz-no-match", "", ""),
			serverScopes: map[string][]string{
				"env": {"a1", "a2", "a3", "a4", "a5", "a6", "a7", "a8", "a9", "a10", "a11"},
			},
			wantErr:     true,
			errContains: "and 1 more — run gcx kg scopes list",
		},
		{
			name:         "multiple invalid flags — error lists all",
			flags:        kg.NewTestScopeFlags("bad-env", "bad-site", ""),
			serverScopes: knownScopes,
			wantErr:      true,
			errContains:  "--env",
		},
		{
			name:      "API error — best-effort, no error returned",
			flags:     kg.NewTestScopeFlags("anything", "", ""),
			serverErr: true,
		},
		{
			name:         "empty known values for dimension — skips that dimension",
			flags:        kg.NewTestScopeFlags("whatever", "", ""),
			serverScopes: map[string][]string{"env": {}},
		},
		{
			name:         "case-insensitive substring match",
			flags:        kg.NewTestScopeFlags("OPS", "", ""),
			serverScopes: knownScopes,
			wantErr:      true,
			errContains:  "ops-eu",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.serverErr {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				scopesHandler(tt.serverScopes)(w, r)
			}))
			defer server.Close()

			client := newTestClient(t, server)
			err := tt.flags.ValidateScopes(t.Context(), client)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}
