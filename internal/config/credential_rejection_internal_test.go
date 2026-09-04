package config

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContextValidateRejectsCredentialBeforeNamespaceDiscovery(t *testing.T) {
	stack := &StackConfig{
		Name:           "prod",
		sourceIdentity: "/tmp/config.yaml",
		Grafana: &GrafanaConfig{
			Server:     "https://example.invalid",
			AuthMethod: "token",
			TLS: &TLS{
				CAFile: filepath.Join(t.TempDir(), "must-not-be-read.pem"),
			},
		},
	}
	stack.rejectCredential(credentials.FieldGrafanaToken, "the referenced keychain entry does not exist")
	ctx := &Context{Name: "prod", Stack: "prod", StackEntry: stack, Grafana: stack.Grafana}

	err := ctx.Validate(t.Context())
	require.Error(t, err)
	var rejected CredentialRejectedError
	require.ErrorAs(t, err, &rejected)
}

func TestMTLSRequiresCertificateAndKeyBeforeRESTConfigConstruction(t *testing.T) {
	tests := map[string]*TLS{
		"certificate data only": {CertData: []byte("certificate")},
		"private key data only": {KeyData: []byte("private-key")},
		"certificate file only": {CertFile: "/must-not-be-read/client.pem"},
		"private key file only": {KeyFile: "/must-not-be-read/client-key.pem"},
	}

	for name, tlsConfig := range tests {
		t.Run(name, func(t *testing.T) {
			ctx := &Context{
				Name: "prod",
				Grafana: &GrafanaConfig{
					Server:     "https://example.invalid",
					AuthMethod: "mtls",
					TLS:        tlsConfig,
				},
			}

			_, err := ctx.ToRESTConfig(t.Context())
			require.Error(t, err)
			var validation ValidationError
			require.ErrorAs(t, err, &validation)
			assert.Contains(t, err.Error(), "requires both a TLS client certificate and private key")
		})
	}
}

func TestEmptyCredentialEnvironmentDoesNotEraseOrRebindStoredCredential(t *testing.T) {
	for name, value := range map[string]string{
		"empty":      "",
		"whitespace": " \t\n ",
	} {
		t.Run(name, func(t *testing.T) {
			t.Run("unchanged destination keeps stored credential", func(t *testing.T) {
				store := newBoundTestStore()
				useBoundTestStore(t, store)
				path := filepath.Join(t.TempDir(), "config.yaml")
				require.NoError(t, Write(context.Background(), ExplicitConfigFile(path),
					boundStackTestConfig("https://original.invalid", "stored-token")))
				t.Setenv("GRAFANA_TOKEN", value)

				loaded, err := Load(context.Background(), ExplicitConfigFile(path), func(cfg *Config) error {
					return ParseEnvIntoContext(cfg.Contexts[cfg.CurrentContext])
				})
				require.NoError(t, err)
				assert.Equal(t, "stored-token", loaded.Contexts["default"].Grafana.APIToken)
				require.NoError(t, loaded.Contexts["default"].GrafanaCredentialRejection())
			})

			t.Run("destination change remains rejected", func(t *testing.T) {
				store := newBoundTestStore()
				useBoundTestStore(t, store)
				path := filepath.Join(t.TempDir(), "config.yaml")
				require.NoError(t, Write(context.Background(), ExplicitConfigFile(path),
					boundStackTestConfig("https://original.invalid", "stored-token")))
				t.Setenv("GRAFANA_SERVER", "https://override.invalid")
				t.Setenv("GRAFANA_TOKEN", value)

				loaded, err := Load(context.Background(), ExplicitConfigFile(path), func(cfg *Config) error {
					return ParseEnvIntoContext(cfg.Contexts[cfg.CurrentContext])
				})
				require.NoError(t, err)
				assert.Empty(t, loaded.Contexts["default"].Grafana.APIToken)
				err = loaded.Contexts["default"].GrafanaCredentialRejection()
				require.Error(t, err)
				var rejected CredentialRejectedError
				require.ErrorAs(t, err, &rejected)
				assert.Equal(t, credentials.FieldGrafanaToken, rejected.Field)
			})
		})
	}
}

