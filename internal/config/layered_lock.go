package config

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/gofrs/flock"
	"github.com/grafana/grafana-app-sdk/logging"
)

// loadLayeredSourcesLocked recovers from a revision conflict. It reads fresh
// bytes while cooperating writers are excluded. External edits can still fail
// the revision check; those errors return without another load.
func loadLayeredSourcesLocked(ctx context.Context, opts loadOptions) (Config, error) {
	if err := ctx.Err(); err != nil {
		return Config{}, err
	}
	sources, err := DiscoverSources()
	if err != nil {
		return Config{}, err
	}
	if len(sources) == 0 {
		return Config{}, errors.New("config sources disappeared during layered loading; retry")
	}
	identities, release, err := lockLayeredSources(ctx, sources, opts.writeLockHeldFor)
	if err != nil {
		return Config{}, err
	}
	defer release()

	// Discovery can change while acquisition waits. Never read a new source
	// under a lock that protects a different path or canonical identity.
	current, err := DiscoverSources()
	if err != nil {
		return Config{}, err
	}
	if len(current) != len(sources) {
		return Config{}, errors.New("config sources changed while acquiring layered load locks; retry")
	}
	for i, src := range current {
		identity, err := canonicalConfigSourceForLayer(src.Path, src.Type)
		if err != nil {
			return Config{}, err
		}
		if src.Path != sources[i].Path || src.Type != sources[i].Type || identity != identities[src.Path] {
			return Config{}, fmt.Errorf("config source changed while acquiring layered load locks: %s; retry", src.Path)
		}
	}
	return loadLayeredSources(ctx, current, opts, identities)
}

// lockLayeredSources locks each canonical source once, in a stable order.
// A caller-owned lock is reused, never released or expanded into a lock set.
func lockLayeredSources(ctx context.Context, sources []ConfigSource, heldFor string) (map[string]string, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	identities := make(map[string]string, len(sources))
	ordered := make([]string, 0, len(sources))
	for _, src := range sources {
		identity, err := canonicalConfigSourceForLayer(src.Path, src.Type)
		if err != nil {
			return nil, nil, err
		}
		if heldFor != "" && identity != heldFor {
			return nil, nil, errors.New("config sources changed during a locked config update; retry")
		}
		identities[src.Path] = identity
		ordered = append(ordered, identity)
	}
	if heldFor != "" {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		return identities, func() {}, nil
	}
	slices.Sort(ordered)
	ordered = slices.Compact(ordered)
	var acquired []*flock.Flock
	release := func() {
		for _, lock := range slices.Backward(acquired) {
			_ = lock.Unlock()
		}
	}
	// Match the existing writer wait policy, with one budget for the whole set.
	lockCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for _, identity := range ordered {
		path, err := configLockFile(identity)
		if err != nil {
			release()
			return nil, nil, err
		}
		lock := flock.New(path)
		locked, err := lock.TryLockContext(lockCtx, 100*time.Millisecond)
		if err != nil || !locked {
			release()
			if err == nil {
				err = errors.New("lock was not acquired")
			}
			return nil, nil, fmt.Errorf("lock config for layered load %s: %w", identity, err)
		}
		acquired = append(acquired, lock)
		logging.FromContext(ctx).Debug("Locked config for layered load", slog.String("filename", identity))
	}
	if err := ctx.Err(); err != nil {
		release()
		return nil, nil, err
	}
	return identities, release, nil
}
