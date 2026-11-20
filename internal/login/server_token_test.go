package login_test

import (
	"context"
	"testing"

	"github.com/grafana/gcx/internal/login"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerLooksLikeToken(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"glsa_FAKE_example", true},
		{"  glsa_FAKE_example ", true},
		{"GLSA_FAKE", true},
		{"glc_FAKE", true},
		{"https://glsa_FAKE", true},
		{"http://glc_FAKE", true},
		{"https://my-stack.grafana.net", false},
		{"my-stack.grafana.net", false},
		{"my_host.internal", false},
		{"grafana_host:3000", false},
		{"localhost:3000", false},
		{"", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, login.ServerLooksLikeToken(tt.in), tt.in)
	}
}

func TestRedactServer(t *testing.T) {
	assert.Equal(t, "https://x.grafana.net", login.RedactServer("https://x.grafana.net"))
	assert.NotContains(t, login.RedactServer("glsa_SECRETVALUE"), "SECRET")
}

func TestRunRejectsTokenAsServerBeforeNetwork(t *testing.T) {
	detected := false
	opts := &login.Options{
		Server:       "glsa_SECRETVALUE",
		GrafanaToken: "t",
		Yes:          true,
		DetectFn: func(context.Context, string) (login.Target, error) {
			detected = true
			return login.TargetOnPrem, nil
		},
	}
	_, err := login.Run(context.Background(), opts)
	require.ErrorIs(t, err, login.ErrServerLooksLikeToken)
	assert.NotContains(t, err.Error(), "SECRETVALUE")
	assert.False(t, detected, "no detection/network call may happen")
}
