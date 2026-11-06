//nolint:testpackage // exercises private message command and wire types
package irm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
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
		{name: "blank see", args: []string{"INC-1", "--body", "finding", "--see", " "}, invalid: true},
		{name: "see prefix", args: []string{"INC-1", "--body", "finding", "--see", "See: dashboard:db"}, invalid: true},
		{name: "indented lowercase see prefix", args: []string{"INC-1", "--body", "finding", "--see", "  see:dashboard:db"}, invalid: true},
		{name: "see prefix on dry run", args: []string{"INC-1", "--body", "finding", "--see", "SEE: dashboard:db", "--dry-run"}, invalid: true},
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

func TestIncidentShareRejectsCredentialsBeforeLoadingClient(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		authErr := errors.New("sharing requires an OAuth user login")
		loader := &fakeOpModelLoader{op: &fakeOpModel{}, sharingAuthErr: authErr}
		cmd, stdout, _ := newOpModelTestCmd()
		err := runIncidentShare(cmd, "INC-1", &messageShareOpts{Body: "finding", DryRun: dryRun}, loader, testSharedOpts())
		if !errors.Is(err, authErr) || loader.loads != 0 || stdout.Len() != 0 {
			t.Fatalf("dry-run=%v: expected rejection before client loading, got err=%v loads=%d stdout=%s", dryRun, err, loader.loads, stdout)
		}
	}
}

func TestIncidentShareDryRunPreviewsWithoutSending(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want map[string]any
	}{
		{
			name: "with see",
			args: []string{"INC-1", "--body", "Database connections are exhausted", "--see", "dashboard:db", "--dry-run"},
			want: map[string]any{"type": "gcx.irm.message_preview", "schema_version": "1", "incident_id": "INC-1", "body": "Database connections are exhausted", "see": "dashboard:db", "preview": "Database connections are exhausted\nSee: dashboard:db"},
		},
		{
			name: "without see",
			args: []string{"INC-1", "--body", "The service has recovered", "--dry-run"},
			want: map[string]any{"type": "gcx.irm.message_preview", "schema_version": "1", "incident_id": "INC-1", "body": "The service has recovered", "preview": "The service has recovered"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setAgentMode(t, true)
			api := &fakeOpModel{}
			loader := &fakeOpModelLoader{op: api}
			stdout, _, err := runOpModelTree(t, func() *cobra.Command { return newIncidentShareCmd(loader) }, tc.args...)
			if err != nil {
				t.Fatal(err)
			}
			if api.calls != 0 || loader.loads != 0 {
				t.Fatalf("dry run reached the message API: loads=%d calls=%d", loader.loads, api.calls)
			}
			if got := decodeSingleJSONValue(t, stdout); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("preview = %v; want %v", got, tc.want)
			}
		})
	}
}
