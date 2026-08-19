// Package extensions implements the third-party extension mechanism described
// in ADR-023: manifest-driven install, a single index gcx owns, and dispatch to
// an extension binary that reaches Grafana by shelling back out to gcx.
package extensions

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/goccy/go-yaml"
)

// ManifestFilename is the file an extension source must contain.
const ManifestFilename = "gcx-extension.yaml"

// APIVersion and Kind are the envelope values gcx accepts, matching the
// apiVersion/kind/metadata shape gcx uses for every other manifest.
const (
	APIVersion = "extensions.gcx.grafana.com/v1alpha1"
	Kind       = "Extension"
)

// anyPlatform is the wildcard a local path row uses for os/arch.
const anyPlatform = "*"

// nameRE constrains extension names to what can safely be a directory name and
// an argv[1] token: lowercase alphanumerics and dashes.
var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}$`)

// Manifest is the parsed gcx-extension.yaml.
type Manifest struct {
	APIVersion string   `yaml:"apiVersion" json:"apiVersion"`
	Kind       string   `yaml:"kind"       json:"kind"`
	Metadata   Metadata `yaml:"metadata"   json:"metadata"`
	Spec       Spec     `yaml:"spec"       json:"spec"`
}

// Metadata carries the extension's identity.
type Metadata struct {
	Name string `yaml:"name" json:"name"`
}

// Spec is the extension's declarative body.
type Spec struct {
	Version       string `yaml:"version"                 json:"version"`
	Description   string `yaml:"description,omitempty"   json:"description,omitempty"`
	Homepage      string `yaml:"homepage,omitempty"      json:"homepage,omitempty"`
	MinGCXVersion string `yaml:"minGCXVersion,omitempty" json:"minGCXVersion,omitempty"`
	// Script names an interpreted entrypoint (relative to the manifest) for
	// extensions that ship no compiled binary. Mutually exclusive with Platforms.
	Script    string     `yaml:"script,omitempty"    json:"script,omitempty"`
	Platforms []Platform `yaml:"platforms,omitempty" json:"platforms,omitempty"`
	Telemetry *Telemetry `yaml:"telemetry,omitempty" json:"telemetry,omitempty"`
}

// Platform is one row of the OS/arch table: where to get the binary for this
// platform and what it is called once unpacked.
type Platform struct {
	OS   string `yaml:"os"   json:"os"`
	Arch string `yaml:"arch" json:"arch"`
	// URL points at a raw binary or a .tar.gz. Required for remote installs.
	URL string `yaml:"url,omitempty" json:"url,omitempty"`
	// SHA256 is mandatory whenever URL is set; install fails closed without it.
	SHA256 string `yaml:"sha256,omitempty" json:"sha256,omitempty"`
	// Path is a binary relative to the manifest, for local (development)
	// installs. Mutually exclusive with URL. OS and Arch may both be "*" on a
	// path row: a local build is for the machine doing the install, so there is
	// nothing to select between.
	Path string `yaml:"path,omitempty" json:"path,omitempty"`
	// Bin is the executable's name inside a .tar.gz archive.
	Bin string `yaml:"bin,omitempty" json:"bin,omitempty"`
}

// Telemetry lets an author opt their extension's name out of gcx's usage
// telemetry.
type Telemetry struct {
	ReportUsage *bool `yaml:"reportUsage,omitempty" json:"reportUsage,omitempty"`
}

// ReportUsage reports whether the extension's name may be recorded. Absent
// telemetry config means yes, matching the ADR's default.
func (s Spec) ReportUsage() bool {
	if s.Telemetry == nil || s.Telemetry.ReportUsage == nil {
		return true
	}
	return *s.Telemetry.ReportUsage
}

// ParseManifest decodes and validates a manifest from bytes.
func ParseManifest(data []byte) (*Manifest, error) {
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", ManifestFilename, err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// LoadManifest reads a manifest from a file path.
func LoadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseManifest(data)
}

// Validate checks the envelope and the parts of the spec that must hold on
// every platform, independent of which row will be selected.
func (m *Manifest) Validate() error {
	if m.APIVersion != APIVersion {
		return fmt.Errorf("unsupported apiVersion %q (want %q)", m.APIVersion, APIVersion)
	}
	if m.Kind != Kind {
		return fmt.Errorf("unsupported kind %q (want %q)", m.Kind, Kind)
	}
	if !nameRE.MatchString(m.Metadata.Name) {
		return fmt.Errorf("invalid metadata.name %q: use lowercase letters, digits and dashes (2-63 chars)", m.Metadata.Name)
	}
	if strings.TrimSpace(m.Spec.Version) == "" {
		return errors.New("spec.version is required")
	}
	if m.Spec.Script == "" && len(m.Spec.Platforms) == 0 {
		return errors.New("spec must set either script or platforms")
	}
	if m.Spec.Script != "" && len(m.Spec.Platforms) > 0 {
		return errors.New("spec.script and spec.platforms are mutually exclusive")
	}
	for i, p := range m.Spec.Platforms {
		if p.OS == "" || p.Arch == "" {
			return fmt.Errorf("spec.platforms[%d]: os and arch are required", i)
		}
		if (p.OS == anyPlatform || p.Arch == anyPlatform) && p.Path == "" {
			return fmt.Errorf("spec.platforms[%d]: os/arch %q is only valid on a path row (a local build)", i, anyPlatform)
		}
		switch {
		case p.URL == "" && p.Path == "":
			return fmt.Errorf("spec.platforms[%d] (%s/%s): set url or path", i, p.OS, p.Arch)
		case p.URL != "" && p.Path != "":
			return fmt.Errorf("spec.platforms[%d] (%s/%s): url and path are mutually exclusive", i, p.OS, p.Arch)
		case p.URL != "" && p.SHA256 == "":
			return fmt.Errorf("spec.platforms[%d] (%s/%s): sha256 is required alongside url", i, p.OS, p.Arch)
		}
	}
	return nil
}

// SelectPlatform returns the row matching the running OS/arch. Only call it for
// a manifest with a platform table: a script extension has no rows to select
// between (check HasPlatforms first).
func (m *Manifest) SelectPlatform() (*Platform, error) {
	if m.Spec.Script != "" {
		return nil, errors.New("this extension ships a script entrypoint, not a platform table")
	}
	for i := range m.Spec.Platforms {
		p := m.Spec.Platforms[i]
		if matches(p.OS, runtime.GOOS) && matches(p.Arch, runtime.GOARCH) {
			return &p, nil
		}
	}
	return nil, fmt.Errorf("extension %q publishes no build for %s/%s", m.Metadata.Name, runtime.GOOS, runtime.GOARCH)
}

// HasPlatforms reports whether the extension ships compiled binaries rather
// than a script entrypoint.
func (m *Manifest) HasPlatforms() bool { return m.Spec.Script == "" }

func matches(declared, actual string) bool {
	return declared == anyPlatform || declared == actual
}

// entrypointName is the file name the installed entrypoint is stored under.
func entrypointName(m *Manifest, p *Platform) string {
	if p == nil {
		return filepath.Base(m.Spec.Script)
	}
	if p.Bin != "" {
		return filepath.Base(p.Bin)
	}
	return m.Metadata.Name
}
