package dashboards_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/dashboards"
	"github.com/grafana/gcx/internal/gcxerrors"
	"k8s.io/client-go/rest"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// newTestClient creates a renderer Client pointing at the given test server.
func newTestClient(t *testing.T, server *httptest.Server) *dashboards.Client {
	t.Helper()
	cfg := config.NamespacedRESTConfig{
		Config: rest.Config{
			Host: server.URL,
		},
		Namespace: "default",
	}
	client, err := dashboards.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

func TestRender_URLConstruction(t *testing.T) {
	pngBytes := []byte("\x89PNG\r\n\x1a\n")

	tests := []struct {
		name       string
		req        dashboards.RenderRequest
		wantPath   string
		wantParams url.Values
	}{
		{
			name: "full dashboard - default org, width, height, kiosk params",
			req: dashboards.RenderRequest{
				UID:    "abc",
				Width:  1920,
				Height: -1,
			},
			wantPath: "/render/d/abc/",
			wantParams: url.Values{
				"orgId":         {"1"},
				"width":         {"1920"},
				"height":        {"-1"},
				"kiosk":         {"true"},
				"hideNav":       {"true"},
				"fullPageImage": {"true"},
			},
		},
		{
			name: "single panel - uses d-solo path with panelId",
			req: dashboards.RenderRequest{
				UID:     "abc",
				PanelID: 42,
				Width:   800,
				Height:  600,
			},
			wantPath: "/render/d-solo/abc/",
			wantParams: url.Values{
				"orgId":         {"1"},
				"panelId":       {"42"},
				"width":         {"800"},
				"height":        {"600"},
				"kiosk":         {"true"},
				"hideNav":       {"true"},
				"fullPageImage": {"true"},
			},
		},
		{
			name: "optional relative time range params are preserved",
			req: dashboards.RenderRequest{
				UID:    "abc",
				Width:  1920,
				Height: 1080,
				From:   "now-1h",
				To:     "now",
				Tz:     "UTC",
				Theme:  "light",
			},
			wantPath: "/render/d/abc/",
			wantParams: url.Values{
				"orgId":         {"1"},
				"width":         {"1920"},
				"height":        {"1080"},
				"from":          {"now-1h"},
				"to":            {"now"},
				"tz":            {"UTC"},
				"theme":         {"light"},
				"kiosk":         {"true"},
				"hideNav":       {"true"},
				"fullPageImage": {"true"},
			},
		},
		{
			name: "template variable overrides",
			req: dashboards.RenderRequest{
				UID:    "abc",
				Width:  1920,
				Height: -1,
				Vars: map[string]string{
					"cluster":    "prod",
					"datasource": "prometheus",
				},
			},
			wantPath: "/render/d/abc/",
			wantParams: url.Values{
				"orgId":          {"1"},
				"width":          {"1920"},
				"height":         {"-1"},
				"kiosk":          {"true"},
				"hideNav":        {"true"},
				"fullPageImage":  {"true"},
				"var-cluster":    {"prod"},
				"var-datasource": {"prometheus"},
			},
		},
		{
			name: "custom orgId is sent",
			req: dashboards.RenderRequest{
				UID:    "abc",
				OrgID:  5,
				Width:  1920,
				Height: 1080,
			},
			wantPath: "/render/d/abc/",
			wantParams: url.Values{
				"orgId":         {"5"},
				"width":         {"1920"},
				"height":        {"1080"},
				"kiosk":         {"true"},
				"hideNav":       {"true"},
				"fullPageImage": {"true"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedReq *http.Request
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedReq = r
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(pngBytes)
			}))
			defer server.Close()

			client := newTestClient(t, server)
			got, err := client.Render(context.Background(), tt.req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) == 0 {
				t.Error("expected non-empty PNG bytes")
			}
			if capturedReq == nil {
				t.Fatal("no request captured")
			}
			if tt.wantPath != "" && capturedReq.URL.Path != tt.wantPath {
				t.Errorf("path = %q, want %q", capturedReq.URL.Path, tt.wantPath)
			}
			q := capturedReq.URL.Query()
			for key, wantVals := range tt.wantParams {
				gotVals, ok := q[key]
				if !ok {
					t.Errorf("query param %q missing", key)
					continue
				}
				if len(gotVals) != len(wantVals) || gotVals[0] != wantVals[0] {
					t.Errorf("query param %q = %v, want %v", key, gotVals, wantVals)
				}
			}
		})
	}
}

