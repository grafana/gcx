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
	Root        string            `json:"root"`
	SkillsDir   string            `json:"skills_dir"`
	Skills      []string          `json:"skills"`
	SkillCount  int               `json:"skill_count"`
	FileCount   int               `json:"file_count"`
	Written     int               `json:"written"`
	Overwritten int               `json:"overwritten"`
	Unchanged   int               `json:"unchanged"`
	DryRun      bool              `json:"dry_run"`
	Force       bool              `json:"force"`
	Notices     []LifecycleNotice `json:"notices,omitempty"`
}

// Install installs current bundled skills. A nil filter selects all active and
// deprecated skills; retired and unmanaged local skills are never installed.
func Install(source fs.FS, catalog []byte, root string, filter map[string]struct{}, force bool, dryRun bool) (InstallResult, error) {
	states, err := Reconcile(source, catalog, root)
	if err != nil {
		return InstallResult{}, err
	}
	selected := make(map[string]struct{})
	var notices []LifecycleNotice
	for _, state := range states {
		if filter != nil {
			if _, ok := filter[state.Name]; !ok {
				continue
			}
		}
		if state.Status == Unmanaged || state.Status == Retired {
			if filter != nil && state.Status == Retired {
				return InstallResult{}, fmt.Errorf("%s; retired skills cannot be installed", LifecycleNotice{Name: state.Name, CatalogEntry: state.CatalogEntry})
			}
			continue
		}
		selected[state.Name] = struct{}{}
		if state.Status == Deprecated {
			notices = append(notices, LifecycleNotice{Name: state.Name, CatalogEntry: state.CatalogEntry})
		}
	}
	for name := range filter {
		if _, ok := selected[name]; !ok {
			return InstallResult{}, fmt.Errorf("unknown skill %q (use 'gcx agent skills list' to see available skills)", name)
		}
	}
	result, err := installFiles(source, root, selected, force, dryRun)
	result.Notices = notices
	return result, err
}

// installFiles only copies selected bundled files. Lifecycle targeting happens
// before this function, and it never prunes obsolete local files.
func installFiles(source fs.FS, root string, filter map[string]struct{}, force bool, dryRun bool) (InstallResult, error) {
	root = filepath.Clean(root)
	result := InstallResult{
		Root:      root,
		SkillsDir: filepath.Join(root, "skills"),
		DryRun:    dryRun,
		Force:     force,
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
		if _, ok := filter[skillName]; !ok {
			if d.IsDir() && len(parts) == 1 {
				return fs.SkipDir
			}
			return nil
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

// Update refreshes installed bundled skills and reports retired installations
// without changing them. Explicit targets are all validated before any writes.
func Update(source fs.FS, catalog []byte, root string, targets []string, dryRun bool) (InstallResult, error) {
	states, err := Reconcile(source, catalog, root)
	if err != nil {
		return InstallResult{}, err
	}
	byName := make(map[string]SkillState, len(states))
	for _, state := range states {
		byName[state.Name] = state
	}
	requested := make(map[string]struct{}, len(targets))
	for _, name := range targets {
		state, ok := byName[name]
		if !ok || state.Status == Unmanaged {
			return InstallResult{}, fmt.Errorf("unknown skill %q (use 'gcx agent skills list' to see available skills)", name)
		}
		if !state.Installed && (state.Status != Retired || !state.Present) {
			return InstallResult{}, fmt.Errorf("skill %q is not installed", name)
		}
		requested[name] = struct{}{}
	}
	filter := make(map[string]struct{})
	var notices []LifecycleNotice
	for _, state := range states {
		if len(targets) > 0 {
			if _, ok := requested[state.Name]; !ok {
				continue
			}
		}
		if state.Status == Unmanaged || (!state.Installed && (state.Status != Retired || !state.Present)) {
			continue
		}
		if state.Status == Deprecated || state.Status == Retired {
			notices = append(notices, LifecycleNotice{Name: state.Name, CatalogEntry: state.CatalogEntry})
		}
		if state.Status != Retired {
			filter[state.Name] = struct{}{}
		}
	}
	result, err := installFiles(source, root, filter, true, dryRun)
	result.Notices = notices
	return result, err
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
		// handle cases where existing skills files are read-only - WriteFile
		// doesn't override permissions on existing files.
		if err := os.Chmod(targetPath, installedFileMode); err != nil {
			return false, false, err
		}
		return true, true, os.WriteFile(targetPath, sourceData, installedFileMode)
	case errors.Is(err, os.ErrNotExist):
		if dryRun {
			return true, false, nil
		}
		return true, false, os.WriteFile(targetPath, sourceData, installedFileMode)
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

// installedFileMode is the install permission applied to bundled skill files.
// Skills are plain markdown, so a uniform 0o644 keeps installed copies
// user-writable regardless of the more restrictive 0o444 that embed.FS reports.
const installedFileMode fs.FileMode = 0o644

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
