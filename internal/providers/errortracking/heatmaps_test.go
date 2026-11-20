package errortracking_test

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const twoHeatmaps = `{"heatmaps":[
 {"id":3,"name":"Checkout by region","customFieldKey":"region","windowDays":14,"projectIds":[314,5]},
 {"id":4,"name":"tenants","customFieldKey":"tenant","windowDays":7,"projectIds":[2]}]}`

const threeHeatmaps = `{"heatmaps":[
 {"id":3,"name":"Checkout by region","customFieldKey":"geo","windowDays":14,"projectIds":[314,5]},
 {"id":4,"name":"tenants","customFieldKey":"tenant","windowDays":7,"projectIds":[2,77]},
 {"id":6,"name":"everything","customFieldKey":"release","windowDays":30,"projectIds":[314,5,2,77]}]}`

const heatmap3 = `{"id":3,"name":"Checkout by region","customFieldKey":"geo","windowDays":14,"projectIds":[314,5]}`

func squash(s string) string { return strings.Join(strings.Fields(s), " ") }

func lines(out string) []string {
	var got []string
	for l := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		got = append(got, squash(l))
	}
	return got
}

func row(t *testing.T, out, first string) string {
	t.Helper()
	for _, l := range lines(out) {
		if strings.HasPrefix(l, first+" ") {
			return l
		}
	}
	t.Fatalf("no row starts with %q in:\n%s", first, out)
	return ""
}

