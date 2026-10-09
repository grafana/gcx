package irm_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/irm"
)

func TestGetIntegrationTemplates(t *testing.T) {
	t.Parallel()

	var gotPath string
	client := newTestOnCallClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"web_title_template":        "{{ payload.title }}",
			"phone_call_title_template": "Kritieke melding",
			"a_field_gcx_does_not_know": "kept",
		})
	}))

	got, err := client.GetIntegrationTemplates(context.Background(), "CH1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantPath := irm.BasePath + "/alert_receive_channel_templates/CH1/"
	if gotPath != wantPath {
		t.Errorf("got path %q, want %q", gotPath, wantPath)
	}
	if got["phone_call_title_template"] != "Kritieke melding" {
		t.Errorf("unexpected phone call template: %v", got["phone_call_title_template"])
	}
	// An unknown field must survive, so that a get/edit/update round trip never
	// drops a template this build does not model.
	if got["a_field_gcx_does_not_know"] != "kept" {
		t.Errorf("unknown field was dropped: %v", got)
	}
}

func TestGetIntegrationTemplatesNotFound(t *testing.T) {
	t.Parallel()

	client := newTestOnCallClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	_, err := client.GetIntegrationTemplates(context.Background(), "missing")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected a not-found error, got %v", err)
	}
}

func TestUpdateIntegrationTemplates(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	var gotBody map[string]any
	client := newTestOnCallClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&gotBody) //nolint:errcheck
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(gotBody) //nolint:errcheck
	}))

	sent := map[string]any{
		"phone_call_title_template": "Kritieke melding",
		"unknown_future_template":   "kept",
	}
	got, err := client.UpdateIntegrationTemplates(context.Background(), "CH1", sent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotMethod != http.MethodPut {
		t.Errorf("got method %q, want PUT", gotMethod)
	}
	wantPath := irm.BasePath + "/alert_receive_channel_templates/CH1/"
	if gotPath != wantPath {
		t.Errorf("got path %q, want %q", gotPath, wantPath)
	}
	for key, want := range sent {
		if gotBody[key] != want {
			t.Errorf("field %q did not reach the backend: got %v, want %v", key, gotBody[key], want)
		}
		if got[key] != want {
			t.Errorf("field %q missing from the result: got %v, want %v", key, got[key], want)
		}
	}
}

func TestStartIntegrationMaintenance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mode     irm.MaintenanceMode
		duration int
	}{
		{name: "debug mode", mode: irm.MaintenanceModeDebug, duration: 3600},
		{name: "maintenance mode", mode: irm.MaintenanceModeMaintenance, duration: 10800},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotMethod, gotPath string
			var gotBody map[string]int
			client := newTestOnCallClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				gotPath = r.URL.Path
				json.NewDecoder(r.Body).Decode(&gotBody) //nolint:errcheck
				w.WriteHeader(http.StatusOK)
				io.WriteString(w, "{}") //nolint:errcheck
			}))

			if err := client.StartIntegrationMaintenance(context.Background(), "CH1", tt.mode, tt.duration); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if gotMethod != http.MethodPost {
				t.Errorf("got method %q, want POST", gotMethod)
			}
			wantPath := irm.BasePath + "/alert_receive_channels/CH1/start_maintenance/"
			if gotPath != wantPath {
				t.Errorf("got path %q, want %q", gotPath, wantPath)
			}
			if gotBody["mode"] != int(tt.mode) || gotBody["duration"] != tt.duration {
				t.Errorf("unexpected body %v", gotBody)
			}
		})
	}
}

func TestStopIntegrationMaintenance(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	client := newTestOnCallClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "{}") //nolint:errcheck
	}))

	if err := client.StopIntegrationMaintenance(context.Background(), "CH1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("got method %q, want POST", gotMethod)
	}
	wantPath := irm.BasePath + "/alert_receive_channels/CH1/stop_maintenance/"
	if gotPath != wantPath {
		t.Errorf("got path %q, want %q", gotPath, wantPath)
	}
}

// An action endpoint can answer with any status in the success range. The
// client must accept all of them, and must report every other status.
func TestIntegrationActionStatusCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{name: "ok", status: http.StatusOK, body: "{}"},
		{name: "accepted", status: http.StatusAccepted, body: "{}"},
		{name: "no content", status: http.StatusNoContent},
		{
			name:    "not found",
			status:  http.StatusNotFound,
			wantErr: `integration "CH1" not found`,
		},
		{
			name:    "bad request",
			status:  http.StatusBadRequest,
			body:    `{"duration":["Invalid duration"]}`,
			wantErr: "Invalid duration",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := newTestOnCallClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				io.WriteString(w, tt.body) //nolint:errcheck
			}))

			err := client.StartIntegrationMaintenance(context.Background(), "CH1", irm.MaintenanceModeMaintenance, 3600)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("got error %v, want one containing %q", err, tt.wantErr)
			}
		})
	}
}
