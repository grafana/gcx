//nolint:testpackage // white-box tests require access to unexported client internals
package irm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	fail "github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/httputils"
)

// newTestOpModelClient serves handler over httptest and returns a client
// pointed at it, using the same transport production uses.
func newTestOpModelClient(t *testing.T, handler http.HandlerFunc) *irmResourceClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return newIRMResourceClient(httputils.NewDefaultClient(context.Background()), srv.URL)
}

func TestOpModelClientBuildsAppPlatformPathAndBody(t *testing.T) {
	var gotPath, gotMethod, gotContentType string
	var gotBody map[string]any

	client := newTestOpModelClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod, gotContentType = r.URL.Path, r.Method, r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"observation_id":"obs-1","revision":"7"}`))
	})

	resp, err := client.CreateObservation(context.Background(), createObservationRequest{
		Container: "INC-1",
		Actor:     opModelActorInput{Kind: "agent", ID: "claude-code", DisplayName: "deric"},
		Text:      "p99 doubled",
		SourceRef: "grafana: d/x",
	})
	if err != nil {
		t.Fatalf("CreateObservation: %v", err)
	}
	if resp.ObservationID != "obs-1" || resp.Revision != "7" {
		t.Errorf("response not decoded: %+v", resp)
	}
	if want := "/opmodel/apis/irm.grafana.app/v1alpha1/containers/INC-1/observations"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if gotMethod != http.MethodPost || gotContentType != "application/json" {
		t.Errorf("method/content-type = %q/%q", gotMethod, gotContentType)
	}
	// The container is a path segment, so it must not also ride in the body.
	if _, present := gotBody["container"]; present {
		t.Errorf("container must not be sent in the body: %v", gotBody)
	}
	actor, ok := gotBody["actor"].(map[string]any)
	if !ok || actor["kind"] != "agent" || actor["id"] != "claude-code" || actor["display_name"] != "deric" {
		t.Errorf("actor body wrong: %v", gotBody["actor"])
	}
	if gotBody["text"] != "p99 doubled" || gotBody["source_ref"] != "grafana: d/x" {
		t.Errorf("body fields wrong: %v", gotBody)
	}
}

func TestOpModelClientEscapesPathSegments(t *testing.T) {
	var gotRawPath string
	client := newTestOpModelClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotRawPath = r.URL.EscapedPath()
		_, _ = w.Write([]byte(`{"revision":"1"}`))
	})

	if _, err := client.AssessClaim(context.Background(), assessClaimRequest{
		Container:        "INC/1",
		ClaimID:          "clm 7",
		Actor:            opModelActorInput{Kind: "human", ID: "deric"},
		ImpactAssessment: "confirmed",
	}); err != nil {
		t.Fatalf("AssessClaim: %v", err)
	}
	// Caller-owned ids must not be able to climb the route.
	for _, want := range []string{"INC%2F1", "clm%207"} {
		if !strings.Contains(gotRawPath, want) {
			t.Errorf("path %q missing escaped segment %q", gotRawPath, want)
		}
	}
}

func TestOpModelClientSendsListEventsQuery(t *testing.T) {
	var gotQuery string
	client := newTestOpModelClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"events":[],"revision":"9"}`))
	})

	if _, err := client.ListEvents(context.Background(), listEventsRequest{
		Container: "INC-1", SinceRevision: "4", PageSize: 50,
	}); err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if !strings.Contains(gotQuery, "since_revision=4") || !strings.Contains(gotQuery, "page_size=50") {
		t.Errorf("query = %q", gotQuery)
	}

	// An unset cursor and page size must be omitted so the server applies its
	// own defaults.
	client2 := newTestOpModelClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"events":[],"revision":"9"}`))
	})
	if _, err := client2.ListEvents(context.Background(), listEventsRequest{Container: "INC-1"}); err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if gotQuery != "" {
		t.Errorf("unset paging must send no query, got %q", gotQuery)
	}
}

func TestOpModelClientStatusToExitCode(t *testing.T) {
	tests := []struct {
		status   int
		wantExit int
	}{
		{http.StatusUnprocessableEntity, fail.ExitUsageError},
		{http.StatusBadRequest, fail.ExitUsageError},
		{http.StatusNotFound, fail.ExitGeneralError},
		{http.StatusConflict, fail.ExitGeneralError},
		{http.StatusUnauthorized, fail.ExitAuthFailure},
		{http.StatusForbidden, fail.ExitAuthFailure},
		{http.StatusInternalServerError, fail.ExitGeneralError},
	}
	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			client := newTestOpModelClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(`{"kind":"Status","apiVersion":"v1","status":"Failure","message":"scripted failure"}`))
			})
			_, err := client.CreateObservation(context.Background(), createObservationRequest{Container: "c"})
			if err == nil {
				t.Fatal("expected error")
			}
			assertDetailedExit(t, err, tt.wantExit)
		})
	}
}

func TestOpModelClientSurfacesStatusFieldViolations(t *testing.T) {
	client := newTestOpModelClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Invalid","code":422,"message":"validation failed",
			"details":{"causes":[{"reason":"FieldValueInvalid","field":"body.actor.kind","message":"must be human or agent"}]}}`))
	})

	_, err := client.CreateObservation(context.Background(), createObservationRequest{Container: "c"})
	if err == nil {
		t.Fatal("expected error")
	}
	var derr *fail.DetailedError
	if !errors.As(err, &derr) {
		t.Fatalf("expected DetailedError, got %T", err)
	}
	// The field the server named must reach the user; otherwise a 422 is
	// unactionable.
	if derr.Details != "validation failed; body.actor.kind must be human or agent" {
		t.Errorf("status detail not surfaced: %q", derr.Details)
	}
}

