package config

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/gofrs/flock"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/credentials"
	"github.com/grafana/gcx/internal/docs"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/xdg"
	"github.com/grafana/grafana-app-sdk/logging"
)

// keychainStoreFn returns the credentials.Store used by Load and Write. It is
// a package-level variable so tests can inject a fake store. Production code
// uses credentials.Open() which probes the OS keychain.
//
// Under `go test` (detected via testing.Testing()), the default is a no-op
// store that reports ErrUnavailable for every operation. This prevents any
// test in any package from triggering OS-keychain prompts when it loads a
// config file. Tests that need to exercise the keychain code path must
// explicitly install their own store by overriding this variable.
//
//nolint:gochecknoglobals // test injection seam for the keychain backend.
var keychainStoreFn = defaultKeychainStore

// renameConfigFile is a test seam for proving keychain rollback when the final
// atomic replacement fails.
//
//nolint:gochecknoglobals // narrow filesystem failure-injection seam.
var renameConfigFile = os.Rename

// syncConfigDirectory is a test seam for the post-rename durability barrier.
// Old keychain generations are deleted only after it succeeds.
//
//nolint:gochecknoglobals // narrow filesystem failure-injection seam.
var syncConfigDirectory = func(dir string) error {
	// Windows does not expose a portable write-capable directory handle through
	// os.Open, and FlushFileBuffers on its read-only handle fails after rename.
	// The atomic replace still completes; retain the Unix durability barrier
	// where directory fsync is supported.
	if runtime.GOOS == "windows" {
		return nil
	}
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() { _ = directory.Close() }()
	return directory.Sync()
}

// openStoreOnce memoizes the OS-keychain probe (credentials.Open) for the
// lifetime of the process. Open probes the backend with a syscall (a
// /usr/bin/security subprocess on macOS); without memoization every Load — and
// every layer of a layered load — repaid that probe.
//
//nolint:gochecknoglobals // process-wide memoization of the keychain probe.
var (
	openStoreOnce sync.Once
	openedStore   credentials.Store
)

func defaultKeychainStore() credentials.Store {
	if testing.Testing() {
		return testingNoopStore{}
	}
	openStoreOnce.Do(func() { openedStore = credentials.Open() })
	return openedStore
}

type testingNoopStore struct{}

func (testingNoopStore) Get(string) (string, error) { return "", credentials.ErrUnavailable }
func (testingNoopStore) Set(string, string) error   { return credentials.ErrUnavailable }
func (testingNoopStore) Delete(string) error        { return credentials.ErrUnavailable }

const (
	configFilePermissions  = 0o600
	StandardConfigFolder   = "gcx"
	StandardConfigFileName = "config.yaml"
	ConfigFileEnvVar       = "GCX_CONFIG"
	LocalConfigFileName    = ".gcx.yaml"

	defaultEmptyConfigFile = `
version: 1
contexts:
  default: {}
current-context: default
`
)

// DefaultEmptyConfigFile is the default content for a newly created config file.
const DefaultEmptyConfigFile = defaultEmptyConfigFile

// ConfigSource describes a discovered config file and its layer type.
type ConfigSource struct {
	Path     string    `json:"path"`
	Type     string    `json:"type"` // "system", "user", "local", "explicit"
	ModTime  time.Time `json:"modified"`
	snapshot []byte
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
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return ConfigSource{}, false, nil
	}
	if err != nil {
		return ConfigSource{}, false, err
	}
	if typ == "local" && !info.Mode().IsRegular() {
		return ConfigSource{}, false, fmt.Errorf("refusing auto-discovered local config %s: file must be regular (symlinks are not allowed)", path)
	}
	return ConfigSource{Path: path, Type: typ, ModTime: info.ModTime()}, true, nil
}

// readConfigSource refuses to follow auto-discovered repository symlinks and
// proves that the opened descriptor is the same regular file observed before
// and after open. Explicit config paths remain user-authorized and may be
// symlinks.
func readConfigSource(source ConfigSource) ([]byte, error) {
	if source.Type != "local" {
		return os.ReadFile(source.Path)
	}
	before, err := os.Lstat(source.Path)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("refusing auto-discovered local config %s: file must be regular (symlinks are not allowed)", source.Path)
	}
	file, err := os.Open(source.Path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	afterOpen, err := os.Lstat(source.Path)
	if err != nil {
		return nil, err
	}
	if !opened.Mode().IsRegular() || !afterOpen.Mode().IsRegular() ||
		!os.SameFile(before, opened) || !os.SameFile(opened, afterOpen) {
		return nil, fmt.Errorf("refusing auto-discovered local config %s: file changed while opening", source.Path)
	}
	contents, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	afterRead, err := os.Lstat(source.Path)
	if err != nil {
		return nil, err
	}
	if !afterRead.Mode().IsRegular() || !os.SameFile(opened, afterRead) {
		return nil, fmt.Errorf("refusing auto-discovered local config %s: file changed while reading", source.Path)
	}
	return contents, nil
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
	if dirs := xdg.ConfigDirs(); len(dirs) > 0 {
		return dirs[0]
	}
	return ""
}

