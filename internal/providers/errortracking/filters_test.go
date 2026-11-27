package errortracking_test

import (
	"testing"
)

func TestFilters(t *testing.T) {
	f := newFakeET(t)
	f.reply("GET projects/314/timeline", 200, `{"bucket":"5m","buckets":[]}`)

	const from, to = "2026-11-16T00:00:00Z", "2026-11-17T00:00:00Z"
	window := func(args ...string) []string { return append([]string{"--from", from, "--to", to}, args...) }
	f.runCases(t, []cliCase{
		{name: "projects timeline filters and bucket", args: append([]string{"projects", "timeline", "314", "-o", "json",
			"--step", "5m", "--status", "unresolved", "--search", "TypeError", "--transaction", "/checkout", "--release", "v1.2",
			"--custom-field", "browser:Chrome"}, window()...),
			calls: []string{"GET projects/314/timeline"}, query: map[string][]string{
				"from": {from}, "to": {to}, "bucket": {"5m"}, "status": {"unresolved"}, "search": {"TypeError"},
				"transaction": {"/checkout"}, "release": {"v1.2"}, "customField": {"browser:Chrome"},
			}},
		{name: "projects timeline keeps a literal all for every filter but --status", args: []string{"projects", "timeline", "314", "-o", "json",
			"--status", "all", "--search", "all", "--transaction", "all", "--release", "all", "--from", from, "--to", to},
			calls: []string{"GET projects/314/timeline"}, query: map[string][]string{"from": {from}, "to": {to}, "search": {"all"}, "transaction": {"all"}, "release": {"all"}}},
	}, usageCases(func(args ...string) []string { return args },
		[]string{"projects", "timeline", "314", "--step", "2h"},
		[]string{"projects", "timeline", "314", "--status", "new"},
		[]string{"projects", "timeline", "314", "--search", ""},
		[]string{"projects", "timeline", "314", "--custom-field", " "},
	))
}
