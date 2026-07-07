package probes

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/crudcmd"
	"github.com/grafana/gcx/internal/providers/synth/smcfg"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/style"
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
	cmd.AddCommand(
		newListCommand(loader),
		newCreateCommand(loader),
		newDeleteCommand(loader),
		newTokenResetCommand(loader),
		newDeployCommand(),
	)
	return cmd
}

// ---------------------------------------------------------------------------
// list
// ---------------------------------------------------------------------------

func newListCommand(loader smcfg.Loader) *cobra.Command {
	return crudcmd.NewTypedListCommand(crudcmd.TypedListConfig[Probe]{
		Use:          "list",
		Short:        "List Synthetic Monitoring probes.",
		DefaultFmt:   "table",
		LimitDefault: 50,
		LimitUsage:   "Maximum number of items to return (0 for all)",
		Codecs:       []format.Codec{&probeTableCodec{}},
		Noun:         "probe",
		NewCRUD:      func(ctx context.Context) (*adapter.TypedCRUD[Probe], string, error) { return NewTypedCRUD(ctx, loader) },
		ToResource: func(crud *adapter.TypedCRUD[Probe], p Probe) (unstructured.Unstructured, error) {
			res, err := ToResource(p, crud.Namespace)
			if err != nil {
				return unstructured.Unstructured{}, err
			}
			return res.ToUnstructured(), nil
		},
	})
}

// ---------------------------------------------------------------------------
// create
// ---------------------------------------------------------------------------

type createOpts struct {
	Name      string
	Region    string
	Labels    []string
	Latitude  float64
	Longitude float64
}

func (o *createOpts) setup(flags *pflag.FlagSet) {
	flags.StringVar(&o.Name, "name", "", "Probe name (required)")
	flags.StringVar(&o.Region, "region", "", "Probe region")
	flags.StringSliceVar(&o.Labels, "labels", nil, "Labels in key=value format")
	flags.Float64Var(&o.Latitude, "latitude", 0, "Probe latitude")
	flags.Float64Var(&o.Longitude, "longitude", 0, "Probe longitude")
}

func (o *createOpts) Validate() error {
	if o.Name == "" {
		return errors.New("--name is required")
	}
	return nil
}

func newCreateCommand(loader smcfg.Loader) *cobra.Command {
	opts := &createOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a Synthetic Monitoring probe.",
		Args:  cobra.NoArgs,
		Example: `  # Create a probe with a name and region.
  gcx synthetic-monitoring probes create --name my-probe --region eu

  # Create a probe with labels and coordinates.
  gcx synthetic-monitoring probes create --name my-probe --region us --labels env=prod,team=sre --latitude 37.7749 --longitude -122.4194`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			w := cmd.OutOrStdout()

			restCfg, uid, _, err := loader.LoadSMProxyConfig(ctx)
			if err != nil {
				return err
			}
			client, err := NewClient(restCfg, uid, loader)
			if err != nil {
				return err
			}

			var labels []ProbeLabel
			for _, l := range opts.Labels {
				k, v, ok := strings.Cut(l, "=")
				if !ok {
					return fmt.Errorf("invalid label %q: expected key=value", l)
				}
				labels = append(labels, ProbeLabel{Name: k, Value: v})
			}

			probe := Probe{
				Name:      opts.Name,
				Region:    opts.Region,
				Public:    false,
				Latitude:  opts.Latitude,
				Longitude: opts.Longitude,
				Labels:    labels,
				Capabilities: ProbeCapabilities{
					DisableScriptedChecks: true,
					DisableBrowserChecks:  true,
				},
			}

			resp, err := client.Create(ctx, probe)
			if err != nil {
				return err
			}

			cmdio.Success(w, "Created probe %q (id=%d)", resp.Probe.Name, resp.Probe.ID)
			fmt.Fprintf(w, "\nProbe auth token (save this — it cannot be retrieved later):\n%s\n", resp.Token)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// delete
// ---------------------------------------------------------------------------

func newDeleteCommand(loader smcfg.Loader) *cobra.Command {
	return crudcmd.NewDeleteCommand(crudcmd.DeleteConfig{
		Use:   "delete ID...",
		Short: "Delete Synthetic Monitoring probes.",
		Args:  cobra.MinimumNArgs(1),
		Confirm: func(args []string) string {
			return fmt.Sprintf("Delete %d probe(s)?", len(args))
		},
		NewDelete: func(ctx context.Context) (func(string) error, error) {
			crud, _, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return nil, err
			}
			return func(id string) error { return crud.Delete(ctx, id) }, nil
		},
		Success: func(id string) string { return "Deleted probe " + id },
	})
}

