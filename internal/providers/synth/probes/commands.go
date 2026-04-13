package probes

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/synth/smcfg"
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

type listOpts struct {
	IO    cmdio.Options
	Limit int64
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &probeTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)

	flags.Int64Var(&o.Limit, "limit", 50, "Maximum number of items to return (0 for all)")
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

			typedObjs, err := crud.List(ctx, opts.Limit)
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
  gcx synth probes create --name my-probe --region eu

  # Create a probe with labels and coordinates.
  gcx synth probes create --name my-probe --region us --labels env=prod,team=sre --latitude 37.7749 --longitude -122.4194`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			w := cmd.OutOrStdout()

			baseURL, token, _, err := loader.LoadSMConfig(ctx)
			if err != nil {
				return err
			}
			client := NewClient(ctx, baseURL, token)

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

type deleteOpts struct {
	Force bool
}

func (o *deleteOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVarP(&o.Force, "force", "f", false, "Skip confirmation prompt")
}

func (o *deleteOpts) Validate() error {
	return nil
}

func newDeleteCommand(loader smcfg.Loader) *cobra.Command {
	opts := &deleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete ID...",
		Short: "Delete Synthetic Monitoring probes.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			w := cmd.OutOrStdout()

			if !opts.Force {
				fmt.Fprintf(w, "Delete %d probe(s)? [y/N] ", len(args))
				reader := bufio.NewReader(cmd.InOrStdin())
				answer, err := reader.ReadString('\n')
				if err != nil {
					return fmt.Errorf("reading confirmation: %w", err)
				}
				answer = strings.TrimSpace(strings.ToLower(answer))
				if answer != "y" && answer != "yes" {
					cmdio.Info(w, "Aborted.")
					return nil
				}
			}

			crud, _, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			for _, arg := range args {
				if err := crud.Delete(ctx, arg); err != nil {
					return fmt.Errorf("deleting probe %s: %w", arg, err)
				}
				cmdio.Success(w, "Deleted probe %s", arg)
			}
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
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

			baseURL, token, _, err := loader.LoadSMConfig(ctx)
			if err != nil {
				return err
			}
			client := NewClient(ctx, baseURL, token)

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
  gcx synth probes deploy --probe-name my-probe --token <token> --api-server-url synthetic-monitoring-grpc.grafana.net:443

  # Pipe directly into kubectl.
  gcx synth probes deploy --probe-name my-probe --token <token> --api-server-url synthetic-monitoring-grpc.grafana.net:443 | kubectl apply -f -`,
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
	probeList, ok := v.([]Probe)
	if !ok {
		return errors.New("invalid data type for table codec: expected []Probe")
	}

	t := style.NewTable("ID", "NAME", "REGION", "PUBLIC", "ONLINE")

	for _, p := range probeList {
		t.Row(
			strconv.FormatInt(p.ID, 10),
			p.Name,
			p.Region,
			strconv.FormatBool(p.Public),
			strconv.FormatBool(p.Online))
	}

	return t.Render(w)
}

func (c *probeTableCodec) Decode(r io.Reader, v any) error {
	return errors.New("table format does not support decoding")
}
