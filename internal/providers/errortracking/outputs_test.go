package errortracking_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers/errortracking"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

type outputCase struct {
	name    string
	args    []string
	calls   []string
	query   map[string]string
	body    string
	json    string
	exact   bool
	table   []string
	narrow  []string
	wide    []string
	card    bool
	text    string
	sel     []string
	jq      []string
	noTable bool
}

func assertJSONSubset(t *testing.T, want, got any, path string) {
	t.Helper()
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		require.True(t, ok, "%s: want an object, got %v", path, got)
		for k, v := range w {
			gv, ok := g[k]
			require.True(t, ok, "%s.%s is missing", path, k)
			assertJSONSubset(t, v, gv, path+"."+k)
		}
	case []any:
		g, ok := got.([]any)
		require.True(t, ok, "%s: want an array, got %v", path, got)
		require.Len(t, g, len(w), path)
		for i := range w {
			assertJSONSubset(t, w[i], g[i], path)
		}
	default:
		assert.Equal(t, want, got, path)
	}
}

func assertOutputJSON(t *testing.T, tc outputCase, out []byte) {
	t.Helper()
	if tc.exact {
		assert.JSONEq(t, tc.json, string(out))
		return
	}
	var want, got any
	require.NoError(t, json.Unmarshal([]byte(tc.json), &want))
	require.NoError(t, json.Unmarshal(out, &got), "%s", out)
	assertJSONSubset(t, want, got, "$")
}

