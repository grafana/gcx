package search_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/grafana/gcx/internal/config"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/yaml"
)

const hybridResponse = `{
  "apiVersion":"search.grafana.app/v0alpha1",
  "kind":"HybridSearchResults",
  "items":[
    {"resource":{"group":"dashboard.grafana.app","resource":"dashboards","kind":"Dashboard","name":"dash-z"},"title":"CPU Overview","folder":"","score":0,"chunks":[{"subresource":"panel/5","content":"CPU saturation"},{"subresource":"panel/8","content":"CPU usage"}]},
    {"resource":{"group":"dashboard.grafana.app","resource":"dashboards","kind":"Dashboard","name":"dash-a"},"title":"Host Metrics","folder":"folder-a","score":0.9}
  ]
}`

func TestHybridSearch_RequestAndOutput(t *testing.T) {
	srv, loader := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if want := "/grafana/apis/dashboard.grafana.app/v0alpha1/namespaces/stacks-test/dashboards/search/hybrid"; r.URL.Path != want {
			t.Errorf("path = %q, want %q", r.URL.Path, want)
		}
		if r.URL.RawQuery != "" {
			t.Errorf("unexpected URL query: %s", r.URL.RawQuery)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		want := map[string]any{
			"apiVersion": "search.grafana.app/v0alpha1",
			"kind":       "HybridSearchQuery",
			"query":      "CPU saturation",
			"limit":      float64(7),
			"filters": []any{map[string]any{
				"field": "folder", "values": []any{"folder-a", "", "folder-b"},
			}},
		}
		if !reflect.DeepEqual(body, want) {
			t.Errorf("request body = %#v, want %#v", body, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(hybridResponse))
	})
	defer srv.Close()
	loader.cfg.Host += "/grafana/"
	loader.cfg.BearerToken = "test-token"

	output, err := runSearchCommand(t, loader, "CPU saturation", "--hybrid", "--limit", "7",
		"--folder", "folder-a", "--folder=", "--folder", "folder-b", "-o", "json")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	var got, want map[string]any
	if err := json.Unmarshal([]byte(output), &got); err != nil {
		t.Fatalf("decode output: %v\n%s", err, output)
	}
	if err := json.Unmarshal([]byte(`{
	  "apiVersion":"dashboard.grafana.app/v0alpha1","kind":"DashboardSearchResultList","limit":7,
	  "items":[
	    {"apiVersion":"dashboard.grafana.app/v0alpha1","kind":"DashboardHit","metadata":{"name":"dash-z"},"spec":{"title":"CPU Overview","folder":"","tags":null,"score":0,"chunks":[{"subresource":"panel/5","content":"CPU saturation"},{"subresource":"panel/8","content":"CPU usage"}]}},
	    {"apiVersion":"dashboard.grafana.app/v0alpha1","kind":"DashboardHit","metadata":{"name":"dash-a"},"spec":{"title":"Host Metrics","folder":"folder-a","tags":null,"score":0.9}}
	  ]
	}`), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("output changed result order or metadata:\ngot: %s\nwant: %#v", output, want)
	}
}

type failLoader struct {
	called bool
}

func (l *failLoader) LoadGrafanaConfig(context.Context) (config.NamespacedRESTConfig, error) {
	l.called = true
	return config.NamespacedRESTConfig{}, errors.New("unexpected config load")
}

