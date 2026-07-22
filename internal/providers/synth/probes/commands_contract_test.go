package probes_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/fatih/color"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers/synth/probes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests pin the agent output contract for the probes mutation commands
// (create, delete, reset-token):
//   - agent mode emits exactly one JSON value on stdout;
//   - the human default output stays byte-identical to the pre-codec lines —
//     including the one-time auth token block on create;
//   - partial failures return *gcxerrors.EmittedError with ExitPartialFailure;
//   - explicit -o json/yaml overrides are honored;
//   - advisory notes land on stderr, never stdout.

// probeAPIState drives the fake SM probes API.
type probeAPIState struct {
	mu         sync.Mutex
	probes     []probes.Probe
	failDelete map[int64]bool
}

func newProbeServer(t *testing.T, st *probeAPIState) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/probe/list", func(w http.ResponseWriter, _ *http.Request) {
		st.mu.Lock()
		defer st.mu.Unlock()
		writeJSON(w, st.probes)
	})
	mux.HandleFunc("/api/v1/probe/add", func(w http.ResponseWriter, r *http.Request) {
		var p probes.Probe
		_ = json.NewDecoder(r.Body).Decode(&p)
		p.ID = 99
		writeJSON(w, probes.CreateResponse{Probe: p, Token: "probe-auth-token-abc"})
	})
	mux.HandleFunc("/api/v1/probe/update", func(w http.ResponseWriter, r *http.Request) {
		var m map[string]any
		_ = json.NewDecoder(r.Body).Decode(&m)
		delete(m, "resetToken")
		writeJSON(w, map[string]any{"probe": m})
	})
	mux.HandleFunc("/api/v1/probe/delete/", func(w http.ResponseWriter, r *http.Request) {
		idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/probe/delete/")
		var id int64
		_, _ = fmt.Sscanf(idStr, "%d", &id)
		if st.failDelete[id] {
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(w, map[string]string{"error": "boom"})
			return
		}
		writeJSON(w, map[string]string{"msg": "deleted"})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// runProbes executes a `probes` subcommand against the fake server, capturing
// stdout and stderr. The command tree is built after the agent flag is set,
// mirroring the real CLI (BindFlags reads agent mode at construction time).
func runProbes(t *testing.T, srvURL string, agentMode bool, stdin string, args ...string) (string, string, error) {
	t.Helper()
	prevNoColor := color.NoColor
	color.NoColor = true
	agent.SetFlag(agentMode)
	t.Cleanup(func() {
		agent.SetFlag(false)
		color.NoColor = prevNoColor
	})

	root := probes.Commands(&fakeProbeLoader{baseURL: srvURL, token: "t", namespace: "default"})
	root.SilenceErrors = true
	root.SilenceUsage = true
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return stdout.String(), stderr.String(), err
}

// decodeSingleJSONValue asserts stdout carries exactly one JSON value
// followed by EOF, and returns it.
func decodeSingleJSONValue(t *testing.T, stdout string) any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(stdout))
	var doc any
	require.NoError(t, dec.Decode(&doc), "stdout must be valid JSON, got: %q", stdout)
	require.ErrorIs(t, dec.Decode(new(any)), io.EOF, "stdout must contain exactly one JSON value, got: %q", stdout)
	return doc
}

// jsonInt converts a JSON-decoded numeric field to int for exact assertions.
func jsonInt(t *testing.T, v any) int {
	t.Helper()
	f, ok := v.(float64)
	require.True(t, ok, "expected JSON number, got %T", v)
	return int(f)
}

func TestProbesCreateOutputContract(t *testing.T) {
	wantHuman := "✔ Created probe \"my-probe\" (id=99)\n" +
		"\nProbe auth token (save this — it cannot be retrieved later):\nprobe-auth-token-abc\n"

	tests := []struct {
		name       string
		agentMode  bool
		extraArgs  []string
		wantStdout string
		checkJSON  bool
		wantInOut  string
	}{
		{
			name:       "human default byte-identical including token block",
			wantStdout: wantHuman,
		},
		{
			name:      "agent mode single JSON document with token field",
			agentMode: true,
			checkJSON: true,
		},
		{
			name:      "explicit -o json override",
			extraArgs: []string{"-o", "json"},
			checkJSON: true,
		},
		{
			name:      "explicit -o yaml override",
			extraArgs: []string{"-o", "yaml"},
			wantInOut: "type: gcx.synth.probe_create",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newProbeServer(t, &probeAPIState{})

			args := append([]string{"create", "--name", "my-probe"}, tc.extraArgs...)
			stdout, _, err := runProbes(t, srv.URL, tc.agentMode, "", args...)
			require.NoError(t, err)

			if tc.wantStdout != "" {
				assert.Equal(t, tc.wantStdout, stdout)
			}
			if tc.wantInOut != "" {
				assert.Contains(t, stdout, tc.wantInOut)
			}
			if tc.checkJSON {
				doc, ok := decodeSingleJSONValue(t, stdout).(map[string]any)
				require.True(t, ok, "create result must be a JSON object")
				assert.Equal(t, "gcx.synth.probe_create", doc["type"])
				assert.Equal(t, "1", doc["schema_version"])
				assert.Equal(t, "my-probe", doc["name"])
				assert.Equal(t, 99, jsonInt(t, doc["id"]))
				assert.Equal(t, "probe-auth-token-abc", doc["token"],
					"one-time token must be a structured field agents can capture")
			}
		})
	}
}