func outputFake(t *testing.T) *fakeET {
	t.Helper()
	f := newFakeET(t)
	for route, body := range map[string]string{
		"GET projects":                               `{"projects":[{"id":314,"name":"Checkout API","ingestKey":"k314","dsn":"https://k314@ingest.example.com/314","createdAt":"2026-11-01T09:30:00Z","stats":{"unresolvedErrors":3,"eventsLast24h":40,"lastEventAt":"2026-11-17T08:15:42Z","dailyEvents":[]}}]}`,
		"GET projects/314":                           `{"id":314,"name":"Checkout API","ingestKey":"k314","dsn":"https://k314@ingest.example.com/314","previousIngestKeyUntil":"2099-11-18T09:00:00Z","stats":{"unresolvedErrors":3,"eventsLast24h":40,"dailyEvents":[]}}`,
		"GET projects/314/timeline":                  `{"bucket":"1h","buckets":[{"bucket":"2026-11-17T08:00:00Z","eventCount":4}]}`,
		"GET /api/user":                              `{"uid":"u1","login":"alex","name":"Alex"}`,
		"GET projects/314/errors":                    `{"errors":[{"id":42,"status":"unresolved","regressed":true,"exceptionType":"TypeError","message":"boom","culprit":"cart.js","transaction":"/checkout","eventCount":9,"firstSeen":"2026-11-10T10:00:00.000Z","lastSeen":"2026-11-17T08:15:42.000Z","lastRelease":"v1.2.3"}],"total":1}`,
		"GET projects/314/errors/42":                 `{"id":42,"projectId":314,"projectName":"Checkout API","status":"unresolved","exceptionType":"TypeError","message":"boom","eventCount":9,"lastSeen":"2026-11-17T08:15:42.000Z","latestEvent":{"id":7,"eventId":"e7","occurredAt":"2026-11-17T08:15:42.000Z","payload":{"level":"error"}},"history":[],"commentCount":1}`,
		"GET projects/314/errors/42/timeline":        `{"bucket":"1h","buckets":[{"bucket":"2026-11-17T08:00:00Z","eventCount":2}]}`,
		"GET projects/314/errors/42/customFields":    `{"customFields":[{"key":"browser","valueCount":1,"eventCount":9,"saturated":false,"values":[{"value":"Chrome","eventCount":9}]}]}`,
		"GET projects/314/errors/42/activity":        `{"activity":[{"id":"comment:7","type":"comment","createdAt":"2026-11-17T08:20:00Z","actor":"u1","actorName":"Alex","comment":{"id":7,"body":"Looking into it.","canEdit":true}}]}`,
		"GET projects/314/errors/42/assignees":       `{"assignees":[{"userUid":"u1","name":"Alex","login":"alex","avatarUrl":"","assignedBy":"u2","assignedAt":"2026-11-17T08:30:00Z"}],"canAssign":true}`,
		"GET projects/314/errors/42/events":          `{"events":[{"id":7,"eventId":"e7","occurredAt":"2026-11-17T08:15:42.000Z","environment":"prod","release":"v1.2.3","level":"error","transaction":"/checkout","serverName":"web-1","traceId":"t1"}]}`,
		"GET projects/314/errors/42/events/e7":       `{"id":7,"eventId":"e7","occurredAt":"2026-11-17T08:15:42.000Z","environment":"prod","release":"v1.2.3","payload":{"level":"error"}}`,
		"GET projects/314/rawEvents":                 `{"rawEvents":[{"id":9,"eventId":"e7","receivedAt":"2026-11-17T08:15:43Z","outcome":"stored","size":512}]}`,
		"GET projects/314/rawEvents/9":               `{"id":9,"eventId":"e7","receivedAt":"2026-11-17T08:15:43Z","outcome":"stored","size":512,"payload":{"level":"error"}}`,
		"GET projects/314/releases":                  `{"releases":[{"release":"v1.2.3","firstSeen":"2026-11-10T10:00:00Z","lastSeen":"2026-11-17T08:15:42Z","eventCount":9}]}`,
		"GET projects/314/environments":              `{"environments":[{"environment":"prod","firstSeen":"2026-11-10T10:00:00Z","lastSeen":"2026-11-17T08:15:42Z","eventCount":9}]}`,
		"GET projects/314/transactions":              `{"transactions":[{"transaction":"/checkout","errorCount":1,"eventCount":9}]}`,
		"GET projects/314/customFields":              `{"keys":[{"key":"browser","valueCount":1,"errorCount":1,"eventCount":9}]}`,
		"GET projects/314/customFields/values":       `{"key":"browser","values":[{"value":"Chrome","errorCount":1,"eventCount":9,"firstSeen":"2026-11-10T10:00:00Z","lastSeen":"2026-11-17T08:15:42Z"}]}`,
		"GET projects/314/users":                     `{"users":[{"uid":"u1","name":"Alex","login":"alex","avatarUrl":""}],"totalCount":1}`,
		"POST projects":                              `{"id":314,"name":"Checkout API","stats":{"dailyEvents":[]}}`,
		"PATCH projects/314":                         `{"id":314,"name":"Checkout API","stats":{"dailyEvents":[]}}`,
		"DELETE projects/314":                        `{"status":"deleted"}`,
		"POST projects/314/ingestKey/rotate":         `{"id":314,"name":"Checkout API","ingestKey":"k-new","dsn":"https://k-new@ingest.example.com/314","stats":{"dailyEvents":[]}}`,
		"POST projects/314/ingestKey/revokePrevious": `{"id":314,"name":"Checkout API","ingestKey":"k-new","stats":{"dailyEvents":[]}}`,
		"PATCH projects/314/errors/42":               `{"id":42,"status":"resolved","history":[]}`,
		"POST projects/314/errors/42/review":         `{"id":42,"status":"unresolved","reviewed":true,"history":[]}`,
		"POST projects/314/errors/42/comments":       `{"id":"comment:7","type":"comment","createdAt":"2026-11-17T08:20:00Z","actor":"u1","comment":{"id":7,"body":"x","canEdit":true}}`,
		"PATCH projects/314/errors/42/comments/7":    `{"id":"comment:7","type":"comment","createdAt":"2026-11-17T08:20:00Z","actor":"u1","comment":{"id":7,"body":"x","canEdit":true}}`,
		"DELETE projects/314/errors/42/comments/7":   `{"status":"deleted"}`,
		"PUT projects/314/errors/42/assignees/u1":    `{"userUid":"u1","name":"Alex"}`,
	} {
		f.reply(route, http.StatusOK, body)
	}
	f.reply("DELETE projects/314/errors/42/assignees/u1", http.StatusNoContent, ``)
	return f
}