// xdgUserConfigDir returns the XDG user config directory.
func xdgUserConfigDir() string {
	return xdg.ConfigHome()
}

type Override func(cfg *Config) error

type Source func() (string, error)

type configWriteLockHeldKey struct{}

func withConfigWriteLockHeld(ctx context.Context) context.Context {
	return context.WithValue(ctx, configWriteLockHeldKey{}, true)
}

func configWriteLockIsHeld(ctx context.Context) bool {
	held, _ := ctx.Value(configWriteLockHeldKey{}).(bool)
	return held
}

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
		if err := CreateDefaultConfigFile(file); err != nil {
			return "", err
		}
		return file, nil
	}

	// Last resort: platform XDG (ConfigFile creates parent dirs).
	configSubpath := filepath.Join(StandardConfigFolder, StandardConfigFileName)
	file, err := xdg.ConfigFile(configSubpath)
	if err != nil {
		return "", err
	}
	if err := CreateDefaultConfigFile(file); err != nil {
		return "", err
	}
	return file, nil
}

// CreateDefaultConfigFile atomically creates a new empty config without ever
// replacing an existing file or following an existing symlink.
func CreateDefaultConfigFile(file string) error {
	created, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, configFilePermissions)
	if errors.Is(err, os.ErrExist) {
		info, inspectErr := os.Lstat(file)
		if inspectErr != nil {
			return inspectErr
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refusing existing config %s: file must be regular (symlinks are not allowed during creation)", file)
		}
		return nil
	}
	if err != nil {
		return err
	}
	complete := false
	closed := false
	defer func() {
		if !closed {
			_ = created.Close()
		}
		if !complete {
			_ = os.Remove(file)
		}
	}()
	if _, err := io.WriteString(created, defaultEmptyConfigFile); err != nil {
		return err
	}
	if err := created.Sync(); err != nil {
		return err
	}
	closeErr := created.Close()
	closed = true
	if closeErr != nil {
		return closeErr
	}
	if err := syncConfigDirectory(filepath.Dir(file)); err != nil {
		return err
	}
	complete = true
	return nil
}

