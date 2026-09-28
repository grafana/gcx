package commands_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/commands"
	"github.com/grafana/gcx/internal/agent"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func searchTestTree(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "gcx", SilenceUsage: true, SilenceErrors: true}
	run := func(*cobra.Command, []string) { t.Fatal("search executed a suggestion") }
	group := &cobra.Command{Use: "widgets", Aliases: []string{"gadgets"}, Short: "Manage widgets", Run: run}
	for _, verb := range []string{"create", "delete", "get", "list", "update", "validate"} {
		leaf := &cobra.Command{Use: verb, Short: verb + " widgets", Run: run}
		leaf.Annotations = map[string]string{
			agent.AnnotationSkill: "widget-guide", agent.AnnotationAvailability: agent.AvailabilityCloudOnly,
			agent.AnnotationStability: agent.StabilityExperimental,
		}
		leaf.Flags().String("filter", "private-default", "Filter by colour")
		group.AddCommand(leaf)
	}
	group.AddCommand(&cobra.Command{Use: "old-widget", Short: "old widget", Deprecated: "use get", Run: run})
	hidden := &cobra.Command{Use: "secret", Hidden: true}
	hidden.AddCommand(&cobra.Command{Use: "widget", Short: "secret widget", Run: run})
	cmd.AddCommand(group, hidden, commands.Command(cmd))
	cmd.InitDefaultCompletionCmd()
	return cmd
}

func TestSearchCommand(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		count   int
		partial bool
		first   string
	}{
		{"default limit", []string{"widgets"}, 5, true, "gcx widgets"},
		{"unlimited", []string{"widgets", "--limit", "0"}, 7, false, "gcx widgets"},
		{"limit one", []string{"widgets list", "--limit", "1"}, 1, false, "gcx widgets list"},
		{"aliases deduplicated", []string{"gadgets", "--limit", "0"}, 7, false, "gcx widgets"},
		{"flags alone cannot qualify", []string{"colour", "--limit", "0"}, 0, false, ""},
		{"defaults excluded", []string{"private-default"}, 0, false, ""},
		{"hidden subtree", []string{"secret"}, 0, false, ""},
		{"deprecated", []string{"old"}, 0, false, ""},
		{"self excluded", []string{"typo"}, 0, false, ""},
		{"completion excluded", []string{"powershell"}, 0, false, ""},
		{"no matches", []string{"zzzxxyyqq"}, 0, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := searchTestTree(t)
			var out, stderr bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&stderr)
			args := append([]string{"commands", "search"}, tc.args...)
			cmd.SetArgs(append(args, "-o", "json"))
			require.NoError(t, cmd.Execute())
			var result struct {
				Items []map[string]string `json:"items"`
				Meta  map[string]any      `json:"list_meta"`
			}
			require.NoError(t, json.Unmarshal(out.Bytes(), &result))
			require.NotNil(t, result.Items)
			require.Len(t, result.Items, tc.count)
			require.Equal(t, tc.partial, result.Meta != nil)
			if tc.first != "" {
				require.Equal(t, tc.first, result.Items[0]["full_path"])
			}
		})
	}
}

func TestSearchFormatsAndValidation(t *testing.T) {
	for _, tc := range []struct {
		name, contains string
		args           []string
		fail           bool
	}{
		{"missing", "", nil, true},
		{"unquoted", "", []string{"list", "widgets"}, true},
		{"empty", "", []string{""}, true},
		{"whitespace", "", []string{" \t "}, true},
		{"punctuation", "", []string{"!!!"}, true},
		{"negative limit", "", []string{"widgets", "--limit=-1"}, true},
		{"text", "COMMAND", []string{"widgets", "-o", "text"}, false},
		{"empty text", "No strong matches", []string{"zzzxxyyqq", "-o", "text"}, false},
		{"yaml", "full_path:", []string{"widgets", "-o", "yaml"}, false},
		{"fields", "full_path", []string{"widgets", "--json", "full_path"}, false},
		{"discovery", "full_path", []string{"widgets", "--json", "?"}, false},
		{"jq", "gcx widgets", []string{"widgets", "--jq", ".items[0].full_path"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := searchTestTree(t)
			var out, stderr bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&stderr)
			cmd.SetArgs(append([]string{"commands", "search"}, tc.args...))
			err := cmd.Execute()
			if tc.fail {
				require.Error(t, err)
				require.Empty(t, out.String())
				return
			}
			require.NoError(t, err)
			require.Contains(t, out.String(), tc.contains)
			if tc.name == "fields" {
				require.Contains(t, out.String(), "list_meta")
				require.NotContains(t, out.String(), "description")
			}
		})
	}
}