func outputCases() []outputCase {
	const ns = `"namespace":"stacks-123"`
	const project = `"project":{"id":314,"name":"Checkout API"}`
	mutation := func(action, target, rest string) string {
		return `{"type":"gcx.mutation","schema_version":"1","action":"` + action + `","changed":true,"target":{` + target + `,` + ns + `}` + rest + `}`
	}
	const commentRest = `,"project":{"id":314},"errorId":"42"`
	p := []string{"--project", "314"}
	with := func(args ...string) []string { return append(args, p...) }
	return []outputCase{
		{name: "projects list", calls: []string{"GET projects"}, args: []string{"projects", "list"},
			exact:  true,
			json:   `{"items":[{"kind":"Project","apiVersion":"error-tracking.ext.grafana.app/v0alpha1","metadata":{"name":"checkout-api-314",` + ns + `},"spec":{"id":314,"name":"Checkout API","description":"","sourceRepoUrl":"","createdAt":"2026-11-01T09:30:00Z","stats":{"unresolvedErrors":3,"eventsLast24h":40,"lastEventAt":"2026-11-17T08:15:42Z","dailyEvents":[]}}}]}`,
			narrow: []string{" ago"}, table: []string{"NAME", "UNRESOLVED", "EVENTS 24H", "LAST EVENT", "checkout-api-314"}, wide: []string{"CREATED", "2026-11-01 09:30:00Z", "2026-11-17 08:15:42Z"},
			jq: []string{".items[0].spec.id", "314"}},
		{name: "projects get", calls: []string{"GET projects/314"}, args: []string{"projects", "get", "314"},
			json:  `{"apiVersion":"error-tracking.ext.grafana.app/v0alpha1","kind":"Project","metadata":{"name":"checkout-api-314",` + ns + `},"spec":{"id":314,"ingestKey":"k314","dsn":"https://k314@ingest.example.com/314"}}`,
			table: []string{"NAME", "checkout-api-314"}, wide: []string{"DSN", "https://k314@ingest.example.com/314"}, jq: []string{".spec.dsn", `"https://k314@ingest.example.com/314"`}},
		{name: "projects use", calls: []string{"GET projects/314"}, args: []string{"projects", "use", "314"}, noTable: true,
			json: `{"type":"gcx.mutation","schema_version":"1","action":"use","target":{"kind":"Project","id":"314","namespace":"stacks-123"},` + project + `,"stack":"test"}`, text: "Default project for stack test: 314 (Checkout API)"},
		{name: "projects timeline", calls: []string{"GET projects/314/timeline"}, query: map[string]string{"from": "*", "to": "*"}, args: []string{"projects", "timeline", "314"}, exact: true,
			json:  `{"bucket":"1h","buckets":[{"bucket":"2026-11-17T08:00:00Z","eventCount":4}]}`,
			table: []string{"BUCKET", "EVENTS", "2026-11-17 08:00Z", "4"}, jq: []string{".buckets[0].eventCount", "4"}},
		{name: "projects create", calls: []string{"POST projects"}, body: `{"name":"Checkout API"}`, args: []string{"projects", "create", "--name", "Checkout API"}, exact: true, noTable: true,
			json: mutation("created", `"kind":"Project","id":"314","name":"Checkout API"`, ""), text: "Created project Checkout API (ID 314)"},
		{name: "projects update", calls: []string{"PATCH projects/314"}, body: `{"description":"x"}`, args: []string{"projects", "update", "314", "--description", "x"}, exact: true, noTable: true,
			json: mutation("updated", `"kind":"Project","id":"314","name":"Checkout API"`, ""), text: "Changed the description of project Checkout API (ID 314)"},
		{name: "projects delete", calls: []string{"GET projects/314", "DELETE projects/314"}, args: []string{"projects", "delete", "314", "--force"}, noTable: true,
			json: mutation("deleted", `"kind":"Project","id":"314","name":"Checkout API"`, ""), text: "Deleted project Checkout API (ID 314)"},
		{name: "projects ingest-keys rotate", calls: []string{"POST projects/314/ingestKey/rotate"}, args: []string{"projects", "ingest-keys", "rotate", "314", "--force"}, exact: true, noTable: true,
			json: mutation("rotated", `"kind":"IngestKey"`, `,`+project),
			text: "Rotated the ingest key of project Checkout API (ID 314); the old key stopped working at once"},
		{name: "projects ingest-keys delete-previous", calls: []string{"GET projects/314", "POST projects/314/ingestKey/revokePrevious"}, args: []string{"projects", "ingest-keys", "delete-previous", "314", "--force"}, exact: true, noTable: true,
			json: mutation("deleted", `"kind":"IngestKey"`, `,`+project), text: "Stopped the previous ingest key of project Checkout API (ID 314)"},
		{name: "errors list", calls: []string{"GET projects/314/errors"}, query: map[string]string{"limit": "50", "sort": "lastSeen", "status": "unresolved"}, args: with("errors", "list"),
			json:   `{"items":[{"id":42,"status":"unresolved","regressed":true,"reviewed":false,"exceptionType":"TypeError","eventCount":9}]}`,
			narrow: []string{" ago"}, table: []string{"ID", "STATUS", "TYPE", "MESSAGE", "EVENTS", "LAST SEEN", "RELEASE", "42", "regressed", "TypeError", "boom", "v1.2.3"},
			wide: []string{"FIRST SEEN", "2026-11-10 10:00:00Z", "2026-11-17 08:15:42Z", "IGNORED UNTIL", "CULPRIT", "cart.js", "TRANSACTION", "/checkout"},
			sel:  []string{"id,status", `{"items":[{"id":42,"status":"unresolved"}]}`}, jq: []string{".items[0].id", "42"}},
		{name: "errors get", calls: []string{"GET projects/314/errors/42"}, args: with("errors", "get", "42"),
			json:   `{"id":42,"latestEvent":{"eventId":"e7","payload":{"level":"error"}},"commentCount":1}`,
			narrow: []string{" ago"}, table: []string{"ID", "LATEST EVENT", "42"}, wide: []string{"CULPRIT", "2026-11-17 08:15:42Z"},
			sel: []string{"id,commentCount", `{"id":42,"commentCount":1}`}, jq: []string{".latestEvent.eventId", `"e7"`}},
		{name: "errors inspect", calls: []string{"GET projects/314/errors/42", "GET projects/314/errors/42/activity", "GET projects/314/errors/42/assignees"}, card: true, sel: []string{"id,assignees", `{"id":42,"assignees":[{"userUid":"u1","name":"Alex","login":"alex","avatarUrl":"","assignedBy":"u2","assignedAt":"2026-11-17T08:30:00Z"}]}`}, args: with("errors", "inspect", "42"), text: "Error 42 · unresolved · project Checkout API (ID 314)",
			json: `{"id":42,"projectName":"Checkout API","recentActivity":[{"id":"comment:7"}],"assignees":[{"userUid":"u1"}]}`},
		{name: "errors events list", calls: []string{"GET projects/314/errors/42/events"}, query: map[string]string{"limit": "50"}, args: with("errors", "events", "list", "42"),
			json:   `{"items":[{"id":7,"eventId":"e7","environment":"prod","release":"v1.2.3","level":"error"}]}`,
			narrow: []string{" ago"}, table: []string{"EVENT ID", "OCCURRED", "ENVIRONMENT", "RELEASE", "LEVEL", "e7", "prod"},
			wide: []string{"TRANSACTION", "/checkout", "SERVER", "web-1", "TRACE ID", "t1", "2026-11-17 08:15:42Z"}, sel: []string{"eventId", `{"items":[{"eventId":"e7"}]}`}},
		{name: "errors timeline", calls: []string{"GET projects/314/errors/42/timeline"}, query: map[string]string{"from": "*", "to": "*"}, args: with("errors", "timeline", "42"), exact: true,
			json:  `{"bucket":"1h","buckets":[{"bucket":"2026-11-17T08:00:00Z","eventCount":2}]}`,
			table: []string{"BUCKET", "2026-11-17 08:00Z", "2"}, jq: []string{".bucket", `"1h"`}},
		{name: "errors fields list", calls: []string{"GET projects/314/errors/42/customFields"}, args: with("errors", "fields", "list", "42"), exact: true,
			json:  `{"items":[{"key":"browser","valueCount":1,"eventCount":9,"saturated":false,"values":[{"value":"Chrome","eventCount":9,"firstSeen":"","lastSeen":""}]}]}`,
			table: []string{"KEY", "VALUES", "SATURATED", "TOP VALUES", "browser", "Chrome"}, sel: []string{"key", `{"items":[{"key":"browser"}]}`}},
		{name: "errors activity list", calls: []string{"GET projects/314/errors/42/activity"}, query: map[string]string{"limit": "50"}, args: with("errors", "activity", "list", "42"), exact: true,
			json:   `{"items":[{"id":"comment:7","type":"comment","createdAt":"2026-11-17T08:20:00Z","actor":"u1","actorName":"Alex","comment":{"id":7,"body":"Looking into it.","canEdit":true}}]}`,
			narrow: []string{" ago"}, table: []string{"COMMENT ID", "TIME", "ACTOR", "WHAT", "7", "Alex u1", "comment: Looking into it."}, wide: []string{"2026-11-17 08:20:00Z"},
			sel: []string{"type", `{"items":[{"type":"comment"}]}`}},
		{name: "errors assignees list", calls: []string{"GET projects/314/errors/42/assignees"}, args: with("errors", "assignees", "list", "42"), exact: true,
			json:   `{"items":[{"userUid":"u1","name":"Alex","login":"alex","avatarUrl":"","assignedBy":"u2","assignedAt":"2026-11-17T08:30:00Z"}],"canAssign":true}`,
			narrow: []string{" ago"}, table: []string{"NAME", "LOGIN", "UID", "ASSIGNED AT", "Alex", "u1"}, wide: []string{"2026-11-17 08:30:00Z"},
			sel: []string{"userUid", `{"items":[{"userUid":"u1"}],"canAssign":true}`}},
		{name: "errors update", calls: []string{"GET projects/314/errors/42", "PATCH projects/314/errors/42"}, body: `{"status":"resolved"}`, args: with("errors", "update", "42", "--status", "resolved"), exact: true, noTable: true,
			json: `{"type":"gcx.mutation_batch","schema_version":"1","action":"updated","summary":{"succeeded":1,"failed":0},"failures":[],` + project + `,"results":[]}`,
			text: "Resolved 1 of 1 errors in project Checkout API (ID 314)"},
		{name: "errors update --reviewed", calls: []string{"GET projects/314/errors/42", "POST projects/314/errors/42/review"}, args: with("errors", "update", "42", "--reviewed"), exact: true, noTable: true,
			json: `{"type":"gcx.mutation_batch","schema_version":"1","action":"reviewed","summary":{"succeeded":1,"failed":0},"failures":[],` + project + `,"results":[]}`,
			text: "Marked 1 of 1 errors in project Checkout API (ID 314) reviewed"},
		{name: "errors comments create", calls: []string{"POST projects/314/errors/42/comments"}, body: `{"body":"x"}`, args: with("errors", "comments", "create", "42", "--body", "x"), exact: true, noTable: true,
			json: mutation("created", `"kind":"Comment","id":"7"`, commentRest), text: "Created comment 7 on error 42 in project 314"},
		{name: "errors comments update", calls: []string{"PATCH projects/314/errors/42/comments/7"}, body: `{"body":"x"}`, args: with("errors", "comments", "update", "42", "7", "--body", "x"), exact: true, noTable: true,
			json: mutation("updated", `"kind":"Comment","id":"7"`, commentRest), text: "Edited comment 7 on error 42 in project 314"},
		{name: "errors comments delete", calls: []string{"DELETE projects/314/errors/42/comments/7"}, args: with("errors", "comments", "delete", "42", "7", "--force"), exact: true, noTable: true,
			json: mutation("deleted", `"kind":"Comment","id":"7"`, commentRest), text: "Deleted comment 7 on error 42 in project 314"},
		{name: "errors assignees add", calls: []string{"GET projects/314/errors/42", "GET /api/user", "GET projects/314/errors/42/assignees"}, args: with("errors", "assignees", "add", "42", "me"), exact: true, noTable: true,
			json: strings.Replace(mutation("assigned", `"kind":"Error","id":"42"`, `,`+project+`,"user":"u1"`), `"changed":true`, `"changed":false`, 1), text: "Alex (alex), UID u1 was already assigned to error 42 in project Checkout API (ID 314); nothing changed"},
		{name: "errors assignees delete", calls: []string{"GET projects/314/errors/42", "GET /api/user", "GET projects/314/errors/42/assignees", "DELETE projects/314/errors/42/assignees/u1"}, args: with("errors", "assignees", "delete", "42", "me"), exact: true, noTable: true,
			json: mutation("unassigned", `"kind":"Error","id":"42"`, `,`+project+`,"user":"u1"`), text: "Unassigned Alex (alex), UID u1 from error 42 in project Checkout API (ID 314)"},
		{name: "errors events get", calls: []string{"GET projects/314/errors/42/events/e7"}, args: with("errors", "events", "get", "42", "e7"),
			json:  `{"eventId":"e7","payload":{"level":"error"}}`,
			table: []string{"EVENT ID", "e7", "prod"}, wide: []string{"TRACE ID"}, sel: []string{"eventId,release", `{"eventId":"e7","release":"v1.2.3"}`}},
		{name: "raw-events list", calls: []string{"GET projects/314/rawEvents"}, query: map[string]string{"limit": "50"}, args: with("raw-events", "list"), exact: true,
			json:   `{"items":[{"id":9,"eventId":"e7","receivedAt":"2026-11-17T08:15:43Z","outcome":"stored","size":512}]}`,
			narrow: []string{" ago"}, table: []string{"ID", "EVENT ID", "RECEIVED", "OUTCOME", "SIZE", "9", "e7", "stored", "512"}, wide: []string{"2026-11-17 08:15:43Z"},
			sel: []string{"id,outcome", `{"items":[{"id":9,"outcome":"stored"}]}`}},
		{name: "raw-events get", calls: []string{"GET projects/314/rawEvents/9"}, args: with("raw-events", "get", "9"), exact: true,
			json:  `{"id":9,"eventId":"e7","receivedAt":"2026-11-17T08:15:43Z","outcome":"stored","size":512,"payload":{"level":"error"}}`,
			table: []string{"ID", "9", "stored"}, jq: []string{".payload.level", `"error"`}},
		{name: "releases list", calls: []string{"GET projects/314/releases"}, query: map[string]string{"limit": "100"}, args: with("releases", "list"), exact: true,
			json:   `{"items":[{"release":"v1.2.3","firstSeen":"2026-11-10T10:00:00Z","lastSeen":"2026-11-17T08:15:42Z","eventCount":9}]}`,
			narrow: []string{" ago"}, table: []string{"RELEASE", "EVENTS", "FIRST SEEN", "LAST SEEN", "v1.2.3", "9"}, wide: []string{"2026-11-10 10:00:00Z", "2026-11-17 08:15:42Z"},
			sel: []string{"release", `{"items":[{"release":"v1.2.3"}]}`}},
		{name: "releases status", calls: []string{"GET projects/314/releases", "GET projects/314/errors", "GET projects/314/errors", "GET projects/314/errors"}, query: map[string]string{"limit": "21", "release": "v1.2.3", "sort": "lastSeen", "status": "all"}, card: true, sel: []string{"project", `{"project":{"id":314}}`}, args: with("releases", "status"), text: "Release v1.2.3 · project 314",
			json: `{"release":{"release":"v1.2.3"},"project":{"id":314},"new":{"items":[{"id":42}]},"regressed":{"items":[{"id":42}]},"lastSeen":{"items":[{"id":42}]}}`},
		{name: "environments list", calls: []string{"GET projects/314/environments"}, query: map[string]string{"limit": "100"}, args: with("environments", "list"), exact: true,
			json:   `{"items":[{"environment":"prod","firstSeen":"2026-11-10T10:00:00Z","lastSeen":"2026-11-17T08:15:42Z","eventCount":9}]}`,
			narrow: []string{" ago"}, table: []string{"ENVIRONMENT", "EVENTS", "FIRST SEEN", "LAST SEEN", "prod", "9"}, wide: []string{"2026-11-10 10:00:00Z"},
			sel: []string{"environment", `{"items":[{"environment":"prod"}]}`}},
		{name: "transactions list", calls: []string{"GET projects/314/transactions"}, query: map[string]string{"limit": "100"}, args: with("transactions", "list"), exact: true,
			json:  `{"items":[{"transaction":"/checkout","errorCount":1,"eventCount":9}]}`,
			table: []string{"TRANSACTION", "ERRORS", "EVENTS", "/checkout"}, sel: []string{"transaction", `{"items":[{"transaction":"/checkout"}]}`}},
		{name: "fields list", calls: []string{"GET projects/314/customFields"}, query: map[string]string{"limit": "100"}, args: with("fields", "list"), exact: true,
			json:  `{"items":[{"key":"browser","valueCount":1,"errorCount":1,"eventCount":9}]}`,
			table: []string{"KEY", "VALUES", "ERRORS", "EVENTS", "browser"}, sel: []string{"key", `{"items":[{"key":"browser"}]}`}},
		{name: "fields list-values", calls: []string{"GET projects/314/customFields/values"}, query: map[string]string{"key": "browser", "limit": "100"}, args: with("fields", "list-values", "browser"), exact: true,
			json:   `{"items":[{"value":"Chrome","errorCount":1,"eventCount":9,"firstSeen":"2026-11-10T10:00:00Z","lastSeen":"2026-11-17T08:15:42Z"}]}`,
			narrow: []string{" ago"}, table: []string{"VALUE", "ERRORS", "EVENTS", "FIRST SEEN", "LAST SEEN", "Chrome"}, wide: []string{"2026-11-10 10:00:00Z"},
			sel: []string{"value", `{"items":[{"value":"Chrome"}]}`}},
		{name: "users list", calls: []string{"GET projects/314/users"}, query: map[string]string{"limit": "51", "page": "1"}, args: with("users", "list"), exact: true,
			json:  `{"items":[{"uid":"u1","name":"Alex","login":"alex","avatarUrl":""}]}`,
			table: []string{"NAME", "LOGIN", "UID", "Alex", "alex", "u1"}, sel: []string{"uid", `{"items":[{"uid":"u1"}]}`}},
	}
}

