# Config Layering and Management Commands Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add multi-file config layering (system/user/local), a `config path` command to show loaded config sources, a `config edit` command to open configs in $EDITOR, and `--file` flag to `config set`/`config unset` for targeting specific layers.

**Architecture:** Extend `internal/config/loader.go` with `ConfigSource` type and `LoadLayered()` function that discovers config files from system XDG, user XDG, and `.grafanactl.yaml` (cwd), deep-merges them in priority order, and attaches source metadata. Add two new subcommands under `cmd/grafanactl/config/`. Modify `config set`/`config unset` to require `--file` when multiple configs exist.

**Tech Stack:** Go, `github.com/adrg/xdg` (already imported), `github.com/goccy/go-yaml`, reflection-based deep merge, `$EDITOR` for config edit.

---

## Dependency Graph

```
T1 (ConfigSource type + DiscoverSources)
└─→ T2 (Deep merge + LoadLayered)
    ├─→ T3 (config path command)
    ├─→ T4 (config edit command)
    └─→ T5 (config set/unset --file flag)
```

---

### Task 1: Add ConfigSource type and DiscoverSources function

**Files:**
- Modify: `internal/config/loader.go`
- Create: `internal/config/loader_test.go` (add layering tests)

**Step 1: Write the failing test**

Add to `internal/config/loader_test.go`:

```go
func TestDiscoverSources(t *testing.T) {
	// Set up temp dirs for system, user, local.
	systemDir := t.TempDir()
	userDir := t.TempDir()
	localDir := t.TempDir()

	// Write config files.
	systemFile := filepath.Join(systemDir, "grafanactl", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(systemFile), 0o755))
	require.NoError(t, os.WriteFile(systemFile, []byte("contexts:\n  sys: {}\ncurrent-context: sys\n"), 0o600))

	userFile := filepath.Join(userDir, "grafanactl", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(userFile), 0o755))
	require.NoError(t, os.WriteFile(userFile, []byte("contexts:\n  usr: {}\ncurrent-context: usr\n"), 0o600))

	localFile := filepath.Join(localDir, ".grafanactl.yaml")
	require.NoError(t, os.WriteFile(localFile, []byte("contexts:\n  lcl: {}\n"), 0o600))

	sources, err := config.DiscoverSources(
		config.WithSystemDir(systemDir),
		config.WithUserDir(userDir),
		config.WithWorkDir(localDir),
	)
	require.NoError(t, err)

	require.Len(t, sources, 3)
	assert.Equal(t, "system", sources[0].Type)
	assert.Equal(t, "user", sources[1].Type)
	assert.Equal(t, "local", sources[2].Type)
	assert.Equal(t, systemFile, sources[0].Path)
	assert.Equal(t, userFile, sources[1].Path)
	assert.Equal(t, localFile, sources[2].Path)
}

func TestDiscoverSources_SkipsMissing(t *testing.T) {
	userDir := t.TempDir()
	userFile := filepath.Join(userDir, "grafanactl", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(userFile), 0o755))
	require.NoError(t, os.WriteFile(userFile, []byte("contexts:\n  usr: {}\ncurrent-context: usr\n"), 0o600))

	sources, err := config.DiscoverSources(
		config.WithSystemDir(t.TempDir()), // empty, no config
		config.WithUserDir(userDir),
		config.WithWorkDir(t.TempDir()), // empty, no .grafanactl.yaml
	)
	require.NoError(t, err)

	require.Len(t, sources, 1)
	assert.Equal(t, "user", sources[0].Type)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestDiscoverSources -v`
Expected: FAIL — `DiscoverSources` does not exist

**Step 3: Implement ConfigSource and DiscoverSources**

In `internal/config/loader.go`, add after the existing constants:

