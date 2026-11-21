package errortracking_test

import (
	"slices"
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
)

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
