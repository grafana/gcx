package errortracking_test

import (
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
)

const roleOnly403 = `{"error":{"code":"forbidden","message":"insufficient role for this action"}}`

const serviceAccount403 = `{"error":{"code":"forbidden","message":"current organization membership and sufficient role are required"}}`

func assertHumanOnly(t *testing.T, _ string, err error, _ []etCall) {
	t.Helper()
	out := rendered(t, err)
	assert.Contains(t, out, gcxerrors.SummaryAuthorizationFailed)
	assert.Equal(t, 1, strings.Count(out, "sufficient role are required"), "the server message is printed once: %s", out)
	assert.Regexp(t, `Ask an organization admin for the (Editor or Admin|Viewer, Editor or Admin) role in this organization`, out)
	assert.Contains(t, out, "Use your own login, not a service account token: gcx login")
	assert.Contains(t, out, "If GRAFANA_TOKEN is set in your environment it overrides the login: unset it for this command")
	assert.NotContains(t, out, "Sign in as yourself", "the role advice and the sign-in note read as one request")
}

func assertRoleOnly(role string) func(t *testing.T, _ string, err error, _ []etCall) {
	return func(t *testing.T, _ string, err error, _ []etCall) {
		t.Helper()
		out := rendered(t, err)
		assert.Contains(t, out, gcxerrors.SummaryAuthorizationFailed)
		assert.Contains(t, out, "Ask an organization admin for the "+role+" role in this organization")
	}
}

func refusedBeforeRequest(what, read string) func(t *testing.T, _ string, err error, _ []etCall) {
	return func(t *testing.T, _ string, err error, _ []etCall) {
		t.Helper()
		out := rendered(t, err)
		assert.Contains(t, out, gcxerrors.SummaryAuthorizationFailed)
		assert.Contains(t, out, what+"; this context uses a service account token")
		assert.Regexp(t, `Sign in as yourself(, with the Editor or Admin role)?: gcx login`, out)
		assert.Contains(t, out, "As a service account you can still read "+read)
	}
}

const errorFixture = `{"id":42,"projectId":314,"projectName":"Checkout API","status":"unresolved","reviewed":false,"exceptionType":"TypeError","message":"boom","history":[]}`

