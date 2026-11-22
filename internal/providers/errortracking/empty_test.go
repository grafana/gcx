package errortracking_test

import (
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
)

func TestEmptyListsSayWhy(t *testing.T) {
	f := newFakeET(t)
	for route, body := range map[string]string{
		"GET projects/314/releases":           `{"releases":[]}`,
		"GET projects/314/environments":       `{"environments":[]}`,
		"GET projects/314/transactions":       `{"transactions":[]}`,
		"GET projects/314/customFields":       `{"keys":[]}`,
		"GET heatmaps":                        `{"heatmaps":[]}`,
		"GET projects/314/errors/42/activity": `{"activity":[]}`,
		"GET heatmaps/3/data/breakdown":       `{"from":"2026-11-16T00:00:00Z","to":"2026-11-17T00:00:00Z","value":"eu","projects":[]}`,
	} {
		f.reply(route, 200, body)
	}
	const lag = "Counts can lag a minute or two behind new events; raw-events list shows arrivals at once"
	f.runCases(t, []cliCase{
		{name: "releases list", args: []string{"releases", "list", "--project", "314"}, calls: []string{"GET projects/314/releases"},
			stderr: "hint: No releases in project 314; an event carries one when the SDK sets its release option. " + lag},
		{name: "releases list in a window", args: []string{"releases", "list", "--project", "314", "--since", "1h"}, calls: []string{"GET projects/314/releases"},
			stderr: "hint: No releases in project 314 in that window;"},
		{name: "environments list", args: []string{"environments", "list", "--project", "314"}, calls: []string{"GET projects/314/environments"},
			stderr: "hint: No environments in project 314; an event carries one when the SDK sets its environment option. " + lag},
		{name: "transactions list", args: []string{"transactions", "list", "--project", "314"}, calls: []string{"GET projects/314/transactions"},
			stderr: "hint: No transactions in project 314; an error carries one when the SDK names the route, job or screen. " + lag},
		{name: "fields list", args: []string{"fields", "list", "--project", "314"}, calls: []string{"GET projects/314/customFields"},
			stderr: "hint: No custom fields in project 314; an event carries them when the SDK sets tags or contexts. " + lag},
		{name: "heatmaps list", args: []string{"heatmaps", "list"}, calls: []string{"GET heatmaps"},
			stderr: "hint: No heatmaps yet; create one: gcx error-tracking heatmaps create --name <name> --field <key> --window-days 7 --projects <project>"},
		{name: "errors activity list of one type", args: []string{"errors", "activity", "list", "42", "--project", "314", "--type", "comment"},
			calls:  []string{"GET projects/314/errors/42/activity"},
			stderr: "hint: No comment entries yet on error 42 in project 314; comment on it: gcx error-tracking errors comments create 42 --project 314 --body <text>"},
		{name: "heatmaps breakdown", args: []string{"heatmaps", "breakdown", "3", "--value", "eu", "--from", "2026-11-16T00:00:00Z", "--to", "2026-11-17T00:00:00Z"},
			calls:  []string{"GET heatmaps/3/data/breakdown"},
			stderr: `hint: No events with value "eu" from 2026-11-16 00:00Z to 2026-11-17 00:00Z; the values with events: gcx error-tracking heatmaps report 3`},
		{name: "JSON keeps stdout one empty list", args: []string{"releases", "list", "--project", "314", "-o", "json"},
			calls: []string{"GET projects/314/releases"}, json: `{"items":[]}`, stderr: "No releases in project 314"},
		{name: "agent mode gets a hint line", setup: asAgent, args: []string{"heatmaps", "list"}, calls: []string{"GET heatmaps"},
			stderr: `{"class":"hint","summary":"No heatmaps yet; create one"`,
			check: func(t *testing.T, _ string, _ error, _ []etCall) {
				t.Helper()
				agent.SetFlag(false)
			}},
		{name: "field discovery says nothing", args: []string{"transactions", "list", "--project", "314", "--json", "list"}, calls: []string{"GET projects/314/transactions"},
			check: func(t *testing.T, _ string, _ error, _ []etCall) {
				t.Helper()
				assert.NotContains(t, f.stderr.String(), "hint:")
			}},
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
		missing("heatmaps breakdown --value", []string{"heatmaps", "breakdown", "3"}, "value"),
		missing("heatmaps create --name", []string{"heatmaps", "create", "--field", "region", "--window-days", "7", "--projects", "314"}, "name"),
		missing("heatmaps create --field", []string{"heatmaps", "create", "--name", "x", "--window-days", "7", "--projects", "314"}, "field"),
		missing("heatmaps create --window-days", []string{"heatmaps", "create", "--name", "x", "--field", "region", "--projects", "314"}, "window-days"),
		missing("heatmaps create --projects", []string{"heatmaps", "create", "--name", "x", "--field", "region", "--window-days", "7"}, "projects"),
	})
}

func TestMissingListsAreEmptyNotNull(t *testing.T) {
	f := newFakeET(t)
	f.reply("GET heatmaps/3", 200, `{"id":3,"name":"x","customFieldKey":"region","windowDays":7}`)
	f.reply("GET heatmaps/3/data", 200, `{"bucket":"1d","from":"2026-11-10T00:00:00Z","to":"2026-11-17T00:00:00Z"}`)
	f.reply("GET heatmaps/3/data/breakdown", 200, `{"from":"2026-11-16T00:00:00Z","to":"2026-11-17T00:00:00Z"}`)
	f.reply("GET projects/314", 200, `{"id":314,"name":"web","stats":{}}`)
	f.reply("GET projects/314/errors/42", 200, `{"id":42,"projectId":314,"status":"unresolved"}`)
	f.reply("GET projects/314/errors/42/timeline", 200, `{"bucket":"1h"}`)
	f.runCases(t, []cliCase{
		{name: "an error without history", args: []string{"errors", "get", "42", "--project", "314", "--jq", ".history", "-o", "json"},
			calls: []string{"GET projects/314/errors/42"}, json: `[]`},
		{name: "a timeline without buckets", args: []string{"errors", "timeline", "42", "--project", "314", "--since", "1h", "--jq", ".buckets", "-o", "json"},
			calls: []string{"GET projects/314/errors/42/timeline"}, json: `[]`},
		{name: "a heatmap without projectIds", args: []string{"heatmaps", "get", "3", "--jq", ".spec.projectIds", "-o", "json"},
			calls: []string{"GET heatmaps/3"}, json: `[]`},
		{name: "a report without cells", args: []string{"heatmaps", "report", "3", "--jq", ".cells", "-o", "json"},
			calls: []string{"GET heatmaps/3/data"}, json: `[]`},
		{name: "a breakdown without projects", args: []string{"heatmaps", "breakdown", "3", "--value", "eu", "--since", "1d", "--jq", ".projects", "-o", "json"},
			calls: []string{"GET heatmaps/3/data/breakdown"}, json: `[]`},
		{name: "project stats without dailyEvents", args: []string{"projects", "get", "314", "--jq", ".spec.stats.dailyEvents", "-o", "json"},
			calls: []string{"GET projects/314"}, json: `[]`},
		{name: "and YAML says the same", args: []string{"projects", "get", "314", "-o", "yaml"},
			calls: []string{"GET projects/314"}, has: []string{"dailyEvents: []"}},
	})
}
