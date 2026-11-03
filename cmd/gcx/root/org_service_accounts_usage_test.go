package root_test

import (
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrgServiceAccountsUsageErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the gcx binary; skipped with -short")
	}
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"empty account name", []string{"service-accounts", "create", " "}, "name must not be empty"},
		{"invalid role", []string{"service-accounts", "create", "robot", "--role=invalid"}, "invalid role"},
		{"invalid account ID", []string{"service-accounts", "delete", "0"}, "invalid ID"},
		{"invalid token ID", []string{"service-accounts", "delete-token", "12", "0"}, "invalid ID"},
		{"empty token name", []string{"service-accounts", "create-token", "12", " ", "--output-file=token.txt"}, "token name must not be empty"},
		{"missing file", []string{"service-accounts", "create-token", "12", "name"}, "--output-file is required"},
		{"invalid TTL", []string{"service-accounts", "create-token", "12", "name", "--output-file=token.txt", "--ttl=1500ms"}, "invalid --ttl"},
		{"invalid target", []string{"service-accounts", "add-role", "0", "role-uid"}, "invalid ID"},
		{"empty role UID", []string{"service-accounts", "add-role", "12", " "}, "role UID must not be empty"},
		{"invalid output", []string{"service-accounts", "list", "--output=invalid"}, "invalid"},
		{"invalid account limit", []string{"service-accounts", "list", "--limit=-1"}, "limit"},
		{"invalid role limit", []string{"roles", "list", "--limit=-1"}, "limit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"org"}, tt.args...)
			stdout, code := runGcx(t, args...)
			require.Equal(t, gcxerrors.ExitUsageError, code, "%s", stdout)
			if tt.name == "invalid output" {
				// An explicit invalid format has no working codec for the error document.
				return
			}
			document, ok := assertOneJSONValue(t, stdout).(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "gcx.error", document["type"])
			detail, ok := document["error"].(map[string]any)
			require.True(t, ok)
			assert.EqualValues(t, gcxerrors.ExitUsageError, detail["exitCode"])
			assert.Contains(t, detail["details"], tt.want)
		})
	}
}