func TestHeatmapsListGet(t *testing.T) {
	f := newFakeET(t)
	f.reply("GET heatmaps", 200, threeHeatmaps)
	f.reply("GET heatmaps/3", 200, heatmap3)
	f.reply("GET projects", 200, twoProjects)

	list := []string{"GET heatmaps", "GET projects"}
	f.runCases(t, []cliCase{
		{name: "list as JSON keeps project IDs", args: []string{"heatmaps", "list", "--jq", "[(.items | length), .items[0].kind, .items[0].metadata.name, .items[0].spec.projectIds, .items[2].spec.projectIds]", "-o", "json"},
			calls: list, json: `[3,"Heatmap","checkout-by-region-3",[314,5],[314,5,2,77]]`},
		{name: "a failed project lookup falls back to IDs with one warning", setup: func(f *fakeET) {
			f.reply("GET projects", 500, `{"error":{"code":"x","message":"server says no"}}`)
		}, args: []string{"heatmaps", "list", "-o", "table"}, calls: []string{"GET heatmaps", "GET projects"},
			check: func(t *testing.T, out string, _ error, _ []etCall) {
				t.Helper()
				assert.Contains(t, lines(out), "checkout-by-region-3 geo 14d 314, 5")
				assert.Equal(t, 1, strings.Count(f.stderr.String(), "could not read project names; showing project IDs"), f.stderr.String())
			}},
		{name: "and JSON stays reachable", setup: func(f *fakeET) {
			f.reply("GET projects", 500, `{"error":{"code":"x","message":"server says no"}}`)
		}, args: []string{"heatmaps", "get", "3", "--jq", ".spec.projectIds", "-o", "json"}, calls: []string{"GET heatmaps/3", "GET projects"},
			json: `[314,5]`},
		{name: "list as a table names up to 3 projects", args: []string{"heatmaps", "list", "-o", "table"},
			calls: list, check: func(t *testing.T, out string, _ error, _ []etCall) {
				t.Helper()
				assert.Equal(t, []string{
					"NAME FIELD WINDOW PROJECTS",
					"checkout-by-region-3 geo 14d Checkout API, api-2",
					"tenants-4 tenant 7d web, 77",
					"everything-6 release 30d 4 projects",
				}, lines(out))
			}},
		{name: "list wide adds the project IDs", args: []string{"heatmaps", "list", "-o", "wide"},
			calls: list, check: func(t *testing.T, out string, _ error, _ []etCall) {
				t.Helper()
				assert.Equal(t, []string{
					"NAME FIELD WINDOW PROJECTS PROJECT IDS UPDATED CREATED",
					"checkout-by-region-3 geo 14d Checkout API, api-2 314,5 - -",
					"tenants-4 tenant 7d web, 77 2,77 - -",
					"everything-6 release 30d 4 projects 314,5,2,77 - -",
				}, lines(out))
			}},
		{name: "list as YAML lists the projects too and keeps their IDs", args: []string{"heatmaps", "list", "-o", "yaml"},
			calls: list, has: []string{"name: checkout-by-region-3", "projectIds:", "- 314"}, hasNot: []string{"Checkout API"}},
		{name: "get by ID", args: []string{"heatmaps", "get", "3", "-o", "json"},
			calls: []string{"GET heatmaps/3", "GET projects"}, has: []string{`"kind": "Heatmap"`, `"name": "checkout-by-region-3"`}, hasNot: []string{"Checkout API"}},
		{name: "get as a table", args: []string{"heatmaps", "get", "3"},
			calls: []string{"GET heatmaps/3", "GET projects"}, check: func(t *testing.T, out string, _ error, _ []etCall) {
				t.Helper()
				assert.Equal(t, []string{"NAME FIELD WINDOW PROJECTS", "checkout-by-region-3 geo 14d Checkout API, api-2"}, lines(out))
			}},
		{name: "get by NAME lists the heatmaps first", args: []string{"heatmaps", "get", "checkout-by-region-3", "-o", "json"},
			calls: []string{"GET heatmaps", "GET heatmaps/3", "GET projects"}, has: []string{`"name": "checkout-by-region-3"`}},
		{name: "get an unknown name", args: []string{"heatmaps", "get", "nope", "-o", "json"},
			calls: []string{"GET heatmaps"}, exit: gcxerrors.ExitGeneralError, err: `no Error Tracking heatmap has the ID or name "nope"`,
			check: func(t *testing.T, _ string, err error, _ []etCall) {
				t.Helper()
				assert.Contains(t, rendered(t, err), "List what exists: gcx error-tracking heatmaps list")
			}},
		{name: "list none needs no projects and hints at create", setup: func(f *fakeET) { f.reply("GET heatmaps", 200, `{"heatmaps":[]}`) },
			args: []string{"heatmaps", "list", "-o", "json"}, calls: []string{"GET heatmaps"}, json: `{"items":[]}`,
			stderr: "No heatmaps yet; create one: gcx error-tracking heatmaps create --name <name> --field <key> --window-days 7 --projects <project>"},
		{name: "a failed project lookup still lists, and says why names are missing", setup: func(f *fakeET) {
			f.reply("GET heatmaps", 200, twoHeatmaps)
			f.reply("GET projects", 500, `{"error":{"code":"x","message":"server says no"}}`)
		}, args: []string{"heatmaps", "list"}, calls: list, stderr: "server says no"},
	})
}

