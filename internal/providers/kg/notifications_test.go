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

func TestClient_UpsertNotification(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.True(t, strings.HasSuffix(r.URL.Path, "/config/alert"),
			"single-item upsert must use the singular path, got %q", r.URL.Path)
		var got kg.AlertConfig
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		assert.Equal(t, "api-server-latency", got.Name)
		assert.Equal(t, "5m", got.For)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client := newTestClient(t, server)

	err := client.UpsertNotification(t.Context(), kg.AlertConfig{
		Name:        "api-server-latency",
		MatchLabels: map[string]string{"asserts_slo_name": "api-server-latency"},
		For:         "5m",
	})
	require.NoError(t, err)
}

func TestClient_DeleteNotification(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.True(t, strings.HasSuffix(r.URL.Path, "/config/alert/my-config"),
			"unexpected path %q", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client := newTestClient(t, server)

	require.NoError(t, client.DeleteNotification(t.Context(), "my-config"))
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

func TestNotifications_Upsert_Batch(t *testing.T) {
	var upserted []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.True(t, strings.HasSuffix(r.URL.Path, "/config/alert"), "unexpected path %q", r.URL.Path)
		var got kg.AlertConfig
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		upserted = append(upserted, got.Name)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	const in = `alertConfigs:
  - name: first
    matchLabels:
      job: a
    for: 5m
  - name: second
    silenced: true
`
	var stdout bytes.Buffer
	cmd := kg.NewNotificationsCommand(writeLoaderFor(server))
	cmd.SetArgs([]string{"upsert", "-f", "-"})
	cmd.SetIn(bytes.NewBufferString(in))
	cmd.SetOut(&stdout)
	require.NoError(t, cmd.Execute())
	assert.Equal(t, []string{"first", "second"}, upserted)
	assert.Contains(t, stdout.String(), "2 notification config(s) upserted")
}

func TestNotifications_Upsert_EmptyFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("server must not be hit when the file has no configs")
	}))
	defer server.Close()

	cmd := kg.NewNotificationsCommand(writeLoaderFor(server))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"upsert", "-f", "-"})
	cmd.SetIn(bytes.NewBufferString("alertConfigs: []\n"))
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no notification configs found")
}

func TestNotifications_Upsert_EmptyName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("server must not be hit when an entry has an empty name")
	}))
	defer server.Close()

	cmd := kg.NewNotificationsCommand(writeLoaderFor(server))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"upsert", "-f", "-"})
	// Second entry has no name; validation must fail before any request.
	cmd.SetIn(bytes.NewBufferString("alertConfigs:\n  - name: ok\n  - matchLabels:\n      alertname: A\n"))
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "notification config 1 has an empty name")
}

func TestNotifications_Delete(t *testing.T) {
	var deleted string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		deleted = strings.TrimPrefix(r.URL.Path[strings.LastIndex(r.URL.Path, "/config/alert/")+len("/config/alert/"):], "")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	var stdout bytes.Buffer
	cmd := kg.NewNotificationsCommand(writeLoaderFor(server))
	cmd.SetArgs([]string{"delete", "my-config", "--force"})
	cmd.SetOut(&stdout)
	require.NoError(t, cmd.Execute())
	assert.Equal(t, "my-config", deleted)
	assert.Contains(t, stdout.String(), "deleted")
}
