package skills

import (
	"errors"
	"fmt"
	goio "io"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	claudeplugin "github.com/grafana/gcx/claude-plugin"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	skillops "github.com/grafana/gcx/internal/skills"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Command returns the top-level skills command group.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Manage portable gcx Agent Skills",
		Long:  "Install the canonical portable gcx Agent Skills bundle for .agents-compatible agent harnesses.",
	}

	source, catalog := claudeplugin.SkillsFS(), claudeplugin.SkillsCatalog()
	cmd.AddCommand(newInstallCommand(source, catalog))
	cmd.AddCommand(newUpdateCommand(source, catalog))
	cmd.AddCommand(newListCommand(source, catalog))
	cmd.AddCommand(newGetCommand(source, catalog))
	cmd.AddCommand(newUninstallCommand(source, catalog))

	return cmd
}

type installOpts struct {
	Dir     string
	All     bool
	Force   bool
	DryRun  bool
	Source  fs.FS
	Catalog []byte
	IO      cmdio.Options
}

func (o *installOpts) setup(flags *pflag.FlagSet) {
	defaultRoot := "~/.agents"

	o.IO.DefaultFormat("text")
	o.IO.RegisterCustomCodec("text", &installTextCodec{})
	o.IO.BindFlags(flags)

	flags.StringVar(&o.Dir, "dir", defaultRoot, "Root directory for the .agents installation")
	flags.BoolVar(&o.All, "all", false, "Install all bundled skills")
	flags.BoolVar(&o.Force, "force", false, "Overwrite existing differing files managed by the gcx skills bundle")
	flags.BoolVar(&o.DryRun, "dry-run", false, "Preview the installation without writing files")
}

func (o *installOpts) Validate(args []string) error {
	if o.Source == nil {
		return errors.New("skills source is not configured")
	}
	if o.All && len(args) > 0 {
		return errors.New("skill names cannot be provided when --all is set")
	}
	if !o.All && len(args) == 0 {
		return errors.New("provide at least one skill name or use --all")
	}

	return o.IO.Validate()
}

