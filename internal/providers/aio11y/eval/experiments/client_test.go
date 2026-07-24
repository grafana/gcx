package experiments_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
	"github.com/grafana/gcx/internal/providers/aio11y/eval/experiments"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func writeJSON(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func newTestClient(t *testing.T, handler http.Handler) *experiments.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srv.URL},
		Namespace: "default",
	}
	base, err := aio11yhttp.NewClient(cfg)
	require.NoError(t, err)
	return experiments.NewClient(base)
}

func TestClient_List(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/plugins/grafana-sigil-app/resources/eval/experiments", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, map[string]any{
			"items": []experiments.Experiment{
				{RunID: "r-1", Name: "exp-1", Status: "succeeded"},
				{RunID: "r-2", Name: "exp-2", Status: "running"},
			},
		})
	}))

	items, err := client.List(context.Background(), 0)
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "r-1", items[0].RunID)
	assert.Equal(t, "running", items[1].Status)
}

func TestClient_List_TransportError(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))

	_, err := client.List(context.Background(), 0)
	require.Error(t, err)
}

func TestClient_ListSuites(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/plugins/grafana-sigil-app/resources/eval/test-suites", r.URL.Path)

		writeJSON(w, map[string]any{
			"items": []experiments.TestSuite{
				{SuiteID: "suite-1", Name: "Suite 1", LatestVersion: "v1"},
			},
		})
	}))

	items, err := client.ListSuites(context.Background(), 0)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "suite-1", items[0].SuiteID)
}

func TestClient_GetSuite(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/plugins/grafana-sigil-app/resources/eval/test-suites/suite-1", r.URL.Path)

		writeJSON(w, experiments.TestSuite{
			SuiteID: "suite-1",
			Name:    "Suite 1",
			Versions: []experiments.TestSuiteVersion{
				{SuiteID: "suite-1", Version: "v1", Published: true},
			},
		})
	}))

	suite, err := client.GetSuite(context.Background(), "suite-1")
	require.NoError(t, err)
	assert.Equal(t, "suite-1", suite.SuiteID)
	require.Len(t, suite.Versions, 1)
}

func TestClient_CreateSuiteVersionAndPublish(t *testing.T) {
	var calls []string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.EscapedPath())
		switch r.URL.EscapedPath() {
		case "/api/plugins/grafana-sigil-app/resources/eval/test-suites/suite-1/versions":
			assert.Equal(t, http.MethodPost, r.Method)
			body, _ := io.ReadAll(r.Body)
			var raw map[string]any
			assert.NoError(t, json.Unmarshal(body, &raw))
			assert.Equal(t, "draft", raw["changelog"])
			writeJSON(w, experiments.TestSuiteVersion{SuiteID: "suite-1", Version: "v2"})
		case "/api/plugins/grafana-sigil-app/resources/eval/test-suites/suite-1/versions/v2:publish":
			assert.Equal(t, http.MethodPost, r.Method)
			writeJSON(w, experiments.TestSuiteVersion{SuiteID: "suite-1", Version: "v2", Published: true})
		default:
			http.NotFound(w, r)
		}
	}))

	version, err := client.CreateSuiteVersion(context.Background(), "suite-1", &experiments.CreateTestSuiteVersionRequest{Changelog: "draft"})
	require.NoError(t, err)
	assert.Equal(t, "v2", version.Version)

	published, err := client.PublishSuiteVersion(context.Background(), "suite-1", "v2")
	require.NoError(t, err)
	assert.True(t, published.Published)
	assert.Equal(t, []string{
		"POST /api/plugins/grafana-sigil-app/resources/eval/test-suites/suite-1/versions",
		"POST /api/plugins/grafana-sigil-app/resources/eval/test-suites/suite-1/versions/v2:publish",
	}, calls)
}

