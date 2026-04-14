package skills

import (
	"bytes"
	"errors"
	"fmt"
	goio "io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	claudeplugin "github.com/grafana/gcx/claude-plugin"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"gopkg.in/yaml.v3"
)

// Command returns the top-level skills command group.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Manage portable gcx Agent Skills",
		Long:  "Install the canonical portable gcx Agent Skills bundle for .agents-compatible agent harnesses.",
	}

	cmd.AddCommand(newInstallCommand(claudeplugin.SkillsFS()))
	cmd.AddCommand(newListCommand(claudeplugin.SkillsFS()))
	cmd.AddCommand(newUninstallCommand(claudeplugin.SkillsFS()))

	return cmd
}

type installOpts struct {
	Dir    string
	All    bool
	Force  bool
	DryRun bool
	Source fs.FS
	IO     cmdio.Options
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

func newInstallCommand(source fs.FS) *cobra.Command {
	opts := &installOpts{Source: source}

	cmd := &cobra.Command{
		Use:   "install [SKILL]...",
		Short: "Install bundled gcx skills into ~/.agents/skills",
		Long:  "Install one or more bundled gcx Agent Skills into a user-level .agents directory for tools that follow the .agents skill convention. Use --all to install the entire bundle.",
		Example: `  gcx skills install setup-gcx
  gcx skills install setup-gcx debug-with-grafana explore-datasources
  gcx skills install --all
  gcx skills install --all --dry-run
  gcx skills install setup-gcx --force`,
		Args: cobra.ArbitraryArgs,
		ValidArgsFunction: func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
			names, err := bundledSkillNames(source)
			if err != nil {
				return nil, cobra.ShellCompDirectiveError
			}
			return names, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(args); err != nil {
				return err
			}

			root, err := resolveInstallRoot(opts.Dir)
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

			result, err := installSkills(opts.Source, root, filter, opts.Force, opts.DryRun)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

func bundledSkillNames(source fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(source, ".")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

type installResult struct {
	Root        string   `json:"root"`
	SkillsDir   string   `json:"skills_dir"`
	Skills      []string `json:"skills"`
	SkillCount  int      `json:"skill_count"`
	FileCount   int      `json:"file_count"`
	Written     int      `json:"written"`
	Overwritten int      `json:"overwritten"`
	Unchanged   int      `json:"unchanged"`
	DryRun      bool     `json:"dry_run"`
	Force       bool     `json:"force"`
}

type installTextCodec struct{}

func (c *installTextCodec) Format() format.Format { return "text" }

func (c *installTextCodec) Encode(dst goio.Writer, value any) error {
	var result installResult
	switch v := value.(type) {
	case installResult:
		result = v
	case *installResult:
		if v == nil {
			return errors.New("nil install result")
		}
		result = *v
	default:
		return fmt.Errorf("install text codec: unsupported value %T", value)
	}

	status := "Installed"
	writtenLabel := "WRITTEN"
	if result.DryRun {
		status = "Would install"
		writtenLabel = "WOULD WRITE"
	}

	fmt.Fprintf(dst, "%s %d skill(s) to %s\n\n", status, result.SkillCount, result.SkillsDir)

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

func (c *installTextCodec) Decode(_ goio.Reader, _ any) error {
	return errors.New("install text codec does not support decoding")
}

type listOpts struct {
	Dir    string
	Source fs.FS
	IO     cmdio.Options
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

func newListCommand(source fs.FS) *cobra.Command {
	opts := &listOpts{Source: source}

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List skills bundled with the gcx binary",
		Long:  "List skills bundled with the gcx binary, including each skill's short description and install status.",
		Example: `  gcx skills list
  gcx skills list -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			root, err := resolveInstallRoot(opts.Dir)
			if err != nil {
				return err
			}

			result, err := listBundledSkills(opts.Source, root)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

type listResult struct {
	Skills     []skillInfo `json:"skills"`
	SkillCount int         `json:"skill_count"`
}

type skillInfo struct {
	Name             string `json:"name"`
	ShortDescription string `json:"short_description"`
	Installed        bool   `json:"installed"`
}

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

	fmt.Fprintf(dst, "%d skill(s) bundled with gcx\n\n", result.SkillCount)

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

func listBundledSkills(source fs.FS, installRoot string) (listResult, error) {
	result := listResult{}

	entries, err := fs.ReadDir(source, ".")
	if err != nil {
		return listResult{}, err
	}

	skillsDir := filepath.Join(installRoot, "skills")

	result.Skills = make([]skillInfo, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		skillDocPath := path.Join(entry.Name(), "SKILL.md")
		data, err := fs.ReadFile(source, skillDocPath)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return listResult{}, err
		}

		installed := isSkillInstalled(skillsDir, entry.Name())

		result.Skills = append(result.Skills, skillInfo{
			Name:             entry.Name(),
			ShortDescription: extractSkillShortDescription(data),
			Installed:        installed,
		})
	}

	sort.Slice(result.Skills, func(i int, j int) bool {
		return result.Skills[i].Name < result.Skills[j].Name
	})
	result.SkillCount = len(result.Skills)

	return result, nil
}

func isSkillInstalled(skillsDir string, name string) bool {
	info, err := os.Stat(filepath.Join(skillsDir, name, "SKILL.md"))
	return err == nil && !info.IsDir()
}

type skillFrontMatter struct {
	Description string `yaml:"description"`
}

func extractSkillShortDescription(data []byte) string {
	description, err := extractSkillDescriptionFromMarkdown(data)
	if err != nil {
		return normalizeDescription(fallbackSkillDescription(data))
	}

	return normalizeDescription(description)
}

func extractSkillDescriptionFromMarkdown(data []byte) (string, error) {
	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(content, "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "---" {
		return "", errors.New("missing front matter")
	}

	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return "", errors.New("unterminated front matter")
	}

	var meta skillFrontMatter
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:end], "\n")), &meta); err != nil {
		return "", err
	}

	return meta.Description, nil
}

func normalizeDescription(description string) string {
	normalized := strings.Join(strings.Fields(description), " ")
	return normalized
}

func fallbackSkillDescription(data []byte) string {
	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	for line := range strings.SplitSeq(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "---") {
			continue
		}
		return trimmed
	}

	return ""
}

func renderSkillsTable(dst goio.Writer, skills []skillInfo) error {
	t := style.NewTable("SKILL", "INSTALLED", "DESCRIPTION")
	for _, skill := range skills {
		installed := "no"
		if skill.Installed {
			installed = "yes"
		}
		t.Row(skill.Name, installed, skill.ShortDescription)
	}
	return t.Render(dst)
}

// installSkills installs skills from source into root. When filter is nil all
// skills are installed; otherwise only skills whose name is in the filter set.
func installSkills(source fs.FS, root string, filter map[string]struct{}, force bool, dryRun bool) (installResult, error) {
	if source == nil {
		return installResult{}, errors.New("skills source is nil")
	}

	root = filepath.Clean(root)
	result := installResult{
		Root:      root,
		SkillsDir: filepath.Join(root, "skills"),
		DryRun:    dryRun,
		Force:     force,
	}

	if err := ensureDirectory(result.SkillsDir, dryRun); err != nil {
		return installResult{}, err
	}

	// Validate requested skill names exist in the bundle.
	if filter != nil {
		available, err := bundledSkillNames(source)
		if err != nil {
			return installResult{}, err
		}
		avail := make(map[string]struct{}, len(available))
		for _, n := range available {
			avail[n] = struct{}{}
		}
		for name := range filter {
			if _, ok := avail[name]; !ok {
				return installResult{}, fmt.Errorf("unknown skill %q (use 'gcx skills list' to see available skills)", name)
			}
		}
	}

	skillSet := make(map[string]struct{})

	err := fs.WalkDir(source, ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == "." {
			return nil
		}

		parts := strings.Split(path, "/")
		skillName := parts[0]

		// Skip skills not in the filter.
		if filter != nil {
			if _, ok := filter[skillName]; !ok {
				if d.IsDir() && len(parts) == 1 {
					return fs.SkipDir
				}
				return nil
			}
		}

		skillSet[skillName] = struct{}{}

		targetPath := filepath.Join(result.SkillsDir, filepath.FromSlash(path))
		if d.IsDir() {
			return ensureDirectory(targetPath, dryRun)
		}

		result.FileCount++

		if err := ensureDirectory(filepath.Dir(targetPath), dryRun); err != nil {
			return err
		}

		changed, overwritten, err := syncFile(source, path, targetPath, force, dryRun)
		if err != nil {
			return err
		}
		if !changed {
			result.Unchanged++
			return nil
		}
		if overwritten {
			result.Overwritten++
			return nil
		}
		result.Written++
		return nil
	})
	if err != nil {
		return installResult{}, err
	}

	result.Skills = sortedKeys(skillSet)
	result.SkillCount = len(result.Skills)

	return result, nil
}

func syncFile(source fs.FS, sourcePath string, targetPath string, force bool, dryRun bool) (bool, bool, error) {
	sourceData, err := fs.ReadFile(source, sourcePath)
	if err != nil {
		return false, false, err
	}

	existingData, err := os.ReadFile(targetPath)
	switch {
	case err == nil:
		if bytes.Equal(existingData, sourceData) {
			return false, false, nil
		}
		if !force {
			return false, false, fmt.Errorf("destination file differs: %s (use --force to overwrite)", targetPath)
		}
		if dryRun {
			return true, true, nil
		}
		return true, true, os.WriteFile(targetPath, sourceData, fileMode(source, sourcePath))
	case errors.Is(err, os.ErrNotExist):
		if dryRun {
			return true, false, nil
		}
		return true, false, os.WriteFile(targetPath, sourceData, fileMode(source, sourcePath))
	default:
		return false, false, err
	}
}

func ensureDirectory(path string, dryRun bool) error {
	info, err := os.Stat(path)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("destination path exists and is not a directory: %s", path)
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if dryRun {
		return nil
	}
	return os.MkdirAll(path, 0o755)
}

func fileMode(source fs.FS, path string) fs.FileMode {
	info, err := fs.Stat(source, path)
	if err != nil {
		return 0o644
	}
	if perm := info.Mode().Perm(); perm != 0 {
		return perm
	}
	return 0o644
}

type uninstallOpts struct {
	Dir    string
	All    bool
	Yes    bool
	DryRun bool
	Source fs.FS
	IO     cmdio.Options
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

func newUninstallCommand(source fs.FS) *cobra.Command {
	opts := &uninstallOpts{Source: source}

	cmd := &cobra.Command{
		Use:   "uninstall [SKILL]...",
		Short: "Uninstall gcx-managed skills from ~/.agents/skills",
		Long:  "Remove one or more gcx-managed skills from a user-level .agents skills directory. Only skills bundled with gcx can be uninstalled; non-gcx skills are never touched.",
		Example: `  gcx skills uninstall setup-gcx
  gcx skills uninstall setup-gcx debug-with-grafana
  gcx skills uninstall --all --yes
  gcx skills uninstall --all --yes --dry-run`,
		Args: cobra.ArbitraryArgs,
		ValidArgsFunction: func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
			names, err := bundledSkillNames(source)
			if err != nil {
				return nil, cobra.ShellCompDirectiveError
			}
			return names, cobra.ShellCompDirectiveNoFileComp
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

			root, err := resolveInstallRoot(opts.Dir)
			if err != nil {
				return err
			}

			bundled, err := bundledSkillNames(opts.Source)
			if err != nil {
				return err
			}
			bundledSet := make(map[string]struct{}, len(bundled))
			for _, name := range bundled {
				bundledSet[name] = struct{}{}
			}

			targets := args
			if opts.All {
				// --all only targets gcx-bundled skills, never non-gcx skills.
				targets = bundled
			} else {
				for _, name := range args {
					if _, ok := bundledSet[name]; !ok {
						return fmt.Errorf("unknown skill %q (use 'gcx skills list' to see gcx-managed skills)", name)
					}
				}
			}

			result, err := uninstallSkills(root, targets, opts.DryRun)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

type uninstallResult struct {
	Root           string   `json:"root"`
	SkillsDir      string   `json:"skills_dir"`
	Requested      []string `json:"requested"`
	RequestedCount int      `json:"requested_count"`
	Removed        []string `json:"removed"`
	RemovedCount   int      `json:"removed_count"`
	Missing        []string `json:"missing"`
	MissingCount   int      `json:"missing_count"`
	DryRun         bool     `json:"dry_run"`
}

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

func uninstallSkills(root string, names []string, dryRun bool) (uninstallResult, error) {
	root = filepath.Clean(root)
	result := uninstallResult{
		Root:      root,
		SkillsDir: filepath.Join(root, "skills"),
		DryRun:    dryRun,
	}

	seen := make(map[string]struct{}, len(names))
	result.Requested = make([]string, 0, len(names))
	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		if err := validateSkillName(trimmed); err != nil {
			return uninstallResult{}, err
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result.Requested = append(result.Requested, trimmed)
	}

	sort.Strings(result.Requested)
	result.RequestedCount = len(result.Requested)
	result.Removed = make([]string, 0, len(result.Requested))
	result.Missing = make([]string, 0, len(result.Requested))

	for _, name := range result.Requested {
		targetPath := filepath.Join(result.SkillsDir, name)
		info, err := os.Stat(targetPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				result.Missing = append(result.Missing, name)
				continue
			}
			return uninstallResult{}, err
		}
		if !info.IsDir() {
			return uninstallResult{}, fmt.Errorf("destination path exists and is not a directory: %s", targetPath)
		}

		if !dryRun {
			if err := os.RemoveAll(targetPath); err != nil {
				return uninstallResult{}, err
			}
		}

		result.Removed = append(result.Removed, name)
	}

	result.RemovedCount = len(result.Removed)
	result.MissingCount = len(result.Missing)

	return result, nil
}

func validateSkillName(name string) error {
	if name == "." || name == ".." {
		return fmt.Errorf("invalid skill name %q", name)
	}
	if strings.Contains(name, "/") || strings.Contains(name, `\`) {
		return fmt.Errorf("invalid skill name %q", name)
	}
	if filepath.Base(name) != name {
		return fmt.Errorf("invalid skill name %q", name)
	}

	return nil
}

func defaultAgentsRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determine home directory: %w", err)
	}
	return filepath.Join(home, ".agents"), nil
}

func resolveInstallRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		defaultRoot, err := defaultAgentsRoot()
		if err != nil {
			return "", err
		}
		root = defaultRoot
	}

	if root == "~" || strings.HasPrefix(root, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("determine home directory: %w", err)
		}
		if root == "~" {
			root = home
		} else {
			root = filepath.Join(home, root[2:])
		}
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve install root %q: %w", root, err)
	}

	return filepath.Clean(absRoot), nil
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
