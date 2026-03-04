package config

import (
	"fmt"

	"github.com/caarlos0/env/v11"
)

// CLIOptions holds CLI-level configuration options that affect command behavior
// but are not specific to any Grafana context.
type CLIOptions struct {
	// AutoApprove automatically enables the --force flag on delete operations,
	// enabling non-interactive operation in CI/CD pipelines.
	AutoApprove bool `env:"GRAFANACTL_AUTO_APPROVE"`
}

// LoadCLIOptions loads CLI options from environment variables.
func LoadCLIOptions() (CLIOptions, error) {
	opts := CLIOptions{}
	if err := env.Parse(&opts); err != nil {
		return opts, fmt.Errorf("failed to parse CLI options: %w", err)
	}
	return opts, nil
}
