package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gofrs/flock"
	"github.com/grafana/gcx/internal/credentials"
)

type keychainPolicyMutationContextKey struct{}

func withKeychainPolicyMutation(ctx context.Context, value string) context.Context {
	return context.WithValue(ctx, keychainPolicyMutationContextKey{}, value)
}

func keychainPolicyMutationFromContext(ctx context.Context) (string, bool) {
	value, ok := ctx.Value(keychainPolicyMutationContextKey{}).(string)
	return value, ok
}

// SetKeychainPolicy changes credentials.keychain using the intended effective
// policy for the entire load-and-write transaction. In particular, disabling
// storage never probes the previous store, while enabling storage stages any
// plaintext credentials and the policy update in the same atomic config write.
func SetKeychainPolicy(ctx context.Context, explicitFile, fileType, value string) (Source, error) {
	value, err := normalizedKeychainPolicyValue(value)
	if err != nil {
		return nil, err
	}
	snapshot := []byte("credentials:\n  keychain: " + strconv.Quote(value) + "\n")
	return mutateKeychainPolicy(ctx, explicitFile, fileType, snapshot,
		func(parent context.Context) context.Context { return withKeychainPolicyMutation(parent, value) },
		func(cfg *Config) {
			if cfg.Credentials == nil {
				cfg.Credentials = &CredentialsConfig{}
			}
			cfg.Credentials.Keychain = value
		},
	)
}

// ClearKeychainPolicy unsets credentials.keychain on the selected config
// layer, running the same locked load-and-write transaction as
// SetKeychainPolicy — intended-policy context, plaintext-migration
// suppression, and a whole-transaction flock — so `unset credentials.keychain`
// gets every guarantee `set` does instead of falling through to the
// unlocked generic mutation path. Clearing the field reverts the layer to
// whatever the remaining trusted layers (or the default "on") resolve to.
func ClearKeychainPolicy(ctx context.Context, explicitFile, fileType string) (Source, error) {
	snapshot := []byte("credentials: {}\n")
	return mutateKeychainPolicy(ctx, explicitFile, fileType, snapshot, nil,
		func(cfg *Config) {
			if cfg.Credentials != nil {
				cfg.Credentials.Keychain = ""
			}
		},
	)
}

// mutateKeychainPolicy holds the write lock for a credentials.keychain
// mutation's entire load-and-write transaction. syntheticSnapshot simulates
// the target layer's post-mutation contents so the intended policy - the one
// the write itself must honor - is resolved before anything is loaded.
// withIntendedValue, when non-nil, layers extra context (e.g. the intended
// value for legacy-migration bookkeeping) onto the mutation context before
// LoadForWrite runs. apply performs the field-specific edit on the loaded
// Config immediately before it is written.
func mutateKeychainPolicy(
	ctx context.Context,
	explicitFile, fileType string,
	syntheticSnapshot []byte,
	withIntendedValue func(context.Context) context.Context,
	apply func(cfg *Config),
) (Source, error) {
	target, sources, targetIndex, err := keychainPolicyMutationTarget(explicitFile, fileType)
	if err != nil {
		return nil, err
	}

	policySources := append([]ConfigSource(nil), sources...)
	policySources[targetIndex].snapshot = syntheticSnapshot
	policy, err := resolveKeychainPolicy(ctx, policySources)
	if err != nil {
		return nil, err
	}

	sourceIdentity, err := canonicalConfigSourceForLayer(target.Path, target.Type)
	if err != nil {
		return nil, err
	}
	lockPath, err := configWriteLockFile(sourceIdentity)
	if err != nil {
		return nil, err
	}
	lock := flock.New(lockPath)
	lockCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	locked, err := lock.TryLockContext(lockCtx, 100*time.Millisecond)
	if err != nil {
		return nil, fmt.Errorf("lock config for keychain policy update: %w", err)
	}
	if !locked {
		return nil, fmt.Errorf("timed out locking config for keychain policy update: %s", target.Path)
	}
	defer func() { _ = lock.Unlock() }()

	mutationCtx := withConfigWriteLockHeld(ctx, sourceIdentity)
	mutationCtx = withKeychainPolicy(mutationCtx, policy)
	if withIntendedValue != nil {
		mutationCtx = withIntendedValue(mutationCtx)
	}
	mutationCtx = withPlaintextMigrationSuppressed(mutationCtx)
	cfg, source, loadErr := LoadForWrite(mutationCtx, explicitFile, fileType)
	if loadErr != nil && (explicitFile == "" || !CanInitializeMissingSource(cfg, loadErr)) {
		return source, loadErr
	}
	apply(&cfg)
	cfg.keychainPolicy = policy
	cfg.keychainStore = newLazyStore(func() credentials.Store { return keychainStoreForPolicy(policy) })
	if err := Write(mutationCtx, source, cfg); err != nil {
		return source, err
	}
	return source, nil
}

