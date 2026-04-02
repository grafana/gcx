package providers_test

import (
	"testing"

	"github.com/grafana/gcx/cmd/gcx/providers"
	coreproviders "github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/spf13/cobra"
)

// mockProvider implements the providers.Provider interface for testing.
type mockProvider struct {
	name      string
	shortDesc string
}

func (m *mockProvider) Name() string                               { return m.name }
func (m *mockProvider) ShortDesc() string                          { return m.shortDesc }
func (m *mockProvider) Commands() []*cobra.Command                 { return nil }
func (m *mockProvider) Validate(_ map[string]string) error         { return nil }
func (m *mockProvider) ConfigKeys() []coreproviders.ConfigKey      { return nil }
func (m *mockProvider) TypedRegistrations() []adapter.Registration { return nil }

var _ coreproviders.Provider = (*mockProvider)(nil)

func Test_ProvidersCommand_NoProviders(t *testing.T) {
	testCase := testutils.CommandTestCase{
		Cmd:     providers.Command(nil),
		Command: []string{"list"},
		Assertions: []testutils.CommandAssertion{
			testutils.CommandSuccess(),
			testutils.CommandOutputContains("No providers registered"),
		},
	}

	testCase.Run(t)
}

func Test_ProvidersCommand_NilProvider(t *testing.T) {
	// A nil entry in the provider slice must not cause a panic and must be
	// silently skipped; the command should still succeed.
	testCase := testutils.CommandTestCase{
		Cmd:     providers.Command([]coreproviders.Provider{nil}),
		Command: []string{"list"},
		Assertions: []testutils.CommandAssertion{
			testutils.CommandSuccess(),
		},
	}

	testCase.Run(t)
}

func Test_ProvidersCommand(t *testing.T) {
	tests := []struct {
		name             string
		pp               []coreproviders.Provider
		expectedInOutput []string
	}{
		{
			name: "single provider",
			pp: []coreproviders.Provider{
				&mockProvider{name: "slo", shortDesc: "Manage SLOs"},
			},
			expectedInOutput: []string{"NAME", "slo", "Manage SLOs"},
		},
		{
			name: "multiple providers",
			pp: []coreproviders.Provider{
				&mockProvider{name: "slo", shortDesc: "Manage SLOs"},
				&mockProvider{name: "oncall", shortDesc: "Manage OnCall"},
			},
			expectedInOutput: []string{"NAME", "slo", "Manage SLOs", "oncall", "Manage OnCall"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertions := []testutils.CommandAssertion{testutils.CommandSuccess()}
			for _, expected := range tc.expectedInOutput {
				assertions = append(assertions, testutils.CommandOutputContains(expected))
			}

			testCase := testutils.CommandTestCase{
				Cmd:        providers.Command(tc.pp),
				Command:    []string{"list"},
				Assertions: assertions,
			}

			testCase.Run(t)
		})
	}
}
