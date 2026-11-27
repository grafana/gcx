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
	for route, body := range map[string]string{
		"GET projects":     `{"projects":[{"id":314,"name":"` + evil + `","stats":{"dailyEvents":[]}}]}`,
		"GET projects/314": `{"id":314,"name":"` + evil + `","stats":{"dailyEvents":[]}}`,
	} {
		f.reply(route, 200, body)
	}

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
	const p = "GET projects/314"
	f.runCases(t, []cliCase{
		{name: "a declined project delete prompt", stdin: "n\n", args: []string{"projects", "delete", "314"},
			calls: []string{p}, exit: 5, check: clean},
		run("projects list names the default", []string{"GET projects"}, "projects", "list"),
	})
}
