package config

import (
	"errors"
	"testing"

	"github.com/grafana/gcx/internal/telemetry"
	"github.com/stretchr/testify/assert"
)

// The complete label derivation, one row per state. The empty string is the
// only non-decision; everything else is a decided value the capture slot may
// record, and no raw configured string appears anywhere in the mapping.
func TestGrafanaAuthMethodLabel(t *testing.T) {
	grafana := &GrafanaConfig{Server: "https://grafana.example.invalid"}

	assert.Empty(t, grafanaAuthMethodLabel(nil, grafanaAuthSelection{}, nil),
		"no Grafana block means nothing was selected")
	assert.Empty(t, grafanaAuthMethodLabel(&GrafanaConfig{}, grafanaAuthSelection{mode: grafanaAuthUnknown}, nil),
		"a wholly empty block is the same non-decision: the tolerant load path installs one where there was none")
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

// This package derives labels the telemetry package clamps, and the two
// vocabularies are declared independently — the selector's String() values
// predate telemetry and cannot import it in production without inverting the
// dependency taste. This is the drift link: if either side renames a value,
// the clamp would silently turn every captured method into "unknown" while
// both packages' own tests stayed green. (Test-only import; production config
// still depends only on the capture leaf.)
func TestGrafanaAuthMethodLabelsAreInTheTelemetryVocabulary(t *testing.T) {
	allowed := make(map[string]bool)
	for _, label := range telemetry.GrafanaAuthMethodLabels() {
		allowed[label] = true
	}

	grafana := &GrafanaConfig{Server: "https://grafana.example.invalid"}
	labels := []string{
		grafanaAuthMethodLabel(grafana, grafanaAuthSelection{}, errors.New("unsupported")),
		grafanaAuthMethodLabel(grafana, grafanaAuthSelection{mode: grafanaAuthUnknown}, nil),
		grafanaAuthMethodLabel(grafana, grafanaAuthSelection{mode: grafanaAuthOAuth}, nil),
		grafanaAuthMethodLabel(grafana, grafanaAuthSelection{mode: grafanaAuthToken}, nil),
		grafanaAuthMethodLabel(grafana, grafanaAuthSelection{mode: grafanaAuthBasic}, nil),
		grafanaAuthMethodLabel(grafana, grafanaAuthSelection{mode: grafanaAuthMTLS}, nil),
	}
	for _, label := range labels {
		assert.True(t, allowed[label],
			"label %q is not in telemetry.GrafanaAuthMethodLabels(): the wire clamp would report it as unknown", label)
	}
}
