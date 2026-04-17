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

func newTestClient(t *testing.T, handler http.HandlerFunc) (*preferences.Client, func()) {
	t.Helper()
	server := httptest.NewServer(handler)
	client, err := preferences.NewClient(config.NamespacedRESTConfig{
		Config: rest.Config{Host: server.URL},
	})
	require.NoError(t, err)
	return client, server.Close
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	_, _ = w.Write(data)
}

func TestClient_Get_Success(t *testing.T) {
	want := preferences.OrgPreferences{
		Theme:           "dark",
		Timezone:        "UTC",
		WeekStart:       "monday",
		Locale:          "en-US",
		HomeDashboardID: 42,
	}
	client, cleanup := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/org/preferences", r.URL.Path)
		writeJSON(w, want)
	})
	defer cleanup()

	got, err := client.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, want, *got)
}

func TestClient_Get_ServerError(t *testing.T) {
	client, cleanup := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, preferences.ErrorResponse{Message: "boom"})
	})
	defer cleanup()

	_, err := client.Get(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestClient_Update_Success(t *testing.T) {
	prefs := preferences.OrgPreferences{Theme: "light", Timezone: "Europe/Amsterdam"}
	client, cleanup := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/api/org/preferences", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		var got preferences.OrgPreferences
		assert.NoError(t, json.Unmarshal(body, &got))
		assert.Equal(t, prefs, got)

		writeJSON(w, map[string]string{"message": "Preferences updated"})
	})
	defer cleanup()

	require.NoError(t, client.Update(t.Context(), &prefs))
}

func TestClient_Update_ServerError(t *testing.T) {
	client, cleanup := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, preferences.ErrorResponse{Message: "boom"})
	})
	defer cleanup()

	err := client.Update(t.Context(), &preferences.OrgPreferences{Theme: "dark"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}
