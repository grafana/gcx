package skills

import (
	"errors"
	"fmt"
	goio "io"
	"io/fs"
	"slices"
	"sort"
	"strconv"
	"strings"

	claudeplugin "github.com/grafana/gcx/claude-plugin"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
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
		Long:  "Manage bundled gcx Agent Skills and explicitly imported user workflows. Search metadata first, then get the selected body or reference for your coding agent.",
	}

	source, catalog := claudeplugin.SkillsFS(), claudeplugin.SkillsCatalog()
	cmd.AddCommand(newInstallCommand(source, catalog))
	cmd.AddCommand(newUpdateCommand(source, catalog))
	cmd.AddCommand(newListCommand(source, catalog))
	cmd.AddCommand(newGetCommand(source, catalog))
	cmd.AddCommand(newUninstallCommand(source, catalog))
	cmd.AddCommand(newSearchCommand(source, catalog))

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
	for _, arg := range args {
		if skillops.IsLocalSkillPath(arg) && (o.All || len(args) != 1 || o.Force) {
			return errors.New("import one explicit local directory without --all or --force; use update custom/<name> --from for replacement")
		}
	}

	return o.IO.Validate()
}

func newInstallCommand(source fs.FS, catalog []byte) *cobra.Command {
	opts := &installOpts{Source: source, Catalog: catalog}

	cmd := &cobra.Command{
		Use:   "install [SKILL]...",
		Short: "Install bundled skills or import a local custom workflow",
		Long:  "Install bundled skills by name, or import one explicit local directory (./path or absolute path) as a custom skill. Custom content is copied to <dir>/custom-skills and read through custom/<name>; it is not automatically activated by a harness. Use --all for bundled skills only. Deprecated skills warn; retired skills cannot be installed.",
		Example: `  gcx agent skills install setup-gcx
  gcx agent skills install setup-gcx debug-with-grafana manage-dashboards
  gcx agent skills install --all
  gcx agent skills install --all --dry-run
  gcx agent skills install setup-gcx --force
  gcx agent skills install ./service-debug --dry-run`,
		Args: cobra.ArbitraryArgs,
		ValidArgsFunction: func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
			return completeSkillNames(catalog, false)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(args); err != nil {
				if slices.ContainsFunc(args, skillops.IsLocalSkillPath) {
					return gcxerrors.NewCommandUsageError(cmd, err.Error(), err)
				}
				return err
			}

			root, err := skillops.ResolveInstallRoot(opts.Dir)
			if err != nil {
				return err
			}
			if len(args) == 1 && skillops.IsLocalSkillPath(args[0]) {
				result, err := skillops.InstallCustom(root, args[0], "", opts.DryRun)
				if err != nil {
					return err
				}
				return opts.IO.Encode(cmd.OutOrStdout(), result)
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

func completeSkillNames(data []byte, includeRetired bool) ([]string, cobra.ShellCompDirective) {
	catalog, err := skillops.LoadCatalog(data)
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
	if result, ok := value.(skillops.CustomMutation); ok {
		return renderCustomMutation(dst, result)
	}
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
	From    string
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
	flags.StringVar(&o.From, "from", "", "Local directory replacing one explicitly named custom skill")
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
		Short: "Refresh installed bundled skills or replace one custom workflow",
		Long:  "With no skill names, refresh installed bundled skills only. Retired files and custom content are preserved. To replace a custom skill, specify custom/<name> --from ./directory. Mixed bundled/custom batches are not supported.",
		Example: `  gcx agent skills update
  gcx agent skills update --dry-run
  gcx agent skills update setup-gcx debug-with-grafana
  gcx agent skills update custom/service-debug --from ./service-debug --dry-run`,
		Args: cobra.ArbitraryArgs,
		ValidArgsFunction: func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
			return completeSkillNames(catalog, true)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			custom := len(args) == 1 && skillops.IsCustomID(args[0])
			if cmd.Flags().Changed("from") && (!custom || strings.TrimSpace(opts.From) == "") {
				return gcxerrors.NewCommandUsageError(cmd, "--from requires one custom/<name> target and a nonempty local directory", nil)
			}
			for _, arg := range args {
				if skillops.IsCustomID(arg) && (!custom || !cmd.Flags().Changed("from")) {
					return gcxerrors.NewCommandUsageError(cmd, "update one custom/<name> with --from; do not mix bundled and custom targets", nil)
				}
			}

			root, err := skillops.ResolveInstallRoot(opts.Dir)
			if err != nil {
				return err
			}

			if custom {
				result, err := skillops.InstallCustom(root, opts.From, args[0], opts.DryRun)
				if err != nil {
					return err
				}
				return opts.IO.Encode(cmd.OutOrStdout(), result)
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
	if result, ok := value.(skillops.CustomMutation); ok {
		return renderCustomMutation(dst, result)
	}
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
	Dir       string
	Selection string
	Source    fs.FS
	Catalog   []byte
	IO        cmdio.Options
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("text")
	o.IO.RegisterCustomCodec("text", &listTextCodec{})
	o.IO.BindFlags(flags)

	flags.StringVar(&o.Dir, "dir", "~/.agents", "Root directory for the .agents installation (used to check installed status)")
	flags.StringVar(&o.Selection, "source", "bundled", "Skill source: bundled, custom, or all (local metadata only)")
}

func (o *listOpts) Validate() error {
	if o.Source == nil {
		return errors.New("skills source is not configured")
	}
	if err := skillops.ValidateSource(o.Selection); err != nil {
		return err
	}

	return o.IO.Validate()
}

func newListCommand(source fs.FS, catalog []byte) *cobra.Command {
	opts := &listOpts{Source: source, Catalog: catalog}

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List bundled skills or explicitly selected custom metadata",
		Long:  "List bundled metadata by default. Use --source custom or --source all to include explicitly imported local skills and their identities. Unrelated harness skills are omitted. Bodies are loaded only by get.",
		Example: `  gcx agent skills list
  gcx agent skills list -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				if cmd.Flags().Changed("source") {
					return gcxerrors.NewCommandUsageError(cmd, err.Error(), err)
				}
				return err
			}

			root, err := skillops.ResolveInstallRoot(opts.Dir)
			if err != nil {
				return err
			}

			var result listResult
			if cmd.Flags().Changed("source") {
				result, err = skillops.ListSelected(opts.Source, opts.Catalog, root, opts.Selection)
			} else {
				result, err = skillops.List(opts.Source, opts.Catalog, root)
			}
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
		name := skill.Name
		if skill.ID != "" {
			name = skill.ID
		}
		t.Row(name, installed, string(skill.Status), skill.Replacement, description)
	}
	return t.Render(dst)
}

type getOpts struct {
	Dir     string
	Source  fs.FS
	Catalog []byte
	IO      cmdio.Options
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	flags.StringVar(&o.Dir, "dir", "~/.agents", "Root directory used only for explicit custom/<name> reads")
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
		Short: "Read a bundled or explicitly selected custom skill",
		Long: `Read a skill or one relative reference without executing its content.

Unqualified names always read the embedded bundle. Explicit custom/<name> reads a local import under --dir instead. By default SKILL.md is printed; pass a relative reference path for one file. This command reads instructions without executing them. Deprecated skills warn; retired skills report replacements.`,
		Example: `  gcx agent skills get create-dashboard
  gcx agent skills get create-dashboard -o json
  gcx agent skills get debug-with-grafana references/query-patterns.md`,
		Args: cobra.RangeArgs(1, 2),
		ValidArgsFunction: func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
			if len(args) > 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return completeSkillNames(catalog, false)
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

			var result getResult
			var err error
			if skillops.IsCustomID(name) {
				root, rootErr := skillops.ResolveInstallRoot(opts.Dir)
				if rootErr != nil {
					return rootErr
				}
				result, err = skillops.GetCustom(root, name, reference)
			} else {
				result, err = skillops.Get(opts.Source, opts.Catalog, name, reference)
			}
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
		Short: "Remove explicitly selected bundled or custom skill installations",
		Long:  "Remove named bundled skills, or one explicitly imported custom/<name>. --all targets bundled skills only and leaves custom skills intact. Unrelated local skills are never targeted. Custom deletion requires --yes or GCX_AUTO_APPROVE=1 unless --dry-run is used.",
		Example: `  gcx agent skills uninstall setup-gcx
  gcx agent skills uninstall setup-gcx debug-with-grafana
  gcx agent skills uninstall --all --yes
  gcx agent skills uninstall --all --yes --dry-run`,
		Args: cobra.ArbitraryArgs,
		ValidArgsFunction: func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
			return completeSkillNames(catalog, true)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(args); err != nil {
				return err
			}
			for _, arg := range args {
				if skillops.IsCustomID(arg) && (opts.All || len(args) != 1) {
					return gcxerrors.NewCommandUsageError(cmd, "uninstall one custom/<name> at a time without --all", nil)
				}
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

			if len(args) == 1 && skillops.IsCustomID(args[0]) {
				if !opts.DryRun && !opts.Yes && !cliOpts.AutoApprove {
					return gcxerrors.NewCommandUsageError(cmd, "custom skill removal requires --yes or GCX_AUTO_APPROVE=1 (or preview with --dry-run)", nil)
				}
				result, err := skillops.UninstallCustom(root, args[0], opts.DryRun)
				if err != nil {
					return err
				}
				return opts.IO.Encode(cmd.OutOrStdout(), result)
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
	if result, ok := value.(skillops.CustomMutation); ok {
		return renderCustomMutation(dst, result)
	}
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

func renderCustomMutation(dst goio.Writer, result skillops.CustomMutation) error {
	_, err := fmt.Fprintf(dst, "%s\npath: %s\nchanged: %t\ndry-run: %t\n", result.ID, result.Path, result.Changed, result.DryRun)
	return err
}

func (c *uninstallTextCodec) Decode(_ goio.Reader, _ any) error {
	return errors.New("uninstall text codec does not support decoding")
}
