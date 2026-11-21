package errortracking_test

import (
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
)

func TestHintCommandsQuoteWhatTheyEmbed(t *testing.T) {
	f := newFakeET(t)
	neutralised := func(value string) func(t *testing.T, out string, err error, _ []etCall) {
		return func(t *testing.T, _ string, err error, _ []etCall) {
			t.Helper()
			text := f.stderr.String()
			if err != nil {
				text += rendered(t, err)
			}
			assert.Contains(t, text, "'"+value+"'", "the value is one single-quoted word, in POSIX shells and in PowerShell")
			assert.NotContains(t, text, " "+value+" ", "the value never appears bare")
			assert.NotContains(t, text, "& '"+value+"'", "one whole command, never a quoted fragment spliced into another")
		}
	}
	f.reply("GET projects", 200, `{"projects":[{"id":314,"name":"a b; c","stats":{"dailyEvents":[]}}]}`)
	f.runCases(t, []cliCase{
		{name: "a --project value in a suggestion is quoted", args: []string{"errors", "get", "x", "--project", "a b; c"}, exit: gcxerrors.ExitUsageError,
			check: func(t *testing.T, _ string, err error, _ []etCall) {
				t.Helper()
				out := rendered(t, err)
				assert.Contains(t, out, "See them: "+shell("gcx", "error-tracking", "errors", "list", "--project", "a b; c"))
				neutralised("a b; c")(t, "", err, nil)
			}},
		{name: "a value shaped like a placeholder is still quoted", args: []string{"errors", "get", "x", "--project", "<$(id)>"}, exit: gcxerrors.ExitUsageError,
			check: func(t *testing.T, _ string, err error, _ []etCall) {
				t.Helper()
				out := rendered(t, err)
				assert.Contains(t, out, "See them: "+shell("gcx", "error-tracking", "errors", "list", "--project", "<$(id)>"))
				assert.Contains(t, out, "'<$(id)>'")
			}},
		{name: "a placeholder stays bare beside a quoted value", args: []string{"errors", "comments", "update", "4 2", "x", "--body", "b"}, exit: gcxerrors.ExitUsageError,
			check: func(t *testing.T, _ string, err error, _ []etCall) {
				t.Helper()
				out := rendered(t, err)
				assert.Contains(t, out, "'4 2'")
				assert.Regexp(t, `--project'? <project> `, out)
				assert.NotContains(t, out, "'<project>'")
			}},
		{name: "a positional value in a suggestion is quoted", args: []string{"errors", "comments", "update", "4 2", "x", "--project", "314", "--body", "b"}, exit: gcxerrors.ExitUsageError,
			check: func(t *testing.T, _ string, err error, _ []etCall) {
				t.Helper()
				out := rendered(t, err)
				assert.Contains(t, out, "See them: "+shell("gcx", "error-tracking", "errors", "activity", "list", "4 2", "--project", "314", "--type", "comment"))
				neutralised("4 2")(t, "", err, nil)
			}},
	})
}
