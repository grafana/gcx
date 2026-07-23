package secrets_test

import (
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/secrets"
	"github.com/stretchr/testify/require"
)

type testStruct struct {
	Public      string
	Secret      string `datapolicy:"secret"`
	SecretBytes []byte `datapolicy:"secret"`
}

func TestRedact_withStruct(t *testing.T) {
	req := require.New(t)

	input := testStruct{Public: "public", Secret: "secret", SecretBytes: []byte("secret bytes")}

	err := secrets.Redact(&input)
	req.NoError(err)

	req.Equal("public", input.Public)
	req.Equal("**REDACTED**", input.Secret)
	req.Equal([]byte("**REDACTED**"), input.SecretBytes)
}

func TestRedact_withEmptySecret(t *testing.T) {
	req := require.New(t)

	input := testStruct{Public: "public", Secret: ""}

	err := secrets.Redact(&input)
	req.NoError(err)

	req.Equal("public", input.Public)
	req.Empty(input.Secret)
	req.Nil(input.SecretBytes)
}

func TestRedact_withMap(t *testing.T) {
	req := require.New(t)

	input := map[string]*testStruct{
		"foo": {Public: "public", Secret: "secret"},
	}

	err := secrets.Redact(&input)
	req.NoError(err)

	req.Equal("public", input["foo"].Public)
	req.Equal("**REDACTED**", input["foo"].Secret)
}

func TestRedact_withSlice(t *testing.T) {
	req := require.New(t)

	input := []*testStruct{
		{Public: "public", Secret: "secret"},
	}

	err := secrets.Redact(&input)
	req.NoError(err)

	req.Equal("public", input[0].Public)
	req.Equal("**REDACTED**", input[0].Secret)
}

// TestRedact_ExecEnvValues guards the datapolicy tag placement on
// ExecEnvVar.Value: the redactor recurses into slice elements with the secret
// flag cleared, so the tag must sit on the struct field, not the parent slice.
func TestRedact_ExecEnvValues(t *testing.T) {
	req := require.New(t)

	cfg := config.Config{
		Contexts: map[string]*config.Context{
			"behind-proxy": {
				Grafana: &config.GrafanaConfig{
					Server:     "https://grafana.example.net",
					AuthMethod: "exec",
					Exec: &config.ExecConfig{
						Command: "gcx-token-helper",
						Env: []config.ExecEnvVar{
							{Name: "AUDIENCE", Value: "grafana"},
							{Name: "CLIENT_SECRET", Value: "super-secret"},
						},
					},
				},
			},
		},
	}

	req.NoError(secrets.Redact(&cfg))

	env := cfg.Contexts["behind-proxy"].Grafana.Exec.Env
	req.Equal("AUDIENCE", env[0].Name, "env var names are not redacted")
	req.Equal("**REDACTED**", env[0].Value)
	req.Equal("CLIENT_SECRET", env[1].Name)
	req.Equal("**REDACTED**", env[1].Value)
	// The command itself is not a secret and stays visible.
	req.Equal("gcx-token-helper", cfg.Contexts["behind-proxy"].Grafana.Exec.Command)
}
