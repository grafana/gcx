package slo

import (
	"context"
	"fmt"

	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/slo/definitions"
	"github.com/grafana/gcx/internal/providers/slo/reports"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
	"k8s.io/client-go/rest"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(NewSLOProvider())
}

// shortDesc is the SLO provider's one-line description, shared by the cobra
// command tree and adapter.NewProvider.
const shortDesc = "Manage Grafana SLO definitions and reports"

// NewSLOProvider builds the declarative SLO provider: SLO definitions
// registered via adapter.Resource[Slo] (definitions.SloResource), with the
// existing hand-written `slo` command tree (definitions + reports) attached
// via WithCommands (FR-018, NC-003, NC-004).
func NewSLOProvider() *adapter.Provider {
	loader := &providers.ConfigLoader{}

	sloCmd := &cobra.Command{
		Use:   "slo",
		Short: shortDesc,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if root := cmd.Root(); root.PersistentPreRun != nil {
				root.PersistentPreRun(cmd, args)
			}
		},
	}

	// Bind config flags on the parent — all subcommands inherit these.
	loader.BindFlags(sloCmd.PersistentFlags())

	sloCmd.AddCommand(definitions.Commands(loader))
	sloCmd.AddCommand(reports.Commands(loader))

	return adapter.NewProvider("slo", shortDesc, loadSLODeps, definitions.SloResource()).
		WithCommands(sloCmd)
}

// loadSLODeps resolves adapter.ClientDeps for the SLO definitions resource:
// it loads REST config via providers.ConfigLoader and builds the HTTP
// client via rest.HTTPClientFor, exactly like every hand-written provider
// command already does. adapter cannot import internal/providers (see
// adapter.DepsLoader's doc comment), so NewProvider takes this loader as a
// caller-supplied closure.
func loadSLODeps(ctx context.Context) (adapter.ClientDeps, error) {
	var loader providers.ConfigLoader
	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return adapter.ClientDeps{}, fmt.Errorf("failed to load REST config for SLO: %w", err)
	}

	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return adapter.ClientDeps{}, fmt.Errorf("failed to create HTTP client for SLO: %w", err)
	}

	return adapter.ClientDeps{
		HTTP:      httpClient,
		BaseURL:   cfg.Host,
		Namespace: cfg.Namespace,
	}, nil
}
