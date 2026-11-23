// Package extensions installs, updates and runs gcx extensions.
//
// Each installed extension has a directory under the store's root holding a
// record of where it came from and, for git installs, its executable. Local
// installs point at the executable in their source directory, so rebuilding
// it needs no reinstall.
package extensions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/grafana/gcx/internal/xdg"
)

const recordFile = "extension.json"

// ErrNotInstalled is returned for an extension name with no installation.
var ErrNotInstalled = errors.New("extension is not installed")

// Extension is an installed extension.
type Extension struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
	// Source is the git URL or absolute local directory it was installed from.
	Source string `json:"source"`
	Local  bool   `json:"local,omitempty"`
	// Tag and Commit identify the installed release of a git install.
	Tag        string `json:"tag,omitempty"`
	Commit     string `json:"commit,omitempty"`
	Executable string `json:"executable"`
}

// Store manages the extensions installed in Dir.
type Store struct {
	Dir string
	// GCXVersion is checked against each manifest's minGCXVersion.
	GCXVersion string
	// HTTPClient downloads release artifacts.
	HTTPClient *http.Client
}

// DefaultDir is where gcx installs extensions.
func DefaultDir() string {
	return filepath.Join(xdg.DataHome(), "gcx", "extensions")
}

// List returns the installed extensions sorted by name.
func (s *Store) List() ([]Extension, error) {
	entries, err := os.ReadDir(s.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return []Extension{}, nil
	}
	if err != nil {
		return nil, err
	}
	exts := []Extension{}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		ext, err := s.Get(entry.Name())
		if errors.Is(err, ErrNotInstalled) {
			continue // not an extension, e.g. left by an interrupted uninstall
		}
		if err != nil {
			return nil, err
		}
		exts = append(exts, *ext)
	}
	sort.Slice(exts, func(i, j int) bool { return exts[i].Name < exts[j].Name })
	return exts, nil
}

