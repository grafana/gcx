//go:build !wasip1

// Package ext implements local git-manifest installation for the extension PoC.
package ext

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/goccy/go-yaml"
	"github.com/gofrs/flock"
)

const Disclosure = "Third-party extensions run with your permissions and credentials. Grafana and gcx maintainers do not review or support their contents and are not responsible for consequences of running them. Use at your own risk."

type Manifest struct {
	SchemaVersion int        `yaml:"schemaVersion" json:"schemaVersion"`
	Name          string     `yaml:"name" json:"name"`
	Description   string     `yaml:"description" json:"description"`
	Version       string     `yaml:"version" json:"version"`
	MinGCXVersion string     `yaml:"minGCXVersion" json:"minGCXVersion"`
	MaxGCXVersion string     `yaml:"maxGCXVersion,omitempty" json:"maxGCXVersion,omitempty"`
	Platforms     []Platform `yaml:"platforms" json:"-"`
}

type Platform struct {
	Selector struct {
		OS   string `yaml:"os"`
		Arch string `yaml:"arch"`
	} `yaml:"selector"`
	Path string `yaml:"path"`
}

type Installed struct {
	Manifest

	Source     string `json:"source"`
	Commit     string `json:"commit"`
	Entrypoint string `json:"entrypoint"`
}

type Plan struct {
	Installed

	Binary []byte
}
type Store struct{ Dir string }

func ValidName(name string) bool {
	return regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}$`).MatchString(name) && !slices.Contains([]string{"install", "list", "uninstall", "update", "help", "search", "scaffold"}, name)
}

func CheckVersion(m Manifest, version string) error {
	minimum, err := semver.StrictNewVersion(m.MinGCXVersion)
	if err != nil {
		return fmt.Errorf("invalid minGCXVersion: %w", err)
	}
	var maximum *semver.Version
	if m.MaxGCXVersion != "" {
		maximum, err = semver.StrictNewVersion(m.MaxGCXVersion)
		if err != nil {
			return fmt.Errorf("invalid maxGCXVersion: %w", err)
		}
		if maximum.LessThan(minimum) {
			return errors.New("maxGCXVersion is below minGCXVersion")
		}
	}
	if version == "" || version == "dev" || version == "SNAPSHOT" || version == "(devel)" {
		return nil
	} // Development binaries have no release compatibility bound to check.
	current, err := semver.NewVersion(version)
	if err != nil {
		return err
	}
	if current.LessThan(minimum) || (maximum != nil && current.GreaterThan(maximum)) {
		return fmt.Errorf("extension %s is incompatible with gcx %s", m.Name, version)
	}
	return nil
}

func Prepare(ctx context.Context, source, version string) (*Plan, error) {
	abs, err := filepath.Abs(source)
	if err != nil {
		return nil, err
	}
	if info, statErr := os.Stat(abs); statErr != nil || !info.IsDir() {
		return nil, errors.New("this PoC installs from local git checkouts only")
	}
	git := func(args ...string) ([]byte, error) {
		//nolint:gosec // Git arguments are constructed here and executed without a shell.
		out, runErr := exec.CommandContext(ctx, "git", append([]string{"-C", abs}, args...)...).Output()
		if runErr != nil {
			return nil, fmt.Errorf("reading local git source: %w", runErr)
		}
		return out, nil
	}
	prefix, err := git("rev-parse", "--show-prefix")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(prefix)) != "" {
		return nil, errors.New("source must be the root of a local git checkout")
	}
	commit, err := git("rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	revision := strings.TrimSpace(string(commit))
	data, err := git("show", revision+":gcx-extension.yaml")
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := yaml.UnmarshalWithOptions(data, &m, yaml.Strict()); err != nil {
		return nil, fmt.Errorf("invalid manifest: %w", err)
	}
	if m.SchemaVersion != 1 || !ValidName(m.Name) {
		return nil, errors.New("manifest requires schemaVersion: 1 and a valid, unreserved extension name")
	}
	if _, err := semver.StrictNewVersion(m.Version); err != nil {
		return nil, fmt.Errorf("invalid extension version: %w", err)
	}
	if err := CheckVersion(m, version); err != nil {
		return nil, err
	}
	var path string
	var supported []string
	for _, p := range m.Platforms {
		if p.Path == "" || !filepath.IsLocal(p.Path) || p.Selector.OS == "" || p.Selector.Arch == "" {
			return nil, errors.New("each platform requires a relative local path and os/arch selector")
		}
		supported = append(supported, p.Selector.OS+"/"+p.Selector.Arch)
		if (p.Selector.OS == "*" || p.Selector.OS == runtime.GOOS) && (p.Selector.Arch == "*" || p.Selector.Arch == runtime.GOARCH) {
			if path != "" {
				return nil, errors.New("multiple platform rows match this host")
			}
			path = p.Path
		}
	}
	if path == "" {
		return nil, fmt.Errorf("unsupported platform %s/%s; declared: %s", runtime.GOOS, runtime.GOARCH, strings.Join(supported, ", "))
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("entrypoint must be a regular file")
	}
	binary, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	return &Plan{Installed: Installed{Manifest: m, Source: abs, Commit: revision}, Binary: binary}, nil
}

func DefaultStore() (*Store, error) {
	dir := os.Getenv("GCX_EXTENSIONS_DIR")
	if dir == "" {
		config, err := os.UserConfigDir()
		if err != nil {
			return nil, err
		}
		dir = filepath.Join(config, "gcx", "extensions")
	}
	abs, err := filepath.Abs(dir)
	return &Store{Dir: abs}, err
}

func (s *Store) Lock(shared bool) (*flock.Flock, error) {
	if err := os.MkdirAll(s.Dir, 0700); err != nil {
		return nil, err
	}
	lock := flock.New(filepath.Join(s.Dir, ".lock"))
	var ok bool
	var err error
	if shared {
		ok, err = lock.TryRLock()
	} else {
		ok, err = lock.TryLock()
	}
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("extension store is busy: quit running extensions and retry")
	}
	return lock, nil
}

func (s *Store) List() ([]Installed, error) {
	data, err := os.ReadFile(filepath.Join(s.Dir, "index.json"))
	if errors.Is(err, os.ErrNotExist) {
		return []Installed{}, nil
	}
	if err != nil {
		return nil, err
	}
	var items []Installed
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Store) Lookup(name string) (*Installed, error) {
	if !ValidName(name) {
		return nil, fmt.Errorf("invalid extension name %q", name)
	}
	items, err := s.List()
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.Name == name {
			return &item, nil
		}
	}
	return nil, fmt.Errorf("extension %q is not installed; use gcx ext install <local-checkout>", name)
}

func (s *Store) Install(plan *Plan) (*Installed, error) {
	lock, err := s.Lock(false)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	items, err := s.List()
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(s.Dir, plan.Name+"-")
	if err != nil {
		return nil, err
	}
	installed := plan.Installed
	installed.Entrypoint = filepath.Join(dir, plan.Name)
	if runtime.GOOS == "windows" {
		installed.Entrypoint += ".exe"
	}
	//nolint:gosec // The installed entrypoint must be executable by its owner.
	if err := os.WriteFile(installed.Entrypoint, plan.Binary, 0700); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	items = slices.DeleteFunc(items, func(item Installed) bool { return item.Name == installed.Name })
	items = append(items, installed)
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return nil, err
	}
	tmp := filepath.Join(s.Dir, "index.json.tmp")
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, filepath.Join(s.Dir, "index.json")); err != nil {
		return nil, err
	}
	return &installed, nil
}
