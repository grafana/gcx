//nolint:testpackage // exercises private message command and wire types
package irm

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/spf13/cobra"
)

func TestMessageClientPostsIndependentMessage(t *testing.T) {
	client := newTestOpModelClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/messages" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["incident_id"] != "INC-1" || body["body"] != "finding" || body["see"] != "dashboard:db" {
			t.Errorf("body=%v", body)
		}
		agent, _ := body["drafted_by"].(map[string]any)
		if agent["id"] != "codex" || agent["display_name"] != "Codex" {
			t.Errorf("agent=%v", agent)
		}
		for _, key := range []string{"citations", "container", "revision", "actor"} {
			if _, ok := body[key]; ok {
				t.Errorf("unexpected %s", key)
			}
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"message_id":"incident:saved"}`))
	})
	resp, err := client.CreateMessage(context.Background(), createMessageRequest{IncidentID: "INC-1", Body: "finding", See: "dashboard:db", DraftedBy: &messageAgent{ID: "codex", DisplayName: "Codex"}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.MessageID != "incident:saved" {
		t.Fatalf("response=%+v", resp)
	}
}

func TestIncidentShareCommand(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		agent   string
		invalid bool
	}{
		{name: "human", args: []string{"INC-1", "--body", "finding", "--see", "dashboard:db"}},
		{name: "agent", args: []string{"INC-1", "--body", "finding", "--see", "dashboard:db", "--drafted-by-agent", "codex", "--drafted-by-agent-name", "Codex"}, agent: "Codex"},
		{name: "agent ID fallback", args: []string{"INC-1", "--body", "finding", "--see", "dashboard:db", "--drafted-by-agent", "codex"}, agent: "codex"},
		{name: "blank incident", args: []string{" ", "--body", "finding"}, invalid: true},
		{name: "blank body", args: []string{"INC-1", "--body", " "}, invalid: true},
		{name: "missing body", args: []string{"INC-1"}, invalid: true},
		{name: "agent name only", args: []string{"INC-1", "--body", "finding", "--drafted-by-agent-name", "Codex"}, invalid: true},
		{name: "invalid timeout", args: []string{"INC-1", "--body", "finding", "--timeout", "0s"}, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setAgentMode(t, true)
			api := &fakeOpModel{}
			loader := &fakeOpModelLoader{op: api}
			stdout, _, err := runOpModelTree(t, func() *cobra.Command { return newIncidentShareCmd(loader) }, tc.args...)
			if tc.invalid {
				requireUsageExit(t, err)
				if loader.loads != 0 || api.calls != 0 {
					t.Fatal("invalid input reached client")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if loader.loads != 1 || api.calls != 1 {
				t.Fatalf("loads=%d calls=%d", loader.loads, api.calls)
			}
			req := reqAs[createMessageRequest](t, api.reqs, 0)
			if req.IncidentID != "INC-1" || req.Body != "finding" || req.See != "dashboard:db" {
				t.Errorf("request=%+v", req)
			}
			if tc.agent == "" {
				if req.DraftedBy != nil {
					t.Errorf("unexpected agent=%+v", req.DraftedBy)
				}
			} else if req.DraftedBy == nil || req.DraftedBy.ID != "codex" || req.DraftedBy.DisplayName != tc.agent {
				t.Errorf("agent=%+v", req.DraftedBy)
			}
			doc := decodeSingleJSONValue(t, stdout)
			if doc["type"] != "gcx.irm.message_receipt" || doc["message_id"] != "upd-1" || doc["incident_id"] != "INC-1" {
				t.Errorf("receipt=%v", doc)
			}
			if _, exists := doc["revision"]; exists {
				t.Error("message has model revision")
			}
		})
	}
}