func TestHybridSearch_ValidationBeforeConfig(t *testing.T) {
	tooManyFolders := make([]string, 1, 2003)
	tooManyFolders[0] = "query"
	for range 1001 {
		tooManyFolders = append(tooManyFolders, "--folder", "folder-a")
	}
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"missing query", nil, "query"},
		{"empty query with folder", []string{"", "--folder", "folder-a"}, "query"},
		{"whitespace query", []string{" \t\n "}, "query"},
		{"query byte limit", []string{strings.Repeat("é", 501)}, "1000"},
		{"tag", []string{"query", "--tag", "prod"}, "--tag"},
		{"empty tag", []string{"query", "--tag="}, "--tag"},
		{"sort", []string{"query", "--sort", "name_sort"}, "--sort"},
		{"empty sort", []string{"query", "--sort="}, "--sort"},
		{"deleted", []string{"query", "--deleted"}, "--deleted"},
		{"explicit false deleted", []string{"query", "--deleted=false"}, "--deleted"},
		{"zero limit", []string{"query", "--limit", "0"}, "--limit"},
		{"negative limit", []string{"query", "--limit", "-1"}, "--limit"},
		{"excessive limit", []string{"query", "--limit", "201"}, "--limit"},
		{"wildcard folder", []string{"query", "--folder", "*"}, "--folder"},
		{"too many folders", tooManyFolders, "1000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader := &failLoader{}
			_, err := runSearchCommand(t, loader, append([]string{"--hybrid"}, tt.args...)...)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want message containing %q", err, tt.want)
			}
			if loader.called {
				t.Error("invalid input loaded configuration")
			}
		})
	}
}

