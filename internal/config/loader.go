package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/adrg/xdg"
	"github.com/goccy/go-yaml"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/grafana-app-sdk/logging"
)

const (
	configFilePermissions  = 0o600
	StandardConfigFolder   = "gcx"
	StandardConfigFileName = "config.yaml"
	ConfigFileEnvVar       = "GCX_CONFIG"
	LocalConfigFileName    = ".gcx.yaml"

	defaultEmptyConfigFile = `
contexts:
  default: {}
current-context: default
`
)

// DefaultEmptyConfigFile is the default content for a newly created config file.
const DefaultEmptyConfigFile = defaultEmptyConfigFile

// ConfigSource describes a discovered config file and its layer type.
type ConfigSource struct {
	Path    string    `json:"path"`
	Type    string    `json:"type"` // "system", "user", "local", "explicit"
	ModTime time.Time `json:"modified"`
}

// Priority returns the priority of this source (lower number = higher priority).
func (s ConfigSource) Priority() int {
	switch s.Type {
	case "explicit":
		return 0
	case "local":
		return 1
	case "user":
		return 2
	case "system":
		return 3
	default:
		return 4
	}
}

// DiscoverOption configures source discovery (primarily for testing).
type DiscoverOption func(*discoverOpts)

type discoverOpts struct {
	systemDir string
	userDir   string
	workDir   string
}

// WithSystemDir overrides the system config directory for discovery.
func WithSystemDir(dir string) DiscoverOption { return func(o *discoverOpts) { o.systemDir = dir } }

// WithUserDir overrides the user config directory for discovery.
func WithUserDir(dir string) DiscoverOption { return func(o *discoverOpts) { o.userDir = dir } }

// WithWorkDir overrides the working directory for local config discovery.
func WithWorkDir(dir string) DiscoverOption { return func(o *discoverOpts) { o.workDir = dir } }

// DiscoverSources finds all config files that exist across the layering hierarchy.
// Returns sources in priority order: system (lowest) → user → local (highest).
//
// For user config, $HOME/.config/gcx/ is checked before the platform XDG
// directory (which differs on macOS: ~/Library/Application Support). The first
// found wins. Use [CheckDuplicateUserConfig] to detect when both locations
// contain a config file.
func DiscoverSources(opts ...DiscoverOption) ([]ConfigSource, error) {
	o := discoverOpts{}
	for _, opt := range opts {
		opt(&o)
	}

	var sources []ConfigSource

	// --- System ---
	sysDir := o.systemDir
	if sysDir == "" {
		sysDir = xdgSystemConfigDir()
	}
	if sysDir != "" {
		if src, ok, err := probeConfigSource(userConfigFile(sysDir), "system"); err != nil {
			return nil, err
		} else if ok {
			sources = append(sources, src)
		}
	}

	// --- User ---
	// When overridden via WithUserDir (tests), check only that directory.
	// Otherwise check $HOME/.config first, then XDG_CONFIG_HOME. First found wins.
	if userSrc, ok, err := discoverUserSource(o.userDir); err != nil {
		return nil, err
	} else if ok {
		sources = append(sources, userSrc)
	}

	// --- Local ---
	workDir := o.workDir
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	if workDir != "" {
		if src, ok, err := probeConfigSource(filepath.Join(workDir, LocalConfigFileName), "local"); err != nil {
			return nil, err
		} else if ok {
			sources = append(sources, src)
		}
	}

	return sources, nil
}

// discoverUserSource finds the user config source, checking either the
// override dir or the standard search path ($HOME/.config then XDG).
// Returns (source, true) when found, (empty, false) when no config exists.
func discoverUserSource(overrideDir string) (ConfigSource, bool, error) {
	dirs := userConfigDirs()
	if overrideDir != "" {
		dirs = []string{overrideDir}
	}
	for _, dir := range dirs {
		src, ok, err := probeConfigSource(userConfigFile(dir), "user")
		if err != nil {
			return ConfigSource{}, false, err
		}
		if ok {
			return src, true, nil
		}
	}
	return ConfigSource{}, false, nil
}

