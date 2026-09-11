//nolint:testpackage // white-box test drives the private OnCall command builder
package irm

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fieldSelectOnCallAPI struct {
	OnCallAPI

	users []User
}

func (f *fieldSelectOnCallAPI) ListUsers(context.Context) ([]User, error) {
	return f.users, nil
}

func TestUsersListFieldSelection(t *testing.T) {
	resetAgentMode(t)

	client := &fieldSelectOnCallAPI{users: []User{{
		PK:       "U123",
		Username: "ward",
		Email:    "ward@example.com",
	}}}

	for _, tc := range []struct{ fields, wantWarning string }{
		{"username,email", "spec.username for username; spec.email for email"},
		{"spec.username,spec.email", ""},
	} {
		t.Run(tc.fields, func(t *testing.T) {
			cmd := newUsersCommand(&fakeLoader{client: client})
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetArgs([]string{"list", "--json", tc.fields})

			err := cmd.ExecuteContext(context.Background())
			require.NoError(t, err)
			if tc.wantWarning != "" {
				assert.Contains(t, stderr.String(), tc.wantWarning)
				assert.JSONEq(t, `[{"email":null,"username":null}]`, stdout.String())
				return
			}
			assert.Empty(t, stderr.String())
			assert.JSONEq(t, `[{"spec.email":"ward@example.com","spec.username":"ward"}]`, stdout.String())
		})
	}
}
