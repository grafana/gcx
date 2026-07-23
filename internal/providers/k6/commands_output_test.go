// Package k6 command output-contract tests.
//
// These tests pin the pre-GA agent output contract for the k6 finite
// commands migrated to the codec system:
//
//   - human default stdout stays byte-identical to the pre-migration
//     implementation (the exact styled "✔ ..." line, or the exact
//     hand-rolled status block);
//   - agent mode (no explicit -o) writes exactly one JSON value to stdout,
//     then EOF;
//   - an explicit -o always wins, even in agent mode.
//
// No k6 command aggregates multiple targets, so there is no partial-failure
// (exit 4 / EmittedError) path in this family: every command either fully
// succeeds or returns a plain error before anything is written to stdout.
//
//nolint:testpackage // Exercises unexported command constructors and shares the package-internal mockLoader.
package k6

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

// newFakeK6Loader starts an httptest server that fakes every k6 Cloud API
// endpoint the migrated commands touch, and returns a loader whose cached
// auth points the DirectClient at it. The stack-bound cache may reject the
// seeded tuple, so the token exchange is served too.
func newFakeK6Loader(t *testing.T) *mockLoader {
	t.Helper()

	mux := http.NewServeMux()

	// token exchange
	mux.HandleFunc("PUT /v3/account/grafana-app/start", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"organization_id":"42","v3_grafana_token":"cached-v3"}`))
	})

	// projects
	mux.HandleFunc("POST /cloud/v6/projects", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":7,"name":"demo"}`))
	})
	mux.HandleFunc("PATCH /cloud/v6/projects/5", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("DELETE /cloud/v6/projects/5", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("PUT /cloud/v6/projects/5/allowed_load_zones", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// load tests
	mux.HandleFunc("POST /cloud/v6/projects/3/load_tests", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":9,"name":"lt","project_id":3}`))
	})
	mux.HandleFunc("PATCH /cloud/v6/load_tests/9", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("PUT /cloud/v6/load_tests/9/script", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("DELETE /cloud/v6/load_tests/9", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("DELETE /cloud/v6/load_tests/9/schedule", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /cloud/v6/load_tests/9/schedule", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":12,"load_test_id":9}`))
	})

	// test-run status resolution
	mux.HandleFunc("GET /cloud/v6/load_tests/4", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":4,"name":"t","project_id":3}`))
	})
	mux.HandleFunc("GET /cloud/v6/load_tests/4/test_runs", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"value":[{"id":11,"load_test_id":4,"status":"finished","result_status":1,` +
			`"created":"2026-01-01T00:00:00Z","ended":"2026-01-01T01:00:00Z"}]}`))
	})

	// schedules
	mux.HandleFunc("PUT /cloud/v6/schedules/12", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":12,"load_test_id":9}`))
	})

	// env vars (org 42 from the cached auth below)
	mux.HandleFunc("POST /v3/organizations/42/envvars", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"envvar":{"id":33,"name":"FOO","value":"bar"}}`))
	})
	mux.HandleFunc("PATCH /v3/organizations/42/envvars/33", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("DELETE /v3/organizations/42/envvars/33", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	// private load zones
	mux.HandleFunc("POST /cloud-resources/v1/load-zones", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"name":"myzone","k6_load_zone_id":"lz-1"}`))
	})
	mux.HandleFunc("DELETE /cloud-resources/v1/load-zones/myzone", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("PUT /cloud/v6/load_zones/8/allowed_projects", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return &mockLoader{
		cloudCfg: providers.CloudRESTConfig{
			Stack:     cloud.StackInfo{ID: 999},
			Namespace: "stack-999",
		},
		grafanaCfg: config.NamespacedRESTConfig{
			Config: rest.Config{BearerToken: "glsa_test"},
		},
		providerCfg: map[string]string{
			"api-domain":     srv.URL,
			keyCachedToken:   "cached-v3",
			keyCachedOrgID:   "42",
			keyCachedStackID: "999",
		},
	}
}

// runK6Command builds the command (after agent mode is set, so BindFlags
// picks the right default format) and executes it with captured streams.
func runK6Command(t *testing.T, agentMode bool, loader CloudConfigLoader,
	build func(CloudConfigLoader) *cobra.Command, args []string, stdin string,
) (string, string, error) {
	t.Helper()
	agent.SetFlag(agentMode)
	t.Cleanup(func() { agent.SetFlag(false) })

	cmd := build(loader)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	if stdin != "" {
		cmd.SetIn(strings.NewReader(stdin))
	}
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

// decodeSingleJSON asserts stdout holds exactly one JSON value followed by
// EOF, and returns it decoded.
func decodeSingleJSON(t *testing.T, stdout string) map[string]any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(stdout))
	var doc map[string]any
	require.NoError(t, dec.Decode(&doc), "stdout is not valid JSON:\n%s", stdout)
	var second any
	err := dec.Decode(&second)
	require.ErrorIs(t, err, io.EOF,
		"stdout must contain exactly one JSON value, second decode = %v\n%s", err, stdout)
	return doc
}

