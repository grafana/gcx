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

// Name returns the unique identifier for this provider.
func (p *AnnotationsProvider) Name() string { return "annotations" }

// ShortDesc returns a one-line description of the provider.
func (p *AnnotationsProvider) ShortDesc() string { return "Manage Grafana annotations" }

// Commands returns the Cobra commands contributed by this provider.
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

// Validate checks that the given provider configuration is valid.
func (p *AnnotationsProvider) Validate(_ map[string]string) error { return nil }

// ConfigKeys returns the configuration keys used by this provider.
func (p *AnnotationsProvider) ConfigKeys() []providers.ConfigKey { return nil }

// TypedRegistrations returns adapter registrations for Annotation resource types.
func (p *AnnotationsProvider) TypedRegistrations() []adapter.Registration {
	loader := &providers.ConfigLoader{}
	return []adapter.Registration{
		{
			Factory:    NewAdapterFactory(loader),
			Descriptor: staticDescriptor,
			GVK:        staticDescriptor.GroupVersionKind(),
			Schema:     AnnotationSchema(),
			Example:    AnnotationExample(),
		},
	}
}
