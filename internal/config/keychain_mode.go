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

// resolveKeychainPolicyForSources honours a policy the caller already resolved
// and passed down as a load option, and otherwise resolves one across every
// discovered source.
func resolveKeychainPolicyForSources(ctx context.Context, opts loadOptions, sources []ConfigSource) (keychainPolicy, error) {
	if policy, ok := opts.resolvedKeychainPolicy(); ok {
		return policy, nil
	}
	return resolveKeychainPolicy(ctx, sources)
}

// resolveKeychainPolicyForSource honours a policy the caller already resolved
// across every trusted layer and passed down as a load option; only a load
// that was handed no policy derives one from the single document it reads.
func resolveKeychainPolicyForSource(ctx context.Context, path string, opts loadOptions, contents []byte) (keychainPolicy, error) {
	if policy, ok := opts.resolvedKeychainPolicy(); ok {
		return policy, nil
	}
	sourceType := opts.layer
	if sourceType == "" {
		sourceType = "explicit"
	}
	return resolveKeychainPolicy(ctx, []ConfigSource{{Path: path, Type: sourceType, snapshot: contents}})
}

// resolveKeychainPolicyForWrite returns the policy Write should bind the
// credential store to. cfg.Credentials.Keychain is only validated when it did
// not come from the auto-discovered local layer: resolveKeychainPolicy
// already ignores that layer's value during policy resolution, so validating
// it here too would hard-fail a write over a typo in an untrusted file.
func resolveKeychainPolicyForWrite(cfg *Config, source string) (keychainPolicy, error) {
	if cfg.sourceLayer != "local" && cfg.Credentials != nil && cfg.Credentials.Keychain != "" {
		if _, ok := parseKeychainValue(cfg.Credentials.Keychain); !ok {
			return keychainPolicy{}, invalidKeychainConfigValue(source, cfg.Credentials.Keychain)
		}
	}
	if cfg.keychainPolicy.source != "" {
		return cfg.keychainPolicy, nil
	}
	// cfg.Credentials.Keychain is a value from some layer (possibly the
	// ignored auto-discovered local one) that was never run through
	// resolveKeychainPolicy. Every production setter of a validated
	// Credentials.Keychain also propagates keychainPolicy onto cfg, so
	// reaching here with a non-empty value means that invariant was not
	// honored; fail loudly instead of silently trusting an unverified layer.
	if cfg.Credentials != nil && cfg.Credentials.Keychain != "" {
		return keychainPolicy{}, fmt.Errorf("keychain policy not resolved before write to %s", source)
	}
	return overlayKeychainEnvironment(defaultKeychainPolicy()), nil
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

// sanitizeLocalKeychainPolicyForDecode removes an untrusted, structurally
// invalid auto-local policy from a throwaway typed-decode snapshot. gcx
// already refuses to honor this value during policy resolution regardless of
// its shape, so it is simply dropped rather than restored on a later write.
func sanitizeLocalKeychainPolicyForDecode(contents []byte) ([]byte, error) {
	if len(contents) == 0 {
		return contents, nil
	}
	var document map[string]any
	if err := yaml.Unmarshal(contents, &document); err != nil {
		return nil, err
	}
	section, ok := document["credentials"].(map[string]any)
	if !ok {
		return contents, nil
	}
	raw, present := section["keychain"]
	if !present {
		return contents, nil
	}
	if _, validTypedValue := raw.(string); validTypedValue {
		return contents, nil
	}
	delete(section, "keychain")
	return yaml.Marshal(document)
}

func typedConfigContents(contents []byte, layer string) ([]byte, error) {
	if layer != "local" {
		return contents, nil
	}
	return sanitizeLocalKeychainPolicyForDecode(contents)
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

// warnIgnoredLocalKeychainPolicyOnce keeps the notice to one per process, for
// the same reason warnUnrecognisedKeychainValueOnce does: a single command can
// reach several independent config entry points (a layered read followed by a
// targeted write, say), and each resolves the policy afresh. Repeating one
// security notice per entry point trains people to skim past it.
//
//nolint:gochecknoglobals // process-wide latch for a once-per-invocation notice.
var warnIgnoredLocalKeychainPolicyOnce sync.Once

func warnIgnoredLocalKeychainPolicy(ctx context.Context, source, value string) {
	warnIgnoredLocalKeychainPolicyOnce.Do(func() {
		writer := warningWriterFromCtx(ctx)
		if writer == nil {
			writer = os.Stderr
		}
		output.EmitWarn(writer, fmt.Sprintf(
			"credentials.keychain=%q in auto-discovered local config %s was ignored; select the file explicitly to use this policy",
			value, source,
		))
	})
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