func TestErrorsUpdateReviewed(t *testing.T) {
	f := newFakeET(t)
	const get, route = "GET projects/314/errors/42", "POST projects/314/errors/42/review"
	f.reply(get, 200, errorFixture)
	f.reply("GET projects/314/errors/43", 200, `{"id":43,"projectName":"Checkout API","status":"resolved","reviewed":true,"history":[]}`)
	f.reply(route, 200, `{"id":42,"status":"unresolved","reviewed":true,"history":[]}`)
	robot := func(f *fakeET) { f.useToken(serviceAccountToken) }

	update := func(args ...string) []string {
		return append([]string{"errors", "update", "42", "--project", "314"}, args...)
	}
	f.runCases(t, []cliCase{
		{name: "review sends no body and names the project", args: update("--reviewed", "-o", "json"),
			calls: []string{get, route}, json: `{"type":"gcx.mutation_batch","schema_version":"1","action":"reviewed","summary":{"succeeded":1,"failed":0},
				"failures":[],"project":{"id":314,"name":"Checkout API"},"results":[]}`,
			check: func(t *testing.T, _ string, _ error, calls []etCall) {
				t.Helper()
				assert.Empty(t, calls[1].Body)
			}},
		{name: "text", args: update("--reviewed"), calls: []string{get, route}, has: []string{"Marked 1 of 1 errors in project Checkout API (ID 314) reviewed"}},
		{name: "an error already reviewed is skipped, with no write", args: []string{"errors", "update", "43", "--project", "314", "--reviewed"},
			calls: []string{"GET projects/314/errors/43"}, has: []string{"Marked 0 of 1 errors in project Checkout API (ID 314) reviewed; 1 was already reviewed", "resolved (no change)"}},
		{name: "several errors give a batch", args: []string{"errors", "update", "42", "43", "--project", "314", "--reviewed", "-o", "json"},
			calls: []string{get, "GET projects/314/errors/43", route}, anyOrd: true,
			json: `{"type":"gcx.mutation_batch","schema_version":"1","action":"reviewed",
					"summary":{"succeeded":1,"failed":0,"skipped":1},"skipped_reasons":{"already reviewed":1},
					"failures":[],"project":{"id":314,"name":"Checkout API"},
					"results":[{"id":43,"previousStatus":"resolved","status":"resolved","changed":false}]}`},
		{name: "a signed-in user without the role gets the API's 403", setup: func(f *fakeET) { f.reply(route, http.StatusForbidden, serviceAccount403) },
			args: update("--reviewed", "-o", "json"), calls: []string{get, route}, exit: gcxerrors.ExitAuthFailure, check: assertHumanOnly},
		{name: "a service account token is refused before any request", setup: robot, args: update("--reviewed"),
			exit: gcxerrors.ExitAuthFailure, check: refusedBeforeRequest("errors update --reviewed needs a signed-in Grafana user (Editor or Admin)",
				"the error: gcx error-tracking errors get 42 --project 314")},
		{name: "a bad error ID", args: []string{"errors", "update", "x", "--project", "314", "--reviewed"}, exit: gcxerrors.ExitUsageError, err: `<error> "x" must be a positive error ID`,
			check: func(t *testing.T, _ string, err error, _ []etCall) {
				t.Helper()
				assert.Contains(t, rendered(t, err), "See them: gcx error-tracking errors list --project 314")
			}},
	}, usageCases(func(args ...string) []string { return update(args...) },
		[]string{"--reviewed=false"},
		[]string{"--reviewed", "--status", "resolved"},
		[]string{"--reviewed", "--until", "7d"},
		[]string{"--reviewed", "--more-users", "3"},
	))
}

func TestErrorsListActivity(t *testing.T) {
	f := newFakeET(t)
	const route = "GET projects/314/errors/42/activity"
	f.handle(route, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cursor") == "" {
			_, _ = w.Write([]byte(`{"activity":[{"id":"comment:7","type":"comment","createdAt":"2026-11-17T10:00:00Z","actor":"u1","actorName":"Alex","comment":{"id":7,"body":"Caused by the pricing rollout.\nMore.","canEdit":true}}],"nextCursor":"c1"}`))
			return
		}
		_, _ = w.Write([]byte(`{"activity":[{"id":"statusChange:3","type":"statusChange","createdAt":"2026-11-16T10:00:00Z","actor":"system:ingest","statusChange":{"fromStatus":"unresolved","toStatus":"resolved"}}]}`))
	})

	f.runCases(t, []cliCase{
		{name: "a table of every entry, with ages", args: []string{"errors", "activity", "list", "42", "--project", "314", "--limit", "0", "-o", "table"},
			calls: []string{route, route}, has: []string{"COMMENT ID", "7 ", "comment: Caused by the pricing rollout. …", "unresolved → resolved", "Alex u1", "system:ingest", " ago"},
			hasNot: []string{"More.", "2026-11-17"}},
		{name: "wide shows the full UTC time", args: []string{"errors", "activity", "list", "42", "--project", "314", "--limit", "0", "-o", "wide"},
			calls: []string{route, route}, has: []string{"2026-11-17 10:00:00Z", "2026-11-16 10:00:00Z"}, hasNot: []string{" ago"}},
		{name: "comments only", args: []string{"errors", "activity", "list", "42", "--project", "314", "--type", "comment", "--limit", "1", "-o", "json"},
			calls: []string{route}, query: map[string][]string{"limit": {"1"}, "type": {"comment"}}, has: []string{`"truncated": true`, `"canEdit": true`}},
		{name: "status changes only", args: []string{"errors", "activity", "list", "42", "--project", "314", "--type", "status-change", "--limit", "1", "-o", "json"},
			calls: []string{route}, query: map[string][]string{"limit": {"1"}, "type": {"statusChange"}}},
		{name: "a bad type", args: []string{"errors", "activity", "list", "42", "--project", "314", "--type", "comments"}, exit: gcxerrors.ExitUsageError},
		{name: "an empty type", args: []string{"errors", "activity", "list", "42", "--project", "314", "--type", ""}, exit: gcxerrors.ExitUsageError},
	})
}

