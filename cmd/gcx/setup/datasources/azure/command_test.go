//nolint:testpackage // white-box tests exercise unexported option validation
package azure

import (
	"errors"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/spf13/pflag"
)

// newOpts builds an azureOpts with flags bound, mirroring command construction.
// Agent-mode env must be set before calling so BindFlags resolves the right
// default output codec.
func newOpts(t *testing.T) *azureOpts {
	t.Helper()
	opts := &azureOpts{}
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	opts.setup(fs)
	return opts
}

func TestValidate_AgentModeRequiresForce(t *testing.T) {
	t.Setenv("GCX_AGENT_MODE", "true")
	agent.ResetForTesting()
	t.Cleanup(agent.ResetForTesting)

	opts := newOpts(t)

	err := opts.Validate()
	if err == nil {
		t.Fatal("expected error in agent mode without --force")
	}
	var de gcxerrors.DetailedError
	if !errors.As(err, &de) {
		t.Fatalf("expected DetailedError, got %T", err)
	}
}

func TestValidate_AgentModeForceAllowed(t *testing.T) {
	t.Setenv("GCX_AGENT_MODE", "true")
	agent.ResetForTesting()
	t.Cleanup(agent.ResetForTesting)

	opts := newOpts(t)
	opts.Force = true
	if err := opts.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_AgentModeCleanupRequiresForce(t *testing.T) {
	t.Setenv("GCX_AGENT_MODE", "true")
	agent.ResetForTesting()
	t.Cleanup(agent.ResetForTesting)

	opts := newOpts(t)
	opts.Cleanup = true

	err := opts.Validate()
	if err == nil {
		t.Fatal("expected error for destructive cleanup in agent mode without --force")
	}
	var de gcxerrors.DetailedError
	if !errors.As(err, &de) {
		t.Fatalf("expected DetailedError, got %T", err)
	}
}

func TestValidate_AgentModeCleanupForceAllowed(t *testing.T) {
	t.Setenv("GCX_AGENT_MODE", "true")
	agent.ResetForTesting()
	t.Cleanup(agent.ResetForTesting)

	opts := newOpts(t)
	opts.Cleanup = true
	opts.Force = true
	if err := opts.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_AgentModeCleanupDryRunAllowedWithoutForce(t *testing.T) {
	t.Setenv("GCX_AGENT_MODE", "true")
	agent.ResetForTesting()
	t.Cleanup(agent.ResetForTesting)

	opts := newOpts(t)
	opts.Cleanup = true
	opts.DryRun = true
	if err := opts.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_AgentModeDryRunAllowedWithoutForce(t *testing.T) {
	t.Setenv("GCX_AGENT_MODE", "true")
	agent.ResetForTesting()
	t.Cleanup(agent.ResetForTesting)

	opts := newOpts(t)
	opts.DryRun = true
	if err := opts.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSplitRoles(t *testing.T) {
	got := splitRoles(" Reader , Monitoring Reader ,")
	want := []string{"Reader", "Monitoring Reader"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