// ---------------------------------------------------------------------------
// token-reset
// ---------------------------------------------------------------------------

type tokenResetOpts struct{}

func (o *tokenResetOpts) setup(_ *pflag.FlagSet) {}

func (o *tokenResetOpts) Validate() error { return nil }

func newTokenResetCommand(loader smcfg.Loader) *cobra.Command {
	opts := &tokenResetOpts{}
	cmd := &cobra.Command{
		Use:   "token-reset ID",
		Short: "Reset the auth token of a Synthetic Monitoring probe.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			w := cmd.OutOrStdout()

			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid probe ID %q: %w", args[0], err)
			}

			restCfg, uid, _, err := loader.LoadSMProxyConfig(ctx)
			if err != nil {
				return err
			}
			client, err := NewClient(restCfg, uid, loader)
			if err != nil {
				return err
			}

			probe, err := client.Get(ctx, id)
			if err != nil {
				return err
			}

			updated, err := client.ResetToken(ctx, *probe)
			if err != nil {
				return err
			}

			cmdio.Success(w, "Reset auth token for probe %q (id=%d)", updated.Name, updated.ID)
			cmdio.Warning(w, "The SM API does not return the new token in the reset response. Re-create the probe if you need the token.")
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// deploy
// ---------------------------------------------------------------------------

type deployOpts struct {
	Token        string
	Namespace    string
	Image        string
	ProbeName    string
	APIServerURL string
}

func (o *deployOpts) setup(flags *pflag.FlagSet) {
	flags.StringVar(&o.Token, "token", "", "Probe auth token (required)")
	flags.StringVar(&o.ProbeName, "probe-name", "", "Name for the k8s resources (required)")
	flags.StringVar(&o.APIServerURL, "api-server-url", "", "SM API gRPC endpoint (required)")
	flags.StringVar(&o.Namespace, "namespace", "synthetic-monitoring", "K8s namespace")
	flags.StringVar(&o.Image, "image", DefaultAgentImage, "SM agent container image")
}

func (o *deployOpts) Validate() error {
	return DeployConfig{
		ProbeName:    o.ProbeName,
		ProbeToken:   o.Token,
		APIServerURL: o.APIServerURL,
	}.Validate()
}

func newDeployCommand() *cobra.Command {
	opts := &deployOpts{}
	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Generate Kubernetes manifests for deploying an SM agent.",
		Args:  cobra.NoArgs,
		Example: `  # Generate manifests for a probe deployment.
  gcx synthetic-monitoring probes deploy --probe-name my-probe --token <token> --api-server-url synthetic-monitoring-grpc.grafana.net:443

  # Pipe directly into kubectl.
  gcx synthetic-monitoring probes deploy --probe-name my-probe --token <token> --api-server-url synthetic-monitoring-grpc.grafana.net:443 | kubectl apply -f -`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			return RenderManifests(cmd.OutOrStdout(), DeployConfig{
				ProbeName:    opts.ProbeName,
				ProbeToken:   opts.Token,
				APIServerURL: opts.APIServerURL,
				Namespace:    opts.Namespace,
				Image:        opts.Image,
			})
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type probeTableCodec struct{}

func (c *probeTableCodec) Format() format.Format { return "table" }

func (c *probeTableCodec) Encode(w io.Writer, v any) error {
	return crudcmd.EncodeTable(w, v, "Probe", []string{"ID", "NAME", "REGION", "PUBLIC", "ONLINE"}, func(t *style.TableBuilder, p Probe) {
		t.Row(
			strconv.FormatInt(p.ID, 10),
			p.Name,
			p.Region,
			strconv.FormatBool(p.Public),
			strconv.FormatBool(p.Online))
	})
}

func (c *probeTableCodec) Decode(r io.Reader, v any) error {
	return crudcmd.ErrTableDecode
}