func TestProbesDeleteOutputContract(t *testing.T) {
	tests := []struct {
		name       string
		agentMode  bool
		extraArgs  []string
		wantStdout string
		checkJSON  bool
	}{
		{
			name:       "human default byte-identical",
			wantStdout: "✔ Deleted probe 1\n✔ Deleted probe 2\n",
		},
		{
			name:      "agent mode single JSON document",
			agentMode: true,
			checkJSON: true,
		},
		{
			name:      "explicit -o json override",
			extraArgs: []string{"-o", "json"},
			checkJSON: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newProbeServer(t, &probeAPIState{})

			args := append([]string{"delete", "1", "2", "--force"}, tc.extraArgs...)
			stdout, _, err := runProbes(t, srv.URL, tc.agentMode, "", args...)
			require.NoError(t, err)

			if tc.wantStdout != "" {
				assert.Equal(t, tc.wantStdout, stdout)
			}
			if tc.checkJSON {
				doc, ok := decodeSingleJSONValue(t, stdout).(map[string]any)
				require.True(t, ok)
				assert.Equal(t, "gcx.synth.delete_batch", doc["type"])
				assert.Equal(t, "1", doc["schema_version"])
				assert.Equal(t, []any{"1", "2"}, doc["deleted"])
			}
		})
	}
}

func TestProbesDeletePartialFailure(t *testing.T) {
	tests := []struct {
		name      string
		agentMode bool
	}{
		{name: "human mode"},
		{name: "agent mode", agentMode: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newProbeServer(t, &probeAPIState{failDelete: map[int64]bool{2: true}})

			stdout, stderr, err := runProbes(t, srv.URL, tc.agentMode, "", "delete", "1", "2", "--force")

			var emitted *gcxerrors.EmittedError
			require.ErrorAs(t, err, &emitted)
			assert.Equal(t, gcxerrors.ExitPartialFailure, emitted.Code)
			assert.Contains(t, stderr, "deleting probe 2")

			if tc.agentMode {
				doc, ok := decodeSingleJSONValue(t, stdout).(map[string]any)
				require.True(t, ok)
				assert.Equal(t, "gcx.synth.delete_batch", doc["type"])
				assert.Equal(t, []any{"1"}, doc["deleted"])
				summary, ok := doc["summary"].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, 1, jsonInt(t, summary["succeeded"]))
				assert.Equal(t, 1, jsonInt(t, summary["failed"]))
			} else {
				assert.Equal(t, "✔ Deleted probe 1\n", stdout)
			}
		})
	}
}

func TestProbesDeletePromptOnStderr(t *testing.T) {
	srv := newProbeServer(t, &probeAPIState{})

	stdout, stderr, err := runProbes(t, srv.URL, false, "n\n", "delete", "1")
	require.NoError(t, err)
	assert.Empty(t, stdout, "prompt and decline note must not touch stdout")
	assert.Contains(t, stderr, "Delete 1 probe(s)? [y/N]")
	assert.Contains(t, stderr, "Aborted.")
}

func TestProbesResetTokenOutputContract(t *testing.T) {
	tests := []struct {
		name       string
		agentMode  bool
		extraArgs  []string
		wantStdout string
		checkJSON  bool
	}{
		{
			name:       "human default byte-identical, note on stderr",
			wantStdout: "✔ Reset auth token for probe \"my-probe\" (id=7)\n",
		},
		{
			name:      "agent mode single JSON document",
			agentMode: true,
			checkJSON: true,
		},
		{
			name:      "explicit -o json override",
			extraArgs: []string{"-o", "json"},
			checkJSON: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := &probeAPIState{probes: []probes.Probe{{ID: 7, Name: "my-probe"}}}
			srv := newProbeServer(t, st)

			args := append([]string{"reset-token", "7"}, tc.extraArgs...)
			stdout, stderr, err := runProbes(t, srv.URL, tc.agentMode, "", args...)
			require.NoError(t, err)

			// The token-not-returned note is advisory: stderr only.
			assert.Contains(t, stderr, "does not return the new token")
			assert.NotContains(t, stdout, "does not return the new token")

			if tc.wantStdout != "" {
				assert.Equal(t, tc.wantStdout, stdout)
			}
			if tc.checkJSON {
				doc, ok := decodeSingleJSONValue(t, stdout).(map[string]any)
				require.True(t, ok)
				assert.Equal(t, "gcx.mutation", doc["type"])
				assert.Equal(t, "reset-token", doc["action"])
				target, ok := doc["target"].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, "my-probe", target["name"])
				assert.Equal(t, "7", target["id"])
			}
		})
	}
}
