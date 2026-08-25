package config

import (
	"bytes"
	"context"
	"os"
	"strings"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/grafana-app-sdk/logging"
)

// keychainMode is the resolved credential-storage backend for this invocation.
type keychainMode string

const (
	keychainModeEnabled  keychainMode = "enabled"
	keychainModeDisabled keychainMode = "disabled"
)

// envKeychain is read by resolveKeychainMode. keychainEnvConsistencyTest
// asserts it matches the CredentialsConfig struct tag the docs generator
// reads, so the documented name cannot drift from the resolved one.
const envKeychain = "GCX_KEYCHAIN"

// keychainModeForProcess is the one place the environment and the config file
// are combined. Every consumer of the keychain mode goes through it (via the
// memoized resolvedKeychainMode), so GCX_KEYCHAIN is honoured on every path
// that can reach the credential store. Nothing else may read
// CredentialsConfig.Keychain directly: doing so would consult the config file
// while ignoring the environment.
func keychainModeForProcess(ctx context.Context) keychainMode {
	return resolveKeychainMode(os.Getenv, func() string {
		return keychainModeConfigValue(ctx)
	})
}

// resolveKeychainMode resolves whether credentials may be stored in the OS
// keychain. Precedence, highest first: GCX_KEYCHAIN, the credentials.keychain
// config value, the built-in default (enabled). configValue is a func so
// callers only pay the config-file read when the environment doesn't already
// decide the mode.
func resolveKeychainMode(getenv func(string) string, configValue func() string) keychainMode {
	if mode, ok := parseKeychainMode(getenv(envKeychain)); ok {
		return mode
	}
	if mode, ok := parseKeychainMode(configValue()); ok {
		return mode
	}
	return keychainModeEnabled
}

func parseKeychainMode(value string) (keychainMode, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return "", false
	case "disabled", "disable", "off", "false", "no", "0":
		return keychainModeDisabled, true
	default:
		// Unrecognised values fail toward encrypted storage: with the keychain
		// enabled by default, a typo in an opt-out must not silently move
		// credentials into plaintext on disk.
		return keychainModeEnabled, true
	}
}

// keychainModeConfigValue returns the credentials.keychain value from the
// config layers permitted to influence credential storage, last-wins. It reads
// the files directly rather than through Load: the keychain backend is needed
// to load a config, so deciding whether to open it cannot depend on a full load.
func keychainModeConfigValue(ctx context.Context) string {
	value := ""
	for _, path := range keychainModeSourcePaths(ctx) {
		creds, err := readCredentials(path)
		if err != nil || creds == nil || creds.Keychain == "" {
			continue
		}
		value = creds.Keychain
	}
	return value
}

// keychainModeSourcePaths returns the config files allowed to switch keychain
// storage off, in low→high precedence order. An explicit GCX_CONFIG file is as
// deliberate as the environment variable and is honoured. An auto-discovered
// repository-local .gcx.yaml is not: a checked-in file must not be able to
// downgrade a user's credential storage to plaintext (docs/adrs/config-v1/001).
func keychainModeSourcePaths(ctx context.Context) []string {
	if envPath := os.Getenv(ConfigFileEnvVar); envPath != "" {
		return []string{envPath}
	}
	sources, err := DiscoverSources()
	if err != nil {
		logging.FromContext(ctx).Debug("keychain mode: source discovery failed", "error", err.Error())
		return nil
	}
	var paths []string
	for _, source := range sources {
		if source.Type == "local" {
			continue
		}
		paths = append(paths, source.Path)
	}
	return paths
}

// readCredentials decodes a config file and returns only its credentials
// block, skipping context parsing, keychain resolution, and the config
// auto-creation Load performs. Legacy-format files are read through the legacy
// struct (never migrated here) so `keychain: disabled` is honoured even on the
// run that performs the migration. Missing or malformed files yield (nil, err).
func readCredentials(path string) (*CredentialsConfig, error) {
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
		return lc.Credentials, nil
	}
	var cfg Config
	if err := codec.Decode(bytes.NewBuffer(contents), &cfg); err != nil {
		return nil, err
	}
	return cfg.Credentials, nil
}
