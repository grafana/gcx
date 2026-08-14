package irm_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/irm"
)

func TestMoveToPosition(t *testing.T) {
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

func TestMoveToPositionNotFound(t *testing.T) {
	t.Parallel()

	client := newTestOnCallClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	err := client.MoveEscalationPolicy(context.Background(), "missing", 1)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected a not-found error, got %v", err)
	}
}

// TestEscalationPolicyPositionRoundTrip asserts that the position survives a
// create. Before this field existed, a caller could not author a chain
// deterministically from manifests: the step order depended on the order of
// the writes.
func TestEscalationPolicyPositionRoundTrip(t *testing.T) {
	t.Parallel()

	var gotBody map[string]any
	client := newTestOnCallClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody) //nolint:errcheck
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"id": "EP1", "escalation_chain": "EC1", "step": 0, "position": 2,
		})
	}))

	position := 2
	got, err := client.CreateEscalationPolicy(context.Background(), irm.EscalationPolicy{
		EscalationChain: "EC1",
		Step:            0,
		Position:        &position,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotBody["position"] != float64(2) {
		t.Errorf("position did not reach the backend: %v", gotBody)
	}
	if got.Position == nil || *got.Position != 2 {
		t.Errorf("position missing from the result: %+v", got)
	}
}

// TestRoutePositionZeroReachesBackend guards the pointer choice: with a plain
// int and omitempty, position 0 — the first route — would be dropped.
func TestRoutePositionZeroReachesBackend(t *testing.T) {
	t.Parallel()

	var gotBody map[string]any
	client := newTestOnCallClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody) //nolint:errcheck
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": "R1", "position": 0}) //nolint:errcheck
	}))

	position := 0
	if _, err := client.CreateRoute(context.Background(), irm.Route{
		AlertReceiveChannel: "INT1",
		Position:            &position,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := gotBody["position"]; !ok {
		t.Errorf("position 0 was dropped from the request body: %v", gotBody)
	}
	if gotBody["position"] != float64(0) {
		t.Errorf("got position %v, want 0", gotBody["position"])
	}
}

// TestRoutePositionAbsentStaysAbsent asserts the other half of the pointer
// choice: a caller who does not set a position must not send one, so the
// backend keeps its own ordering.
func TestRoutePositionAbsentStaysAbsent(t *testing.T) {
	t.Parallel()

	var gotBody map[string]any
	client := newTestOnCallClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody) //nolint:errcheck
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": "R1"}) //nolint:errcheck
	}))

	if _, err := client.CreateRoute(context.Background(), irm.Route{AlertReceiveChannel: "INT1"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := gotBody["position"]; ok {
		t.Errorf("an unset position reached the backend: %v", gotBody)
	}
}
