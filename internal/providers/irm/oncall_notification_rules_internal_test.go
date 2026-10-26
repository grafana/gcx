package irm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
					if r.URL.Path != BasePath+"/notification_policies/" || (!r.URL.Query().Has("user") || !r.URL.Query().Has("important") || r.URL.Query().Get("user") != "" || r.URL.Query().Get("important") != "") {
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

func TestPersonalNotificationRuleExampleManifest(t *testing.T) {
	var manifest unstructured.Unstructured
	if err := json.Unmarshal(personalNotificationRuleExample(), &manifest.Object); err != nil {
		t.Fatal(err)
	}
	if manifest.GetAPIVersion() != APIVersion || manifest.GetKind() != "PersonalNotificationRule" || manifest.GetName() == "" {
		t.Fatalf("incomplete manifest: %s", personalNotificationRuleExample())
	}
	spec, found, err := unstructured.NestedMap(manifest.Object, "spec")
	if err != nil || !found {
		t.Fatalf("manifest has no spec: %v", err)
	}
	var rule PersonalNotificationRule
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &rule); err != nil {
		t.Fatal(err)
	}
	if rule.WaitDelay == nil || *rule.WaitDelay != "300.0" {
		t.Errorf("example wait_delay = %v", rule.WaitDelay)
	}
}

func TestSharedOnCallListRejectsArguments(t *testing.T) {
	resetAgentMode(t)
	cmd := newTeamsCmd(&fakeLoader{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"list", "extra"})
	if err := cmd.ExecuteContext(context.Background()); err == nil {
		t.Fatal("list accepted an extra argument")
	}
}

func TestPersonalNotificationRuleExampleCreateAfterNotFound(t *testing.T) {
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		switch r.Method {
		case http.MethodGet:
			if r.URL.Path != BasePath+"/notification_policies/my-wait-rule/" {
				t.Errorf("GET path = %s", r.URL.Path)
			}
			w.WriteHeader(http.StatusNotFound)
		case http.MethodPost:
			if r.URL.Path != BasePath+"/notification_policies/" {
				t.Errorf("POST path = %s", r.URL.Path)
			}
			var rule PersonalNotificationRule
			if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
				t.Fatal(err)
			}
			if rule.ID != "my-wait-rule" || rule.WaitDelay == nil || *rule.WaitDelay != "300.0" {
				t.Errorf("POST body = %#v", rule)
			}
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"id":"NNEW","user":"U1","step":0,"notify_by":null,"wait_delay":"300.0"}`)
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	}))
	defer srv.Close()
	loader := &fakeLoader{client: &OnCallClient{Host: srv.URL, HTTPClient: srv.Client()}}
	for _, reg := range buildOnCallRegistrations(loader) {
		if reg.GVK.Kind != "PersonalNotificationRule" {
			continue
		}
		a, err := reg.Factory(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		var manifest unstructured.Unstructured
		if err := json.Unmarshal(reg.Example, &manifest.Object); err != nil {
			t.Fatal(err)
		}
		_, err = a.Get(t.Context(), manifest.GetName(), metav1.GetOptions{})
		if !apierrors.IsNotFound(err) {
			t.Fatalf("Get must return typed NotFound before create: %v", err)
		}
		result, err := a.Create(t.Context(), &manifest, metav1.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if result.GetName() != "NNEW" {
			t.Errorf("created name = %q", result.GetName())
		}
		if len(methods) != 2 || methods[0] != http.MethodGet || methods[1] != http.MethodPost {
			t.Errorf("requests = %v", methods)
		}
		return
	}
	t.Fatal("personal notification rule registration not found")
}
