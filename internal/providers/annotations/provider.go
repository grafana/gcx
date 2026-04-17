package annotations

import (
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

var _ providers.Provider = &AnnotationsProvider{}

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&AnnotationsProvider{})
}

// AnnotationsProvider manages Grafana annotations.
type AnnotationsProvider struct{}

func (p *AnnotationsProvider) Name() string { return "annotations" }

func (p *AnnotationsProvider) ShortDesc() string { return "Manage Grafana annotations" }

func (p *AnnotationsProvider) Commands() []*cobra.Command {
	loader := &providers.ConfigLoader{}

	root := &cobra.Command{
		Use:   "annotations",
		Short: p.ShortDesc(),
	}
	// Bubble parent PersistentPreRun when attached to a real CLI root; guard
	// against self-recursion when root itself is used as the root (e.g. in
	// isolated tests where cmd.Root() == root).
	root.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		if r := cmd.Root(); r != root && r.PersistentPreRun != nil {
			r.PersistentPreRun(cmd, args)
		}
	}

	loader.BindFlags(root.PersistentFlags())
	root.AddCommand(
		newListCommand(loader),
		newGetCommand(loader),
		newCreateCommand(loader),
		newUpdateCommand(loader),
		newDeleteCommand(loader),
	)

	return []*cobra.Command{root}
}

func (p *AnnotationsProvider) Validate(_ map[string]string) error { return nil }

func (p *AnnotationsProvider) ConfigKeys() []providers.ConfigKey { return nil }

func (p *AnnotationsProvider) TypedRegistrations() []adapter.Registration { return nil }
