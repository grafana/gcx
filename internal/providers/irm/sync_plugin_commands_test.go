//nolint:testpackage // white-box tests require access to unexported IRM command builders
package irm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// fakePluginAPI stubs the plugin sync surface. Unimplemented OnCallAPI
// methods panic via the embedded nil interface.
type fakePluginAPI struct {
	OnCallAPI

	calls  int
	result *PluginSyncResult
	err    error
}

func (f *fakePluginAPI) SyncPlugin(context.Context) (*PluginSyncResult, error) {
	f.calls++
	return f.result, f.err
}

func runSyncPluginCmd(t *testing.T, fake *fakePluginAPI, args ...string) (string, error) {
	t.Helper()
	cmd := newSyncPluginCommand(&fakeLoader{client: fake})
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return out.String(), err
}

func TestSyncPluginCommandText(t *testing.T) {
	resetAgentMode(t)

	tests := []struct {
		name   string
		result *PluginSyncResult
		want   string
	}{
		{
			name:   "the backend sends a message",
			result: &PluginSyncResult{Message: "Sync request processed successfully"},
			want:   "Requested a sync of the IRM plugin: Sync request processed successfully",
		},
		{
			name:   "the backend sends no message",
			result: &PluginSyncResult{},
			want:   "Requested a sync of the IRM plugin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakePluginAPI{result: tt.result}
			out, err := runSyncPluginCmd(t, fake)
			if err != nil {
				t.Fatal(err)
			}
			if fake.calls != 1 {
				t.Errorf("expected one sync call, got %d", fake.calls)
			}
			if !strings.Contains(out, tt.want) {
				t.Errorf("expected %q, got %q", tt.want, out)
			}
		})
	}
}

func TestSyncPluginCommandStructuredResult(t *testing.T) {
	resetAgentMode(t)

	fake := &fakePluginAPI{result: &PluginSyncResult{Message: "Sync request processed successfully"}}
	out, err := runSyncPluginCmd(t, fake, "-o", "json")
	if err != nil {
		t.Fatal(err)
	}

	var got struct {
		Type          string `json:"type"`
		SchemaVersion string `json:"schema_version"`
		Action        string `json:"action"`
		Message       string `json:"message"`
		Target        struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"target"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if got.Type != "gcx.mutation" || got.SchemaVersion != "1" {
		t.Errorf("missing mutation discriminators: %+v", got)
	}
	if got.Action != "sync-requested" || got.Target.Kind != "Plugin" || got.Target.Name != "grafana-irm-app" {
		t.Errorf("unexpected mutation document: %+v", got)
	}
	if got.Message != "Sync request processed successfully" {
		t.Errorf("expected the backend message, got %q", got.Message)
	}
}

// TestSyncPluginCommandOmitsEmptyMessage proves that the result document drops
// the message field when the backend sends none.
func TestSyncPluginCommandOmitsEmptyMessage(t *testing.T) {
	resetAgentMode(t)

	fake := &fakePluginAPI{result: &PluginSyncResult{}}
	out, err := runSyncPluginCmd(t, fake, "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "message") {
		t.Errorf("expected no message field, got %q", out)
	}
}

func TestSyncPluginCommandSurfacesBackendError(t *testing.T) {
	resetAgentMode(t)

	fake := &fakePluginAPI{err: errors.New("irm: sync plugin: permission denied")}
	_, err := runSyncPluginCmd(t, fake)
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("expected the backend error, got %v", err)
	}
}