func TestOpModelStatusErrorFallbacks(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
	}{
		{"message", `{"kind":"Status","status":"Failure","message":"only a person can assess a claim","reason":"Conflict","code":409}`, "only a person can assess a claim"},
		{"causes only", `{"details":{"causes":[{"field":"body.text","message":"must not be blank"},{"message":"invalid input"},{"field":"body.actor"}]}}`, "body.text must not be blank; invalid input"},
		{"plugin error", `{"message":"permission denied"}`, "permission denied"},
		{"plain text", "  gateway unavailable\n", "gateway unavailable"},
		{"malformed JSON", `{"message":`, `{"message":`},
		{"unrecognized JSON", `{"error":"upstream failed"}`, `{"error":"upstream failed"}`},
		{"empty body", "", "Conflict"},
		{"bounded body", strings.Repeat("x", 9000), strings.Repeat("x", 8192)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := &http.Response{StatusCode: http.StatusConflict, Body: io.NopCloser(strings.NewReader(tc.body))}
			defer resp.Body.Close()
			if got := decodeOpModelProblem(resp); got != tc.want {
				t.Errorf("detail = %q; want %q", got, tc.want)
			}
		})
	}
}

func TestOpModelClientUnreachableEndpointFailsFast(t *testing.T) {
	// Port 0 on the loopback address refuses immediately: the agent
	// conformance suite runs every leaf with no backend, so this path must
	// produce one actionable error rather than hanging.
	client := newIRMResourceClient(httputils.NewDefaultClient(context.Background()), "http://127.0.0.1:0")

	_, err := client.CreateObservation(context.Background(), createObservationRequest{Container: "c"})
	if err == nil {
		t.Fatal("expected error")
	}
	assertDetailedExit(t, err, fail.ExitGeneralError)
	var derr *fail.DetailedError
	_ = errors.As(err, &derr)
	if !strings.Contains(derr.Summary, "unreachable") {
		t.Errorf("summary should name the unreachable endpoint: %q", derr.Summary)
	}
	if len(derr.Suggestions) == 0 || !strings.Contains(strings.Join(derr.Suggestions, " "), "gcx config check") {
		t.Errorf("suggestions should point at authenticated context diagnostics: %v", derr.Suggestions)
	}
}

func TestOpModelClientTimeoutIsPerRequest(t *testing.T) {
	// --timeout is set as the client timeout, so a slow response must surface
	// as the timeout error rather than the generic unreachable one. `shares
	// publish` depends on this being per-request: one budget for its whole
	// sequence would fail a legal package partway.
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
			_, _ = w.Write([]byte(`{"revision":"1"}`))
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(slow.Close)

	client := newIRMResourceClient(
		httputils.NewClient(httputils.ClientOpts{Timeout: 50 * time.Millisecond}), slow.URL)

	_, err := client.GetContainer(context.Background(), getContainerRequest{Container: "c"})
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	var derr *fail.DetailedError
	if !errors.As(err, &derr) {
		t.Fatalf("expected DetailedError, got %T", err)
	}
	if !strings.Contains(derr.Summary, "timed out") {
		t.Errorf("summary should report a timeout, got %q", derr.Summary)
	}
	if len(derr.Suggestions) == 0 || !strings.Contains(strings.Join(derr.Suggestions, " "), "--timeout") {
		t.Errorf("suggestions should point at --timeout: %v", derr.Suggestions)
	}
}
