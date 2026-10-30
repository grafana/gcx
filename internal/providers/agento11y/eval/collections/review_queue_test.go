package collections_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/grafana/gcx/internal/providers/agento11y/eval/collections"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateReviewQueue(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/plugins/grafana-agento11y-app/resources/eval/review-queues", r.URL.Path)
		var request collections.ReviewQueueRequest
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&request)) {
			return
		}
		assert.Equal(t, "collection", request.CollectionID)
		assert.Equal(t, []string{"human-rubric"}, request.RubricEvaluatorIDs)
		writeJSON(w, map[string]any{"collection_id": "collection", "progress": map[string]any{"reviewed": 0}})
	}))
	result, err := client.CreateReviewQueue(context.Background(), &collections.ReviewQueueRequest{CollectionID: "collection", RubricEvaluatorIDs: []string{"human-rubric"}})
	require.NoError(t, err)
	assert.Equal(t, "collection", result["collection_id"])
	assert.Contains(t, result, "progress")
}

func TestReviewQueueRequiresRubric(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing rubric", args: []string{"create-review-queue", "collection"}, want: "at least one --rubric"},
		{name: "blank rubric", args: []string{"create-review-queue", "collection", "--rubric", " "}, want: "--rubric cannot be blank"},
		{name: "blank collection", args: []string{"create-review-queue", " ", "--rubric", "r"}, want: "collection ID is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := collections.Commands(nil)
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			cmd.SetArgs(tt.args)
			require.ErrorContains(t, cmd.Execute(), tt.want)
		})
	}
}
