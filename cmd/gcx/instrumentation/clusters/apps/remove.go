package apps

import (
	"context"
	"errors"
	"io"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	instoutput "github.com/grafana/gcx/internal/providers/instrumentation/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type removeOpts struct {
	IO  cmdio.Options
	yes bool
}

func (o *removeOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.yes, "yes", false, "Confirm removal of namespace app instrumentation")
	// The remove result is a MutationResult document through the codec
	// system: the default text codec prints the familiar one-liner; agent
	// mode and explicit -o json/yaml get the structured document.
	instoutput.BindMutationIO(&o.IO, flags)
}

func (o *removeOpts) Validate() error {
	if !o.yes {
		return errors.New("apps remove: requires --yes to proceed (this removes namespace app instrumentation)")
	}
	return o.IO.Validate()
}

// makeRemoveCmd builds the "apps remove <cluster> <namespace>" command.
//
// Removes the namespace entry from the cluster's namespaces[] list via
// SetAppInstrumentation. Requires --yes to proceed.
//
// factory is called inside RunE — after cobra has parsed all flags — to
// lazily construct the appsClient and BackendURLs.
func makeRemoveCmd(factory appClientFactory) *cobra.Command {
	opts := &removeOpts{}

	cmd := &cobra.Command{
		Use:   "remove <cluster> <namespace>",
		Short: "Remove Beyla instrumentation for a namespace",
		Long: `Remove Beyla auto-instrumentation for a namespace by removing its entry
from the cluster's app instrumentation configuration.

The namespace entry is removed from namespaces[] via a whole-list replacement
(SetAppInstrumentation). When no namespace entries remain with included content,
the backend deletes the app pipeline entirely.

This command requires --yes to proceed.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			client, urls, _, err := factory(ctx)
			if err != nil {
				return err
			}
			cluster := args[0]
			namespace := args[1]

			return runAppRemove(ctx, &opts.IO, client, cluster, namespace, urls, cmd.OutOrStdout())
		},
	}

	opts.setup(cmd.Flags())
	return cmd
}

// runAppRemove performs the core remove logic. Separated from makeRemoveCmd
// for testability with fake clients.
func runAppRemove(
	ctx context.Context,
	outOpts *cmdio.Options,
	client appsClient,
	cluster, namespace string,
	urls instrumentation.BackendURLs,
	w io.Writer,
) error {
	resp, err := client.GetAppInstrumentation(ctx, cluster)
	if err != nil {
		return err
	}

	// Remove the target namespace from the list.
	updated := make([]instrumentation.App, 0, len(resp.Namespaces))
	for _, ns := range resp.Namespaces {
		if ns.Name == namespace {
			continue
		}
		updated = append(updated, ns)
	}

	if err := client.SetAppInstrumentation(ctx, cluster, updated, urls); err != nil {
		return err
	}

	result := instoutput.NewMutationResult("remove", instoutput.Target{Cluster: cluster, Namespace: namespace})
	result.Changed = true
	return outOpts.Encode(w, result)
}

// newRemoveCmd is a test-facing constructor that injects a pre-built appsClient
// and BackendURLs. Production code uses makeRemoveCmd(factoryFromLoader(loader)) instead.
func newRemoveCmd(client appsClient, urls instrumentation.BackendURLs) *cobra.Command {
	return makeRemoveCmd(func(_ context.Context) (appsClient, instrumentation.BackendURLs, instrumentation.PromHeaders, error) {
		return client, urls, instrumentation.PromHeaders{}, nil
	})
}