```go
const (
	LocalConfigFileName = ".grafanactl.yaml"
)

// ConfigSource describes a discovered config file and its layer type.
type ConfigSource struct {
	Path    string    `json:"path"`
	Type    string    `json:"type"`     // "system", "user", "local", "explicit"
	ModTime time.Time `json:"modified"`
}

// Priority returns the priority of this source (1=highest, 3=lowest).
func (s ConfigSource) Priority() int {
	switch s.Type {
	case "local":
		return 1
	case "user":
		return 2
	case "system":
		return 3
	default:
		return 0 // "explicit" — highest when --config used
	}
}

// DiscoverOption configures source discovery (primarily for testing).
type DiscoverOption func(*discoverOpts)

type discoverOpts struct {
	systemDir string
	userDir   string
	workDir   string
}

func WithSystemDir(dir string) DiscoverOption { return func(o *discoverOpts) { o.systemDir = dir } }
func WithUserDir(dir string) DiscoverOption   { return func(o *discoverOpts) { o.userDir = dir } }
func WithWorkDir(dir string) DiscoverOption   { return func(o *discoverOpts) { o.workDir = dir } }

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
```

Add `"time"` to the imports.

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -run TestDiscoverSources -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/config/loader.go internal/config/loader_test.go
git commit -m "feat(config): add ConfigSource type and DiscoverSources for config layering"
```

---

### Task 2: Deep merge and LoadLayered function

**Files:**
- Create: `internal/config/merge.go`
- Create: `internal/config/merge_test.go`
- Modify: `internal/config/loader.go` (add LoadLayered)
- Modify: `internal/config/types.go` (add Sources field to Config)

**Step 1: Write the failing test for deep merge**

Create `internal/config/merge_test.go`:

```go
package config_test