func TestClient_Cases(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/plugins/grafana-sigil-app/resources/eval/test-suites/suite-1/versions/v1/test-cases":
			writeJSON(w, map[string]any{"items": []experiments.TestCase{{TestCaseID: "case-1", SuiteID: "suite-1", SuiteVersion: "v1"}}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/plugins/grafana-sigil-app/resources/eval/test-suites/suite-1/versions/v1/test-cases/case-1":
			writeJSON(w, experiments.TestCase{TestCaseID: "case-1"})
		case r.Method == http.MethodPost && r.URL.Path == "/api/plugins/grafana-sigil-app/resources/eval/test-suites/suite-1/versions/v1/test-cases":
			body, _ := io.ReadAll(r.Body)
			assert.Contains(t, string(body), `"test_case_id":"case-1"`)
			writeJSON(w, experiments.TestCase{TestCaseID: "case-1"})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/plugins/grafana-sigil-app/resources/eval/test-suites/suite-1/versions/v1/test-cases/case-1":
			body, _ := io.ReadAll(r.Body)
			assert.Contains(t, string(body), `"name":"renamed"`)
			writeJSON(w, experiments.TestCase{TestCaseID: "case-1", Name: "renamed"})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/plugins/grafana-sigil-app/resources/eval/test-suites/suite-1/versions/v1/test-cases/case-1":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))

	cases, err := client.ListCases(context.Background(), "suite-1", "v1", 0)
	require.NoError(t, err)
	require.Len(t, cases, 1)

	tc, err := client.GetCase(context.Background(), "suite-1", "v1", "case-1")
	require.NoError(t, err)
	assert.Equal(t, "case-1", tc.TestCaseID)

	tc, err = client.UpsertCase(context.Background(), "suite-1", "v1", &experiments.TestCase{TestCaseID: "case-1", Input: map[string]any{"prompt": "hi"}})
	require.NoError(t, err)
	assert.Equal(t, "case-1", tc.TestCaseID)

	tc, err = client.PatchCase(context.Background(), "suite-1", "v1", "case-1", map[string]any{"name": "renamed"})
	require.NoError(t, err)
	assert.Equal(t, "renamed", tc.Name)

	require.NoError(t, client.DeleteCase(context.Background(), "suite-1", "v1", "case-1"))
}

func TestClient_Get(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/plugins/grafana-sigil-app/resources/eval/experiments/r-1", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, experiments.Experiment{
			RunID:     "r-1",
			Name:      "exp-1",
			Status:    "running",
			Source:    "external",
			CreatedAt: time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC),
		})
	}))

	exp, err := client.Get(context.Background(), "r-1")
	require.NoError(t, err)
	assert.Equal(t, "r-1", exp.RunID)
	assert.Equal(t, "external", exp.Source)
}

func TestClient_Trials(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/plugins/grafana-sigil-app/resources/eval/experiments/exp-1/trials":
			writeJSON(w, map[string]any{"items": []experiments.TestCaseTrial{{TrialID: "trial-1", ExperimentID: "exp-1", TestCaseID: "case-1", Attempt: 1}}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/plugins/grafana-sigil-app/resources/eval/experiments/exp-1/trials":
			body, _ := io.ReadAll(r.Body)
			assert.Contains(t, string(body), `"test_case_id":"case-1"`)
			writeJSON(w, experiments.TestCaseTrial{TrialID: "trial-1", ExperimentID: "exp-1", TestCaseID: "case-1", Attempt: 1})
		case r.Method == http.MethodGet && r.URL.Path == "/api/plugins/grafana-sigil-app/resources/eval/test-case-trials/trial-1":
			writeJSON(w, experiments.TestCaseTrial{TrialID: "trial-1", ExperimentID: "exp-1", TestCaseID: "case-1", Attempt: 1})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/plugins/grafana-sigil-app/resources/eval/test-case-trials/trial-1":
			body, _ := io.ReadAll(r.Body)
			assert.Contains(t, string(body), `"status":"completed"`)
			writeJSON(w, experiments.TestCaseTrial{TrialID: "trial-1", Status: "completed"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/plugins/grafana-sigil-app/resources/eval/test-case-trials/trial-1/scores":
			writeJSON(w, map[string]any{"items": []experiments.ScoreItem{{ScoreID: "score-1", TrialID: "trial-1", ScoreKey: "final"}}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/plugins/grafana-sigil-app/resources/eval/test-case-trials/trial-1/artifacts":
			writeJSON(w, map[string]any{"items": []experiments.Artifact{{ArtifactID: "art-1", ParentKind: "test_case_trial", ParentID: "trial-1", Name: "output.json", Kind: "json"}}})
		default:
			http.NotFound(w, r)
		}
	}))

	trials, err := client.ListTrials(context.Background(), "exp-1", 0)
	require.NoError(t, err)
	require.Len(t, trials, 1)

	trial, err := client.CreateTrial(context.Background(), "exp-1", &experiments.TestCaseTrial{TestCaseID: "case-1", Attempt: 1})
	require.NoError(t, err)
	assert.Equal(t, "trial-1", trial.TrialID)

	trial, err = client.GetTrial(context.Background(), "trial-1")
	require.NoError(t, err)
	assert.Equal(t, "case-1", trial.TestCaseID)

	status := "completed"
	trial, err = client.UpdateTrial(context.Background(), "trial-1", &experiments.UpdateTrialRequest{Status: &status})
	require.NoError(t, err)
	assert.Equal(t, "completed", trial.Status)

	scores, err := client.ListTrialScores(context.Background(), "trial-1", 0)
	require.NoError(t, err)
	require.Len(t, scores, 1)
	assert.Equal(t, "score-1", scores[0].ScoreID)

	artifacts, err := client.ListTrialArtifacts(context.Background(), "trial-1", 0)
	require.NoError(t, err)
	require.Len(t, artifacts, 1)
	assert.Equal(t, "art-1", artifacts[0].ArtifactID)
}

func TestClient_Get_NotFound(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))

	_, err := client.Get(context.Background(), "missing")
	require.Error(t, err)
	require.ErrorIs(t, err, experiments.ErrNotFound)
}