func normalizedKeychainPolicyValue(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	mode, ok := parseKeychainValue(trimmed)
	if !ok {
		return "", fmt.Errorf("invalid credentials.keychain value %q: expected on or off", value)
	}
	if mode == keychainModeDisabled {
		return "off", nil
	}
	return "on", nil
}

// keychainPolicyMutationTarget resolves the config source a
// credentials.keychain mutation (set or unset) should write to. The
// auto-discovered local layer is never a valid target for this field: its
// policy is untrusted and ignored during resolution (see
// resolveKeychainPolicy), so writing the mutation there would change the
// file on disk without ever changing the effective policy. Callers must
// instead pick a trusted layer with --file user, --file system, or select
// the repository file explicitly with --config.
func keychainPolicyMutationTarget(explicitFile, fileType string) (ConfigSource, []ConfigSource, int, error) {
	if explicitFile != "" {
		target := ConfigSource{Path: explicitFile, Type: "explicit"}
		contents, err := readConfigSource(target)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return ConfigSource{}, nil, 0, err
		}
		target.snapshot = contents
		return target, []ConfigSource{target}, 0, nil
	}
	if fileType != "" && os.Getenv(ConfigFileEnvVar) != "" {
		return ConfigSource{}, nil, 0, fmt.Errorf("no %s config file found", fileType)
	}
	if envPath := os.Getenv(ConfigFileEnvVar); envPath != "" {
		target := ConfigSource{Path: envPath, Type: "explicit"}
		contents, err := readConfigSource(target)
		if err != nil {
			return ConfigSource{}, nil, 0, err
		}
		target.snapshot = contents
		return target, []ConfigSource{target}, 0, nil
	}

	sources, err := DiscoverSources()
	if err != nil {
		return ConfigSource{}, nil, 0, err
	}
	for i := range sources {
		contents, readErr := readConfigSource(sources[i])
		if readErr != nil {
			return ConfigSource{}, nil, 0, readErr
		}
		sources[i].snapshot = contents
	}

	target, index, selErr := selectConfigSource(sources, fileType)
	switch {
	case errors.Is(selErr, errNoConfigSourcesDiscovered):
		path, err := StandardLocation()()
		if err != nil {
			return ConfigSource{}, nil, 0, err
		}
		created := ConfigSource{Path: path, Type: "user"}
		contents, err := readConfigSource(created)
		if err != nil {
			return ConfigSource{}, nil, 0, err
		}
		created.snapshot = contents
		return created, []ConfigSource{created}, 0, nil
	case errors.Is(selErr, errAmbiguousConfigSource):
		// local is deliberately omitted: it is never a trusted target for
		// this field, unlike the generic multi-source message in
		// LoadForWrite.
		return ConfigSource{}, nil, 0, errors.New("multiple config files loaded; specify which to update with --file (system, user)")
	case selErr != nil:
		return ConfigSource{}, nil, 0, selErr
	}

	if target.Type == "local" {
		return ConfigSource{}, nil, 0, fmt.Errorf(
			"credentials.keychain cannot be changed in the auto-discovered local config %s: this security setting is untrusted there and never takes effect; use --file user, --file system, or --config %s to select a trusted file explicitly",
			target.Path, target.Path,
		)
	}

	return target, sources, index, nil
}
