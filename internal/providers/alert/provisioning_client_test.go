package alert_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/providers/alert"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_ListContactPoints(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		want    int
		wantErr bool
	}{
		{
			name: "returns contact points",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/api/v1/provisioning/contact-points", r.URL.Path)
				writeJSON(w, []alert.ContactPoint{
					{UID: "u1", Name: "Primary", Type: "webhook"},
					{UID: "u2", Name: "Secondary", Type: "email"},
				})
			},
			want: 2,
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

			client := newTestClient(t, server)
			points, err := client.ListContactPoints(context.Background())
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, points, tt.want)
		})
	}
}

func TestClient_GetContactPoint_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, []alert.ContactPoint{{UID: "u1", Name: "Primary"}})
	}))
	defer server.Close()

	client := newTestClient(t, server)
	_, err := client.GetContactPoint(context.Background(), "missing")
	require.Error(t, err)
	assert.ErrorIs(t, err, alert.ErrProvisioningNotFound)
}

func TestClient_MuteTimings_CRUD(t *testing.T) {
	var lastMethod, lastPath string
	var lastBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastMethod = r.Method
		lastPath = r.URL.Path
		lastBody, _ = io.ReadAll(r.Body)
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, alert.MuteTiming{Name: "weekends"})
		case http.MethodPost:
			writeJSON(w, alert.MuteTiming{Name: "weekends"})
		case http.MethodPut:
			w.WriteHeader(http.StatusAccepted) // 202, no body
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server)
	ctx := context.Background()

	got, err := client.GetMuteTiming(ctx, "weekends")
	require.NoError(t, err)
	assert.Equal(t, "weekends", got.Name)
	assert.Equal(t, "/api/v1/provisioning/mute-timings/weekends", lastPath)

	created, err := client.CreateMuteTiming(ctx, alert.MuteTiming{Name: "weekends"})
	require.NoError(t, err)
	assert.Equal(t, "weekends", created.Name)
	assert.Equal(t, http.MethodPost, lastMethod)

	updated, err := client.UpdateMuteTiming(ctx, "weekends", alert.MuteTiming{Name: "weekends"})
	require.NoError(t, err)
	assert.Equal(t, "weekends", updated.Name) // PUT returns 202 with no body; the input is echoed back.
	assert.NotEmpty(t, lastBody)

	require.NoError(t, client.DeleteMuteTiming(ctx, "weekends"))
	assert.Equal(t, http.MethodDelete, lastMethod)
}

func TestClient_NotificationPolicy_RoundTrip(t *testing.T) {
	var putBody alert.NotificationPolicy
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, alert.NotificationPolicy{
				Receiver: "default",
				Routes: []alert.NotificationRoute{{
					Receiver: "team-a",
					Matchers: []alert.Matcher{{Label: "severity", Match: "=", Value: "info"}},
				}},
			})
		case http.MethodPut:
			if err := json.NewDecoder(r.Body).Decode(&putBody); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusAccepted)
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server)
	ctx := context.Background()

	policy, err := client.GetNotificationPolicy(ctx)
	require.NoError(t, err)
	require.Len(t, policy.Routes, 1)
	assert.Equal(t, "severity", policy.Routes[0].Matchers[0].Label)

	require.NoError(t, client.SetNotificationPolicy(ctx, *policy))
	// Matchers must round-trip as the 3-element array wire format.
	require.Len(t, putBody.Routes, 1)
	assert.Equal(t, "info", putBody.Routes[0].Matchers[0].Value)

	require.NoError(t, client.ResetNotificationPolicy(ctx))
}

func TestClient_Templates_UpsertHandles202EmptyBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/api/v1/provisioning/templates/welcome", r.URL.Path)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	saved, err := client.UpsertTemplate(context.Background(), alert.NotificationTemplate{Name: "welcome", Template: "{{ .Alerts }}"})
	require.NoError(t, err)
	assert.Equal(t, "welcome", saved.Name)
}

func TestClient_Export_ReturnsRawBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/provisioning/contact-points/export", r.URL.Path)
		assert.Equal(t, "yaml", r.URL.Query().Get("format"))
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write([]byte("apiVersion: 1\ncontactPoints: []\n"))
	}))
	defer server.Close()

	client := newTestClient(t, server)
	data, err := client.ExportContactPoints(context.Background(), "yaml")
	require.NoError(t, err)
	assert.Contains(t, string(data), "apiVersion: 1")
}

func TestClient_doJSON_NotFoundWrapsSentinelError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	_, err := client.GetMuteTiming(context.Background(), "missing")
	require.Error(t, err)
	assert.ErrorIs(t, err, alert.ErrProvisioningNotFound)
}
