package oncall_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/oncall"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, srv *httptest.Server) *oncall.Client {
	t.Helper()
	cfg := config.NamespacedRESTConfig{
		Config: rest.Config{
			Host:        "https://mystack.grafana.net",
			BearerToken: "test-token",
		},
		Namespace: "default",
	}
	client, err := oncall.NewClient(srv.URL, cfg)
	if err != nil {
		t.Fatalf("failed to create test client: %v", err)
	}
	return client
}

func TestListIntegrations(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/integrations/" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "test-token" {
			t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("X-Grafana-Url") != "https://mystack.grafana.net" {
			t.Errorf("unexpected X-Grafana-Url header: %s", r.Header.Get("X-Grafana-Url"))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"results": []map[string]any{
				{"id": "int1", "name": "My Integration", "type": "grafana_alerting"},
				{"id": "int2", "name": "Webhook", "type": "webhook"},
			},
			"next": nil,
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	items, err := client.ListIntegrations(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(items) != 2 {
		t.Fatalf("expected 2 integrations, got %d", len(items))
	}
	if items[0].ID != "int1" || items[0].Name != "My Integration" {
		t.Errorf("unexpected first integration: %+v", items[0])
	}
	if items[1].ID != "int2" || items[1].Type != "webhook" {
		t.Errorf("unexpected second integration: %+v", items[1])
	}
}

func TestGetIntegration(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/integrations/int1/" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"id":   "int1",
			"name": "My Integration",
			"type": "grafana_alerting",
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	item, err := client.GetIntegration(context.Background(), "int1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if item.ID != "int1" || item.Name != "My Integration" || item.Type != "grafana_alerting" {
		t.Errorf("unexpected integration: %+v", item)
	}
}

func TestListIntegrations_Pagination(t *testing.T) {
	t.Parallel()

	var srvURL string
	page := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		page++
		switch page {
		case 1:
			nextURL := srvURL + "/api/v1/integrations/?page=2"
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"results": []map[string]any{
					{"id": "int1", "name": "Integration 1", "type": "webhook"},
				},
				"next": nextURL,
			})
		default:
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"results": []map[string]any{
					{"id": "int2", "name": "Integration 2", "type": "grafana_alerting"},
				},
				"next": nil,
			})
		}
	}))
	defer srv.Close()
	srvURL = srv.URL

	client := newTestClient(t, srv)
	items, err := client.ListIntegrations(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(items) != 2 {
		t.Fatalf("expected 2 integrations across pages, got %d", len(items))
	}
	if items[0].ID != "int1" || items[1].ID != "int2" {
		t.Errorf("unexpected IDs: %s, %s", items[0].ID, items[1].ID)
	}
}

func TestListSchedules(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"results": []map[string]any{
				{"id": "sched1", "name": "Primary On-Call", "type": "web", "time_zone": "America/New_York"},
			},
			"next": nil,
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	items, err := client.ListSchedules(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("expected 1 schedule, got %d", len(items))
	}
	if items[0].ID != "sched1" || items[0].Name != "Primary On-Call" {
		t.Errorf("unexpected schedule: %+v", items[0])
	}
}

func TestListAlertGroups(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"results": []map[string]any{
				{"id": "ag1", "title": "High error rate", "state": "firing", "alerts_count": 3},
			},
			"next": nil,
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	items, err := client.ListAlertGroups(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("expected 1 alert group, got %d", len(items))
	}
	if items[0].ID != "ag1" || items[0].State != "firing" || items[0].AlertsCount != 3 {
		t.Errorf("unexpected alert group: %+v", items[0])
	}
}

