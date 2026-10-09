package scores_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/providers/agento11y/scores"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTableCodec_Encode(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	b := func(v bool) *bool { return &v }

	now := time.Date(2026, 4, 2, 18, 30, 0, 0, time.UTC)
	items := []scores.Score{
		{
			ScoreKey: "relevance", ScoreType: "number",
			Value: scores.ScoreValue{Number: f(0.95)}, Passed: b(true),
			EvaluatorID: "eval-1", EvaluatorVersion: "1.0", RuleID: "rule-1",
			Explanation: "High relevance", CreatedAt: now,
		},
		{
			ScoreKey: "harmful", ScoreType: "bool",
			Value: scores.ScoreValue{Bool: b(false)}, Passed: b(false),
			EvaluatorID: "eval-2", EvaluatorVersion: "2.0",
			CreatedAt: now,
		},
		{
			ScoreKey: "sentiment", ScoreType: "string",
			Value:       scores.ScoreValue{},
			EvaluatorID: "eval-3",
		},
	}

	tests := []struct {
		name       string
		wide       bool
		genMeta    bool
		want       []string
		wantAbsent []string
	}{
		{
			name: "table format",
			want: []string{"SCORE KEY", "VALUE", "PASSED", "EVALUATOR", "CREATED AT",
				"relevance", "0.95", "yes", "eval-1", "2026-04-02 18:30"},
		},
		{
			name:    "wide with gen meta includes VERSION, RULE, AGENT, MODEL, EXPLANATION",
			wide:    true,
			genMeta: true,
			want: []string{"TYPE", "VERSION", "RULE", "AGENT", "MODEL", "EXPLANATION",
				"number", "1.0", "rule-1", "High relevance"},
		},
		{
			name:       "wide without gen meta omits AGENT and MODEL",
			wide:       true,
			want:       []string{"TYPE", "VERSION", "RULE", "EXPLANATION", "High relevance"},
			wantAbsent: []string{"AGENT", "MODEL"},
		},
		{
			name: "failed shows no",
			want: []string{"harmful", "no"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			codec := &scores.TableCodec{Wide: tc.wide, GenMeta: tc.genMeta}
			var buf bytes.Buffer
			require.NoError(t, codec.Encode(&buf, items))

			output := buf.String()
			for _, s := range tc.want {
				assert.Contains(t, output, s)
			}
			for _, s := range tc.wantAbsent {
				assert.NotContains(t, output, s)
			}
		})
	}
}

func TestNewListRuleScoresCommand_Flags(t *testing.T) {
	cmd := scores.NewListRuleScoresCommand(nil)
	require.Equal(t, "list-scores <rule-id>", cmd.Use)

	f := cmd.Flags()
	assert.NotNil(t, f.Lookup("limit"))
	assert.NotNil(t, f.Lookup("evaluator-id"))
	assert.NotNil(t, f.Lookup("passed"))
	assert.NotNil(t, f.Lookup("from"))
	assert.NotNil(t, f.Lookup("to"))
	assert.NotNil(t, f.Lookup("agent-name"))
	assert.NotNil(t, f.Lookup("model"))
	assert.NotNil(t, f.Lookup("provider"))
	assert.NotNil(t, f.Lookup("score-value"))
	assert.NotNil(t, f.Lookup("min-value"))
	assert.NotNil(t, f.Lookup("max-value"))
	assert.NotNil(t, f.Lookup("sort-by"))
	assert.NotNil(t, f.Lookup("sort-dir"))

	// Tri-state: --passed is unset by default.
	assert.False(t, f.Changed("passed"))
	require.NoError(t, f.Set("passed", "false"))
	assert.True(t, f.Changed("passed"))
	passed, err := f.GetBool("passed")
	require.NoError(t, err)
	assert.False(t, passed)
}

