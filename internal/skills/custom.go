package skills

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

const customDirectory = "custom-skills"
const customMarker = ".gcx-managed"
const customMarkerContent = "gcx-custom-skill-v1\n"

var customNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// CustomMutation reports an explicitly targeted local skill mutation.
type CustomMutation struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Changed bool   `json:"changed"`
	DryRun  bool   `json:"dry_run"`
}

type customSnapshot struct {
	name        string
	description string
	tags        []string
	files       map[string][]byte
}

// IsCustomID recognizes the explicit namespace; unqualified names stay bundled.
func IsCustomID(name string) bool { return strings.HasPrefix(name, "custom/") }

// IsLocalSkillPath requires an explicit relative or absolute directory path.
func IsLocalSkillPath(name string) bool {
	return filepath.IsAbs(name) || strings.HasPrefix(name, "./") || strings.HasPrefix(name, "../") ||
		strings.HasPrefix(name, `.\`) || strings.HasPrefix(name, `..\`)
}

func customName(id string) (string, error) {
	name, ok := strings.CutPrefix(id, "custom/")
	if !ok || len(name) > 64 || !customNamePattern.MatchString(name) {
		return "", fmt.Errorf("invalid custom skill identity %q: use custom/<lowercase-kebab-name> (maximum 64 characters)", id)
	}
	return name, nil
}

// readCustom snapshots a bounded directory without following symlinks. It never
// executes scripts or resolves remote resources named in the skill's content.
func readCustom(directory string) (customSnapshot, error) {
	info, err := os.Lstat(directory)
	if err != nil {
		return customSnapshot{}, err
	}
	if !info.IsDir() {
		return customSnapshot{}, fmt.Errorf("skill source must be a directory, not a file or symlink: %s", directory)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return customSnapshot{}, err
	}
	defer root.Close()
	result := customSnapshot{files: map[string][]byte{}, tags: []string{}}
	total := int64(0)
	err = fs.WalkDir(root.FS(), ".", func(p string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return errors.New("skill source must not include a .git directory")
			}
			return nil
		}
		if p == customMarker {
			return nil
		}
		stat, statErr := entry.Info()
		if statErr != nil {
			return statErr
		}
		if !stat.Mode().IsRegular() {
			return fmt.Errorf("skill file must be regular (symlinks are not supported): %s", p)
		}
		if stat.Size() > 2<<20 || len(result.files) >= 256 {
			return errors.New("skill exceeds local import limits: 256 files, 2 MiB per file, 16 MiB total")
		}
		file, openErr := root.Open(p)
		if openErr != nil {
			return openErr
		}
		data, readErr := io.ReadAll(io.LimitReader(file, (2<<20)+1))
		if err := errors.Join(readErr, file.Close()); err != nil {
			return err
		}
		total += int64(len(data))
		if total > 16<<20 {
			return errors.New("skill files exceed 16 MiB total")
		}
		if len(data) > 2<<20 {
			return fmt.Errorf("skill file exceeds 2 MiB: %s", p)
		}
		result.files[p] = data
		return nil
	})
	if err != nil {
		return customSnapshot{}, err
	}
	content := strings.ReplaceAll(string(result.files["SKILL.md"]), "\r\n", "\n")
	front, body, found := strings.Cut(strings.TrimPrefix(content, "---\n"), "\n---\n")
	if !strings.HasPrefix(content, "---\n") || !found || strings.TrimSpace(body) == "" {
		return customSnapshot{}, errors.New("SKILL.md must contain YAML front matter followed by workflow instructions")
	}
	var metadata struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
		Metadata    struct {
			Tags string `yaml:"tags"`
		} `yaml:"metadata"`
	}
	if err := yaml.Unmarshal([]byte(front), &metadata); err != nil {
		return customSnapshot{}, fmt.Errorf("invalid SKILL.md front matter: %w", err)
	}
	if _, err := customName("custom/" + metadata.Name); err != nil {
		return customSnapshot{}, err
	}
	if strings.TrimSpace(metadata.Description) == "" {
		return customSnapshot{}, errors.New("SKILL.md requires a nonempty description explaining when to use the workflow")
	}
	result.name = metadata.Name
	result.description = normalizeDescription(metadata.Description)
	for tag := range strings.SplitSeq(metadata.Metadata.Tags, ",") {
		if tag = strings.ToLower(strings.TrimSpace(tag)); tag != "" && !slices.Contains(result.tags, tag) {
			result.tags = append(result.tags, tag)
		}
	}
	return result, nil
}

func customStore(root string, create bool) (*os.Root, error) {
	store := filepath.Join(root, customDirectory)
	info, err := os.Lstat(store)
	if errors.Is(err, os.ErrNotExist) && create {
		if err := os.MkdirAll(root, 0o755); err != nil {
			return nil, err
		}
		if err := os.Mkdir(store, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		info, err = os.Lstat(store)
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("custom skills store must be a directory, not a symlink")
	}
	return os.OpenRoot(store)
}

func requireManaged(store *os.Root, name string) error {
	info, err := store.Lstat(name)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("custom skill destination must be a directory, not a symlink")
	}
	marker, err := store.Lstat(path.Join(name, customMarker))
	if err != nil || !marker.Mode().IsRegular() {
		return fmt.Errorf("custom skill %q is not managed by gcx", name)
	}
	data, err := store.ReadFile(path.Join(name, customMarker))
	if err != nil || string(data) != customMarkerContent {
		return fmt.Errorf("custom skill %q has an invalid ownership marker", name)
	}
	return nil
}

func acquireCustomLock(store *os.Root, name string) (func(), error) {
	lockName := ".lock-" + name
	lock, err := store.OpenFile(lockName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("custom skill is already being modified, or its lock is unavailable: %w", err)
	}
	if err := lock.Close(); err != nil {
		_ = store.Remove(lockName)
		return nil, err
	}
	return func() { _ = store.Remove(lockName) }, nil
}

// InstallCustom imports a local snapshot. Updating requires an explicit matching
// ID. Staging and rename prevent a failed copy from leaving a partial workflow.
func InstallCustom(root, directory, id string, dryRun bool) (CustomMutation, error) {
	snapshot, err := readCustom(directory)
	if err != nil {
		return CustomMutation{}, err
	}
	updating := id != ""
	if updating {
		name, err := customName(id)
		if err != nil {
			return CustomMutation{}, err
		}
		if name != snapshot.name {
			return CustomMutation{}, errors.New("source skill name must match the explicitly targeted custom skill")
		}
	}
	id = "custom/" + snapshot.name
	result := CustomMutation{ID: id, Path: filepath.Join(root, customDirectory, snapshot.name), Changed: true, DryRun: dryRun}
	store, err := customStore(root, !dryRun && !updating)
	if errors.Is(err, os.ErrNotExist) && dryRun && !updating {
		return result, nil
	}
	if err != nil {
		return CustomMutation{}, err
	}
	defer store.Close()
	if !dryRun {
		unlock, lockErr := acquireCustomLock(store, snapshot.name)
		if lockErr != nil {
			return CustomMutation{}, lockErr
		}
		defer unlock()
	}
	_, existsErr := store.Lstat(snapshot.name)
	if updating {
		if err := requireManaged(store, snapshot.name); err != nil {
			return CustomMutation{}, err
		}
		previous, err := readCustom(result.Path)
		if err != nil {
			return CustomMutation{}, err
		}
		if maps.EqualFunc(previous.files, snapshot.files, bytes.Equal) {
			result.Changed = false
			return result, nil
		}
	} else if !errors.Is(existsErr, os.ErrNotExist) {
		return CustomMutation{}, fmt.Errorf("custom skill %q already exists or cannot be inspected: use update with --from", snapshot.name)
	}
	if dryRun {
		return result, nil
	}
	stage, err := os.MkdirTemp(filepath.Join(root, customDirectory), ".import-")
	if err != nil {
		return CustomMutation{}, err
	}
	stageName := filepath.Base(stage)
	defer func() {
		// Best-effort cleanup of only the staging directory created here.
		_ = store.RemoveAll(stageName)
	}()
	for p, data := range snapshot.files {
		if err := store.MkdirAll(path.Join(stageName, path.Dir(p)), 0o755); err != nil {
			return CustomMutation{}, err
		}
		if err := store.WriteFile(path.Join(stageName, p), data, 0o644); err != nil {
			return CustomMutation{}, err
		}
	}
	if err := store.WriteFile(path.Join(stageName, customMarker), []byte(customMarkerContent), 0o644); err != nil {
		return CustomMutation{}, err
	}
	if updating {
		backup := stageName + "-previous"
		if err := store.Rename(snapshot.name, backup); err != nil {
			return CustomMutation{}, err
		}
		if err := store.Rename(stageName, snapshot.name); err != nil {
			return CustomMutation{}, errors.Join(err, store.Rename(backup, snapshot.name))
		}
		if err := store.RemoveAll(backup); err != nil {
			return CustomMutation{}, err
		}
	} else if err := store.Rename(stageName, snapshot.name); err != nil {
		return CustomMutation{}, err
	}
	return result, nil
}

// GetCustom reads only the explicitly selected local skill; it cannot shadow a
// bundled name and does not execute any workflow instructions.
func GetCustom(root, id, reference string) (GetResult, error) {
	name, err := customName(id)
	if err != nil {
		return GetResult{}, err
	}
	store, err := customStore(root, false)
	if err != nil {
		return GetResult{}, err
	}
	defer store.Close()
	if err := requireManaged(store, name); err != nil {
		return GetResult{}, err
	}
	snapshot, err := readCustom(filepath.Join(root, customDirectory, name))
	if err != nil {
		return GetResult{}, err
	}
	if snapshot.name != name {
		return GetResult{}, errors.New("stored skill name does not match its custom identity")
	}
	p := "SKILL.md"
	if reference != "" {
		if !fs.ValidPath(reference) || strings.Contains(reference, `\`) {
			return GetResult{}, errors.New("reference must be a relative file path without parent traversal")
		}
		p = reference
	}
	data, ok := snapshot.files[p]
	if !ok {
		return GetResult{}, fmt.Errorf("reference %q not found in %s", p, id)
	}
	refs := []string{}
	for p := range snapshot.files {
		if strings.HasPrefix(p, "references/") {
			refs = append(refs, p)
		}
	}
	slices.Sort(refs)
	return GetResult{Name: id, Source: "custom", Description: snapshot.description, Path: p, Body: string(data), References: refs}, nil
}

// UninstallCustom removes only a positively identified gcx-managed directory.
func UninstallCustom(root, id string, dryRun bool) (CustomMutation, error) {
	name, err := customName(id)
	if err != nil {
		return CustomMutation{}, err
	}
	store, err := customStore(root, false)
	if err != nil {
		return CustomMutation{}, err
	}
	defer store.Close()
	if !dryRun {
		unlock, err := acquireCustomLock(store, name)
		if err != nil {
			return CustomMutation{}, err
		}
		defer unlock()
	}
	if err := requireManaged(store, name); err != nil {
		return CustomMutation{}, err
	}
	if !dryRun {
		if err := store.RemoveAll(name); err != nil {
			return CustomMutation{}, err
		}
	}
	return CustomMutation{ID: id, Path: filepath.Join(root, customDirectory, name), Changed: true, DryRun: dryRun}, nil
}