func TestRender_TimeRangeURLConstruction(t *testing.T) {
	pngBytes := []byte("\x89PNG\r\n\x1a\n")
	epochMillis := func(value string) string {
		t.Helper()
		tm, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			t.Fatalf("parse fixture time: %v", err)
		}
		return strconv.FormatInt(tm.UnixMilli(), 10)
	}

	tests := []struct {
		name     string
		from     string
		to       string
		wantFrom string
		wantTo   string
	}{
		{
			name:     "RFC3339 params are normalized to epoch milliseconds",
			from:     "2024-01-15T10:30:00Z",
			to:       "2024-01-15T11:30:00Z",
			wantFrom: epochMillis("2024-01-15T10:30:00Z"),
			wantTo:   epochMillis("2024-01-15T11:30:00Z"),
		},
		{
			name:     "Unix seconds params are normalized to epoch milliseconds",
			from:     "1705315800",
			to:       "1705319400",
			wantFrom: "1705315800000",
			wantTo:   "1705319400000",
		},
		{
			name:     "Unix milliseconds params are preserved",
			from:     "1705315800123",
			to:       "1705319400456",
			wantFrom: "1705315800123",
			wantTo:   "1705319400456",
		},
		{
			name:     "historical Unix milliseconds params are preserved",
			from:     "10000000000",
			to:       "10000060000",
			wantFrom: "10000000000",
			wantTo:   "10000060000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedReq *http.Request
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedReq = r
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(pngBytes)
			}))
			defer server.Close()

			client := newTestClient(t, server)
			_, err := client.Render(context.Background(), dashboards.RenderRequest{
				UID:    "abc",
				Width:  1920,
				Height: 1080,
				From:   tt.from,
				To:     tt.to,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if capturedReq == nil {
				t.Fatal("no request captured")
			}
			q := capturedReq.URL.Query()
			if got := q.Get("from"); got != tt.wantFrom {
				t.Errorf("from = %q, want %q", got, tt.wantFrom)
			}
			if got := q.Get("to"); got != tt.wantTo {
				t.Errorf("to = %q, want %q", got, tt.wantTo)
			}
		})
	}
}

func TestRender_InvalidTimeRange(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("\x89PNG\r\n\x1a\n"))
	}))
	defer server.Close()

	client := newTestClient(t, server)
	_, err := client.Render(context.Background(), dashboards.RenderRequest{
		UID:  "abc",
		From: "2024-01-15",
		To:   "now",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid from time") {
		t.Errorf("error %q does not contain invalid from time", err.Error())
	}
	if called {
		t.Error("server was called despite invalid time range")
	}
}

