package skills

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	SourceEnv = "GCX_SKILLS_SOURCE_DIR"
	TargetEnv = "GCX_SKILLS_INSTALL_DIR"
)

// Source identifies where skill assets should be read from.
type Source struct {
	FS   fs.FS
	Root string
}

// Skill is metadata for one bundled skill.
type Skill struct {
	Name        string
	Description string
}

// Manager implements skill discovery and installation logic.
type Manager struct {
	embeddedFS   fs.FS
	embeddedRoot string
}

func NewManager(embeddedFS fs.FS, embeddedRoot string) *Manager {
	return &Manager{embeddedFS: embeddedFS, embeddedRoot: embeddedRoot}
}

func (m *Manager) DefaultInstallDir() string {
	if fromEnv := strings.TrimSpace(os.Getenv(TargetEnv)); fromEnv != "" {
		return fromEnv
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".claude/skills"
	}
	return filepath.Join(home, ".claude", "skills")
}

func (m *Manager) ResolveSource() (Source, error) {
	if fromEnv := strings.TrimSpace(os.Getenv(SourceEnv)); fromEnv != "" {
		st, err := os.Stat(fromEnv)
		if err != nil {
			return Source{}, fmt.Errorf("%s is set, but path is not readable: %w", SourceEnv, err)
		}
		if !st.IsDir() {
			return Source{}, fmt.Errorf("%s must point to a directory: %s", SourceEnv, fromEnv)
		}
		return Source{FS: os.DirFS(fromEnv), Root: "."}, nil
	}
	return Source{FS: m.embeddedFS, Root: m.embeddedRoot}, nil
}

func ValidateSkillName(skillName string) error {
	skillName = strings.TrimSpace(skillName)
	if skillName == "" {
		return errors.New("skill name cannot be empty")
	}
	if strings.ContainsRune(skillName, '/') || strings.ContainsRune(skillName, '\\') {
		return fmt.Errorf("invalid skill name %q: must not include path separators", skillName)
	}
	return nil
}

func (m *Manager) List(source Source) ([]Skill, error) {
	entries, err := fs.ReadDir(source.FS, source.Root)
	if err != nil {
		return nil, err
	}

	items := make([]Skill, 0, len(entries))
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
		items = append(items, Skill{Name: name, Description: strings.TrimSpace(meta.Description)})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})

	return items, nil
}

func (m *Manager) Exists(source Source, skillName string) bool {
	_, err := fs.ReadFile(source.FS, path.Join(source.Root, skillName, "SKILL.md"))
	return err == nil
}

// Install copies one embedded skill tree to a target path.
// The copy preserves relative layout (including references/) and file modes.
func (m *Manager) Install(source Source, skillName, targetDir string, force bool) error {
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