func TestHeatmapsCreateUpdate(t *testing.T) {
	f := newFakeET(t)
	f.reply("GET projects", 200, twoProjects)
	created := `{"id":9,"name":"Checkout by region","customFieldKey":"region","windowDays":14,"projectIds":[314,5]}`
	f.reply("POST heatmaps", 201, created)
	f.reply("PATCH heatmaps/9", 200, created)

	create := func(args ...string) []string {
		return replaced([]string{"heatmaps", "create", "--name", "x", "--field", "region", "--window-days", "7", "--projects", "314", "-o", "json"}, args...)
	}
	update := func(args ...string) []string {
		return append([]string{"heatmaps", "update", "9", "-o", "json"}, args...)
	}
	cases := []cliCase{
		{name: "create", args: []string{"heatmaps", "create", "--name", "Checkout by region", "--field", "region",
			"--window-days", "14", "--projects", "314", "--projects", "api-2", "-o", "json"},
			calls: []string{"GET projects", "POST heatmaps"},
			body:  `{"name":"Checkout by region","customFieldKey":"region","windowDays":14,"projectIds":[314,5]}`,
			json: `{"type":"gcx.mutation","schema_version":"1","action":"created","changed":true,
				"target":{"kind":"Heatmap","name":"Checkout by region","id":"9","namespace":"stacks-123"}}`},
		{name: "create prints the name", args: create("-o", "text"),
			calls: []string{"POST heatmaps"}, body: `{"name":"x","customFieldKey":"region","windowDays":7,"projectIds":[314]}`,
			has: []string{"Created heatmap Checkout by region (ID 9)"}},
		{name: "create needs every flag", args: []string{"heatmaps", "create", "--name", "x", "--field", "region", "--projects", "314", "-o", "json"},
			exit: gcxerrors.ExitUsageError, err: `required flag(s) "window-days" not set`},
		{name: "create takes a 90-day window", args: create("--window-days", "90"),
			calls: []string{"POST heatmaps"}, body: `{"name":"x","customFieldKey":"region","windowDays":90,"projectIds":[314]}`},
		{name: "create takes dots, dashes and underscores", args: create("--field", "app.tenant_id-2"),
			calls: []string{"POST heatmaps"}, body: `{"name":"x","customFieldKey":"app.tenant_id-2","windowDays":7,"projectIds":[314]}`},
		{name: "a 700-day window", args: create("--window-days", "700"),
			exit: gcxerrors.ExitUsageError, err: "--window-days 700: use 1 to 90"},
		{name: "a 0-day window", args: create("--window-days", "0"),
			exit: gcxerrors.ExitUsageError, err: "--window-days 0: use 1 to 90"},
		{name: "a name over 255 characters", args: create("--name", strings.Repeat("n", 256)),
			exit: gcxerrors.ExitUsageError, err: "--name is 256 characters; use at most 255"},
		{name: "a bad window with a good field and a project name", args: create("--field", "region", "--window-days", "91", "--projects", "checkout-api"),
			exit: gcxerrors.ExitUsageError, err: "--window-days 91: use 1 to 90"},
		{name: "update sends only the flags passed", args: update("--window-days", "30"),
			calls: []string{"PATCH heatmaps/9"}, body: `{"windowDays":30}`},
		{name: "update replaces the projects", args: update("--projects", "314", "--projects", "2", "--name", "n"),
			calls: []string{"PATCH heatmaps/9"}, body: `{"name":"n","projectIds":[314,2]}`},
		{name: "update without flags", args: update(),
			exit: gcxerrors.ExitUsageError, err: "pass at least one of --name, --field, --window-days or --projects"},
		{name: "a repeated project is sent once", args: update("--projects", "314", "--projects", "314", "--projects", "2"),
			calls: []string{"PATCH heatmaps/9"}, body: `{"projectIds":[314,2]}`},
		{name: "update lists projects once for NAMEs", args: update("--projects", "checkout-api-314", "--projects", "api-2"),
			calls: []string{"GET projects", "PATCH heatmaps/9"}, body: `{"projectIds":[314,5]}`},
		{name: "update checks the window", args: update("--window-days", "-1"),
			exit: gcxerrors.ExitUsageError, err: "--window-days -1: use 1 to 90"},
		{name: "update by name checks before any lookup", args: []string{"heatmaps", "update", "Checkout by region", "--name", strings.Repeat("n", 300), "-o", "json"},
			exit: gcxerrors.ExitUsageError, err: "--name is 300 characters; use at most 255"},
	}
	f.runCases(t, cases, usageCases(update, []string{"--projects", "0"}, []string{"--projects", ""}, []string{"--name", " "}, []string{"--field", ""}))
}

const reportWindow = `{"bucket":"1d","from":"2026-11-03T00:00:00Z","to":"2026-11-17T00:00:00Z","cells":[
 {"bucket":"2026-11-16T00:00:00Z","value":"eu","eventCount":12},
 {"bucket":"2026-11-15T00:00:00Z","value":"eu","eventCount":3},
 {"bucket":"2026-11-16T00:00:00Z","value":"us","eventCount":20},
 {"bucket":"2026-11-14T00:00:00Z","value":"apac","eventCount":2},
 {"bucket":"2026-11-16T00:00:00Z","value":"","eventCount":2}]}`

