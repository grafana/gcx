package faro //nolint:testpackage // Drives the unexported command constructors through the loader seams.

// These tests pin the pre-GA agent output contract for the five faro mutation
// commands (apps create/update/delete, apply-sourcemap, delete-sourcemap):
//
//   - default human stdout stays byte-identical to the pre-migration
//     cmdio.Success one-liner;
//   - agent mode emits EXACTLY ONE JSON value on stdout, then EOF
//     (gcx.mutation for the CRUD verbs, the bespoke gcx.faro.sourcemap_*
//     shapes for the sourcemap verbs);
//   - explicit -o json / -o yaml overrides are honored;
//   - the create command's advisory warning is a typed stderr diagnostic
//     (JSONL in agent mode), never stdout.
//
// The commands are driven end-to-end (cobra Execute) against a fake Faro API
// server, with the config loader stubbed through the command loader seams.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/cloud"
	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

// fakeConfigLoader satisfies RESTConfigLoader and sourcemapUploadConfigLoader
// so a single fake drives every faro mutation command.
type fakeConfigLoader struct {
	grafanaURL string
	faroAPIURL string
}

func (f *fakeConfigLoader) LoadGrafanaConfig(context.Context) (internalconfig.NamespacedRESTConfig, error) {
	return internalconfig.NamespacedRESTConfig{
		Config:    rest.Config{Host: f.grafanaURL},
		Namespace: "stack-1",
	}, nil
}

func (f *fakeConfigLoader) LoadDirectProviderSnapshot(context.Context, providers.DirectProviderPolicy) (providers.DirectProviderSnapshot, error) {
	return providers.DirectProviderSnapshot{
		ProviderConfig: map[string]string{"faro-api-url": f.faroAPIURL},
		Namespace:      "stack-1",
		ResolveCloudConfig: func(context.Context) (providers.CloudRESTConfig, error) {
			return providers.CloudRESTConfig{Token: "tok", Stack: cloud.StackInfo{ID: 7}}, nil
		},
	}, nil
}

func (f *fakeConfigLoader) SaveProviderConfig(context.Context, string, string, string) error {
	return nil
}

// newFaroAPIServer serves the Faro plugin-proxy endpoints (app CRUD,
// sourcemap batch delete) and the direct Faro API upload endpoint for one
// fixed app (my-app/42).
func newFaroAPIServer(t *testing.T) *httptest.Server {
	t.Helper()

	writeJSON := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(v); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}

	app := map[string]any{"id": 42, "name": "my-app"}

	mux := http.NewServeMux()
	mux.HandleFunc(basePath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			writeJSON(w, app)
			return
		}
		writeJSON(w, []map[string]any{app}) // list (create re-fetch)
	})
	mux.HandleFunc(basePath+"/42", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			writeJSON(w, app)
		case http.MethodDelete:
			writeJSON(w, map[string]any{})
		default:
			writeJSON(w, app)
		}
	})
	// Batch sourcemap delete (bundle IDs are comma-joined in the path).
	mux.HandleFunc(basePath+"/42/sourcemaps/batch/", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{})
	})
	// Direct Faro API sourcemap upload endpoint.
	mux.HandleFunc("/api/v1/app/42/sourcemaps/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func writeTestFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func appManifest(t *testing.T) string {
	t.Helper()
	return writeTestFile(t, "app.yaml", `apiVersion: faro.ext.grafana.app/v1alpha1
kind: FaroApp
metadata:
  name: my-app-42
spec:
  name: my-app
`)
}

