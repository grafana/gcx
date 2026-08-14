//nolint:testpackage // white-box tests require access to unexported IRM command builders
package irm

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// fakeIntegrationAPI stubs the integration template and maintenance surface.
// Unimplemented OnCallAPI methods panic via the embedded nil interface.
type fakeIntegrationAPI struct {
	OnCallAPI

	templates    map[string]any
	gotID        string
	gotTemplates map[string]any
	gotMode      MaintenanceMode
	gotDuration  int
	stopped      []string
	err          error
}

func (f *fakeIntegrationAPI) GetIntegrationTemplates(_ context.Context, id string) (map[string]any, error) {
	f.gotID = id
	return f.templates, f.err
}

func (f *fakeIntegrationAPI) UpdateIntegrationTemplates(_ context.Context, id string, t map[string]any) (map[string]any, error) {
	f.gotID = id
	f.gotTemplates = t
	return t, f.err
}

func (f *fakeIntegrationAPI) StartIntegrationMaintenance(_ context.Context, id string, mode MaintenanceMode, duration int) error {
	f.gotID = id
	f.gotMode = mode
	f.gotDuration = duration
	return f.err
}

func (f *fakeIntegrationAPI) StopIntegrationMaintenance(_ context.Context, id string) error {
	f.stopped = append(f.stopped, id)
	return f.err
}

func runIntegrationsCmd(t *testing.T, fake *fakeIntegrationAPI, args ...string) (string, error) {
	t.Helper()
	cmd := newIntegrationsCmd(&fakeLoader{client: fake})
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return out.String(), err
}

func TestIntegrationGetTemplatesCommand(t *testing.T) {
	resetAgentMode(t)

	fake := &fakeIntegrationAPI{templates: map[string]any{
		"phone_call_title_template": "Kritieke melding",
		"web_title_template":        "{{ payload.title }}",
	}}

	out, err := runIntegrationsCmd(t, fake, "get-templates", "CH1", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	if fake.gotID != "CH1" {
		t.Errorf("expected templates of CH1, got %q", fake.gotID)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if got["phone_call_title_template"] != "Kritieke melding" {
		t.Errorf("unexpected output %v", got)
	}
}

func TestIntegrationUpdateTemplatesCommand(t *testing.T) {
	resetAgentMode(t)

	fake := &fakeIntegrationAPI{}
	manifest := writeManifest(t, `{"phone_call_title_template":"Kritieke melding","future_field":"kept"}`)

	out, err := runIntegrationsCmd(t, fake, "update-templates", "CH1", "-f", manifest, "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	if fake.gotID != "CH1" {
		t.Errorf("expected update of CH1, got %q", fake.gotID)
	}
	if fake.gotTemplates["phone_call_title_template"] != "Kritieke melding" {
		t.Errorf("template did not reach the client: %v", fake.gotTemplates)
	}
	// A field the Go code does not model must still travel to the backend.
	if fake.gotTemplates["future_field"] != "kept" {
		t.Errorf("unknown field was dropped before the request: %v", fake.gotTemplates)
	}
	if !strings.Contains(out, "Kritieke melding") {
		t.Errorf("expected the stored document on stdout, got %q", out)
	}
}

func TestIntegrationUpdateTemplatesRequiresFilename(t *testing.T) {
	resetAgentMode(t)

	_, err := runIntegrationsCmd(t, &fakeIntegrationAPI{}, "update-templates", "CH1")
	if err == nil || !strings.Contains(err.Error(), "--filename is required") {
		t.Errorf("expected missing-filename error, got %v", err)
	}
}

func TestIntegrationStartMaintenanceCommand(t *testing.T) {
	resetAgentMode(t)

	tests := []struct {
		name     string
		args     []string
		wantMode MaintenanceMode
		wantSecs int
	}{
		{
			name:     "defaults to maintenance mode for one hour",
			args:     []string{"start-maintenance", "CH1"},
			wantMode: MaintenanceModeMaintenance,
			wantSecs: 3600,
		},
		{
			name:     "explicit debug mode and duration",
			args:     []string{"start-maintenance", "CH1", "--mode", "debug", "--duration", "10800"},
			wantMode: MaintenanceModeDebug,
			wantSecs: 10800,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeIntegrationAPI{}
			out, err := runIntegrationsCmd(t, fake, tt.args...)
			if err != nil {
				t.Fatal(err)
			}
			if fake.gotID != "CH1" {
				t.Errorf("expected maintenance on CH1, got %q", fake.gotID)
			}
			if fake.gotMode != tt.wantMode {
				t.Errorf("got mode %d, want %d", fake.gotMode, tt.wantMode)
			}
			if fake.gotDuration != tt.wantSecs {
				t.Errorf("got duration %d, want %d", fake.gotDuration, tt.wantSecs)
			}
			if !strings.Contains(out, "Started maintenance on integration CH1") {
				t.Errorf("expected the success line, got %q", out)
			}
		})
	}
}

func TestIntegrationStartMaintenanceRejectsBadFlags(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "unknown mode",
			args:    []string{"start-maintenance", "CH1", "--mode", "sleep"},
			wantErr: `unknown --mode "sleep"`,
		},
		{
			name:    "zero duration",
			args:    []string{"start-maintenance", "CH1", "--duration", "0"},
			wantErr: "--duration must be a positive number of seconds",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetAgentMode(t)

			fake := &fakeIntegrationAPI{}
			_, err := runIntegrationsCmd(t, fake, tt.args...)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("got error %v, want one containing %q", err, tt.wantErr)
			}
			if fake.gotID != "" {
				t.Error("the command called the backend despite an invalid flag")
			}
		})
	}
}

func TestIntegrationStopMaintenanceCommand(t *testing.T) {
	resetAgentMode(t)

	fake := &fakeIntegrationAPI{}
	out, err := runIntegrationsCmd(t, fake, "stop-maintenance", "CH1")
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.stopped) != 1 || fake.stopped[0] != "CH1" {
		t.Errorf("expected stop-maintenance on CH1, got %v", fake.stopped)
	}
	if !strings.Contains(out, "Stopped maintenance on integration CH1") {
		t.Errorf("expected the success line, got %q", out)
	}
}

func TestIntegrationMaintenanceStructuredResult(t *testing.T) {
	resetAgentMode(t)

	fake := &fakeIntegrationAPI{}
	out, err := runIntegrationsCmd(t, fake, "start-maintenance", "CH1", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}

	var got struct {
		Type          string `json:"type"`
		SchemaVersion string `json:"schema_version"`
		Action        string `json:"action"`
		Target        struct {
			Kind string `json:"kind"`
			ID   string `json:"id"`
		} `json:"target"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if got.Type != "gcx.mutation" || got.SchemaVersion != "1" {
		t.Errorf("missing mutation discriminators: %+v", got)
	}
	if got.Action != "maintenance-started" || got.Target.ID != "CH1" || got.Target.Kind != "Integration" {
		t.Errorf("unexpected mutation document: %+v", got)
	}
}
