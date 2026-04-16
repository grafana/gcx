package irm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/irm"
	"github.com/grafana/gcx/internal/providers/irm/oncalltypes"
)

func newTestOnCallClient(t *testing.T, handler http.Handler) *irm.OnCallClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &irm.OnCallClient{HTTPClient: srv.Client(), Host: srv.URL}
}

func TestDoRequestURL(t *testing.T) {
	t.Parallel()

	var gotPath string
	client := newTestOnCallClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))

	resp, err := client.DoRequest(context.Background(), http.MethodGet, "alert_receive_channels/", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	want := irm.BasePath + "/alert_receive_channels/"
	if gotPath != want {
		t.Errorf("got path %q, want %q", gotPath, want)
	}
}

func TestDoRequestNoAuthHeader(t *testing.T) {
	t.Parallel()

	var gotAuth string
	client := newTestOnCallClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))

	resp, err := client.DoRequest(context.Background(), http.MethodGet, "test/", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if gotAuth != "" {
		t.Errorf("expected no Authorization header, got %q", gotAuth)
	}
}

func TestListIntegrations(t *testing.T) {
	t.Parallel()

	client := newTestOnCallClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := irm.BasePath + "/alert_receive_channels/"
		if r.URL.Path != wantPath {
			t.Errorf("got path %q, want %q", r.URL.Path, wantPath)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"results": []map[string]any{
				{"id": "int1", "verbal_name": "My Integration", "integration": "grafana_alerting"},
				{"id": "int2", "verbal_name": "Webhook", "integration": "webhook"},
			},
		})
	}))

	items, err := client.ListIntegrations(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 integrations, got %d", len(items))
	}
	if items[0].ID != "int1" || items[0].VerbalName != "My Integration" {
		t.Errorf("unexpected first integration: %+v", items[0])
	}
}

func TestGetIntegration(t *testing.T) {
	t.Parallel()

	client := newTestOnCallClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := irm.BasePath + "/alert_receive_channels/int1/"
		if r.URL.Path != wantPath {
			t.Errorf("got path %q, want %q", r.URL.Path, wantPath)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"id": "int1", "verbal_name": "Test", "integration": "webhook",
		})
	}))

	item, err := client.GetIntegration(context.Background(), "int1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if item.ID != "int1" || item.VerbalName != "Test" {
		t.Errorf("unexpected integration: %+v", item)
	}
}

func TestPaginationExtractsPathFromBackendURL(t *testing.T) {
	t.Parallel()

	page := 0
	client := newTestOnCallClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if page == 0 {
			page++
			nextURL := "https://oncall-prod.example.com/oncall/api/internal/v1/escalation_chains/?page=2"
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"results": []map[string]any{{"id": "ec1", "name": "First"}},
				"next":    &nextURL,
			})
			return
		}
		wantPath := irm.BasePath + "/escalation_chains/"
		if r.URL.Path != wantPath {
			t.Errorf("page 2 path: got %q, want %q", r.URL.Path, wantPath)
		}
		if r.URL.Query().Get("page") != "2" {
			t.Errorf("page 2 query: got %q, want page=2", r.URL.RawQuery)
		}
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"results": []map[string]any{{"id": "ec2", "name": "Second"}},
		})
	}))

	items, err := client.ListEscalationChains(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].ID != "ec1" || items[1].ID != "ec2" {
		t.Errorf("unexpected items: %+v", items)
	}
}

func TestPaginationCursorBased(t *testing.T) {
	t.Parallel()

	page := 0
	client := newTestOnCallClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if page == 0 {
			page++
			nextURL := "https://oncall-prod.example.com/oncall/api/internal/v1/alertgroups/?cursor=abc123"
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"results": []map[string]any{{"pk": "ag1"}},
				"next":    &nextURL,
			})
			return
		}
		if r.URL.Query().Get("cursor") != "abc123" {
			t.Errorf("expected cursor=abc123, got %q", r.URL.RawQuery)
		}
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"results": []map[string]any{{"pk": "ag2"}},
		})
	}))

	items, err := client.ListAlertGroups(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
}