//nolint:gocyclo,nestif // Loading keeps versioning, legacy migration, source binding, and keychain migration in one ordered trust pipeline.
func Load(ctx context.Context, source Source, overrides ...Override) (Config, error) {
	config := Config{}

	filename, err := source()
	if err != nil {
		return config, err
	}
	layer, err := configLayerForPath(filename, configLayerFromCtx(ctx))
	if err != nil {
		return config, err
	}
	if layer != "" {
		ctx = withConfigLayer(ctx, layer)
	}

	logging.FromContext(ctx).Debug("Loading config", slog.String("filename", filename))
	config.Source = filename

	contents, snapshotted := configSnapshotFromContext(ctx, filename)
	if !snapshotted {
		contents, err = readConfigFileForLayer(filename, configLayerFromCtx(ctx))
		if err != nil {
			if os.IsNotExist(err) {
				sourceIdentity, identityErr := canonicalConfigSourceForLayer(filename, configLayerFromCtx(ctx))
				if identityErr != nil {
					return config, identityErr
				}
				config.sourceLayer = configLayerFromCtx(ctx)
				config.bindSourceIdentity(sourceIdentity)
				config.expectSourceAbsent = true
			}
			return config, err
		}
	}
	if err := validateDeclaredConfigVersion(filename, contents); err != nil {
		return config, err
	}

	loadedLegacy := isLegacyConfig(contents)
	if loadedLegacy {
		config, err = migrateLegacyConfig(ctx, source, filename, contents)
		if err != nil {
			return config, err
		}
		config.Source = filename
		if !config.migrationDeferred {
			persisted, readErr := readConfigFileForLayer(filename, configLayerFromCtx(ctx))
			if readErr != nil {
				return config, readErr
			}
			contents = persisted
			codec := &format.YAMLCodec{BytesAsBase64: true}
			if err := codec.Decode(bytes.NewReader(contents), &config); err != nil {
				return config, UnmarshalError{File: filename, Err: err}
			}
			config.Source = filename
		}
	} else {
		codec := &format.YAMLCodec{BytesAsBase64: true}
		if err := codec.Decode(bytes.NewBuffer(contents), &config); err != nil {
			return config, UnmarshalError{File: filename, Err: err}
		}
	}

	sourceIdentity, err := canonicalConfigSourceForLayer(filename, configLayerFromCtx(ctx))
	if err != nil {
		return config, err
	}
	config.sourceLayer = configLayerFromCtx(ctx)
	config.bindSourceIdentity(sourceIdentity)
	config.sourceRevision = sha256.Sum256(contents)
	config.hasSourceRevision = true
	if migrationPersistenceSuppressed(ctx) {
		config.migrationDeferred = true
	}

	config.Resolve()
	if err := config.materializeCloudCredentialDestinations(); err != nil {
		return config, err
	}
	if err := validateLocalExternalTLSCredentials(&config); err != nil {
		return config, err
	}
	inventoryBoundSentinels(&config)
	config.capturePlaintextCredentialOrigins()

	log := logging.FromContext(ctx)
	// Defer opening the keychain until a sentinel actually needs resolving or a
	// plaintext secret needs migrating; configs with no keychain-backed secrets
	// then never probe the OS keychain.
	store := newLazyStore(keychainStoreFn)
	config.keychainStore = store

	// Only resolve sentinels for the current context eagerly. Other contexts
	// are resolved on demand via Config.ResolveContext to avoid redundant
	// keychain lookups.
	if cur := config.Contexts[config.CurrentContext]; cur != nil {
		backed, preserve, states := resolveSentinelsForContext(cur, store)
		config.trackKeychainResults(backed, preserve, states)
	}
	if loadedLegacy && !config.migrationDeferred {
		for name := range config.Contexts {
			config.ResolveContext(name)
		}
	}

	if !config.migrationDeferred && config.hasPlaintextSecrets() {
		migrated, writeErr := writeConfig(ctx, source, config, true)
		var durabilityErr *configDurabilityError
		switch {
		case errors.As(writeErr, &durabilityErr) && migrated > 0:
			if err := refreshKeychainRuntimeAfterWrite(&config, filename, sourceIdentity, configLayerFromCtx(ctx), store); err != nil {
				return config, err
			}
			log.Warn("config was replaced but its directory durability barrier failed; old and new keychain generations were retained",
				"file", filename,
				"error", writeErr.Error())
		case writeErr != nil:
			log.Warn("could not persist keychain migration; credentials remain in plaintext",
				"file", filename,
				"error", writeErr.Error())
		case migrated > 0:
			log.Info("migrated plaintext credentials into OS keychain",
				"count", migrated,
				"file", filename)
			if err := refreshKeychainRuntimeAfterWrite(&config, filename, sourceIdentity, configLayerFromCtx(ctx), store); err != nil {
				return config, err
			}
		}
	}

	initialContext := config.CurrentContext
	for _, override := range overrides {
		if err := override(&config); err != nil {
			return config, annotateErrorWithSource(filename, contents, err)
		}
	}

	// If an override (e.g. --context flag) switched the current context,
	// resolve that context's keychain sentinels too.
	if config.CurrentContext != initialContext {
		config.ResolveContext(config.CurrentContext)
	}
	if err := enforceRuntimeCredentialBindings(&config); err != nil {
		return config, err
	}

	return config, nil
}

type configDurabilityError struct {
	err error
}

func (e *configDurabilityError) Error() string { return e.err.Error() }
func (e *configDurabilityError) Unwrap() error { return e.err }

func Write(ctx context.Context, source Source, cfg Config) error {
	_, err := writeConfig(ctx, source, cfg, false)
	return err
}

func refreshKeychainRuntimeAfterWrite(cfg *Config, filename, sourceIdentity, layer string, store credentials.Store) error {
	contents, err := readConfigFileForLayer(filename, layer)
	if err != nil {
		return err
	}
	var disk Config
	codec := &format.YAMLCodec{BytesAsBase64: true}
	if err := codec.Decode(bytes.NewReader(contents), &disk); err != nil {
		return UnmarshalError{File: filename, Err: err}
	}
	disk.sourceLayer = layer
	disk.bindSourceIdentity(sourceIdentity)
	disk.Resolve()
	if err := disk.materializeCloudCredentialDestinations(); err != nil {
		return err
	}
	inventoryBoundSentinels(&disk)
	disk.keychainStore = store
	if cur := disk.Contexts[disk.CurrentContext]; cur != nil {
		backed, preserve, states := resolveSentinelsForContext(cur, store)
		disk.trackKeychainResults(backed, preserve, states)
	}
	disk.capturePlaintextCredentialOrigins()
	disk.Source = cfg.Source
	disk.Sources = cfg.Sources
	disk.sourceLayer = cfg.sourceLayer
	disk.sourceRevision = sha256.Sum256(contents)
	disk.hasSourceRevision = true
	*cfg = disk
	return nil
}

func readConfigFileForLayer(filename, layer string) ([]byte, error) {
	if layer == "local" {
		return readConfigSource(ConfigSource{Path: filename, Type: layer})
	}
	return os.ReadFile(filename)
}