// Get returns an installed extension.
func (s *Store) Get(name string) (*Extension, error) {
	if !namePattern.MatchString(name) {
		return nil, fmt.Errorf("%w: %s", ErrNotInstalled, name)
	}
	data, err := os.ReadFile(filepath.Join(s.Dir, name, recordFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s", ErrNotInstalled, name)
	}
	if err != nil {
		return nil, err
	}
	var ext Extension
	if err := json.Unmarshal(data, &ext); err != nil {
		return nil, fmt.Errorf("read extension %s: %w", name, err)
	}
	return &ext, nil
}

// Uninstall removes an installed extension. A local install's source
// directory is left untouched.
func (s *Store) Uninstall(name string) (*Extension, error) {
	ext, err := s.Get(name)
	if err != nil {
		return nil, err
	}
	return ext, os.RemoveAll(filepath.Join(s.Dir, name))
}

// Install installs an extension from a local directory or a git repository.
// A repository is installed at its latest release tag.
func (s *Store) Install(ctx context.Context, source string) (*Extension, error) {
	if info, err := os.Stat(source); err == nil && info.IsDir() {
		return s.installLocal(source)
	}
	if !isGitURL(source) {
		return nil, fmt.Errorf("%s is neither a local directory nor a git URL", source)
	}
	tag, err := latestTag(ctx, source)
	if err != nil {
		return nil, err
	}
	return s.installGit(ctx, source, tag, "")
}

// UpdateResult reports what Update did to one extension.
type UpdateResult struct {
	Name   string `json:"name"`
	From   string `json:"from"`
	To     string `json:"to"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// Update statuses.
const (
	StatusUpdated  = "updated"
	StatusUpToDate = "up-to-date"
	// StatusSkipped is reported for local installs, which update when rebuilt.
	StatusSkipped = "skipped"
	StatusFailed  = "failed"
)

// Update reinstalls the named extensions, or all of them, at the latest
// release tag of their repository when it is newer than the installed one.
// A failure to update one extension is reported in its result and does not
// stop the others.
func (s *Store) Update(ctx context.Context, names []string) ([]UpdateResult, error) {
	var exts []Extension
	if len(names) == 0 {
		all, err := s.List()
		if err != nil {
			return nil, err
		}
		exts = all
	}
	for _, name := range names {
		ext, err := s.Get(name)
		if err != nil {
			return nil, err
		}
		exts = append(exts, *ext)
	}

	results := []UpdateResult{}
	for _, ext := range exts {
		result := UpdateResult{Name: ext.Name, From: ext.Version, To: ext.Version, Status: StatusSkipped}
		if !ext.Local {
			result.Status = StatusUpToDate
			tag, err := newerTag(ctx, ext)
			var updated *Extension
			if err == nil && tag != "" {
				updated, err = s.installGit(ctx, ext.Source, tag, ext.Name)
			}
			switch {
			case err != nil:
				result.Status, result.Error = StatusFailed, err.Error()
			case updated != nil:
				result.To, result.Status = updated.Version, StatusUpdated
			}
		}
		results = append(results, result)
	}
	return results, nil
}

// newerTag returns the latest release tag of ext's repository, or "" when it
// is not newer than the installed one.
func newerTag(ctx context.Context, ext Extension) (string, error) {
	tag, err := latestTag(ctx, ext.Source)
	if err != nil {
		return "", err
	}
	installed, err := semver.NewVersion(ext.Tag)
	if err == nil && !semver.MustParse(tag).GreaterThan(installed) {
		return "", nil
	}
	return tag, nil
}

func (s *Store) installLocal(source string) (*Extension, error) {
	dir, err := filepath.Abs(source)
	if err != nil {
		return nil, err
	}
	m, err := s.loadManifest(dir)
	if err != nil {
		return nil, err
	}
	p, err := m.devPlatform()
	if err != nil {
		return nil, err
	}
	exe := filepath.Join(dir, filepath.FromSlash(p.Path))
	if info, err := os.Stat(exe); err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("extension executable %s not found; build it first", exe)
	}
	ext := &Extension{Name: m.Name, Version: m.Version, Description: m.Description, Source: dir, Local: true, Executable: exe}
	return ext, s.commit(ext, "", "")
}

// installGit installs the extension at tag. replacing names the installed
// extension being updated; it is empty for a new install.
func (s *Store) installGit(ctx context.Context, source, tag, replacing string) (*Extension, error) {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return nil, err
	}
	staging, err := os.MkdirTemp(s.Dir, ".install-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(staging)

	checkout := filepath.Join(staging, "checkout")
	commit, err := cloneTag(ctx, source, tag, checkout)
	if err != nil {
		return nil, err
	}
	m, err := s.loadManifest(checkout)
	if err != nil {
		return nil, err
	}
	if replacing != "" && m.Name != replacing {
		return nil, fmt.Errorf("%s at %s is now named %s; uninstall and install it again", replacing, tag, m.Name)
	}
	p, err := m.releasePlatform()
	if err != nil {
		return nil, err
	}

	pkg := filepath.Join(staging, "package")
	if err := os.Mkdir(pkg, 0o755); err != nil {
		return nil, err
	}
	exeName, err := s.fetch(ctx, m, p, filepath.Join(staging, "download"), pkg)
	if err != nil {
		return nil, err
	}

	ext := &Extension{
		Name: m.Name, Version: m.Version, Description: m.Description,
		Source: source, Tag: tag, Commit: commit,
		Executable: filepath.Join(s.Dir, m.Name, exeName),
	}
	return ext, s.commit(ext, pkg, replacing)
}

func (s *Store) loadManifest(dir string) (*Manifest, error) {
	m, err := loadManifest(dir)
	if err != nil {
		return nil, err
	}
	return m, m.checkGCXVersion(s.GCXVersion)
}

// commit writes the extension's record into pkg, or a new directory when pkg
// is empty, and moves it into place.
func (s *Store) commit(ext *Extension, pkg, replacing string) error {
	dst := filepath.Join(s.Dir, ext.Name)
	if replacing == "" {
		if _, err := os.Stat(dst); err == nil {
			return fmt.Errorf("extension %s is already installed; uninstall it first", ext.Name)
		}
	}
	if pkg == "" {
		if err := os.MkdirAll(s.Dir, 0o755); err != nil {
			return err
		}
		dir, err := os.MkdirTemp(s.Dir, ".install-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		pkg = dir
	}
	data, err := json.MarshalIndent(ext, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(pkg, recordFile), data, 0o600); err != nil {
		return err
	}
	// Keep the installed version until the new one is in place.
	old := filepath.Join(s.Dir, ".replaced-"+ext.Name)
	if err := os.RemoveAll(old); err != nil {
		return err
	}
	if err := os.Rename(dst, old); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(pkg, dst); err != nil {
		_ = os.Rename(old, dst)
		return err
	}
	return os.RemoveAll(old)
}
