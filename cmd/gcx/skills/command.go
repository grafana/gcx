package skills

import (
	"bytes"
	"errors"
	"fmt"
	goio "io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	claudeplugin "github.com/grafana/gcx/claude-plugin"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
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

	cmd.AddCommand(newInstallCommand(claudeplugin.SkillsFS()))

	return cmd
}

type installOpts struct {
	Dir    string
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

	flags.StringVar(&o.Dir, "dir", defaultRoot, "Root directory for the .agents installation (default: ~/.agents)")
	flags.BoolVar(&o.Force, "force", false, "Overwrite existing differing files managed by the gcx skills bundle")
	flags.BoolVar(&o.DryRun, "dry-run", false, "Preview the installation without writing files")
}

func (o *installOpts) Validate() error {
	if o.Source == nil {
		return errors.New("skills source is not configured")
	}

	return o.IO.Validate()
}

func newInstallCommand(source fs.FS) *cobra.Command {
	opts := &installOpts{Source: source}

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install the portable gcx skill bundle into ~/.agents/skills",
		Long:  "Install the canonical portable gcx Agent Skills bundle into a user-level .agents directory for tools that follow the .agents skill convention.",
		Example: `  gcx skills install
  gcx skills install --dry-run
  gcx skills install --dir ~/.agents --force`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			root, err := resolveInstallRoot(opts.Dir)
			if err != nil {
				return err
			}

			result, err := installSkills(opts.Source, root, opts.Force, opts.DryRun)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}

	opts.setup(cmd.Flags())

	return cmd
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
	t.Row("SKILLS", fmt.Sprintf("%d", result.SkillCount))
	t.Row("FILES", fmt.Sprintf("%d", result.FileCount))
	t.Row(writtenLabel, fmt.Sprintf("%d", result.Written))
	t.Row("OVERWRITTEN", fmt.Sprintf("%d", result.Overwritten))
	t.Row("UNCHANGED", fmt.Sprintf("%d", result.Unchanged))
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

func installSkills(source fs.FS, root string, force bool, dryRun bool) (installResult, error) {
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

	skillSet := make(map[string]struct{})

	err := fs.WalkDir(source, ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == "." {
			return nil
		}

		parts := strings.Split(path, "/")
		if len(parts) > 0 && parts[0] != "" {
			skillSet[parts[0]] = struct{}{}
		}

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

func syncFile(source fs.FS, sourcePath string, targetPath string, force bool, dryRun bool) (changed bool, overwritten bool, err error) {
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
