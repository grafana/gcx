package sandbox

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequiredAccess(t *testing.T) {
	for _, tc := range []struct {
		access  Access
		method  string
		path    string
		allowed bool
	}{
		{AccessRead, http.MethodGet, "/api/dashboards/uid/x", true},
		{AccessRead, http.MethodHead, "/api/health", true},
		{AccessRead, http.MethodPost, "/api/ds/query", true},
		{AccessRead, http.MethodPost, "/apis/query.grafana.app/v0alpha1/namespaces/stacks-1/query", true},
		{AccessRead, http.MethodPost, "/apis/historian.alerting.grafana.app/v0alpha1/namespaces/default/notification/query", true},
		{AccessRead, http.MethodPost, "/api/datasources/proxy/uid/pyro/querier.v1.QuerierService/SelectMergeStacktraces", true},
		{AccessRead, http.MethodPost, "/api/plugin-proxy/grafana-collector-app/fleet-management-api/pipeline.v1.PipelineService/ListPipelines", true},
		{AccessRead, http.MethodPost, "/api/plugin-proxy/grafana-collector-app/fleet-management-api/pipeline.v1.PipelineService/DeletePipeline", false},
		{AccessRead, http.MethodPost, "/api/plugins/grafana-irm-app/resources/api/v1/IncidentsService.QueryIncidentPreviews", true},
		{AccessRead, http.MethodPost, "/api/plugins/grafana-irm-app/resources/api/v1/IncidentsService.CreateIncident", false},
		{AccessRead, http.MethodPost, "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/search", true},
		{AccessRead, http.MethodPost, "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/config/alert", false},
		{AccessRead, http.MethodPost, "/api/datasources/uid/ath/resources/tables", true},
		{AccessRead, http.MethodPost, "/api/datasources/proxy/uid/t/api/v2/traces/diff", true},
		{AccessRead, http.MethodPost, "/api/dashboards/db/../../ds/query", false},
		{AccessRead, http.MethodPost, "/api/dashboards/db", false},
		{AccessRead, http.MethodPut, "/api/folders/x", false},
		{AccessRead, http.MethodDelete, "/api/folders/x", false},
		{AccessWrite, http.MethodPost, "/api/dashboards/db", true},
		{AccessWrite, http.MethodPatch, "/api/folders/x", true},
		{AccessWrite, http.MethodDelete, "/api/folders/x", false},
		{AccessDelete, http.MethodDelete, "/api/folders/x", true},
		{AccessDelete, "PROPFIND", "/x", true},
		{AccessWrite, "PROPFIND", "/x", false},
	} {
		if allowed := RequiredAccess(tc.method, tc.path) <= tc.access; allowed != tc.allowed {
			t.Errorf("%s %s %s: allowed=%v, want %v", tc.access, tc.method, tc.path, allowed, tc.allowed)
		}
	}
}

func TestRequireAccess(t *testing.T) {
	var hits int
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
	defer srv.Close()
	egress := []Destination{{Host: strings.TrimPrefix(srv.URL, "https://")}}
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+"/api/x", strings.NewReader("{}"))
	var raw bytes.Buffer
	if err := req.WriteProxy(&raw); err != nil {
		t.Fatal(err)
	}

	s := newSession(egress, RequireAccess(AccessRead), srv.Client().Transport)
	want := "gcx_http: request refused: access denied: POST /api/x needs read-write access, this invocation is read-only"
	if _, errMsg := s.roundTrip(context.Background(), raw.Bytes()); errMsg != want {
		t.Errorf("read-only: got %q, want %q", errMsg, want)
	}
	if hits != 0 {
		t.Errorf("refused request reached the server %d times", hits)
	}

	s = newSession(egress, RequireAccess(AccessWrite), srv.Client().Transport)
	if _, errMsg := s.roundTrip(context.Background(), raw.Bytes()); errMsg != "" {
		t.Errorf("read-write: %s", errMsg)
	}
	if hits != 1 {
		t.Errorf("allowed request reached the server %d times, want 1", hits)
	}
}
