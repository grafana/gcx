package skills

import (
	"errors"
	"io/fs"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	skillops "github.com/grafana/gcx/internal/skills"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type searchOpts struct {
	Dir       string
	Selection string
	Tags      []string
	Limit     int
	IO        cmdio.Options
}

func (o *searchOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Dir, "dir", "~/.agents", "Root directory for imported custom skills")
	flags.StringVar(&o.Selection, "source", "all", "Skill source: bundled, custom, or all")
	flags.StringArrayVar(&o.Tags, "tag", nil, "Require an exact tag, case-insensitively (repeatable; all tags must match)")
	flags.IntVar(&o.Limit, "limit", 5, "Maximum metadata matches (0 returns all); output discloses truncation")
}

func (o *searchOpts) Validate(args []string) error {
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" || o.Limit < 0 {
		return errors.New("provide one nonempty query and a nonnegative --limit (0 returns all)")
	}
	if err := skillops.ValidateSource(o.Selection); err != nil {
		return err
	}
	for _, tag := range o.Tags {
		if strings.TrimSpace(tag) == "" {
			return errors.New("--tag must not be empty")
		}
	}
	return o.IO.Validate()
}

func newSearchCommand(source fs.FS, catalog []byte) *cobra.Command {
	opts := &searchOpts{}
	cmd := &cobra.Command{
		Use:   "search QUERY",
		Short: "[experimental] Search bundled and imported custom skill metadata",
		Long:  "This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions.\n\nSearch local skill names, descriptions and tags without loading bodies into the result. Whitespace-separated terms are case-insensitive literal substrings and must all match. Repeated --tag values must all match exactly. Results are ordered by identity, not semantic relevance. No Grafana or model request is made.",
		Example: `  gcx agent skills search latency --limit 5
  gcx agent skills search latency --source custom --tag logs -o json`,
		Annotations: map[string]string{agent.AnnotationStability: agent.StabilityExperimental},
		Args:        cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(args); err != nil {
				return gcxerrors.NewCommandUsageError(cmd, err.Error(), err)
			}
			root, err := skillops.ResolveInstallRoot(opts.Dir)
			if err != nil {
				return err
			}
			result, err := skillops.Search(source, catalog, root, opts.Selection, args[0], opts.Tags, opts.Limit)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}
