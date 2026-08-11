package kg_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/kg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Client tests ---

func TestClient_ListNotifications(t *testing.T) {
	t.Run("no category hits the collection endpoint", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.True(t, strings.HasSuffix(r.URL.Path, "/config/alerts"),
				"unexpected path %q", r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(kg.AlertConfigs{AlertConfigs: []kg.AlertConfig{
				{Name: "a", MatchLabels: map[string]string{"job": "svc"}},
				{Name: "b", For: "5m", Silenced: true},
			}})
		}))
		defer server.Close()
		client := newTestClient(t, server)

		got, err := client.ListNotifications(t.Context(), "")
		require.NoError(t, err)
		require.Len(t, got.AlertConfigs, 2)
		assert.Equal(t, "a", got.AlertConfigs[0].Name)
		assert.Equal(t, "5m", got.AlertConfigs[1].For)
		assert.True(t, got.AlertConfigs[1].Silenced)
	})

	t.Run("category appends a path suffix", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.True(t, strings.HasSuffix(r.URL.Path, "/config/alerts/slo"),
				"unexpected path %q", r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(kg.AlertConfigs{AlertConfigs: []kg.AlertConfig{{Name: "slo-1"}}})
		}))
		defer server.Close()
		client := newTestClient(t, server)

		got, err := client.ListNotifications(t.Context(), kg.NotificationCategorySLO)
		require.NoError(t, err)
		require.Len(t, got.AlertConfigs, 1)
		assert.Equal(t, "slo-1", got.AlertConfigs[0].Name)
	})
}

func TestClient_GetNotification(t *testing.T) {
	newServer := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(kg.AlertConfigs{AlertConfigs: []kg.AlertConfig{
				{Name: "wanted", MatchLabels: map[string]string{"job": "svc"}, For: "10m"},
				{Name: "other"},
			}})
		}))
	}

	t.Run("hit returns the matching config", func(t *testing.T) {
		server := newServer()
		defer server.Close()
		client := newTestClient(t, server)

		got, err := client.GetNotification(t.Context(), "wanted")
		require.NoError(t, err)
		assert.Equal(t, "wanted", got.Name)
		assert.Equal(t, "10m", got.For)
	})

	t.Run("miss returns a not-found error", func(t *testing.T) {
		server := newServer()
		defer server.Close()
		client := newTestClient(t, server)

		_, err := client.GetNotification(t.Context(), "nope")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		var apiErr *kg.APIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, http.StatusNotFound, apiErr.StatusCode)
	})
}

// --- Command tests ---

func TestNotifications_List_Table(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.True(t, strings.HasSuffix(r.URL.Path, "/config/alerts"), "unexpected path %q", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(kg.AlertConfigs{AlertConfigs: []kg.AlertConfig{
			{Name: "api-server-latency", MatchLabels: map[string]string{"asserts_slo_name": "api-server-latency"}, For: "5m", Silenced: false},
		}})
	}))
	defer server.Close()

	var stdout bytes.Buffer
	cmd := kg.NewNotificationsCommand(writeLoaderFor(server))
	cmd.SetArgs([]string{"list"})
	cmd.SetOut(&stdout)
	require.NoError(t, cmd.Execute())
	assert.Contains(t, stdout.String(), "api-server-latency")
	assert.Contains(t, stdout.String(), "5m")
}

func TestNotifications_List_CategoryFilter(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(kg.AlertConfigs{AlertConfigs: []kg.AlertConfig{{Name: "slo-1"}}})
	}))
	defer server.Close()

	var stdout bytes.Buffer
	cmd := kg.NewNotificationsCommand(writeLoaderFor(server))
	cmd.SetArgs([]string{"list", "--category", "slo", "-o", "json"})
	cmd.SetOut(&stdout)
	require.NoError(t, cmd.Execute())
	assert.True(t, strings.HasSuffix(gotPath, "/config/alerts/slo"), "unexpected path %q", gotPath)
	assert.Contains(t, stdout.String(), "slo-1")
}

func TestNotifications_List_InvalidCategory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("server must not be hit for an invalid category")
	}))
	defer server.Close()

	cmd := kg.NewNotificationsCommand(writeLoaderFor(server))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"list", "--category", "bogus"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --category")
}

func TestNotifications_Get_HitAndMiss(t *testing.T) {
	newServer := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(kg.AlertConfigs{AlertConfigs: []kg.AlertConfig{
				{Name: "wanted", For: "10m"},
			}})
		}))
	}

	t.Run("hit", func(t *testing.T) {
		server := newServer()
		defer server.Close()
		var stdout bytes.Buffer
		cmd := kg.NewNotificationsCommand(writeLoaderFor(server))
		cmd.SetArgs([]string{"get", "wanted", "-o", "json"})
		cmd.SetOut(&stdout)
		require.NoError(t, cmd.Execute())

		var got kg.AlertConfig
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &got))
		assert.Equal(t, "wanted", got.Name)
		assert.Equal(t, "10m", got.For)
	})

	t.Run("miss", func(t *testing.T) {
		server := newServer()
		defer server.Close()
		cmd := kg.NewNotificationsCommand(writeLoaderFor(server))
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		cmd.SetArgs([]string{"get", "nope"})
		err := cmd.Execute()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}