func TestListAlertGroups_StopsEarlyWithLimit(t *testing.T) {
	t.Parallel()

	var srvURL string
	pageHits := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		pageHits++
		w.Header().Set("Content-Type", "application/json")
		switch pageHits {
		case 1:
			nextURL := srvURL + irm.BasePath + "/alertgroups/?page=2"
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck,errchkjson
				"results": []map[string]any{
					{"pk": "ag1", "title": "Alert 1", "state": "firing", "alerts_count": 1},
					{"pk": "ag2", "title": "Alert 2", "state": "firing", "alerts_count": 2},
				},
				"next": nextURL,
			})
		default:
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck,errchkjson
				"results": []map[string]any{
					{"pk": "ag3", "title": "Alert 3", "state": "resolved", "alerts_count": 1},
				},
				"next": nil,
			})
		}
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()
	srvURL = srv.URL

	client := &irm.OnCallClient{HTTPClient: srv.Client(), Host: srv.URL}
	items, err := client.ListAlertGroups(context.Background(), oncalltypes.WithLimit(1))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("expected 1 alert group with limit=1, got %d", len(items))
	}
	if items[0].PK != "ag1" {
		t.Errorf("expected first alert group, got %s", items[0].PK)
	}
	if pageHits > 1 {
		t.Errorf("expected only 1 page fetch with limit=1, but fetched %d pages", pageHits)
	}
}

func TestAlertGroupAction(t *testing.T) {
	t.Parallel()

	var gotPath, gotMethod string
	client := newTestOnCallClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))

	err := client.AcknowledgeAlertGroup(context.Background(), "ag1")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("expected POST, got %s", gotMethod)
	}
	wantPath := irm.BasePath + "/alertgroups/ag1/acknowledge/"
	if gotPath != wantPath {
		t.Errorf("got path %q, want %q", gotPath, wantPath)
	}
}

func TestGetCurrentUser(t *testing.T) {
	t.Parallel()

	client := newTestOnCallClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := irm.BasePath + "/user/"
		if r.URL.Path != wantPath {
			t.Errorf("got path %q, want %q", r.URL.Path, wantPath)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"pk": "u1", "username": "testuser", "email": "test@example.com",
		})
	}))

	user, err := client.GetCurrentUser(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if user.PK != "u1" || user.Username != "testuser" {
		t.Errorf("unexpected user: %+v", user)
	}
}

func TestCreateDirectPaging(t *testing.T) {
	t.Parallel()

	var gotBody map[string]any
	client := newTestOnCallClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := irm.BasePath + "/direct_paging"
		if r.URL.Path != wantPath {
			t.Errorf("got path %q, want %q", r.URL.Path, wantPath)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		json.NewDecoder(r.Body).Decode(&gotBody) //nolint:errcheck
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"alert_group_id": "ag1"}) //nolint:errcheck
	}))

	input := irm.DirectPagingInput{
		Title: "Page oncall",
		Users: []irm.UserReference{{ID: "u1", Important: true}},
		Team:  "t1",
	}
	result, err := client.CreateDirectPaging(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.AlertGroupID != "ag1" {
		t.Errorf("unexpected result: %+v", result)
	}

	users, ok := gotBody["users"].([]any)
	if !ok || len(users) != 1 {
		t.Fatalf("expected users array with 1 item, got %v", gotBody["users"])
	}
	userRef, ok := users[0].(map[string]any)
	if !ok {
		t.Fatalf("expected user reference to be map, got %T", users[0])
	}
	if userRef["id"] != "u1" || userRef["important"] != true {
		t.Errorf("unexpected user reference: %v", userRef)
	}
}

func TestExtractNextPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		rawURL string
		want   string
	}{
		{
			name:   "page-based with oncall prefix",
			rawURL: "https://oncall-prod.example.com/oncall/api/internal/v1/alert_receive_channels/?page=2",
			want:   "alert_receive_channels/?page=2",
		},
		{
			name:   "cursor-based",
			rawURL: "https://oncall-prod.example.com/oncall/api/internal/v1/alertgroups/?cursor=abc123",
			want:   "alertgroups/?cursor=abc123",
		},
		{
			name:   "no query string",
			rawURL: "https://oncall-prod.example.com/oncall/api/internal/v1/teams/",
			want:   "teams/",
		},
		{
			name:   "no oncall prefix",
			rawURL: "https://oncall-prod.example.com/api/internal/v1/teams/?page=3",
			want:   "teams/?page=3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := irm.ExtractNextPath(tt.rawURL)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got = strings.TrimPrefix(got, "/")
			want := strings.TrimPrefix(tt.want, "/")
			if got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}