func TestRender_TimeoutOverridesHTTPClientPerRequest(t *testing.T) {
	const configuredTimeout = 2 * time.Second

	var remaining []time.Duration
	cfg := config.NamespacedRESTConfig{
		Config: rest.Config{
			Host:    "http://example.test",
			Timeout: configuredTimeout,
			WrapTransport: func(http.RoundTripper) http.RoundTripper {
				return roundTripperFunc(func(req *http.Request) (*http.Response, error) {
					deadline, ok := req.Context().Deadline()
					if !ok {
						t.Fatal("request context has no HTTP client deadline")
					}
					remaining = append(remaining, time.Until(deadline))
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader("\x89PNG\r\n\x1a\n")),
						Header:     make(http.Header),
						Request:    req,
					}, nil
				})
			},
		},
		Namespace: "default",
	}
	client, err := dashboards.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	if _, err := client.Render(context.Background(), dashboards.RenderRequest{UID: "with-timeout", Timeout: 3 * time.Minute}); err != nil {
		t.Fatalf("Render() with timeout = %v", err)
	}
	if _, err := client.Render(context.Background(), dashboards.RenderRequest{UID: "without-timeout"}); err != nil {
		t.Fatalf("Render() without timeout = %v", err)
	}
	deadlineCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if _, err := client.Render(deadlineCtx, dashboards.RenderRequest{UID: "earlier-deadline", Timeout: 3 * time.Minute}); err != nil {
		t.Fatalf("Render() with earlier parent deadline = %v", err)
	}

	if len(remaining) != 3 {
		t.Fatalf("captured deadlines = %d, want 3", len(remaining))
	}
	assertDurationNear(t, remaining[0], 3*time.Minute+30*time.Second)
	assertDurationNear(t, remaining[1], configuredTimeout)
	assertDurationNear(t, remaining[2], 500*time.Millisecond)
}

func TestRender_TimeoutPreservesParentCancellation(t *testing.T) {
	requestStarted := make(chan struct{})
	cfg := config.NamespacedRESTConfig{
		Config: rest.Config{
			Host: "http://example.test",
			WrapTransport: func(http.RoundTripper) http.RoundTripper {
				return roundTripperFunc(func(req *http.Request) (*http.Response, error) {
					close(requestStarted)
					<-req.Context().Done()
					return nil, req.Context().Err()
				})
			},
		},
		Namespace: "default",
	}
	client, err := dashboards.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := client.Render(ctx, dashboards.RenderRequest{UID: "abc", Timeout: 3 * time.Minute})
		result <- err
	}()

	select {
	case <-requestStarted:
		cancel()
	case <-time.After(time.Second):
		t.Fatal("render request did not start")
	}

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Render() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Render() did not stop after parent cancellation")
	}
}

func TestRender_TimeoutPreservesAuthenticatedTransport(t *testing.T) {
	const token = "renderer-token"
	var gotAuthorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("\x89PNG\r\n\x1a\n"))
	}))
	defer server.Close()

	client, err := dashboards.NewClient(config.NamespacedRESTConfig{
		Config: rest.Config{Host: server.URL, BearerToken: token},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := client.Render(context.Background(), dashboards.RenderRequest{UID: "abc", Timeout: time.Minute}); err != nil {
		t.Fatalf("Render() = %v", err)
	}

	if want := "Bearer " + token; gotAuthorization != want {
		t.Errorf("Authorization = %q, want %q", gotAuthorization, want)
	}
}

func assertDurationNear(t *testing.T, got, want time.Duration) {
	t.Helper()
	const tolerance = time.Second
	if got < want-tolerance || got > want+tolerance {
		t.Errorf("duration = %v, want %v (+/- %v)", got, want, tolerance)
	}
}

func TestRender_Errors(t *testing.T) {
	tests := []struct {
		name           string
		handler        func(w http.ResponseWriter, r *http.Request)
		wantErrContain string
	}{
		{
			name: "HTTP 500 returns error with status and body",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("internal server error"))
			},
			wantErrContain: "500",
		},
		{
			name: "HTTP 500 error message includes body excerpt",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("plugin not found"))
			},
			wantErrContain: "plugin not found",
		},
		{
			name: "HTTP 200 with empty body returns error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
			wantErrContain: "empty",
		},
	}

	req := dashboards.RenderRequest{UID: "abc", Width: 1920, Height: 1080}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(tt.handler))
			defer server.Close()

			client := newTestClient(t, server)
			_, err := client.Render(context.Background(), req)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErrContain) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantErrContain)
			}
		})
	}
}

