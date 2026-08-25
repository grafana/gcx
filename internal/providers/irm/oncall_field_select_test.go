//nolint:testpackage // white-box test drives the private OnCall command builder
package irm

import (
	"bytes"
	"context"
	"strings"
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

func TestUsersListRejectsLeafNameAndSuggestsDeclaredPath(t *testing.T) {
	resetAgentMode(t)

	client := &fieldSelectOnCallAPI{users: []User{{
		PK:       "U123",
		Username: "ward",
		Email:    "ward@example.com",
	}}}
	cmd := newUsersCommand(&fakeLoader{client: client})
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"list", "--json", "username,email"})

	err := cmd.ExecuteContext(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown field(s) in --json: username, email")
	assert.Contains(t, err.Error(), "spec.username for username; spec.email for email")
}

func TestUsersListAcceptsDeclaredPaths(t *testing.T) {
	resetAgentMode(t)

	client := &fieldSelectOnCallAPI{users: []User{{
		PK:       "U123",
		Username: "ward",
		Email:    "ward@example.com",
	}}}
	cmd := newUsersCommand(&fakeLoader{client: client})
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"list", "--json", "spec.username,spec.email"})

	require.NoError(t, cmd.ExecuteContext(context.Background()))
	assert.JSONEq(t, `[{"spec.email":"ward@example.com","spec.username":"ward"}]`, strings.TrimSpace(stdout.String()))
}
