package errortracking_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const twoProjects = `{"projects":[
 {"id":314,"name":"Checkout API","ingestKey":"k314","dsn":"https://k314@ingest.example.com/314","stats":{"unresolvedErrors":3,"eventsLast24h":40,"dailyEvents":[]}},
 {"id":5,"name":"api-2","ingestKey":"k5","stats":{"unresolvedErrors":0,"eventsLast24h":0,"dailyEvents":[]}},
 {"id":2,"name":"web","ingestKey":"k2","stats":{"unresolvedErrors":0,"eventsLast24h":0,"dailyEvents":[]}}]}`

func TestProjects(t *testing.T) {
	f := newFakeET(t)
	f.reply("GET projects", 200, twoProjects)
	f.reply("GET projects/314", 200, `{"id":314,"name":"Checkout API","ingestKey":"k314","dsn":"https://k314@ingest.example.com/314","stats":{"dailyEvents":[]}}`)

	f.runCases(t, []cliCase{
		{name: "list as JSON has no ingest keys or DSNs", args: []string{"projects", "list", "-o", "json"},
			calls: []string{"GET projects"}, query: map[string][]string{}, hasNot: []string{"k314", "ingestKey", "dsn"}, check: func(t *testing.T, out string, _ error, _ []etCall) {
				t.Helper()
				var list struct {
					Items []struct {
						APIVersion string `json:"apiVersion"`
						Kind       string `json:"kind"`
						Metadata   struct {
							Name      string `json:"name"`
							Namespace string `json:"namespace"`
						} `json:"metadata"`
					} `json:"items"`
					ListMeta any `json:"list_meta"`
				}
				require.NoError(t, json.Unmarshal([]byte(out), &list))
				require.Len(t, list.Items, 3)
				assert.Equal(t, "error-tracking.ext.grafana.app/v0alpha1", list.Items[0].APIVersion)
				assert.Equal(t, "Project", list.Items[0].Kind)
				assert.Equal(t, "checkout-api-314", list.Items[0].Metadata.Name)
				assert.Equal(t, "stacks-123", list.Items[0].Metadata.Namespace)
				assert.Nil(t, list.ListMeta, "a complete list has no list_meta")
			}},
		{name: "the table leads with the composite NAME and has no ID column", args: []string{"projects", "list", "-o", "table"},
			calls: []string{"GET projects"}, has: []string{"NAME", "checkout-api-314"}, hasNot: []string{"ID", "INGEST KEY", "k314", "Checkout API"},
			check: func(t *testing.T, out string, _ error, _ []etCall) {
				t.Helper()
				assert.True(t, strings.HasPrefix(out, "NAME "), "NAME is the first column: %q", out)
			}},
		{name: "a list in YAML has no ingest keys either", args: []string{"projects", "list", "-o", "yaml"},
			calls: []string{"GET projects"}, hasNot: []string{"k314", "ingestKey", "dsn"}},
		{name: "wide shows neither", args: []string{"projects", "list", "-o", "wide"},
			calls: []string{"GET projects"}, has: []string{"REPO", "CREATED"}, hasNot: []string{"INGEST KEY", "k314", "DSN"}},
		{name: "get's table shows no ingest key", args: []string{"projects", "get", "314", "-o", "table"},
			calls: []string{"GET projects/314"}, has: []string{"NAME", "checkout-api-314"}, hasNot: []string{"ID", "INGEST KEY", "k314"}},
		{name: "get's wide table shows the DSN", args: []string{"projects", "get", "314", "-o", "wide"},
			calls: []string{"GET projects/314"}, has: []string{"DSN", "https://k314@ingest.example.com/314"}, hasNot: []string{"INGEST KEY"}},
		{name: "get's JSON has the ingest key and the DSN", args: []string{"projects", "get", "314", "--json", "spec.ingestKey,spec.dsn"},
			calls: []string{"GET projects/314"}, json: `{"spec.ingestKey":"k314","spec.dsn":"https://k314@ingest.example.com/314"}`},
		{name: "--search keeps names that contain the text, any case", args: []string{"projects", "list", "--search", "CHECK", "--json", "spec.id"},
			calls: []string{"GET projects"}, query: map[string][]string{}, json: `{"items":[{"spec.id":314}]}`},
		{name: "there is no --name alias", args: []string{"projects", "list", "--name", "api"}, exit: gcxerrors.ExitUsageError, err: "unknown flag: --name"},
		{name: "no match says so on stderr", args: []string{"projects", "list", "--search", "nope", "-o", "json"},
			calls: []string{"GET projects"}, json: `{"items":[]}`, stderr: `hint: No project name contains "nope"; see all: gcx error-tracking projects list`},
		{name: "no projects at all", setup: func(f *fakeET) { f.reply("GET projects", 200, `{"projects":[]}`) }, args: []string{"projects", "list"},
			calls: []string{"GET projects"}, stderr: "hint: No Error Tracking projects yet; create one: gcx error-tracking projects create --name <name>",
			check: func(t *testing.T, _ string, _ error, _ []etCall) {
				t.Helper()
				f.reply("GET projects", 200, twoProjects)
			}},
		{name: "get by ID keeps the name-id as metadata.name", args: []string{"projects", "get", "314", "-o", "json"},
			calls: []string{"GET projects/314"}, has: []string{`"name": "checkout-api-314"`}},
	})
}

