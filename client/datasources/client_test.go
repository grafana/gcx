package datasources_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	datasources "github.com/grafana/gcx/client/datasources"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestClient(t *testing.T, handler http.Handler) *datasources.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return datasources.NewClient(server.Client(), server.URL)
}

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
		name      string
		handler   http.HandlerFunc
		wantNames []string
		wantErr   bool
	}{
		{
			name: "success with items",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/api/datasources", r.URL.Path)
				writeJSON(w, []datasources.Datasource{
					{UID: "uid-1", Name: "prom", Type: "prometheus"},
					{UID: "uid-2", Name: "loki", Type: "loki"},
				})
			},
			wantNames: []string{"prom", "loki"},
		},
		{
			name: "empty result",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, []datasources.Datasource{})
			},
			wantNames: []string{},
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				writeJSON(w, map[string]string{"message": "access denied"})
			},
			wantErr: true,
		},
		{
			name: "malformed body",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("not json"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, tt.handler)
			got, err := client.List(context.Background())
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			names := make([]string, 0, len(got))
			for _, ds := range got {
				names = append(names, ds.Name)
			}
			assert.Equal(t, tt.wantNames, names)
		})
	}
}

func TestClient_ListPluginTypes(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		want    []datasources.PluginType
		wantErr bool
	}{
		{
			name: "sorted by id",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/api/plugins", r.URL.Path)
				assert.Equal(t, "datasource", r.URL.Query().Get("type"))
				_, _ = w.Write([]byte(`[
					{"id":"prometheus","name":"Prometheus","category":"tsdb","enabled":true},
					{"id":"cloudwatch","name":"CloudWatch","category":"cloud"},
					{"id":"alertmanager","name":"Alertmanager","category":""}
				]`))
			},
			want: []datasources.PluginType{
				{ID: "alertmanager", Name: "Alertmanager", Category: ""},
				{ID: "cloudwatch", Name: "CloudWatch", Category: "cloud"},
				{ID: "prometheus", Name: "Prometheus", Category: "tsdb"},
			},
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				writeJSON(w, map[string]string{"message": "access denied"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, tt.handler)
			got, err := client.ListPluginTypes(context.Background())
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestClient_Get(t *testing.T) {
	tests := []struct {
		name     string
		lookup   func(*datasources.Client) (*datasources.Datasource, error)
		wantPath string
		status   int
		body     string
		wantUID  string
		wantErr  bool
	}{
		{
			name: "by uid",
			lookup: func(c *datasources.Client) (*datasources.Datasource, error) {
				return c.GetByUID(context.Background(), "uid-1")
			},
			wantPath: "/api/datasources/uid/uid-1",
			status:   http.StatusOK,
			body:     `{"uid":"uid-1","name":"prom","type":"prometheus"}`,
			wantUID:  "uid-1",
		},
		{
			name: "by name escapes path",
			lookup: func(c *datasources.Client) (*datasources.Datasource, error) {
				return c.GetByName(context.Background(), "my prom")
			},
			wantPath: "/api/datasources/name/my prom",
			status:   http.StatusOK,
			body:     `{"uid":"uid-1","name":"my prom","type":"prometheus"}`,
			wantUID:  "uid-1",
		},
		{
			name: "not found",
			lookup: func(c *datasources.Client) (*datasources.Datasource, error) {
				return c.GetByUID(context.Background(), "missing")
			},
			wantPath: "/api/datasources/uid/missing",
			status:   http.StatusNotFound,
			body:     `{"message":"Datasource not found"}`,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, tt.wantPath, r.URL.Path)
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))

			got, err := tt.lookup(client)
			if tt.wantErr {
				require.Error(t, err)
				assert.True(t, datasources.IsNotFound(err))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantUID, got.UID)
		})
	}
}

