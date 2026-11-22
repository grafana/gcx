package errortracking_test

import (
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
)

func TestMissingRequiredFlagsSayWhat(t *testing.T) {
	f := newFakeET(t)
	missing := func(name string, args []string, flag string) cliCase {
		return cliCase{name: name, args: args, exit: gcxerrors.ExitUsageError, err: `required flag(s) "` + flag + `" not set`}
	}
	f.runCases(t, []cliCase{
		missing("projects create --name", []string{"projects", "create"}, "name"),
	})
}

func TestMissingListsAreEmptyNotNull(t *testing.T) {
	f := newFakeET(t)
	f.reply("GET projects/314", 200, `{"id":314,"name":"web","stats":{}}`)
	f.reply("GET projects/314/errors/42", 200, `{"id":42,"projectId":314,"status":"unresolved"}`)
	f.runCases(t, []cliCase{
		{name: "an error without history", args: []string{"errors", "get", "42", "--project", "314", "--jq", ".history", "-o", "json"},
			calls: []string{"GET projects/314/errors/42"}, json: `[]`},
		{name: "project stats without dailyEvents", args: []string{"projects", "get", "314", "--jq", ".spec.stats.dailyEvents", "-o", "json"},
			calls: []string{"GET projects/314"}, json: `[]`},
		{name: "and YAML says the same", args: []string{"projects", "get", "314", "-o", "yaml"},
			calls: []string{"GET projects/314"}, has: []string{"dailyEvents: []"}},
	})
}