func TestClient_Get_TransportError(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))

	_, err := client.Get(context.Background(), "r-1")
	require.Error(t, err)
}

func TestClient_Create(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/plugins/grafana-sigil-app/resources/eval/experiments", r.URL.Path)

		body, _ := io.ReadAll(r.Body)
		var raw map[string]any
		assert.NoError(t, json.Unmarshal(body, &raw))
		assert.Equal(t, "exp-1", raw["name"])
		assert.Equal(t, "Nightly", raw["description"])
		assert.Equal(t, "external", raw["source"])
		assert.Equal(t, []any{"support", "prompt-v2"}, raw["tags"])

		w.WriteHeader(http.StatusCreated)
		writeJSON(w, experiments.Experiment{
			RunID:       "r-99",
			Name:        "exp-1",
			Description: "Nightly",
			Tags:        []string{"support", "prompt-v2"},
			Source:      "external",
			Status:      "pending",
		})
	}))

	exp, err := client.Create(context.Background(), &experiments.Experiment{
		Name:        "exp-1",
		Description: "Nightly",
		Tags:        []string{"support", "prompt-v2"},
		Source:      "external",
	})
	require.NoError(t, err)
	assert.Equal(t, "r-99", exp.RunID)
	assert.Equal(t, "pending", exp.Status)
	assert.Equal(t, []string{"support", "prompt-v2"}, exp.Tags)
}

func TestClient_Create_TransportError(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))

	_, err := client.Create(context.Background(), &experiments.Experiment{Name: "exp"})
	require.Error(t, err)
}

func TestClient_Update_PATCH(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPatch, r.Method)
		assert.Equal(t, "/api/plugins/grafana-sigil-app/resources/eval/experiments/r-1", r.URL.Path)

		body, _ := io.ReadAll(r.Body)
		var raw map[string]any
		assert.NoError(t, json.Unmarshal(body, &raw))
		assert.Equal(t, "renamed", raw["name"])
		assert.Equal(t, "Updated notes", raw["description"])
		assert.Equal(t, []any{"support", "release"}, raw["tags"])

		w.WriteHeader(http.StatusOK)
		writeJSON(w, experiments.Experiment{RunID: "r-1", Name: "renamed", Description: "Updated notes", Tags: []string{"support", "release"}})
	}))

	name := "renamed"
	description := "Updated notes"
	tags := []string{"support", "release"}
	exp, err := client.Update(context.Background(), "r-1", &experiments.UpdateRequest{Name: &name, Description: &description, Tags: &tags})
	require.NoError(t, err)
	assert.Equal(t, "renamed", exp.Name)
	assert.Equal(t, "Updated notes", exp.Description)
	assert.Equal(t, []string{"support", "release"}, exp.Tags)
}

func TestClient_Update_NotFound(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))

	name := "renamed"
	_, err := client.Update(context.Background(), "missing", &experiments.UpdateRequest{Name: &name})
	require.Error(t, err)
	require.ErrorIs(t, err, experiments.ErrNotFound)
}

func TestClient_Update_TransportError(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))

	name := "renamed"
	_, err := client.Update(context.Background(), "r-1", &experiments.UpdateRequest{Name: &name})
	require.Error(t, err)
}

func TestClient_Cancel(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/plugins/grafana-sigil-app/resources/eval/experiments/r-1:cancel", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))

	require.NoError(t, client.Cancel(context.Background(), "r-1"))
}

func TestClient_Cancel_EscapesColonInRunID(t *testing.T) {
	// A literal `:` in a runID must be escaped so the `:cancel` suffix match
	// stays unambiguous. r.URL.Path is the decoded form, so we check RawPath
	// to see the wire bytes.
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/plugins/grafana-sigil-app/resources/eval/experiments/r%3Afoo:cancel", r.URL.EscapedPath())
		w.WriteHeader(http.StatusOK)
	}))

	require.NoError(t, client.Cancel(context.Background(), "r:foo"))
}

