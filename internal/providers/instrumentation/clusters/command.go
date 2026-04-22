package clusters

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/grafana/gcx/internal/fleet"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/shared"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Commands returns the clusters command group.
func Commands(loader *providers.ConfigLoader) *cobra.Command {
	return newCommand(loader)
}

func newCommand(loader fleet.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clusters",
		Short: "Manage Kubernetes cluster monitoring configuration.",
	}
	cmd.AddCommand(
		newListCommand(loader),
		newGetCommand(loader),
		newCreateCommand(loader),
		newUpdateCommand(loader),
		newDeleteCommand(loader),
	)
	return cmd
}

// --- list ---

type listOpts struct {
	IO    cmdio.Options
	Limit int64
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &clusterTableCodec{})
	o.IO.RegisterCustomCodec("wide", &clusterTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.Int64Var(&o.Limit, "limit", 0, "Maximum number of clusters to return (0 for no limit)")
}

func newListCommand(loader fleet.ConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List cluster monitoring configurations.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			crud, err := instrumentation.NewClusterTypedCRUD(cmd.Context(), loader)
			if err != nil {
				return err
			}
			items, err := crud.List(cmd.Context(), opts.Limit)
			if err != nil {
				return err
			}
			if err := opts.IO.Encode(cmd.OutOrStdout(), items); err != nil {
				return err
			}
			var footer instrumentation.Footer
			if len(items) == 0 {
				footer.Notef("no clusters found.")
			}
			footer.Notef("data reflects Alloy collector-reported state (refresh ~30s).")
			if len(items) == 0 {
				footer.Hint("to register a cluster", "gcx instrumentation clusters setup --help")
			}
			footer.Print(cmd.ErrOrStderr())
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- get ---

type getOpts struct {
	IO cmdio.Options
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &clusterTableCodec{})
	o.IO.RegisterCustomCodec("wide", &clusterTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newGetCommand(loader fleet.ConfigLoader) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get <name>",
		Short: "Get a cluster monitoring configuration.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			crud, err := instrumentation.NewClusterTypedCRUD(cmd.Context(), loader)
			if err != nil {
				return err
			}
			name := args[0]
			item, err := crud.Get(cmd.Context(), name)
			if err != nil {
				if errors.Is(err, adapter.ErrNotFound) {
					var footer instrumentation.Footer
					footer.Hint("to verify the cluster name", "gcx instrumentation clusters list")
					footer.Hint("to check if the collector has reported", "gcx instrumentation status")
					footer.Print(cmd.ErrOrStderr())
				}
				return err
			}
			if err := opts.IO.Encode(cmd.OutOrStdout(), []adapter.TypedObject[instrumentation.Cluster]{*item}); err != nil {
				return err
			}
			var footer instrumentation.Footer
			footer.Notef("data reflects Alloy collector-reported state (refresh ~30s).")
			footer.Print(cmd.ErrOrStderr())
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- create ---

type createOpts struct {
	IO   cmdio.Options
	File string
}

func (o *createOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the cluster manifest (use - for stdin)")
}

func (o *createOpts) Validate() error {
	if o.File == "" {
		return errors.New("--filename/-f is required")
	}
	return o.IO.Validate()
}

func newCreateCommand(loader fleet.ConfigLoader) *cobra.Command {
	opts := &createOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a cluster monitoring configuration.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			cluster, err := readClusterFromFile(opts.File, cmd.InOrStdin())
			if err != nil {
				return err
			}
			crud, err := instrumentation.NewClusterTypedCRUD(cmd.Context(), loader)
			if err != nil {
				return err
			}
			typedObj := &adapter.TypedObject[instrumentation.Cluster]{Spec: *cluster}
			typedObj.SetName(cluster.GetResourceName())
			created, err := crud.Create(cmd.Context(), typedObj)
			if err != nil {
				return err
			}
			name := cluster.GetResourceName()
			if err := opts.IO.Encode(cmd.OutOrStdout(), created); err != nil {
				return err
			}
			var footer instrumentation.Footer
			footer.Notef("Cluster %q is persisted; list/get shows Alloy collector-reported state and may take ~30s to refresh.", name)
			footer.Hint("to verify", "gcx instrumentation status --cluster "+name)
			footer.Print(cmd.ErrOrStderr())
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- update ---

type updateOpts struct {
	IO   cmdio.Options
	File string
}

func (o *updateOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the cluster manifest (use - for stdin)")
}

func (o *updateOpts) Validate() error {
	if o.File == "" {
		return errors.New("--filename/-f is required")
	}
	return o.IO.Validate()
}

func newUpdateCommand(loader fleet.ConfigLoader) *cobra.Command {
	opts := &updateOpts{}
	cmd := &cobra.Command{
		Use:   "update <name>",
		Short: "Update a cluster monitoring configuration.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			name := args[0]
			cluster, err := readClusterFromFile(opts.File, cmd.InOrStdin())
			if err != nil {
				return err
			}
			cluster.SetResourceName(name)
			crud, err := instrumentation.NewClusterTypedCRUD(cmd.Context(), loader)
			if err != nil {
				return err
			}
			typedObj := &adapter.TypedObject[instrumentation.Cluster]{Spec: *cluster}
			typedObj.SetName(name)
			updated, err := crud.Update(cmd.Context(), name, typedObj)
			if err != nil {
				return err
			}
			if err := opts.IO.Encode(cmd.OutOrStdout(), updated); err != nil {
				return err
			}
			var footer instrumentation.Footer
			footer.Notef("Cluster %q is persisted; list/get shows Alloy collector-reported state and may take ~30s to refresh.", name)
			footer.Hint("to verify", "gcx instrumentation status --cluster "+name)
			footer.Print(cmd.ErrOrStderr())
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- delete ---

type deleteOpts struct{}

func (o *deleteOpts) setup(_ *pflag.FlagSet) {}

func (o *deleteOpts) Validate() error { return nil }

func newDeleteCommand(loader fleet.ConfigLoader) *cobra.Command {
	opts := &deleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a cluster monitoring configuration.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			crud, err := instrumentation.NewClusterTypedCRUD(cmd.Context(), loader)
			if err != nil {
				return err
			}
			name := args[0]
			if err := crud.Delete(cmd.Context(), name); err != nil {
				return err
			}
			var footer instrumentation.Footer
			footer.Notef("Cluster %q deletion is persisted; list/get shows Alloy collector-reported state and may take ~30s to refresh.", name)
			footer.Hint("to verify", "gcx instrumentation status --cluster "+name)
			footer.Print(cmd.ErrOrStderr())
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- table / wide codec ---

type clusterTableCodec struct {
	Wide bool
}

func (tc *clusterTableCodec) Format() format.Format {
	if tc.Wide {
		return "wide"
	}
	return "table"
}

func (tc *clusterTableCodec) Encode(w io.Writer, v any) error {
	objs, ok := v.([]adapter.TypedObject[instrumentation.Cluster])
	if !ok {
		return errors.New("invalid data type for table codec: expected []TypedObject[Cluster]")
	}
	var t *style.TableBuilder
	if tc.Wide {
		t = style.NewTable("NAME", "STATUS", "COST_METRICS", "CLUSTER_EVENTS", "ENERGY_METRICS", "NODE_LOGS", "WORKLOADS", "PODS", "NODES")
		for _, obj := range objs {
			c := obj.Spec
			t.Row(c.GetResourceName(), shared.ValOrDash(c.Status),
				strconv.FormatBool(c.CostMetrics), strconv.FormatBool(c.ClusterEvents),
				strconv.FormatBool(c.EnergyMetrics), strconv.FormatBool(c.NodeLogs),
				strconv.Itoa(c.Workloads), strconv.Itoa(c.Pods), strconv.Itoa(c.Nodes))
		}
	} else {
		t = style.NewTable("NAME", "STATUS", "COST_METRICS", "CLUSTER_EVENTS", "ENERGY_METRICS", "NODE_LOGS")
		for _, obj := range objs {
			c := obj.Spec
			t.Row(c.GetResourceName(), shared.ValOrDash(c.Status),
				strconv.FormatBool(c.CostMetrics), strconv.FormatBool(c.ClusterEvents),
				strconv.FormatBool(c.EnergyMetrics), strconv.FormatBool(c.NodeLogs))
		}
	}
	return t.Render(w)
}

func (tc *clusterTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// --- file reader ---

func readClusterFromFile(file string, stdin io.Reader) (*instrumentation.Cluster, error) {
	var reader io.Reader
	if file == "-" {
		reader = stdin
	} else {
		f, err := os.Open(file)
		if err != nil {
			return nil, fmt.Errorf("failed to open file %s: %w", file, err)
		}
		defer f.Close()
		reader = f
	}

	yamlCodec := format.NewYAMLCodec()
	var obj unstructured.Unstructured
	if err := yamlCodec.Decode(reader, &obj); err != nil {
		return nil, fmt.Errorf("failed to parse input: %w", err)
	}

	name := obj.GetName()

	specRaw, ok := obj.Object["spec"]
	if !ok {
		return nil, errors.New("manifest is missing spec field")
	}

	specJSON, err := json.Marshal(specRaw)
	if err != nil {
		return nil, fmt.Errorf("failed to encode spec: %w", err)
	}

	var cluster instrumentation.Cluster
	if err := json.Unmarshal(specJSON, &cluster); err != nil {
		return nil, fmt.Errorf("failed to decode spec: %w", err)
	}

	cluster.SetResourceName(name)
	return &cluster, nil
}
