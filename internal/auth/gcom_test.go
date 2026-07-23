package auth_test

import (
	"testing"

	"github.com/grafana/gcx/internal/auth"
	"github.com/stretchr/testify/assert"
)

// The `gcx login` cloud followup requests DefaultGCOMScopes directly, so this
// pins the full scope set gcx needs across all commands - not just stacks.
func TestDefaultGCOMScopes(t *testing.T) {
	want := []string{
		"stacks:read", "stacks:write", "stacks:delete",
		"metrics:write",
		"logs:write",
		"traces:write",
		"fleet-management:read", "fleet-management:write",
	}
	assert.Equal(t, want, auth.DefaultGCOMScopes())

	// Callers may mutate their copy (e.g. Cobra flag defaults); a fresh slice
	// must be returned each call.
	got := auth.DefaultGCOMScopes()
	got[0] = "mutated"
	assert.Equal(t, want, auth.DefaultGCOMScopes())
}