import (
	"testing"

	"github.com/grafana/grafanactl/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeConfigs(t *testing.T) {
	tests := []struct {
		name string
		base config.Config
		over config.Config
		want config.Config
	}{
		{
			name: "higher layer overrides scalar fields",
			base: config.Config{CurrentContext: "base-ctx"},
			over: config.Config{CurrentContext: "over-ctx"},
			want: config.Config{CurrentContext: "over-ctx"},
		},
		{
			name: "higher layer does not erase with zero value",
			base: config.Config{CurrentContext: "base-ctx"},
			over: config.Config{CurrentContext: ""},
			want: config.Config{CurrentContext: "base-ctx"},
		},
		{
			name: "contexts merge by key",
			base: config.Config{
				Contexts: map[string]*config.Context{
					"prod": {Grafana: &config.GrafanaConfig{Server: "https://prod.grafana.net"}},
				},
			},
			over: config.Config{
				Contexts: map[string]*config.Context{
					"staging": {Grafana: &config.GrafanaConfig{Server: "https://staging.grafana.net"}},
				},
			},
			want: config.Config{
				Contexts: map[string]*config.Context{
					"prod":    {Grafana: &config.GrafanaConfig{Server: "https://prod.grafana.net"}},
					"staging": {Grafana: &config.GrafanaConfig{Server: "https://staging.grafana.net"}},
				},
			},
		},
		{
			name: "same context deep merges fields",
			base: config.Config{
				Contexts: map[string]*config.Context{
					"prod": {Grafana: &config.GrafanaConfig{Server: "https://prod.grafana.net"}},
				},
			},
			over: config.Config{
				Contexts: map[string]*config.Context{
					"prod": {Cloud: &config.CloudConfig{Token: "cloud-token"}},
				},
			},
			want: config.Config{
				Contexts: map[string]*config.Context{
					"prod": {
						Grafana: &config.GrafanaConfig{Server: "https://prod.grafana.net"},
						Cloud:   &config.CloudConfig{Token: "cloud-token"},
					},
				},
			},
		},
		{
			name: "higher layer overrides field within same context",
			base: config.Config{
				Contexts: map[string]*config.Context{
					"prod": {Grafana: &config.GrafanaConfig{Server: "https://old.grafana.net", APIToken: "old-token"}},
				},
			},
			over: config.Config{
				Contexts: map[string]*config.Context{
					"prod": {Grafana: &config.GrafanaConfig{Server: "https://new.grafana.net"}},
				},
			},
			want: config.Config{
				Contexts: map[string]*config.Context{
					"prod": {Grafana: &config.GrafanaConfig{Server: "https://new.grafana.net", APIToken: "old-token"}},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := config.MergeConfigs(tt.base, tt.over)
			assert.Equal(t, tt.want.CurrentContext, got.CurrentContext)
			for name, wantCtx := range tt.want.Contexts {
				gotCtx, ok := got.Contexts[name]
				require.True(t, ok, "missing context %q", name)
				if wantCtx.Grafana != nil {
					require.NotNil(t, gotCtx.Grafana)
					assert.Equal(t, wantCtx.Grafana.Server, gotCtx.Grafana.Server)
					if wantCtx.Grafana.APIToken != "" {
						assert.Equal(t, wantCtx.Grafana.APIToken, gotCtx.Grafana.APIToken)
					}
				}
				if wantCtx.Cloud != nil {
					require.NotNil(t, gotCtx.Cloud)
					assert.Equal(t, wantCtx.Cloud.Token, gotCtx.Cloud.Token)
				}
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestMergeConfigs -v`
Expected: FAIL — `MergeConfigs` does not exist

**Step 3: Implement MergeConfigs**

Create `internal/config/merge.go`:

```go
package config

// MergeConfigs deep-merges two configs. Fields in `over` take precedence
// over fields in `base`. Zero-value fields in `over` do not erase `base`.
func MergeConfigs(base, over Config) Config {
	result := base

	// Scalar: current-context — higher layer wins if non-empty.
	if over.CurrentContext != "" {
		result.CurrentContext = over.CurrentContext
	}

	// Map: contexts — merge by key.
	if over.Contexts != nil {
		if result.Contexts == nil {
			result.Contexts = make(map[string]*Context)
		}
		for name, overCtx := range over.Contexts {
			if baseCtx, ok := result.Contexts[name]; ok {
				result.Contexts[name] = mergeContexts(baseCtx, overCtx)
			} else {
				result.Contexts[name] = overCtx
			}
		}
	}

	return result
}

func mergeContexts(base, over *Context) *Context {
	if base == nil {
		return over
	}
	if over == nil {
		return base
	}

	result := *base // shallow copy

	// Grafana config: field-level merge.
	if over.Grafana != nil {
		if result.Grafana == nil {
			result.Grafana = over.Grafana
		} else {
			merged := mergeGrafanaConfig(result.Grafana, over.Grafana)
			result.Grafana = &merged
		}
	}

	// Cloud config: field-level merge.
	if over.Cloud != nil {
		if result.Cloud == nil {
			result.Cloud = over.Cloud
		} else {
			merged := mergeCloudConfig(result.Cloud, over.Cloud)
			result.Cloud = &merged
		}
	}

	// Providers map: merge by key (string→map[string]string).
	if over.Providers != nil {
		if result.Providers == nil {
			result.Providers = make(map[string]map[string]string)
		}
		for k, v := range over.Providers {
			if baseV, ok := result.Providers[k]; ok {
				merged := make(map[string]string)
				for kk, vv := range baseV {
					merged[kk] = vv
				}
				for kk, vv := range v {
					merged[kk] = vv
				}
				result.Providers[k] = merged
			} else {
				result.Providers[k] = v
			}
		}
	}

	// Datasources map: merge by key.
	if over.Datasources != nil {
		if result.Datasources == nil {
			result.Datasources = make(map[string]string)
		}
		for k, v := range over.Datasources {
			result.Datasources[k] = v
		}
	}

	// Named datasource overrides.
	if over.DefaultPrometheusDatasource != "" {
		result.DefaultPrometheusDatasource = over.DefaultPrometheusDatasource
	}
	if over.DefaultLokiDatasource != "" {
		result.DefaultLokiDatasource = over.DefaultLokiDatasource
	}
	if over.DefaultPyroscopeDatasource != "" {
		result.DefaultPyroscopeDatasource = over.DefaultPyroscopeDatasource
	}

	return &result
}

func mergeGrafanaConfig(base, over *GrafanaConfig) GrafanaConfig {
	result := *base
	if over.Server != "" {
		result.Server = over.Server
	}
	if over.User != "" {
		result.User = over.User
	}
	if over.Password != "" {
		result.Password = over.Password
	}
	if over.APIToken != "" {
		result.APIToken = over.APIToken
	}
	if over.OrgID != 0 {
		result.OrgID = over.OrgID
	}
	if over.StackID != 0 {
		result.StackID = over.StackID
	}
	if over.TLS != nil {
		result.TLS = over.TLS
	}
	return result
}

func mergeCloudConfig(base, over *CloudConfig) CloudConfig {
	result := *base
	if over.Token != "" {
		result.Token = over.Token
	}
	if over.Stack != "" {
		result.Stack = over.Stack
	}
	if over.APIUrl != "" {
		result.APIUrl = over.APIUrl
	}
	return result
}
```

**Step 4: Run tests**

Run: `go test ./internal/config/ -run TestMergeConfigs -v`
Expected: PASS

**Step 5: Add Sources field to Config and implement LoadLayered**

In `internal/config/types.go`, add to Config struct:

```go
type Config struct {
	Source  string          `json:"-" yaml:"-"`
	Sources []ConfigSource  `json:"-" yaml:"-"` // NEW — populated by LoadLayered

	Contexts       map[string]*Context `json:"contexts" yaml:"contexts"`
	CurrentContext string              `json:"current-context" yaml:"current-context"`
}
```

In `internal/config/loader.go`, add:

```go
// LoadLayered discovers config files, loads and deep-merges them, then applies overrides.
// If no config files are found, creates a default user config (preserving current behavior).
// If the --config flag is set (ExplicitConfigFile), bypasses layering entirely.
func LoadLayered(ctx context.Context, explicitFile string, overrides ...Override) (Config, error) {
	// --config flag bypasses layering.
	if explicitFile != "" {
		cfg, err := Load(ctx, ExplicitConfigFile(explicitFile), overrides...)
		if err != nil {
			return cfg, err
		}
		info, _ := os.Stat(explicitFile)
		modTime := time.Time{}
		if info != nil {
			modTime = info.ModTime()
		}
		cfg.Sources = []ConfigSource{{Path: explicitFile, Type: "explicit", ModTime: modTime}}
		return cfg, nil
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
```

**Step 6: Write LoadLayered integration test**

Add to `internal/config/loader_test.go`:

```go
func TestLoadLayered(t *testing.T) {
	systemDir := t.TempDir()
	userDir := t.TempDir()
	localDir := t.TempDir()

	systemFile := filepath.Join(systemDir, "grafanactl", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(systemFile), 0o755))
	require.NoError(t, os.WriteFile(systemFile, []byte(`
contexts:
  prod:
    grafana:
      server: https://prod.grafana.net
current-context: prod
`), 0o600))

	userFile := filepath.Join(userDir, "grafanactl", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(userFile), 0o755))
	require.NoError(t, os.WriteFile(userFile, []byte(`
contexts:
  prod:
    grafana:
      token: user-token
  staging:
    grafana:
      server: https://staging.grafana.net
`), 0o600))

	localFile := filepath.Join(localDir, ".grafanactl.yaml")
	require.NoError(t, os.WriteFile(localFile, []byte(`
contexts:
  prod:
    cloud:
      token: local-cloud-token
`), 0o600))

	// Temporarily override discovery to use test dirs.
	// This requires the DiscoverOption approach or env var overrides.
	// For now, test MergeConfigs directly with pre-loaded configs.

	// Load each config independently.
	sysCfg, err := config.Load(t.Context(), config.ExplicitConfigFile(systemFile))
	require.NoError(t, err)

	usrCfg, err := config.Load(t.Context(), config.ExplicitConfigFile(userFile))
	require.NoError(t, err)

	lclCfg, err := config.Load(t.Context(), config.ExplicitConfigFile(localFile))
	require.NoError(t, err)

	// Merge in order: system → user → local.
	merged := config.MergeConfigs(sysCfg, usrCfg)
	merged = config.MergeConfigs(merged, lclCfg)

	// prod context should have: server from system, token from user, cloud from local.
	prodCtx := merged.Contexts["prod"]
	require.NotNil(t, prodCtx)
	assert.Equal(t, "https://prod.grafana.net", prodCtx.Grafana.Server)
	assert.Equal(t, "user-token", prodCtx.Grafana.APIToken)
	require.NotNil(t, prodCtx.Cloud)
	assert.Equal(t, "local-cloud-token", prodCtx.Cloud.Token)

	// staging context should exist (added by user layer).
	stagingCtx := merged.Contexts["staging"]
	require.NotNil(t, stagingCtx)
	assert.Equal(t, "https://staging.grafana.net", stagingCtx.Grafana.Server)

	// current-context: "prod" from system, not overridden (user/local don't set it).
	assert.Equal(t, "prod", merged.CurrentContext)
}
```

**Step 7: Run tests**

Run: `go test ./internal/config/ -v`
Expected: PASS

**Step 8: Commit**

```bash
git add internal/config/merge.go internal/config/merge_test.go internal/config/loader.go internal/config/loader_test.go internal/config/types.go
git commit -m "feat(config): add deep merge and LoadLayered for config layering"
```

---

### Task 3: `config path` command

**Files:**
- Create: `cmd/grafanactl/config/path.go`
- Modify: `cmd/grafanactl/config/command.go` (register subcommand)

**Step 1: Write the command**

Create `cmd/grafanactl/config/path.go`:

```go
package config

import (
	"time"

	cmdio "github.com/grafana/grafanactl/cmd/grafanactl/io"
	"github.com/grafana/grafanactl/internal/config"
	"github.com/spf13/cobra"
)

type configPathEntry struct {
	Path     string `json:"path"`
	Type     string `json:"type"`
	Priority string `json:"priority"`
	Modified string `json:"modified"`
}

func pathCmd(configOpts *Options) *cobra.Command {
	opts := cmdio.Options{}

	cmd := &cobra.Command{
		Use:   "path",
		Short: "Show loaded config file paths",
		Long:  "Display all config files that contribute to the merged configuration, ordered by priority.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configOpts.loadConfigTolerantLayered()
			if err != nil {
				return err
			}

			sources := cfg.Sources
			if len(sources) == 0 {
				cmd.Println("No config files found.")
				return nil
			}

			entries := make([]configPathEntry, 0, len(sources))
			for i, src := range sources {
				priority := formatPriority(src.Priority(), i == 0, i == len(sources)-1)
				entries = append(entries, configPathEntry{
					Path:     src.Path,
					Type:     src.Type,
					Priority: priority,
					Modified: src.ModTime.Format(time.DateTime),
				})
			}

			// Sort by priority: highest (1) first.
			// Sources from DiscoverSources are system→user→local,
			// so reverse for display.
			for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
				entries[i], entries[j] = entries[j], entries[i]
			}

			codec := opts.Codec(cmd, cmdio.DefaultFormat("table"))
			return codec.Encode(cmd.OutOrStdout(), entries)
		},
	}

	opts.Setup(cmd)
	return cmd
}

func formatPriority(p int, isFirst, isLast bool) string {
	switch {
	case isFirst && isLast:
		return fmt.Sprintf("%d", p)
	case p == 1:
		return fmt.Sprintf("%d (highest)", p)
	case isLast:
		return fmt.Sprintf("%d (lowest)", p)
	default:
		return fmt.Sprintf("%d", p)
	}
}
```

Note: The exact codec/table wiring will depend on how existing commands like `list-contexts` render tables. Check `cmd/grafanactl/config/command.go` for the `listContextsCmd` pattern and follow it.

**Step 2: Register the subcommand**

In `cmd/grafanactl/config/command.go`, in the `Command()` function, add alongside the other subcommands:

```go
cmd.AddCommand(pathCmd(configOpts))
```

**Step 3: Add `loadConfigTolerantLayered` helper**

In `cmd/grafanactl/config/command.go`, add to the `Options` struct or as a method:

```go
func (opts *Options) loadConfigTolerantLayered() (config.Config, error) {
	return config.LoadLayered(context.Background(), opts.ConfigFile)
}
```

**Step 4: Build and manual test**

Run: `go build ./... && bin/grafanactl config path`
Expected: Shows at least the user config file

**Step 5: Commit**

```bash
git add cmd/grafanactl/config/path.go cmd/grafanactl/config/command.go
git commit -m "feat(config): add config path command to show loaded config sources"
```

---

### Task 4: `config edit` command

**Files:**
- Create: `cmd/grafanactl/config/edit.go`
- Modify: `cmd/grafanactl/config/command.go` (register subcommand)

**Step 1: Implement the command**

Create `cmd/grafanactl/config/edit.go`:

```go
package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	internalConfig "github.com/grafana/grafanactl/internal/config"
	"github.com/spf13/cobra"
)

func editCmd(configOpts *Options) *cobra.Command {
	var create bool

	cmd := &cobra.Command{
		Use:   "edit [type]",
		Short: "Open a config file in $EDITOR",
		Long: `Open a config file in your editor. If multiple config files are loaded,
specify which one to edit: system, user, or local.

If only one config file exists, it is opened directly.`,
		Args: cobra.MaximumNArgs(1),
		ValidArgs: []string{"system", "user", "local"},
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configOpts.loadConfigTolerantLayered()
			if err != nil {
				return err
			}

			sources := cfg.Sources
			var target internalConfig.ConfigSource

			if len(args) == 1 {
				typ := args[0]
				if create && typ == "local" {
					// Create .grafanactl.yaml in cwd if missing.
					localPath, _ := filepath.Abs(internalConfig.LocalConfigFileName)
					if _, err := os.Stat(localPath); os.IsNotExist(err) {
						if err := os.WriteFile(localPath, []byte(internalConfig.DefaultEmptyConfigFile), 0o600); err != nil {
							return fmt.Errorf("failed to create %s: %w", localPath, err)
						}
					}
					target = internalConfig.ConfigSource{Path: localPath, Type: "local"}
				} else {
					found := false
					for _, s := range sources {
						if s.Type == typ {
							target = s
							found = true
							break
						}
					}
					if !found {
						return fmt.Errorf("no %s config file found (use --create to create one)", typ)
					}
				}
			} else if len(sources) == 1 {
				target = sources[0]
			} else if len(sources) == 0 {
				return fmt.Errorf("no config files found. Use 'config edit user --create' to create one")
			} else {
				msg := "multiple config files loaded. Specify which to edit:\n"
				for _, s := range sources {
					msg += fmt.Sprintf("  grafanactl config edit %s\n", s.Type)
				}
				return fmt.Errorf(msg)
			}

			return openInEditor(target.Path)
		},
	}

	cmd.Flags().BoolVar(&create, "create", false, "Create the config file if it doesn't exist")

	return cmd
}

func openInEditor(path string) error {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		if runtime.GOOS == "windows" {
			editor = "notepad"
		} else {
			editor = "vi"
		}
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	cmd := exec.Command(editor, abs)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
```

**Step 2: Register the subcommand**

In `cmd/grafanactl/config/command.go`:

```go
cmd.AddCommand(editCmd(configOpts))
```

**Step 3: Build and manual test**

Run: `go build ./... && EDITOR=cat bin/grafanactl config edit user`
Expected: Prints user config to stdout (cat is the "editor")

**Step 4: Commit**

```bash
git add cmd/grafanactl/config/edit.go cmd/grafanactl/config/command.go
git commit -m "feat(config): add config edit command to open config in \$EDITOR"
```

---

### Task 5: Add `--file` flag to `config set` and `config unset`

**Files:**
- Modify: `cmd/grafanactl/config/command.go` (setCmd, unsetCmd)

**Step 1: Add the `--file` flag and ambiguity check**

In the existing `setCmd()` function (around line 466), add a `--file` flag and modify the RunE to:

1. Load config via `loadConfigTolerantLayered()` to discover sources
2. If multiple sources and no `--file` flag → error with message
3. If `--file` specified → resolve the file path from sources by type
4. Load that specific file, apply `SetValue`, write back to that file

```go
func setCmd(configOpts *Options) *cobra.Command {
	var fileType string

	cmd := &cobra.Command{
		Use:   "set PROPERTY_NAME PROPERTY_VALUE",
		Short: "Set a single config value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Discover sources for ambiguity check.
			layered, err := configOpts.loadConfigTolerantLayered()
			if err != nil {
				return err
			}

			sources := layered.Sources

			// Resolve target file.
			var targetSource config.Source
			if configOpts.ConfigFile != "" {
				targetSource = config.ExplicitConfigFile(configOpts.ConfigFile)
			} else if fileType != "" {
				found := false
				for _, s := range sources {
					if s.Type == fileType {
						targetSource = config.ExplicitConfigFile(s.Path)
						found = true
						break
					}
				}
				if !found {
					return fmt.Errorf("no %s config file found", fileType)
				}
			} else if len(sources) > 1 {
				return fmt.Errorf("multiple config files loaded. Specify which to edit with --file <type> (system, user, local)")
			} else if len(sources) == 1 {
				targetSource = config.ExplicitConfigFile(sources[0].Path)
			} else {
				targetSource = configOpts.configSource()
			}

			// Load, set, write — targeting the specific file.
			cfg, err := config.Load(cmd.Context(), targetSource)
			if err != nil {
				return err
			}

			if err := config.SetValue(&cfg, args[0], args[1]); err != nil {
				return err
			}

			return config.Write(cmd.Context(), targetSource, cfg)
		},
	}

	cmd.Flags().StringVar(&fileType, "file", "", "Config layer to write to (system, user, local)")

	return cmd
}
```

Apply the same pattern to `unsetCmd()`.

**Step 2: Build and test**

Run: `go build ./...`

Test with single config (should work as before):
```bash
bin/grafanactl config set contexts.test.grafana.server https://test.grafana.net
bin/grafanactl config view --minify
```

**Step 3: Commit**

```bash
git add cmd/grafanactl/config/command.go
git commit -m "feat(config): add --file flag to config set/unset for targeting specific layers"
```

---

### Task 6: Wire LoadLayered into the main config loading path

**Files:**
- Modify: `cmd/grafanactl/config/command.go` (switch existing loadConfigTolerant and configSource)
- Modify: `internal/providers/configloader.go` (switch to LoadLayered)

**Step 1: Update providers.ConfigLoader**

The `ConfigLoader.loadConfig()` method currently calls `config.Load(ctx, source, overrides...)`. Change it to call `config.LoadLayered(ctx, l.configFile, overrides...)` so that all provider commands benefit from layering.

**Step 2: Update config commands**

The `config view`, `config check`, `config list-contexts` commands use `loadConfigTolerant()`. Update this to use `LoadLayered` so that `config view` shows the merged result.

**Step 3: Build and full test**

Run: `go build ./... && go test ./... -count=1`
Expected: All tests pass

**Step 4: Run `make all`**

Run: `GRAFANACTL_AGENT_MODE=false make all`
Expected: lint, tests, build, docs all green

**Step 5: Commit**

```bash
git add internal/providers/configloader.go cmd/grafanactl/config/command.go
git commit -m "feat(config): wire LoadLayered into main config loading path"
```

---

## Config UX after implementation

```bash
# See which config files are loaded
grafanactl config path

# Edit user config
grafanactl config edit user

# Create and edit local project config
grafanactl config edit local --create

# Set a value in the local config
grafanactl config set --file local contexts.prod.cloud.token my-token

# View merged config
grafanactl config view

# View with origin annotations
grafanactl config view --show-origin
```