// lookup resolves a dot path ("target.kind") in a decoded JSON document.
func lookup(doc map[string]any, path string) any {
	cur := any(doc)
	for part := range strings.SplitSeq(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[part]
	}
	return cur
}

const projectManifest = `apiVersion: k6.ext.grafana.app/v1alpha1
kind: Project
metadata:
  name: "5"
spec:
  name: renamed
`

// outputContractCase describes one command's expected behavior under the
// four output-contract invocation modes exercised by runOutputContractCases.
type outputContractCase struct {
	name  string
	build func(CloudConfigLoader) *cobra.Command
	args  []string
	stdin string
	// wantHuman is the exact stdout of a default human invocation —
	// byte-identical to the pre-migration implementation.
	wantHuman string
	// wantHumanStderr is the exact stderr of a default human invocation
	// ("" = must be empty). Create-echo commands moved their status note
	// here, same rendering.
	wantHumanStderr string
	// wantAgent maps dot paths to expected values in the single
	// agent-mode JSON document.
	wantAgent map[string]any
	// wantYAMLSubstrs must all appear in explicit `-o yaml` output
	// (requested in agent mode, proving explicit -o beats the agents
	// default).
	wantYAMLSubstrs []string
}

// runOutputContractCases runs the four contract subtests (human default,
// agent default, explicit -o json in human mode, explicit -o yaml in agent
// mode) for each case.
func runOutputContractCases(t *testing.T, cases []outputContractCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("human default stdout byte-identical", func(t *testing.T) {
				loader := newFakeK6Loader(t)
				stdout, stderr, err := runK6Command(t, false, loader, tc.build, tc.args, tc.stdin)
				require.NoError(t, err)
				if tc.wantHuman != "" {
					assert.Equal(t, tc.wantHuman, stdout)
				} else {
					// Echo commands: stdout is the object document only —
					// no styled status prose contaminating it.
					assert.NotContains(t, stdout, "✔")
					assert.NotEmpty(t, stdout)
				}
				assert.Equal(t, tc.wantHumanStderr, stderr)
			})

			t.Run("agent mode emits exactly one JSON value", func(t *testing.T) {
				loader := newFakeK6Loader(t)
				stdout, _, err := runK6Command(t, true, loader, tc.build, tc.args, tc.stdin)
				require.NoError(t, err)
				doc := decodeSingleJSON(t, stdout)
				for path, want := range tc.wantAgent {
					assert.Equal(t, want, lookup(doc, path), "path %s in %s", path, stdout)
				}
			})

			t.Run("explicit -o json wins in human mode", func(t *testing.T) {
				loader := newFakeK6Loader(t)
				args := append(append([]string{}, tc.args...), "-o", "json")
				stdout, _, err := runK6Command(t, false, loader, tc.build, args, tc.stdin)
				require.NoError(t, err)
				doc := decodeSingleJSON(t, stdout)
				for path, want := range tc.wantAgent {
					assert.Equal(t, want, lookup(doc, path), "path %s in %s", path, stdout)
				}
			})

			t.Run("explicit -o yaml wins in agent mode", func(t *testing.T) {
				loader := newFakeK6Loader(t)
				args := append(append([]string{}, tc.args...), "-o", "yaml")
				stdout, _, err := runK6Command(t, true, loader, tc.build, args, tc.stdin)
				require.NoError(t, err)
				// Explicit -o yaml must beat the agents default: yaml, not
				// a JSON document.
				assert.False(t, strings.HasPrefix(strings.TrimSpace(stdout), "{"),
					"expected yaml, got JSON:\n%s", stdout)
				for _, substr := range tc.wantYAMLSubstrs {
					assert.Contains(t, stdout, substr)
				}
			})
		})
	}
}

