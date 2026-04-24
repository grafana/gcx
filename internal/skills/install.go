package skills

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// InstallResult summarizes an install/update operation against a .agents root.
type InstallResult struct {
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

// BundledSkillNames returns all top-level bundled skill directory names.
func BundledSkillNames(source fs.FS) ([]string, error) {
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
	sort.Strings(names)
	return names, nil
}

// InstalledBundledSkillNames returns the subset of bundled skills that are
// already installed under root/skills.
func InstalledBundledSkillNames(source fs.FS, root string) ([]string, error) {
	bundled, err := BundledSkillNames(source)
	if err != nil {
		return nil, err
	}

	skillsDir := filepath.Join(root, "skills")
	installed := make([]string, 0, len(bundled))
	for _, name := range bundled {
		if isSkillInstalled(skillsDir, name) {
			installed = append(installed, name)
		}
	}

	sort.Strings(installed)
	return installed, nil
}

// Install installs bundled skills from source into root. When filter is nil all
// skills are installed; otherwise only skills whose name is in the filter set.
func Install(source fs.FS, root string, filter map[string]struct{}, force bool, dryRun bool) (InstallResult, error) {
	if source == nil {
		return InstallResult{}, errors.New("skills source is nil")
	}

	root = filepath.Clean(root)
	result := InstallResult{
		Root:      root,
		SkillsDir: filepath.Join(root, "skills"),
		DryRun:    dryRun,
		Force:     force,
	}

	if filter != nil {
		available, err := BundledSkillNames(source)
		if err != nil {
			return InstallResult{}, err
		}
		avail := make(map[string]struct{}, len(available))
		for _, n := range available {
			avail[n] = struct{}{}
		}
		for name := range filter {
			if _, ok := avail[name]; !ok {
				return InstallResult{}, fmt.Errorf("unknown skill %q (use 'gcx skills list' to see available skills)", name)
			}
		}
	}

	skillSet := make(map[string]struct{})
	if err := fs.WalkDir(source, ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == "." {
			return nil
		}

		parts := strings.Split(path, "/")
		skillName := parts[0]
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
	}); err != nil {
		return InstallResult{}, err
	}

	result.Skills = sortedKeys(skillSet)
	result.SkillCount = len(result.Skills)
	return result, nil
}

// Update applies the same targeting semantics as `gcx skills update`.
// With no targets, only already-installed bundled skills are updated.
func Update(source fs.FS, root string, targets []string, dryRun bool) (InstallResult, error) {
	installedTargets, err := InstalledBundledSkillNames(source, root)
	if err != nil {
		return InstallResult{}, err
	}

	resolvedTargets := targets
	if len(resolvedTargets) == 0 {
		resolvedTargets = installedTargets
	} else {
		bundledTargets, err := BundledSkillNames(source)
		if err != nil {
			return InstallResult{}, err
		}

		installedSet := make(map[string]struct{}, len(installedTargets))
		for _, name := range installedTargets {
			installedSet[name] = struct{}{}
		}

		bundledSet := make(map[string]struct{}, len(bundledTargets))
		for _, name := range bundledTargets {
			bundledSet[name] = struct{}{}
		}

		for _, name := range resolvedTargets {
			if _, ok := bundledSet[name]; !ok {
				return InstallResult{}, fmt.Errorf("unknown skill %q (use 'gcx skills list' to see available skills)", name)
			}
			if _, ok := installedSet[name]; !ok {
				return InstallResult{}, fmt.Errorf("skill %q is not installed; use 'gcx skills install %s' to install it first", name, name)
			}
		}
	}

	filter := make(map[string]struct{}, len(resolvedTargets))
	for _, name := range resolvedTargets {
		filter[name] = struct{}{}
	}

	return Install(source, root, filter, true, dryRun)
}

// ResolveInstallRoot resolves ~ and returns an absolute .agents root path.
func ResolveInstallRoot(root string) (string, error) {
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

func isSkillInstalled(skillsDir string, name string) bool {
	info, err := os.Stat(filepath.Join(skillsDir, name, "SKILL.md"))
	return err == nil && !info.IsDir()
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

func defaultAgentsRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determine home directory: %w", err)
	}
	return filepath.Join(home, ".agents"), nil
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
