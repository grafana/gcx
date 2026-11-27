package extensions

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/goccy/go-yaml"
)

// ManifestFile is the manifest gcx reads from the root of an extension's
// source directory or git repository.
const ManifestFile = "gcx-extension.yaml"

// reservedNames are the ext subcommands; an extension cannot shadow them.
//
//nolint:gochecknoglobals
var reservedNames = []string{"help", "install", "list", "uninstall", "update"}

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Manifest describes an extension and where to get its executable.
type Manifest struct {
	Name          string     `yaml:"name"`
	Version       string     `yaml:"version"`
	Description   string     `yaml:"description"`
	MinGCXVersion string     `yaml:"minGCXVersion,omitempty"`
	Platforms     []Platform `yaml:"platforms"`
}

// Platform is the executable for one OS and architecture.
//
// A release row has URL, SHA256 and, for archives, Bin. A development row
// has Path instead: an executable relative to the manifest, used only when
// installing from a local directory. A development row may use "*" for OS
// and Arch.
type Platform struct {
	OS     string `yaml:"os"`
	Arch   string `yaml:"arch"`
	URL    string `yaml:"url,omitempty"`
	SHA256 string `yaml:"sha256,omitempty"`
	Bin    string `yaml:"bin,omitempty"`
	Path   string `yaml:"path,omitempty"`
}

func loadManifest(dir string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, ManifestFile))
	if err != nil {
		return nil, fmt.Errorf("read extension manifest: %w", err)
	}
	var m Manifest
	if err := yaml.UnmarshalWithOptions(data, &m, yaml.Strict()); err != nil {
		return nil, fmt.Errorf("parse %s: %w", ManifestFile, err)
	}
	if err := m.validate(); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", ManifestFile, err)
	}
	return &m, nil
}

func (m *Manifest) validate() error {
	if !namePattern.MatchString(m.Name) {
		return fmt.Errorf("name %q must start with a lowercase letter and contain only lowercase letters, digits and hyphens", m.Name)
	}
	if slices.Contains(reservedNames, m.Name) {
		return fmt.Errorf("name %q is reserved by gcx ext", m.Name)
	}
	if _, err := semver.StrictNewVersion(strings.TrimPrefix(m.Version, "v")); err != nil {
		return fmt.Errorf("version %q is not a semantic version", m.Version)
	}
	if m.MinGCXVersion != "" {
		if _, err := semver.NewVersion(m.MinGCXVersion); err != nil {
			return fmt.Errorf("minGCXVersion %q is not a semantic version", m.MinGCXVersion)
		}
	}
	if len(m.Platforms) == 0 {
		return errors.New("at least one platform is required")
	}
	for i, p := range m.Platforms {
		switch {
		case p.OS == "" || p.Arch == "":
			return fmt.Errorf("platforms[%d]: os and arch are required", i)
		case (p.URL == "") == (p.Path == ""):
			return fmt.Errorf("platforms[%d]: exactly one of url and path is required", i)
		case p.URL != "" && !strings.HasPrefix(p.URL, "https://"):
			return fmt.Errorf("platforms[%d]: url must use https", i)
		case p.URL != "" && !sha256Pattern.MatchString(p.SHA256):
			return fmt.Errorf("platforms[%d]: sha256 must be a hex-encoded SHA-256 digest", i)
		// Windows treats a backslash as a separator, so it could escape the extension directory.
		case strings.Contains(p.Bin, `\`):
			return fmt.Errorf("platforms[%d]: bin must use / as its path separator", i)
		case strings.EqualFold(path.Base(p.Bin), recordFile):
			return fmt.Errorf("platforms[%d]: bin cannot be named %s", i, recordFile)
		case p.URL != "" && archiveFormat(p.URL) != "" && p.Bin == "":
			return fmt.Errorf("platforms[%d]: bin is required for archives", i)
		case p.URL != "" && (p.OS == "*" || p.Arch == "*"):
			return fmt.Errorf("platforms[%d]: release rows must name an os and arch", i)
		}
	}
	return nil
}

var sha256Pattern = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)

// checkGCXVersion fails when gcx is older than the manifest requires.
// Development builds have no comparable version and always pass.
func (m *Manifest) checkGCXVersion(gcxVersion string) error {
	if m.MinGCXVersion == "" {
		return nil
	}
	current, err := semver.NewVersion(gcxVersion)
	if err != nil {
		return nil //nolint:nilerr // development builds are not versioned
	}
	if current.LessThan(semver.MustParse(m.MinGCXVersion)) {
		return fmt.Errorf("extension %s requires gcx %s or later; this is gcx %s", m.Name, m.MinGCXVersion, gcxVersion)
	}
	return nil
}

// releasePlatform returns the release row for this machine.
func (m *Manifest) releasePlatform() (Platform, error) {
	for _, p := range m.Platforms {
		if p.URL != "" && p.OS == runtime.GOOS && p.Arch == runtime.GOARCH {
			return p, nil
		}
	}
	return Platform{}, fmt.Errorf("extension %s has no release for %s/%s", m.Name, runtime.GOOS, runtime.GOARCH)
}

// devPlatform returns the first development row that matches this machine.
func (m *Manifest) devPlatform() (Platform, error) {
	for _, p := range m.Platforms {
		if p.Path != "" && matches(p.OS, runtime.GOOS) && matches(p.Arch, runtime.GOARCH) {
			return p, nil
		}
	}
	return Platform{}, fmt.Errorf("extension %s has no local path for %s/%s", m.Name, runtime.GOOS, runtime.GOARCH)
}

func matches(want, got string) bool { return want == "*" || want == got }
