package snapshot_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/gcxerrors"
)

// grafanaErrorPage mimics the start of the HTML page Grafana serves when a
// render fails.
const grafanaErrorPage = "<!DOCTYPE html>\n<html lang=\"en\">\n  <head>\n    <meta charset=\"utf-8\" />\n    <title>Grafana</title>\n  </head>\n  <body>Internal Server Error</body>\n</html>"

// newFailingRenderServer serves every render with the given status, content
// type and body, and /api/frontend/settings with settingsStatus and
// settingsBody. It counts settings requests.
func newFailingRenderServer(t *testing.T, renderStatus int, contentType, renderBody string, settingsStatus int, settingsBody string) (*httptest.Server, *int) {
	t.Helper()
	probes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/render/"):
			w.Header().Set("Content-Type", contentType)
			w.WriteHeader(renderStatus)
			_, _ = io.WriteString(w, renderBody)
		case r.URL.Path == "/api/frontend/settings":
			probes++
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(settingsStatus)
			_, _ = io.WriteString(w, settingsBody)
		default:
			t.Errorf("unexpected request path %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, &probes
}

func TestSnapshot_RenderFailureDiagnostics(t *testing.T) {
	const htmlType = "text/html; charset=UTF-8"

	tests := []struct {
		name           string
		args           []string
		renderStatus   int
		contentType    string
		renderBody     string
		settingsStatus int
		settingsBody   string
		wantSummary    string
		wantContains   []string
		wantMissing    bool // details name the missing renderer
		wantProbes     int
	}{
		{
			name:           "no renderer",
			args:           []string{"dash-a"},
			renderStatus:   http.StatusInternalServerError,
			contentType:    htmlType,
			renderBody:     grafanaErrorPage,
			settingsStatus: http.StatusOK,
			settingsBody:   `{"rendererAvailable":false}`,
			wantSummary:    gcxerrors.SummaryEndpointNotAvailable,
			wantContains:   []string{`"dash-a"`, "HTTP 500 Internal Server Error"},
			wantMissing:    true,
			wantProbes:     1,
		},
		{
			name:           "no renderer names every failed dashboard",
			args:           []string{"dash-a", "dash-b"},
			renderStatus:   http.StatusInternalServerError,
			contentType:    htmlType,
			renderBody:     grafanaErrorPage,
			settingsStatus: http.StatusOK,
			settingsBody:   `{"rendererAvailable":false}`,
			wantSummary:    gcxerrors.SummaryEndpointNotAvailable,
			wantContains:   []string{`"dash-a", "dash-b"`},
			wantMissing:    true,
			wantProbes:     1,
		},
		{
			name:           "no renderer keeps failures with other causes",
			args:           []string{"dash-a", "bad/name"},
			renderStatus:   http.StatusInternalServerError,
			contentType:    htmlType,
			renderBody:     grafanaErrorPage,
			settingsStatus: http.StatusOK,
			settingsBody:   `{"rendererAvailable":false}`,
			wantSummary:    gcxerrors.SummaryEndpointNotAvailable,
			wantContains: []string{
				`Rendering "dash-a" failed with HTTP 500`,
				"Other failures:",
				`dashboard name "bad/name" contains invalid path characters`,
			},
			wantMissing: true,
			wantProbes:  1,
		},
		{
			// Joined errors after the first are not cleaned by the
			// converters, so the render error itself must carry no markup.
			name:           "renderer present with two dashboards stays readable",
			args:           []string{"dash-a", "dash-b"},
			renderStatus:   http.StatusInternalServerError,
			contentType:    htmlType,
			renderBody:     grafanaErrorPage,
			settingsStatus: http.StatusOK,
			settingsBody:   `{"rendererAvailable":true}`,
			wantSummary:    gcxerrors.SummaryAPIError,
			wantContains:   []string{`"dash-a"`, `"dash-b"`, "HTML error page"},
			wantProbes:     1,
		},
		{
			name:           "renderer present keeps a generic, readable error",
			args:           []string{"dash-a"},
			renderStatus:   http.StatusInternalServerError,
			contentType:    htmlType,
			renderBody:     grafanaErrorPage,
			settingsStatus: http.StatusOK,
			settingsBody:   `{"rendererAvailable":true}`,
			wantSummary:    gcxerrors.SummaryAPIError,
			wantContains:   []string{`"dash-a"`, "got an HTML page (HTTP 500 Internal Server Error)"},
			wantProbes:     1,
		},
		{
			name:           "probe forbidden falls back to the generic error",
			args:           []string{"dash-a"},
			renderStatus:   http.StatusInternalServerError,
			contentType:    htmlType,
			renderBody:     grafanaErrorPage,
			settingsStatus: http.StatusForbidden,
			settingsBody:   `{"message":"forbidden"}`,
			wantSummary:    gcxerrors.SummaryAPIError,
			wantContains:   []string{"got an HTML page (HTTP 500 Internal Server Error)"},
			wantProbes:     1,
		},
		{
			name:           "JSON error body passes through",
			args:           []string{"dash-a"},
			renderStatus:   http.StatusInternalServerError,
			contentType:    "application/json",
			renderBody:     `{"message":"Rendering failed: timeout"}`,
			settingsStatus: http.StatusOK,
			settingsBody:   `{"rendererAvailable":true}`,
			wantSummary:    gcxerrors.SummaryAPIError,
			wantContains:   []string{`{"message":"Rendering failed: timeout"}`},
			wantProbes:     1,
		},
		{
			name:           "plain text body is bounded",
			args:           []string{"dash-a"},
			renderStatus:   http.StatusInternalServerError,
			contentType:    "text/plain",
			renderBody:     "renderer crashed " + strings.Repeat("x", 2000),
			settingsStatus: http.StatusOK,
			settingsBody:   `{"rendererAvailable":true}`,
			wantSummary:    gcxerrors.SummaryAPIError,
			wantContains:   []string{"renderer crashed", "…"},
			wantProbes:     1,
		},
		{
			name:           "client errors skip the probe",
			args:           []string{"dash-a"},
			renderStatus:   http.StatusNotFound,
			contentType:    "application/json",
			renderBody:     `{"message":"Dashboard not found"}`,
			settingsStatus: http.StatusOK,
			settingsBody:   `{"rendererAvailable":false}`,
			wantSummary:    gcxerrors.SummaryResourceNotFound,
			wantContains:   []string{"Dashboard not found"},
			wantProbes:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, probes := newFailingRenderServer(t, tt.renderStatus, tt.contentType, tt.renderBody, tt.settingsStatus, tt.settingsBody)

			_, _, err := runSnapshotCmd(t, server, append(tt.args, "--output-dir", t.TempDir()))
			if err == nil {
				t.Fatal("Execute() = nil, want a render error")
			}

			detailed := fail.ErrorToDetailedError(err)
			if detailed == nil {
				t.Fatalf("ErrorToDetailedError(%v) = nil", err)
			}
			if detailed.Summary != tt.wantSummary {
				t.Errorf("Summary = %q, want %q (details: %s)", detailed.Summary, tt.wantSummary, detailed.Details)
			}
			if detailed.ExitCode != nil && *detailed.ExitCode != gcxerrors.ExitGeneralError {
				t.Errorf("ExitCode = %d, want %d", *detailed.ExitCode, gcxerrors.ExitGeneralError)
			}
			if *probes != tt.wantProbes {
				t.Errorf("renderer probes = %d, want %d", *probes, tt.wantProbes)
			}

			rendered := detailed.Error()
			for _, want := range tt.wantContains {
				if !strings.Contains(rendered, want) {
					t.Errorf("rendered error missing %q:\n%s", want, rendered)
				}
			}
			if strings.Contains(rendered, "<!DOCTYPE") || strings.Contains(rendered, "<html") {
				t.Errorf("rendered error contains raw HTML:\n%s", rendered)
			}
			if len(rendered) > 2000 {
				t.Errorf("rendered error is %d bytes, want it bounded:\n%s", len(rendered), rendered)
			}

			claimsMissing := strings.Contains(detailed.Details, "no image renderer")
			if claimsMissing != tt.wantMissing {
				t.Errorf("details name a missing renderer = %v, want %v:\n%s", claimsMissing, tt.wantMissing, detailed.Details)
			}
			if tt.wantMissing {
				suggestions := strings.Join(detailed.Suggestions, "\n")
				for _, want := range []string{"server_url", "callback_url", "gcx api /api/plugins?type=renderer"} {
					if !strings.Contains(suggestions, want) {
						t.Errorf("suggestions missing %q: %q", want, detailed.Suggestions)
					}
				}
				if detailed.DocsLink == "" {
					t.Error("DocsLink is empty, want the image rendering docs")
				}
			}
		})
	}
}

