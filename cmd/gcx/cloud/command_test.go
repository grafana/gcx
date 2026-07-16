//nolint:testpackage // white-box: loginCmd is unexported
package cloud

import (
	"testing"

	"github.com/grafana/gcx/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// `gcx cloud login` and the `gcx login` cloud followup must request the same
// scope set (auth.DefaultGCOMScopes) so tokens from either path are equivalent.
func TestLoginScopeFlagDefaultMatchesDefaultGCOMScopes(t *testing.T) {
	scopes, err := loginCmd().Flags().GetStringSlice("scope")
	require.NoError(t, err)
	assert.Equal(t, auth.DefaultGCOMScopes(), scopes)
}
