package apps

import (
	"context"
	"errors"

	"github.com/grafana/gcx/internal/providers/instrumentation"
	instoutput "github.com/grafana/gcx/internal/providers/instrumentation/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type removeOpts struct {
	yes bool
}

func (o *removeOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.yes, "yes", false, "Confirm removal of namespace app instrumentation")
}

func (o *removeOpts) Validate() error {
	if !o.yes {
		return errors.New("apps remove: requires --yes to proceed (this removes namespace app instrumentation)")
	}
	return nil
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

			return runAppRemove(ctx, client, cluster, namespace, urls, cmd.OutOrStdout())
		},
	}

	opts.setup(cmd.Flags())
	return cmd
}

// runAppRemove performs the core remove logic. Separated from makeRemoveCmd
// for testability with fake clients.
func runAppRemove(
	ctx context.Context,
	client appsClient,
	cluster, namespace string,
	urls instrumentation.BackendURLs,
	w interface{ Write(p []byte) (int, error) },
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

	return instoutput.MutationResult{
		Action:  "remove",
		Target:  instoutput.Target{Cluster: cluster, Namespace: namespace},
		Changed: true,
	}.Emit(w)
}

// newRemoveCmd is a test-facing constructor that injects a pre-built appsClient
// and BackendURLs. Production code uses makeRemoveCmd(factoryFromLoader(loader)) instead.
func newRemoveCmd(client appsClient, urls instrumentation.BackendURLs) *cobra.Command {
	return makeRemoveCmd(func(_ context.Context) (appsClient, instrumentation.BackendURLs, instrumentation.PromHeaders, error) {
		return client, urls, instrumentation.PromHeaders{}, nil
	})
}
