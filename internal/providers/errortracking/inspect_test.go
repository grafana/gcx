package errortracking_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
)

const inspectError = `{"id":42,"projectId":314,"projectName":"Checkout API","status":"ignored","ignoredUntilEvents":110,"exceptionType":"TypeError",
	"message":"Cannot read properties of undefined (reading 'total')","culprit":"checkout.ts in computeTotal","transaction":"POST /checkout",
	"eventCount":4,"firstSeen":"2026-11-16T10:00:00Z","lastSeen":"2026-11-17T10:00:00Z","lastRelease":"web@1.1.0","commentCount":1,
	"latestEvent":{"id":7,"eventId":"10bd72c2aa4b4c3b8e7f6a5b4c3ec0d8","occurredAt":"2026-11-17T10:00:00Z","environment":"production","release":"web@1.1.0",
		"level":"error","tags":{"region":"us","plan":"pro"},
		"payload":{"exception":{"values":[{"type":"TypeError","stacktrace":{"frames":[
			{"filename":"app:///main.js","function":"main","lineno":1,"colno":10,"in_app":false},
			{"filename":"src/checkout.ts","function":"computeTotal","lineno":12,"colno":3,"in_app":true}]}}]}}},"history":[]}`

func TestErrorsInspect(t *testing.T) {
	f := newFakeET(t)
	const get, activity, assignees = "GET projects/314/errors/42", "GET projects/314/errors/42/activity", "GET projects/314/errors/42/assignees"
	f.reply(get, 200, inspectError)
	f.reply(activity, 200, `{"activity":[{"id":"comment:12","type":"comment","createdAt":"2026-11-17T10:05:00Z","actor":"u1","actorName":"Alex","comment":{"id":12,"body":"pricing rollout"}},
		{"id":"statusChange:3","type":"statusChange","createdAt":"2026-11-17T10:04:00Z","actor":"u1","actorName":"Alex","statusChange":{"fromStatus":"unresolved","toStatus":"ignored"}}]}`)
	f.reply(assignees, 200, `{"assignees":[{"userUid":"u-alex","name":"Alex","login":"alex"}],"canAssign":true}`)
	inspect := []string{"errors", "inspect", "42", "--project", "314"}
	all := []string{get, activity, assignees}

	f.runCases(t, []cliCase{
		{name: "the card shows the error, its frames newest first, activity and assignees", args: inspect, calls: all,
			has: []string{
				"Error 42 · ignored · project Checkout API (ID 314)",
				"TypeError: Cannot read properties of undefined (reading 'total')",
				"culprit      checkout.ts in computeTotal",
				"transaction  POST /checkout",
				"4 · first seen 2026-11-16 10:00Z (",
				"ignored      until 110 events (106 more)",
				"assignees    Alex",
				"Latest event 10bd72c2…ec0d8 · 2026-11-17 10:00:00Z · production · web@1.1.0\n  frames (most recent call first)\n",
				"src/checkout.ts:12:3",
				"computeTotal",
				"in app",
				"tags         plan=pro  region=us",
				"Activity (latest 2)",
				"comment: pricing rollout",
				"unresolved → ignored",
			},
			check: func(t *testing.T, out string, _ error, calls []etCall) {
				t.Helper()
				assert.Less(t, strings.Index(out, "src/checkout.ts"), strings.Index(out, "app:///main.js:1:10"), "the newest frame comes first")
				assert.Equal(t, "3", calls[1].Query.Get("limit"))
				assert.NotContains(t, f.stderr.String(), "hint:")
			}},
		{name: "a multi-line message shows every line", setup: func(f *fakeET) {
			f.reply(get, 200, `{"id":42,"projectId":314,"status":"unresolved","exceptionType":"MultiLine","message":"first line\nsecond line\n  third","history":[]}`)
		}, args: inspect, calls: all, has: []string{"MultiLine: first line\n  second line\n    third\n"}, hasNot: []string{"…"}},
		{name: "a tab inside the message becomes spaces", setup: func(f *fakeET) {
			f.reply(get, 200, `{"id":42,"projectId":314,"status":"unresolved","exceptionType":"E","message":"first\nsecond line\ttabbed","history":[]}`)
		}, args: inspect, calls: all, has: []string{"  second line    tabbed\n"}, hasNot: []string{"second linetabbed"}},
		{name: "a long message without a type names only the message", setup: func(f *fakeET) {
			f.reply(get, 200, `{"id":42,"projectId":314,"status":"unresolved","message":"`+strings.Repeat("word ", 600)+`","history":[]}`)
		}, args: inspect, calls: all, has: []string{"  … (3000 characters; all of it: --json message)"}},
		{name: "a long type without a message names only the type", setup: func(f *fakeET) {
			f.reply(get, 200, `{"id":42,"projectId":314,"status":"unresolved","exceptionType":"`+strings.Repeat("Type", 750)+`","history":[]}`)
		}, args: inspect, calls: all, has: []string{"  … (3000 characters; all of it: --json exceptionType)"}},
		{name: "a 3000-character line is wrapped and capped", setup: func(f *fakeET) {
			f.reply(get, 200, `{"id":42,"projectId":314,"status":"unresolved","exceptionType":"E","message":"`+strings.Repeat("word ", 600)+`","history":[]}`)
		}, args: inspect, calls: all, has: []string{"  … (3001 characters; all of it: --json exceptionType,message)"},
			check: func(t *testing.T, out string, _ error, _ []etCall) {
				t.Helper()
				for line := range strings.SplitSeq(out, "\n") {
					assert.LessOrEqual(t, len([]rune(line)), 100, "no line is longer than the width: %q", line)
				}
				assert.Equal(t, 7, strings.Count(out, "\n  word"), "the message stops after 8 lines")
				f.reply(get, 200, inspectError)
			}},
		{name: "JSON is errors get plus recentActivity and assignees", args: append(inspect, "--json", "id,recentActivity,assignees"), calls: all,
			json: `{"id":42,"recentActivity":[{"id":"comment:12","type":"comment","createdAt":"2026-11-17T10:05:00Z","actor":"u1","actorName":"Alex","comment":{"id":12,"body":"pricing rollout","canEdit":false}},
				{"id":"statusChange:3","type":"statusChange","createdAt":"2026-11-17T10:04:00Z","actor":"u1","actorName":"Alex","statusChange":{"fromStatus":"unresolved","toStatus":"ignored"}}],
				"assignees":[{"userUid":"u-alex","name":"Alex","login":"alex","avatarUrl":"","assignedBy":"","assignedAt":""}]}`},
		{name: "a service account is not asked for assignees", setup: func(f *fakeET) { f.useToken(serviceAccountToken) }, args: inspect,
			calls: []string{get, activity}, has: []string{"assignees    not shown: needs a user login with permission to read assignees (gcx login)"}},
		{name: "JSON leaves assignees out when they cannot be read", setup: func(f *fakeET) { f.useToken(serviceAccountToken) }, args: append(inspect, "-o", "json"),
			calls: []string{get, activity}, hasNot: []string{`"assignees"`},
			stderr: "hint: assignees are not in the output: reading them needs a user login with permission to read assignees: gcx login"},
		{name: "JSON discovery prints no assignees hint", setup: func(f *fakeET) { f.useToken(serviceAccountToken) }, args: append(inspect, "--json", "?"),
			calls: []string{get, activity}, check: func(t *testing.T, _ string, _ error, _ []etCall) {
				t.Helper()
				assert.NotContains(t, f.stderr.String(), "assignees are not in the output")
			}},
		{name: "JSON names the role when a signed-in user is refused, not gcx login", setup: func(f *fakeET) {
			f.useToken("test-token")
			f.reply(assignees, http.StatusForbidden, `{"error":{"code":"forbidden","message":"permission denied"}}`)
		}, args: append(inspect, "-o", "json"), calls: all, hasNot: []string{`"assignees"`},
			stderr: "hint: assignees are not in the output: your role cannot read assignees\n",
			check: func(t *testing.T, _ string, _ error, _ []etCall) {
				t.Helper()
				assert.NotContains(t, f.stderr.String(), "gcx login")
			}},
		{name: "the text card says it on its row, with no extra hint", setup: func(f *fakeET) { f.useToken(serviceAccountToken) }, args: inspect,
			calls: []string{get, activity}, check: func(t *testing.T, _ string, _ error, _ []etCall) {
				t.Helper()
				assert.NotContains(t, f.stderr.String(), "assignees are not in the output")
			}},
		{name: "a 403 on assignees names the role, not a failure", setup: func(f *fakeET) {
			f.useToken("test-token")
			f.reply(assignees, http.StatusForbidden, `{"error":{"code":"forbidden","message":"permission denied"}}`)
		}, args: inspect, calls: all, has: []string{"assignees    not shown: your role cannot read assignees"}, hasNot: []string{"gcx login"}},
		{name: "a 403 with the service account body gets the same role note", setup: func(f *fakeET) {
			f.useToken("test-token")
			f.reply(assignees, http.StatusForbidden, serviceAccount403)
		}, args: inspect, calls: all, has: []string{"assignees    not shown: your role cannot read assignees"}, hasNot: []string{"gcx login"}},
		{name: "another assignees failure fails the command", setup: func(f *fakeET) {
			f.reply(assignees, http.StatusInternalServerError, `{"error":{"code":"internal","message":"boom"}}`)
		}, args: inspect, calls: all, exit: gcxerrors.ExitGeneralError, err: "boom"},
		{name: "a payload without frames says so", setup: func(f *fakeET) {
			f.reply(get, 200, `{"id":42,"projectId":314,"status":"resolved","latestEvent":{"eventId":"e1","occurredAt":"2026-11-17T10:00:00Z","level":"error","payload":{}},"history":[]}`)
			f.reply(activity, 200, `{"activity":[]}`)
			f.reply(assignees, 200, `{"assignees":[]}`)
		}, args: inspect, calls: all, has: []string{"\n  frames       none in the payload\n  level        error\n", "Activity: none", "assignees    nobody"},
			check: func(t *testing.T, _ string, _ error, _ []etCall) {
				t.Helper()
				assert.NotContains(t, f.stderr.String(), "--status resolved", "a resolved error gets no resolve hint")
			}},
		{name: "agent mode prints one JSON value and no hint lines", setup: asAgent, args: append(inspect, "--json", "id,status"), calls: all,
			json: `{"id":42,"status":"ignored"}`,
			check: func(t *testing.T, _ string, _ error, _ []etCall) {
				t.Helper()
				assert.NotContains(t, f.stderr.String(), `"class":"hint"`)
				agent.SetFlag(false)
			}},
		{name: "no error ID", args: []string{"errors", "inspect", "--project", "314"}, exit: gcxerrors.ExitUsageError, err: "accepts 1 arg(s), received 0"},
	})
}

