package alert_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/alert"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/yaml"
)

func newTestRulerClient(t *testing.T, server *httptest.Server, subtype string) *alert.RulerClient {
	t.Helper()
	cfg := config.NamespacedRESTConfig{
		Config: rest.Config{Host: server.URL},
	}
	client, err := alert.NewRulerClient(cfg, "my-ds", subtype)
	require.NoError(t, err)
	return client
}

func writeYAML(w http.ResponseWriter, v any) {
	data, err := yaml.Marshal(v)
	if err != nil {
		panic(err)
	}
	w.Header().Set("Content-Type", "application/yaml")
	_, _ = w.Write(data)
}

func TestRulerClient_ListNamespaces(t *testing.T) {
	tests := []struct {
		name    string
		subtype string
		handler http.HandlerFunc
		want    int
		wantErr bool
	}{
		{
			name:    "returns namespaces with mimir subtype",
			subtype: "mimir",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/api/ruler/my-ds/api/v1/rules", r.URL.Path)
				assert.Equal(t, "mimir", r.URL.Query().Get("subtype"))
				writeYAML(w, map[string][]alert.RulerRuleGroup{
					"ns-a": {{Name: "g1", Rules: []alert.RulerRule{{Alert: "A", Expr: "up == 0"}}}},
					"ns-b": {{Name: "g2", Rules: []alert.RulerRule{{Record: "r", Expr: "up"}}}},
				})
			},
			want: 2,
		},
		{
			name: "no subtype query param when subtype is empty",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.False(t, r.URL.Query().Has("subtype"))
				writeYAML(w, map[string][]alert.RulerRuleGroup{})
			},
			want: 0,
		},
		{
			name: "404 empty tenant maps to empty result",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte("no rule groups found\n"))
			},
			want: 0,
		},
		{
			name: "server error surfaces",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestRulerClient(t, server, tt.subtype)
			namespaces, err := client.ListNamespaces(context.Background())
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, namespaces, tt.want)
		})
	}
}

func TestRulerClient_GetGroup(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		group     string
		handler   http.HandlerFunc
		wantName  string
		wantErr   error
	}{
		{
			name:      "returns group with escaped path segments",
			namespace: "my ns",
			group:     "high/errors",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/api/ruler/my-ds/api/v1/rules/my%20ns/high%2Ferrors", r.URL.RawPath)
				writeYAML(w, alert.RulerRuleGroup{Name: "high/errors", Rules: []alert.RulerRule{{Alert: "A", Expr: "up == 0"}}})
			},
			wantName: "high/errors",
		},
		{
			name:      "404 maps to ErrRulerNotFound",
			namespace: "ns",
			group:     "missing",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr: alert.ErrRulerNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestRulerClient(t, server, "")
			group, err := client.GetGroup(context.Background(), tt.namespace, tt.group)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantName, group.Name)
		})
	}
}

func TestRulerClient_ApplyGroup(t *testing.T) {
	group := alert.RulerRuleGroup{
		Name:     "g1",
		Interval: "1m",
		Rules:    []alert.RulerRule{{Alert: "A", Expr: "up == 0", For: "5m"}},
	}

	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "posts JSON body and accepts 202 with empty body",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/api/ruler/my-ds/api/v1/rules/ns", r.URL.Path)
				// Grafana's ruler proxy binds POST bodies as JSON only.
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				body, err := io.ReadAll(r.Body)
				assert.NoError(t, err)
				var got alert.RulerRuleGroup
				assert.NoError(t, yaml.Unmarshal(body, &got))
				assert.Equal(t, group, got)
				w.WriteHeader(http.StatusAccepted)
			},
		},
		{
			name: "400 surfaces as error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"message":"invalid rule group"}`))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestRulerClient(t, server, "")
			err := client.ApplyGroup(context.Background(), "ns", group)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestRulerClient_Delete(t *testing.T) {
	t.Run("delete group", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodDelete, r.Method)
			assert.Equal(t, "/api/ruler/my-ds/api/v1/rules/ns/g1", r.URL.Path)
			assert.Equal(t, "mimir", r.URL.Query().Get("subtype"))
			w.WriteHeader(http.StatusAccepted)
		}))
		defer server.Close()

		client := newTestRulerClient(t, server, "mimir")
		require.NoError(t, client.DeleteGroup(context.Background(), "ns", "g1"))
	})

	t.Run("delete namespace", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodDelete, r.Method)
			assert.Equal(t, "/api/ruler/my-ds/api/v1/rules/ns", r.URL.Path)
			w.WriteHeader(http.StatusAccepted)
		}))
		defer server.Close()

		client := newTestRulerClient(t, server, "")
		require.NoError(t, client.DeleteNamespace(context.Background(), "ns"))
	})

	t.Run("delete missing group maps to ErrRulerNotFound", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		client := newTestRulerClient(t, server, "")
		require.ErrorIs(t, client.DeleteGroup(context.Background(), "ns", "missing"), alert.ErrRulerNotFound)
	})
}
