package config_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	commandconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/credentials"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/require"
)

func runConfigCheck(t *testing.T, path string) (string, error) {
	t.Helper()
	cmd := commandconfig.Command()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	output := &bytes.Buffer{}
	cmd.SetOut(output)
	cmd.SetErr(output)
	cmd.SetArgs([]string{"check", "--config", path})
	err := cmd.Execute()
	return output.String(), err
}

func TestCheckCommandResolvesNonCurrentKeychainCredentialBeforeNetwork(t *testing.T) {
	for _, envKey := range []string{
		"GRAFANA_SERVER",
		"GRAFANA_USER",
		"GRAFANA_PASSWORD",
		"GRAFANA_TOKEN",
		"GRAFANA_PROXY_ENDPOINT",
		"GRAFANA_ORG_ID",
		"GRAFANA_STACK_ID",
		"GRAFANA_TLS_CERT_FILE",
		"GRAFANA_TLS_KEY_FILE",
		"GRAFANA_TLS_CA_FILE",
	} {
		t.Setenv(envKey, "")
	}
	t.Setenv("GCX_DISCOVERY_CACHE_DIR", t.TempDir())

	tests := []struct {
		name            string
		storedToken     string
		wantRequests    bool
		wantOutput      string
		wantTypedReject bool
	}{
		{
			name:         "existing entry resolves before connectivity",
			storedToken:  "resolved-non-current-token",
			wantRequests: true,
		},
		{
			name:            "missing entry is rejected before connectivity",
			wantOutput:      "the referenced keychain entry does not exist",
			wantTypedReject: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			var headerMu sync.Mutex
			var authorization string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				headerMu.Lock()
				authorization = r.Header.Get("Authorization")
				headerMu.Unlock()
				http.Error(w, `{"message":"unauthorized"}`, http.StatusUnauthorized)
			}))
			defer server.Close()

			path := writeYAML(t, "version: 1\ncontexts: {}\n")
			binding := testStackBinding(t, path, "non-current", server.URL, credentials.FieldGrafanaToken)
			reference, err := credentials.NewBoundReference(binding)
			require.NoError(t, err)

			contents := fmt.Sprintf(`version: 1
stacks:
  non-current:
    grafana:
      server: %q
      org-id: 1
      auth-method: token
      token: %q
contexts:
  current: {}
  non-current:
    stack: non-current
current-context: current
`, server.URL, reference.Sentinel)
			require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))

			store := withFakeStore(t)
			if tc.storedToken != "" {
				store.entries[reference.Account] = tc.storedToken
			}

			// Exercise the same deferred resolution and validation boundary
			// directly so the missing-entry assertion retains its concrete type;
			// config check is a diagnostic command and renders this error instead
			// of returning it from Cobra.
			loaded, err := (&commandconfig.Options{ConfigFile: path}).LoadConfigTolerant(t.Context())
			require.NoError(t, err)
			require.Equal(t, reference.Sentinel, loaded.Contexts["non-current"].Grafana.APIToken)
			loaded.ResolveContext("non-current")
			if tc.wantTypedReject {
				validationErr := loaded.Contexts["non-current"].Validate(t.Context())
				var rejected config.CredentialRejectedError
				require.ErrorAs(t, validationErr, &rejected)
				require.Equal(t, credentials.FieldGrafanaToken, rejected.Field)
				require.Zero(t, requests.Load(), "credential rejection must happen before an upstream request")
			} else {
				require.Equal(t, tc.storedToken, loaded.Contexts["non-current"].Grafana.APIToken)
				require.NoError(t, loaded.Contexts["non-current"].Validate(t.Context()))
			}

			output, err := runConfigCheck(t, path)
			require.ErrorIs(t, err, gcxerrors.ErrAlreadyReported)
			if tc.wantRequests {
				require.Positive(t, requests.Load())
				headerMu.Lock()
				gotAuthorization := authorization
				headerMu.Unlock()
				require.Equal(t, "Bearer "+tc.storedToken, gotAuthorization)
			} else {
				require.Zero(t, requests.Load(), "missing keychain entries must skip all connectivity requests")
				require.Contains(t, output, tc.wantOutput)
				require.Contains(t, output, "skipped")
			}
		})
	}
}
