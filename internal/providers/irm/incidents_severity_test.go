package irm_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/irm"
	"github.com/spf13/cobra"
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
	status              string
	// notFoundOnVersioned makes the versioned base path answer 404, so the
	// test can drive the unversioned fallback.
	notFoundOnVersioned bool
	// failSeverityUpdate makes UpdateSeverity answer 500, so the test can
	// drive the failure path of Create.
	failSeverityUpdate bool
	// failTitleUpdate makes UpdateTitle answer 500, so the test can drive a
	// partial update.
	failTitleUpdate bool
	// incidentMissing makes every update method answer 404 on both base paths,
	// the way the API reports an unknown incidentID.
	incidentMissing bool
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

		if s.notFoundOnVersioned && versioned && strings.HasPrefix(method, "IncidentsService.Update") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if s.incidentMissing && strings.HasPrefix(method, "IncidentsService.Update") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if s.failSeverityUpdate && method == "IncidentsService.UpdateSeverity" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if s.failTitleUpdate && method == "IncidentsService.UpdateTitle" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		switch method {
		case "SeveritiesService.GetOrgSeverities":
			json.NewEncoder(w).Encode(map[string]any{"severities": s.severities}) //nolint:errcheck
		case "IncidentsService.GetIncident":
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"incident": map[string]any{
					"incidentID": "1", "title": s.title,
					"severity": s.severityAfterUpdate, "status": s.status,
				},
			})
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
			s.status, _ = body["status"].(string)
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"incident": map[string]any{
					"incidentID": "1", "title": s.title, "severity": s.severityAfterUpdate,
					"status": s.status,
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
			// The label resolves before the create call, so a bad severityID
			// leaves no incident behind.
			wantCalls: []string{
				"SeveritiesService.GetOrgSeverities",
				"IncidentsService.CreateIncident",
				"IncidentsService.UpdateSeverity",
			},
		},
		{
			// A hand-written manifest can carry both fields. severityID has
			// precedence there. A pulled manifest carries the label alone.
			name:         "severityID beats a severity label in the same manifest",
			incident:     irm.Incident{Title: "probe", Severity: "Pending", SeverityID: "sev-1"},
			wantSeverity: "Critical",
			wantCalls: []string{
				"SeveritiesService.GetOrgSeverities",
				"IncidentsService.CreateIncident",
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

// TestCreateRejectsUnknownSeverityID proves that a bad severityID leaves no
// incident behind. The IRM API has no delete method, so gcx must resolve the
// label before it creates the incident.
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
	for _, call := range srv.calls {
		if call == "IncidentsService.CreateIncident" {
			t.Fatalf("the client created an incident it cannot delete: %v", srv.calls)
		}
	}
}

// TestCreateReportsTheIncidentAfterAFailedSeverityUpdate covers the second
// failure path: the incident exists, and the caller needs its identifier to
// repair the severity.
func TestCreateReportsTheIncidentAfterAFailedSeverityUpdate(t *testing.T) {
	t.Parallel()

	srv := &severityServer{title: "probe", failSeverityUpdate: true}
	client := newSeverityTestClient(t, srv)

	inc := irm.Incident{Title: "probe", Severity: "Critical"}
	created, err := client.Create(context.Background(), &inc)
	if err == nil {
		t.Fatal("expected a failed-severity error")
	}
	if created == nil || created.IncidentID != "1" {
		t.Fatalf("expected the created incident next to the error, got %+v", created)
	}
	for _, want := range []string{"incident 1 exists", `gcx irm incidents update 1 --severity "Critical"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected %q in the error, got %v", want, err)
		}
	}
}

const createSeverityManifest = `apiVersion: incident.ext.grafana.app/v1alpha1
kind: Incident
metadata:
  name: my-incident
spec:
  title: "probe"
  status: active
  severity: Critical
`

// TestCreateCommandKeepsTheRepairErrorAlone covers the second failure path of
// the create command. The incident exists, so the command must not report a
// failed create next to the repair command that the client error carries.
func TestCreateCommandKeepsTheRepairErrorAlone(t *testing.T) {
	srv := &severityServer{title: "probe", failSeverityUpdate: true}
	server := httptest.NewServer(srv.handler(t))
	t.Cleanup(server.Close)

	_, _, err := runIncidentCmd(t, func() *cobra.Command {
		return irm.NewCreateCommand(incidentLoader(server))
	}, createSeverityManifest, "-f", "-")
	if err == nil {
		t.Fatal("expected the failed-severity error")
	}
	if strings.Contains(err.Error(), "failed to create incident") {
		t.Errorf("the incident exists, so the error must not report a failed create: %v", err)
	}
	if !strings.Contains(err.Error(), "incident 1 exists") {
		t.Errorf("expected the repair error of the client, got %v", err)
	}
}

// TestUpdateFieldReportsAMissingIncident covers an unknown incidentID. Both
// base paths answer 404, so a missing route is not the cause. The caller must
// read the identifier and the not-found classification that Get produces, not
// a bare status line.
func TestUpdateFieldReportsAMissingIncident(t *testing.T) {
	t.Parallel()

	srv := &severityServer{title: "probe", incidentMissing: true}
	client := newSeverityTestClient(t, srv)

	_, err := client.UpdateSeverity(context.Background(), "does-not-exist", "Critical")
	if !errors.Is(err, irm.ErrNotFound) {
		t.Fatalf("expected a not-found error, got %v", err)
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Errorf("expected the incident identifier in the error, got %v", err)
	}
	// One call on the versioned path, one on the unversioned path.
	if len(srv.calls) != 2 {
		t.Errorf("expected two calls, got %v", srv.calls)
	}
}

// TestUpdateReportsAHalfAppliedChange covers a failure after an earlier field
// reached the server. The incident stands between two states, so gcx returns
// no incident, and the error names the field that gcx applied.
func TestUpdateReportsAHalfAppliedChange(t *testing.T) {
	t.Parallel()

	srv := &severityServer{title: "old title", status: "active", failTitleUpdate: true}
	client := newSeverityTestClient(t, srv)

	got, _, err := client.Update(context.Background(), "1", &irm.Incident{
		Status:   "resolved",
		Title:    "new title",
		Severity: "Critical",
	})
	if err == nil {
		t.Fatal("expected an error from the failed title update")
	}
	if got != nil {
		t.Fatalf("expected no incident next to the error, got %+v", got)
	}
	for _, want := range []string{"update 1", "gcx applied the status", "title update failed"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected %q in the error, got %v", want, err)
		}
	}
	// The severity call must not run after the title failed.
	for _, call := range srv.calls {
		if call == "IncidentsService.UpdateSeverity" {
			t.Fatalf("the client continued after a failed update: %v", srv.calls)
		}
	}
}

// TestUpdateRejectsAnUnknownSeverityIDBeforeItWrites covers a manifest with an
// unknown spec.severityID. The resolve reads only, so it runs before the first
// write: the IRM API cannot undo a write that a later step abandons. The error
// must not report a failed severity update either, because gcx tried none.
func TestUpdateRejectsAnUnknownSeverityIDBeforeItWrites(t *testing.T) {
	t.Parallel()

	srv := &severityServer{
		title:      "old title",
		status:     "active",
		severities: []map[string]any{{"severityID": "sev-1", "displayLabel": "Critical", "level": 1}},
	}
	client := newSeverityTestClient(t, srv)

	got, _, err := client.Update(context.Background(), "1", &irm.Incident{
		Status:     "resolved",
		Title:      "new title",
		SeverityID: "does-not-exist",
	})
	if err == nil || !strings.Contains(err.Error(), "unknown severityID") {
		t.Fatalf("expected an unknown-severityID error, got %v", err)
	}
	if got != nil {
		t.Errorf("expected no incident, got %+v", got)
	}
	if strings.Contains(err.Error(), "update failed") {
		t.Errorf("gcx tried no update, so the error must not report one: %v", err)
	}
	for _, call := range srv.calls {
		if strings.HasPrefix(call, "IncidentsService.Update") {
			t.Fatalf("the client wrote before it resolved the severity: %v", srv.calls)
		}
	}
}

// TestUpdateReportsTheFirstFailureAlone covers a failure before any field
// reached the server: the error stands on its own, and there is no incident
// to report.
func TestUpdateReportsTheFirstFailureAlone(t *testing.T) {
	t.Parallel()

	srv := &severityServer{title: "old title", status: "active", failTitleUpdate: true}
	client := newSeverityTestClient(t, srv)

	got, _, err := client.Update(context.Background(), "1", &irm.Incident{Title: "new title"})
	if err == nil {
		t.Fatal("expected an error from the failed title update")
	}
	if got != nil {
		t.Errorf("expected no incident, got %+v", got)
	}
	if strings.Contains(err.Error(), "gcx applied") {
		t.Errorf("expected no applied field in the error, got %v", err)
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

	got, changed, err := client.Update(context.Background(), "1", &irm.Incident{
		Status:   "active",
		Title:    "new title",
		Severity: "Critical",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Join(changed, ",") != "status,title,severity" {
		t.Errorf("got changed fields %v, want status, title and severity", changed)
	}

	want := []string{
		"IncidentsService.GetIncident",
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
// matches the server costs the read alone, and `gcx resources push` writes
// nothing on a second run. The status compares like the severity, so a
// difference in letter case causes no write either.
func TestUpdateSkipsUnchangedFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status string
	}{
		{name: "the same status", status: "active"},
		{name: "the same status in other letter case", status: "ACTIVE"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := &severityServer{
				title:               "same title",
				status:              "active",
				severityAfterUpdate: "Critical",
			}
			client := newSeverityTestClient(t, srv)

			_, changed, err := client.Update(context.Background(), "1", &irm.Incident{
				Status:   tt.status,
				Title:    "same title",
				Severity: "Critical",
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(changed) != 0 {
				t.Errorf("got changed fields %v, want none", changed)
			}

			want := []string{"IncidentsService.GetIncident"}
			if len(srv.calls) != len(want) || srv.calls[0] != want[0] {
				t.Errorf("got calls %v, want %v", srv.calls, want)
			}
		})
	}
}

// TestUpdateSkipsAnEmptyStatus covers a manifest without a status: nothing
// enforces the required fields of the schema on push, and an empty status
// must not block the title and the severity.
func TestUpdateSkipsAnEmptyStatus(t *testing.T) {
	t.Parallel()

	srv := &severityServer{title: "old title", status: "active"}
	client := newSeverityTestClient(t, srv)

	if _, _, err := client.Update(context.Background(), "1", &irm.Incident{
		Title:    "new title",
		Severity: "Critical",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, call := range srv.calls {
		if call == "IncidentsService.UpdateStatus" {
			t.Fatalf("the client sent an empty status: %v", srv.calls)
		}
	}
	want := []string{
		"IncidentsService.GetIncident",
		"IncidentsService.UpdateTitle",
		"IncidentsService.UpdateSeverity",
	}
	if len(srv.calls) != len(want) {
		t.Fatalf("got calls %v, want %v", srv.calls, want)
	}
}
