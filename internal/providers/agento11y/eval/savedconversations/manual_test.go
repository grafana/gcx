package savedconversations_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/agento11y/eval/savedconversations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateManual(t *testing.T) {
	generation := json.RawMessage(`{"generation_id":"generation","mode":"SYNC","model":{"provider":"synthetic","name":"test"},"input":[{"role":"user","content":"hello"}],"output":[{"role":"assistant","content":"hi"}]}`)
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/plugins/grafana-agento11y-app/resources/eval/saved-conversations:manual", r.URL.Path)
		var request savedconversations.ManualSaveRequest
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&request)) {
			return
		}
		assert.Equal(t, "manual", request.SavedID)
		if assert.Len(t, request.Generations, 1) {
			assert.JSONEq(t, string(generation), string(request.Generations[0]))
		}
		writeJSON(w, map[string]any{"saved_id": "manual", "source": "manual"})
	}))
	result, err := client.CreateManual(context.Background(), &savedconversations.ManualSaveRequest{SavedID: "manual", Name: "Fixture", Generations: []json.RawMessage{generation}})
	require.NoError(t, err)
	require.Equal(t, "manual", result.Source)
}

func TestManualInvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		input string
		want  string
	}{
		{name: "malformed JSON", args: []string{"create-manual", "-f", "-"}, input: "{", want: "invalid manual conversation JSON"},
		{name: "missing fields", args: []string{"create-manual", "-f", "-"}, input: "{}", want: "saved_id, name and nonempty generations"},
		{name: "empty generations", args: []string{"create-manual", "-f", "-"}, input: `{"saved_id":"manual","name":"Fixture","generations":[]}`, want: "nonempty generations"},
		{name: "missing filename", args: []string{"create-manual"}, want: "--filename is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := savedconversations.Commands(nil)
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			cmd.SetArgs(tt.args)
			cmd.SetIn(strings.NewReader(tt.input))
			require.ErrorContains(t, cmd.Execute(), tt.want)
		})
	}
}
