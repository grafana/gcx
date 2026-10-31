package collections_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/agento11y/eval/collections"
	"github.com/stretchr/testify/require"
)

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
