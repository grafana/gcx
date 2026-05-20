package dashboards_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/dashboards"
	"k8s.io/client-go/rest"
)

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
