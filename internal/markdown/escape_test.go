package markdown_test

import (
	"testing"

	"github.com/grafana/gcx/internal/markdown"
	"github.com/stretchr/testify/require"
)

func TestEscapeRawHTML(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		want  string
	}{
		{name: "prose placeholder", input: "Select one exact title with --name <title> and choose yaml.", want: "Select one exact title with --name &lt;title&gt; and choose yaml."},
		{name: "qualified name", input: `Pass the "<namespace>/<name>" form.`, want: `Pass the "&lt;namespace&gt;/&lt;name&gt;" form.`},
		{name: "code span", input: "Run `gcx alert time-intervals get <title>`.", want: "Run `gcx alert time-intervals get <title>`."},
		{name: "fenced block", input: "```shell\ngcx alert time-intervals get <title>\n```", want: "```shell\ngcx alert time-intervals get <title>\n```"},
		{name: "html block", input: "<title>\n\nTrailing prose.", want: "&lt;title&gt;\n\nTrailing prose."},
		{name: "comparison", input: "Use when count > 1 and depth < 3.", want: "Use when count > 1 and depth < 3."},
		{name: "autolink", input: "See <https://grafana.com/docs/>.", want: "See <https://grafana.com/docs/>."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, markdown.EscapeRawHTML(tc.input))
		})
	}
}