func TestForbidden(t *testing.T) {
	f := newFakeET(t)
	f.reply("GET projects", http.StatusForbidden, `{"error":{"code":"forbidden","message":"You cannot read projects in this stack."}}`)
	_, err := f.run("projects", "list", "-o", "json")
	out := rendered(t, err)
	assert.Contains(t, out, gcxerrors.SummaryAuthorizationFailed)
	assert.Contains(t, out, "You cannot read projects in this stack.")
	assert.Contains(t, out, "Check your roles and access-policy scopes")
	assert.Equal(t, gcxerrors.ExitAuthFailure, exitCode(err))
}

func TestErrorTrackingNotAvailable(t *testing.T) {
	f := newFakeET(t)
	const k8s404 = `{"kind":"Status","apiVersion":"v1","status":"Failure","message":"the server could not find the requested resource","reason":"NotFound","code":404}`
	const notEnabled = "Error Tracking isn't enabled for this stack.\nAsk your Grafana Labs account team to enable it."
	const unknownRoute = "Error Tracking on this stack serves error-tracking.ext.grafana.app/v0alpha1 but not GET projects/314 (HTTP 404).\nThis gcx and the stack's Error Tracking API may be different versions."
	const discovery = "GET /apis/error-tracking.ext.grafana.app/v0alpha1"
	for _, tc := range []struct {
		name, body, summary, details string
		status, discovery            int
	}{
		{"ET 404", `{"error":{"code":"not_found","message":"project 314 not found"}}`, gcxerrors.SummaryResourceNotFound, "", http.StatusNotFound, 0},
		{"ET 503", `{"error":{"code":"unavailable","message":"try later"}}`, gcxerrors.SummaryAPIError, "", http.StatusServiceUnavailable, 0},
		{"k8s 404", k8s404, gcxerrors.SummaryEndpointNotAvailable, notEnabled, http.StatusNotFound, http.StatusNotFound},
		{"plain 404", "404 page not found", gcxerrors.SummaryEndpointNotAvailable, notEnabled, http.StatusNotFound, http.StatusNotFound},
		{"a route the served API lacks is not a disabled stack", k8s404, gcxerrors.SummaryEndpointNotAvailable, unknownRoute, http.StatusNotFound, http.StatusOK},
		{"an unreadable API version keeps the plain 404", "404 page not found", gcxerrors.SummaryResourceNotFound, "", http.StatusNotFound, http.StatusForbidden},
		{"a 503 without an Error Tracking body is a server error", "no healthy upstream", gcxerrors.SummaryAPIError, "", http.StatusServiceUnavailable, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f.reply("GET projects/314", tc.status, tc.body)
			if tc.discovery != 0 {
				f.reply(discovery, tc.discovery, `{"kind":"APIResourceList","groupVersion":"error-tracking.ext.grafana.app/v0alpha1","resources":[]}`)
			}
			_, err := f.run("projects", "get", "314", "-o", "json")
			d := fail.ErrorToDetailedError(err)
			require.NotNil(t, d)
			var out bytes.Buffer
			require.NoError(t, d.WriteJSON(&out, exitCode(err)))
			var got struct {
				Error struct {
					Summary  string `json:"summary"`
					ExitCode int    `json:"exitCode"`
					Details  string `json:"details"`
				} `json:"error"`
			}
			require.NoError(t, json.Unmarshal(out.Bytes(), &got))
			assert.Equal(t, tc.summary, got.Error.Summary)
			assert.Equal(t, gcxerrors.ExitGeneralError, got.Error.ExitCode)
			if tc.details != "" {
				assert.Equal(t, tc.details, got.Error.Details)
			} else {
				assert.NotContains(t, got.Error.Details, "not available")
			}
			want := []string{"GET projects/314"}
			if tc.status == http.StatusServiceUnavailable {
				want = []string{"GET projects/314", "GET projects/314", "GET projects/314", "GET projects/314"}
			}
			if tc.discovery != 0 {
				want = append(want, discovery)
			}
			assert.Equal(t, want, routesOf(f.takeCalls()), "reads retry a 503 and never a 404; a 404 without an Error Tracking body asks once whether the API is served")
		})
	}
}

func TestErrorTextDropsControlCharacters(t *testing.T) {
	f := newFakeET(t)
	for _, body := range []string{
		`{"error":{"code":"bad","message":"no\u001b[31m\u009b2J"}}`,
		`{"message":"no","traceID":"t\u001b]0;x\u0007"}`,
		`{"err":"no\u001b[2J\u202e"}`,
	} {
		t.Run(body, func(t *testing.T) {
			f.reply("GET projects", http.StatusBadRequest, body)
			_, err := f.run("projects", "list", "-o", "json")
			out := rendered(t, err)
			assert.NotContains(t, out, "\x1b")
			assert.NotContains(t, out, "\u009b")
			assert.NotContains(t, out, "\u202e")
			assert.Contains(t, out, "no")
		})
	}
	f.takeCalls()
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if emitted, ok := errors.AsType[*gcxerrors.EmittedError](err); ok {
		return emitted.Code
	}
	if d := fail.ErrorToDetailedError(err); d != nil && d.ExitCode != nil {
		return *d.ExitCode
	}
	return 1
}
