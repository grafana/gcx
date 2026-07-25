package irm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/providers/irm"
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
	items, err := client.ListAlertGroups(context.Background(), irm.WithLimit(1))
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

func TestListAlertGroups_WithStartedAfter(t *testing.T) {
	t.Parallel()

	var gotStartedAt string
	client := newTestOnCallClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotStartedAt = r.URL.Query().Get("started_at")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"results": []map[string]any{
				{"pk": "ag1", "started_at": "2025-01-15T10:00:00Z"},
			},
		})
	}))

	cutoff := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	items, err := client.ListAlertGroups(context.Background(), irm.WithStartedAfter(cutoff))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}

	if !strings.HasPrefix(gotStartedAt, "2025-01-15T00:00:00_") {
		t.Errorf("started_at = %q, want prefix %q", gotStartedAt, "2025-01-15T00:00:00_")
	}
}

func TestListAlertGroups_NoStartedAfterByDefault(t *testing.T) {
	t.Parallel()

	var gotRawQuery string
	client := newTestOnCallClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"results": []map[string]any{{"pk": "ag1"}},
		})
	}))

	_, err := client.ListAlertGroups(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotRawQuery != "" {
		t.Errorf("expected no query params, got %q", gotRawQuery)
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

func TestListEscalationStepOptions(t *testing.T) {
	t.Parallel()

	// Response shape verified against a live stack (escalation_options).
	body := `[
		{"value":0,"create_display_name":"Wait","display_name":"Wait {{wait_delay}} minute(s)","slack_integration_required":false,"can_change_importance":false},
		{"value":19,"create_display_name":"Declare Incident (valid only for non-default integration routes)","display_name":"Declare Incident with severity {{severity}} (valid only for non-default integration routes)","slack_integration_required":false,"can_change_importance":false},
		{"value":2,"create_display_name":"Escalate to all Slack channel members (use with caution)","display_name":"Escalate to all Slack channel members (use with caution)","slack_integration_required":true,"can_change_importance":false}
	]`
	client := newTestOnCallClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := irm.BasePath + "/escalation_policies/escalation_options/"
		if r.URL.Path != wantPath {
			t.Errorf("got path %q, want %q", r.URL.Path, wantPath)
		}
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body)) //nolint:errcheck
	}))

	options, err := client.ListEscalationStepOptions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(options) != 3 {
		t.Fatalf("expected 3 options, got %d", len(options))
	}
	if options[1].Value != 19 || !strings.HasPrefix(options[1].CreateDisplayName, "Declare Incident") {
		t.Errorf("unexpected option: %+v", options[1])
	}
	if !options[2].SlackIntegrationRequired {
		t.Errorf("expected slack_integration_required to decode, got %+v", options[2])
	}
}

func TestListWebhookPresets(t *testing.T) {
	t.Parallel()

	// Response shape verified against a live stack (preset_options).
	body := `[
		{"id":"grafana_assistant","name":"Grafana Assistant","description":"Investigate alert groups with Grafana Assistant.","logo":"assistant",
		 "controlled_fields":["url","http_method"],
		 "trigger_types":[{"label":"Alert Group Created","value":"1"},{"label":"Acknowledged","value":"2"}]}
	]`
	client := newTestOnCallClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := irm.BasePath + "/webhooks/preset_options/"
		if r.URL.Path != wantPath {
			t.Errorf("got path %q, want %q", r.URL.Path, wantPath)
		}
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body)) //nolint:errcheck
	}))

	presets, err := client.ListWebhookPresets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(presets) != 1 {
		t.Fatalf("expected 1 preset, got %d", len(presets))
	}
	p := presets[0]
	if p.ID != "grafana_assistant" || len(p.ControlledFields) != 2 {
		t.Errorf("unexpected preset: %+v", p)
	}
	if len(p.TriggerTypes) != 2 || p.TriggerTypes[0].Value != "1" || p.TriggerTypes[0].Label != "Alert Group Created" {
		t.Errorf("unexpected trigger types: %+v", p.TriggerTypes)
	}
}