func TestErrorsAssignees(t *testing.T) {
	f := newFakeET(t)
	const list, put, del = "GET projects/314/errors/42/assignees", "PUT projects/314/errors/42/assignees/u-alex", "DELETE projects/314/errors/42/assignees/u-alex"
	const me = "GET /api/user"
	f.reply(list, 200, `{"assignees":[{"userUid":"u-alex","name":"Alex","login":"alex","avatarUrl":"","assignedBy":"u1","assignedAt":"2026-11-17T10:00:00Z"}],"canAssign":true}`)
	f.reply(put, 200, `{"userUid":"u-alex","name":"Alex","login":"alex","avatarUrl":"","assignedBy":"u1","assignedAt":"2026-11-17T10:00:00Z"}`)
	f.reply(del, http.StatusNoContent, ``)
	f.reply("PUT projects/314/errors/42/assignees/u%2Fx", 200, `{}`)
	f.reply("GET projects/314/errors/42", 200, errorFixture)
	f.reply(me, 200, `{"uid":"u-alex","login":"alex","name":"Alex","email":"alex@example.com"}`)
	robot := func(f *fakeET) { f.useToken(serviceAccountToken) }
	unassigned := func(f *fakeET) { f.reply(list, 200, `{"assignees":[],"canAssign":true}`) }
	assigned := `{"type":"gcx.mutation","schema_version":"1","action":"assigned","changed":true,"target":{"kind":"Error","id":"42","namespace":"stacks-123"},"project":{"id":314,"name":"Checkout API"},"user":"u-alex"}`

	f.runCases(t, []cliCase{
		{name: "list as JSON", args: []string{"errors", "assignees", "list", "42", "--project", "314", "-o", "json"},
			calls: []string{list}, json: `{"items":[{"userUid":"u-alex","name":"Alex","login":"alex","avatarUrl":"","assignedBy":"u1","assignedAt":"2026-11-17T10:00:00Z"}],"canAssign":true}`},
		{name: "the agent example returns real values", args: []string{"errors", "assignees", "list", "42", "--project", "314", "--json", "userUid,login"},
			calls: []string{list}, json: `{"items":[{"userUid":"u-alex","login":"alex"}],"canAssign":true}`},
		{name: "list as a table", args: []string{"errors", "assignees", "list", "42", "--project", "314", "-o", "table"},
			calls: []string{list}, check: func(t *testing.T, out string, _ error, _ []etCall) {
				t.Helper()
				assert.Regexp(t, `(?m)^UID +NAME +LOGIN +ASSIGNED AT\n`, out)
				assert.Regexp(t, `(?m)^u-alex +Alex +alex +`, out)
			}},
		{name: "list none", setup: func(f *fakeET) { f.reply(list, 200, `{"canAssign":false}`) },
			args: []string{"errors", "assignees", "list", "42", "--project", "314", "-o", "json"}, calls: []string{list}, json: `{"items":[],"canAssign":false}`},
		{setup: unassigned, name: "assign by UID", args: []string{"errors", "assignees", "add", "42", " u-alex ", "--project", "314", "-o", "json"},
			calls: []string{"GET projects/314/errors/42", list, put}, json: assigned},
		{setup: unassigned, name: "assign by UID names the user and the project in text", args: []string{"errors", "assignees", "add", "42", "u-alex", "--project", "314"},
			calls: []string{"GET projects/314/errors/42", list, put}, has: []string{"Assigned u-alex to error 42 in project Checkout API (ID 314)"},
			check: func(t *testing.T, _ string, _ error, _ []etCall) {
				t.Helper()
				assert.NotContains(t, f.stderr.String(), "hint:")
			}},
		{setup: unassigned, name: "assign yourself", args: []string{"errors", "assignees", "add", "42", "me", "--project", "314", "-o", "json"},
			calls: []string{"GET projects/314/errors/42", me, list, put}, json: assigned},
		{setup: unassigned, name: "a UID is one path segment", args: []string{"errors", "assignees", "add", "42", "u/x", "--project", "314", "-o", "json"},
			calls: []string{"GET projects/314/errors/42", list, "PUT projects/314/errors/42/assignees/u%2Fx"}},
		{name: "an assigned UID is assigned again as changed: false, with nothing sent", args: []string{"errors", "assignees", "add", "42", "u-alex", "--project", "314", "-o", "json"},
			calls: []string{"GET projects/314/errors/42", list},
			json:  `{"type":"gcx.mutation","schema_version":"1","action":"assigned","changed":false,"target":{"kind":"Error","id":"42","namespace":"stacks-123"},"project":{"id":314,"name":"Checkout API"},"user":"u-alex"}`},
		{name: "an assigned UID says so in text", args: []string{"errors", "assignees", "add", "42", "u-alex", "--project", "314"},
			calls: []string{"GET projects/314/errors/42", list}, has: []string{"Alex (alex), UID u-alex was already assigned to error 42 in project Checkout API (ID 314); nothing changed"}},
		{name: "an assigned UID is unassigned without a prompt", args: []string{"errors", "assignees", "delete", "42", "u-alex", "--project", "314"},
			calls: []string{"GET projects/314/errors/42", list, del}, has: []string{"Unassigned Alex (alex), UID u-alex from error 42 in project Checkout API (ID 314)"}},
		{name: "assignees delete takes no --force", args: []string{"errors", "assignees", "delete", "42", "u-alex", "--project", "314", "--force"}, exit: gcxerrors.ExitUsageError},
		{name: "yourself is unassigned", args: []string{"errors", "assignees", "delete", "42", "me", "--project", "314", "-o", "json"},
			calls: []string{"GET projects/314/errors/42", me, list, del}, has: []string{`"changed": true`}},
		{name: "an assigned user who left is unassigned by UID", setup: func(f *fakeET) {
			f.reply(list, 200, `{"assignees":[{"userUid":"u-gone"}],"canAssign":true}`)
			f.reply("DELETE projects/314/errors/42/assignees/u-gone", http.StatusNoContent, ``)
		}, args: []string{"errors", "assignees", "delete", "42", "u-gone", "--project", "314", "-o", "json"},
			calls: []string{"GET projects/314/errors/42", list, "DELETE projects/314/errors/42/assignees/u-gone"}, has: []string{`"changed": true`}},
		{name: "agent mode unassigns without a prompt", setup: asAgent, args: []string{"errors", "assignees", "delete", "42", "u-alex", "--project", "314", "-o", "json"},
			calls: []string{"GET projects/314/errors/42", list, del}, has: []string{`"changed": true`}},
		{name: "a UID who is not assigned changes nothing and sends no delete", setup: func(f *fakeET) {
			f.reply(list, 200, `{"assignees":[{"userUid":"u-sam1","name":"Sam","login":"sam1"}],"canAssign":true}`)
		}, args: []string{"errors", "assignees", "delete", "42", "u-alex", "--project", "314", "-o", "json"},
			calls: []string{"GET projects/314/errors/42", list},
			json:  `{"type":"gcx.mutation","schema_version":"1","action":"unassigned","changed":false,"target":{"kind":"Error","id":"42","namespace":"stacks-123"},"project":{"id":314,"name":"Checkout API"},"user":"u-alex"}`},
		{name: "and says so in text", setup: func(f *fakeET) {
			f.reply(list, 200, `{"assignees":[],"canAssign":true}`)
		}, args: []string{"errors", "assignees", "delete", "42", "u-alex", "--project", "314"},
			calls: []string{"GET projects/314/errors/42", list}, has: []string{"u-alex was not assigned to error 42 in project Checkout API (ID 314); nothing changed"}},
		{name: "a service account token is refused before assignees delete sends anything", setup: robot, args: []string{"errors", "assignees", "delete", "42", "u-alex", "--project", "314"},
			exit: gcxerrors.ExitAuthFailure, check: refusedBeforeRequest("errors assignees delete needs a signed-in Grafana user (Editor or Admin)", "the error: gcx error-tracking errors get 42 --project 314")},
		{name: "a signed-in user without the role gets the API's 403", setup: func(f *fakeET) {
			unassigned(f)
			f.reply(put, http.StatusForbidden, serviceAccount403)
		},
			args: []string{"errors", "assignees", "add", "42", "u-alex", "--project", "314", "-o", "json"}, calls: []string{"GET projects/314/errors/42", list, put}, exit: gcxerrors.ExitAuthFailure, check: assertHumanOnly},
		{name: "a service account token is refused before any request", setup: robot, args: []string{"errors", "assignees", "add", "42", "alex", "--project", "314"},
			exit: gcxerrors.ExitAuthFailure, check: refusedBeforeRequest("errors assignees add needs a signed-in Grafana user (Editor or Admin)", "the error: gcx error-tracking errors get 42 --project 314")},
		{name: "assignees list too", setup: robot, args: []string{"errors", "assignees", "list", "42", "--project", "314"},
			exit: gcxerrors.ExitAuthFailure, check: refusedBeforeRequest("errors assignees list needs a signed-in Grafana user", "the error: gcx error-tracking errors get 42 --project 314")},
		{name: "an empty user", args: []string{"errors", "assignees", "add", "42", " ", "--project", "314"}, exit: gcxerrors.ExitUsageError, err: "<user> must be a user UID or me"},
		{name: "a dot user", args: []string{"errors", "assignees", "add", "42", ".", "--project", "314"}, exit: gcxerrors.ExitUsageError},
		{name: "a dot-dot user", args: []string{"errors", "assignees", "delete", "42", "..", "--project", "314"}, exit: gcxerrors.ExitUsageError},
		{name: "selected fields keep canAssign", setup: func(f *fakeET) {
			f.reply(list, 200, `{"assignees":[{"userUid":"u-alex","name":"Alex"}],"canAssign":true}`)
		}, args: []string{"errors", "assignees", "list", "42", "--project", "314", "--json", "userUid"},
			calls: []string{list}, json: `{"items":[{"userUid":"u-alex"}],"canAssign":true}`},
		{name: "a 403 that names only the role gets role advice on assignees add", setup: func(f *fakeET) {
			unassigned(f)
			f.reply(put, http.StatusForbidden, roleOnly403)
		},
			args: []string{"errors", "assignees", "add", "42", "u-alex", "--project", "314", "-o", "json"}, calls: []string{"GET projects/314/errors/42", list, put},
			exit: gcxerrors.ExitAuthFailure, check: assertRoleOnly("Editor or Admin")},
		{name: "a 403 that names only the role gets role advice on assignees delete", setup: func(f *fakeET) { f.reply(del, http.StatusForbidden, roleOnly403) },
			args: []string{"errors", "assignees", "delete", "42", "u-alex", "--project", "314"}, calls: []string{"GET projects/314/errors/42", list, del},
			exit: gcxerrors.ExitAuthFailure, check: assertRoleOnly("Editor or Admin")},
		{name: "a Grafana-shaped 403 body gets the role advice too", setup: func(f *fakeET) {
			unassigned(f)
			f.reply(put, http.StatusForbidden, `{"message":"Permission denied"}`)
		},
			args: []string{"errors", "assignees", "add", "42", "u-alex", "--project", "314"}, calls: []string{"GET projects/314/errors/42", list, put},
			exit: gcxerrors.ExitAuthFailure, check: assertRoleOnly("Editor or Admin")},
		{name: "a 403 that names only the role gets Viewer advice on the list", setup: func(f *fakeET) { f.reply(list, http.StatusForbidden, roleOnly403) },
			args: []string{"errors", "assignees", "list", "42", "--project", "314"}, calls: []string{list},
			exit: gcxerrors.ExitAuthFailure, check: assertRoleOnly("Viewer, Editor or Admin")},
		{name: "a signed-in user without the role gets a clear 403 on the list", setup: func(f *fakeET) { f.reply(list, http.StatusForbidden, serviceAccount403) },
			args: []string{"errors", "assignees", "list", "42", "--project", "314", "-o", "json"}, calls: []string{list}, exit: gcxerrors.ExitAuthFailure, check: assertHumanOnly},
		{name: "a bad error ID", args: []string{"errors", "assignees", "delete", "x", "u-alex", "--project", "314"}, exit: gcxerrors.ExitUsageError, err: `<error> "x" must be a positive error ID`},
		{name: "a missing user", args: []string{"errors", "assignees", "add", "42", "--project", "314"}, exit: gcxerrors.ExitUsageError, err: "accepts 2 arg(s), received 1"},
	})
}

