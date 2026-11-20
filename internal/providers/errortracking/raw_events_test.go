package errortracking_test

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
)

func TestRawEvents(t *testing.T) {
	f := newFakeET(t)
	const list, get = "GET projects/314/rawEvents", "GET projects/314/rawEvents/9"
	f.handle(list, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("cursor") {
		case "":
			_, _ = w.Write([]byte(`{"rawEvents":[{"id":9,"eventId":"e9","outcome":"accepted","size":512}],"nextCursor":"c1"}`))
		case "c1":
			_, _ = w.Write([]byte(`{"rawEvents":[{"id":8,"eventId":"e8","outcome":"dropped","size":64}]}`))
		}
	})
	f.reply(get, 200, `{"id":9,"eventId":"e9","outcome":"accepted","size":512,"payload":{"exception":{}}}`)

	f.runCases(t, []cliCase{
		{name: "an empty --event-id", args: []string{"raw-events", "list", "--project", "314", "--event-id", " ", "-o", "json"}, exit: gcxerrors.ExitUsageError},
		{name: "--limit 0 follows every page", args: []string{"raw-events", "list", "--project", "314", "--limit", "0", "--json", "id,outcome"},
			calls: []string{list, list}, json: `{"items":[{"id":9,"outcome":"accepted"},{"id":8,"outcome":"dropped"}]}`,
			check: func(t *testing.T, _ string, _ error, calls []etCall) {
				t.Helper()
				assert.Equal(t, "200", calls[0].Query.Get("limit"))
				assert.Empty(t, calls[0].Query.Get("eventId"), "no --event-id, no filter")
				assert.Equal(t, "c1", calls[1].Query.Get("cursor"))
			}},
		{name: "ET normalizes the event ID", args: []string{"raw-events", "list", "--project", "314", "--event-id", " E9 ", "--limit", "1", "-o", "json"},
			calls: []string{list}, has: []string{`"truncated": true`}, query: map[string][]string{"eventId": {"E9"}, "limit": {"1"}}},
		{name: "no items", setup: func(f *fakeET) { f.reply(list, 200, `{"rawEvents":[]}`) },
			args: []string{"raw-events", "list", "--project", "314", "-o", "json"}, calls: []string{list}, json: `{"items":[]}`},
		{name: "get selected fields", args: []string{"raw-events", "get", "9", "--project", "314", "--json", "outcome,size"},
			calls: []string{get}, json: `{"outcome":"accepted","size":512}`},
		{name: "get as a table", args: []string{"raw-events", "get", "9", "--project", "314", "-o", "table"},
			calls: []string{get}, has: []string{"accepted"}},
		{name: "get by EVENT ID looks the item up first", setup: func(f *fakeET) {
			f.reply(list, 200, `{"rawEvents":[{"id":9,"eventId":"e9","outcome":"accepted","size":512}]}`)
		}, args: []string{"raw-events", "get", "e9", "--project", "314", "--json", "outcome"},
			calls: []string{list, get}, json: `{"outcome":"accepted"}`, check: func(t *testing.T, _ string, _ error, calls []etCall) {
				t.Helper()
				assert.Equal(t, "e9", calls[0].Query.Get("eventId"))
			}},
		{name: "an EVENT ID in several items lists them", setup: func(f *fakeET) {
			f.reply(list, 200, `{"rawEvents":[{"id":9,"eventId":"e9","outcome":"accepted","receivedAt":"2026-11-16T10:00:00Z"},{"id":7,"eventId":"e9","outcome":"duplicate","receivedAt":"2026-11-16T09:00:00Z"}]}`)
		}, args: []string{"raw-events", "get", "e9", "--project", "314"}, calls: []string{list}, exit: gcxerrors.ExitUsageError,
			err: `event ID "e9" matches 2 items: 9 (accepted, 2026-11-16 10:00Z), 7 (duplicate, 2026-11-16 09:00Z)`},
		{name: "a full page of matches says it shows the first 50", setup: func(f *fakeET) {
			items := make([]string, 50)
			for i := range items {
				items[i] = fmt.Sprintf(`{"id":%d,"eventId":"e9","outcome":"accepted","receivedAt":"2026-11-16T10:00:00Z"}`, 100-i)
			}
			f.reply(list, 200, `{"rawEvents":[`+strings.Join(items, ",")+`]}`)
		}, args: []string{"raw-events", "get", "e9", "--project", "314"}, calls: []string{list}, exit: gcxerrors.ExitUsageError,
			err: `event ID "e9" matches 50 or more items; the first 10 are shown: 100 (accepted, 2026-11-16 10:00Z), 99`,
			check: func(t *testing.T, _ string, err error, _ []etCall) {
				t.Helper()
				out := rendered(t, err)
				assert.Contains(t, out, "91 (accepted, 2026-11-16 10:00Z)")
				assert.NotContains(t, out, "90 (accepted")
			}},
		{name: "a partial page of more than ten matches shows ten and says how many", setup: func(f *fakeET) {
			items := make([]string, 12)
			for i := range items {
				items[i] = fmt.Sprintf(`{"id":%d,"eventId":"e9","outcome":"accepted","receivedAt":"2026-11-16T10:00:00Z"}`, 100-i)
			}
			f.reply(list, 200, `{"rawEvents":[`+strings.Join(items, ",")+`]}`)
		}, args: []string{"raw-events", "get", "e9", "--project", "314"}, calls: []string{list}, exit: gcxerrors.ExitUsageError,
			err: `event ID "e9" matches 12 items; the first 10 are shown: 100 (accepted`},
		{name: "an unknown EVENT ID", exit: gcxerrors.ExitGeneralError, setup: func(f *fakeET) { f.reply(list, 200, `{"rawEvents":[]}`) }, args: []string{"raw-events", "get", "zzz", "--project", "314"},
			calls: []string{list}, err: `no item with event ID "zzz" in the latest items of project 314; Error Tracking keeps only the latest items, and event IDs are 32 hex characters`},
		{name: "an unknown --event-id says so on stderr", setup: func(f *fakeET) { f.reply(list, 200, `{"rawEvents":[]}`) }, args: []string{"raw-events", "list", "--project", "314", "--event-id", "zzz"},
			calls: []string{list}, stderr: `hint: No item with event ID "zzz" in the latest items of project 314. Event IDs are 32 hex characters`},
		{name: "an empty project points at the DSN", setup: func(f *fakeET) { f.reply(list, 200, `{"rawEvents":[]}`) }, args: []string{"raw-events", "list", "--project", "314"},
			calls: []string{list}, stderr: "hint: Nothing has arrived in project 314 yet; point an SDK at its DSN: gcx error-tracking projects get 314 --json spec.dsn"},
		{name: "get a zero ID", args: []string{"raw-events", "get", "0", "--project", "314", "-o", "json"}, exit: gcxerrors.ExitUsageError,
			err: `<raw-event> "0" must be a positive raw event ID or an EVENT ID`},
		{name: "get an ID too large is a usage error, not an EVENT ID lookup", args: []string{"raw-events", "get", "9223372036854775808", "--project", "314"}, exit: gcxerrors.ExitUsageError,
			err: `<raw-event> "9223372036854775808" must be a positive raw event ID or an EVENT ID`},
		{name: "and so is the largest int64", args: []string{"raw-events", "get", "9223372036854775807", "--project", "314"}, exit: gcxerrors.ExitUsageError,
			err: `<raw-event> "9223372036854775807" must be a positive raw event ID or an EVENT ID`},
	})
}

