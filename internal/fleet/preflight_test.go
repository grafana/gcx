package fleet_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/grafana/gcx/internal/fleet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckCollectorApp(t *testing.T) {
	tests := []struct {
		name             string
		settingsStatus   int
		enabled          bool
		actionsStatus    int
		actions          map[string]bool
		want             fleet.CollectorAppState
		wantActionsCalls int64
	}{
		{
			name:           "plugin is absent",
			settingsStatus: http.StatusNotFound,
			want: fleet.CollectorAppState{
				PluginKnown: true,
			},
		},
		{
			name:           "plugin is disabled",
			settingsStatus: http.StatusOK,
			want: fleet.CollectorAppState{
				PluginKnown: true,
				Installed:   true,
			},
		},
		{
			name:           "plugin state is unknown",
			settingsStatus: http.StatusForbidden,
			want: fleet.CollectorAppState{
				PluginStatus: http.StatusForbidden,
			},
		},
		{
			name:           "plugin has both route actions",
			settingsStatus: http.StatusOK,
			enabled:        true,
			actionsStatus:  http.StatusOK,
			actions: map[string]bool{
				fleet.CollectorAppReadAction:  true,
				fleet.CollectorAppAdminAction: true,
			},
			want: fleet.CollectorAppState{
				PluginKnown:  true,
				Installed:    true,
				Enabled:      true,
				ActionsKnown: true,
				CanRead:      true,
				CanAdmin:     true,
			},
			wantActionsCalls: 1,
		},
		{
			name:           "route actions are unknown",
			settingsStatus: http.StatusOK,
			enabled:        true,
			actionsStatus:  http.StatusForbidden,
			want: fleet.CollectorAppState{
				PluginKnown: true,
				Installed:   true,
				Enabled:     true,
			},
			wantActionsCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var actionsCalls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case fleet.CollectorAppSettingsPath:
					if tt.settingsStatus != http.StatusOK {
						w.WriteHeader(tt.settingsStatus)
						return
					}
					writeFleetJSON(w, map[string]any{
						"id":      fleet.CollectorAppID,
						"enabled": tt.enabled,
					})
				case "/api/access-control/user/actions":
					actionsCalls.Add(1)
					if tt.actionsStatus != http.StatusOK {
						w.WriteHeader(tt.actionsStatus)
						return
					}
					writeFleetJSON(w, tt.actions)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()

			got, err := fleet.CheckCollectorApp(context.Background(), server.URL, server.Client())
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantActionsCalls, actionsCalls.Load())
		})
	}
}

func TestCheckCollectorApp_RejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"enabled":true,"padding":"` + strings.Repeat("x", (1<<20)+1) + `"}`))
	}))
	defer server.Close()

	_, err := fleet.CheckCollectorApp(context.Background(), server.URL, server.Client())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "response body exceeds 1 MB limit")
}

func writeFleetJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