const reportDay = `{"bucket":"1h","from":"2026-11-16T00:00:00Z","to":"2026-11-17T00:00:00Z","cells":[
 {"bucket":"2026-11-16T03:00:00Z","value":"eu","eventCount":4},
 {"bucket":"2026-11-16T15:00:00Z","value":"eu","eventCount":2},
 {"bucket":"2026-11-16T03:00:00Z","value":"x\u001b[31my","eventCount":1}]}`

const reportEmpty = `{"bucket":"1d","from":"2026-11-11T00:00:00Z","to":"2026-11-18T00:00:00Z","cells":[]}`

func TestHeatmapsReport(t *testing.T) {
	f := newFakeET(t)
	const route = "GET heatmaps/3/data"
	with := func(body string) func(*fakeET) {
		return func(f *fakeET) { f.reply(route, 200, body) }
	}
	window := url.Values{}
	day := url.Values{"day": {"2026-11-16T00:00:00Z"}}
	today := time.Now().UTC().Format(time.DateOnly)
	windowHint := "No events in 2026-11-11 … 2026-11-17. The window ends yesterday (UTC); for today"

	f.runCases(t, []cliCase{
		{name: "the window as a grid", setup: with(reportWindow), args: []string{"heatmaps", "report", "3"},
			calls: []string{route}, query: window, check: func(t *testing.T, out string, _ error, _ []etCall) {
				t.Helper()
				assert.Equal(t, []string{
					"VALUE 11-03 11-04 11-05 11-06 11-07 11-08 11-09 11-10 11-11 11-12 11-13 11-14 11-15 11-16 TOTAL",
					"us - - - - - - - - - - - - - 20 20",
					"eu - - - - - - - - - - - - 3 12 15",
					"(no fields) - - - - - - - - - - - - - 2 2",
					"apac - - - - - - - - - - - 2 - - 2",
				}, lines(out))
				assert.Empty(t, f.stderr.String())
			}},
		{name: "the window wide has full dates", setup: with(reportWindow), args: []string{"heatmaps", "report", "3", "-o", "wide"},
			calls: []string{route}, query: window, check: func(t *testing.T, out string, _ error, _ []etCall) {
				t.Helper()
				assert.Equal(t, "VALUE 2026-11-03 2026-11-04 2026-11-05 2026-11-06 2026-11-07 2026-11-08 2026-11-09 "+
					"2026-11-10 2026-11-11 2026-11-12 2026-11-13 2026-11-14 2026-11-15 2026-11-16 TOTAL", lines(out)[0])
				assert.Equal(t, "us - - - - - - - - - - - - - 20 20", row(t, out, "us"))
			}},
		{name: "cells only", setup: with(reportWindow), args: []string{"heatmaps", "report", "3", "--json", "cells"},
			calls: []string{route}, query: window, json: `{"cells":[
				{"bucket":"2026-11-16T00:00:00Z","value":"eu","eventCount":12},
				{"bucket":"2026-11-15T00:00:00Z","value":"eu","eventCount":3},
				{"bucket":"2026-11-16T00:00:00Z","value":"us","eventCount":20},
				{"bucket":"2026-11-14T00:00:00Z","value":"apac","eventCount":2},
				{"bucket":"2026-11-16T00:00:00Z","value":"","eventCount":2}]}`},
		{name: "one day per hour", setup: with(reportDay), args: []string{"heatmaps", "report", "3", "--day", "2026-11-16"},
			calls: []string{route}, query: day, check: func(t *testing.T, out string, _ error, _ []etCall) {
				t.Helper()
				assert.Equal(t, []string{
					"VALUE 00h 01h 02h 03h 04h 05h 06h 07h 08h 09h 10h 11h 12h 13h 14h 15h 16h 17h 18h 19h 20h 21h 22h 23h TOTAL",
					"eu - - - 4 - - - - - - - - - - - 2 - - - - - - - - 6",
					"x[31my - - - 1 - - - - - - - - - - - - - - - - - - - - 1",
				}, lines(out))
				assert.NotContains(t, out, "\x1b")
			}},
		{name: "one day wide has full hours", setup: with(reportDay), args: []string{"heatmaps", "report", "3", "--day", "2026-11-16", "-o", "wide"},
			calls: []string{route}, query: day, check: func(t *testing.T, out string, _ error, _ []etCall) {
				t.Helper()
				assert.Equal(t, "VALUE 2026-11-16 00:00Z 2026-11-16 01:00Z 2026-11-16 02:00Z 2026-11-16 03:00Z 2026-11-16 04:00Z "+
					"2026-11-16 05:00Z 2026-11-16 06:00Z 2026-11-16 07:00Z 2026-11-16 08:00Z 2026-11-16 09:00Z 2026-11-16 10:00Z "+
					"2026-11-16 11:00Z 2026-11-16 12:00Z 2026-11-16 13:00Z 2026-11-16 14:00Z 2026-11-16 15:00Z 2026-11-16 16:00Z "+
					"2026-11-16 17:00Z 2026-11-16 18:00Z 2026-11-16 19:00Z 2026-11-16 20:00Z 2026-11-16 21:00Z 2026-11-16 22:00Z "+
					"2026-11-16 23:00Z TOTAL", lines(out)[0])
				assert.Equal(t, "eu - - - 4 - - - - - - - - - - - 2 - - - - - - - - 6", row(t, out, "eu"))
			}},
		{name: "a day that is not a date", args: []string{"heatmaps", "report", "3", "--day", "yesterday", "-o", "json"},
			exit: gcxerrors.ExitUsageError, err: `invalid --day "yesterday": use a UTC date like 2026-11-16`},
		{name: "an empty --day", args: []string{"heatmaps", "report", "3", "--day", "", "-o", "json"},
			exit: gcxerrors.ExitUsageError, err: "--day must not be empty"},
		{name: "an empty window prints the header and a hint for today in UTC", setup: with(reportEmpty), args: []string{"heatmaps", "report", "3"},
			calls: []string{route}, query: window, check: func(t *testing.T, out string, _ error, _ []etCall) {
				t.Helper()
				assert.Equal(t, []string{"VALUE 11-11 11-12 11-13 11-14 11-15 11-16 11-17 TOTAL"}, lines(out))
				assert.Equal(t, "hint: "+windowHint+": gcx error-tracking heatmaps report 3 --day "+today+"\n", f.stderr.String())
			}},
		{name: "an empty window as JSON still hints", setup: with(reportEmpty), args: []string{"heatmaps", "report", "3", "-o", "json"},
			calls: []string{route}, query: window, json: reportEmpty, stderr: windowHint},
		{name: "an empty window in agent mode hints as JSON", setup: func(f *fakeET) {
			with(reportEmpty)(f)
			agent.SetFlag(true)
		}, args: []string{"heatmaps", "report", "3", "-o", "json"},
			calls: []string{route}, query: window, check: func(t *testing.T, _ string, _ error, _ []etCall) {
				t.Helper()
				assert.JSONEq(t, `{"class":"hint","summary":"`+windowHint+`","command":"gcx error-tracking heatmaps report 3 --day `+today+`"}`, f.stderr.String())
			}},
		{name: "a day without events", setup: with(`{"bucket":"1h","from":"2026-11-16T00:00:00Z","to":"2026-11-17T00:00:00Z","cells":[]}`),
			args: []string{"heatmaps", "report", "3", "--day", "2026-11-16", "-o", "json"}, calls: []string{route}, query: day,
			check: func(t *testing.T, _ string, _ error, _ []etCall) {
				t.Helper()
				assert.Equal(t, "hint: No events on 2026-11-16. Counts can lag a minute or two behind new events; raw-events list shows arrivals at once\n", f.stderr.String())
			}},
		{name: "50 values are cut at the API cap, in list_meta and in a hint", setup: func(f *fakeET) {
			cells := make([]string, 0, 50)
			for i := range 50 {
				cells = append(cells, fmt.Sprintf(`{"bucket":"2026-11-16T00:00:00Z","value":"v%d","eventCount":1}`, i))
			}
			f.reply(route, 200, `{"bucket":"1d","from":"2026-11-03T00:00:00Z","to":"2026-11-17T00:00:00Z","cells":[`+strings.Join(cells, ",")+`]}`)
		}, args: []string{"heatmaps", "report", "3", "-o", "json", "--jq", ".list_meta"},
			calls: []string{route}, query: window, json: `{"truncated":true,"returned":50,"cap":50}`, stderr: "hint: The API keeps only the 50 custom field values with the most events; the others are not shown"},
		{name: "49 values carry no list_meta", setup: func(f *fakeET) {
			cells := make([]string, 0, 49)
			for i := range 49 {
				cells = append(cells, fmt.Sprintf(`{"bucket":"2026-11-16T00:00:00Z","value":"v%d","eventCount":1}`, i))
			}
			f.reply(route, 200, `{"bucket":"1d","from":"2026-11-03T00:00:00Z","to":"2026-11-17T00:00:00Z","cells":[`+strings.Join(cells, ",")+`]}`)
		}, args: []string{"heatmaps", "report", "3", "-o", "json", "--jq", ".list_meta"},
			calls: []string{route}, query: window, json: `null`, check: func(t *testing.T, _ string, _ error, _ []etCall) {
				t.Helper()
				assert.Empty(t, f.stderr.String())
			}},
		{name: "field discovery prints no hint", setup: with(reportEmpty), args: []string{"heatmaps", "report", "3", "--json", "?"},
			calls: []string{route}, query: window, has: []string{"cells"}, check: func(t *testing.T, _ string, _ error, _ []etCall) {
				t.Helper()
				assert.Empty(t, f.stderr.String())
			}},
		{name: "an unparsable window falls back to the cells", setup: with(`{"bucket":"1d","from":"a","to":"b","cells":[]}`),
			args: []string{"heatmaps", "report", "3"}, calls: []string{route}, query: window, check: func(t *testing.T, out string, _ error, _ []etCall) {
				t.Helper()
				assert.Equal(t, []string{"VALUE TOTAL"}, lines(out))
				assert.Equal(t, "hint: No events in a … b. The window ends yesterday (UTC); for today: gcx error-tracking heatmaps report 3 --day "+today+"\n", f.stderr.String())
			}},
	})
}

