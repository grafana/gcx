package errortracking_test

import (
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
)

func TestEmptyListsSayWhy(t *testing.T) {
	f := newFakeET(t)
	for route, body := range map[string]string{
		"GET projects/314/errors/42/activity": `{"activity":[]}`,
	} {
		f.reply(route, 200, body)
	}
	f.runCases(t, []cliCase{
		{name: "errors activity list of one type", args: []string{"errors", "activity", "list", "42", "--project", "314", "--type", "comment"},
			calls:  []string{"GET projects/314/errors/42/activity"},
			stderr: "hint: No comment entries yet on error 42 in project 314; comment on it: gcx error-tracking errors comments create 42 --project 314 --body <text>"},
	})
}

func TestMissingRequiredFlagsSayWhat(t *testing.T) {
	f := newFakeET(t)
	missing := func(name string, args []string, flag string) cliCase {
		return cliCase{name: name, args: args, exit: gcxerrors.ExitUsageError, err: `required flag(s) "` + flag + `" not set`}
	}
	f.runCases(t, []cliCase{
		missing("projects create --name", []string{"projects", "create"}, "name"),
		missing("comments create --body", []string{"errors", "comments", "create", "42", "--project", "314"}, "body"),
		missing("comments update --body", []string{"errors", "comments", "update", "42", "7", "--project", "314"}, "body"),
	})
}

func TestMissingListsAreEmptyNotNull(t *testing.T) {
	f := newFakeET(t)
	f.reply("GET projects/314", 200, `{"id":314,"name":"web","stats":{}}`)
	f.reply("GET projects/314/errors/42", 200, `{"id":42,"projectId":314,"status":"unresolved"}`)
	f.reply("GET projects/314/errors/42/timeline", 200, `{"bucket":"1h"}`)
	f.runCases(t, []cliCase{
		{name: "an error without history", args: []string{"errors", "get", "42", "--project", "314", "--jq", ".history", "-o", "json"},
			calls: []string{"GET projects/314/errors/42"}, json: `[]`},
		{name: "a timeline without buckets", args: []string{"errors", "timeline", "42", "--project", "314", "--since", "1h", "--jq", ".buckets", "-o", "json"},
			calls: []string{"GET projects/314/errors/42/timeline"}, json: `[]`},
		{name: "project stats without dailyEvents", args: []string{"projects", "get", "314", "--jq", ".spec.stats.dailyEvents", "-o", "json"},
			calls: []string{"GET projects/314"}, json: `[]`},
		{name: "and YAML says the same", args: []string{"projects", "get", "314", "-o", "yaml"},
			calls: []string{"GET projects/314"}, has: []string{"dailyEvents: []"}},
	})
}