func TestGrafanaCredentialRejectionFailsBeforeRESTConfigConstruction(t *testing.T) {
	stack := &StackConfig{
		Name:           "prod",
		sourceIdentity: "/tmp/config.yaml",
		Grafana: &GrafanaConfig{
			Server:     "https://example.invalid",
			AuthMethod: "token",
		},
	}
	stack.rejectCredential(credentials.FieldGrafanaToken, "the keychain reference belongs to another config source")
	ctx := &Context{Name: "prod", Stack: "prod", StackEntry: stack, Grafana: stack.Grafana}

	_, err := ctx.ToRESTConfig(t.Context())
	require.Error(t, err)
	var rejected CredentialRejectedError
	require.ErrorAs(t, err, &rejected)
	assert.Equal(t, credentials.FieldGrafanaToken, rejected.Field)
	assert.Contains(t, err.Error(), "before network use")
	assert.Contains(t, err.Error(), `--config "/tmp/config.yaml"`)
}

func TestRejectedGrafanaTokenCannotFallThroughToEmptyBasicAuth(t *testing.T) {
	stack := &StackConfig{
		Name:           "prod",
		sourceIdentity: "/tmp/config.yaml",
		Grafana: &GrafanaConfig{
			Server: "https://example.invalid",
			User:   "admin",
		},
	}
	stack.rejectCredential(credentials.FieldGrafanaToken, "the keychain reference belongs to another config source")
	ctx := &Context{Name: "prod", Stack: "prod", StackEntry: stack, Grafana: stack.Grafana}

	_, err := ctx.ToRESTConfig(t.Context())
	require.Error(t, err)
	var rejected CredentialRejectedError
	require.ErrorAs(t, err, &rejected)
	assert.Equal(t, credentials.FieldGrafanaToken, rejected.Field)
}

func TestLegacyAuthRejectedHigherPriorityCredentialCannotDowngrade(t *testing.T) {
	tests := map[string]struct {
		grafana       GrafanaConfig
		rejectedField credentials.Field
	}{
		"rejected OAuth cannot downgrade to token or Basic": {
			grafana: GrafanaConfig{
				APIToken: "valid-service-account-token",
				User:     "admin",
				Password: "valid-basic-password",
			},
			rejectedField: credentials.FieldOAuthToken,
		},
		"rejected token cannot downgrade to Basic": {
			grafana: GrafanaConfig{
				User:     "admin",
				Password: "valid-basic-password",
			},
			rejectedField: credentials.FieldGrafanaToken,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			test.grafana.Server = "https://must-not-be-contacted.invalid"
			test.grafana.TLS = &TLS{CAFile: filepath.Join(t.TempDir(), "must-not-be-read.pem")}
			stack := &StackConfig{
				Name:                 "prod",
				sourceIdentity:       "/tmp/config.yaml",
				Grafana:              &test.grafana,
				credentialRejections: map[credentials.Field]CredentialRejectedError{},
			}
			stack.rejectCredential(test.rejectedField, "the configured higher-priority credential was rejected")
			ctx := &Context{Name: "prod", Stack: "prod", StackEntry: stack, Grafana: stack.Grafana}

			_, err := ctx.ToRESTConfig(t.Context())
			require.Error(t, err)
			var rejected CredentialRejectedError
			require.ErrorAs(t, err, &rejected)
			assert.Equal(t, test.rejectedField, rejected.Field)
		})
	}
}

func TestExplicitAuthMethodIgnoresUnrelatedCredentialRejections(t *testing.T) {
	tests := map[string]struct {
		grafana        GrafanaConfig
		rejectedFields []credentials.Field
		assertConfig   func(*testing.T, NamespacedRESTConfig)
	}{
		"explicit Basic ignores OAuth and token rejection": {
			grafana: GrafanaConfig{
				AuthMethod: "basic",
				User:       "admin",
				Password:   "valid-basic-password",
			},
			rejectedFields: []credentials.Field{
				credentials.FieldOAuthToken,
				credentials.FieldOAuthRefreshToken,
				credentials.FieldGrafanaToken,
			},
			assertConfig: func(t *testing.T, cfg NamespacedRESTConfig) {
				t.Helper()
				assert.Equal(t, "admin", cfg.Username)
				assert.Equal(t, "valid-basic-password", cfg.Password)
				assert.Empty(t, cfg.BearerToken)
			},
		},
		"explicit token ignores OAuth and Basic rejection": {
			grafana: GrafanaConfig{
				AuthMethod: "token",
				APIToken:   "valid-service-account-token",
			},
			rejectedFields: []credentials.Field{
				credentials.FieldOAuthToken,
				credentials.FieldOAuthRefreshToken,
				credentials.FieldGrafanaPassword,
			},
			assertConfig: func(t *testing.T, cfg NamespacedRESTConfig) {
				t.Helper()
				assert.Equal(t, "valid-service-account-token", cfg.BearerToken)
				assert.Empty(t, cfg.Username)
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			test.grafana.Server = "https://example.invalid"
			test.grafana.StackID = 12345
			stack := &StackConfig{
				Name:           "prod",
				sourceIdentity: "/tmp/config.yaml",
				Grafana:        &test.grafana,
			}
			for _, field := range test.rejectedFields {
				stack.rejectCredential(field, "unrelated credential rejection")
			}
			ctx := &Context{Name: "prod", Stack: "prod", StackEntry: stack, Grafana: stack.Grafana}

			restConfig, err := ctx.ToRESTConfig(t.Context())
			require.NoError(t, err)
			test.assertConfig(t, restConfig)
		})
	}
}