func TestHeatmapsBreakdown(t *testing.T) {
	f := newFakeET(t)
	f.reply("GET heatmaps", 200, twoHeatmaps)
	f.reply("GET heatmaps/3", 200, heatmap3)
	f.reply("GET heatmaps/3/data/breakdown", 200, `{"from":"x","to":"y","value":"eu","projects":[{"projectId":314,"projectName":"Checkout API","eventCount":9}]}`)

	const route = "GET heatmaps/3/data/breakdown"
	fixed := url.Values{"value": {"eu"}, "from": {"2026-11-16T00:00:00Z"}, "to": {"2026-11-17T00:00:00Z"}}
	midnight := time.Now().UTC().Truncate(24 * time.Hour)
	windowOf := func(days int) url.Values {
		return url.Values{"value": {"eu"}, "from": {midnight.AddDate(0, 0, -days).Format(time.RFC3339)}, "to": {midnight.Format(time.RFC3339)}}
	}
	fourteenDays := windowOf(14)
	span := func(value string, d time.Duration) func(t *testing.T, _ string, _ error, calls []etCall) {
		return func(t *testing.T, _ string, _ error, calls []etCall) {
			t.Helper()
			require.Len(t, calls, 1)
			q := calls[0].Query
			assert.Equal(t, []string{value}, q["value"])
			from, err := time.Parse(time.RFC3339, q.Get("from"))
			require.NoError(t, err)
			to, err := time.Parse(time.RFC3339, q.Get("to"))
			require.NoError(t, err)
			assert.Equal(t, d, to.Sub(from))
		}
	}
	f.runCases(t, []cliCase{
		{name: "a fixed window", args: []string{"heatmaps", "breakdown", "3", "--value", "eu",
			"--from", "2026-11-16T00:00:00Z", "--to", "2026-11-17T00:00:00Z", "-o", "table"},
			calls: []string{route}, query: fixed, check: func(t *testing.T, out string, _ error, _ []etCall) {
				t.Helper()
				assert.Equal(t, []string{"PROJECT ID PROJECT EVENTS", "314 Checkout API 9"}, lines(out))
			}},
		{name: "events without custom fields", args: []string{"heatmaps", "breakdown", "3", "--value", "", "--since", "7d", "-o", "json"},
			calls: []string{route}, check: span("", 7*24*time.Hour)},
		{name: "no window uses the heatmap's up to today in UTC", args: []string{"heatmaps", "breakdown", "3", "--value", "eu", "-o", "json"},
			calls: []string{"GET heatmaps/3", route}, query: fourteenDays,
			stderr: "Heatmap Checkout by region (ID 3)'s 14-day window: " + midnight.AddDate(0, 0, -14).Format(time.DateOnly) + " … " + midnight.AddDate(0, 0, -1).Format(time.DateOnly) + " (UTC)"},
		{name: "no window by name", args: []string{"heatmaps", "breakdown", "Checkout by region", "--value", "eu"},
			calls: []string{"GET heatmaps", "GET heatmaps/3", route}, query: fourteenDays, has: []string{"Checkout API"}},
		{name: "no window in agent mode says nothing extra", setup: asAgent,
			args:  []string{"heatmaps", "breakdown", "3", "--value", "eu", "-o", "json"},
			calls: []string{"GET heatmaps/3", route}, query: fourteenDays, check: func(t *testing.T, _ string, _ error, _ []etCall) {
				t.Helper()
				assert.Empty(t, f.stderr.String())
			}},
		{name: "a heatmap with another window", setup: func(f *fakeET) {
			f.reply("GET heatmaps/3", 200, `{"id":3,"name":"Checkout by region","windowDays":90,"projectIds":[314]}`)
		}, args: []string{"heatmaps", "breakdown", "3", "--value", "eu", "-o", "json"},
			calls: []string{"GET heatmaps/3", route}, query: windowOf(90)},
		{name: "--since wins over the heatmap's window", args: []string{"heatmaps", "breakdown", "3", "--value", "eu", "--since", "1h", "-o", "json"},
			calls: []string{route}, check: span("eu", time.Hour)},
		{name: "--from without --to", args: []string{"heatmaps", "breakdown", "3", "--value", "eu", "--from", "2026-11-16T00:00:00Z", "-o", "json"},
			exit: gcxerrors.ExitUsageError, err: "--to is required when --from is set"},
		{name: "a reversed range is refused before any request", args: []string{"heatmaps", "breakdown", "3", "--value", "eu", "--from", "2026-11-17T00:00:00Z", "--to", "2026-11-16T00:00:00Z", "-o", "json"},
			exit: gcxerrors.ExitUsageError, err: `--from "2026-11-17T00:00:00Z" is not before --to "2026-11-16T00:00:00Z"`},
		{name: "and so is an empty one", args: []string{"errors", "timeline", "42", "--project", "314", "--from", "now", "--to", "now", "-o", "json"},
			exit: gcxerrors.ExitUsageError, err: `--from "now" is not before --to "now"`},
		{name: "no value", args: []string{"heatmaps", "breakdown", "3", "--since", "7d", "-o", "json"},
			exit: gcxerrors.ExitUsageError, err: `required flag(s) "value" not set`},
		{name: "no projects hints at the report", setup: func(f *fakeET) {
			f.reply(route, 200, `{"from":"2026-11-16T00:00:00Z","to":"2026-11-17T00:00:00Z","value":"eu","projects":[]}`)
		}, args: []string{"heatmaps", "breakdown", "3", "--value", "eu", "--from", "2026-11-16T00:00:00Z", "--to", "2026-11-17T00:00:00Z"},
			calls: []string{route}, query: fixed, check: func(t *testing.T, out string, _ error, _ []etCall) {
				t.Helper()
				assert.Equal(t, []string{"PROJECT ID PROJECT EVENTS"}, lines(out))
				assert.Equal(t, `hint: No events with value "eu" from 2026-11-16 00:00Z to 2026-11-17 00:00Z; the values with events: gcx error-tracking heatmaps report 3`+"\n", f.stderr.String())
			}},
	})
}

