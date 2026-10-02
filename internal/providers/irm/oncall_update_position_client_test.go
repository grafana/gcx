package irm_test

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/irm"
)

func TestUpdatePositionClientRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		position int
		call     func(client *irm.OnCallClient, position int) error
		wantPath string
	}{
		{
			name:     "escalation policy to the first step",
			position: 0,
			call: func(client *irm.OnCallClient, position int) error {
				return client.MoveEscalationPolicy(context.Background(), "EP1", position)
			},
			wantPath: irm.BasePath + "/escalation_policies/EP1/move_to_position/",
		},
		{
			name:     "escalation policy to a later step",
			position: 3,
			call: func(client *irm.OnCallClient, position int) error {
				return client.MoveEscalationPolicy(context.Background(), "EP1", position)
			},
			wantPath: irm.BasePath + "/escalation_policies/EP1/move_to_position/",
		},
		{
			name:     "route to the first position",
			position: 0,
			call: func(client *irm.OnCallClient, position int) error {
				return client.MoveRoute(context.Background(), "R1", position)
			},
			wantPath: irm.BasePath + "/channel_filters/R1/move_to_position/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotMethod, gotPath, gotPosition string
			client := newTestOnCallClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				gotPath = r.URL.Path
				gotPosition = r.URL.Query().Get("position")
				w.WriteHeader(http.StatusOK)
				io.WriteString(w, "{}") //nolint:errcheck
			}))

			if err := tt.call(client, tt.position); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if gotMethod != http.MethodPut {
				t.Errorf("got method %q, want PUT", gotMethod)
			}
			if gotPath != tt.wantPath {
				t.Errorf("got path %q, want %q", gotPath, tt.wantPath)
			}
			// Position 0 is the first slot, not an absent value, so it must
			// reach the backend.
			want := strconv.Itoa(tt.position)
			if gotPosition != want {
				t.Errorf("got position %q, want %q", gotPosition, want)
			}
		})
	}
}

func TestUpdatePositionClientNotFound(t *testing.T) {
	t.Parallel()

	client := newTestOnCallClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	err := client.MoveEscalationPolicy(context.Background(), "missing", 1)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected a not-found error, got %v", err)
	}
}