func newInstallCommand(source fs.FS, catalog []byte) *cobra.Command {
	opts := &installOpts{Source: source, Catalog: catalog}

	cmd := &cobra.Command{
		Use:   "install [SKILL]...",
		Short: "Install bundled gcx skills into ~/.agents/skills",
		Long:  "Install one or more bundled gcx Agent Skills into a user-level .agents directory for tools that follow the .agents skill convention. Use --all to install the entire bundle. Deprecated skills are installed with a warning; retired skills cannot be installed.",
		Example: `  gcx agent skills install setup-gcx
  gcx agent skills install setup-gcx debug-with-grafana manage-dashboards
  gcx agent skills install --all
  gcx agent skills install --all --dry-run
  gcx agent skills install setup-gcx --force`,
		Args: cobra.ArbitraryArgs,
		ValidArgsFunction: func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
			return completeSkillNames(source, catalog, false)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(args); err != nil {
				return err
			}

			root, err := skillops.ResolveInstallRoot(opts.Dir)
			if err != nil {
				return err
			}

			var filter map[string]struct{}
			if !opts.All {
				filter = make(map[string]struct{}, len(args))
				for _, name := range args {
					filter[name] = struct{}{}
				}
			}

			result, err := skillops.Install(opts.Source, opts.Catalog, root, filter, opts.Force, opts.DryRun)
			if err != nil {
				return err
			}
			emitLifecycleNotices(cmd.ErrOrStderr(), result.Notices)
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

func emitLifecycleNotices(dst goio.Writer, notices []skillops.LifecycleNotice) {
	for _, notice := range notices {
		message := notice.String()
		if notice.Status == skillops.Retired {
			message += "; local files left untouched; remove explicitly with gcx agent skills uninstall " + notice.Name + " (using the same --dir)"
		}
		cmdio.EmitWarn(dst, message)
	}
}

func completeSkillNames(source fs.FS, data []byte, includeRetired bool) ([]string, cobra.ShellCompDirective) {
	catalog, err := skillops.LoadCatalog(source, data)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	names := make([]string, 0, len(catalog.Skills))
	for name, entry := range catalog.Skills {
		if includeRetired || entry.Status != skillops.Retired {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, cobra.ShellCompDirectiveNoFileComp
}

type installResult = skillops.InstallResult

type installTextCodec struct{}

func (c *installTextCodec) Format() format.Format { return "text" }

func decodeInstallResult(value any, op string) (installResult, error) {
	switch v := value.(type) {
	case installResult:
		return v, nil
	case *installResult:
		if v == nil {
			return installResult{}, fmt.Errorf("nil %s result", op)
		}
		return *v, nil
	default:
		return installResult{}, fmt.Errorf("%s text codec: unsupported value %T", op, value)
	}
}

func renderInstallResultText(dst goio.Writer, result installResult, status string, dryRunStatus string, preposition string) error {
	writtenLabel := "WRITTEN"
	if result.DryRun {
		status = dryRunStatus
		writtenLabel = "WOULD WRITE"
	}

	fmt.Fprintf(dst, "%s %d skill(s) %s %s\n\n", status, result.SkillCount, preposition, result.SkillsDir)

	t := style.NewTable("FIELD", "VALUE")
	t.Row("ROOT", result.Root)
	t.Row("SKILLS DIR", result.SkillsDir)
	t.Row("SKILLS", strconv.Itoa(result.SkillCount))
	t.Row("FILES", strconv.Itoa(result.FileCount))
	t.Row(writtenLabel, strconv.Itoa(result.Written))
	t.Row("OVERWRITTEN", strconv.Itoa(result.Overwritten))
	t.Row("UNCHANGED", strconv.Itoa(result.Unchanged))
	if err := t.Render(dst); err != nil {
		return err
	}

	if len(result.Skills) > 0 {
		_, _ = fmt.Fprintln(dst)
		fmt.Fprintf(dst, "Skill names: %s\n", strings.Join(result.Skills, ", "))
	}

	return nil
}

func (c *installTextCodec) Encode(dst goio.Writer, value any) error {
	result, err := decodeInstallResult(value, "install")
	if err != nil {
		return err
	}

	return renderInstallResultText(dst, result, "Installed", "Would install", "to")
}

func (c *installTextCodec) Decode(_ goio.Reader, _ any) error {
	return errors.New("install text codec does not support decoding")
}

type updateOpts struct {
	Dir     string
	DryRun  bool
	Source  fs.FS
	Catalog []byte
	IO      cmdio.Options
}

func (o *updateOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("text")
	o.IO.RegisterCustomCodec("text", &updateTextCodec{})
	o.IO.BindFlags(flags)

	flags.StringVar(&o.Dir, "dir", "~/.agents", "Root directory for the .agents installation")
	flags.BoolVar(&o.DryRun, "dry-run", false, "Preview the update without writing files")
}

func (o *updateOpts) Validate() error {
	if o.Source == nil {
		return errors.New("skills source is not configured")
	}

	return o.IO.Validate()
}

func newUpdateCommand(source fs.FS, catalog []byte) *cobra.Command {
	opts := &updateOpts{Source: source, Catalog: catalog}

	cmd := &cobra.Command{
		Use:   "update [SKILL]...",
		Short: "Update installed gcx skills in ~/.agents/skills",
		Long:  "Update installed gcx skills in a user-level .agents skills directory. With no skill names, update all installed bundled skills and report retired installations. Retired skills are left untouched; replacements are never installed automatically.",
		Example: `  gcx agent skills update
  gcx agent skills update --dry-run
  gcx agent skills update setup-gcx debug-with-grafana`,
		Args: cobra.ArbitraryArgs,
		ValidArgsFunction: func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
			return completeSkillNames(source, catalog, true)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			root, err := skillops.ResolveInstallRoot(opts.Dir)
			if err != nil {
				return err
			}

			result, err := skillops.Update(opts.Source, opts.Catalog, root, args, opts.DryRun)
			if err != nil {
				return err
			}
			emitLifecycleNotices(cmd.ErrOrStderr(), result.Notices)
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

type updateTextCodec struct{}

func (c *updateTextCodec) Format() format.Format { return "text" }

func (c *updateTextCodec) Encode(dst goio.Writer, value any) error {
	result, err := decodeInstallResult(value, "update")
	if err != nil {
		return err
	}

	return renderInstallResultText(dst, result, "Updated", "Would update", "in")
}

func (c *updateTextCodec) Decode(_ goio.Reader, _ any) error {
	return errors.New("update text codec does not support decoding")
}

type listOpts struct {
	Dir     string
	Source  fs.FS
	Catalog []byte
	IO      cmdio.Options
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("text")
	o.IO.RegisterCustomCodec("text", &listTextCodec{})
	o.IO.BindFlags(flags)

	flags.StringVar(&o.Dir, "dir", "~/.agents", "Root directory for the .agents installation (used to check installed status)")
}

func (o *listOpts) Validate() error {
	if o.Source == nil {
		return errors.New("skills source is not configured")
	}

	return o.IO.Validate()
}

func newListCommand(source fs.FS, catalog []byte) *cobra.Command {
	opts := &listOpts{Source: source, Catalog: catalog}

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List bundled skills and locally present retired gcx skills",
		Long:  "List bundled skills and locally present retired gcx skills, including descriptions, lifecycle status, replacements, and installation state. Skills not in the gcx catalog are unmanaged and omitted.",
		Example: `  gcx agent skills list
  gcx agent skills list -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			root, err := skillops.ResolveInstallRoot(opts.Dir)
			if err != nil {
				return err
			}

			result, err := skillops.List(opts.Source, opts.Catalog, root)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

type listResult = skillops.ListResult

type skillInfo = skillops.SkillState

type listTextCodec struct{}

func (c *listTextCodec) Format() format.Format { return "text" }

func (c *listTextCodec) Encode(dst goio.Writer, value any) error {
	var result listResult
	switch v := value.(type) {
	case listResult:
		result = v
	case *listResult:
		if v == nil {
			return errors.New("nil list result")
		}
		result = *v
	default:
		return fmt.Errorf("list text codec: unsupported value %T", value)
	}

	fmt.Fprintf(dst, "%d gcx skill(s)\n\n", result.SkillCount)

	if len(result.Skills) > 0 {
		if err := renderSkillsTable(dst, result.Skills); err != nil {
			return err
		}
	}

	return nil
}

func (c *listTextCodec) Decode(_ goio.Reader, _ any) error {
	return errors.New("list text codec does not support decoding")
}

func renderSkillsTable(dst goio.Writer, skills []skillInfo) error {
	t := style.NewTable("SKILL", "INSTALLED", "STATUS", "REPLACEMENT", "DESCRIPTION")
	for _, skill := range skills {
		installed := "no"
		if skill.Installed {
			installed = "yes"
		}
		if skill.Present && !skill.Installed {
			installed = "incomplete"
		}
		description := skill.ShortDescription
		if skill.Message != "" {
			description = skill.Message
		}
		t.Row(skill.Name, installed, string(skill.Status), skill.Replacement, description)
	}
	return t.Render(dst)
}

type getOpts struct {
	Source  fs.FS
	Catalog []byte
	IO      cmdio.Options
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("text")
	o.IO.RegisterCustomCodec("text", &getTextCodec{})
	o.IO.BindFlags(flags)
}

func (o *getOpts) Validate(args []string) error {
	if o.Source == nil {
		return errors.New("skills source is not configured")
	}
	if len(args) == 0 {
		return errors.New("provide a skill name")
	}
	if len(args) > 2 {
		return errors.New("provide a skill name and at most one reference path")
	}
	return o.IO.Validate()
}

func newGetCommand(source fs.FS, catalog []byte) *cobra.Command {
	opts := &getOpts{Source: source, Catalog: catalog}

	cmd := &cobra.Command{
		Use:   "get SKILL [REFERENCE]",
		Short: "Print a bundled skill's content without installing it",
		Long: `Print the content of a bundled gcx Agent Skill straight from the embedded bundle, without writing anything to ~/.agents.

By default the skill's SKILL.md body is printed. Pass a reference path (e.g. references/query-patterns.md) to print a single bundled reference file instead. Deprecated skills emit a warning; retired skills report their replacement, when one is recorded, instead of content.`,
		Example: `  gcx agent skills get create-dashboard
  gcx agent skills get create-dashboard -o json
  gcx agent skills get debug-with-grafana references/query-patterns.md`,
		Args: cobra.RangeArgs(1, 2),
		ValidArgsFunction: func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
			if len(args) > 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return completeSkillNames(source, catalog, false)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(args); err != nil {
				return err
			}

			name := args[0]
			reference := ""
			if len(args) == 2 {
				reference = args[1]
			}

			result, err := skillops.Get(opts.Source, opts.Catalog, name, reference)
			if err != nil {
				return err
			}
			if result.Status == skillops.Deprecated {
				emitLifecycleNotices(cmd.ErrOrStderr(), []skillops.LifecycleNotice{{Name: name, CatalogEntry: result.CatalogEntry}})
			}
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

type getResult = skillops.GetResult

type getTextCodec struct{}

func (c *getTextCodec) Format() format.Format { return "text" }

func (c *getTextCodec) Encode(dst goio.Writer, value any) error {
	var result getResult
	switch v := value.(type) {
	case getResult:
		result = v
	case *getResult:
		if v == nil {
			return errors.New("nil get result")
		}
		result = *v
	default:
		return fmt.Errorf("get text codec: unsupported value %T", value)
	}

	_, err := goio.WriteString(dst, result.Body)
	return err
}

func (c *getTextCodec) Decode(_ goio.Reader, _ any) error {
	return errors.New("get text codec does not support decoding")
}

type uninstallOpts struct {
	Dir     string
	All     bool
	Yes     bool
	DryRun  bool
	Source  fs.FS
	Catalog []byte
	IO      cmdio.Options
}

func (o *uninstallOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("text")
	o.IO.RegisterCustomCodec("text", &uninstallTextCodec{})
	o.IO.BindFlags(flags)

	flags.StringVar(&o.Dir, "dir", "~/.agents", "Root directory for the .agents installation")
	flags.BoolVar(&o.All, "all", false, "Uninstall all gcx-managed skills")
	flags.BoolVarP(&o.Yes, "yes", "y", false, "Auto-approve uninstalling all skills")
	flags.BoolVar(&o.DryRun, "dry-run", false, "Preview the uninstall without removing files")
}

func (o *uninstallOpts) Validate(args []string) error {
	if o.Source == nil {
		return errors.New("skills source is not configured")
	}
	if o.All && len(args) > 0 {
		return errors.New("skill names cannot be provided when --all is set")
	}
	if !o.All && len(args) == 0 {
		return errors.New("provide at least one skill name or use --all")
	}
	return o.IO.Validate()
}

func newUninstallCommand(source fs.FS, catalog []byte) *cobra.Command {
	opts := &uninstallOpts{Source: source, Catalog: catalog}

	cmd := &cobra.Command{
		Use:   "uninstall [SKILL]...",
		Short: "Uninstall gcx-managed skills from ~/.agents/skills",
		Long:  "Remove one or more current or retired gcx skills from a user-level .agents skills directory. Only names recorded in the gcx catalog can be uninstalled; unmanaged skills are never touched. Catalog names identify skills but do not prove ownership of local files.",
		Example: `  gcx agent skills uninstall setup-gcx
  gcx agent skills uninstall setup-gcx debug-with-grafana
  gcx agent skills uninstall --all --yes
  gcx agent skills uninstall --all --yes --dry-run`,
		Args: cobra.ArbitraryArgs,
		ValidArgsFunction: func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
			return completeSkillNames(source, catalog, true)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(args); err != nil {
				return err
			}

			cliOpts, err := config.LoadCLIOptions()
			if err != nil {
				return err
			}

			if opts.All && !opts.Yes && !cliOpts.AutoApprove {
				return errors.New("refusing to uninstall all gcx skills without --yes (or GCX_AUTO_APPROVE=1)")
			}

			root, err := skillops.ResolveInstallRoot(opts.Dir)
			if err != nil {
				return err
			}

			result, err := skillops.Uninstall(opts.Source, opts.Catalog, root, args, opts.All, opts.DryRun)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

type uninstallResult = skillops.UninstallResult

type uninstallTextCodec struct{}

func (c *uninstallTextCodec) Format() format.Format { return "text" }

func (c *uninstallTextCodec) Encode(dst goio.Writer, value any) error {
	var result uninstallResult
	switch v := value.(type) {
	case uninstallResult:
		result = v
	case *uninstallResult:
		if v == nil {
			return errors.New("nil uninstall result")
		}
		result = *v
	default:
		return fmt.Errorf("uninstall text codec: unsupported value %T", value)
	}

	status := "Uninstalled"
	removedLabel := "REMOVED"
	if result.DryRun {
		status = "Would uninstall"
		removedLabel = "WOULD REMOVE"
	}

	fmt.Fprintf(dst, "%s %d skill(s) from %s\n\n", status, result.RemovedCount, result.SkillsDir)

	t := style.NewTable("FIELD", "VALUE")
	t.Row("ROOT", result.Root)
	t.Row("SKILLS DIR", result.SkillsDir)
	t.Row("REQUESTED", strconv.Itoa(result.RequestedCount))
	t.Row(removedLabel, strconv.Itoa(result.RemovedCount))
	t.Row("MISSING", strconv.Itoa(result.MissingCount))
	if err := t.Render(dst); err != nil {
		return err
	}

	if len(result.Removed) > 0 {
		_, _ = fmt.Fprintln(dst)
		fmt.Fprintf(dst, "Removed: %s\n", strings.Join(result.Removed, ", "))
	}
	if len(result.Missing) > 0 {
		fmt.Fprintf(dst, "Missing: %s\n", strings.Join(result.Missing, ", "))
	}

	return nil
}

func (c *uninstallTextCodec) Decode(_ goio.Reader, _ any) error {
	return errors.New("uninstall text codec does not support decoding")
}