func writeSandboxGrafanaConfig(t *testing.T, home, serverURL string) {
	t.Helper()
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
`, serverURL)
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), 0o600))
}

func TestNewListRuleScoresCommand_FlagMapping(t *testing.T) {
	home := testutils.SandboxConfigEnv(t)

	tests := []struct {
		name        string
		args        []string
		assertQuery func(t *testing.T, q url.Values)
	}{
		{
			name: "all flags map to query params",
			args: []string{
				"rule-1",
				"--limit", "50",
				"--evaluator-id", "eval-1",
				"--passed=false",
				"--from", "2026-04-01T00:00:00Z",
				"--to", "2026-04-02T00:00:00Z",
				"--agent-name", "agent-a", "--agent-name", "agent-b",
				"--model", "gpt-4",
				"--provider", "openai",
				"--score-value", "fail",
				"--min-value", "0.1",
				"--max-value", "0.9",
				"--sort-by", "value",
				"--sort-dir", "asc",
			},
			assertQuery: func(t *testing.T, q url.Values) {
				t.Helper()
				assert.Equal(t, "eval-1", q.Get("evaluator_id"))
				assert.Equal(t, "false", q.Get("passed"))
				assert.Equal(t, "2026-04-01T00:00:00Z", q.Get("from"))
				assert.Equal(t, "2026-04-02T00:00:00Z", q.Get("to"))
				assert.Equal(t, []string{"agent-a", "agent-b"}, q["agent_name"])
				assert.Equal(t, []string{"gpt-4"}, q["model"])
				assert.Equal(t, []string{"openai"}, q["provider"])
				assert.Equal(t, []string{"fail"}, q["score_value"])
				assert.Equal(t, "0.1", q.Get("min_value"))
				assert.Equal(t, "0.9", q.Get("max_value"))
				assert.Equal(t, "value", q.Get("sort_by"))
				assert.Equal(t, "asc", q.Get("sort_dir"))
				assert.Equal(t, "50", q.Get("limit"))
			},
		},
		{
			name: "passed omitted",
			args: []string{"rule-1"},
			assertQuery: func(t *testing.T, q url.Values) {
				t.Helper()
				_, ok := q["passed"]
				assert.False(t, ok, "unset --passed must not send passed filter")
				_, hasMin := q["min_value"]
				_, hasMax := q["max_value"]
				assert.False(t, hasMin, "unset --min-value must not send min_value filter")
				assert.False(t, hasMax, "unset --max-value must not send max_value filter")
				assert.Equal(t, "100", q.Get("limit"))
				assert.Equal(t, "created_at", q.Get("sort_by"))
				assert.Equal(t, "desc", q.Get("sort_dir"))
			},
		},
		{
			name: "passed true",
			args: []string{"rule-1", "--passed=true"},
			assertQuery: func(t *testing.T, q url.Values) {
				t.Helper()
				assert.Equal(t, "true", q.Get("passed"))
			},
		},
		{
			name: "min and max explicitly set",
			args: []string{"rule-1", "--min-value", "0.1", "--max-value", "0.9"},
			assertQuery: func(t *testing.T, q url.Values) {
				t.Helper()
				assert.Equal(t, "0.1", q.Get("min_value"))
				assert.Equal(t, "0.9", q.Get("max_value"))
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var captured url.Values
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Contains(t, r.URL.Path, "/eval/rules/rule-1/scores")
				captured = r.URL.Query()
				w.Header().Set("Content-Type", "application/json")
				assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"items": []any{}}))
			}))
			t.Cleanup(srv.Close)
			writeSandboxGrafanaConfig(t, home, srv.URL)

			cmd := scores.NewListRuleScoresCommand(nil)
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetArgs(tc.args)

			require.NoError(t, cmd.ExecuteContext(context.Background()), "stderr: %s", stderr.String())
			require.NotNil(t, captured, "command must reach the API")
			tc.assertQuery(t, captured)
		})
	}
}

func TestNewListRuleScoresCommand_ValidateBeforeClient(t *testing.T) {
	run := func(args ...string) error {
		cmd := scores.NewListRuleScoresCommand(nil)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(append([]string{"rule-1"}, args...))
		return cmd.Execute()
	}

	t.Run("invalid sort-dir", func(t *testing.T) {
		err := run("--sort-dir", "sideways")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--sort-dir")
		assert.NotContains(t, err.Error(), "sort_dir")
	})

	t.Run("invalid sort-by", func(t *testing.T) {
		err := run("--sort-by", "score_key")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--sort-by")
	})

	t.Run("negative limit", func(t *testing.T) {
		err := run("--limit", "-1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--limit")
	})

	t.Run("too many score values", func(t *testing.T) {
		args := make([]string, 0, 42)
		for range 21 {
			args = append(args, "--score-value", "v")
		}
		err := run(args...)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--score-value")
	})

	t.Run("min greater than max", func(t *testing.T) {
		err := run("--min-value", "1", "--max-value", "0.5")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--min-value")
		assert.Contains(t, err.Error(), "--max-value")
	})

	t.Run("invalid from", func(t *testing.T) {
		err := run("--from", "yesterday")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid --from value:")
	})

	t.Run("invalid to", func(t *testing.T) {
		err := run("--to", "not-a-time")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid --to value:")
	})

	t.Run("from after to", func(t *testing.T) {
		err := run("--from", "2026-04-02T00:00:00Z", "--to", "2026-04-01T00:00:00Z")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--from must be before --to")
	})

	t.Run("non-finite min-value", func(t *testing.T) {
		err := run("--min-value", "NaN")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--min-value")
		assert.Contains(t, err.Error(), "finite")
	})

	t.Run("non-finite max-value", func(t *testing.T) {
		err := run("--max-value", "Inf")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--max-value")
		assert.Contains(t, err.Error(), "finite")
	})

	t.Run("explicit empty scalar filter", func(t *testing.T) {
		err := run("--evaluator-id", "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--evaluator-id")
		assert.Contains(t, err.Error(), "empty value")
	})

	t.Run("explicit empty sort-by reports allowed values", func(t *testing.T) {
		// sort-by is not a filter: an empty value falls through to the sort switch,
		// which gives the more useful allowed-values error rather than "omit ...".
		err := run("--sort-by", "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--sort-by must be")
		assert.Contains(t, err.Error(), "created_at")
	})

	t.Run("empty repeatable filter entry", func(t *testing.T) {
		err := run("--agent-name", "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--agent-name")
		assert.Contains(t, err.Error(), "empty")
	})
}

func TestNewListRuleScoresCommand_TruncationWarning(t *testing.T) {
	home := testutils.SandboxConfigEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"items": []scores.Score{
				{ScoreID: "s-1", ScoreKey: "relevance"},
				{ScoreID: "s-2", ScoreKey: "harmful"},
			},
			"next_cursor": "page-2",
		}))
	}))
	t.Cleanup(srv.Close)
	writeSandboxGrafanaConfig(t, home, srv.URL)

	cmd := scores.NewListRuleScoresCommand(nil)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"rule-1", "--limit", "2", "-o", "json"})
	require.NoError(t, cmd.ExecuteContext(context.Background()), "stderr: %s", stderr.String())

	// Standardized §15.4 truncation hint on stderr; the hint never leaks to stdout.
	assert.Contains(t, stderr.String(), "showing first 2")
	assert.Contains(t, stderr.String(), "more results are available")
	assert.NotContains(t, stdout.String(), "showing first")

	// The rule command emits the list envelope, carrying list_meta in-band.
	var got struct {
		Items    []scores.Score `json:"items"`
		ListMeta *struct {
			Truncated bool `json:"truncated"`
		} `json:"list_meta"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &got))
	assert.Len(t, got.Items, 2)
	require.NotNil(t, got.ListMeta, "envelope must carry list_meta when truncated")
	assert.True(t, got.ListMeta.Truncated)
}