// probeConfigSource checks whether a config file exists at path and returns
// a ConfigSource if it does.
func probeConfigSource(path, typ string) (ConfigSource, bool, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return ConfigSource{}, false, nil
	}
	if err != nil {
		return ConfigSource{}, false, err
	}
	return ConfigSource{Path: path, Type: typ, ModTime: info.ModTime()}, true, nil
}

// userConfigFile returns the full config file path for a given config root directory.
func userConfigFile(dir string) string {
	return filepath.Join(dir, StandardConfigFolder, StandardConfigFileName)
}

// findExistingUserConfigFile returns the path of the first existing user config
// file across candidate directories (dotconfig first, then platform XDG).
// Returns empty string if none found.
func findExistingUserConfigFile() string {
	for _, dir := range userConfigDirs() {
		path := userConfigFile(dir)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// DuplicateUserConfig describes a situation where config files exist in both
// $HOME/.config/gcx/ and the platform-specific XDG config directory.
type DuplicateUserConfig struct {
	Active  string // the file being used ($HOME/.config/gcx/config.yaml)
	Ignored string // the file being ignored (platform XDG path)
}

// CheckDuplicateUserConfig reports whether config files exist in both
// $HOME/.config/gcx/ and the platform XDG directory. Returns nil when there is
// no ambiguity (same directory, one missing, etc.).
func CheckDuplicateUserConfig() *DuplicateUserConfig {
	dirs := userConfigDirs()
	if len(dirs) < 2 {
		return nil
	}
	active := userConfigFile(dirs[0])
	ignored := userConfigFile(dirs[1])
	if _, err := os.Stat(active); err != nil {
		return nil
	}
	if _, err := os.Stat(ignored); err != nil {
		return nil
	}
	return &DuplicateUserConfig{Active: active, Ignored: ignored}
}

// userConfigDirs returns candidate directories for user config in priority
// order. $HOME/.config is always checked first (cross-platform convention),
// followed by the platform XDG_CONFIG_HOME (which differs on macOS).
// Duplicates are removed.
func userConfigDirs() []string {
	dotConfig := dotConfigDir()
	xdgConfig := xdgUserConfigDir()

	switch {
	case dotConfig == "" && xdgConfig == "":
		return nil
	case dotConfig == "":
		return []string{xdgConfig}
	case xdgConfig == "" || dotConfig == xdgConfig:
		return []string{dotConfig}
	default:
		return []string{dotConfig, xdgConfig}
	}
}

// dotConfigDir returns $HOME/.config as a cross-platform config directory.
// Returns empty string if $HOME cannot be determined.
func dotConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config")
}

// xdgSystemConfigDir returns the first XDG system config directory.
func xdgSystemConfigDir() string {
	if len(xdg.ConfigDirs) > 0 {
		return xdg.ConfigDirs[0]
	}
	return ""
}

// xdgUserConfigDir returns the XDG user config directory.
func xdgUserConfigDir() string {
	return xdg.ConfigHome
}

type Override func(cfg *Config) error

type Source func() (string, error)

func ExplicitConfigFile(path string) Source {
	return func() (string, error) {
		return path, nil
	}
}

func StandardLocation() Source {
	return func() (string, error) {
		if envPath := os.Getenv(ConfigFileEnvVar); envPath != "" {
			return envPath, nil
		}

		// Return the first existing config ($HOME/.config wins over platform XDG).
		if existing := findExistingUserConfigFile(); existing != "" {
			return existing, nil
		}

		// No existing config — create in $HOME/.config if available,
		// otherwise fall back to the platform XDG directory.
		return createDefaultConfig()
	}
}

// createDefaultConfig creates a new empty config file in the preferred location
// ($HOME/.config, falling back to platform XDG) and returns its path.
func createDefaultConfig() (string, error) {
	if dir := dotConfigDir(); dir != "" {
		file := userConfigFile(dir)
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(file, []byte(defaultEmptyConfigFile), configFilePermissions); err != nil {
			return "", err
		}
		return file, nil
	}

	// Last resort: platform XDG (xdg.ConfigFile creates parent dirs).
	configSubpath := filepath.Join(StandardConfigFolder, StandardConfigFileName)
	file, err := xdg.ConfigFile(configSubpath)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(file); os.IsNotExist(err) {
		if err := os.WriteFile(file, []byte(defaultEmptyConfigFile), configFilePermissions); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	return file, nil
}

func Load(ctx context.Context, source Source, overrides ...Override) (Config, error) {
	config := Config{}

	filename, err := source()
	if err != nil {
		return config, err
	}

	logging.FromContext(ctx).Debug("Loading config", slog.String("filename", filename))
	config.Source = filename

	contents, err := os.ReadFile(filename)
	if err != nil {
		return config, err
	}

	codec := &format.YAMLCodec{BytesAsBase64: true}
	if err := codec.Decode(bytes.NewBuffer(contents), &config); err != nil {
		return config, UnmarshalError{File: filename, Err: err}
	}

	for name, ctx := range config.Contexts {
		ctx.Name = name
	}

	for _, override := range overrides {
		if err := override(&config); err != nil {
			return config, annotateErrorWithSource(filename, contents, err)
		}
	}

	return config, nil
}

func Write(ctx context.Context, source Source, cfg Config) error {
	filename, err := source()
	if err != nil {
		return err
	}

	logging.FromContext(ctx).Debug("Writing config", slog.String("filename", filename))

	file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, configFilePermissions)
	if err != nil {
		return err
	}
	defer file.Close()

	codec := &format.YAMLCodec{BytesAsBase64: true}
	return codec.Encode(file, cfg)
}

// LoadLayered discovers config files, loads and deep-merges them, then applies overrides.
// If no config files are found, creates a default user config (preserving current behavior).
// If explicitFile is set (--config flag) or GCX_CONFIG env var is set,
// bypasses layering entirely and loads that single file.
func LoadLayered(ctx context.Context, explicitFile string, overrides ...Override) (Config, error) {
	// --config flag bypasses layering.
	if explicitFile != "" {
		return loadExplicit(ctx, explicitFile, overrides...)
	}

	// GCX_CONFIG env var also bypasses layering (preserving existing behavior).
	if envPath := os.Getenv(ConfigFileEnvVar); envPath != "" {
		return loadExplicit(ctx, envPath, overrides...)
	}

	// Warn when configs exist in both $HOME/.config and the platform XDG dir.
	if dup := CheckDuplicateUserConfig(); dup != nil && !agent.IsAgentMode() {
		fmt.Fprintf(os.Stderr, "Warning: config found in both %s and %s; using %s\n",
			dup.Active, dup.Ignored, dup.Active)
	}

	sources, err := DiscoverSources()
	if err != nil {
		return Config{}, err
	}

	// No config files — auto-create user config (current behavior).
	if len(sources) == 0 {
		cfg, err := Load(ctx, StandardLocation(), overrides...)
		if err != nil {
			return cfg, err
		}
		newSources, _ := DiscoverSources()
		cfg.Sources = newSources
		return cfg, nil
	}

	// Load and merge in priority order (system → user → local).
	var merged Config
	for i, src := range sources {
		loaded, err := Load(ctx, ExplicitConfigFile(src.Path))
		if err != nil {
			return Config{}, err
		}
		if i == 0 {
			merged = loaded
		} else {
			merged = MergeConfigs(merged, loaded)
		}
	}

	merged.Sources = sources

	// Apply overrides on the merged config.
	for _, override := range overrides {
		if err := override(&merged); err != nil {
			return merged, err
		}
	}

	return merged, nil
}

// loadExplicit loads a single explicit config file, bypassing layered discovery.
func loadExplicit(ctx context.Context, path string, overrides ...Override) (Config, error) {
	cfg, err := Load(ctx, ExplicitConfigFile(path), overrides...)
	if err != nil {
		return cfg, err
	}
	info, _ := os.Stat(path)
	modTime := time.Time{}
	if info != nil {
		modTime = info.ModTime()
	}
	cfg.Sources = []ConfigSource{{Path: path, Type: "explicit", ModTime: modTime}}
	return cfg, nil
}

func annotateErrorWithSource(filename string, contents []byte, err error) error {
	if err == nil {
		return nil
	}

	validationError := ValidationError{}
	if errors.As(err, &validationError) {
		path, err := yaml.PathString(validationError.Path)
		if err != nil {
			return err
		}

		annotatedSource, err := path.AnnotateSource(contents, true)
		if err != nil {
			return err
		}

		validationError.File = filename
		validationError.AnnotatedSource = string(annotatedSource)

		return validationError
	}

	return err
}
