package setup_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/cmd/gcx/setup"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/setup/framework"
	"github.com/grafana/gcx/internal/setup/framework/testhelpers"
)

// setupProvider implements both providers.Provider and framework.Setupable for run tests.
type setupProvider struct {
	stubProvider

	categories []framework.InfraCategory
	setupErr   error // returned by Setup(); nil means success
}

func (p *setupProvider) ProductName() string { return p.name }
func (p *setupProvider) Status(_ context.Context) (*framework.ProductStatus, error) {
	return &framework.ProductStatus{Product: p.name, State: framework.StateNotConfigured}, nil
}
func (p *setupProvider) InfraCategories() []framework.InfraCategory { return p.categories }
func (p *setupProvider) ResolveChoices(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (p *setupProvider) ValidateSetup(_ context.Context, _ map[string]string) error { return nil }
func (p *setupProvider) Setup(_ context.Context, _ map[string]string) error         { return p.setupErr }

func TestRunCommand_AgentModeRefusal(t *testing.T) {
	agent.SetFlag(true)
	t.Cleanup(agent.ResetForTesting)

	cmd := setup.NewRunCommand(nil)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	err := cmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected error for agent mode, got nil")
	}

	var detailedErr *fail.DetailedError
	if !errors.As(err, &detailedErr) {
		t.Fatalf("expected *fail.DetailedError, got %T: %v", err, err)
	}
	if detailedErr.ExitCode == nil || *detailedErr.ExitCode != fail.ExitUsageError {
		t.Errorf("expected exit code %d, got %v", fail.ExitUsageError, detailedErr.ExitCode)
	}
	if !strings.Contains(stderr.String(), "not available in agent mode") {
		t.Errorf("expected stderr to contain 'not available in agent mode', got: %q", stderr.String())
	}
}

func TestRunCommand_ZeroCategories(t *testing.T) {
	agent.SetFlag(false)
	t.Cleanup(agent.ResetForTesting)

	setup.SetIsInteractiveForTest(func() bool { return true })
	t.Cleanup(func() { setup.SetIsInteractiveForTest(nil) })

	ps := []providers.Provider{
		&setupProvider{
			stubProvider: stubProvider{name: "nocat"},
			categories:   nil,
		},
	}
	testhelpers.SetupTestRegistry(t, ps)

	cmd := setup.NewRunCommand(nil)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	err := cmd.ExecuteContext(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stderr.String(), "No interactive setup flows available") {
		t.Errorf("expected stderr to contain 'No interactive setup flows available', got: %q", stderr.String())
	}
}

func TestRunCommand_NonTTY(t *testing.T) {
	agent.SetFlag(false)
	t.Cleanup(agent.ResetForTesting)

	// Ensure no TTY override from other tests.
	setup.SetIsInteractiveForTest(nil)

	cmd := setup.NewRunCommand(nil)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	err := cmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected error for non-TTY stdin, got nil")
	}

	var detailedErr *fail.DetailedError
	if !errors.As(err, &detailedErr) {
		t.Fatalf("expected *fail.DetailedError, got %T: %v", err, err)
	}
	if detailedErr.ExitCode == nil || *detailedErr.ExitCode != fail.ExitUsageError {
		t.Errorf("expected exit code %d, got %v", fail.ExitUsageError, detailedErr.ExitCode)
	}
}

func TestRunCommand_CancelledSummary(t *testing.T) {
	agent.SetFlag(false)
	t.Cleanup(agent.ResetForTesting)

	setup.SetIsInteractiveForTest(func() bool { return true })
	t.Cleanup(func() { setup.SetIsInteractiveForTest(nil) })

	ps := []providers.Provider{
		&setupProvider{
			stubProvider: stubProvider{name: "svc"},
			categories: []framework.InfraCategory{
				{
					ID:    "infra",
					Label: "Infrastructure",
					Params: []framework.SetupParam{
						{Name: "endpoint", Prompt: "Endpoint", Kind: framework.ParamKindText, Required: true},
					},
				},
			},
		},
	}
	testhelpers.SetupTestRegistry(t, ps)

	cmd := setup.NewRunCommand(nil)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	// Enter (select all categories) + param value + "n" (decline preview)
	cmd.SetIn(strings.NewReader("\nval\nn\n"))
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	err := cmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected error for cancelled setup, got nil")
	}

	var detailedErr *fail.DetailedError
	if !errors.As(err, &detailedErr) {
		t.Fatalf("expected *fail.DetailedError, got %T: %v", err, err)
	}
	if detailedErr.ExitCode == nil || *detailedErr.ExitCode != fail.ExitCancelled {
		t.Errorf("expected exit code %d, got %v", fail.ExitCancelled, detailedErr.ExitCode)
	}
}

func TestRunCommand_FailedExitCode(t *testing.T) {
	agent.SetFlag(false)
	t.Cleanup(agent.ResetForTesting)

	setup.SetIsInteractiveForTest(func() bool { return true })
	t.Cleanup(func() { setup.SetIsInteractiveForTest(nil) })

	ps := []providers.Provider{
		&setupProvider{
			stubProvider: stubProvider{name: "svc"},
			setupErr:     errors.New("boom"),
			categories: []framework.InfraCategory{
				{
					ID:    "infra",
					Label: "Infrastructure",
					Params: []framework.SetupParam{
						{Name: "endpoint", Prompt: "Endpoint", Kind: framework.ParamKindText, Required: true},
					},
				},
			},
		},
	}
	testhelpers.SetupTestRegistry(t, ps)

	cmd := setup.NewRunCommand(nil)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	// Enter (select all categories) + param value + "y" (confirm preview → triggers Setup())
	cmd.SetIn(strings.NewReader("\nval\ny\n"))
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	err := cmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected error when setup operation fails, got nil")
	}

	var detailedErr *fail.DetailedError
	if !errors.As(err, &detailedErr) {
		t.Fatalf("expected *fail.DetailedError, got %T: %v", err, err)
	}
	if detailedErr.ExitCode == nil || *detailedErr.ExitCode != fail.ExitPartialFailure {
		t.Errorf("expected exit code %d (ExitPartialFailure), got %v", fail.ExitPartialFailure, detailedErr.ExitCode)
	}
}
