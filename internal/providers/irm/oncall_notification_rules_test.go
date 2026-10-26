package irm_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/irm"
	"k8s.io/client-go/rest"
)

func TestPersonalNotificationRulesRequests(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, method, path string }{
		{"list", http.MethodGet, ""}, {"get", http.MethodGet, "N1/"},
		{"create", http.MethodPost, ""}, {"update", http.MethodPut, "N1/"}, {"delete", http.MethodDelete, "N1/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != tc.method || r.URL.Path != irm.BasePath+"/notification_policies/"+tc.path {
					t.Errorf("unexpected request %s %s", r.Method, r.URL)
				}
				if r.Header.Get("Authorization") != "Bearer user-token" {
					t.Errorf("auth header = %q", r.Header.Get("Authorization"))
				}
				if tc.name == "list" {
					q := r.URL.Query()
					for _, key := range []string{"user", "important"} {
						if !q.Has(key) || q.Get(key) != "" {
							t.Errorf("missing empty filter %q: %s", key, r.URL.RawQuery)
						}
					}
					fmt.Fprint(w, `[{"id":"N1","user":"U1","important":true,"step":0,"notify_by":null,"wait_delay":"60.0"},{"id":"N2","user":"U2","important":false,"step":1,"notify_by":8,"wait_delay":null}]`)
					return
				}
				if tc.method == http.MethodPost || tc.method == http.MethodPut {
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if body["step"] != float64(0) || body["wait_delay"] != "60.0" || body["important"] != false || body["user"] != "U1" {
						t.Errorf("body = %#v", body)
					}
				}
				if tc.method == http.MethodDelete {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				if tc.method == http.MethodPost {
					w.WriteHeader(http.StatusCreated)
				}
				fmt.Fprint(w, `{"id":"N1","user":"U1","step":0,"notify_by":null,"wait_delay":"60.0"}`)
			}))
			defer srv.Close()
			c, err := irm.NewOnCallClient(config.NamespacedRESTConfig{Config: rest.Config{Host: srv.URL, BearerToken: "user-token"}})
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			delay := "60.0"
			rule := irm.PersonalNotificationRule{User: "U1", Step: 0, WaitDelay: &delay}
			switch tc.name {
			case "list":
				items, e := c.ListPersonalNotificationRules(ctx)
				err = e
				assertNotificationRules(t, items)
			case "get":
				_, err = c.GetPersonalNotificationRule(ctx, "N1")
			case "create":
				_, err = c.CreatePersonalNotificationRule(ctx, rule)
			case "update":
				_, err = c.UpdatePersonalNotificationRule(ctx, "N1", rule)
			case "delete":
				err = c.DeletePersonalNotificationRule(ctx, "N1")
			}
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Errorf("request count = %d", calls)
			}
		})
	}
}

func TestPersonalNotificationRulesEmptyAndForbidden(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		wantErr bool
	}{
		{"empty", 200, `[]`, false}, {"service account rejected", 403, `{"detail":"Service accounts requests are not allowed."}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := newTestOnCallClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) }))
			items, err := c.ListPersonalNotificationRules(context.Background())
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v", err)
			}
			if !tc.wantErr && (items == nil || len(items) != 0) {
				t.Errorf("empty rules = %#v", items)
			}
		})
	}
}

func assertNotificationRules(t *testing.T, items []irm.PersonalNotificationRule) {
	t.Helper()
	if len(items) != 2 || items[0].User != "U1" || !items[0].Important || items[0].NotifyBy != nil || items[1].User != "U2" || items[1].Important {
		t.Errorf("rules = %#v", items)
	}
}
