package skills

import (
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const (
	skillsSourceEnv = "GCX_SKILLS_SOURCE_DIR"
	skillsTargetEnv = "GCX_SKILLS_INSTALL_DIR"
)

// embeddedSkillsFS contains the shipped skill assets.
//
// The folder lives under cmd/gcx/skills/skills so ownership stays with
// the command package and release artifacts remain self-contained.
//
//go:embed skills
var embeddedSkillsFS embed.FS

// skillSource describes where skill assets are loaded from.
// In normal operation we use embedded assets; GCX_SKILLS_SOURCE_DIR is a
// development/testing override to load from disk.
type skillSource struct {
	FS   fs.FS
	Root string
}

// Command returns the skills command group.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "skills",
		Aliases: []string{"skill"},
		Short:   "List and install bundled gcx agent skills",
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "small",
		},
	}

	cmd.AddCommand(newListCommand())
	cmd.AddCommand(newInstallCommand())

	return cmd
}

type listItem struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type listOpts struct {
	IO cmdio.Options
}

func (o *listOpts) setup(cmd *cobra.Command) {
	o.IO.DefaultFormat("text")
	o.IO.RegisterCustomCodec("text", &listTextCodec{})
	o.IO.BindFlags(cmd.Flags())
}

func (o *listOpts) Validate() error {
	return o.IO.Validate()
}

func newListCommand() *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List bundled skills available for installation",
		Args:  cobra.NoArgs,
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "small",
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			source, err := resolveSkillSource()
			if err != nil {
				return err
			}

			items, err := discoverSkills(source)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), items)
		},
	}
	opts.setup(cmd)
	return cmd
}

type installResult struct {
	Name   string `json:"name"`
	Target string `json:"target"`
	Force  bool   `json:"force"`
}

type installOpts struct {
	IO     cmdio.Options
	Target string
	Force  bool
}

func (o *installOpts) setup(cmd *cobra.Command) {
	o.IO.DefaultFormat("text")
	o.IO.RegisterCustomCodec("text", &installTextCodec{})
	o.IO.BindFlags(cmd.Flags())
	cmd.Flags().StringVar(&o.Target, "dir", defaultInstallDir(), "Install directory for skills")
	cmd.Flags().BoolVar(&o.Force, "force", false, "Overwrite the destination skill directory if it already exists")
}

func (o *installOpts) Validate() error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(o.Target) == "" {
		return errors.New("install directory cannot be empty")
	}
	return nil
}

func newInstallCommand() *cobra.Command {
	opts := &installOpts{}
	cmd := &cobra.Command{
		Use:   "install <skill-name>",
		Short: "Install a bundled skill into your local skills directory",
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "small",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			source, err := resolveSkillSource()
			if err != nil {
				return err
			}

			skillName := strings.TrimSpace(args[0])
			if skillName == "" {
				return errors.New("skill name cannot be empty")
			}
			if strings.ContainsRune(skillName, '/') || strings.ContainsRune(skillName, '\\') {
				return fmt.Errorf("invalid skill name %q: must not include path separators", skillName)
			}

			if !skillExists(source, skillName) {
				return fmt.Errorf("skill %q not found. Run `gcx skills list` to see available skills", skillName)
			}

			targetDir := filepath.Join(opts.Target, skillName)
			if err := installSkillDirectory(source, skillName, targetDir, opts.Force); err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), installResult{
				Name:   skillName,
				Target: targetDir,
				Force:  opts.Force,
			})
		},
	}
	opts.setup(cmd)
	return cmd
}

func defaultInstallDir() string {
	if fromEnv := strings.TrimSpace(os.Getenv(skillsTargetEnv)); fromEnv != "" {
		return fromEnv
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".claude/skills"
	}
	return filepath.Join(home, ".claude", "skills")
}

func resolveSkillSource() (skillSource, error) {
	if fromEnv := strings.TrimSpace(os.Getenv(skillsSourceEnv)); fromEnv != "" {
		st, err := os.Stat(fromEnv)
		if err != nil {
			return skillSource{}, fmt.Errorf("%s is set, but path is not readable: %w", skillsSourceEnv, err)
		}
		if !st.IsDir() {
			return skillSource{}, fmt.Errorf("%s must point to a directory: %s", skillsSourceEnv, fromEnv)
		}
		return skillSource{FS: os.DirFS(fromEnv), Root: "."}, nil
	}

	return skillSource{FS: embeddedSkillsFS, Root: "skills"}, nil
}

func discoverSkills(source skillSource) ([]listItem, error) {
	entries, err := fs.ReadDir(source.FS, source.Root)
	if err != nil {
		return nil, err
	}

	items := make([]listItem, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillName := entry.Name()
		skillMD := path.Join(source.Root, skillName, "SKILL.md")
		data, err := fs.ReadFile(source.FS, skillMD)
		if err != nil {
			continue
		}

		meta := readSkillMetadata(data)
		name := skillName
		if strings.TrimSpace(meta.Name) != "" {
			name = strings.TrimSpace(meta.Name)
		}
		items = append(items, listItem{Name: name, Description: strings.TrimSpace(meta.Description)})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})

	return items, nil
}