func TestOutputFormats(t *testing.T) {
	f := outputFake(t)
	cases := outputCases()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := func(t *testing.T, args ...string) string {
				t.Helper()
				out, err := f.run(append(append([]string{}, tc.args...), args...)...)
				require.NoError(t, err)
				calls := f.takeCalls()
				require.Equal(t, tc.calls, routesOf(calls), "the requests behind %v", args)
				last := calls[len(calls)-1]
				got := map[string]string{}
				for k := range last.Query {
					got[k] = last.Query.Get(k)
					if tc.query[k] == "*" {
						got[k] = "*"
					}
				}
				want := tc.query
				if want == nil {
					want = map[string]string{}
				}
				assert.Equal(t, want, got, "the query of %s", last.Route)
				if tc.body != "" {
					assert.JSONEq(t, tc.body, last.Body)
				}
				return out
			}
			t.Run("json", func(t *testing.T) { assertOutputJSON(t, tc, []byte(run(t, "-o", "json"))) })
			t.Run("yaml", func(t *testing.T) {
				out, err := yaml.YAMLToJSON([]byte(run(t, "-o", "yaml")))
				require.NoError(t, err)
				assertOutputJSON(t, tc, out)
			})
			t.Run("agents", func(t *testing.T) { assertOutputJSON(t, tc, []byte(run(t, "-o", "agents"))) })
			if tc.card {
				t.Run("text by default", func(t *testing.T) {
					assert.Contains(t, run(t), tc.text)
					assert.Contains(t, run(t, "-o", "text"), tc.text)
				})
				if tc.sel != nil {
					t.Run("json fields", func(t *testing.T) { assert.JSONEq(t, tc.sel[1], run(t, "--json", tc.sel[0])) })
				}
				return
			}
			if tc.noTable {
				var want struct {
					Action string `json:"action"`
				}
				require.NoError(t, json.Unmarshal([]byte(tc.json), &want))
				t.Run("json fields", func(t *testing.T) {
					assert.JSONEq(t, `{"action":"`+want.Action+`"}`, run(t, "--json", "action"))
				})
				t.Run("jq", func(t *testing.T) {
					assert.Equal(t, `"`+want.Action+`"`, strings.TrimSpace(run(t, "--jq", ".action")))
				})
				t.Run("text by default", func(t *testing.T) {
					assert.Contains(t, run(t), tc.text)
					assert.Contains(t, run(t, "-o", "text"), tc.text)
				})
				return
			}
			t.Run("table by default", func(t *testing.T) {
				for _, out := range []string{run(t), run(t, "-o", "table")} {
					for _, s := range append(append([]string{}, tc.table...), tc.narrow...) {
						assert.Contains(t, out, s)
					}
					for _, s := range tc.wide {
						if !strings.Contains(strings.Join(tc.table, " "), s) {
							assert.NotContains(t, out, s, "only -o wide shows it")
						}
					}
				}
			})
			if tc.wide != nil {
				t.Run("wide", func(t *testing.T) {
					out := run(t, "-o", "wide")
					for _, s := range append(append([]string{}, tc.table...), tc.wide...) {
						assert.Contains(t, out, s)
					}
					for _, s := range tc.narrow {
						assert.NotContains(t, out, s, "only the narrow table shows it")
					}
				})
			}
			if tc.sel != nil {
				t.Run("json fields", func(t *testing.T) { assert.JSONEq(t, tc.sel[1], run(t, "--json", tc.sel[0])) })
			}
			if tc.jq != nil {
				t.Run("jq", func(t *testing.T) { assert.Equal(t, tc.jq[1], strings.TrimSpace(run(t, "--jq", tc.jq[0]))) })
			}
		})
	}
}

