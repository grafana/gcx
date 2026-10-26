package ml

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientCRUD(t *testing.T) {
	for _, path := range []string{"jobs", "holidays", "outliers", "jobs/parent/alerts", "outliers/parent/alerts"} {
		t.Run(path, func(t *testing.T) {
			var methods []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				methods = append(methods, r.Method+" "+r.URL.Path)
				assert.Equal(t, "Bearer test", r.Header.Get("Authorization"))
				if r.Method == http.MethodPost {
					var payload map[string]any
					assert.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
					assert.NotContains(t, payload, "id")
					assert.Equal(t, "https://public.example", payload["grafanaUrl"])
					assert.Equal(t, "forecast", payload["name"])
				}
				if r.Method == http.MethodDelete {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet && r.URL.Path == basePath+path {
					fmt.Fprint(w, `{"status":"success","data":[{"id":"one","name":"forecast","grafanaApiKey":"secret","createdBy":"server"}]}`)
				} else {
					fmt.Fprint(w, `{"status":"success","data":{"id":"one","name":"forecast"}}`)
				}
			}))
			defer srv.Close()
			httpClient := srv.Client()
			httpClient.Transport = bearerTransport{next: httpClient.Transport}
			c := &client[Job]{deps: adapter.ClientDeps{HTTP: httpClient, BaseURL: srv.URL, GrafanaURL: "https://public.example"}, path: path}
			ctx := context.Background()
			items, err := c.List(ctx, adapter.ListOptions{})
			require.NoError(t, err)
			require.Len(t, items, 1)
			raw, err := json.Marshal(items)
			require.NoError(t, err)
			require.NotContains(t, string(raw), "secret")
			require.NotContains(t, string(raw), "createdBy")
			_, err = c.Get(ctx, "one")
			require.NoError(t, err)
			input := Job{ID: "source-id", Name: "forecast", GrafanaURL: "https://source.example"}
			_, err = c.Create(ctx, &input)
			require.NoError(t, err)
			_, err = c.Update(ctx, "one", &input)
			require.NoError(t, err)
			require.Equal(t, "source-id", input.ID)
			require.NoError(t, c.Delete(ctx, "one"))
			require.Equal(t, []string{"GET " + basePath + path, "GET " + basePath + path + "/one", "POST " + basePath + path, "POST " + basePath + path + "/one", "DELETE " + basePath + path + "/one"}, methods)
		})
	}
}

type bearerTransport struct{ next http.RoundTripper }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r.Header.Set("Authorization", "Bearer test")
	return b.next.RoundTrip(r)
}

func TestClientErrors(t *testing.T) {
	tests := []struct {
		name, body string
		status     int
		want       string
	}{
		{"not found", `{"message":"missing"}`, 404, "not found"},
		{"permission", `{"message":"denied"}`, 403, "403"},
		{"API failure", `{"status":"error","error":"failed","data":{}}`, 200, "failed"},
		{"missing data", `{"status":"success"}`, 200, "no data"},
		{"null data", `{"status":"success","data":null}`, 200, "no data"},
		{"wrong ID", `{"status":"success","data":{"id":"other"}}`, 200, "does not match"},
		{"malformed", `invalid`, 200, "decode"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tt.status); fmt.Fprint(w, tt.body) }))
			defer srv.Close()
			c := &client[Job]{deps: adapter.ClientDeps{HTTP: srv.Client(), BaseURL: srv.URL}, path: "jobs"}
			_, err := c.Get(context.Background(), "one")
			require.ErrorContains(t, err, tt.want)
			if tt.status == 404 {
				require.ErrorIs(t, err, adapter.ErrNotFound)
			}
		})
	}
}

func TestInvalidIDDoesNotSendRequest(t *testing.T) {
	for _, id := range []string{"", ".", "..", "a/b", "a?b", "a#b", "%2f"} {
		t.Run(id, func(t *testing.T) {
			c := &client[Job]{}
			_, err := c.Get(context.Background(), id)
			require.Error(t, err)
			_, err = c.Update(context.Background(), id, &Job{})
			require.Error(t, err)
			require.Error(t, c.Delete(context.Background(), id))
		})
	}
}
