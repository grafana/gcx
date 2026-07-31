package config

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The complete label derivation, one row per state. The empty string is the
// only non-decision; everything else is a decided value the capture slot may
// record, and no raw configured string appears anywhere in the mapping.
func TestGrafanaAuthMethodLabel(t *testing.T) {
	grafana := &GrafanaConfig{}

	assert.Empty(t, grafanaAuthMethodLabel(nil, grafanaAuthSelection{}, nil),
		"no Grafana block means nothing was selected")
	assert.Equal(t, "unknown", grafanaAuthMethodLabel(grafana, grafanaAuthSelection{}, errors.New("unsupported auth-method")),
		"a selector error is a decision: the method could not be classified")
	assert.Equal(t, "anonymous", grafanaAuthMethodLabel(grafana, grafanaAuthSelection{mode: grafanaAuthUnknown}, nil),
		"a valid selection with no credential material goes out unauthenticated")

	for mode, want := range map[grafanaAuthMode]string{
		grafanaAuthOAuth: "oauth",
		grafanaAuthToken: "token",
		grafanaAuthBasic: "basic",
		grafanaAuthMTLS:  "mtls",
	} {
		assert.Equal(t, want, grafanaAuthMethodLabel(grafana, grafanaAuthSelection{mode: mode}, nil))
	}
}