// writeConfig returns the number of newly staged credential generations.
// autoMigrationOnly leaves the config untouched when no plaintext credential
// could be staged (for example while the keychain is unavailable).
//
//nolint:gocyclo,nestif // The filesystem rename, durability barrier, and keychain commit/rollback are one atomic write protocol.
func writeConfig(ctx context.Context, source Source, cfg Config, autoMigrationOnly bool) (int, error) {
	// Config contains pointer-backed stack, cloud, and context maps. Work on an
	// isolated copy so validation failures and temporary keychain sentinel swaps
	// cannot mutate the caller's in-memory configuration.
	cfg = cloneConfigForWrite(cfg)
	filename, err := source()
	if err != nil {
		return 0, err
	}

	log := logging.FromContext(ctx)
	log.Debug("Writing config", slog.String("filename", filename))
	if cfg.migrationDeferred {
		return 0, fmt.Errorf("legacy config migration is deferred; resolve the reported migration blocker before writing %s (%s)", filename, docs.ConfigMigration)
	}
	if err := validateConfigForWrite(filename, &cfg); err != nil {
		return 0, err
	}
	layer := cfg.sourceLayer
	if layer == "" {
		layer = configLayerFromCtx(ctx)
	}
	layer, err = configLayerForPath(filename, layer)
	if err != nil {
		return 0, err
	}
	cfg.sourceLayer = layer
	sourceIdentity, err := canonicalConfigSourceForLayer(filename, layer)
	if err != nil {
		return 0, err
	}
	writeFilename, err := configWriteTarget(filename, sourceIdentity, layer != "local")
	if err != nil {
		return 0, err
	}
	if !configWriteLockIsHeld(ctx) {
		writeLockPath, err := configLockFile(sourceIdentity, "write")
		if err != nil {
			return 0, err
		}
		writeLock := flock.New(writeLockPath)
		lockCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		locked, err := writeLock.TryLockContext(lockCtx, 100*time.Millisecond)
		if err != nil {
			return 0, fmt.Errorf("lock config for write: %w", err)
		}
		if !locked {
			return 0, fmt.Errorf("timed out locking config for write: %s", filename)
		}
		defer func() { _ = writeLock.Unlock() }()
	}
	if err := prepareConfigSourceForWrite(&cfg, sourceIdentity); err != nil {
		return 0, err
	}
	if err := validateConfigWriteSnapshot(writeFilename, sourceIdentity, &cfg); err != nil {
		return 0, err
	}
	cfg.Resolve()
	if err := cfg.materializeCloudCredentialDestinations(); err != nil {
		return 0, err
	}

	var keychainTxn *keychainWriteTransaction
	configRenamed := false
	if cfg.hasSecretsToReconcile() {
		keychainTxn, err = reconcileKeychain(&cfg, keychainStoreFn(), log)
		if err != nil {
			return 0, err
		}
		defer func() {
			keychainTxn.restore()
			if !configRenamed {
				keychainTxn.rollback()
			}
		}()
	}
	staged := 0
	if keychainTxn != nil {
		staged = len(keychainTxn.writes)
	}
	if autoMigrationOnly && staged == 0 {
		return 0, nil
	}
	if keychainTxn != nil {
		if err := keychainTxn.preflightDeletes(); err != nil {
			return 0, err
		}
	}
	var previousContents []byte
	if keychainTxn != nil && len(keychainTxn.deletes) > 0 {
		previousContents, err = os.ReadFile(writeFilename)
		if err != nil {
			return 0, fmt.Errorf("capture config before keychain deletion: %w", err)
		}
	}

	// Write to a temp file and rename it into place so concurrent readers
	// never observe a truncated config. Token persistence rewrites this file
	// while other gcx invocations Load it without holding the refresh flock.
	tmp, err := os.CreateTemp(filepath.Dir(writeFilename), filepath.Base(writeFilename)+"-*.tmp")
	if err != nil {
		return 0, err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once renamed

	codec := &format.YAMLCodec{BytesAsBase64: true}
	if err := codec.Encode(tmp, cfg); err != nil {
		_ = tmp.Close()
		return 0, err
	}
	// os.CreateTemp creates the file 0o600; chmod keeps the contract explicit
	// should configFilePermissions ever change.
	if err := tmp.Chmod(configFilePermissions); err != nil {
		_ = tmp.Close()
		return 0, err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return 0, err
	}
	if err := tmp.Close(); err != nil {
		return 0, err
	}
	if cfg.expectSourceAbsent {
		if err := os.Link(tmpName, writeFilename); err != nil {
			if os.IsExist(err) {
				return 0, fmt.Errorf("config was created since it was loaded; reload %s before writing", filename)
			}
			return 0, fmt.Errorf("install new config without replacement: %w", err)
		}
	} else {
		if err := renameConfigFile(tmpName, writeFilename); err != nil {
			return 0, err
		}
	}
	configRenamed = true
	if err := syncConfigDirectory(filepath.Dir(writeFilename)); err != nil {
		return staged, &configDurabilityError{err: fmt.Errorf("sync config directory after rename: %w", err)}
	}
	if keychainTxn != nil {
		if err := keychainTxn.commit(warningWriterFromCtx(ctx)); err != nil {
			var commitErr *keychainCommitError
			if errors.As(err, &commitErr) && !commitErr.rollbackComplete {
				// The new config is already renamed and directory-synced. At least
				// one old keychain generation could not be restored, so reverting
				// the YAML would risk pointing at a missing credential. Keep the new
				// config and every staged generation; a later cleanup may remove any
				// harmless orphaned old accounts.
				return staged, fmt.Errorf("keychain cleanup failed and an old credential generation could not be restored; retained the committed config and new credential generations: %w", err)
			}
			if restoreErr := restoreConfigContents(writeFilename, previousContents); restoreErr != nil {
				return staged, errors.Join(err, fmt.Errorf("restore config after keychain deletion failure: %w", restoreErr))
			}
			configRenamed = false
			return staged, err
		}
	}
	return staged, nil
}

func restoreConfigContents(filename string, contents []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(filename), filepath.Base(filename)+"-restore-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(contents); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(configFilePermissions); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := renameConfigFile(tmpName, filename); err != nil {
		return err
	}
	return syncConfigDirectory(filepath.Dir(filename))
}