func TestEventsGetFormats(t *testing.T) {
	f := newFakeET(t)
	const route = "GET projects/314/errors/42/events/10bd72c2aa4b4c3b8e7f6a5b4c3ec0d8"
	f.reply(route, 200, `{"id":7,"eventId":"10bd72c2aa4b4c3b8e7f6a5b4c3ec0d8","occurredAt":"2026-11-17T10:00:00Z","environment":"production","release":"web@1.1.0",
		"level":"error","transaction":"POST /checkout","serverName":"web-1","traceId":"t1","tags":{"region":"us"},
		"payload":{"exception":{"values":[{"stacktrace":{"frames":[{"module":"checkout","function":"total"},{"abs_path":"/srv/a.py","function":"handle","lineno":9}]}}]}}}`)
	get := []string{"errors", "events", "get", "42", "10bd72c2aa4b4c3b8e7f6a5b4c3ec0d8", "--project", "314"}

	f.runCases(t, []cliCase{
		{name: "the table stays the default", args: get, calls: []string{route}, has: []string{"EVENT ID", "production"}, hasNot: []string{"Event 10bd72c2…ec0d8 ·"}},
		{name: "there is no text card", args: append(slices.Clone(get), "-o", "text"), exit: gcxerrors.ExitUsageError, err: "unknown output format 'text'"},
		{name: "JSON is the same object", args: append(slices.Clone(get), "--json", "eventId,release"), calls: []string{route},
			json: `{"eventId":"10bd72c2aa4b4c3b8e7f6a5b4c3ec0d8","release":"web@1.1.0"}`},
		{name: "the numeric id is explained", args: []string{"errors", "events", "get", "42", "7", "--project", "314"}, exit: gcxerrors.ExitUsageError,
			err: "looks like the numeric id"},
		{name: "swapped arguments say the error comes first", args: []string{"errors", "events", "get", "10bd72c2aa4b4c3b8e7f6a5b4c3ec0d8", "42", "--project", "314"},
			exit: gcxerrors.ExitUsageError, err: `<error> "10bd72c2aa4b4c3b8e7f6a5b4c3ec0d8" is not an error ID; the error comes first, then the event`,
			check: func(t *testing.T, _ string, err error, _ []etCall) {
				t.Helper()
				assert.Contains(t, rendered(t, err), "Swap them: gcx error-tracking errors events get 42 10bd72c2aa4b4c3b8e7f6a5b4c3ec0d8 --project 314")
			}},
		{name: "no event", args: []string{"errors", "events", "get", "42", "--project", "314"}, exit: gcxerrors.ExitUsageError, err: "accepts 2 arg(s), received 1"},
	})
}
