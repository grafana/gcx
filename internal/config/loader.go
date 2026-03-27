package config

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/adrg/xdg"
	"github.com/goccy/go-yaml"
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
func DiscoverSources(opts ...DiscoverOption) ([]ConfigSource, error) {
	o := discoverOpts{}
	for _, opt := range opts {
		opt(&o)
	}

	candidates := []struct {
		dir      string
		fallback func() string
		subpath  string
		typ      string
	}{
		{o.systemDir, xdgSystemConfigDir, filepath.Join(StandardConfigFolder, StandardConfigFileName), "system"},
		{o.userDir, xdgUserConfigDir, filepath.Join(StandardConfigFolder, StandardConfigFileName), "user"},
		{o.workDir, func() string { d, _ := os.Getwd(); return d }, LocalConfigFileName, "local"},
	}

	var sources []ConfigSource
	for _, c := range candidates {
		dir := c.dir
		if dir == "" {
			dir = c.fallback()
		}
		path := filepath.Join(dir, c.subpath)
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		sources = append(sources, ConfigSource{
			Path:    path,
			Type:    c.typ,
			ModTime: info.ModTime(),
		})
	}
	return sources, nil
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
		// Check if GCX_CONFIG environment variable is set
		if envPath := os.Getenv(ConfigFileEnvVar); envPath != "" {
			return envPath, nil
		}

		file, err := xdg.ConfigFile(filepath.Join(StandardConfigFolder, StandardConfigFileName))
		if err != nil {
			return "", err
		}

		_, err = os.Stat(file)
		// Create an empty config file, to ensure that the loader won't fail.
		if os.IsNotExist(err) {
			if createErr := os.WriteFile(file, []byte(defaultEmptyConfigFile), configFilePermissions); createErr != nil {
				return "", createErr
			}
		} else if err != nil {
			return "", err
		}

		return file, nil
	}
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