// faroMutationCases is the shared table for all five faro mutation commands.
func faroMutationCases(t *testing.T) []struct {
	name       string
	build      func(l *fakeConfigLoader) *cobra.Command
	args       []string
	wantHuman  string
	wantType   string
	wantDoc    map[string]any
	wantTarget map[string]any
} {
	t.Helper()
	return []struct {
		name       string
		build      func(l *fakeConfigLoader) *cobra.Command
		args       []string
		wantHuman  string
		wantType   string
		wantDoc    map[string]any
		wantTarget map[string]any
	}{
		{
			name:       "apps create",
			build:      func(l *fakeConfigLoader) *cobra.Command { return newCreateCommand(l) },
			args:       []string{"-f", appManifest(t)},
			wantHuman:  "✔ Created Frontend Observability app \"my-app\" (id=42)\n",
			wantType:   "gcx.mutation",
			wantDoc:    map[string]any{"action": "created"},
			wantTarget: map[string]any{"kind": Kind, "name": "my-app", "id": "42"},
		},
		{
			name:       "apps update",
			build:      func(l *fakeConfigLoader) *cobra.Command { return newUpdateCommand(l) },
			args:       []string{"my-app-42", "-f", appManifest(t)},
			wantHuman:  "✔ Updated Frontend Observability app \"my-app\" (id=42)\n",
			wantType:   "gcx.mutation",
			wantDoc:    map[string]any{"action": "updated"},
			wantTarget: map[string]any{"kind": Kind, "name": "my-app", "id": "42"},
		},
		{
			name:       "apps delete",
			build:      func(l *fakeConfigLoader) *cobra.Command { return newDeleteCommand(l) },
			args:       []string{"my-app-42"},
			wantHuman:  "✔ Deleted Frontend Observability app \"my-app-42\"\n",
			wantType:   "gcx.mutation",
			wantDoc:    map[string]any{"action": "deleted"},
			wantTarget: map[string]any{"kind": Kind, "name": "my-app-42"},
		},
		{
			name:      "apps apply-sourcemap",
			build:     func(l *fakeConfigLoader) *cobra.Command { return newApplySourcemapCommand(l) },
			args:      []string{"my-app-42", "-f", writeTestFile(t, "bundle.js.map", `{"version":3}`), "--bundle-id", "b-1"},
			wantHuman: "✔ Uploaded sourcemap for app 42 (bundle b-1)\n",
			wantType:  sourcemapUploadResultType,
			wantDoc: map[string]any{
				"schema_version": "1",
				"action":         "uploaded",
				"app_id":         "42",
				"bundle_id":      "b-1",
			},
		},
		{
			name:      "apps delete-sourcemap",
			build:     func(l *fakeConfigLoader) *cobra.Command { return newDeleteSourcemapCommand(l) },
			args:      []string{"my-app-42", "b1", "b2"},
			wantHuman: "✔ Deleted 2 sourcemap(s) from app 42\n",
			wantType:  sourcemapDeleteResultType,
			wantDoc: map[string]any{
				"schema_version": "1",
				"action":         "deleted",
				"app_id":         "42",
				"bundle_ids":     []any{"b1", "b2"},
				"deleted":        float64(2),
			},
		},
	}
}

// runFaroCommand builds the command against a fresh fake API server and
// executes it, capturing stdout and stderr.
func runFaroCommand(t *testing.T, build func(l *fakeConfigLoader) *cobra.Command, args []string) (string, string, error) {
	t.Helper()
	server := newFaroAPIServer(t)
	loader := &fakeConfigLoader{grafanaURL: server.URL, faroAPIURL: server.URL}
	cmd := build(loader)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func withPlainColors(t *testing.T) {
	t.Helper()
	prev := color.NoColor
	color.NoColor = true
	t.Cleanup(func() { color.NoColor = prev })
}

// decodeSingleJSONValue asserts that raw holds exactly one JSON value
// followed by EOF, and returns it.
func decodeSingleJSONValue(t *testing.T, raw string) map[string]any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(raw))
	var doc map[string]any
	require.NoError(t, dec.Decode(&doc), "stdout is not valid JSON:\n%s", raw)
	var second any
	err := dec.Decode(&second)
	require.ErrorIs(t, err, io.EOF, "stdout must contain exactly one JSON value\n%s", raw)
	return doc
}

func TestFaroMutations_HumanDefault_ByteIdentical(t *testing.T) {
	withPlainColors(t)
	agent.SetFlag(false)
	t.Cleanup(func() { agent.SetFlag(false) })

	for _, tc := range faroMutationCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			stdout, _, err := runFaroCommand(t, tc.build, tc.args)
			require.NoError(t, err)
			assert.Equal(t, tc.wantHuman, stdout, "default human stdout must stay byte-identical")
		})
	}
}

