package irm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNotificationRuleCommands(t *testing.T) {
	resetAgentMode(t)
	for _, tc := range []struct {
		name  string
		args  []string
		calls int
	}{
		{"list", []string{"list", "-o", "json"}, 1},
		{"get", []string{"get", "N1", "-o", "json"}, 1},
		{"reject list args", []string{"list", "extra", "-o", "json"}, 0},
		{"reject missing ID", []string{"get", "-o", "json"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				switch tc.name {
				case "list":
					if r.URL.Path != BasePath+"/notification_policies/" || r.URL.RawQuery != "user=&important=" {
						t.Errorf("URL = %s", r.URL)
					}
					fmt.Fprint(w, `[{"id":"N1","user":"U1","important":true,"step":1,"notify_by":8,"wait_delay":null}]`)
				case "get":
					if r.URL.Path != BasePath+"/notification_policies/N1/" {
						t.Errorf("URL = %s", r.URL)
					}
					fmt.Fprint(w, `{"id":"N1","user":"U1","important":true,"step":1,"notify_by":8,"wait_delay":null}`)
				default:
					t.Error("invalid input caused an API request")
				}
			}))
			defer srv.Close()
			c := &OnCallClient{Host: srv.URL, HTTPClient: srv.Client()}
			cmd := newPersonalNotificationRulesCmd(&fakeLoader{client: c})
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs(tc.args)
			err := cmd.ExecuteContext(context.Background())
			if (err != nil) != (tc.calls == 0) {
				t.Fatalf("error = %v, output = %s", err, out.String())
			}
			if calls != tc.calls {
				t.Errorf("API calls = %d, want %d", calls, tc.calls)
			}
			if tc.calls > 0 {
				var v any
				if err := json.Unmarshal(out.Bytes(), &v); err != nil {
					t.Fatal(err)
				}
				if !bytes.Contains(out.Bytes(), []byte(`"N1"`)) || (tc.name == "list" && !bytes.Contains(out.Bytes(), []byte(`"PersonalNotificationRule"`))) {
					t.Errorf("output = %s", out.String())
				}
			}
		})
	}
}
