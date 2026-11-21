package errortracking_test

import (
	"os"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIngestKeys(t *testing.T) {
	t.Setenv("GCX_AUTO_APPROVE", "")
	os.Unsetenv("GCX_AUTO_APPROVE")
	t.Cleanup(agent.ResetForTesting)
	agent.SetFlag(false)

	f := newFakeET(t)
	const get, rotate, revoke = "GET projects/314", "POST projects/314/ingestKey/rotate", "POST projects/314/ingestKey/revokePrevious"
	const withOld = `{"id":314,"name":"Checkout API","ingestKey":"k-old","dsn":"https://k-old@ingest.example.com/314","previousIngestKeyUntil":"2099-11-19T09:00:00Z","stats":{"dailyEvents":[]}}`
	f.reply("GET projects", 200, twoProjects)
	f.reply(get, 200, withOld)
	f.reply(rotate, 200, `{"id":314,"name":"Checkout API","ingestKey":"k-new","dsn":"https://k-new@ingest.example.com/314","previousIngestKeyUntil":"2026-11-19T10:00:00Z","stats":{"dailyEvents":[]}}`)
	f.reply(revoke, 200, `{"id":314,"name":"Checkout API","ingestKey":"k-new","dsn":"https://k-new@ingest.example.com/314","stats":{"dailyEvents":[]}}`)
	noPrevious := func(f *fakeET) {
		f.reply(get, 200, `{"id":314,"name":"Checkout API","ingestKey":"k-new","stats":{"dailyEvents":[]}}`)
	}

	f.runCases(t, []cliCase{
		{name: "rotate's JSON names the project and the old key's deadline, and holds no key or DSN", args: []string{"projects", "ingest-keys", "rotate", "314", "--force", "-o", "json"},
			calls: []string{rotate}, hasNot: []string{"k-new", "k-old", "ingestKey", "dsn"}, json: `{"type":"gcx.mutation","schema_version":"1","action":"rotated","changed":true,
				"target":{"kind":"IngestKey","namespace":"stacks-123"},"project":{"id":314,"name":"Checkout API"},
				"previousIngestKeyUntil":"2026-11-19T10:00:00Z"}`,
			check: func(t *testing.T, _ string, _ error, calls []etCall) {
				t.Helper()
				require.Empty(t, calls[0].Body, "no --grace sends no body, so the server picks the grace period")
			}},
		{name: "rotate as text names the project and hides the DSN", stdin: "y\n", args: []string{"projects", "ingest-keys", "rotate", "Checkout API", "--grace", "0s"},
			calls: []string{"GET projects", get, rotate}, body: `{"grace":"0s"}`, stderr: "Rotate the ingest key of Error Tracking project Checkout API (ID 314)?",
			has:    []string{"Rotated the ingest key of project Checkout API (ID 314); the old key works until 2026-11-19 10:00Z ("},
			hasNot: []string{"k-new", "k-old", "checkout-api-314"}},
		{name: "a declined rotation changes nothing", stdin: "n\n", args: []string{"projects", "ingest-keys", "rotate", "314", "--grace", "168h"},
			calls: []string{get}, exit: gcxerrors.ExitCancelled},
		{name: "revoke reads as a deleted ingest key, not a deleted project", args: []string{"projects", "ingest-keys", "delete-previous", "314", "--force", "-o", "json"},
			calls: []string{get, revoke}, hasNot: []string{"k-new", "k-old"}, json: `{"type":"gcx.mutation","schema_version":"1","action":"deleted","changed":true,
				"target":{"kind":"IngestKey","namespace":"stacks-123"},"project":{"id":314,"name":"Checkout API"}}`},
		{name: "revoke as text, after a prompt", stdin: "y\n", args: []string{"projects", "ingest-keys", "delete-previous", "314"},
			calls: []string{get, revoke}, stderr: "Stop the previous ingest key of Error Tracking project Checkout API (ID 314) now?",
			has: []string{"Stopped the previous ingest key of project Checkout API (ID 314)"}, hasNot: []string{"k-new"}},
		{name: "no previous key is changed: false, without a prompt or a write", setup: noPrevious, args: []string{"projects", "ingest-keys", "delete-previous", "314", "-o", "json"},
			calls: []string{get}, json: `{"type":"gcx.mutation","schema_version":"1","action":"deleted","changed":false,
				"target":{"kind":"IngestKey","namespace":"stacks-123"},"project":{"id":314,"name":"Checkout API"}}`},
		{name: "no previous key as text", setup: noPrevious, args: []string{"projects", "ingest-keys", "delete-previous", "314", "--force"},
			calls: []string{get}, has: []string{"Project Checkout API (ID 314) has no previous ingest key; nothing to stop"}},
		{name: "an expired previous key is nothing to stop", setup: func(f *fakeET) {
			f.reply(get, 200, `{"id":314,"name":"Checkout API","ingestKey":"k-new","previousIngestKeyUntil":"2020-01-01T00:00:00Z","stats":{"dailyEvents":[]}}`)
		}, args: []string{"projects", "ingest-keys", "delete-previous", "314", "--force"},
			calls: []string{get}, has: []string{"has no previous ingest key; nothing to stop"}},
		{name: "a declined revoke changes nothing", stdin: "n\n", args: []string{"projects", "ingest-keys", "delete-previous", "314"},
			calls: []string{get}, exit: gcxerrors.ExitCancelled, err: "delete cancelled"},
		{name: "a grace in hours is sent as parsed", args: []string{"projects", "ingest-keys", "rotate", "314", "--grace", " 1h ", "--force", "-o", "json"},
			calls: []string{rotate}, body: `{"grace":"1h0m0s"}`},
		{name: "the help says the server picks the grace period without --grace", args: []string{"projects", "ingest-keys", "rotate", "--help"},
			has: []string{"without it, the server picks the grace period", "projects get --json spec.dsn reads the new DSN"}, hasNot: []string{"24h by default", `(default "24h")`, "how to read the new DSN"}},
		{name: "ingest-keys delete-previous takes no --grace", args: []string{"projects", "ingest-keys", "delete-previous", "314", "--grace", "1h", "--force"},
			exit: gcxerrors.ExitUsageError, err: "unknown flag: --grace"},
		{name: "an empty --grace on rotate is refused before any request", args: []string{"projects", "ingest-keys", "rotate", "314", "--grace", " ", "--force"},
			exit: gcxerrors.ExitUsageError, err: "--grace must not be empty"},
		{name: "a grace in days", args: []string{"projects", "ingest-keys", "rotate", "314", "--grace", "7d", "--force", "-o", "json"},
			calls: []string{rotate}, body: `{"grace":"168h0m0s"}`},
		{name: "closed stdin", args: []string{"projects", "ingest-keys", "rotate", "314"}, calls: []string{get}, exit: gcxerrors.ExitUsageError},
		{name: "agent mode refuses without --force", setup: asAgent, args: []string{"projects", "ingest-keys", "rotate", "314"},
			exit: gcxerrors.ExitUsageError, check: func(t *testing.T, _ string, err error, _ []etCall) {
				t.Helper()
				assert.Contains(t, rendered(t, err), "destructive operation in agent mode: use --force to proceed")
				agent.SetFlag(false)
			}},
	}, usageCases(func(args ...string) []string { return append([]string{"projects"}, args...) },
		[]string{"ingest-keys", "rotate", "314", "--grace", "169h", "--force"},
		[]string{"ingest-keys", "rotate", "314", "--grace", "-1s", "--force"},
		[]string{"ingest-keys", "rotate", "314", "--grace", "8d", "--force"},
		[]string{"ingest-keys", "rotate", "314", "--grace", "", "--force"},
		[]string{"ingest-keys", "rotate", "0", "--force"},
		[]string{"ingest-keys", "delete-previous", " ", "--force"},
	))
}
