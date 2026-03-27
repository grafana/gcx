package alert_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/alert"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, server *httptest.Server) *alert.Client {
	t.Helper()
	cfg := config.NamespacedRESTConfig{
		Config: rest.Config{Host: server.URL},
	}
	client, err := alert.NewClient(cfg)
	require.NoError(t, err)
	return client
}

// writeJSON encodes v as JSON to w.
// Panics on marshal error since test data is always known-good.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	_, _ = w.Write(data)
}

func TestClient_List(t *testing.T) {
	tests := []struct {
		name       string
		opts       alert.ListOptions
		handler    http.HandlerFunc
		wantGroups int
		wantErr    bool
	}{
		{
			name: "success with items",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/api/prometheus/grafana/api/v1/rules", r.URL.Path)
				writeJSON(w, alert.RulesResponse{
					Status: "success",
					Data: alert.RulesData{
						Groups: []alert.RuleGroup{
							{Name: "group-1", Rules: []alert.RuleStatus{{UID: "uid-1", Name: "Rule 1"}}},
							{Name: "group-2", Rules: []alert.RuleStatus{{UID: "uid-2", Name: "Rule 2"}}},
						},
					},
				})
			},
			wantGroups: 2,
			wantErr:    false,
		},
		{
			name: "empty result",
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, alert.RulesResponse{Status: "success", Data: alert.RulesData{}})
			},
			wantGroups: 0,
			wantErr:    false,
		},
		{
			name: "filter params sent",
			opts: alert.ListOptions{GroupName: "my-group", FolderUID: "folder-uid"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "my-group", r.URL.Query().Get("rule_group"))
				assert.Equal(t, "folder-uid", r.URL.Query().Get("folder_uid"))
				writeJSON(w, alert.RulesResponse{Status: "success", Data: alert.RulesData{}})
			},
			wantGroups: 0,
			wantErr:    false,
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				writeJSON(w, alert.ErrorResponse{Error: "internal server error"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			resp, err := client.List(t.Context(), tt.opts)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, resp.Data.Groups, tt.wantGroups)
		})
	}
}

func TestClient_GetRule(t *testing.T) {
	tests := []struct {
		name    string
		uid     string
		handler http.HandlerFunc
		wantErr bool
		wantUID string
	}{
		{
			name: "success",
			uid:  "uid-1",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "uid-1", r.URL.Query().Get("rule_uid"))
				writeJSON(w, alert.RulesResponse{
					Status: "success",
					Data: alert.RulesData{
						Groups: []alert.RuleGroup{
							{Name: "g", Rules: []alert.RuleStatus{{UID: "uid-1", Name: "Rule 1"}}},
						},
					},
				})
			},
			wantErr: false,
			wantUID: "uid-1",
		},
		{
			name: "not found",
			uid:  "uid-missing",
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, alert.RulesResponse{Status: "success", Data: alert.RulesData{}})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			rule, err := client.GetRule(t.Context(), tt.uid)

			if tt.wantErr {
				require.Error(t, err)
				if tt.name == "not found" {
					require.ErrorIs(t, err, alert.ErrNotFound)
				}
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantUID, rule.UID)
		})
	}
}

func TestClient_ListGroups(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantGroups int
		wantErr    bool
	}{
		{
			name: "success",
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, alert.RulesResponse{
					Status: "success",
					Data: alert.RulesData{
						Groups: []alert.RuleGroup{
							{Name: "group-1"},
							{Name: "group-2"},
						},
					},
				})
			},
			wantGroups: 2,
			wantErr:    false,
		},
		{
			name: "empty",
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, alert.RulesResponse{Status: "success", Data: alert.RulesData{}})
			},
			wantGroups: 0,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			groups, err := client.ListGroups(t.Context())

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, groups, tt.wantGroups)
		})
	}
}

func TestClient_GetGroup(t *testing.T) {
	tests := []struct {
		name      string
		groupName string
		handler   http.HandlerFunc
		wantErr   bool
		wantName  string
	}{
		{
			name:      "success",
			groupName: "my-group",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "my-group", r.URL.Query().Get("rule_group"))
				writeJSON(w, alert.RulesResponse{
					Status: "success",
					Data: alert.RulesData{
						Groups: []alert.RuleGroup{
							{Name: "my-group"},
						},
					},
				})
			},
			wantErr:  false,
			wantName: "my-group",
		},
		{
			name:      "not found",
			groupName: "missing-group",
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, alert.RulesResponse{Status: "success", Data: alert.RulesData{}})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			group, err := client.GetGroup(t.Context(), tt.groupName)

			if tt.wantErr {
				require.Error(t, err)
				if tt.name == "not found" {
					require.ErrorIs(t, err, alert.ErrNotFound)
				}
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantName, group.Name)
		})
	}
}

func TestClient_ErrorResponses(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       func(w http.ResponseWriter)
		wantErrMsg string
	}{
		{
			name:       "401 with JSON body",
			statusCode: http.StatusUnauthorized,
			body: func(w http.ResponseWriter) {
				writeJSON(w, alert.ErrorResponse{Error: "unauthorized"})
			},
			wantErrMsg: "401",
		},
		{
			name:       "403 with JSON body",
			statusCode: http.StatusForbidden,
			body: func(w http.ResponseWriter) {
				writeJSON(w, alert.ErrorResponse{Error: "forbidden"})
			},
			wantErrMsg: "403",
		},
		{
			name:       "500 with plain text body",
			statusCode: http.StatusInternalServerError,
			body: func(w http.ResponseWriter) {
				_, _ = w.Write([]byte("internal server error"))
			},
			wantErrMsg: "500",
		},
		{
			name:       "500 with empty body",
			statusCode: http.StatusInternalServerError,
			body:       func(w http.ResponseWriter) {},
			wantErrMsg: "500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				tt.body(w)
			}))
			defer server.Close()

			client := newTestClient(t, server)
			_, err := client.List(t.Context(), alert.ListOptions{})

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErrMsg)
		})
	}
}