func skillExists(source skillSource, skillName string) bool {
	_, err := fs.ReadFile(source.FS, path.Join(source.Root, skillName, "SKILL.md"))
	return err == nil
}

type skillFrontMatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

func readSkillMetadata(data []byte) skillFrontMatter {
	content := string(data)
	if !strings.HasPrefix(content, "---\n") {
		return skillFrontMatter{}
	}
	trimmed := strings.TrimPrefix(content, "---\n")
	frontMatter, _, ok := strings.Cut(trimmed, "\n---\n")
	if !ok {
		return skillFrontMatter{}
	}

	meta := skillFrontMatter{}
	if err := yaml.Unmarshal([]byte(frontMatter), &meta); err != nil {
		// Some skill descriptions contain informal YAML (for example unquoted ":").
		// Fall back to a permissive parser so one bad frontmatter block does not
		// break listing for all skills.
		return parseFrontMatterFallback(frontMatter)
	}
	return meta
}

func parseFrontMatterFallback(frontMatter string) skillFrontMatter {
	lines := strings.Split(frontMatter, "\n")
	meta := skillFrontMatter{}
	for i := range lines {
		line := strings.TrimRight(lines[i], " \t")
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "name:"):
			meta.Name = strings.TrimSpace(strings.TrimPrefix(trimmed, "name:"))
		case strings.HasPrefix(trimmed, "description:"):
			v := strings.TrimSpace(strings.TrimPrefix(trimmed, "description:"))
			if v == ">" || v == "|" {
				var parts []string
				for j := i + 1; j < len(lines); j++ {
					next := strings.TrimRight(lines[j], " \t")
					if strings.TrimSpace(next) == "" {
						parts = append(parts, "")
						continue
					}
					if len(next) > 0 && (next[0] == ' ' || next[0] == '\t') {
						parts = append(parts, strings.TrimSpace(next))
						continue
					}
					break
				}
				meta.Description = strings.TrimSpace(strings.Join(parts, " "))
			} else {
				meta.Description = v
			}
		}
	}
	return meta
}

// installSkillDirectory copies one embedded skill tree to a target path.
// The copy preserves relative layout (including references/) and file modes.
func installSkillDirectory(source skillSource, skillName, targetDir string, force bool) error {
	if st, err := os.Stat(targetDir); err == nil && st.IsDir() {
		if !force {
			return fmt.Errorf("skill already installed at %s (use --force to overwrite)", targetDir)
		}
		if err := os.RemoveAll(targetDir); err != nil {
			return err
		}
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}

	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}

	sourceRoot := path.Join(source.Root, skillName)
	return fs.WalkDir(source.FS, sourceRoot, func(sourcePath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(sourcePath, sourceRoot+"/")
		if sourcePath == sourceRoot {
			rel = "."
		}
		if rel == "." {
			return nil
		}
		dstPath := filepath.Join(targetDir, filepath.FromSlash(rel))

		if d.IsDir() {
			// Use writable directory perms even for embedded assets, which are
			// exposed as read-only and would otherwise create 0555 directories.
			return os.MkdirAll(dstPath, 0o755)
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("symlinks are not supported in skill directory: %s", sourcePath)
		}

		data, err := fs.ReadFile(source.FS, sourcePath)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dstPath, data, info.Mode().Perm())
	})
}

type listTextCodec struct{}

func (c *listTextCodec) Format() format.Format { return "text" }

func (c *listTextCodec) Encode(w io.Writer, v any) error {
	items, ok := v.([]listItem)
	if !ok {
		return fmt.Errorf("unexpected payload type %T", v)
	}
	if len(items) == 0 {
		_, err := fmt.Fprintln(w, "No skills found.")
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', tabwriter.TabIndent|tabwriter.DiscardEmptyColumns)
	_, _ = fmt.Fprintln(tw, "NAME\tDESCRIPTION")
	for _, it := range items {
		_, _ = fmt.Fprintf(tw, "%s\t%s\n", it.Name, it.Description)
	}
	return tw.Flush()
}

func (c *listTextCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("skills list text codec does not support decoding")
}

type installTextCodec struct{}

func (c *installTextCodec) Format() format.Format { return "text" }

func (c *installTextCodec) Encode(w io.Writer, v any) error {
	res, ok := v.(installResult)
	if !ok {
		return fmt.Errorf("unexpected payload type %T", v)
	}
	_, err := fmt.Fprintf(w, "Installed skill %q to %s\n", res.Name, res.Target)
	return err
}

func (c *installTextCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("skills install text codec does not support decoding")
}
