package errortracking_test

import (
	"errors"
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
)

const evil = `\u001b]0;owned\u0007\u001b[2J\u202eevil\u009b`

func assertNoEscapes(t *testing.T, where, s string) {
	t.Helper()
	for _, bad := range []string{"\x1b", "\a", "\x00", "\u009b", "\u202e"} {
		assert.NotContains(t, s, bad, "%s carries a control character: %q", where, s)
	}
}

func TestEscapeSequencesNeverReachTheTerminal(t *testing.T) {
	f := newFakeET(t)
	errorJSON := `{"id":42,"projectId":314,"projectName":"` + evil + `","status":"` + evil + `","exceptionType":"` + evil + `","message":"` + evil + `\n` + evil + `",
		"culprit":"` + evil + `","transaction":"` + evil + `","lastRelease":"` + evil + `","firstSeen":"` + evil + `","lastSeen":"` + evil + `",
		"latestEvent":{"eventId":"` + evil + `","occurredAt":"` + evil + `","environment":"` + evil + `","release":"` + evil + `","level":"` + evil + `","tags":{"` + evil + `":"` + evil + `"},
		"payload":{"exception":{"values":[{"stacktrace":{"frames":[{"filename":"` + evil + `","function":"` + evil + `"}]}}]}}},"history":[]}`
	for route, body := range map[string]string{
		"GET projects":                         `{"projects":[{"id":314,"name":"` + evil + `","stats":{"dailyEvents":[]}}]}`,
		"GET projects/314":                     `{"id":314,"name":"` + evil + `","stats":{"dailyEvents":[]}}`,
		"GET projects/314/errors/42":           errorJSON,
		"GET projects/314/errors/43":           `{"id":43,"projectName":"x","status":"unresolved","exceptionType":"` + evil + `","message":"` + evil + `","history":[]}`,
		"PATCH projects/314/errors/42":         `{"id":42,"status":"resolved","history":[]}`,
		"PATCH projects/314/errors/43":         `{"error":{"code":"invalid_transition","message":"` + evil + `","currentStatus":"` + evil + `"}}`,
		"GET projects/314/errors/42/activity":  `{"activity":[{"id":"x","type":"` + evil + `","createdAt":"` + evil + `","actor":"` + evil + `","actorName":"` + evil + `"}]}`,
		"GET projects/314/errors/42/assignees": `{"assignees":[{"userUid":"` + evil + `","name":"` + evil + `","login":"` + evil + `"}]}`,
		"GET projects/314/errors/42/events/e1": `{"eventId":"` + evil + `","occurredAt":"` + evil + `","release":"` + evil + `","payload":{}}`,
		"GET projects/314/rawEvents":           `{"rawEvents":[{"id":9,"eventId":"e","outcome":"` + evil + `","receivedAt":"` + evil + `"},{"id":8,"eventId":"e","outcome":"` + evil + `"}]}`,
		"GET projects/314/rawEvents/9":         `{"id":9,"eventId":"` + evil + `","outcome":"` + evil + `","receivedAt":"` + evil + `","payload":"` + evil + `"}`,
		"GET projects/314/releases":            `{"releases":[{"release":"` + evil + `","firstSeen":"` + evil + `","lastSeen":"` + evil + `"}]}`,
		"GET projects/314/errors":              `{"errors":[{"id":1,"status":"` + evil + `","exceptionType":"` + evil + `","message":"` + evil + `","lastRelease":"` + evil + `","lastSeen":"` + evil + `"}],"total":9,"nextCursor":"n"}`,
		"GET projects/314/users":               `{"users":[{"uid":"a","name":"` + evil + `","login":"x"},{"uid":"b","name":"` + evil + `","login":"x"}]}`,
	} {
		f.reply(route, 200, body)
	}
	f.reply("GET projects/314/errors/43/ignored", 200, `{}`)
	f.reply("PUT projects/314/errors/42/assignees/%1B%5B31mnobody%07%E2%80%AE%C2%9B", 204, "")
	f.reply("DELETE projects/314/errors/42/assignees/%1B%5D0%3Bowned%07%1B%5B2J%E2%80%AEevil%C2%9B", 204, "")
	f.reply("PATCH projects/314/errors/43", 409, `{"error":{"code":"invalid_transition","message":"`+evil+`","currentStatus":"`+evil+`"}}`)

	clean := func(t *testing.T, out string, err error, _ []etCall) {
		t.Helper()
		assertNoEscapes(t, "stdout", out)
		assertNoEscapes(t, "stderr", f.stderr.String())
		if _, emitted := errors.AsType[*gcxerrors.EmittedError](err); err != nil && !emitted {
			assertNoEscapes(t, "the error", rendered(t, err))
		}
	}
	run := func(name string, calls []string, args ...string) cliCase {
		return cliCase{name: name, args: args, calls: calls, check: clean}
	}
	const p, e42, e43 = "GET projects/314", "GET projects/314/errors/42", "GET projects/314/errors/43"
	const list = "GET projects/314/errors"
	f.runCases(t, []cliCase{
		run("errors inspect", []string{e42, e42 + "/activity", e42 + "/assignees"}, "errors", "inspect", "42", "--project", "314"),
		{name: "raw-events get by an ambiguous EVENT ID", args: []string{"raw-events", "get", "e", "--project", "314"}, calls: []string{"GET projects/314/rawEvents"},
			exit: 2, check: clean},
		run("releases status", []string{"GET projects/314/releases", list, list, list}, "releases", "status", "--project", "314", "--limit", "1"),
		{name: "a batch update in text", args: []string{"errors", "update", "42", "43", "--project", "314", "--status", "resolved"},
			calls: []string{e42, e43, "PATCH projects/314/errors/42", "PATCH projects/314/errors/43"}, anyOrd: true, exit: 4, check: clean},
		{name: "a refused single update", args: []string{"errors", "update", "43", "--project", "314", "--status", "resolved"},
			calls: []string{e43, "PATCH projects/314/errors/43"}, exit: 1, check: clean},
		run("errors list in wide", []string{list}, "errors", "list", "--project", "314", "--limit", "1", "-o", "wide"),
		{name: "a hostile <user> UID on assignees add", args: []string{"errors", "assignees", "add", "42", "\u001b[31mnobody\u0007\u202e\u009b", "--project", "314"},
			calls: []string{e42, e42 + "/assignees", "PUT projects/314/errors/42/assignees/%1B%5B31mnobody%07%E2%80%AE%C2%9B"}, check: clean},
		{name: "a hostile <user> UID that is not assigned on assignees delete", args: []string{"errors", "assignees", "delete", "42", "\u001b[31mnobody\u0007\u202e\u009b", "--project", "314"},
			calls: []string{e42, e42 + "/assignees"}, check: clean},
		{name: "an assigned user with hostile fields on assignees delete", args: []string{"errors", "assignees", "delete", "42", "\u001b]0;owned\u0007\u001b[2J\u202eevil\u009b", "--project", "314"},
			calls: []string{e42, e42 + "/assignees", "DELETE projects/314/errors/42/assignees/%1B%5D0%3Bowned%07%1B%5B2J%E2%80%AEevil%C2%9B"}, check: clean},
		{name: "a declined project delete prompt", stdin: "n\n", args: []string{"projects", "delete", "314"},
			calls: []string{p}, exit: 5, check: clean},
		run("projects use", []string{p}, "projects", "use", "314"),
		{name: "no project given and none picked", args: []string{"errors", "get", "42"}, exit: 2, check: clean},
		run("projects list names the default", []string{"GET projects"}, "projects", "list"),
		{name: "agent mode hints", setup: asAgent, args: []string{"releases", "status", "--project", "314", "--limit", "1"},
			calls: []string{"GET projects/314/releases", list, list, list},
			check: func(t *testing.T, _ string, _ error, _ []etCall) {
				t.Helper()
				assert.Contains(t, f.stderr.String(), `"class":"hint"`)
				assertNoEscapes(t, "stderr", f.stderr.String())
			}},
	})
}
