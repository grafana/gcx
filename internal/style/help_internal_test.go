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
				if styled {
					defaultHelp = func(*cobra.Command, []string) { t.Fatal("styled help must not delegate to plain help") }
				}
				cmd.SetHelpFunc(helpFunc(defaultHelp, func() bool { return styled },
					HelpTopic{Name: "first", Short: "First topic."},
					HelpTopic{Name: "next", Short: "Next topic."}))
				require.NoError(t, cmd.Help())
				out := buf.String()
				want := "Additional help topics:\n"
				if native {
					want += fmt.Sprintf("  %-16s %s\n", "test about", "About the command.")
				}
				want += "  test help first  First topic.\n  test help next   Next topic.\n\n"
				want += "Use \"test [command] --help\" for more information about a command.\n"
				assert.Contains(t, out, want)
				assert.Equal(t, 1, strings.Count(out, "Additional help topics:"))

				buf.Reset()
				require.NoError(t, cmd.Commands()[0].Help())
				assert.NotContains(t, buf.String(), "test help first")
			})
		}
	}
}
