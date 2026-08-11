package config

import "fmt"

// CLIOptions holds CLI-level configuration options that affect command behavior
// but are not specific to any Grafana context.
type CLIOptions struct {
	// AutoApprove automatically enables the --force flag on delete operations,
	// enabling non-interactive operation in CI/CD pipelines.
	AutoApprove bool `env:"GCX_AUTO_APPROVE"`

	// DisableUpdateNotifier disables the periodic notifier that reminds users
	// when their installed gcx skills can be updated. Any non-empty value
	// disables the notifier (NO_COLOR convention).
	DisableUpdateNotifier string `env:"GCX_NO_UPDATE_NOTIFIER"`

	// KGDatasourceUID routes all Knowledge Graph API traffic through the KG
	// datasource proxy (/api/datasources/proxy/uid/<uid>) instead of the
	// grafana-asserts-app plugin resource proxy. The datasource mirrors all of
	// the plugin's /resources routes. Set to "1" or "true" to use the default
	// provisioned UID (grafana-knowledgegraph-datasource), or to an explicit
	// datasource UID. Development escape hatch; unset means the default
	// plugin route.
	KGDatasourceUID string `env:"GCX_KG_DATASOURCE_UID"`
}

// LoadCLIOptions loads CLI options from environment variables.
func LoadCLIOptions() (CLIOptions, error) {
	opts := CLIOptions{}
	if err := parseEnvTags(&opts); err != nil {
		return opts, fmt.Errorf("failed to parse CLI options: %w", err)
	}
	return opts, nil
}
