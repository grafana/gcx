package cloud_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/cloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListOrgs(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		want       []cloud.OrgMembership
		fail       bool
	}{
		{"memberships", `[{"login":"example-org","role":"Admin"},{"login":"another-org","role":"Viewer"}]`, 200, []cloud.OrgMembership{{Slug: "example-org", Role: "Admin"}, {Slug: "another-org", Role: "Viewer"}}, false},
		{"empty", `[]`, 200, []cloud.OrgMembership{}, false},
		{"no user", `null`, 200, nil, true},
		{"invalid JSON", `broken`, 200, nil, true},
		{"missing slug", `[{"role":"Admin"}]`, 200, nil, true},
		{"unauthorized", `{}`, 401, nil, true},
		{"forbidden", `{}`, 403, nil, true},
		{"server error", `{}`, 500, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/api/oauth2/user/orgs", r.URL.Path)
				assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			client, err := cloud.NewGCOMClient(srv.URL, "test-token")
			require.NoError(t, err)
			got, err := client.ListOrgs(context.Background())
			if tc.fail {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}