func TestFaroMutations_AgentMode_SingleJSONDocument(t *testing.T) {
	withPlainColors(t)

	for _, tc := range faroMutationCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			// The agents default is resolved when the command binds its
			// flags, so the flag must be set before building the command.
			agent.SetFlag(true)
			t.Cleanup(func() { agent.SetFlag(false) })

			stdout, _, err := runFaroCommand(t, tc.build, tc.args)
			require.NoError(t, err)

			doc := decodeSingleJSONValue(t, stdout)
			assert.Equal(t, tc.wantType, doc["type"])
			for key, want := range tc.wantDoc {
				assert.Equal(t, want, doc[key], "field %q", key)
			}
			if tc.wantTarget != nil {
				target, ok := doc["target"].(map[string]any)
				require.True(t, ok, "target must be an object: %v", doc["target"])
				for key, want := range tc.wantTarget {
					assert.Equal(t, want, target[key], "target field %q", key)
				}
			}
		})
	}
}

func TestFaroMutations_ExplicitOutputOverride(t *testing.T) {
	withPlainColors(t)
	agent.SetFlag(false)
	t.Cleanup(func() { agent.SetFlag(false) })

	for _, tc := range faroMutationCases(t) {
		t.Run(tc.name+" -o json", func(t *testing.T) {
			stdout, _, err := runFaroCommand(t, tc.build, append(tc.args, "-o", "json"))
			require.NoError(t, err)
			doc := decodeSingleJSONValue(t, stdout)
			assert.Equal(t, tc.wantType, doc["type"])
		})

		t.Run(tc.name+" -o yaml", func(t *testing.T) {
			stdout, _, err := runFaroCommand(t, tc.build, append(tc.args, "-o", "yaml"))
			require.NoError(t, err)
			assert.Contains(t, stdout, "type: "+tc.wantType)
			assert.NotContains(t, stdout, "✔", "explicit -o yaml must not carry the styled human line")
		})
	}
}

// TestFaroCreate_AdvisoryWarningIsTypedStderrDiagnostic pins the create
// command's extraLogLabels/settings warning to the typed diagnostic stream:
// plain "warn:" prose on stderr for humans, a JSONL warning record on stderr
// in agent mode, and never any of it on stdout.
func TestFaroCreate_AdvisoryWarningIsTypedStderrDiagnostic(t *testing.T) {
	withPlainColors(t)

	manifest := `apiVersion: faro.ext.grafana.app/v1alpha1
kind: FaroApp
metadata:
  name: my-app-42
spec:
  name: my-app
  extraLogLabels:
    team: web
`
	const warning = "extraLogLabels and settings are ignored during creation (API limitation); use update to apply them"

	tests := []struct {
		name      string
		agentMode bool
	}{
		{name: "human mode prose diagnostic", agentMode: false},
		{name: "agent mode JSONL diagnostic", agentMode: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			agent.SetFlag(tc.agentMode)
			t.Cleanup(func() { agent.SetFlag(false) })

			path := writeTestFile(t, "app.yaml", manifest)
			stdout, stderr, err := runFaroCommand(t, func(l *fakeConfigLoader) *cobra.Command {
				return newCreateCommand(l)
			}, []string{"-f", path})
			require.NoError(t, err)

			assert.NotContains(t, stdout, warning, "warning must never reach stdout")

			if tc.agentMode {
				// Stdout still holds exactly one JSON value.
				decodeSingleJSONValue(t, stdout)

				// The stderr warning is a JSONL typed-class record.
				line, _, _ := strings.Cut(stderr, "\n")
				var record map[string]any
				require.NoError(t, json.Unmarshal([]byte(line), &record), "agent-mode stderr warning must be JSONL: %q", stderr)
				assert.Equal(t, "warning", record["class"])
				assert.Equal(t, warning, record["summary"])
				return
			}

			assert.Contains(t, stderr, "warn: "+warning+"\n")
			assert.Equal(t, "✔ Created Frontend Observability app \"my-app\" (id=42)\n", stdout)
		})
	}
}