func TestEveryLeafTeachesAgents(t *testing.T) {
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if !c.HasSubCommands() {
			assert.NotEmpty(t, c.Annotations[agent.AnnotationLLMHint], "%s has an llm_hint", c.CommandPath())
			assert.Contains(t, c.Example, "As an agent", "%s has an agent example", c.CommandPath())
		}
		for _, s := range c.Commands() {
			walk(s)
		}
	}
	walk((&errortracking.Provider{}).Commands()[0])
}

func TestOutputEdges(t *testing.T) {
	f := newFakeET(t)
	long := strings.Repeat("x", 70)
	f.reply("GET projects/314/errors", 200, `{"errors":[{"id":1,"message":"`+long+`"}]}`)
	f.reply("GET projects/314/errors/42", 200, `{"id":42,"history":[]}`)
	f.reply("GET projects/314/errors/42/activity", 200, `{"activity":[{"id":"assignment:1","type":"assignment","createdAt":"2026-11-17T08:20:00Z","actor":"u1"}]}`)
	f.reply("GET projects/314/errors/42/customFields", 200, ``)
	f.runCases(t, []cliCase{
		{name: "a long message is cut in the table", args: []string{"errors", "list", "--project", "314"},
			calls: []string{"GET projects/314/errors"}, has: []string{strings.Repeat("x", 59) + "…"}, hasNot: []string{strings.Repeat("x", 60)}},
		{name: "the whole message shows in wide", args: []string{"errors", "list", "--project", "314", "-o", "wide"},
			calls: []string{"GET projects/314/errors"}, has: []string{long}},
		{name: "an error without a stored event", args: []string{"errors", "get", "42", "--project", "314"},
			calls: []string{"GET projects/314/errors/42"}, has: []string{"LATEST EVENT"}, check: func(t *testing.T, out string, _ error, _ []etCall) {
				t.Helper()
				lines := strings.Split(strings.TrimSpace(out), "\n")
				require.Len(t, lines, 2)
				assert.True(t, strings.HasSuffix(strings.TrimSpace(lines[1]), "-"), "%q", lines[1])
			}},
		{name: "an activity type gcx does not know shows as its type", args: []string{"errors", "activity", "list", "42", "--project", "314"},
			calls: []string{"GET projects/314/errors/42/activity"}, has: []string{"assignment"}},
		{name: "an empty reply is no data", args: []string{"errors", "fields", "list", "42", "--project", "314", "-o", "json"},
			calls: []string{"GET projects/314/errors/42/customFields"}, json: `{"items":[]}`},
		{name: "field discovery prints no truncation hint", setup: func(f *fakeET) {
			f.reply("GET projects/314/errors", 200, `{"errors":[`+strings.TrimSuffix(strings.Repeat(`{"id":1,"message":"m"},`, 50), ",")+`],"nextCursor":"c"}`)
		}, args: []string{"errors", "list", "--project", "314", "--json", "list"}, calls: []string{"GET projects/314/errors"}, has: []string{"eventCount\n", "message\n"},
			check: func(t *testing.T, _ string, _ error, calls []etCall) {
				t.Helper()
				assert.Equal(t, "50", calls[0].Query.Get("limit"), "--json discovery reads the list as --limit asks")
				assert.NotContains(t, f.stderr.String(), "showing first")
				assert.NotContains(t, f.stderr.String(), "hint:")
			}},
		{name: "an invalid --jq is refused before any request", args: []string{"errors", "list", "--project", "314", "--jq", ".["},
			exit: gcxerrors.ExitUsageError, err: "jq"},
	})
}
