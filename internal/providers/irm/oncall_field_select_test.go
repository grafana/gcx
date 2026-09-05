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

	for _, tc := range []struct{ fields, wantErr string }{
		{"username,email", "spec.username for username; spec.email for email"},
		{"spec.username,spec.email", ""},
	} {
		t.Run(tc.fields, func(t *testing.T) {
			cmd := newUsersCommand(&fakeLoader{client: client})
			var stdout bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs([]string{"list", "--json", tc.fields})

			err := cmd.ExecuteContext(context.Background())
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.JSONEq(t, `[{"spec.email":"ward@example.com","spec.username":"ward"}]`, stdout.String())
		})
	}
}
