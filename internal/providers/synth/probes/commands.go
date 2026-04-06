package probes

import (
	"errors"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/synth/smcfg"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Commands returns the probes command group.
func Commands(loader smcfg.Loader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "probes",
		Short:   "Manage Synthetic Monitoring probes.",
		Aliases: []string{"probe"},
	}
	cmd.AddCommand(newListCommand(loader))
	return cmd
}

// ---------------------------------------------------------------------------
// list
// ---------------------------------------------------------------------------

type listOpts struct {
	IO cmdio.Options
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &probeTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newListCommand(loader smcfg.Loader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Synthetic Monitoring probes.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			crud, namespace, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			typedObjs, err := crud.List(ctx)
			if err != nil {
				return err
			}

			// Extract probes from TypedObject
			probeList := make([]Probe, len(typedObjs))
			for i := range typedObjs {
				probeList[i] = typedObjs[i].Spec
			}

			codec, err := opts.IO.Codec()
			if err != nil {
				return err
			}

			if codec.Format() == "table" {
				return codec.Encode(cmd.OutOrStdout(), probeList)
			}

			var objs []unstructured.Unstructured
			for _, typedObj := range typedObjs {
				res, err := ToResource(typedObj.Spec, namespace)
				if err != nil {
					return fmt.Errorf("converting probe %d: %w", typedObj.Spec.ID, err)
				}
				objs = append(objs, res.ToUnstructured())
			}
			return opts.IO.Encode(cmd.OutOrStdout(), objs)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type probeTableCodec struct{}

func (c *probeTableCodec) Format() format.Format { return "table" }

func (c *probeTableCodec) Encode(w io.Writer, v any) error {
	probeList, ok := v.([]Probe)
	if !ok {
		return errors.New("invalid data type for table codec: expected []Probe")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tREGION\tPUBLIC\tONLINE")

	for _, p := range probeList {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%v\t%v\n",
			p.ID, p.Name, p.Region, p.Public, p.Online)
	}

	return tw.Flush()
}

func (c *probeTableCodec) Decode(r io.Reader, v any) error {
	return errors.New("table format does not support decoding")
}