func TestRender_ErrorCarriesHTTPResponse(t *testing.T) {
	longText := strings.Repeat("é", 400) // 800 bytes of two-byte runes

	tests := []struct {
		name              string
		contentType       string
		body              string
		wantServerMessage string // exact, unless empty
		wantMaxServerLen  int    // when > 0, ServerMessage is at most this long
	}{
		{
			name:        "HTML error page",
			contentType: "text/html; charset=UTF-8",
			body:        "<!DOCTYPE html>\n<html lang=\"en\"><head><title>Grafana</title></head><body>error</body></html>",
		},
		{
			name:              "JSON error body is kept unchanged",
			contentType:       "application/json",
			body:              `{"message":"Rendering failed: no renderer"}`,
			wantServerMessage: `{"message":"Rendering failed: no renderer"}`,
		},
		{
			name:             "long plain text is bounded on a rune boundary",
			contentType:      "text/plain",
			body:             longText,
			wantMaxServerLen: 256 + len("…"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tt.contentType)
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer server.Close()

			_, err := newTestClient(t, server).Render(context.Background(), dashboards.RenderRequest{UID: "abc"})

			statusErr, ok := errors.AsType[*gcxerrors.HTTPStatusError](err)
			if !ok {
				t.Fatalf("Render() error = %v (%T), want *gcxerrors.HTTPStatusError", err, err)
			}
			if statusErr.Status != http.StatusInternalServerError {
				t.Errorf("Status = %d, want 500", statusErr.Status)
			}
			if statusErr.ContentType != tt.contentType {
				t.Errorf("ContentType = %q, want %q", statusErr.ContentType, tt.contentType)
			}
			if !strings.HasPrefix(statusErr.Error(), "render request failed with status 500: ") {
				t.Errorf("Error() = %q, want the existing render failure prefix", statusErr.Error())
			}
			if strings.Contains(statusErr.Error(), "<") {
				t.Errorf("Error() = %q, want no markup", statusErr.Error())
			}
			if tt.wantServerMessage != "" && statusErr.ServerMessage != tt.wantServerMessage {
				t.Errorf("ServerMessage = %q, want %q", statusErr.ServerMessage, tt.wantServerMessage)
			}
			if tt.wantMaxServerLen > 0 {
				if len(statusErr.ServerMessage) > tt.wantMaxServerLen {
					t.Errorf("ServerMessage is %d bytes, want at most %d", len(statusErr.ServerMessage), tt.wantMaxServerLen)
				}
				if !utf8.ValidString(statusErr.ServerMessage) || !strings.HasSuffix(statusErr.ServerMessage, "…") {
					t.Errorf("ServerMessage = %q, want valid UTF-8 ending in an ellipsis", statusErr.ServerMessage)
				}
			}
		})
	}
}

func TestRendererAvailable(t *testing.T) {
	tests := []struct {
		name          string
		status        int
		body          string
		wantAvailable bool
		wantOK        bool
	}{
		{name: "no renderer", status: http.StatusOK, body: `{"rendererAvailable":false}`, wantOK: true},
		{name: "renderer present", status: http.StatusOK, body: `{"rendererAvailable":true}`, wantAvailable: true, wantOK: true},
		{name: "setting absent", status: http.StatusOK, body: `{"buildInfo":{}}`},
		{name: "forbidden", status: http.StatusForbidden, body: `{"message":"forbidden"}`},
		{name: "not JSON", status: http.StatusOK, body: "<html></html>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/frontend/settings" {
					t.Errorf("unexpected request path %q", r.URL.Path)
				}
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer server.Close()

			available, ok := newTestClient(t, server).RendererAvailable(context.Background())
			if available != tt.wantAvailable || ok != tt.wantOK {
				t.Errorf("RendererAvailable() = (%v, %v), want (%v, %v)", available, ok, tt.wantAvailable, tt.wantOK)
			}
		})
	}

	t.Run("cancelled context", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"rendererAvailable":false}`)
		}))
		defer server.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, ok := newTestClient(t, server).RendererAvailable(ctx); ok {
			t.Error("RendererAvailable() ok = true for a cancelled context, want false")
		}
	})
}