func TestK6ProjectsCommands_OutputContract(t *testing.T) {
	runOutputContractCases(t, []outputContractCase{
		{
			name:      "projects update",
			build:     newProjectsUpdateCommand,
			args:      []string{"5", "-f", "-"},
			stdin:     projectManifest,
			wantHuman: "✔ Updated project 5\n",
			wantAgent: map[string]any{
				"type": "gcx.mutation", "schema_version": "1", "action": "updated",
				"target.kind": "project", "target.id": "5",
			},
			wantYAMLSubstrs: []string{"type: gcx.mutation", "action: updated", "kind: project"},
		},
		{
			name:      "projects delete",
			build:     newProjectsDeleteCommand,
			args:      []string{"5"},
			wantHuman: "✔ Deleted project 5\n",
			wantAgent: map[string]any{
				"type": "gcx.mutation", "action": "deleted",
				"target.kind": "project", "target.id": "5",
			},
			wantYAMLSubstrs: []string{"type: gcx.mutation", "action: deleted"},
		},
		{
			name:      "projects update-allowed-load-zones",
			build:     newUpdateAllowedLoadZonesCommand,
			args:      []string{"5", "-f", "-"},
			stdin:     "[1,2]",
			wantHuman: "✔ Updated allowed load zones for project 5\n",
			wantAgent: map[string]any{
				"type": "gcx.mutation", "action": "updated-allowed-load-zones",
				"target.kind": "project", "target.id": "5",
			},
			wantYAMLSubstrs: []string{"action: updated-allowed-load-zones"},
		},
	})
}

func TestK6LoadTestsCommands_OutputContract(t *testing.T) {
	runOutputContractCases(t, []outputContractCase{
		{
			name:      "load-tests update",
			build:     newTestsUpdateCommand,
			args:      []string{"9", "-f", "-"},
			stdin:     "name: lt2\n",
			wantHuman: "✔ Updated load test 9\n",
			wantAgent: map[string]any{
				"type": "gcx.mutation", "action": "updated",
				"target.kind": "load-test", "target.id": "9",
			},
			wantYAMLSubstrs: []string{"action: updated", "kind: load-test"},
		},
		{
			name:      "load-tests update-script",
			build:     newTestsUpdateScriptCommand,
			args:      []string{"9", "-f", "-"},
			stdin:     "export default function () {}\n",
			wantHuman: "✔ Updated script for load test 9\n",
			wantAgent: map[string]any{
				"type": "gcx.mutation", "action": "updated-script",
				"target.kind": "load-test", "target.id": "9",
			},
			wantYAMLSubstrs: []string{"action: updated-script"},
		},
		{
			name:      "load-tests delete",
			build:     newTestsDeleteCommand,
			args:      []string{"9"},
			wantHuman: "✔ Deleted load test 9\n",
			wantAgent: map[string]any{
				"type": "gcx.mutation", "action": "deleted",
				"target.kind": "load-test", "target.id": "9",
			},
			wantYAMLSubstrs: []string{"action: deleted"},
		},
		{
			name:      "load-tests delete-schedule",
			build:     newTestsDeleteScheduleCommand,
			args:      []string{"9"},
			wantHuman: "✔ Deleted schedule for load test 9\n",
			wantAgent: map[string]any{
				"type": "gcx.mutation", "action": "deleted-schedule",
				"target.kind": "load-test", "target.id": "9",
			},
			wantYAMLSubstrs: []string{"action: deleted-schedule"},
		},
	})
}

func TestK6EnvVarsCommands_OutputContract(t *testing.T) {
	runOutputContractCases(t, []outputContractCase{
		{
			name:      "env-vars create",
			build:     newEnvVarsCreateCommand,
			args:      []string{"-f", "-"},
			stdin:     `{"name":"FOO","value":"bar"}`,
			wantHuman: "✔ Created env var \"FOO\" (id=33)\n",
			wantAgent: map[string]any{
				"type": "gcx.mutation", "action": "created",
				"target.kind": "env-var", "target.name": "FOO", "target.id": "33",
			},
			wantYAMLSubstrs: []string{"action: created", "name: FOO"},
		},
		{
			name:      "env-vars update",
			build:     newEnvVarsUpdateCommand,
			args:      []string{"33", "-f", "-"},
			stdin:     `{"name":"FOO","value":"baz"}`,
			wantHuman: "✔ Updated env var 33\n",
			wantAgent: map[string]any{
				"type": "gcx.mutation", "action": "updated",
				"target.kind": "env-var", "target.id": "33",
			},
			wantYAMLSubstrs: []string{"action: updated"},
		},
		{
			name:      "env-vars delete",
			build:     newEnvVarsDeleteCommand,
			args:      []string{"33"},
			wantHuman: "✔ Deleted env var 33\n",
			wantAgent: map[string]any{
				"type": "gcx.mutation", "action": "deleted",
				"target.kind": "env-var", "target.id": "33",
			},
			wantYAMLSubstrs: []string{"action: deleted"},
		},
	})
}

