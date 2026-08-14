package irm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/irm"
)

// severityServer records every incident call and answers CreateIncident with
// the default severity, the way the live backend does: CreateIncident ignores
// the severity of the request body.
type severityServer struct {
	calls      []string
	bodies     []map[string]any
	severities []map[string]any
	// severityAfterUpdate is the label the server reports once UpdateSeverity
	// has run.
	severityAfterUpdate string
	title               string
	// notFoundOnVersioned makes the versioned base path answer 404, so the
	// test can drive the unversioned fallback.
	notFoundOnVersioned bool
}

func (s *severityServer) handler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		versioned := strings.Contains(r.URL.Path, "/api/v1/")

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		s.calls = append(s.calls, method)
		s.bodies = append(s.bodies, body)

		if s.notFoundOnVersioned && versioned &&
			(method == "IncidentsService.UpdateSeverity" || method == "IncidentsService.UpdateTitle") {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		switch method {
		case "SeveritiesService.GetOrgSeverities":
			json.NewEncoder(w).Encode(map[string]any{"severities": s.severities}) //nolint:errcheck
		case "IncidentsService.CreateIncident":
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"incident": map[string]any{"incidentID": "1", "title": s.title, "severity": "Pending"},
			})
		case "IncidentsService.UpdateSeverity":
			s.severityAfterUpdate, _ = body["severity"].(string)
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"incident": map[string]any{"incidentID": "1", "title": s.title, "severity": s.severityAfterUpdate},
			})
		case "IncidentsService.UpdateTitle":
			s.title, _ = body["title"].(string)
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"incident": map[string]any{"incidentID": "1", "title": s.title, "severity": s.severityAfterUpdate},
			})
		case "IncidentsService.UpdateStatus":
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"incident": map[string]any{
					"incidentID": "1", "title": s.title, "severity": s.severityAfterUpdate,
					"status": body["status"],
				},
			})
		default:
			t.Errorf("unexpected call to %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func newSeverityTestClient(t *testing.T, s *severityServer) *irm.IncidentClient {
	t.Helper()
	srv := httptest.NewServer(s.handler(t))
	t.Cleanup(srv.Close)
	return newTestClient(t, srv)
}

// TestCreateAppliesSeverity is the regression test for the reported defect:
// every incident was created at the default severity, whichever severity the
// manifest asked for.
func TestCreateAppliesSeverity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		incident     irm.Incident
		wantSeverity string
		wantCalls    []string
	}{
		{
			name:         "severity label on the spec",
			incident:     irm.Incident{Title: "probe", Severity: "Critical"},
			wantSeverity: "Critical",
			wantCalls:    []string{"IncidentsService.CreateIncident", "IncidentsService.UpdateSeverity"},
		},
		{
			name:         "severityID resolved through the organization severities",
			incident:     irm.Incident{Title: "probe", SeverityID: "sev-2"},
			wantSeverity: "Major",
			wantCalls: []string{
				"IncidentsService.CreateIncident",
				"SeveritiesService.GetOrgSeverities",
				"IncidentsService.UpdateSeverity",
			},
		},
		{
			name:         "no severity asked for leaves the default alone",
			incident:     irm.Incident{Title: "probe"},
			wantSeverity: "",
			wantCalls:    []string{"IncidentsService.CreateIncident"},
		},
		{
			name:         "the default severity needs no second call",
			incident:     irm.Incident{Title: "probe", Severity: "Pending"},
			wantSeverity: "",
			wantCalls:    []string{"IncidentsService.CreateIncident"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := &severityServer{
				title: "probe",
				severities: []map[string]any{
					{"severityID": "sev-1", "displayLabel": "Critical", "level": 1},
					{"severityID": "sev-2", "displayLabel": "Major", "level": 2},
				},
			}
			client := newSeverityTestClient(t, srv)

			inc := tt.incident
			got, err := client.Create(context.Background(), &inc)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(srv.calls) != len(tt.wantCalls) {
				t.Fatalf("got calls %v, want %v", srv.calls, tt.wantCalls)
			}
			for i, want := range tt.wantCalls {
				if srv.calls[i] != want {
					t.Errorf("call %d: got %q, want %q", i, srv.calls[i], want)
				}
			}
			if srv.severityAfterUpdate != tt.wantSeverity {
				t.Errorf("got severity %q on the server, want %q", srv.severityAfterUpdate, tt.wantSeverity)
			}
			if tt.wantSeverity != "" && got.Severity != tt.wantSeverity {
				t.Errorf("got severity %q in the result, want %q", got.Severity, tt.wantSeverity)
			}
		})
	}
}

