package reviewqueues_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/grafana/gcx/internal/providers/agento11y/eval/reviewqueues"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runCommand points a nil-loader command group at srv through a sandboxed
// config and runs args, so the command goes through config discovery, the
// plugin client and an HTTP round-trip.
func runCommand(t *testing.T, srv *httptest.Server, stdin string, args ...string) (string, string, error) {
	t.Helper()
	home := testutils.SandboxConfigEnv(t)
	cfgPath := filepath.Join(home, ".config", "gcx", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(cfgPath), 0o755))
	cfg := fmt.Sprintf(`version: 1
stacks:
  main:
    grafana:
      server: %s
      token: test-token
      stack-id: 11111
contexts:
  default:
    stack: main
current-context: default
`, srv.URL)
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), 0o600))

	cmd := reviewqueues.Commands(nil)
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	err := cmd.ExecuteContext(t.Context())
	return out.String(), errOut.String(), err
}

func TestCommands_Validation(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "get blank collection", args: []string{"get", " "}, want: "collection ID is required"},
		{name: "get blank conversation", args: []string{"get", "col", "--conversation", " "}, want: "--conversation cannot be blank"},
		{name: "update needs a rubric", args: []string{"update", "col"}, want: "at least one --rubric"},
		{name: "update blank rubric", args: []string{"update", "col", "--rubric", " "}, want: "--rubric cannot be blank"},
		{name: "create-review needs a file", args: []string{"create-review", "col"}, want: "--filename is required"},
		{name: "list-scores blank collection", args: []string{"list-scores", " "}, want: "collection ID is required"},
		{name: "list rejects args", args: []string{"list", "extra"}, want: "unknown command"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := reviewqueues.Commands(nil)
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			cmd.SetArgs(tt.args)
			require.ErrorContains(t, cmd.Execute(), tt.want)
		})
	}
}

func TestCreateReview_FileInput(t *testing.T) {
	const jsonReview = `{"conversation_id":"conv-1","generation_id":"gen-1","note":"fine","answers":[{"evaluator_id":"helpful","evaluator_version":"v1","value":{"number":4}}]}`
	const yamlReview = `conversation_id: conv-1
generation_id: gen-1
note: fine
answers:
  - evaluator_id: helpful
    evaluator_version: v1
    value:
      number: 4
`
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "json", input: jsonReview},
		{name: "yaml", input: yamlReview},
		{name: "missing answers", input: `{"conversation_id":"c","generation_id":"g"}`, want: "at least one answer are required"},
		{name: "missing generation", input: `{"conversation_id":"c","answers":[{"evaluator_id":"h"}]}`, want: "generation_id"},
		{name: "not a document", input: "[1, 2", want: "parsing -"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body json.RawMessage
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, resourcesPath+"/eval/review-queues/col/reviews", r.URL.Path)
				_ = json.NewDecoder(r.Body).Decode(&body)
				writeJSON(w, http.StatusCreated, map[string]any{"scores": []any{}})
			}))
			t.Cleanup(srv.Close)

			stdout, stderr, err := runCommand(t, srv, tt.input, "create-review", "col", "-f", "-")
			if tt.want != "" {
				require.ErrorContains(t, err, tt.want)
				assert.Empty(t, body, "nothing may be sent for an invalid file")
				return
			}
			require.NoError(t, err, "stderr: %s", stderr)
			assert.JSONEq(t, jsonReview, string(body))
			assert.JSONEq(t, `{"scores":[]}`, stdout)
			assert.Contains(t, stderr, "conv-1")
		})
	}
}

func TestListScores_Envelope(t *testing.T) {
	const csvBody = "conversation_id,value\nc1,4\nc2,5\nc3,3\n"
	tests := []struct {
		name       string
		args       []string
		backendCut bool
		wantIDs    []string
		wantMeta   map[string]any
		wantStderr string
		wantNoMeta bool
	}{
		{name: "complete", wantIDs: []string{"c1", "c2", "c3"}, wantNoMeta: true},
		{
			name: "backend row cap rides in the payload", backendCut: true, wantIDs: []string{"c1", "c2", "c3"},
			wantMeta:   map[string]any{"truncated": true, "returned": 3.0, "cap": 3.0},
			wantStderr: "truncated by the backend row cap",
		},
		{
			name: "limit trims a complete export", args: []string{"--limit", "2"}, wantIDs: []string{"c1", "c2"},
			wantMeta: map[string]any{"truncated": true, "returned": 2.0, "total": 3.0},
		},
		{
			name: "limit under a backend cap leaves the total unknown", args: []string{"--limit", "2"}, backendCut: true, wantIDs: []string{"c1", "c2"},
			wantMeta:   map[string]any{"truncated": true, "returned": 2.0},
			wantStderr: "truncated by the backend row cap",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/csv; charset=utf-8")
				if tt.backendCut {
					w.Header().Set("X-Review-Export-Truncated", "true")
				}
				_, _ = io.WriteString(w, csvBody)
			}))
			t.Cleanup(srv.Close)

			args := append([]string{"list-scores", "col", "-o", "json"}, tt.args...)
			stdout, stderr, err := runCommand(t, srv, "", args...)
			require.NoError(t, err, "stderr: %s", stderr)

			var got struct {
				Items    []reviewqueues.ScoreRow `json:"items"`
				ListMeta map[string]any          `json:"list_meta"`
			}
			require.NoError(t, json.Unmarshal([]byte(stdout), &got), "stdout must be an items envelope: %s", stdout)
			ids := make([]string, 0, len(got.Items))
			for _, row := range got.Items {
				ids = append(ids, row.ConversationID)
			}
			assert.Equal(t, tt.wantIDs, ids)
			if tt.wantNoMeta {
				assert.Nil(t, got.ListMeta)
			} else {
				for k, v := range tt.wantMeta {
					assert.Equal(t, v, got.ListMeta[k], k)
				}
				if tt.backendCut {
					assert.NotContains(t, got.ListMeta, "total", "total must stay unknown when the backend cut the export")
				}
			}
			if tt.wantStderr != "" {
				assert.Contains(t, stderr, tt.wantStderr)
			}
		})
	}
}