func configLayerForPath(filename, declared string) (string, error) {
	_ = filename
	return declared, nil
}

func configLockFile(sourceIdentity, purpose string) (string, error) {
	stateHome := xdg.StateHome()
	if testing.Testing() {
		stateHome = filepath.Join(os.TempDir(), "gcx-test-state")
	}
	if stateHome == "" {
		return "", errors.New("cannot determine state directory for config lock")
	}
	lockDir := filepath.Join(stateHome, "gcx", "locks")
	if err := os.MkdirAll(lockDir, 0o700); err != nil {
		return "", fmt.Errorf("create private config lock directory: %w", err)
	}
	info, err := os.Lstat(lockDir)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("config lock directory is not a private directory: %s", lockDir)
	}
	if err := os.Chmod(lockDir, 0o700); err != nil {
		return "", fmt.Errorf("secure config lock directory: %w", err)
	}
	digest := sha256.Sum256([]byte(sourceIdentity))
	return filepath.Join(lockDir, fmt.Sprintf("%x.%s.lock", digest, purpose)), nil
}

func validateConfigWriteSnapshot(filename, sourceIdentity string, cfg *Config) error {
	contents, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) && !cfg.hasSourceRevision {
			return nil
		}
		return fmt.Errorf("verify credential references before write: %w", err)
	}
	if err := validateDeclaredConfigVersion(filename, contents); err != nil {
		return err
	}
	if cfg.expectSourceAbsent {
		return fmt.Errorf("config was created since it was loaded; reload %s before writing", filename)
	}
	if cfg.hasSourceRevision && sha256.Sum256(contents) != cfg.sourceRevision {
		return fmt.Errorf("config changed since it was loaded; reload %s before writing", filename)
	}
	if len(cfg.keychainStates) == 0 {
		return nil
	}
	if isLegacyConfig(contents) {
		return fmt.Errorf("config changed since it was loaded; reload %s before writing", filename)
	}
	var disk Config
	codec := &format.YAMLCodec{BytesAsBase64: true}
	if err := codec.Decode(bytes.NewReader(contents), &disk); err != nil {
		return UnmarshalError{File: filename, Err: err}
	}
	disk.sourceLayer = cfg.sourceLayer
	disk.bindSourceIdentity(sourceIdentity)
	disk.Resolve()
	if err := disk.materializeCloudCredentialDestinations(); err != nil {
		return err
	}
	diskOwners := map[string]secretOwner{}
	for _, owner := range disk.secretOwners() {
		diskOwners[owner.key] = owner
	}
	for key, state := range cfg.keychainStates {
		if key.source != sourceIdentity || state.sentinel == "" {
			continue
		}
		owner, ok := diskOwners[key.owner]
		if !ok {
			return fmt.Errorf("config changed since it was loaded: credential owner %q is missing; reload before writing", key.owner)
		}
		ref, ok := owner.ref(key.field)
		if !ok || ref.get() != state.sentinel {
			return fmt.Errorf("config changed since it was loaded: credential %q field %q was updated; reload before writing", key.owner, key.field)
		}
	}
	return nil
}