func TestComments(t *testing.T) {
	t.Setenv("GCX_AUTO_APPROVE", "")
	os.Unsetenv("GCX_AUTO_APPROVE")
	t.Cleanup(agent.ResetForTesting)
	agent.SetFlag(false)

	f := newFakeET(t)
	const create, edit, del = "POST projects/314/errors/42/comments", "PATCH projects/314/errors/42/comments/7", "DELETE projects/314/errors/42/comments/7"
	entry := `{"id":"comment:7","type":"comment","createdAt":"2026-11-17T10:00:00Z","actor":"u1","comment":{"id":7,"body":"Caused by X.","canEdit":true}}`
	f.reply("GET projects/314", 200, `{"id":314,"name":"Checkout API","stats":{"dailyEvents":[]}}`)
	f.reply(create, http.StatusCreated, entry)
	f.reply(edit, 200, entry)
	f.reply(del, 200, `{"status":"deleted"}`)

	mutation := func(action string) string {
		return `{"type":"gcx.mutation","schema_version":"1","action":"` + action + `","changed":true,"errorId":"42","project":{"id":314},"target":{"kind":"Comment","id":"7","namespace":"stacks-123"}}`
	}
	comments := func(verb string, args ...string) []string {
		return append([]string{"errors", "comments", verb, "42"}, args...)
	}
	f.runCases(t, []cliCase{
		{name: "create trims the text", args: comments("create", "--project", "314", "--body", "  Caused by X.\n", "-o", "json"),
			calls: []string{create}, body: `{"body":"Caused by X."}`, json: mutation("created")},
		{name: "create as text names the error and the project", args: comments("create", "--project", "314", "--body", "Caused by X."), calls: []string{create},
			has: []string{"Created comment 7 on error 42 in project 314"},
			check: func(t *testing.T, _ string, _ error, _ []etCall) {
				t.Helper()
				assert.NotContains(t, f.stderr.String(), "hint:")
			}},
		{name: "a create whose response has no comment succeeds and says the ID is missing", setup: func(f *fakeET) { f.reply(create, 201, `{}`) },
			args: comments("create", "--project", "314", "--body", "x"), calls: []string{create},
			has: []string{"Created a comment on error 42 in project", "the response did not include its ID"}},
		{name: "an update whose response has no comment keeps the argument ID", setup: func(f *fakeET) { f.reply(edit, 200, `{}`) },
			args: comments("update", "7", "--project", "314", "--body", "x", "-o", "json"), calls: []string{edit}, has: []string{`"id": "7"`}},
		{name: "a comment ID too large is a usage error", args: comments("update", "9223372036854775808", "--project", "314", "--body", "Fixed."),
			exit: gcxerrors.ExitUsageError, err: `<comment> "9223372036854775808" must be a positive comment ID`},
		{name: "and so is one past the safe range", args: comments("delete", "9007199254740992", "--project", "314", "--force"),
			exit: gcxerrors.ExitUsageError, err: `<comment> "9007199254740992" must be a positive comment ID`},
		{name: "update", args: comments("update", "7", "--project", "314", "--body", "Fixed.", "-o", "json"),
			calls: []string{edit}, body: `{"body":"Fixed."}`, json: mutation("updated")},
		{name: "someone else's comment", setup: func(f *fakeET) {
			f.reply(edit, http.StatusForbidden, `{"error":{"code":"forbidden","message":"only the author can edit a comment"}}`)
		}, args: comments("update", "7", "--project", "314", "--body", "Fixed."), calls: []string{edit}, exit: gcxerrors.ExitAuthFailure, err: "only the author can edit a comment"},
		{name: "a declined prompt deletes nothing", stdin: "n\n", args: comments("delete", "7", "--project", "314", "-o", "json"),
			exit: gcxerrors.ExitCancelled, stderr: "Delete comment 7 on Error Tracking error 42 in project 314?"},
		{name: "a confirmed prompt deletes", stdin: "y\n", args: comments("delete", "7", "--project", "314"), calls: []string{del}, has: []string{"Deleted comment 7 on error 42 in project 314"}},
		{name: "update as text", setup: func(f *fakeET) { f.reply(edit, 200, entry) }, args: comments("update", "7", "--project", "314", "--body", "Fixed."),
			calls: []string{edit}, has: []string{"Edited comment 7 on error 42 in project 314"}},
		{name: "--force deletes at once", args: comments("delete", "7", "--project", "314", "--force", "-o", "json"), calls: []string{del}, json: mutation("deleted")},
		{name: "agent mode refuses without --force", setup: asAgent, args: comments("delete", "7", "--project", "314"),
			exit: gcxerrors.ExitUsageError, check: func(t *testing.T, _ string, err error, _ []etCall) {
				t.Helper()
				assert.Contains(t, rendered(t, err), "destructive operation in agent mode: use --force to proceed")
				agent.SetFlag(false)
			}},
		{name: "the length counts a CRLF as one character", args: comments("create", "--project", "314", "--body", strings.Repeat("a", 4999)+"\r\n"+strings.Repeat("b", 5000), "-o", "json"),
			calls: []string{create}, body: `{"body":"` + strings.Repeat("a", 4999) + `\n` + strings.Repeat("b", 5000) + `"}`},
		{name: "agent mode refuses before looking up the project", setup: asAgent, args: comments("delete", "7", "--project", "checkout-api"),
			exit: gcxerrors.ExitUsageError, check: func(t *testing.T, _ string, err error, _ []etCall) {
				t.Helper()
				assert.Contains(t, rendered(t, err), "destructive operation in agent mode: use --force to proceed")
				agent.SetFlag(false)
			}},
	}, usageCases(func(args ...string) []string { return append([]string{"errors"}, args...) },
		[]string{"comments", "create", "42", "--project", "314", "--body", " "},
		[]string{"comments", "create", "42", "--project", "314", "--body", strings.Repeat("é", 10001)},
		[]string{"comments", "create", "x", "--project", "314", "--body", "x"},
		[]string{"comments", "create", "42", "--project", "314"},
		[]string{"comments", "update", "42", "x", "--project", "314", "--body", "x"},
		[]string{"comments", "delete", "42", "0", "--project", "314", "--force"},
	))
}
