package errortracking_test

import (
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
)

const serviceAccount403 = `{"error":{"code":"forbidden","message":"current organization membership and sufficient role are required"}}`

func assertHumanOnly(t *testing.T, _ string, err error, _ []etCall) {
	t.Helper()
	out := rendered(t, err)
	assert.Contains(t, out, gcxerrors.SummaryAuthorizationFailed)
	assert.Equal(t, 1, strings.Count(out, "sufficient role are required"), "the server message is printed once: %s", out)
	assert.Regexp(t, `Ask an organization admin for the (Editor or Admin|Viewer, Editor or Admin) role in this organization`, out)
	assert.Contains(t, out, "Use your own login, not a service account token: gcx login")
	assert.Contains(t, out, "If GRAFANA_TOKEN is set in your environment it overrides the login: unset it for this command")
	assert.NotContains(t, out, "Sign in as yourself", "the role advice and the sign-in note read as one request")
}

func refusedBeforeRequest(what, read string) func(t *testing.T, _ string, err error, _ []etCall) {
	return func(t *testing.T, _ string, err error, _ []etCall) {
		t.Helper()
		out := rendered(t, err)
		assert.Contains(t, out, gcxerrors.SummaryAuthorizationFailed)
		assert.Contains(t, out, what+"; this context uses a service account token")
		assert.Regexp(t, `Sign in as yourself(, with the Editor or Admin role)?: gcx login`, out)
		assert.Contains(t, out, "As a service account you can still read "+read)
	}
}
