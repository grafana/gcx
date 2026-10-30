package main

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/root"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/renderer/html"
)

func TestLongHelpShellComments(t *testing.T) {
	for _, description := range []string{
		"Recommended workflow:\n\n  gcx dashboards get <name> -o yaml > dashboard.yaml\n  # edit dashboard.yaml\n  gcx dashboards update <name> -f dashboard.yaml",
		"Examples:\n  # List all workloads\n  gcx instrumentation services list\n\n  # Filter to a specific cluster\n  gcx instrumentation services list --cluster <cluster>",
	} {
		cmd := &cobra.Command{Use: "gcx", Long: description}
		page := render(cmd, "v1.2.3", "# Env")
		_, body, found := strings.Cut(page, "\n---\n")
		require.True(t, found)
		var html bytes.Buffer
		require.NoError(t, goldmark.Convert([]byte(body), &html))
		require.Equal(t, 1, strings.Count(html.String(), "<h1>"), html.String())
		require.Contains(t, html.String(), "<h1>CLI reference</h1>")
		require.Less(t, strings.Index(html.String(), "<h1>CLI reference</h1>"), strings.Index(html.String(), "This reference describes"))
		require.Contains(t, html.String(), `<pre><code class="language-shell">`)
		require.Contains(t, html.String(), "  # ")
		if strings.Contains(description, "<name>") {
			require.Contains(t, html.String(), "&lt;name&gt;")
		} else {
			require.Contains(t, html.String(), "&lt;cluster&gt;")
		}
	}
}

func TestRenderCommands(t *testing.T) {
	for _, tc := range []struct {
		name       string
		hidden     bool
		deprecated string
	}{
		{name: "public"},
		{name: "hidden", hidden: true},
		{name: "deprecated", deprecated: "Use gcx new instead."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: "gcx"}
			cmd.PersistentFlags().String("context", "", "Context to use")
			group := &cobra.Command{Use: "group"}
			group.PersistentFlags().String("project", "", "Project ID")
			leaf := &cobra.Command{Use: "get ID", Aliases: []string{"show"}, Hidden: tc.hidden, Deprecated: tc.deprecated, Run: func(*cobra.Command, []string) {}}
			leaf.Flags().String("context", "local", "Local context override")
			leaf.Flags().String("internal", "", "Hidden flag")
			require.NoError(t, leaf.Flags().MarkHidden("internal"))
			group.AddCommand(leaf)
			cmd.AddCommand(group)
			page := render(cmd, "v1.2.3", "# Env\n\n## `GCX_TEST`\n\nTest variable")
			require.Contains(t, page, "gcx **v1.2.3**")
			require.Contains(t, page, "### `GCX_TEST`")
			require.NotContains(t, page, "--internal")
			if tc.hidden {
				require.NotContains(t, page, "gcx group get")
				return
			}
			require.Contains(t, page, "### `gcx group get` {#gcx-group-get}")
			require.Contains(t, page, "gcx group get ID [flags]")
			require.Contains(t, page, "**Aliases:** `show`")
			_, leafPage, ok := strings.Cut(page, "### `gcx group get`")
			require.True(t, ok)
			require.Contains(t, leafPage, "Inherits `--project` from [`gcx group`](#gcx-group-flags)")
			require.NotContains(t, leafPage, "Inherits `--context`")
			if tc.deprecated != "" {
				require.Contains(t, leafPage, "**Deprecated:** "+tc.deprecated)
			}
		})
	}
}

func TestFullCommandCoverageAndLinks(t *testing.T) {
	t.Setenv("GCX_AGENT_MODE", "false")
	cmd := root.Command("v1.2.3")
	cmd.Use = "gcx"
	page := render(cmd, "v1.2.3", "# Env\n\n## `GCX_TEST`\n\nTest variable")
	var html bytes.Buffer
	require.NoError(t, goldmark.Convert([]byte(page), &html))
	require.Equal(t, 1, strings.Count(html.String(), "<h1>"))
	require.Contains(t, html.String(), "<h1>CLI reference</h1>")
	ids := map[string]bool{"commands": true, "environment-variables": true}
	for _, match := range regexp.MustCompile(`\{#([^}]+)\}`).FindAllStringSubmatch(page, -1) {
		require.False(t, ids[match[1]], "duplicate anchor %s", match[1])
		ids[match[1]] = true
	}
	for _, match := range regexp.MustCompile(`\]\(#([^)]+)\)`).FindAllStringSubmatch(page, -1) {
		require.True(t, ids[match[1]], "broken anchor %s", match[1])
	}
	var check func(*cobra.Command)
	check = func(command *cobra.Command) {
		if command.Hidden {
			return
		}
		require.Equal(t, 1, strings.Count(page, "### `"+command.CommandPath()+"` {#"))
		for _, child := range command.Commands() {
			check(child)
		}
	}
	check(cmd)
	require.Equal(t, page, render(cmd, "v1.2.3", "# Env\n\n## `GCX_TEST`\n\nTest variable"))
}

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
			require.Equal(t, tc.want, escapeRawHTML(tc.input))
		})
	}
}

