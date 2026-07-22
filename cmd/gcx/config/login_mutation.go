package config

import (
	"context"
	"fmt"
	"os"

	internalconfig "github.com/grafana/gcx/internal/config"
)

// PreflightLoginMutationTarget reports an explicit or sole discovered target
// without loading its contents. The boolean is false when multiple layers need
// intent-aware ownership planning. Credential-accepting commands use this to
// reject an auto-discovered local file before parsing repository-controlled
// destinations or starting any authentication work.
func (opts *Options) PreflightLoginMutationTarget() (internalconfig.ConfigSource, bool, error) {
	if opts.ConfigFile != "" || os.Getenv(internalconfig.ConfigFileEnvVar) != "" {
		target, err := opts.resolveMutationConfigTarget()
		return target, true, err
	}

	sources, err := internalconfig.DiscoverSources()
	if err != nil {
		return internalconfig.ConfigSource{}, false, fmt.Errorf("discover config login target: %w", err)
	}
	switch len(sources) {
	case 0:
		path, err := internalconfig.StandardLocation()()
		if err != nil {
			return internalconfig.ConfigSource{}, false, err
		}
		return internalconfig.ConfigSource{Path: path, Type: "user"}, true, nil
	case 1:
		return sources[0], true, nil
	default:
		return internalconfig.ConfigSource{}, false, nil
	}
}

// PlanLoginMutation resolves an intent-aware raw write target for login. It
// deliberately does not weaken MutationConfigTarget: generic mutations remain
// ambiguous whenever layered discovery finds more than one source.
//
// Explicit --config/GCX_CONFIG selection is authoritative. Zero/one-source
// behavior is unchanged. With multiple sources, ownership is inferred only
// from an existing target context and its atomic stack/Cloud entries.
func (opts *Options) PlanLoginMutation(
	cfg internalconfig.Config,
	contextName string,
	intent internalconfig.LoginMutationIntent,
) (internalconfig.ConfigSource, error) {
	if opts.ConfigFile != "" || os.Getenv(internalconfig.ConfigFileEnvVar) != "" {
		return opts.cacheLoginMutationTarget(opts.resolveMutationConfigTarget())
	}

	var target internalconfig.ConfigSource
	var err error
	switch len(cfg.Sources) {
	case 0:
		var path string
		path, err = internalconfig.StandardLocation()()
		if err == nil {
			target = internalconfig.ConfigSource{Path: path, Type: "user"}
		}
	case 1:
		target = cfg.Sources[0]
	default:
		target, err = cfg.PlanLoginMutation(contextName, intent)
	}
	return opts.cacheLoginMutationTarget(target, err)
}

// LoginMutationContext preserves inferred discovery provenance. In
// particular, a sole auto-discovered local config remains "local" so the login
// command's fresh-credential policy still requires explicit selection.
func (opts *Options) LoginMutationContext(ctx context.Context, target internalconfig.ConfigSource) context.Context {
	if target.Type == "explicit" || target.Type == "" {
		return ctx
	}
	return internalconfig.ContextWithConfigSource(ctx, target)
}

func (opts *Options) cacheLoginMutationTarget(
	target internalconfig.ConfigSource,
	err error,
) (internalconfig.ConfigSource, error) {
	opts.mutationResolved = true
	opts.mutationTarget = target
	opts.mutationErr = err
	return target, err
}