// cloneConfigForWrite copies every node that Write or its helpers can mutate.
// Runtime credential maps are read-only during reconciliation except for the
// source-key rewrite in prepareConfigSourceForWrite, so only secretMutations
// needs its own map allocation.
func cloneConfigForWrite(cfg Config) Config {
	cloned := cfg
	cloned.Stacks = make(map[string]*StackConfig, len(cfg.Stacks))
	for name, stack := range cfg.Stacks {
		if stack == nil {
			cloned.Stacks[name] = nil
			continue
		}
		stackCopy := *stack
		if stack.Grafana != nil {
			grafanaCopy := *stack.Grafana
			if stack.Grafana.TLS != nil {
				tlsCopy := *stack.Grafana.TLS
				tlsCopy.CertData = slices.Clone(stack.Grafana.TLS.CertData)
				tlsCopy.KeyData = slices.Clone(stack.Grafana.TLS.KeyData)
				tlsCopy.CAData = slices.Clone(stack.Grafana.TLS.CAData)
				tlsCopy.credentialCertFile.contents = slices.Clone(stack.Grafana.TLS.credentialCertFile.contents)
				tlsCopy.credentialKeyFile.contents = slices.Clone(stack.Grafana.TLS.credentialKeyFile.contents)
				tlsCopy.credentialCAFile.contents = slices.Clone(stack.Grafana.TLS.credentialCAFile.contents)
				grafanaCopy.TLS = &tlsCopy
			}
			stackCopy.Grafana = &grafanaCopy
		}
		stackCopy.Providers = make(map[string]map[string]string, len(stack.Providers))
		for provider, values := range stack.Providers {
			valuesCopy := make(map[string]string, len(values))
			maps.Copy(valuesCopy, values)
			stackCopy.Providers[provider] = valuesCopy
		}
		cloned.Stacks[name] = &stackCopy
	}
	cloned.Cloud = make(map[string]*CloudEntry, len(cfg.Cloud))
	for name, entry := range cfg.Cloud {
		if entry == nil {
			cloned.Cloud[name] = nil
			continue
		}
		entryCopy := *entry
		entryCopy.OAuthScopes = slices.Clone(entry.OAuthScopes)
		cloned.Cloud[name] = &entryCopy
	}
	cloned.Contexts = make(map[string]*Context, len(cfg.Contexts))
	for name, contextEntry := range cfg.Contexts {
		if contextEntry == nil {
			cloned.Contexts[name] = nil
			continue
		}
		contextCopy := *contextEntry
		cloned.Contexts[name] = &contextCopy
	}
	cloned.Sources = slices.Clone(cfg.Sources)
	cloned.secretMutations = make(secretMutationSet, len(cfg.secretMutations))
	maps.Copy(cloned.secretMutations, cfg.secretMutations)
	return cloned
}

func configWriteTarget(filename, canonicalSource string, allowSymlink bool) (string, error) {
	info, err := os.Lstat(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return filename, nil
		}
		return "", err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return filename, nil
	}
	if !allowSymlink {
		return "", fmt.Errorf("refusing to write auto-discovered local config symlink: %s", filename)
	}
	if _, err := filepath.EvalSymlinks(filename); err != nil {
		return "", fmt.Errorf("resolve config symlink %s: %w", filename, err)
	}
	return canonicalSource, nil
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
	var hasLegacyLayer bool
	if err := preflightLayeredSources(sources, &hasLegacyLayer); err != nil {
		return Config{}, err
	}
	var migrationWarnings *inMemoryMigrationWarningCollector
	if hasLegacyLayer && len(sources) > 1 {
		migrationWarnings = &inMemoryMigrationWarningCollector{}
	}

	// Load and merge in priority order (system → user → local).
	var merged Config
	for i, src := range sources {
		loadCtx := withConfigLayer(ctx, src.Type)
		if hasLegacyLayer && len(sources) > 1 && isLegacyConfig(src.snapshot) {
			loadCtx = withMigrationPersistenceSuppressed(loadCtx)
			loadCtx = withInMemoryMigrationWarningCollector(loadCtx, migrationWarnings)
		}
		if src.snapshot != nil {
			loadCtx = withConfigSnapshot(loadCtx, src.Path, src.snapshot)
		}
		loaded, err := Load(loadCtx, ExplicitConfigFile(src.Path))
		if err != nil {
			return Config{}, err
		}
		current, err := readConfigSource(src)
		if err != nil {
			return Config{}, err
		}
		if loaded.hasSourceRevision && sha256.Sum256(current) != loaded.sourceRevision {
			return Config{}, fmt.Errorf("config %s changed while loading layered configuration; retry", src.Path)
		}
		sources[i].snapshot = bytes.Clone(current)
		if info, statErr := os.Lstat(src.Path); statErr == nil {
			sources[i].ModTime = info.ModTime()
		}
		if i == 0 {
			merged = loaded
		} else {
			merged = MergeConfigs(merged, loaded)
		}
	}
	if hasLegacyLayer && len(sources) > 1 {
		warnIncompleteLayeredMigration(
			ctx,
			remainingLegacySourceSnapshots(sources, "", true),
			migrationWarnings.exceptionalWarnings(),
		)
	}

	merged.Sources = sources

	// Apply overrides on the merged config.
	for _, override := range overrides {
		if err := override(&merged); err != nil {
			return merged, err
		}
	}

	// Each layer's Load only resolved its own current-context, so the effective
	// context after merge and overrides (e.g. a --context selecting a context
	// that was current in no layer) may still hold raw keychain sentinels.
	// Idempotent for already-resolved fields.
	merged.ResolveContext(merged.CurrentContext)
	if err := enforceRuntimeCredentialBindings(&merged); err != nil {
		return merged, err
	}

	return merged, nil
}

