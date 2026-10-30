package reviewqueues_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/agento11y/agento11yhttp"
	"github.com/grafana/gcx/internal/providers/agento11y/eval/reviewqueues"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

const resourcesPath = "/api/plugins/grafana-agento11y-app/resources"

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func newTestClient(t *testing.T, handler http.Handler) *reviewqueues.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	base, err := agento11yhttp.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: srv.URL}, Namespace: "default"})
	require.NoError(t, err)
	return reviewqueues.NewClient(base)
}

func queueDoc() map[string]any {
	return map[string]any{
		"collection_id": "col/1",
		"name":          "Regression",
		"rubric":        []map[string]any{{"evaluator_id": "helpful", "evaluator_version": "v1", "output_key": map[string]any{"key": "helpful", "type": "number"}}},
		"total":         3, "reviewed": 1, "pending": 2,
		"next_conversation_id": "conv-2",
		"items":                []map[string]any{{"conversation_id": "conv-1", "reviewed": true, "reviewers": []string{"alice"}, "scores": []map[string]any{{"evaluator_id": "helpful", "score_key": "helpful", "value": map[string]any{"number": 4}, "reviewer": "alice"}}}},
	}
}

func withoutItems(doc map[string]any) map[string]any {
	delete(doc, "items")
	return doc
}

func TestClient_Requests(t *testing.T) {
	tests := []struct {
		name       string
		wantMethod string
		wantPath   string
		wantBody   string
		status     int
		response   any
		call       func(*reviewqueues.Client) error
	}{
		{
			name: "list", wantMethod: http.MethodGet, wantPath: "/eval/review-queues", status: http.StatusOK,
			response: map[string]any{"items": []any{withoutItems(queueDoc())}},
			call: func(c *reviewqueues.Client) error {
				queues, err := c.List(context.Background())
				if err == nil {
					assert.Len(t, queues, 1)
					assert.Empty(t, queues[0].Items)
				}
				return err
			},
		},
		{
			name: "get escapes the collection ID", wantMethod: http.MethodGet, wantPath: "/eval/review-queues/col%2F1", status: http.StatusOK,
			response: queueDoc(),
			call: func(c *reviewqueues.Client) error {
				queue, err := c.Get(context.Background(), "col/1")
				if err == nil {
					assert.Equal(t, 2, queue.Pending)
					assert.Equal(t, "conv-2", queue.NextConversationID)
					assert.InDelta(t, 4.0, *queue.Items[0].Scores[0].Value.Number, 0.0001)
				}
				return err
			},
		},
		{
			name: "get conversation", wantMethod: http.MethodGet, wantPath: "/eval/review-queues/col1/conversations/conv%2F1", status: http.StatusOK,
			response: map[string]any{"collection_id": "col1", "member": true, "position": 2, "item": map[string]any{"conversation_id": "conv/1"}},
			call: func(c *reviewqueues.Client) error {
				view, err := c.GetConversation(context.Background(), "col1", "conv/1")
				if err == nil {
					assert.True(t, view.Member)
					assert.Equal(t, 2, view.Position)
				}
				return err
			},
		},
		{
			name: "set rubric", wantMethod: http.MethodPut, wantPath: "/eval/review-queues/col1", status: http.StatusOK,
			wantBody: `{"rubric_evaluator_ids":["a","b"]}`, response: queueDoc(),
			call: func(c *reviewqueues.Client) error {
				_, err := c.SetRubric(context.Background(), "col1", &reviewqueues.RubricRequest{RubricEvaluatorIDs: []string{"a", "b"}})
				return err
			},
		},
		{
			name: "submit review", wantMethod: http.MethodPost, wantPath: "/eval/review-queues/col1/reviews", status: http.StatusCreated,
			wantBody: `{"conversation_id":"conv-1","generation_id":"gen-1","answers":[{"evaluator_id":"helpful","evaluator_version":"v1","value":{"number":4}}],"note":"ok"}`,
			response: map[string]any{"scores": []map[string]any{{"score_id": "s1", "future_field": "kept"}}},
			call: func(c *reviewqueues.Client) error {
				four := 4.0
				result, err := c.SubmitReview(context.Background(), "col1", &reviewqueues.ReviewRequest{
					ConversationID: "conv-1", GenerationID: "gen-1", Note: "ok",
					Answers: []reviewqueues.Answer{{EvaluatorID: "helpful", EvaluatorVersion: "v1", Value: reviewqueues.ScoreValue{Number: &four}}},
				})
				if err == nil {
					assert.Equal(t, "kept", result.Scores[0]["future_field"])
				}
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, tt.wantMethod, r.Method)
				assert.Equal(t, resourcesPath+tt.wantPath, r.URL.EscapedPath())
				if tt.wantBody != "" {
					var got json.RawMessage
					if assert.NoError(t, json.NewDecoder(r.Body).Decode(&got)) {
						assert.JSONEq(t, tt.wantBody, string(got))
					}
				}
				writeJSON(w, tt.status, tt.response)
			}))
			require.NoError(t, tt.call(client))
		})
	}
}

