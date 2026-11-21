package errortracking_test

import (
	"testing"
)

func TestMissingListsAreEmptyNotNull(t *testing.T) {
	f := newFakeET(t)
	f.reply("GET projects/314", 200, `{"id":314,"name":"web","stats":{}}`)
	f.runCases(t, []cliCase{
		{name: "project stats without dailyEvents", args: []string{"projects", "get", "314", "--jq", ".spec.stats.dailyEvents", "-o", "json"},
			calls: []string{"GET projects/314"}, json: `[]`},
		{name: "and YAML says the same", args: []string{"projects", "get", "314", "-o", "yaml"},
			calls: []string{"GET projects/314"}, has: []string{"dailyEvents: []"}},
	})
}