func TestCloudCredentialRejectionDoesNotFallThroughToAlternateAuth(t *testing.T) {
	entry := &CloudEntry{
		Name:           "grafana-com",
		sourceIdentity: "/tmp/config.yaml",
		OAuthToken:     "oauth-token-that-must-not-mask-a-rejected-cap",
	}
	entry.rejectCredential(credentials.FieldCloudToken, "repository environment credentials are not trusted")

	token, err := entry.ResolveToken()
	assert.Empty(t, token)
	require.Error(t, err)
	var rejected CredentialRejectedError
	require.ErrorAs(t, err, &rejected)
	assert.Equal(t, credentials.FieldCloudToken, rejected.Field)
}

func TestFreshCredentialClearsRuntimeRejection(t *testing.T) {
	stack := &StackConfig{
		Name:           "prod",
		sourceIdentity: "/tmp/config.yaml",
		Grafana: &GrafanaConfig{
			Server:     "https://example.invalid",
			AuthMethod: "token",
			APIToken:   "fresh-token",
		},
	}
	stack.rejectCredential(credentials.FieldGrafanaToken, "old reference was rejected")
	stack.clearCredentialRejection(credentials.FieldGrafanaToken)
	ctx := &Context{Name: "prod", Stack: "prod", StackEntry: stack, Grafana: stack.Grafana}

	restConfig, err := ctx.ToRESTConfig(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "fresh-token", restConfig.BearerToken)
}

// A locked keychain is a condition that the user can correct. The rejection
// reason must name it, so the user does not read it as a permanent failure.
func TestKeychainReadRejectionReasonNamesTheLockedKeychain(t *testing.T) {
	tests := map[string]struct {
		err  error
		want string
	}{
		"locked keychain": {
			err:  fmt.Errorf("%w: failed to unlock correct collection '/org/freedesktop/secrets/collection/login'", credentials.ErrLocked),
			want: "the OS keychain is locked",
		},
		"unavailable keychain": {
			err:  fmt.Errorf("%w: dbus: connection closed", credentials.ErrUnavailable),
			want: "the OS keychain could not be read",
		},
		"unknown failure": {
			err:  errors.New("some other keychain failure"),
			want: "the OS keychain could not be read",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, test.want, keychainReadRejectionReason(test.err))
		})
	}
}

func TestKeychainReadRejectionReasonNamesTheSourceNeutralOptOut(t *testing.T) {
	assert.Equal(t, "keychain use is disabled by configuration", keychainReadRejectionReason(credentials.ErrDisabled))
	assert.Equal(t, "the OS keychain is locked", keychainReadRejectionReason(credentials.ErrLocked),
		"ErrDisabled wraps ErrUnavailable, so its branch must not shadow the locked one")
	assert.Equal(t, "the OS keychain could not be read", keychainReadRejectionReason(credentials.ErrUnavailable))
}

func TestLockedKeychainReadPreservesCauseInCredentialRejection(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, Write(t.Context(), ExplicitConfigFile(path),
		boundStackTestConfig("https://example.invalid", "stored-token")))

	lockedErr := fmt.Errorf("%w: exit status 154", credentials.ErrLocked)
	store.getErr = lockedErr
	loaded, err := Load(t.Context(), ExplicitConfigFile(path))
	require.NoError(t, err, "config inspection remains available while the keychain is locked")

	rejectionErr := loaded.Contexts["default"].GrafanaCredentialRejection()
	require.Error(t, rejectionErr)
	require.ErrorIs(t, rejectionErr, credentials.ErrLocked)
	require.ErrorIs(t, rejectionErr, lockedErr)
	var rejected CredentialRejectedError
	require.ErrorAs(t, rejectionErr, &rejected)
	assert.Equal(t, "the OS keychain is locked", rejected.Reason)
	assert.Contains(t, rejected.Error(), "unlock the keychain in this session")
	assert.NotContains(t, rejected.Error(), "re-authenticate")
}