func TestClient_Write(t *testing.T) {
	tests := []struct {
		name       string
		call       func(*datasources.Client) (*datasources.Datasource, error)
		wantMethod string
		wantPath   string
		body       string
		wantName   string
	}{
		{
			name: "create wrapped response",
			call: func(c *datasources.Client) (*datasources.Datasource, error) {
				return c.Create(context.Background(), &datasources.Datasource{UID: "uid-1", Name: "prom", Type: "prometheus"})
			},
			wantMethod: http.MethodPost,
			wantPath:   "/api/datasources",
			body:       `{"datasource":{"uid":"uid-1","name":"prom","type":"prometheus"},"id":1,"message":"Datasource added"}`,
			wantName:   "prom",
		},
		{
			name: "update direct response",
			call: func(c *datasources.Client) (*datasources.Datasource, error) {
				return c.Update(context.Background(), "uid-1", &datasources.Datasource{Name: "prom2", Type: "prometheus"})
			},
			wantMethod: http.MethodPut,
			wantPath:   "/api/datasources/uid/uid-1",
			body:       `{"uid":"uid-1","name":"prom2","type":"prometheus"}`,
			wantName:   "prom2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, tt.wantMethod, r.Method)
				assert.Equal(t, tt.wantPath, r.URL.Path)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				_, _ = w.Write([]byte(tt.body))
			}))

			got, err := tt.call(client)
			require.NoError(t, err)
			assert.Equal(t, tt.wantName, got.Name)
		})
	}
}

func TestClient_Delete(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		wantErr bool
	}{
		{name: "success", status: http.StatusOK},
		{name: "not found", status: http.StatusNotFound, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodDelete, r.Method)
				assert.Equal(t, "/api/datasources/uid/uid-1", r.URL.Path)
				w.WriteHeader(tt.status)
			}))

			err := client.Delete(context.Background(), "uid-1")
			if tt.wantErr {
				require.Error(t, err)
				assert.True(t, datasources.IsNotFound(err))
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestClient_Health(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		want    *datasources.HealthResult
		wantErr bool
	}{
		{
			name:   "ok",
			status: http.StatusOK,
			body:   `{"status":"OK","message":"Data source is working"}`,
			want:   &datasources.HealthResult{UID: "uid-1", Status: "OK", Message: "Data source is working"},
		},
		{
			name:    "server error",
			status:  http.StatusBadRequest,
			body:    `{"message":"bad gateway"}`,
			wantErr: true,
		},
		{
			name:    "malformed body",
			status:  http.StatusOK,
			body:    "not json",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/api/datasources/uid/uid-1/health", r.URL.Path)
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))

			got, err := client.Health(context.Background(), "uid-1")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// The CLI renders datasource failures from APIError's fields and message, so
// both are pinned here.
func TestClient_APIErrorMessages(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		call     func(*datasources.Client) error
		wantOp   string
		wantID   string
		wantMsg  string
		wantText string
	}{
		{
			name:   "list with message field",
			status: http.StatusForbidden,
			body:   `{"message":"access denied"}`,
			call: func(c *datasources.Client) error {
				_, err := c.List(context.Background())
				return err
			},
			wantOp:   "list datasources",
			wantMsg:  "access denied",
			wantText: "list datasources failed with status 403: access denied",
		},
		{
			name:   "get with identifier and error field",
			status: http.StatusNotFound,
			body:   `{"error":"Datasource not found"}`,
			call: func(c *datasources.Client) error {
				_, err := c.GetByUID(context.Background(), "missing")
				return err
			},
			wantOp:   "get datasource",
			wantID:   "missing",
			wantMsg:  "Datasource not found",
			wantText: `get datasource "missing" failed with status 404: Datasource not found`,
		},
		{
			name:   "empty body omits message",
			status: http.StatusInternalServerError,
			body:   "",
			call: func(c *datasources.Client) error {
				return c.Delete(context.Background(), "uid-1")
			},
			wantOp:   "delete datasource",
			wantID:   "uid-1",
			wantText: `delete datasource "uid-1" failed with status 500`,
		},
		{
			name:   "non-json body used verbatim",
			status: http.StatusBadGateway,
			body:   "upstream unavailable",
			call: func(c *datasources.Client) error {
				_, err := c.Health(context.Background(), "uid-1")
				return err
			},
			wantOp:   "check datasource health",
			wantID:   "uid-1",
			wantMsg:  "upstream unavailable",
			wantText: `check datasource health "uid-1" failed with status 502: upstream unavailable`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))

			err := tt.call(client)
			require.Error(t, err)

			var apiErr *datasources.APIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, tt.wantOp, apiErr.Operation)
			assert.Equal(t, tt.wantID, apiErr.Identifier)
			assert.Equal(t, tt.status, apiErr.StatusCode)
			assert.Equal(t, tt.wantMsg, apiErr.Message)
			assert.Equal(t, tt.wantText, apiErr.Error())
			assert.Equal(t, tt.status, apiErr.HTTPStatusCode())
			assert.Equal(t, "Datasources", apiErr.APIServiceName())
			assert.Equal(t, tt.wantMsg, apiErr.APIUserMessage())
		})
	}
}
