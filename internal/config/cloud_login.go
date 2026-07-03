package config

import (
	"context"
	"errors"
	"os"

	"github.com/grafana/gcx/internal/gcxerrors"
)

// MergeCloudInto applies non-empty fields from incoming onto existing,
// allocating existing if nil. It returns the merged config.
func MergeCloudInto(existing, incoming *CloudConfig) *CloudConfig {
	if existing == nil {
		existing = &CloudConfig{}
	}
	if incoming.Token != "" {
		existing.Token = incoming.Token
	}
	if incoming.OAuthUrl != "" {
		existing.OAuthUrl = incoming.OAuthUrl
	}
	if incoming.APIUrl != "" {
		existing.APIUrl = incoming.APIUrl
	}
	if incoming.Stack != "" {
		existing.Stack = incoming.Stack
	}
	return existing
}

// ResolveContextName picks the context to operate on: the explicit override when
// set, otherwise the config's current context, falling back to the default.
func ResolveContextName(override string, cfg Config) string {
	if override != "" {
		return override
	}
	if cfg.CurrentContext != "" {
		return cfg.CurrentContext
	}
	return DefaultContextName
}

// SaveCloudConfig writes cloud credentials into the resolved context (see
// ResolveContextName), creating the context if it doesn't exist and preserving
// any existing Stack selection so re-authenticating doesn't drop it. It returns
// the name of the context that was written.
func SaveCloudConfig(ctx context.Context, source Source, contextOverride string, cloud *CloudConfig) (string, error) {
	cfg, err := Load(ctx, source)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", &gcxerrors.DetailedError{
			Summary: "Failed to load config",
			Parent:  err,
			Suggestions: []string{
				"Check your config file syntax: gcx config edit",
				"Or reset with: rm ~/.config/gcx/config.yaml && gcx cloud login",
			},
		}
	}
	if errors.Is(err, os.ErrNotExist) {
		cfg = Config{}
	}

	contextName := ResolveContextName(contextOverride, cfg)

	if !cfg.HasContext(contextName) {
		cfg.SetContext(contextName, true, Context{})
	}
	curCtx := cfg.Contexts[contextName]
	// Merge the incoming auth fields onto the existing cloud config so
	// re-authenticating refreshes credentials without dropping the non-auth
	// Stack selection.
	curCtx.Cloud = MergeCloudInto(curCtx.Cloud, cloud)

	if err := Write(ctx, source, cfg); err != nil {
		return "", &gcxerrors.DetailedError{
			Summary: "Failed to save config",
			Parent:  err,
			Suggestions: []string{
				"Check file permissions on the config file",
				"Try: gcx config edit",
			},
		}
	}

	return contextName, nil
}