// LoadForWrite resolves the target config layer, loads only that layer, and
// returns both the Config and its Source. Callers should mutate the Config
// and pass the Source to Write, preserving layer separation when multiple
// config files are present.
//
// explicitFile is the value of the --config flag; fileType is the value of
// the --file flag. Both may be empty.
//
//nolint:nestif // Legacy-layer preflight and interrupted-migration recovery are one ordered, fail-before-write selection flow.
func LoadForWrite(ctx context.Context, explicitFile, fileType string) (Config, Source, error) {
	if explicitFile != "" {
		src := ExplicitConfigFile(explicitFile)
		cfg, err := loadExplicit(ctx, explicitFile)
		return cfg, src, err
	}

	if fileType != "" {
		// Selecting a layer by --file only needs the discovered source paths, not
		// a full layered load+merge (which would parse every layer and resolve its
		// keychain sentinels just to discard all but one). Honor the explicit-file
		// env bypass exactly as LoadLayered does: when set, layering is bypassed
		// and there is no named layer to select.
		if os.Getenv(ConfigFileEnvVar) != "" {
			return Config{}, nil, fmt.Errorf("no %s config file found", fileType)
		}
		sources, err := DiscoverSources()
		if err != nil {
			return Config{}, nil, err
		}
		for _, s := range sources {
			if s.Type == fileType {
				src := ExplicitConfigFile(s.Path)
				loadCtx := withConfigLayer(ctx, s.Type)
				contents, readErr := readConfigSource(s)
				if readErr != nil {
					return Config{}, nil, readErr
				}
				// Freeze the target bytes selected for this write before inspecting
				// their schema. If an older or concurrent process rewrites a v1 file
				// back to legacy before Load runs, loading the snapshot prevents an
				// un-preflighted migration; the eventual Write revision check rejects
				// the intervening change.
				loadCtx = withConfigSnapshot(loadCtx, s.Path, contents)
				targetWasLegacy := isLegacyConfig(contents)
				if targetWasLegacy && len(sources) > 1 {
					preflightErr := preflightLayeredSources(sources)
					if preflightErr != nil {
						var incomplete *layeredMigrationIncompleteError
						if !errors.As(preflightErr, &incomplete) || !incomplete.includesLayer(fileType) {
							return Config{}, nil, preflightErr
						}
						// A previous explicit step already migrated another layer.
						// Let this targeted write finish one of the named remaining
						// legacy layers; ordinary loads keep returning the typed error
						// until every overlapping layer is complete.
					}
					for _, preflightSource := range sources {
						if preflightSource.Type == fileType && preflightSource.snapshot != nil {
							loadCtx = withConfigSnapshot(loadCtx, s.Path, preflightSource.snapshot) //nolint:fatcontext // One immutable snapshot is attached to the selected layer.
							break
						}
					}
				}
				cfg, err := Load(loadCtx, src)
				if err == nil && targetWasLegacy {
					remaining := remainingLegacySourceSnapshots(sources, fileType, cfg.migrationDeferred)
					warnIncompleteLayeredMigration(ctx, remaining, nil)
				}
				return cfg, src, err
			}
		}
		// Fresh system (no config files yet): preserve LoadLayered's auto-create.
		// LoadLayered only ever created the user layer, so --file user creates and
		// returns it; other layer types have nothing to auto-create and still error.
		if fileType == "user" && len(sources) == 0 {
			cfg, err := Load(ctx, StandardLocation())
			return cfg, StandardLocation(), err
		}
		return Config{}, nil, fmt.Errorf("no %s config file found", fileType)
	}

	layered, err := LoadLayered(ctx, "")
	if err != nil {
		return Config{}, nil, err
	}
	switch len(layered.Sources) {
	case 0:
		// Defensive: LoadLayered auto-created a config file and re-ran discovery,
		// so it normally returns exactly one source (case 1). This only hits if
		// that re-discovery failed to find the just-created file; reuse it anyway.
		return layered, StandardLocation(), nil
	case 1:
		// Single source - LoadLayered already loaded exactly this file.
		return layered, ExplicitConfigFile(layered.Sources[0].Path), nil
	default:
		return Config{}, nil, errors.New("multiple config files loaded; specify which to update with --file (system, user, local)")
	}
}

// CanInitializeMissingSource reports whether err is the initial ENOENT from
// loading cfg's selected source. Callers with constructive intent may proceed
// from this state: Write will use cfg's private absent-source marker to install
// the first document without replacement. A later ENOENT after a source was
// successfully read (for example during migration) deliberately returns false.
func CanInitializeMissingSource(cfg Config, err error) bool {
	return errors.Is(err, os.ErrNotExist) && cfg.expectSourceAbsent && !cfg.hasSourceRevision
}