func TestNewListRuleScoresCommand_CapBoundedHint(t *testing.T) {
	home := testutils.SandboxConfigEnv(t)
	// Always return a full page + cursor: --limit 0 must stop at the hard cap.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		page := make([]scores.Score, 500)
		for i := range page {
			page[i] = scores.Score{ScoreID: "s", ScoreKey: "a"}
		}
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"items": page, "next_cursor": "more"}))
	}))
	t.Cleanup(srv.Close)
	writeSandboxGrafanaConfig(t, home, srv.URL)

	cmd := scores.NewListRuleScoresCommand(nil)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"rule-1", "--limit", "0", "-o", "json"})
	require.NoError(t, cmd.ExecuteContext(context.Background()), "stderr: %s", stderr.String())

	// Cap-bounded: the hint says so and offers no --limit continuation.
	assert.Contains(t, stderr.String(), "safety cap")
	assert.Contains(t, stderr.String(), "Refine filters")

	var got struct {
		Items    []scores.Score `json:"items"`
		ListMeta *struct {
			Truncated bool   `json:"truncated"`
			Cap       int    `json:"cap"`
			Continue  string `json:"continue"`
		} `json:"list_meta"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &got))
	assert.Len(t, got.Items, scores.ScoreHardCap())
	require.NotNil(t, got.ListMeta)
	assert.Equal(t, scores.ScoreHardCap(), got.ListMeta.Cap)
	assert.Empty(t, got.ListMeta.Continue, "cap-bounded page offers no larger --limit continuation")
}

func TestNewListRuleScoresCommand_RejectEmptyID(t *testing.T) {
	cmd := scores.NewListRuleScoresCommand(nil)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"   "})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rule ID must not be empty")
}

func TestNewListRuleScoresCommand_TrimIDBeforeRequest(t *testing.T) {
	home := testutils.SandboxConfigEnv(t)
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"items": []any{}}))
	}))
	t.Cleanup(srv.Close)
	writeSandboxGrafanaConfig(t, home, srv.URL)

	cmd := scores.NewListRuleScoresCommand(nil)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"  rule-1  "})
	require.NoError(t, cmd.ExecuteContext(context.Background()))
	// The padded ID is trimmed before it reaches the URL — no %20 padding.
	assert.Contains(t, gotPath, "/eval/rules/rule-1/scores")
	assert.NotContains(t, gotPath, "%20")
	assert.NotContains(t, gotPath, "  ")
}

func TestNewListRuleScoresCommand_TableOutput(t *testing.T) {
	home := testutils.SandboxConfigEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"items": []scores.Score{{ScoreID: "s-1", ScoreKey: "relevance", EvaluatorID: "eval-1"}},
		}))
	}))
	t.Cleanup(srv.Close)
	writeSandboxGrafanaConfig(t, home, srv.URL)

	cmd := scores.NewListRuleScoresCommand(nil)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	// Default (table) output must render the envelope's rows, not error on the type.
	cmd.SetArgs([]string{"rule-1"})
	require.NoError(t, cmd.ExecuteContext(context.Background()), "stderr: %s", stderr.String())
	assert.Contains(t, stdout.String(), "SCORE KEY")
	assert.Contains(t, stdout.String(), "relevance")
}

func TestTableCodec_WideWithAgentModel(t *testing.T) {
	b := func(v bool) *bool { return &v }
	f := func(v float64) *float64 { return &v }
	now := time.Date(2026, 4, 2, 18, 30, 0, 0, time.UTC)
	items := []scores.Score{
		{
			ScoreKey: "relevance", ScoreType: "number",
			Value: scores.ScoreValue{Number: f(0.2)}, Passed: b(false),
			EvaluatorID: "eval-1", EvaluatorVersion: "1.0", RuleID: "rule-1",
			AgentName: "billing-bot", GenModel: "gpt-4",
			Explanation: "Off topic", CreatedAt: now,
		},
	}

	codec := &scores.TableCodec{Wide: true, GenMeta: true}
	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, items))
	output := buf.String()
	assert.Contains(t, output, "billing-bot")
	assert.Contains(t, output, "gpt-4")
	assert.Contains(t, output, "Off topic")
}

func TestTableCodec_WrongType(t *testing.T) {
	codec := &scores.TableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, "not-a-slice")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected []Score")
}

func TestTableCodec_Format(t *testing.T) {
	tests := []struct {
		wide   bool
		expect string
	}{
		{false, "table"},
		{true, "wide"},
	}
	for _, tc := range tests {
		codec := &scores.TableCodec{Wide: tc.wide}
		assert.Equal(t, tc.expect, string(codec.Format()))
	}
}

func TestTableCodec_DecodeUnsupported(t *testing.T) {
	codec := &scores.TableCodec{}
	err := codec.Decode(nil, nil)
	require.Error(t, err)
}
