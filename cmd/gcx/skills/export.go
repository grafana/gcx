package skills

import (
	"errors"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	skillops "github.com/grafana/gcx/internal/skills"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type exportOpts struct {
	Dir    string
	DryRun bool
	IO     cmdio.Options
}

func (o *exportOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("text")
	o.IO.RegisterCustomCodec("text", &updateTextCodec{})
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Dir, "dir", "~/.agents", "Root directory for imported custom skills")
	flags.BoolVar(&o.DryRun, "dry-run", false, "Preview the export without writing files")
}

func (o *exportOpts) Validate(args []string) error {
	if len(args) != 2 || !skillops.IsCustomID(args[0]) || strings.TrimSpace(args[1]) == "" {
		return errors.New("provide one custom/<name> and a new local destination directory")
	}
	return o.IO.Validate()
}

func newExportCommand() *cobra.Command {
	opts := &exportOpts{}
	cmd := &cobra.Command{
		Use:   "export SKILL DIRECTORY",
		Short: "[experimental] Copy a complete custom skill into a new edit directory",
		Long:  "This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions.\n\nExport all saved files and a base revision for editing. Unlike get, this includes every reference and asset. The destination must not exist. After editing, preview update custom/<name> --from ./directory --dry-run, then save with update. Stale exported revisions are rejected; export again and reapply edits. No scripts are executed.",
		Example: `  gcx agent skills export custom/service-debug ./service-debug-edit
  gcx agent skills update custom/service-debug --from ./service-debug-edit --dry-run`,
		Annotations: map[string]string{agent.AnnotationStability: agent.StabilityExperimental},
		Args:        cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(args); err != nil {
				return gcxerrors.NewCommandUsageError(cmd, err.Error(), err)
			}
			root, err := skillops.ResolveInstallRoot(opts.Dir)
			if err != nil {
				return err
			}
			result, err := skillops.ExportCustom(root, args[0], args[1], opts.DryRun)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}