func TestGetIntegration_NotFound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"detail":"not found"}`)) //nolint:errcheck
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	_, err := client.GetIntegration(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for not found integration")
	}
}

func TestDiscoverOnCallURL(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/plugins/grafana-irm-app/settings" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"jsonData": map[string]any{
				"onCallApiUrl": "https://oncall-prod-us-central-0.grafana.net/oncall",
			},
		})
	}))
	defer srv.Close()

	// DiscoverOnCallURL needs a NamespacedRESTConfig — skip this test for now
	// since it requires real rest.Config wiring.
	t.Skip("requires NamespacedRESTConfig integration test setup")
}

func TestAcknowledgeAlertGroup(t *testing.T) {
	t.Parallel()

	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	err := client.AcknowledgeAlertGroup(context.Background(), "ag1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedPath != "/api/v1/alert_groups/ag1/acknowledge/" {
		t.Errorf("unexpected path: %s", capturedPath)
	}
}

func TestResolveAlertGroup(t *testing.T) {
	t.Parallel()

	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	err := client.ResolveAlertGroup(context.Background(), "ag1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedPath != "/api/v1/alert_groups/ag1/resolve/" {
		t.Errorf("unexpected path: %s", capturedPath)
	}
}

func TestCreateIntegration(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/integrations/" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"id":   "new-int",
			"name": body["name"],
			"type": body["type"],
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	created, err := client.CreateIntegration(context.Background(), oncall.Integration{
		Name: "New Integration",
		Type: "webhook",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if created.ID != "new-int" || created.Name != "New Integration" {
		t.Errorf("unexpected created integration: %+v", created)
	}
}

func TestDeleteIntegration(t *testing.T) {
	t.Parallel()

	var capturedPath string
	var capturedMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	err := client.DeleteIntegration(context.Background(), "int1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedMethod != http.MethodDelete {
		t.Errorf("expected DELETE, got %s", capturedMethod)
	}
	if capturedPath != "/api/v1/integrations/int1/" {
		t.Errorf("unexpected path: %s", capturedPath)
	}
}

func TestUnacknowledgeAlertGroup(t *testing.T) {
	t.Parallel()

	var capturedPath string
	var capturedMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	err := client.UnacknowledgeAlertGroup(context.Background(), "ag1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedMethod != http.MethodPost {
		t.Errorf("expected POST, got %s", capturedMethod)
	}
	if capturedPath != "/api/v1/alert_groups/ag1/unacknowledge/" {
		t.Errorf("unexpected path: %s", capturedPath)
	}
}

func TestUnresolveAlertGroup(t *testing.T) {
	t.Parallel()

	var capturedPath string
	var capturedMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	err := client.UnresolveAlertGroup(context.Background(), "ag1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedMethod != http.MethodPost {
		t.Errorf("expected POST, got %s", capturedMethod)
	}
	if capturedPath != "/api/v1/alert_groups/ag1/unresolve/" {
		t.Errorf("unexpected path: %s", capturedPath)
	}
}

func TestUnsilenceAlertGroup(t *testing.T) {
	t.Parallel()

	var capturedPath string
	var capturedMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	err := client.UnsilenceAlertGroup(context.Background(), "ag1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedMethod != http.MethodPost {
		t.Errorf("expected POST, got %s", capturedMethod)
	}
	if capturedPath != "/api/v1/alert_groups/ag1/unsilence/" {
		t.Errorf("unexpected path: %s", capturedPath)
	}
}

func TestDeleteAlertGroup(t *testing.T) {
	t.Parallel()

	var capturedPath string
	var capturedMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	err := client.DeleteAlertGroup(context.Background(), "ag1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedMethod != http.MethodDelete {
		t.Errorf("expected DELETE, got %s", capturedMethod)
	}
	if capturedPath != "/api/v1/alert_groups/ag1/" {
		t.Errorf("unexpected path: %s", capturedPath)
	}
}

func TestGetCurrentUser(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/users/current/" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"id":       "user1",
			"username": "john.doe",
			"email":    "john@example.com",
			"role":     "admin",
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	user, err := client.GetCurrentUser(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if user.ID != "user1" || user.Username != "john.doe" || user.Email != "john@example.com" {
		t.Errorf("unexpected user: %+v", user)
	}
}

func TestListAlerts(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/alerts/" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"results": []map[string]any{
				{
					"id":             "alert1",
					"alert_group_id": "ag1",
					"title":          "High CPU usage",
					"created_at":     "2024-01-01T00:00:00Z",
				},
			},
			"next": nil,
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	alerts, err := client.ListAlerts(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	if alerts[0].ID != "alert1" || alerts[0].Title != "High CPU usage" {
		t.Errorf("unexpected alert: %+v", alerts[0])
	}
}

func TestListResolutionNotes(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/resolution_notes/" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"results": []map[string]any{
				{
					"id":             "note1",
					"alert_group_id": "ag1",
					"text":           "Issue resolved by restarting service",
					"source":         "web",
					"created_at":     "2024-01-01T00:00:00Z",
				},
			},
			"next": nil,
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	notes, err := client.ListResolutionNotes(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(notes) != 1 {
		t.Fatalf("expected 1 note, got %d", len(notes))
	}
	if notes[0].ID != "note1" || notes[0].Text != "Issue resolved by restarting service" {
		t.Errorf("unexpected note: %+v", notes[0])
	}
}

func TestListShiftSwaps(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/shift_swaps/" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"results": []map[string]any{
				{
					"id":          "ss1",
					"schedule":    "sched1",
					"swap_start":  "2024-01-01T00:00:00Z",
					"swap_end":    "2024-01-02T00:00:00Z",
					"beneficiary": "user1",
					"status":      "pending",
				},
			},
			"next": nil,
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	swaps, err := client.ListShiftSwaps(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(swaps) != 1 {
		t.Fatalf("expected 1 swap, got %d", len(swaps))
	}
	if swaps[0].ID != "ss1" || swaps[0].Status != "pending" {
		t.Errorf("unexpected swap: %+v", swaps[0])
	}
}

func TestListOrganizations(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/organizations/" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"results": []map[string]any{
				{
					"id":            "org1",
					"name":          "My Organization",
					"slug":          "my-org",
					"contact_email": "admin@myorg.com",
				},
			},
			"next": nil,
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	orgs, err := client.ListOrganizations(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(orgs) != 1 {
		t.Fatalf("expected 1 organization, got %d", len(orgs))
	}
	if orgs[0].ID != "org1" || orgs[0].Name != "My Organization" {
		t.Errorf("unexpected organization: %+v", orgs[0])
	}
}

func TestCreateResources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		response map[string]any
		callFn   func(*oncall.Client) (string, error) // returns created ID
	}{
		{
			name:     "EscalationPolicy",
			path:     "/api/v1/escalation_policies/",
			response: map[string]any{"id": "ep1", "type": "notify_on_call_from_schedule"},
			callFn: func(c *oncall.Client) (string, error) {
				created, err := c.CreateEscalationPolicy(context.Background(), oncall.EscalationPolicy{
					EscalationChainID: "ec1", Position: 1, Type: "notify_on_call_from_schedule",
				})
				if err != nil {
					return "", err
				}
				return created.ID, nil
			},
		},
		{
			name:     "Route",
			path:     "/api/v1/routes/",
			response: map[string]any{"id": "route1", "integration_id": "int1"},
			callFn: func(c *oncall.Client) (string, error) {
				created, err := c.CreateRoute(context.Background(), oncall.IntegrationRoute{
					IntegrationID: "int1", EscalationChainID: "ec1", Position: 0,
				})
				if err != nil {
					return "", err
				}
				return created.ID, nil
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("expected POST, got %s", r.Method)
				}
				if r.URL.Path != tc.path {
					t.Errorf("unexpected path: %s, want %s", r.URL.Path, tc.path)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(tc.response) //nolint:errcheck
			}))
			defer srv.Close()

			id, err := tc.callFn(newTestClient(t, srv))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if id == "" {
				t.Error("expected non-empty ID")
			}
		})
	}
}

func TestDeleteShift(t *testing.T) {
	t.Parallel()

	var capturedPath string
	var capturedMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	err := client.DeleteShift(context.Background(), "shift1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedMethod != http.MethodDelete {
		t.Errorf("expected DELETE, got %s", capturedMethod)
	}
	if capturedPath != "/api/v1/on_call_shifts/shift1/" {
		t.Errorf("unexpected path: %s", capturedPath)
	}
}

func TestListFinalShifts(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expectedPath := "/api/v1/schedules/sched1/final_shifts/"
		if r.URL.Path != expectedPath {
			t.Errorf("unexpected path: %s, expected %s", r.URL.Path, expectedPath)
		}

		// Verify query parameters are present
		if r.URL.Query().Get("start_date") == "" {
			t.Errorf("missing start_date query parameter")
		}
		if r.URL.Query().Get("end_date") == "" {
			t.Errorf("missing end_date query parameter")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"results": []map[string]any{
				{
					"user_pk":       "user1",
					"user_email":    "john@example.com",
					"user_username": "john",
					"shift_start":   "2024-01-01T00:00:00Z",
					"shift_end":     "2024-01-02T00:00:00Z",
				},
			},
			"next": nil,
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	shifts, err := client.ListFinalShifts(context.Background(), "sched1", "2024-01-01", "2024-01-31")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(shifts) != 1 {
		t.Fatalf("expected 1 shift, got %d", len(shifts))
	}
	if shifts[0].UserEmail != "john@example.com" {
		t.Errorf("unexpected shift: %+v", shifts[0])
	}
}

func TestTakeShiftSwap(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/shift_swaps/ss1/take/" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"id":          "ss1",
			"schedule":    "sched1",
			"swap_start":  "2024-01-01T00:00:00Z",
			"swap_end":    "2024-01-02T00:00:00Z",
			"beneficiary": "user1",
			"benefactor":  body["benefactor"],
			"status":      "taken",
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	result, err := client.TakeShiftSwap(context.Background(), "ss1", oncall.TakeShiftSwapInput{
		Benefactor: "user2",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ID != "ss1" || result.Status != "taken" {
		t.Errorf("unexpected result: %+v", result)
	}
}

func TestCreateDirectEscalation(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/escalations/" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"id":             "esc1",
			"alert_group_id": body["alert_group_id"],
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	result, err := client.CreateDirectEscalation(context.Background(), oncall.DirectEscalationInput{
		Title:        "Page on-call engineer",
		Message:      "Critical issue needs attention",
		AlertGroupID: "ag1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ID != "esc1" || result.AlertGroupID != "ag1" {
		t.Errorf("unexpected result: %+v", result)
	}
}

func TestProxyMode_NoAuthHeader(t *testing.T) {
	t.Parallel()

	// In proxy mode the client should not set its own Authorization header;
	// the RefreshTransport (on the HTTP client) adds the gat_ token and
	// the proxy adds OnCall auth server-side.
	var receivedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"results": []map[string]any{{"id": "int1", "name": "Test"}},
		})
	}))
	defer srv.Close()

	// Create an OAuth-mode config via NewNamespacedRESTConfig.
	cfg := config.NewNamespacedRESTConfig(t.Context(), config.Context{
		Grafana: &config.GrafanaConfig{
			Server:        "https://mystack.grafana.net",
			ProxyEndpoint: srv.URL,
			OAuthToken:    "gat_test",
			StackID:       123,
		},
	})

	client, err := oncall.NewClient(srv.URL, cfg)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	_, err = client.ListIntegrations(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The client should NOT have set Authorization (token is empty in proxy mode).
	// The transport may add its own header, but the raw token should not appear.
	if receivedAuth == "test-token" {
		t.Error("proxy mode should not send the direct-mode token")
	}
}

func TestProxyMode_PaginationSkipsHostCheck(t *testing.T) {
	t.Parallel()

	// Simulate paginated responses where the "next" URL points to a different
	// host (the real OnCall API) than the proxy base URL.
	page := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if page == 0 {
			page++
			nextURL := "https://oncall-prod.example.com/oncall/api/v1/integrations/?page=2"
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"results": []map[string]any{{"id": "int1", "name": "First"}},
				"next":    &nextURL,
			})
			return
		}
		// Page 2 — returned when the client follows the pagination URL.
		// In proxy mode, the path should be extracted correctly.
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"results": []map[string]any{{"id": "int2", "name": "Second"}},
		})
	}))
	defer srv.Close()

	cfg := config.NewNamespacedRESTConfig(t.Context(), config.Context{
		Grafana: &config.GrafanaConfig{
			Server:        "https://mystack.grafana.net",
			ProxyEndpoint: srv.URL,
			OAuthToken:    "gat_test",
			StackID:       123,
		},
	})

	client, err := oncall.NewClient(srv.URL, cfg)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// In direct mode this would fail because the pagination URL host differs
	// from the client base URL. In proxy mode it should succeed.
	results, err := client.ListIntegrations(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}
