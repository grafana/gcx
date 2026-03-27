package config_test

import (
	"testing"

	"github.com/grafana/gcx/internal/config"
)

func TestLoadCLIOptions_AutoApproveTrue(t *testing.T) {
	t.Setenv("GCX_AUTO_APPROVE", "1")

	opts, err := config.LoadCLIOptions()
	if err != nil {
		t.Fatalf("LoadCLIOptions() error = %v, want nil", err)
	}

	if !opts.AutoApprove {
		t.Errorf("AutoApprove = %v, want true", opts.AutoApprove)
	}
}

func TestLoadCLIOptions_AutoApproveTrueString(t *testing.T) {
	t.Setenv("GCX_AUTO_APPROVE", "true")

	opts, err := config.LoadCLIOptions()
	if err != nil {
		t.Fatalf("LoadCLIOptions() error = %v, want nil", err)
	}

	if !opts.AutoApprove {
		t.Errorf("AutoApprove = %v, want true", opts.AutoApprove)
	}
}

func TestLoadCLIOptions_AutoApproveFalse(t *testing.T) {
	t.Setenv("GCX_AUTO_APPROVE", "0")

	opts, err := config.LoadCLIOptions()
	if err != nil {
		t.Fatalf("LoadCLIOptions() error = %v, want nil", err)
	}

	if opts.AutoApprove {
		t.Errorf("AutoApprove = %v, want false", opts.AutoApprove)
	}
}

func TestLoadCLIOptions_AutoApproveEmpty(t *testing.T) {
	t.Setenv("GCX_AUTO_APPROVE", "")

	opts, err := config.LoadCLIOptions()
	if err != nil {
		t.Fatalf("LoadCLIOptions() error = %v, want nil", err)
	}

	if opts.AutoApprove {
		t.Errorf("AutoApprove = %v, want false (default)", opts.AutoApprove)
	}
}