func TestK6SchedulesCommands_OutputContract(t *testing.T) {
	runOutputContractCases(t, []outputContractCase{
		{
			name:      "schedules create",
			build:     newSchedulesCreateCommand,
			args:      []string{"--load-test-id", "9", "-f", "-"},
			stdin:     `{"starts":"2026-01-01T00:00:00Z"}`,
			wantHuman: "✔ Created schedule 12 for load test 9\n",
			wantAgent: map[string]any{
				"type": "gcx.mutation", "action": "created",
				"target.kind": "schedule", "target.id": "12",
			},
			wantYAMLSubstrs: []string{"action: created", "kind: schedule"},
		},
		{
			name:      "schedules update",
			build:     newSchedulesUpdateCommand,
			args:      []string{"12", "-f", "-"},
			stdin:     `{"starts":"2026-01-01T00:00:00Z"}`,
			wantHuman: "✔ Updated schedule 12\n",
			wantAgent: map[string]any{
				"type": "gcx.mutation", "action": "updated",
				"target.kind": "schedule", "target.id": "12",
			},
			wantYAMLSubstrs: []string{"action: updated"},
		},
	})
}

func TestK6LoadZonesCommands_OutputContract(t *testing.T) {
	runOutputContractCases(t, []outputContractCase{
		{
			name:      "load-zones create",
			build:     newLoadZonesCreateCommand,
			args:      []string{"--name", "myzone", "--provider-id", "aws"},
			wantHuman: "✔ Registered load zone \"myzone\"\n",
			wantAgent: map[string]any{
				"type": "gcx.mutation", "action": "registered",
				"target.kind": "load-zone", "target.name": "myzone",
			},
			wantYAMLSubstrs: []string{"action: registered", "name: myzone"},
		},
		{
			name:      "load-zones delete",
			build:     newLoadZonesDeleteCommand,
			args:      []string{"myzone"},
			wantHuman: "✔ Deregistered load zone \"myzone\"\n",
			wantAgent: map[string]any{
				"type": "gcx.mutation", "action": "deregistered",
				"target.kind": "load-zone", "target.name": "myzone",
			},
			wantYAMLSubstrs: []string{"action: deregistered"},
		},
		{
			name:      "load-zones update-allowed-projects",
			build:     newUpdateAllowedProjectsCommand,
			args:      []string{"8", "-f", "-"},
			stdin:     "[1]",
			wantHuman: "✔ Updated allowed projects for load zone 8\n",
			wantAgent: map[string]any{
				"type": "gcx.mutation", "action": "updated-allowed-projects",
				"target.kind": "load-zone", "target.id": "8",
			},
			wantYAMLSubstrs: []string{"action: updated-allowed-projects"},
		},
	})
}

func TestK6CreateEchoCommands_OutputContract(t *testing.T) {
	runOutputContractCases(t, []outputContractCase{
		{
			name:  "projects create echoes object with note on stderr",
			build: newProjectsCreateCommand,
			args:  []string{"-f", "-"},
			stdin: strings.ReplaceAll(projectManifest, "renamed", "demo"),
			// Human default stdout is the created object (yaml); the status
			// note moved to stderr with the same rendering.
			wantHuman:       "", // checked via wantYAMLSubstrs below and stderr
			wantHumanStderr: "✔ Created project \"demo\" (id=7)\n",
			wantAgent: map[string]any{
				"kind": "Project", "apiVersion": "k6.ext.grafana.app/v1alpha1",
				"metadata.name": "7", "spec.name": "demo",
			},
			wantYAMLSubstrs: []string{"apiVersion: k6.ext.grafana.app/v1alpha1", "kind: Project", "name: demo"},
		},
		{
			name:            "load-tests create echoes object with note on stderr",
			build:           newTestsCreateCommand,
			args:            []string{"-f", "-"},
			stdin:           "name: lt\nproject_id: 3\nscript: export default function () {}\n",
			wantHuman:       "",
			wantHumanStderr: "✔ Created load test \"lt\" (id=9)\n",
			wantAgent: map[string]any{
				"id": float64(9), "name": "lt", "project_id": float64(3),
			},
			wantYAMLSubstrs: []string{"id: 9", "name: lt", "project_id: 3"},
		},
	})
}

func TestK6TestRunStatusCommand_OutputContract(t *testing.T) {
	runOutputContractCases(t, []outputContractCase{
		{
			name:  "test-run status",
			build: newTestrunStatusCommand,
			args:  []string{"--id", "4"},
			wantHuman: "Run ID:  11\n" +
				"Status:  finished\n" +
				"Result:  passed\n" +
				"Created: 2026-01-01T00:00:00Z\n" +
				"Ended:   2026-01-01T01:00:00Z\n",
			wantAgent: map[string]any{
				"id": float64(11), "load_test_id": float64(4),
				"status": "finished", "result_status": float64(1),
			},
			wantYAMLSubstrs: []string{"id: 11", "status: finished"},
		},
	})
}