func TestListWebhooksIntegrationFilterShapes(t *testing.T) {
	t.Parallel()

	client := newTestOnCallClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := irm.BasePath + "/webhooks/"
		if r.URL.Path != wantPath {
			t.Errorf("got path %q, want %q", r.URL.Path, wantPath)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":"webhook-1","name":"Legacy","is_webhook_enabled":true,"integration_filter":["integration-1"]},
			{"id":"webhook-2","name":"Expanded","is_webhook_enabled":true,"integration_filter":[{"id":"integration-2","name":"Grafana Alerts"}]}
		]`))
	}))

	webhooks, err := client.ListWebhooks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []irm.WebhookIntegrationFilter{{"integration-1"}, {"integration-2"}}
	if len(webhooks) != len(want) {
		t.Fatalf("got %d webhooks, want %d", len(webhooks), len(want))
	}
	for i := range webhooks {
		if !reflect.DeepEqual(webhooks[i].IntegrationFilter, want[i]) {
			t.Errorf("webhook %d integration filter: got %v, want %v", i, webhooks[i].IntegrationFilter, want[i])
		}
	}
}

func TestWebhookIntegrationFilterJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    irm.WebhookIntegrationFilter
		wantErr string
	}{
		{name: "id strings", input: `["integration-1","integration-2"]`, want: irm.WebhookIntegrationFilter{"integration-1", "integration-2"}},
		{name: "expanded objects", input: `[{"id":"integration-1","name":"Primary"}]`, want: irm.WebhookIntegrationFilter{"integration-1"}},
		{name: "mixed entries", input: `["integration-1",{"id":"integration-2","name":"Secondary"}]`, want: irm.WebhookIntegrationFilter{"integration-1", "integration-2"}},
		{name: "empty", input: `[]`, want: irm.WebhookIntegrationFilter{}},
		{name: "null", input: `null`, want: nil},
		{name: "object missing id", input: `[{"name":"Primary"}]`, wantErr: "missing id"},
		{name: "invalid entry", input: `[42]`, wantErr: "cannot unmarshal number"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got irm.WebhookIntegrationFilter
			err := json.Unmarshal([]byte(tt.input), &got)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("got error %v, want error containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
			encoded, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			var encodedIDs []string
			if err := json.Unmarshal(encoded, &encodedIDs); err != nil {
				t.Fatalf("marshaled filter should contain only IDs, got %s: %v", encoded, err)
			}
			if !reflect.DeepEqual(encodedIDs, []string(tt.want)) {
				t.Errorf("marshaled filter: got %v, want %v", encodedIDs, tt.want)
			}
		})
	}
}

func TestListRouteFilterTypes(t *testing.T) {
	t.Parallel()

	// Response shape verified against a live stack (DRF OPTIONS metadata).
	body := `{"actions":{"POST":{
		"filtering_term_type":{"choices":[
			{"display_name":"regex","value":0},
			{"display_name":"jinja2","value":1},
			{"display_name":"labels","value":2}
		],"label":"Filtering term type","type":"choice"},
		"filtering_term":{"label":"Filtering term","type":"string"}
	}}}`
	client := newTestOnCallClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := irm.BasePath + "/channel_filters/"
		if r.URL.Path != wantPath {
			t.Errorf("got path %q, want %q", r.URL.Path, wantPath)
		}
		if r.Method != http.MethodOptions {
			t.Errorf("expected OPTIONS, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body)) //nolint:errcheck
	}))

	types, err := client.ListRouteFilterTypes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []irm.RouteFilterType{
		{Value: 0, DisplayName: "regex"},
		{Value: 1, DisplayName: "jinja2"},
		{Value: 2, DisplayName: "labels"},
	}
	if len(types) != len(want) {
		t.Fatalf("expected %d filter types, got %d", len(want), len(types))
	}
	for i, ft := range types {
		if ft != want[i] {
			t.Errorf("filter type %d: got %+v, want %+v", i, ft, want[i])
		}
	}
}

func TestListWebhookTriggerOptions(t *testing.T) {
	t.Parallel()

	// Response shape verified against a live stack (webhooks/filters/):
	// a filters list whose trigger_type entry carries the full enum. The
	// preset entry's options use string values and must not break decoding.
	body := `[
		{"name":"search","type":"search"},
		{"global":true,"href":"/api/internal/v1/teams/","name":"team","type":"team_select"},
		{"name":"trigger_type","type":"options","options":[
			{"display_name":"Manual or escalation step","value":0},
			{"display_name":"Alert Group Created","value":1},
			{"display_name":"Shift Swap Taken","value":20}
		]},
		{"name":"preset","type":"options","options":[
			{"display_name":"Grafana Assistant","value":"grafana_assistant"}
		]}
	]`
	client := newTestOnCallClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := irm.BasePath + "/webhooks/filters/"
		if r.URL.Path != wantPath {
			t.Errorf("got path %q, want %q", r.URL.Path, wantPath)
		}
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body)) //nolint:errcheck
	}))

	options, err := client.ListWebhookTriggerOptions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []irm.WebhookTriggerOption{
		{Value: 0, DisplayName: "Manual or escalation step"},
		{Value: 1, DisplayName: "Alert Group Created"},
		{Value: 20, DisplayName: "Shift Swap Taken"},
	}
	if !reflect.DeepEqual(options, want) {
		t.Errorf("got %+v, want %+v", options, want)
	}
}

func TestListWebhookTriggerOptionsMissingFilter(t *testing.T) {
	t.Parallel()

	client := newTestOnCallClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"name":"search","type":"search"}]`)) //nolint:errcheck
	}))

	_, err := client.ListWebhookTriggerOptions(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no trigger_type filter") {
		t.Errorf("expected missing-filter error, got %v", err)
	}
}