func TestSnapshot_NoRendererKeepsOtherHTTPFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/render/d/dash-a/"):
			w.Header().Set("Content-Type", "text/html; charset=UTF-8")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, grafanaErrorPage)
		case strings.HasPrefix(r.URL.Path, "/render/d/dash-gone/"):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"message":"Dashboard not found"}`)
		case r.URL.Path == "/api/frontend/settings":
			_, _ = io.WriteString(w, `{"rendererAvailable":false}`)
		default:
			t.Errorf("unexpected request path %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	_, _, err := runSnapshotCmd(t, server, []string{"dash-a", "dash-gone", "--output-dir", t.TempDir()})
	if err == nil {
		t.Fatal("Execute() = nil, want a render error")
	}

	detailed := fail.ErrorToDetailedError(err)
	if detailed == nil {
		t.Fatalf("ErrorToDetailedError(%v) = nil", err)
	}
	if detailed.Summary != gcxerrors.SummaryEndpointNotAvailable {
		t.Errorf("Summary = %q, want %q", detailed.Summary, gcxerrors.SummaryEndpointNotAvailable)
	}
	for _, want := range []string{
		`Rendering "dash-a" failed with HTTP 500`,
		"Other failures:",
		`failed to render "dash-gone": render request failed with status 404: {"message":"Dashboard not found"}`,
	} {
		if !strings.Contains(detailed.Details, want) {
			t.Errorf("details missing %q:\n%s", want, detailed.Details)
		}
	}
	if strings.Contains(detailed.Details, `Rendering "dash-a", "dash-gone"`) {
		t.Errorf("details attribute the 404 to the missing renderer:\n%s", detailed.Details)
	}
}