func TestRawEventsGetFormats(t *testing.T) {
	f := newFakeET(t)
	const get = "GET projects/314/rawEvents/9"
	f.reply(get, 200, `{"id":9,"eventId":"0f6c2b1e9d4a4c3b8e7f6a5b4c3d2e1f","receivedAt":"2026-11-16T10:00:00Z","outcome":"accepted","size":512,"payload":{"exception":{"values":[{"type":"TypeError"}]}}}`)
	get9 := []string{"raw-events", "get", "9", "--project", "314"}
	f.runCases(t, []cliCase{
		{name: "the table is the default", args: get9, calls: []string{get}, has: []string{"OUTCOME", "accepted"}},
		{name: "JSON selects fields", args: append(slices.Clone(get9), "--json", "outcome,size"), calls: []string{get}, json: `{"outcome":"accepted","size":512}`},
		{name: "there is no text card", args: append(slices.Clone(get9), "-o", "text"), exit: gcxerrors.ExitUsageError, err: "unknown output format 'text'"},
	})
}

func TestRawEventsGetLongNumericIsAnEventID(t *testing.T) {
	f := newFakeET(t)
	const list, get = "GET projects/314/rawEvents", "GET projects/314/rawEvents/9"
	f.reply(list, 200, `{"rawEvents":[{"id":9,"eventId":"12345678901234567890123456789012","outcome":"accepted"}]}`)
	f.reply(get, 200, `{"id":9,"eventId":"12345678901234567890123456789012","outcome":"accepted","payload":{}}`)
	f.runCases(t, []cliCase{
		{name: "a 32-digit argument is looked up as an EVENT ID, not refused as a number", args: []string{"raw-events", "get", "12345678901234567890123456789012", "--project", "314", "--json", "outcome"},
			calls: []string{list, get}, json: `{"outcome":"accepted"}`,
			check: func(t *testing.T, _ string, _ error, calls []etCall) {
				t.Helper()
				assert.Equal(t, "12345678901234567890123456789012", calls[0].Query.Get("eventId"))
			}},
	})
}