func TestCreateRejectsUnknownSeverityID(t *testing.T) {
	t.Parallel()

	srv := &severityServer{
		title:      "probe",
		severities: []map[string]any{{"severityID": "sev-1", "displayLabel": "Critical", "level": 1}},
	}
	client := newSeverityTestClient(t, srv)

	inc := irm.Incident{Title: "probe", SeverityID: "does-not-exist"}
	_, err := client.Create(context.Background(), &inc)
	if err == nil || !strings.Contains(err.Error(), "unknown severityID") {
		t.Errorf("expected an unknown-severityID error, got %v", err)
	}
}

func TestUpdateSeverityAndTitle(t *testing.T) {
	t.Parallel()

	srv := &severityServer{title: "old title"}
	client := newSeverityTestClient(t, srv)

	if _, err := client.UpdateTitle(context.Background(), "1", "new title"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := client.UpdateSeverity(context.Background(), "1", "Critical")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.Title != "new title" || got.Severity != "Critical" {
		t.Errorf("unexpected incident: %+v", got)
	}
	if srv.bodies[0]["incidentID"] != "1" || srv.bodies[0]["title"] != "new title" {
		t.Errorf("unexpected UpdateTitle body: %v", srv.bodies[0])
	}
	if srv.bodies[1]["incidentID"] != "1" || srv.bodies[1]["severity"] != "Critical" {
		t.Errorf("unexpected UpdateSeverity body: %v", srv.bodies[1])
	}
}

// TestUpdateFallsBackToUnversionedPath covers a Grafana build that predates
// the versioned base path of IncidentsService.
func TestUpdateFallsBackToUnversionedPath(t *testing.T) {
	t.Parallel()

	srv := &severityServer{title: "probe", notFoundOnVersioned: true}
	client := newSeverityTestClient(t, srv)

	got, err := client.UpdateSeverity(context.Background(), "1", "Critical")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Severity != "Critical" {
		t.Errorf("got severity %q, want Critical", got.Severity)
	}
	// One call on the versioned path, one on the unversioned path.
	if len(srv.calls) != 2 {
		t.Errorf("expected a retry on the unversioned path, got calls %v", srv.calls)
	}
}

// TestUpdateAppliesEveryChangedField covers the push path: the adapter calls
// Update, which must carry the title and the severity, not the status alone.
func TestUpdateAppliesEveryChangedField(t *testing.T) {
	t.Parallel()

	srv := &severityServer{title: "old title"}
	client := newSeverityTestClient(t, srv)

	got, err := client.Update(context.Background(), "1", &irm.Incident{
		Status:   "active",
		Title:    "new title",
		Severity: "Critical",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{
		"IncidentsService.UpdateStatus",
		"IncidentsService.UpdateTitle",
		"IncidentsService.UpdateSeverity",
	}
	if len(srv.calls) != len(want) {
		t.Fatalf("got calls %v, want %v", srv.calls, want)
	}
	for i, w := range want {
		if srv.calls[i] != w {
			t.Errorf("call %d: got %q, want %q", i, srv.calls[i], w)
		}
	}
	if got.Title != "new title" || got.Severity != "Critical" {
		t.Errorf("unexpected incident: %+v", got)
	}
}

// TestUpdateSkipsUnchangedFields keeps the push path cheap: a manifest that
// matches the server costs one call, not three.
func TestUpdateSkipsUnchangedFields(t *testing.T) {
	t.Parallel()

	srv := &severityServer{title: "same title", severityAfterUpdate: "Critical"}
	client := newSeverityTestClient(t, srv)

	if _, err := client.Update(context.Background(), "1", &irm.Incident{
		Status:   "active",
		Title:    "same title",
		Severity: "Critical",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(srv.calls) != 1 || srv.calls[0] != "IncidentsService.UpdateStatus" {
		t.Errorf("expected only a status call, got %v", srv.calls)
	}
}
