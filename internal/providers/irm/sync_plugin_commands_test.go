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

	calls int
	err   error
}

func (f *fakePluginAPI) SyncPlugin(context.Context) error {
	f.calls++
	return f.err
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

// TestSyncPluginCommandText proves that the human line reports the request.
func TestSyncPluginCommandText(t *testing.T) {
	resetAgentMode(t)

	fake := &fakePluginAPI{}
	out, err := runSyncPluginCmd(t, fake)
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls != 1 {
		t.Errorf("expected one sync call, got %d", fake.calls)
	}
	want := "Requested a sync of the IRM plugin"
	if !strings.Contains(out, want) {
		t.Errorf("expected %q, got %q", want, out)
	}
}

// TestSyncPluginCommandStructuredResult proves that the structured result is a
// plain single-mutation document, without a key outside that shared family.
func TestSyncPluginCommandStructuredResult(t *testing.T) {
	resetAgentMode(t)

	fake := &fakePluginAPI{}
	out, err := runSyncPluginCmd(t, fake, "-o", "json")
	if err != nil {
		t.Fatal(err)
	}

	var got struct {
		Type          string `json:"type"`
		SchemaVersion string `json:"schema_version"`
		Action        string `json:"action"`
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

	var keys map[string]any
	if err := json.Unmarshal([]byte(out), &keys); err != nil {
		t.Fatalf("output is not a JSON object: %v\n%s", err, out)
	}
	if _, ok := keys["message"]; ok {
		t.Errorf("expected no message key, got %q", out)
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