func TestClient_Cancel_NotFound(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))

	err := client.Cancel(context.Background(), "missing")
	require.Error(t, err)
	require.ErrorIs(t, err, experiments.ErrNotFound)
}

func TestClient_Cancel_TransportError(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))

	require.Error(t, client.Cancel(context.Background(), "r-1"))
}

func TestClient_ListScores(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/plugins/grafana-sigil-app/resources/eval/experiments/r-1/scores", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, map[string]any{
			"items": []map[string]any{
				{
					"score_id":          "s-1",
					"run_id":            "r-1",
					"evaluator_id":      "ev-1",
					"evaluator_version": "v1",
					"generation_id":     "gen-1",
					"score_key":         "quality",
					"score_type":        "number",
					"value":             map[string]any{"number": 0.9},
					"passed":            true,
					"explanation":       "looks good",
					"created_at":        "2026-04-01T10:00:00Z",
					"ingested_at":       "2026-04-01T10:00:01Z",
				},
				{"score_id": "s-2", "evaluator_id": "ev-2", "score_key": "tone"},
			},
		})
	}))

	items, err := client.ListScores(context.Background(), "r-1", 0)
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "s-1", items[0].ScoreID)
	assert.Equal(t, "quality", items[0].ScoreKey)
	assert.Equal(t, "number", items[0].ScoreType)
	assert.Equal(t, "looks good", items[0].Explanation)
	require.NotNil(t, items[0].Value.Number)
	assert.InDelta(t, 0.9, *items[0].Value.Number, 1e-9)
}

func TestClient_ListScores_TransportError(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))

	_, err := client.ListScores(context.Background(), "r-1", 0)
	require.Error(t, err)
}

func TestClient_GetReport(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/plugins/grafana-sigil-app/resources/eval/experiments/r-1/report", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, map[string]any{
			"run": map[string]any{
				"run_id":      "r-1",
				"name":        "exp-1",
				"source":      "external",
				"status":      "succeeded",
				"score_count": 10,
				"created_at":  "2026-04-01T10:00:00Z",
				"updated_at":  "2026-04-01T10:05:00Z",
			},
			"summary": map[string]any{
				"n_conversations": 2,
				"n_generations":   3,
				"n_scores":        10,
				"pass_rate":       0.8,
				"mean_score":      0.72,
				"total_cost_usd":  0.1234,
				"total_tokens":    42,
			},
			"breakdowns": map[string]any{
				"by_evaluator": []map[string]any{
					{"key": "ev-1", "count": 10, "pass_rate": 0.8, "mean_score": 0.72, "total_cost_usd": 0.1234, "total_tokens": 42},
				},
				"by_score_key": []map[string]any{
					{"key": "quality", "count": 10, "pass_rate": 0.8, "mean_score": 0.72},
				},
			},
			"points": []map[string]any{
				{
					"conversation_id": "conv-1",
					"generation_id":   "gen-1",
					"score_id":        "s-1",
					"evaluator_id":    "ev-1",
					"score_key":       "quality",
					"score_type":      "number",
					"value":           map[string]any{"number": 0.9},
					"value_number":    0.9,
					"passed":          true,
					"created_at":      "2026-04-01T10:00:00Z",
				},
			},
		})
	}))

	report, err := client.GetReport(context.Background(), "r-1")
	require.NoError(t, err)
	assert.Equal(t, "r-1", report.Run.RunID)
	assert.Equal(t, "exp-1", report.Run.Name)
	assert.Equal(t, 10, report.Summary.NScores)
	assert.Equal(t, 2, report.Summary.NConversations)
	assert.InDelta(t, 0.8, report.Summary.PassRate, 1e-9)
	require.Len(t, report.Breakdowns.ByEvaluator, 1)
	assert.Equal(t, "ev-1", report.Breakdowns.ByEvaluator[0].Key)
	require.Len(t, report.Points, 1)
	assert.Equal(t, "quality", report.Points[0].ScoreKey)
}

func TestClient_GetReport_NotFound(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))

	_, err := client.GetReport(context.Background(), "missing")
	require.Error(t, err)
	require.ErrorIs(t, err, experiments.ErrNotFound)
}

func TestClient_GetReport_TransportError(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))

	_, err := client.GetReport(context.Background(), "r-1")
	require.Error(t, err)
}
