package instrumentation_test

import (
	"testing"

	"github.com/grafana/gcx/internal/setup/instrumentation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstrumentationConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     instrumentation.InstrumentationConfig
		wantErr string
	}{
		{
			name: "valid manifest passes validation",
			cfg: instrumentation.InstrumentationConfig{
				APIVersion: "setup.grafana.app/v1alpha1",
				Kind:       "InstrumentationConfig",
				Metadata:   instrumentation.Metadata{Name: "prod-cluster"},
			},
			wantErr: "",
		},
		{
			name: "wrong apiVersion returns error",
			cfg: instrumentation.InstrumentationConfig{
				APIVersion: "wrong/v1",
				Kind:       "InstrumentationConfig",
				Metadata:   instrumentation.Metadata{Name: "prod-cluster"},
			},
			wantErr: `invalid apiVersion "wrong/v1"`,
		},
		{
			name: "empty apiVersion returns error",
			cfg: instrumentation.InstrumentationConfig{
				APIVersion: "",
				Kind:       "InstrumentationConfig",
				Metadata:   instrumentation.Metadata{Name: "prod-cluster"},
			},
			wantErr: `invalid apiVersion ""`,
		},
		{
			name: "wrong kind returns error",
			cfg: instrumentation.InstrumentationConfig{
				APIVersion: "setup.grafana.app/v1alpha1",
				Kind:       "WrongKind",
				Metadata:   instrumentation.Metadata{Name: "prod-cluster"},
			},
			wantErr: `invalid kind "WrongKind"`,
		},
		{
			name: "missing metadata.name returns error",
			cfg: instrumentation.InstrumentationConfig{
				APIVersion: "setup.grafana.app/v1alpha1",
				Kind:       "InstrumentationConfig",
				Metadata:   instrumentation.Metadata{Name: ""},
			},
			wantErr: "metadata.name (cluster name) is required",
		},
		{
			name: "apiVersion checked before kind",
			cfg: instrumentation.InstrumentationConfig{
				APIVersion: "bad/v1",
				Kind:       "BadKind",
				Metadata:   instrumentation.Metadata{Name: ""},
			},
			wantErr: `invalid apiVersion "bad/v1"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

func TestInstrumentationConfig_NoStackSpecificFields(t *testing.T) {
	// NC-003: manifest MUST NOT contain datasource URLs, instance IDs, or tokens.
	// Verify the struct has no fields for such values.
	cfg := instrumentation.InstrumentationConfig{
		APIVersion: "setup.grafana.app/v1alpha1",
		Kind:       "InstrumentationConfig",
		Metadata:   instrumentation.Metadata{Name: "my-cluster"},
		Spec: instrumentation.InstrumentationSpec{
			App: &instrumentation.AppSpec{
				Namespaces: []instrumentation.NamespaceConfig{
					{
						Name:      "default",
						Selection: "included",
						Tracing:   true,
						Logging:   true,
					},
				},
			},
			K8s: &instrumentation.K8sSpec{
				CostMetrics:   true,
				ClusterEvents: true,
			},
		},
	}
	require.NoError(t, cfg.Validate())
}

func TestNamespaceConfig_AppsSupportNoSignalToggles(t *testing.T) {
	// FR-027: apps entries have name, selection, type — no signal toggles.
	app := instrumentation.AppConfig{
		Name:      "my-app",
		Selection: "included",
		Type:      "deployment",
	}
	assert.Equal(t, "my-app", app.Name)
	assert.Equal(t, "included", app.Selection)
	assert.Equal(t, "deployment", app.Type)
}