func TestListScores_RejectsNegativeLimit(t *testing.T) {
	cmd := reviewqueues.Commands(nil)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"list-scores", "col", "--limit", "-1"})
	require.ErrorContains(t, cmd.Execute(), "--limit must be 0 or greater")
}

func TestGet_ConversationQualifierChangesEndpoint(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		writeJSON(w, http.StatusOK, map[string]any{"collection_id": "col"})
	}))
	t.Cleanup(srv.Close)

	_, stderr, err := runCommand(t, srv, "", "get", "col", "-o", "json")
	require.NoError(t, err, stderr)
	_, stderr, err = runCommand(t, srv, "", "get", "col", "--conversation", "conv-1", "-o", "json")
	require.NoError(t, err, stderr)
	assert.Equal(t, []string{
		resourcesPath + "/eval/review-queues/col",
		resourcesPath + "/eval/review-queues/col/conversations/conv-1",
	}, paths)
}

func TestUpdate_ReplacesRubric(t *testing.T) {
	var body json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeJSON(w, http.StatusOK, map[string]any{"collection_id": "col"})
	}))
	t.Cleanup(srv.Close)

	_, stderr, err := runCommand(t, srv, "", "update", "col", "--rubric", "a", "--rubric", "b", "-o", "json")
	require.NoError(t, err, stderr)
	assert.JSONEq(t, `{"rubric_evaluator_ids":["a","b"]}`, string(body))
	assert.Contains(t, stderr, "rubric updated")
}

// An explicitly empty --conversation must fail before any request: reading it
// as "unset" would fetch the whole queue instead of the requested view.
func TestGet_EmptyConversationSendsNoRequest(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		writeJSON(w, http.StatusOK, map[string]any{"collection_id": "col"})
	}))
	t.Cleanup(srv.Close)

	stdout, _, err := runCommand(t, srv, "", "get", "col", "--conversation", "", "-o", "json")
	require.ErrorContains(t, err, "--conversation cannot be blank")
	assert.Empty(t, stdout)
	assert.Zero(t, requests.Load())
}

// The backend counts a bounded number of conversations per queue. When it
// stops there, the default table would otherwise read as complete.
func TestProgressWarnings(t *testing.T) {
	queue := func(truncated bool) map[string]any {
		doc := withoutItems(queueDoc())
		if truncated {
			doc["truncated"] = true
		}
		return doc
	}
	tests := []struct {
		name      string
		args      []string
		respond   func() any
		wantWarn  bool
		wantField bool
	}{
		{name: "list truncated", args: []string{"list"}, respond: func() any { return map[string]any{"items": []any{queue(true)}} }, wantWarn: true},
		{name: "get truncated", args: []string{"get", "col/1"}, respond: func() any { return queue(true) }, wantWarn: true, wantField: true},
		{name: "get complete", args: []string{"get", "col/1"}, respond: func() any { return queue(false) }},
		{name: "get conversation truncated", args: []string{"get", "col/1", "--conversation", "conv-1"}, respond: func() any {
			view := queue(true)
			view["member"] = true
			view["position"] = 2
			view["item"] = map[string]any{"conversation_id": "conv-1", "reviewed": true, "reviewers": []string{"alice"}}
			return view
		}, wantWarn: true, wantField: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, http.StatusOK, tt.respond())
			}))
			t.Cleanup(srv.Close)

			stdout, stderr, err := runCommand(t, srv, "", append(tt.args, "-o", "json")...)
			require.NoError(t, err, "stderr: %s", stderr)
			if tt.wantWarn {
				assert.Contains(t, stderr, "progress is partial")
				assert.Contains(t, stderr, "lower bounds")
			} else {
				assert.NotContains(t, stderr, "partial")
			}
			if tt.wantField {
				assert.Contains(t, stdout, `"truncated": true`)
			}
		})
	}
}
