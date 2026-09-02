package config

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/goccy/go-yaml"
	"github.com/grafana/gcx/internal/output"
)

// keychainMode is the resolved credential-storage backend for this invocation.
type keychainMode string

const (
	keychainModeEnabled  keychainMode = "enabled"
	keychainModeDisabled keychainMode = "disabled"
)

// keychainPolicy is the immutable credential-storage decision attached to a
// loaded Config. source records which trusted input supplied the decision.
type keychainPolicy struct {
	mode   keychainMode
	source string
}

type keychainPolicyContextKey struct{}

type keychainConfigValue struct {
	value   string
	present bool
}

// envKeychain is declared as CLIOptions.Keychain, which is how it reaches the
// generated environment-variable reference. It is read here directly rather
// than through LoadCLIOptions so that a malformed value in an unrelated
// variable cannot make an explicit opt-out silently resolve to enabled.
// TestKeychainEnvTagMatchesResolvedName pins the two names together.
const envKeychain = "GCX_KEYCHAIN"

// keychainModeForProcess resolves the environment-only policy used by legacy
// callers. Config loading uses resolveKeychainPolicy so trusted config layers
// participate before the credential store is constructed.
func keychainModeForProcess() keychainMode {
	return overlayKeychainEnvironment(defaultKeychainPolicy()).mode
}

// parseKeychainEnv returns the environment mode, plus the value to warn about
// when it was neither empty nor one of the shared on/off values.
func parseKeychainEnv(value string) (keychainMode, string) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return keychainModeEnabled, ""
	}
	if mode, ok := parseKeychainValue(trimmed); ok {
		return mode, ""
	}
	// An unrecognised value keeps the keychain in use: a typo in an opt-out
	// must not move credentials into plaintext on disk.
	return keychainModeEnabled, trimmed
}

func parseKeychainValue(value string) (keychainMode, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "on":
		return keychainModeEnabled, true
	case "off":
		return keychainModeDisabled, true
	default:
		return keychainModeEnabled, false
	}
}

func defaultKeychainPolicy() keychainPolicy {
	return keychainPolicy{mode: keychainModeEnabled, source: "default"}
}

func withKeychainPolicy(ctx context.Context, policy keychainPolicy) context.Context {
	return context.WithValue(ctx, keychainPolicyContextKey{}, policy)
}

func keychainPolicyFromContext(ctx context.Context) (keychainPolicy, bool) {
	policy, ok := ctx.Value(keychainPolicyContextKey{}).(keychainPolicy)
	return policy, ok
}

// resolveKeychainPolicy overlays trusted source snapshots in discovery order,
// ignores auto-discovered repository policy, then applies the environment.
func resolveKeychainPolicy(ctx context.Context, sources []ConfigSource) (keychainPolicy, error) {
	policy := defaultKeychainPolicy()
	for _, source := range sources {
		value, err := decodeKeychainConfigValue(source.snapshot)
		if err != nil {
			return keychainPolicy{}, UnmarshalError{File: source.Path, Err: err}
		}
		if !value.present {
			continue
		}
		if source.Type == "local" {
			warnIgnoredLocalKeychainPolicy(ctx, source.Path, value.value)
			continue
		}
		mode, ok := parseKeychainValue(value.value)
		if !ok {
			return keychainPolicy{}, invalidKeychainConfigValue(source.Path, value.value)
		}
		policy = keychainPolicy{mode: mode, source: source.Path}
	}
	return overlayKeychainEnvironment(policy), nil
}

func resolveKeychainPolicyForSource(ctx context.Context, path, sourceType string, contents []byte) (keychainPolicy, error) {
	if policy, ok := keychainPolicyFromContext(ctx); ok {
		return policy, nil
	}
	if sourceType == "" {
		sourceType = "explicit"
	}
	return resolveKeychainPolicy(ctx, []ConfigSource{{Path: path, Type: sourceType, snapshot: contents}})
}

func resolveKeychainPolicyForWrite(cfg *Config, source string) (keychainPolicy, error) {
	if cfg.Credentials != nil && cfg.Credentials.Keychain != "" {
		if _, ok := parseKeychainValue(cfg.Credentials.Keychain); !ok {
			return keychainPolicy{}, invalidKeychainConfigValue(source, cfg.Credentials.Keychain)
		}
	}
	if cfg.keychainPolicy.source != "" {
		return cfg.keychainPolicy, nil
	}
	policy := defaultKeychainPolicy()
	if cfg.Credentials != nil && cfg.Credentials.Keychain != "" {
		mode, _ := parseKeychainValue(cfg.Credentials.Keychain)
		policy = keychainPolicy{mode: mode, source: source}
	}
	return overlayKeychainEnvironment(policy), nil
}

func decodeKeychainConfigValue(contents []byte) (keychainConfigValue, error) {
	if len(contents) == 0 {
		return keychainConfigValue{}, nil
	}
	var document map[string]any
	if err := yaml.Unmarshal(contents, &document); err != nil {
		return keychainConfigValue{}, err
	}
	section, ok := document["credentials"].(map[string]any)
	if !ok {
		return keychainConfigValue{}, nil
	}
	raw, present := section["keychain"]
	if !present {
		return keychainConfigValue{}, nil
	}
	value, ok := raw.(string)
	if !ok {
		value = fmt.Sprint(raw)
	}
	return keychainConfigValue{value: value, present: true}, nil
}

func overlayKeychainEnvironment(policy keychainPolicy) keychainPolicy {
	raw := os.Getenv(envKeychain)
	if strings.TrimSpace(raw) == "" {
		return policy
	}
	mode, rejected := parseKeychainEnv(raw)
	if rejected != "" {
		warnUnrecognisedKeychainValue(rejected)
	}
	return keychainPolicy{mode: mode, source: envKeychain}
}

func invalidKeychainConfigValue(source, value string) error {
	return fmt.Errorf("invalid credentials.keychain value %q in %s: expected on or off", value, source)
}

func warnIgnoredLocalKeychainPolicy(ctx context.Context, source, value string) {
	writer := warningWriterFromCtx(ctx)
	if writer == nil {
		writer = os.Stderr
	}
	output.EmitWarn(writer, fmt.Sprintf(
		"credentials.keychain=%q in auto-discovered local config %s was ignored; select the file explicitly to use this policy",
		value, source,
	))
}

func unrecognisedKeychainWarning(value string) string {
	return fmt.Sprintf("%s=%q is not a recognized value and was ignored; the OS credential store is still in use. Set %s=off to disable it.",
		envKeychain, value, envKeychain)
}

// warnUnrecognisedKeychainValueOnce keeps the notice to one per process.
// Several independent config loads can resolve policy during one command, so
// without the latch a malformed environment value could be reported repeatedly.
//
//nolint:gochecknoglobals // process-wide latch for a once-per-invocation notice.
var warnUnrecognisedKeychainValueOnce sync.Once

func warnUnrecognisedKeychainValue(value string) {
	warnUnrecognisedKeychainValueOnce.Do(func() {
		output.EmitWarn(os.Stderr, unrecognisedKeychainWarning(value))
	})
}
