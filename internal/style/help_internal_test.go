package style

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// renderLong must not hard-break long tokens such as documentation URLs, or
// terminals stop detecting them as clickable links.
func TestRenderLong_KeepsLongURLOnOneLine(t *testing.T) {
	url := "https://grafana.com/docs/grafana-cloud/security-and-account-management/authentication-and-permissions/access-policies/create-access-policies.md"

	out, err := renderLong("See:\n" + url)
	require.NoError(t, err)

	// Strip ANSI escapes before inspecting line contents.
	plain := regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(out, "")

	var found bool
	for line := range strings.SplitSeq(plain, "\n") {
		if strings.Contains(line, url) {
			found = true
			break
		}
	}
	assert.True(t, found, "the full URL must appear on a single line, got:\n%s", plain)
}

func TestHelpFunc_AdditionalTopics(t *testing.T) {
	for _, styled := range []bool{false, true} {
		for _, native := range []bool{false, true} {
			t.Run(fmt.Sprintf("styled=%t/native=%t", styled, native), func(t *testing.T) {
				cmd := &cobra.Command{Use: "test", Short: "Test command."}
				cmd.AddCommand(&cobra.Command{Use: "run", Run: func(*cobra.Command, []string) {}})
				if native {
					cmd.AddCommand(&cobra.Command{Use: "about", Short: "About the command."})
				}
				var buf bytes.Buffer
				cmd.SetOut(&buf)
				defaultHelp := cmd.HelpFunc()
				defaultHelp(cmd, nil)
				defaultOutput := buf.String()
				buf.Reset()
				if styled {
					defaultHelp = func(*cobra.Command, []string) { t.Fatal("styled help must not delegate to plain help") }
				}
				cmd.SetHelpFunc(helpFunc(defaultHelp, func() bool { return styled },
					HelpTopic{Name: "first", Short: "First topic."},
					HelpTopic{Name: "next", Short: "Next topic."}))
				require.NoError(t, cmd.Help())
				out := buf.String()
				registryTopics := "  test help first  First topic.\n  test help next   Next topic.\n\n"
				if styled {
					want := "Additional help topics:\n"
					if native {
						want += fmt.Sprintf("  %-16s %s\n", "test about", "About the command.")
					}
					want += registryTopics
					want += "Use \"test [command] --help\" for more information about a command.\n"
					assert.Contains(t, out, want)
					assert.Equal(t, 1, strings.Count(out, "Additional help topics:"))
				} else {
					assert.Equal(t, defaultOutput+"\nAdditional help topics:\n"+registryTopics, out)
				}
				assert.Equal(t, 1, strings.Count(out, "test help first"))
				assert.Equal(t, 1, strings.Count(out, "test help next"))
				if native {
					assert.Equal(t, 1, strings.Count(out, "test about"))
				}

				buf.Reset()
				require.NoError(t, cmd.Commands()[0].Help())
				assert.NotContains(t, buf.String(), "test help first")
			})
		}
	}
}

// renderLong must show placeholders such as <title>, which markdown would
// otherwise parse as HTML tags that glamour drops, without altering code or
// breaking autolinks.
func TestRenderLong_KeepsPlaceholders(t *testing.T) {
	tests := []struct {
		name string
		long string
		want string
	}{
		{
			name: "bare placeholder",
			long: "<title>",
			want: "<title>",
		},
		{
			name: "placeholder mid-sentence",
			long: "Select one exact title with --name <title> and choose yaml.",
			want: "Select one exact title with --name <title> and choose yaml.",
		},
		{
			name: "kebab-case placeholders in a path",
			long: "Archives are written under <path-prefix>/tenant=<logs-instance-id>/.",
			want: "Archives are written under <path-prefix>/tenant=<logs-instance-id>/.",
		},
		{
			name: "placeholder in inline code",
			long: "Run `gcx instrumentation explain <id>` for details.",
			want: "Run gcx instrumentation explain <id> for details.",
		},
		{
			name: "placeholder in fenced code block",
			long: "Example:\n\n```\ngcx alert rules get <name>\n```",
			want: "Example: gcx alert rules get <name>",
		},
		{
			name: "placeholder in tilde fenced code block",
			long: "Example:\n\n~~~\ngcx alert rules get <name>\n~~~",
			want: "Example: gcx alert rules get <name>",
		},
		{
			name: "placeholder in indented code block",
			long: "Example:\n\n    gcx alert rules get <name>",
			want: "Example: gcx alert rules get <name>",
		},
		{
			name: "placeholder in a code span containing a backtick",
			long: "Run ``echo ` <name>`` for details.",
			want: "Run echo ` <name> for details.",
		},
		{
			name: "already escaped placeholder",
			long: `Pass \<name> to select one.`,
			want: "Pass <name> to select one.",
		},
		{
			name: "autolink",
			long: "See <https://grafana.com/docs/> for details.",
			want: "See https://grafana.com/docs/ for details.",
		},
		{
			name: "no placeholders",
			long: "Operators: =, !=, >, <, >=, <= compare values.",
			want: "Operators: =, !=, >, <, >=, <= compare values.",
		},
	}

	// Strip SGR colors and OSC 8 hyperlinks, then collapse whitespace,
	// including the non-breaking spaces glamour pads inline code with.
	ansi := regexp.MustCompile(`\x1b\[[0-9;]*m|\x1b\]8;[^\a]*\a`)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := renderLong(tc.long)
			require.NoError(t, err)

			plain := strings.Join(strings.Fields(ansi.ReplaceAllString(out, "")), " ")
			assert.Equal(t, tc.want, plain)
		})
	}
}