func TestHybridSearch_InputBoundaries(t *testing.T) {
	query := strings.Repeat("é", 500)
	args := make([]string, 0, 2006)
	args = append(args, query, "--hybrid", "--limit", "200", "-o", "json")
	for range 1000 {
		args = append(args, "--folder", "folder-a")
	}
	srv, loader := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query   string `json:"query"`
			Limit   int    `json:"limit"`
			Filters []struct {
				Values []string `json:"values"`
			} `json:"filters"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if body.Query != query || body.Limit != 200 || len(body.Filters) != 1 || len(body.Filters[0].Values) != 1000 {
			t.Errorf("boundary inputs were not preserved: query bytes=%d, limit=%d, filters=%#v", len(body.Query), body.Limit, body.Filters)
		}
		writeJSONResponse(w, map[string]any{"items": []any{}})
	})
	defer srv.Close()
	if _, err := runSearchCommand(t, loader, args...); err != nil {
		t.Fatalf("valid boundary inputs rejected: %v", err)
	}
}

func TestHybridSearch_MachineOutputFormats(t *testing.T) {
	for _, format := range []string{"json", "yaml"} {
		for _, empty := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/empty=%t", format, empty), func(t *testing.T) {
				srv, loader := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Errorf("decode request: %v", err)
					}
					if body["limit"] != float64(50) {
						t.Errorf("default limit = %v, want 50", body["limit"])
					}
					if _, exists := body["filters"]; exists {
						t.Error("request should omit filters when no folders are supplied")
					}
					w.Header().Set("Content-Type", "application/json")
					response := hybridResponse
					if empty {
						response = `{"items":[]}`
					}
					_, _ = w.Write([]byte(response))
				})
				defer srv.Close()
				output, err := runSearchCommand(t, loader, "CPU", "--hybrid", "-o", format)
				if err != nil {
					t.Fatalf("search: %v", err)
				}
				var result struct {
					Items []struct {
						Spec struct {
							Score  *float64         `json:"score"`
							Chunks []map[string]any `json:"chunks"`
						} `json:"spec"`
					} `json:"items"`
					Limit int `json:"limit"`
				}
				if err := yaml.Unmarshal([]byte(output), &result); err != nil {
					t.Fatalf("decode output: %v\n%s", err, output)
				}
				if result.Limit != 50 || result.Items == nil {
					t.Fatalf("expected limit 50 and an items array:\n%s", output)
				}
				if empty {
					if len(result.Items) != 0 {
						t.Errorf("expected no items:\n%s", output)
					}
					return
				}
				if len(result.Items) != 2 || result.Items[0].Spec.Score == nil || *result.Items[0].Spec.Score != 0 || len(result.Items[0].Spec.Chunks) != 2 {
					t.Errorf("output lost zero score or matching chunks:\n%s", output)
				}
			})
		}
	}
}

func TestHybridSearch_TableOutputFormats(t *testing.T) {
	for _, format := range []string{"table", "wide"} {
		for _, empty := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/empty=%t", format, empty), func(t *testing.T) {
				srv, loader := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					response := hybridResponse
					if empty {
						response = `{"items":[]}`
					}
					_, _ = w.Write([]byte(response))
				})
				defer srv.Close()
				output, err := runSearchCommand(t, loader, "CPU", "--hybrid", "-o", format)
				if err != nil {
					t.Fatalf("search: %v", err)
				}
				if empty {
					if strings.Contains(output, "dash-z") {
						t.Errorf("empty result rendered a hit:\n%s", output)
					}
					return
				}
				for _, want := range []string{"NAME", "TITLE", "FOLDER", "SCORE", "dash-z", "CPU Overview", "General", "dash-a", "0.9"} {
					if !strings.Contains(output, want) {
						t.Errorf("missing %q in output:\n%s", want, output)
					}
				}
				if strings.Contains(output, "TAGS") || strings.Contains(output, "AGE") || strings.Index(output, "dash-z") > strings.Index(output, "dash-a") {
					t.Errorf("unexpected columns or result order:\n%s", output)
				}
				if format == "wide" && (!strings.Contains(output, "MATCH") || !strings.Contains(output, "CPU saturation")) {
					t.Errorf("wide output omitted best matching chunk:\n%s", output)
				}
			})
		}
	}
}

func TestHybridSearch_ErrorsDoNotFallback(t *testing.T) {
	for _, tt := range []struct {
		status     int
		structured bool
		wantHint   bool
	}{
		{http.StatusForbidden, true, false},
		{http.StatusNotFound, true, true},
		{http.StatusNotImplemented, false, true},
		{http.StatusInternalServerError, false, false},
	} {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			var requests atomic.Int32
			srv, loader := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/dashboards/search/hybrid") {
					t.Errorf("unexpected fallback request: %s %s", r.Method, r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				if tt.structured {
					writeJSONResponse(w, map[string]any{
						"kind": "Status", "apiVersion": "v1", "status": "Failure", "code": tt.status,
						"reason": "TestReason", "message": "hybrid backend failed",
					})
				} else {
					_, _ = w.Write([]byte("hybrid backend failed"))
				}
			})
			defer srv.Close()
			_, err := runSearchCommand(t, loader, "CPU", "--hybrid", "-o", "json")
			if err == nil || !strings.Contains(err.Error(), "hybrid backend failed") || !strings.Contains(err.Error(), strconv.Itoa(tt.status)) {
				t.Fatalf("error did not preserve backend failure: %v", err)
			}
			if gotHint := strings.Contains(err.Error(), "--hybrid"); gotHint != tt.wantHint {
				t.Errorf("remove-flag hint present=%t, want %t: %v", gotHint, tt.wantHint, err)
			}
			if tt.structured {
				var status apierrors.APIStatus
				if !errors.As(err, &status) || int(status.Status().Code) != tt.status || status.Status().Reason != "TestReason" {
					t.Errorf("Kubernetes status was not preserved: %v", err)
				}
			}
			if count := requests.Load(); count != 1 {
				t.Errorf("made %d requests, want one hybrid request without fallback", count)
			}
		})
	}
}

func TestHybridSearch_LexicalOutputUnchanged(t *testing.T) {
	for _, args := range [][]string{{"CPU", "-o", "json"}, {"CPU", "--hybrid=false", "-o", "json"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			srv, loader := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || strings.HasSuffix(r.URL.Path, "/hybrid") {
					t.Errorf("lexical search used %s %s", r.Method, r.URL.Path)
				}
				writeJSONResponse(w, testServerResponse{Hits: []testServerHit{{Name: "dash-1", Title: "CPU"}}})
			})
			defer srv.Close()
			output, err := runSearchCommand(t, loader, args...)
			if err != nil {
				t.Fatal(err)
			}
			var result struct {
				Limit *int `json:"limit"`
				Items []struct {
					Spec map[string]json.RawMessage `json:"spec"`
				} `json:"items"`
			}
			if err := json.Unmarshal([]byte(output), &result); err != nil {
				t.Fatalf("decode output: %v", err)
			}
			if result.Limit != nil || len(result.Items) != 1 || result.Items[0].Spec["score"] != nil || result.Items[0].Spec["chunks"] != nil {
				t.Errorf("lexical output gained hybrid fields:\n%s", output)
			}
		})
	}
}