func remainingLegacySourceSnapshots(sources []ConfigSource, migratedLayer string, migrationDeferred bool) []ConfigSource {
	remaining := make([]ConfigSource, 0, len(sources))
	for _, source := range sources {
		if !isLegacyConfig(source.snapshot) {
			continue
		}
		if source.Type == migratedLayer && !migrationDeferred {
			continue
		}
		remaining = append(remaining, source)
	}
	return remaining
}

func warnIncompleteLayeredMigration(ctx context.Context, remaining []ConfigSource, warnings []inMemoryMigrationWarning) {
	if len(remaining) == 0 {
		return
	}
	steps := layeredMigrationSteps(remaining)
	guidance := layeredMigrationGuidance(remaining)
	const message = "layered configuration migration is incomplete"
	if writer := warningWriterFromCtx(ctx); writer != nil {
		fmt.Fprintf(writer,
			"Warning: %s: several of your config files still use the legacy format.\n"+
				"gcx converted them in memory for this run - commands keep working, but the files themselves\n"+
				"are unchanged and config or credential writes stay blocked until each file is migrated:\n%s\n%s",
			message, steps, guidance)
		writeExceptionalMigrationWarnings(writer, warnings)
		fmt.Fprintln(writer)
		return
	}
	if blockers := formatExceptionalMigrationWarnings(warnings); blockers != "" {
		logging.FromContext(ctx).Warn(message,
			"steps", steps,
			"repair", layeredMigrationEditCommands(remaining),
			"guide", docs.ConfigMigration,
			"blockers", blockers)
		return
	}
	logging.FromContext(ctx).Warn(message,
		"steps", steps,
		"repair", layeredMigrationEditCommands(remaining),
		"guide", docs.ConfigMigration)
}

// loadExplicit loads a single explicit config file, bypassing layered discovery.
func loadExplicit(ctx context.Context, path string, overrides ...Override) (Config, error) {
	// Reaching this function means the caller explicitly selected one document
	// through --config or GCX_CONFIG. Preserve that provenance through legacy
	// migration; constructing an ExplicitConfigFile Source and calling Load
	// directly is only a path resolver and does not itself grant legacy-keychain
	// authority.
	var err error
	ctx, err = withExplicitLegacyMigrationConsent(ctx, path)
	if err != nil {
		return Config{}, err
	}
	ctx = withConfigLayer(ctx, "explicit")
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

// LoadDiagnostics reads only the diagnostics settings from the layered config,
// skipping context parsing and keychain resolution entirely. It runs on every
// command invocation to configure agent logging, so unlike LoadLayered it never
// builds the full Config, never probes the OS keychain, and never auto-creates a
// config file. Returns nil when diagnostics are not configured in any layer.
func LoadDiagnostics(ctx context.Context) *DiagnosticsConfig {
	var result *DiagnosticsConfig
	for _, path := range diagnosticsSourcePaths(ctx) {
		d, err := readDiagnostics(path)
		if err != nil || d == nil {
			continue
		}
		if result == nil {
			result = d
			continue
		}
		merged := mergeDiagnosticsConfig(result, d)
		result = &merged
	}
	return result
}

// diagnosticsSourcePaths returns config file paths in low→high precedence order,
// honoring the GCX_CONFIG explicit-file bypass exactly as LoadLayered does.
func diagnosticsSourcePaths(ctx context.Context) []string {
	if envPath := os.Getenv(ConfigFileEnvVar); envPath != "" {
		return []string{envPath}
	}
	sources, err := DiscoverSources()
	if err != nil {
		logging.FromContext(ctx).Debug("diagnostics: source discovery failed", "error", err.Error())
		return nil
	}
	paths := make([]string, 0, len(sources))
	for _, s := range sources {
		paths = append(paths, s.Path)
	}
	return paths
}

// readDiagnostics decodes a config file and returns only its diagnostics block.
// It parses into the full Config (the codec rejects unknown fields, so a partial
// struct will not do) but deliberately skips keychain resolution, plaintext
// migration, and the config auto-creation that Load performs. Legacy-format
// files are read through the legacy struct (never migrated here) so settings
// like `telemetry: disabled` are honoured even on the run that performs the
// migration. Missing or malformed files yield (nil, err).
func readDiagnostics(path string) (*DiagnosticsConfig, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := validateDeclaredConfigVersion(path, contents); err != nil {
		return nil, err
	}
	codec := &format.YAMLCodec{BytesAsBase64: true}
	if isLegacyConfig(contents) {
		var lc legacyConfig
		if err := codec.Decode(bytes.NewBuffer(contents), &lc); err != nil {
			return nil, err
		}
		return lc.Diagnostics, nil
	}
	var cfg Config
	if err := codec.Decode(bytes.NewBuffer(contents), &cfg); err != nil {
		return nil, err
	}
	return cfg.Diagnostics, nil
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
