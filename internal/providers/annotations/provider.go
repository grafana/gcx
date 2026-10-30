package annotations

import (
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Register the provider once.
	providers.Register(NewAnnotationsProvider())
}

const shortDesc = "Manage Grafana annotations"

// NewAnnotationsProvider creates the command-only provider.
// Server-assigned IDs prevent safe resource push between stacks.
func NewAnnotationsProvider() *adapter.Provider {
	return adapter.NewProvider("annotations", shortDesc, providers.LoadGrafanaDeps).WithCommands(newAnnotationCommands)
}

func newAnnotationCommands() []*cobra.Command {
	loader := &providers.ConfigLoader{}
	root := &cobra.Command{Use: "annotations", Short: shortDesc}
	loader.BindFlags(root.PersistentFlags())
	root.AddCommand(newListCommand(loader), newGetCommand(loader), newCreateCommand(loader), newUpdateCommand(loader), newDeleteCommand(loader), newTagsCommand(loader), newMassDeleteCommand(loader))
	return []*cobra.Command{root}
}
