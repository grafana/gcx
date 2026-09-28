package commands

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type searchOpts struct {
	IO    cmdio.Options
	Query string
	Limit int
}

func (o *searchOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("text")
	o.IO.RegisterCustomCodec("text", &searchTextCodec{})
	o.IO.BindFlags(flags)
	o.IO.BindListLimit(flags, &o.Limit, "command suggestions", 5)
}

func (o *searchOpts) Validate() error {
	if strings.IndexFunc(o.Query, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) }) < 0 {
		return errors.New("query must contain letters or numbers; try gcx commands search \"create an uptime check\"")
	}
	return o.IO.Validate()
}

type searchResult struct {
	FullPath     string `json:"full_path" yaml:"full_path"`
	Description  string `json:"description" yaml:"description"`
	Skill        string `json:"skill,omitempty" yaml:"skill,omitempty"`
	Availability string `json:"availability,omitempty" yaml:"availability,omitempty"`
	Stability    string `json:"stability,omitempty" yaml:"stability,omitempty"`
}

type searchOutput struct {
	Items    []searchResult  `json:"items" yaml:"items"`
	ListMeta *cmdio.ListMeta `json:"list_meta,omitempty" yaml:"list_meta,omitempty"`
}

func searchCommand(root *cobra.Command) *cobra.Command {
	opts := &searchOpts{}
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Find CLI commands by intent using local text search",
		Long: `Search the installed CLI's command paths, aliases, descriptions and parameters.
Quote a task description to receive up to five ranked suggestions. Matching uses
case-insensitive words, prefixes and single-character typo correction, not semantic
understanding. Commands matching more query words rank above partial matches.
Suggestions may only match part of your query; inspect the selected command with
--help before using it. No Grafana connection or credentials are required, and
suggestions are not checked for availability in your current context.

Use --limit 0 for all matches or help-tree to browse a known command group.`,
		Example: `  gcx commands search "create an uptime check"
  gcx commands search "export dashboards" --limit 10
  gcx commands search "query metrics" -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Query = args[0]
			if err := opts.Validate(); err != nil {
				return err
			}
			documents, results := searchDocuments(root, cmd)
			paths := agent.SearchCommands(documents, opts.Query)
			items := make([]searchResult, 0, len(paths))
			for _, path := range paths {
				items = append(items, results[path])
			}
			items, meta := cmdio.TruncateCompleteList(items, opts.Limit)
			result := searchOutput{Items: items, ListMeta: cmdio.AttachListMeta(meta, os.Args)}
			if err := opts.IO.Encode(cmd.OutOrStdout(), result); err != nil {
				return err
			}
			cmdio.EmitListTruncationHint(cmd.ErrOrStderr(), result.ListMeta)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func searchDocuments(root, search *cobra.Command) ([]agent.SearchDocument, map[string]searchResult) {
	var documents []agent.SearchDocument
	results := make(map[string]searchResult)
	var walk func(*cobra.Command, string, string, string)
	walk = func(cmd *cobra.Command, parent, ancestors, aliases string) {
		if cmd.Hidden || cmd.Deprecated != "" || cmd == search || cmd.Name() == "completion" {
			return
		}
		info := commandInfo(cmd, parent)
		if info.Skill == "" {
			info.Skill = strings.Join(agent.SkillsForCommand(cmd), ",")
		}
		aliases = strings.TrimSpace(aliases + " " + strings.Join(cmd.Aliases, " "))
		if cmd.Runnable() && cmd != root {
			var context strings.Builder
			context.WriteString(ancestors + " " + info.Args + " " + info.Skill)
			for _, flag := range info.Flags {
				// Presentation flags are shared infrastructure, not command intent.
				switch flag.Name {
				case "output", "json", "jq", "help", "save":
					continue
				}
				context.WriteString(" " + flag.Name + " " + flag.Description)
			}
			documents = append(documents, agent.SearchDocument{
				Path: info.FullPath, Aliases: aliases, Summary: info.Description,
				Description: info.Long, Context: context.String(),
			})
			results[info.FullPath] = searchResult{
				FullPath: info.FullPath, Description: info.Description, Skill: info.Skill,
				Availability: info.Availability, Stability: info.Stability,
			}
		}
		for _, child := range cmd.Commands() {
			walk(child, info.FullPath, ancestors+" "+cmd.Short, aliases)
		}
	}
	walk(root, "", "", "")
	return documents, results
}

type searchTextCodec struct{}

func (c *searchTextCodec) Format() format.Format { return "text" }

func (c *searchTextCodec) Decode(io.Reader, any) error {
	return errors.New("search text codec does not support decoding")
}

func (c *searchTextCodec) Encode(w io.Writer, value any) error {
	result, ok := value.(searchOutput)
	if !ok {
		return fmt.Errorf("unsupported search output: %T", value)
	}
	if len(result.Items) == 0 {
		_, err := fmt.Fprintln(w, "No matching commands. Try different words or browse gcx help-tree --depth 1.")
		return err
	}
	table := style.NewTable("COMMAND", "DESCRIPTION", "AVAILABILITY", "STABILITY", "SKILL")
	for _, item := range result.Items {
		table.Row(item.FullPath, item.Description, item.Availability, item.Stability, item.Skill)
	}
	return table.Render(w)
}
