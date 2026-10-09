package skills

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// UninstallResult summarizes explicit removal, preserving the existing receipt.
type UninstallResult struct {
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

// Uninstall accepts current and retired catalog names, never unmanaged names.
// Approval for --all belongs to the CLI; this function only executes its plan.
func Uninstall(source fs.FS, catalog []byte, root string, names []string, all, dryRun bool) (UninstallResult, error) {
	states, err := Reconcile(source, catalog, root)
	if err != nil {
		return UninstallResult{}, err
	}
	known := make(map[string]SkillState, len(states))
	for _, state := range states {
		if state.Known {
			known[state.Name] = state
		}
	}
	selected := make(map[string]struct{})
	if all {
		for name, state := range known {
			if state.Status != Retired || state.Present {
				selected[name] = struct{}{}
			}
		}
	} else {
		for _, name := range names {
			if err := ValidateSkillName(name); err != nil {
				return UninstallResult{}, err
			}
			if _, ok := known[name]; !ok {
				return UninstallResult{}, fmt.Errorf("unknown skill %q (use 'gcx agent skills list' to see gcx skills)", name)
			}
			selected[name] = struct{}{}
		}
	}
	root = filepath.Clean(root)
	result := UninstallResult{
		Root: root, SkillsDir: filepath.Join(root, "skills"),
		Requested: sortedKeys(selected), Removed: []string{}, Missing: []string{}, DryRun: dryRun,
	}
	result.RequestedCount = len(result.Requested)
	// Check every destination before removing any of them.
	for _, name := range result.Requested {
		target := filepath.Join(result.SkillsDir, name)
		info, err := os.Lstat(target)
		if errors.Is(err, os.ErrNotExist) {
			result.Missing = append(result.Missing, name)
			continue
		}
		if err != nil {
			return UninstallResult{}, err
		}
		if !info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			return UninstallResult{}, fmt.Errorf("destination path exists and is not a directory: %s", target)
		}
		result.Removed = append(result.Removed, name)
	}
	if !dryRun {
		for _, name := range result.Removed {
			// RemoveAll unlinks symlinks without following them.
			if err := os.RemoveAll(filepath.Join(result.SkillsDir, name)); err != nil {
				return UninstallResult{}, err
			}
		}
	}
	result.RemovedCount = len(result.Removed)
	result.MissingCount = len(result.Missing)
	return result, nil
}
