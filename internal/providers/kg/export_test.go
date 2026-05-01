package kg

import (
	"context"

	"github.com/spf13/cobra"
)

// ScopeFlags is an exported alias for scopeFlags, used only in tests.
type ScopeFlags = scopeFlags

// NewTestScopeFlags constructs a ScopeFlags for use in tests.
func NewTestScopeFlags(env, site, namespace string) ScopeFlags {
	return ScopeFlags{env: env, site: site, namespace: namespace}
}

// ValidateScopes wraps the unexported validateScopes method for testing.
func (f ScopeFlags) ValidateScopes(ctx context.Context, c *Client) error {
	return f.validateScopes(ctx, c)
}

// NewTestSuppressionsCommand exposes the suppressions command for black-box command tests.
func NewTestSuppressionsCommand(loader RESTConfigLoader) *cobra.Command {
	return newSuppressionsCommand(loader)
}