func TestClient_NotFound(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))

	_, err := client.Get(context.Background(), "missing")
	require.ErrorIs(t, err, reviewqueues.ErrNotFound)
	_, err = client.GetConversation(context.Background(), "missing", "c")
	require.ErrorIs(t, err, reviewqueues.ErrNotFound)
	_, err = client.SetRubric(context.Background(), "missing", &reviewqueues.RubricRequest{})
	require.ErrorIs(t, err, reviewqueues.ErrNotFound)
	_, err = client.SubmitReview(context.Background(), "missing", &reviewqueues.ReviewRequest{})
	require.ErrorIs(t, err, reviewqueues.ErrNotFound)
	_, _, err = client.ExportScores(context.Background(), "missing")
	require.ErrorIs(t, err, reviewqueues.ErrNotFound)
}

func TestClient_ExportScores(t *testing.T) {
	const header = "conversation_id,generation_id,agent_name,agent_version,evaluator_id,evaluator_version,score_key,value,passed,reviewer,note,created_at\n"
	tests := []struct {
		name          string
		body          string
		truncated     string
		status        int
		wantRows      []reviewqueues.ScoreRow
		wantTruncated bool
		wantErr       string
	}{
		{
			name:   "rows decoded by header name",
			body:   header + "c1,g1,bot,v2,helpful,v1,helpful,4,true,alice,\"multi, line\",2026-09-01T12:00:00Z\n" + "c2,g2,,,helpful,v1,helpful,'=cmd,,bob,,2026-09-02T12:00:00Z\n",
			status: http.StatusOK,
			wantRows: []reviewqueues.ScoreRow{
				{ConversationID: "c1", GenerationID: "g1", AgentName: "bot", AgentVersion: "v2", EvaluatorID: "helpful", EvaluatorVersion: "v1", ScoreKey: "helpful", Value: "4", Passed: "true", Reviewer: "alice", Note: "multi, line", CreatedAt: "2026-09-01T12:00:00Z"},
				{ConversationID: "c2", GenerationID: "g2", EvaluatorID: "helpful", EvaluatorVersion: "v1", ScoreKey: "helpful", Value: "'=cmd", Reviewer: "bob", CreatedAt: "2026-09-02T12:00:00Z"},
			},
		},
		{name: "header only is an empty list", body: header, status: http.StatusOK, wantRows: []reviewqueues.ScoreRow{}},
		{
			name: "reordered and extra columns are tolerated", status: http.StatusOK,
			body:     "future,value,conversation_id\nx,5,c9\n",
			wantRows: []reviewqueues.ScoreRow{{ConversationID: "c9", Value: "5"}},
		},
		{name: "truncation header", body: header, truncated: "true", status: http.StatusOK, wantRows: []reviewqueues.ScoreRow{}, wantTruncated: true},
		{name: "unexpected format", body: "not,csv,for,us\n1,2,3,4\n", status: http.StatusOK, wantErr: "unexpected export format"},
		{name: "export already running", body: "a review export is already running", status: http.StatusTooManyRequests, wantErr: "429"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, resourcesPath+"/eval/review-queues/col1/scores.csv", r.URL.Path)
				w.Header().Set("Content-Type", "text/csv; charset=utf-8")
				if tt.truncated != "" {
					w.Header().Set("X-Review-Export-Truncated", tt.truncated)
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))

			rows, truncated, err := client.ExportScores(context.Background(), "col1")
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantRows, rows)
			assert.Equal(t, tt.wantTruncated, truncated)
		})
	}
}