func TestHeatmapsDelete(t *testing.T) {
	t.Setenv("GCX_AUTO_APPROVE", "")
	os.Unsetenv("GCX_AUTO_APPROVE")

	f := newFakeET(t)
	f.reply("GET heatmaps", 200, twoHeatmaps)
	f.reply("GET heatmaps/3", 200, `{"id":3,"name":"Checkout by region"}`)
	f.reply("DELETE heatmaps/3", 200, `{"status":"deleted"}`)

	f.runCases(t, []cliCase{
		{name: "a declined prompt deletes nothing", stdin: "n\n", args: []string{"heatmaps", "delete", "3", "-o", "json"},
			calls: []string{"GET heatmaps/3"}, exit: gcxerrors.ExitCancelled, stderr: "Delete Error Tracking heatmap Checkout by region (ID 3)?"},
		{name: "a confirmed prompt deletes", stdin: "y\n", args: []string{"heatmaps", "delete", "checkout-by-region-3", "-o", "json"},
			calls: []string{"GET heatmaps", "GET heatmaps/3", "DELETE heatmaps/3"}, stderr: "Delete Error Tracking heatmap Checkout by region (ID 3)?",
			json: `{"type":"gcx.mutation","schema_version":"1","action":"deleted","changed":true,
				"target":{"kind":"Heatmap","id":"3","name":"Checkout by region","namespace":"stacks-123"}}`},
		{name: "closed stdin", args: []string{"heatmaps", "delete", "3", "-o", "json"},
			calls: []string{"GET heatmaps/3"}, exit: gcxerrors.ExitUsageError},
		{name: "agent mode refuses without --force", setup: asAgent, args: []string{"heatmaps", "delete", "3", "-o", "json"},
			exit: gcxerrors.ExitUsageError, err: "destructive operation in agent mode: use --force to proceed"},
		{name: "--force by ID looks up the name, then deletes", args: []string{"heatmaps", "delete", "3", "--force", "-o", "json"},
			calls: []string{"GET heatmaps/3", "DELETE heatmaps/3"}, json: `{"type":"gcx.mutation","schema_version":"1","action":"deleted","changed":true,
				"target":{"kind":"Heatmap","id":"3","name":"Checkout by region","namespace":"stacks-123"}}`},
	})
}

func TestHeatmapsCreateHelpMarksRequiredFlags(t *testing.T) {
	f := newFakeET(t)
	f.runCases(t, []cliCase{
		{name: "create", args: []string{"heatmaps", "create", "--help"}, check: func(t *testing.T, out string, _ error, _ []etCall) {
			t.Helper()
			for _, flag := range []string{"--name", "--field", "--window-days", "--projects"} {
				assert.Regexp(t, `(?m)^\s+`+flag+` \S+ .*\(required\)$`, out)
			}
		}},
		{name: "update", args: []string{"heatmaps", "update", "--help"}, hasNot: []string{"(required)"}},
		{name: "sourcemaps upsert names both Cloud scopes", args: []string{"sourcemaps", "upsert", "--help"},
			has: []string{"sourcemaps:write scope", "stacks:read to look up the stack and confirm its address"}},
		{name: "--status help says what new and regressed send", args: []string{"errors", "list", "--help"},
			has: []string{"regressed (resolved errors that came back)", "new (unresolved, first seen since the window start and not reviewed)"}},
		{name: "--project names no auto-pick", args: []string{"errors", "list", "--help"}, has: []string{"then the stack's default (projects use)\n"}, hasNot: []string{"only project"}},
	})
}
