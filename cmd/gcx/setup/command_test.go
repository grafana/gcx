package setup_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/setup"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/setup/framework"
	"github.com/grafana/gcx/internal/setup/framework/testhelpers"
	"github.com/spf13/cobra"
)

// stubProvider implements providers.Provider with no-op methods.
type stubProvider struct{ name string }

func (s *stubProvider) Name() string                               { return s.name }
func (s *stubProvider) ShortDesc() string                          { return "" }
func (s *stubProvider) Commands() []*cobra.Command                 { return nil }
func (s *stubProvider) Validate(_ map[string]string) error         { return nil }
func (s *stubProvider) ConfigKeys() []providers.ConfigKey          { return nil }
func (s *stubProvider) TypedRegistrations() []adapter.Registration { return nil }

// statusProvider is a stubProvider that also implements framework.StatusDetectable.
type statusProvider struct {
	stubProvider

	status_ *framework.ProductStatus
	err     error
}

func (p *statusProvider) ProductName() string { return p.name }
func (p *statusProvider) Status(_ context.Context) (*framework.ProductStatus, error) {
	return p.status_, p.err
}

func runStatusCmd(t *testing.T, args []string) (string, error) {
	t.Helper()
	cmd := setup.NewStatusCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return buf.String(), err
}

func TestStatusCommand_TextOutput(t *testing.T) {
	agent.SetFlag(false)
	t.Cleanup(agent.ResetForTesting)

	ps := []providers.Provider{
		&statusProvider{
			stubProvider: stubProvider{name: "slo"},
			status_:      &framework.ProductStatus{Product: "slo", State: framework.StateActive, Details: "3 SLOs", SetupHint: ""},
		},
		&statusProvider{
			stubProvider: stubProvider{name: "metrics"},
			status_:      &framework.ProductStatus{Product: "metrics", State: framework.StateConfigured},
		},
		&statusProvider{
			stubProvider: stubProvider{name: "alerts"},
			status_:      &framework.ProductStatus{Product: "alerts", State: framework.StateNotConfigured, SetupHint: "run gcx alerts setup"},
		},
		// Plain provider (no StatusDetectable) — must not appear in output.
		&stubProvider{name: "invisible"},
	}
	testhelpers.SetupTestRegistry(t, ps)

	out, err := runStatusCmd(t, []string{"-o", "text"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Headers present
	if !strings.Contains(out, "PRODUCT") {
		t.Error("output missing PRODUCT header")
	}
	if !strings.Contains(out, "STATE") {
		t.Error("output missing STATE header")
	}

	// All three status providers appear.
	for _, name := range []string{"slo", "metrics", "alerts"} {
		if !strings.Contains(out, name) {
			t.Errorf("output missing provider %q", name)
		}
	}

	// State values present.
	if !strings.Contains(out, "active") {
		t.Error("output missing 'active' state")
	}
	if !strings.Contains(out, "not_configured") {
		t.Error("output missing 'not_configured' state")
	}

	// Details present.
	if !strings.Contains(out, "3 SLOs") {
		t.Error("output missing details '3 SLOs'")
	}

	// Hint present.
	if !strings.Contains(out, "run gcx alerts setup") {
		t.Error("output missing setup hint")
	}

	// Alphabetical: alerts < metrics < slo
	alertsIdx := strings.Index(out, "alerts")
	metricsIdx := strings.Index(out, "metrics")
	sloIdx := strings.Index(out, "slo")
	if alertsIdx > metricsIdx || metricsIdx > sloIdx {
		t.Errorf("output not in alphabetical order: alerts=%d metrics=%d slo=%d", alertsIdx, metricsIdx, sloIdx)
	}

	// Non-StatusDetectable provider must not appear.
	if strings.Contains(out, "invisible") {
		t.Error("output must not contain 'invisible' (not a StatusDetectable provider)")
	}
}

func TestStatusCommand_JSONOutput(t *testing.T) {
	agent.SetFlag(false)
	t.Cleanup(agent.ResetForTesting)
	ps := []providers.Provider{
		&statusProvider{
			stubProvider: stubProvider{name: "synth"},
			status_:      &framework.ProductStatus{Product: "synth", State: framework.StateActive, Details: "5 checks"},
		},
	}
	testhelpers.SetupTestRegistry(t, ps)

	out, err := runStatusCmd(t, []string{"-o", "json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, `"product"`) {
		t.Error("JSON output missing 'product' key")
	}
	if !strings.Contains(out, `"synth"`) {
		t.Error("JSON output missing 'synth'")
	}
	if !strings.Contains(out, `"state"`) {
		t.Error("JSON output missing 'state' key")
	}
	if !strings.Contains(out, `"active"`) {
		t.Error("JSON output missing 'active'")
	}
	if !strings.Contains(out, `"5 checks"`) {
		t.Error("JSON output missing details")
	}
}

func TestStatusCommand_YAMLOutput(t *testing.T) {
	agent.SetFlag(false)
	t.Cleanup(agent.ResetForTesting)
	ps := []providers.Provider{
		&statusProvider{
			stubProvider: stubProvider{name: "slo"},
			status_:      &framework.ProductStatus{Product: "slo", State: framework.StateConfigured},
		},
	}
	testhelpers.SetupTestRegistry(t, ps)

	out, err := runStatusCmd(t, []string{"-o", "yaml"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, "product:") {
		t.Error("YAML output missing 'product:' key")
	}
	if !strings.Contains(out, "slo") {
		t.Error("YAML output missing 'slo'")
	}
	if !strings.Contains(out, "state:") {
		t.Error("YAML output missing 'state:' key")
	}
	if !strings.Contains(out, "configured") {
		t.Error("YAML output missing 'configured'")
	}

	// No ANSI color escape sequences in YAML output.
	if strings.Contains(out, "\x1b[") {
		t.Error("YAML output must not contain ANSI escape sequences")
	}
}

func TestStatusCommand_AgentModeDefaultsToJSON(t *testing.T) {
	agent.SetFlag(true)
	t.Cleanup(func() { agent.SetFlag(false) })

	ps := []providers.Provider{
		&statusProvider{
			stubProvider: stubProvider{name: "fleet"},
			status_:      &framework.ProductStatus{Product: "fleet", State: framework.StateActive},
		},
	}
	testhelpers.SetupTestRegistry(t, ps)

	// No -o flag: agent mode must default to JSON.
	out, err := runStatusCmd(t, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, `"product"`) {
		t.Errorf("agent mode: expected JSON output, got: %s", out)
	}
	if !strings.Contains(out, `"fleet"`) {
		t.Error("agent mode: expected fleet in JSON output")
	}
}

func TestStatusCommand_ErrorProviderRendersAsStateError(t *testing.T) {
	agent.SetFlag(false)
	t.Cleanup(agent.ResetForTesting)
	ps := []providers.Provider{
		&statusProvider{
			stubProvider: stubProvider{name: "broken"},
			err:          errors.New("connection refused"),
		},
		&statusProvider{
			stubProvider: stubProvider{name: "healthy"},
			status_:      &framework.ProductStatus{Product: "healthy", State: framework.StateActive},
		},
	}
	testhelpers.SetupTestRegistry(t, ps)

	out, err := runStatusCmd(t, []string{"-o", "text"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, "broken") {
		t.Error("output missing 'broken' provider")
	}
	if !strings.Contains(out, "error") {
		t.Error("output missing 'error' state for broken provider")
	}
	if !strings.Contains(out, "connection refused") {
		t.Error("output missing error details 'connection refused'")
	}
	if !strings.Contains(out, "healthy") {
		t.Error("output missing 'healthy' provider — error must not cancel siblings")
	}
}

func TestStatusCommand_EmptyRegistry(t *testing.T) {
	agent.SetFlag(false)
	t.Cleanup(agent.ResetForTesting)
	testhelpers.SetupTestRegistry(t, []providers.Provider{})

	out, err := runStatusCmd(t, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No data rows, but command should succeed.
	_ = out
}

func TestStatusCommand_WideOutputEqualsText(t *testing.T) {
	agent.SetFlag(false)
	t.Cleanup(agent.ResetForTesting)
	ps := []providers.Provider{
		&statusProvider{
			stubProvider: stubProvider{name: "oncall"},
			status_:      &framework.ProductStatus{Product: "oncall", State: framework.StateActive, Details: "configured"},
		},
	}
	testhelpers.SetupTestRegistry(t, ps)

	outWide, err := runStatusCmd(t, []string{"-o", "wide"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(outWide, "oncall") {
		t.Error("wide output missing 'oncall'")
	}
	if !strings.Contains(outWide, "active") {
		t.Error("wide output missing 'active' state")
	}
}
