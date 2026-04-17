package preferences_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/preferences"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, server *httptest.Server) *preferences.Client {
	t.Helper()
	cfg := config.NamespacedRESTConfig{
		Config: rest.Config{Host: server.URL},
	}
	client, err := preferences.NewClient(cfg)
	require.NoError(t, err)
	return client
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	_, _ = w.Write(data)
}

func TestClient_Get(t *testing.T) {
	tests := []struct {
		name     string
		handler  http.HandlerFunc
		wantErr  bool
		wantPref preferences.OrgPreferences
	}{
		{
			name: "success",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/api/org/preferences", r.URL.Path)
				writeJSON(w, preferences.OrgPreferences{
					Theme:           "dark",
					Timezone:        "UTC",
					WeekStart:       "monday",
					Locale:          "en-US",
					HomeDashboardID: 42,
				})
			},
			wantPref: preferences.OrgPreferences{
				Theme:           "dark",
				Timezone:        "UTC",
				WeekStart:       "monday",
				Locale:          "en-US",
				HomeDashboardID: 42,
			},
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				writeJSON(w, preferences.ErrorResponse{Message: "boom"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			got, err := client.Get(t.Context())

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "500")
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantPref, *got)
		})
	}
}

func TestClient_Update(t *testing.T) {
	tests := []struct {
		name    string
		prefs   preferences.OrgPreferences
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "success",
			prefs: preferences.OrgPreferences{
				Theme:    "light",
				Timezone: "Europe/Amsterdam",
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPut, r.Method)
				assert.Equal(t, "/api/org/preferences", r.URL.Path)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

				body, err := io.ReadAll(r.Body)
				if !assert.NoError(t, err) {
					return
				}

				var got preferences.OrgPreferences
				if !assert.NoError(t, json.Unmarshal(body, &got)) {
					return
				}
				assert.Equal(t, "light", got.Theme)
				assert.Equal(t, "Europe/Amsterdam", got.Timezone)

				writeJSON(w, map[string]string{"message": "Preferences updated"})
			},
		},
		{
			name:  "server error",
			prefs: preferences.OrgPreferences{Theme: "dark"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				writeJSON(w, preferences.ErrorResponse{Message: "boom"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			err := client.Update(t.Context(), &tt.prefs)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "500")
				return
			}
			require.NoError(t, err)
		})
	}
}