// Hugo renders with raw HTML enabled, so a placeholder such as <title> left in
// command help becomes an element and the browser drops the rest of the page.
func TestDescriptionsNeverEmitRawHTML(t *testing.T) {
	t.Setenv("GCX_AGENT_MODE", "false")
	for _, tc := range []struct {
		name string
		cmd  *cobra.Command
	}{
		{name: "long", cmd: &cobra.Command{Use: "gcx", Long: "Select one exact title with --name <title> and choose yaml."}},
		{name: "short", cmd: &cobra.Command{Use: "gcx", Short: "Delete the <title> interval."}},
		{name: "deprecated", cmd: &cobra.Command{Use: "gcx", Deprecated: "Use gcx alert mute-timings get <title> instead."}},
		{name: "command tree", cmd: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := tc.cmd
			if cmd == nil {
				cmd = root.Command("v1.2.3")
				cmd.Use = "gcx"
			}
			page := render(cmd, "v1.2.3", "# Env\n\n## `GCX_TEST`\n\nUse the <namespace>/<name> form.")
			// Skip the front matter and the generated-by comment the page emits itself.
			_, body, found := strings.Cut(page, "\n# CLI reference\n")
			require.True(t, found)
			var omitted bytes.Buffer
			require.NoError(t, goldmark.Convert([]byte(body), &omitted))
			// Without WithUnsafe, goldmark marks every span it would pass through to Hugo.
			require.NotContains(t, omitted.String(), "raw HTML omitted", "command metadata reached the page as raw HTML")

			var rendered bytes.Buffer
			unsafe := goldmark.New(goldmark.WithRendererOptions(html.WithUnsafe()))
			require.NoError(t, unsafe.Convert([]byte(body), &rendered))
			require.NotContains(t, rendered.String(), "<title>")
			require.Contains(t, rendered.String(), "<h2>Environment variables</h2>")
			if tc.name == "command tree" {
				require.Contains(t, rendered.String(), "--name &lt;title&gt;")
			}
		})
	}
}

func TestDateDefaults(t *testing.T) {
	cmd := &cobra.Command{Use: "gcx"}
	cmd.Flags().String("date", "2026-10-06", "Report date")
	page := render(cmd, "v1.2.3", "# Env")
	require.Contains(t, page, `(default "YYYY-MM-DD")`)
	require.NotContains(t, page, "2026-10-06")
}

func TestSupplementEnvironment(t *testing.T) {
	env := "# Env\n\n## `GRAFANA_SERVER`\n\nServer URL"
	guide := "| `GRAFANA_SERVER` | context | duplicate |\n" +
		"| `GCX_CONFIG` | global | Config file override |\n" +
		"| `GRAFANA_PROVIDER_SLO_TOKEN` | slo | token |\n" +
		"| `GCX_AGENT_MODE` | opt-in/out | Enable agent mode |\n" +
		"| `OPENCODE` | opencode | Truthy value activates agent mode |"
	page := supplementEnvironment(env, guide)
	require.Equal(t, 1, strings.Count(page, "## `GRAFANA_SERVER`"))
	require.Contains(t, page, "## `GCX_CONFIG`\n\nConfig file override")
	require.Contains(t, page, "Overrides the slo provider's token configuration.")
	require.Contains(t, page, "## `GCX_AGENT_MODE`")
	require.Contains(t, page, "## `OPENCODE`\n\nTruthy value activates agent mode")
}

func TestRenderConfiguration(t *testing.T) {
	guide := "# Configure gcx\n\nHandwritten guidance.\n\n"
	schema := "# Configuration reference\n\n```yaml\n# schema comment\nversion: int\n```\n"
	page, err := renderConfiguration(guide+configurationMarker+"\nOld schema\n", schema, "v1.2.3")
	require.NoError(t, err)
	require.Equal(t, guide+configurationMarker+"\n\n## Configuration reference\n\nThis schema describes configuration fields and their types in gcx **v1.2.3**.\n\n```yaml\n# schema comment\nversion: int\n```\n", page)
	again, err := renderConfiguration(page, schema, "v1.2.3")
	require.NoError(t, err)
	require.Equal(t, page, again)
	_, err = renderConfiguration(guide, schema, "v1.2.3")
	require.ErrorContains(t, err, "missing")
}

// The schema generator fences everything today, but prose around that fence
// would reach the configuration page the same way <title> reached this one.
func TestRenderConfigurationNeverEmitsRawHTML(t *testing.T) {
	page, err := renderConfiguration("# Configure gcx\n\n"+configurationMarker+"\n", "# Configuration reference\n\nSet --config <path> to override discovery.\n", "v1.2.3")
	require.NoError(t, err)
	require.Contains(t, page, "--config &lt;path&gt;")
	// Skip the handwritten guide and the marker comment that separates it.
	_, generated, found := strings.Cut(page, configurationMarker)
	require.True(t, found)
	var omitted bytes.Buffer
	require.NoError(t, goldmark.Convert([]byte(generated), &omitted))
	require.NotContains(t, omitted.String(), "raw HTML omitted")
}
