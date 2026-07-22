package probes

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
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
		newResetTokenCommand(loader),
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
	IO        cmdio.Options
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
	// The create result flows through the codec system: the default text
	// codec reproduces the historical lines byte-for-byte; agent mode and
	// explicit -o json/yaml get the structured document — including the
	// one-time auth token as a first-class field.
	o.IO.RegisterCustomCodec("text", &probeCreateCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func (o *createOpts) Validate() error {
	if o.Name == "" {
		return errors.New("--name is required")
	}
	return o.IO.Validate()
}

// probeCreateResult is the finite result document for `probes create`. It is
// bespoke because the one-time auth token — unrecoverable after this
// invocation — must be a structured field, so the shape carries its own
// discriminators.
type probeCreateResult struct {
	Type          string `json:"type" yaml:"type"`
	SchemaVersion string `json:"schema_version" yaml:"schema_version"`
	Action        string `json:"action" yaml:"action"`
	Name          string `json:"name" yaml:"name"`
	ID            int64  `json:"id" yaml:"id"`
	// Token is the one-time probe auth token — it cannot be retrieved later.
	Token string `json:"token" yaml:"token"`
}

// probeCreateCodec is the human "text" codec for probeCreateResult values:
// exactly the lines create has always printed, including the token block.
type probeCreateCodec struct{}

func (c *probeCreateCodec) Format() format.Format { return "text" }

func (c *probeCreateCodec) Decode(io.Reader, any) error {
	return errors.New("text codec does not support decoding")
}

func (c *probeCreateCodec) Encode(w io.Writer, v any) error {
	r, ok := v.(probeCreateResult)
	if !ok {
		return errors.New("invalid data type for probe create codec: expected probeCreateResult")
	}
	cmdio.Success(w, "Created probe %q (id=%d)", r.Name, r.ID)
	fmt.Fprintf(w, "\nProbe auth token (save this — it cannot be retrieved later):\n%s\n", r.Token)
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

			result := probeCreateResult{
				Type:          "gcx.synth.probe_create",
				SchemaVersion: "1",
				Action:        "created",
				Name:          resp.Probe.Name,
				ID:            resp.Probe.ID,
				Token:         resp.Token,
			}
			return opts.IO.Encode(w, result)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// delete
// ---------------------------------------------------------------------------

type deleteOpts struct {
	IO    cmdio.Options
	Force bool
}

func (o *deleteOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.Force, "force", false, "Skip confirmation prompt")
	// The delete result flows through the codec system: the default text
	// codec reproduces the historical per-target lines byte-for-byte; agent
	// mode and explicit -o json/yaml get the structured document.
	o.IO.RegisterCustomCodec("text", &deleteResultCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func (o *deleteOpts) Validate() error {
	return o.IO.Validate()
}

// deleteBatchResult is the finite result document for `probes delete`.
// Bespoke rather than a cmdio.BatchMutation because the historical human
// output enumerates each deleted target, so the result must carry them.
type deleteBatchResult struct {
	Type          string                  `json:"type" yaml:"type"`
	SchemaVersion string                  `json:"schema_version" yaml:"schema_version"`
	Action        string                  `json:"action" yaml:"action"`
	Summary       cmdio.MutationSummary   `json:"summary" yaml:"summary"`
	Deleted       []string                `json:"deleted" yaml:"deleted"`
	Failures      []cmdio.MutationFailure `json:"failures" yaml:"failures"`
}

func newDeleteBatchResult() deleteBatchResult {
	return deleteBatchResult{
		Type:          "gcx.synth.delete_batch",
		SchemaVersion: "1",
		Action:        "deleted",
		Deleted:       []string{},
		Failures:      []cmdio.MutationFailure{},
	}
}

// deleteResultCodec is the human "text" codec for deleteBatchResult values:
// exactly the per-target lines delete has always printed. Failures stay on
// stderr as diagnostics, matching the pre-codec behavior.
type deleteResultCodec struct{}

func (c *deleteResultCodec) Format() format.Format { return "text" }

func (c *deleteResultCodec) Decode(io.Reader, any) error {
	return errors.New("text codec does not support decoding")
}

func (c *deleteResultCodec) Encode(w io.Writer, v any) error {
	result, ok := v.(deleteBatchResult)
	if !ok {
		return errors.New("invalid data type for delete result codec: expected deleteBatchResult")
	}
	for _, id := range result.Deleted {
		cmdio.Success(w, "Deleted probe %s", id)
	}
	return nil
}

// emitPartialResult writes the completed result document (which enumerates
// the failure) to stdout and returns the exit-4 sentinel. Call only after at
// least one target succeeded — the cause additionally goes to stderr because
// reportError writes nothing more for an EmittedError.
func emitPartialResult(cmd *cobra.Command, io *cmdio.Options, result any, cause error) error {
	cmdio.Error(cmd.ErrOrStderr(), "%v", cause)
	if err := io.Encode(cmd.OutOrStdout(), result); err != nil {
		return err
	}
	return gcxerrors.NewEmittedError(gcxerrors.ExitPartialFailure, cause)
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

			// The prompt and the decline note are diagnostics — stderr keeps
			// them out of the stdout result document.
			proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force,
				fmt.Sprintf("Delete %d probe(s)?", len(args)))
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			crud, _, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			result := newDeleteBatchResult()
			for i, arg := range args {
				if err := crud.Delete(ctx, arg); err != nil {
					cause := fmt.Errorf("deleting probe %s: %w", arg, err)
					result.Summary.Failed++
					result.Summary.Skipped = len(args) - i - 1
					result.Failures = append(result.Failures, cmdio.MutationFailure{
						Target: cmdio.MutationTarget{Kind: "Probe", ID: arg},
						Error:  cause.Error(),
					})
					if result.Summary.Succeeded == 0 {
						return cause
					}
					return emitPartialResult(cmd, &opts.IO, result, cause)
				}
				result.Deleted = append(result.Deleted, arg)
				result.Summary.Succeeded++
			}

			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// reset-token
// ---------------------------------------------------------------------------

type tokenResetOpts struct {
	IO cmdio.Options
}

func (o *tokenResetOpts) setup(flags *pflag.FlagSet) {
	// The reset result flows through the codec system: the default text
	// codec reproduces the historical line byte-for-byte; agent mode and
	// explicit -o json/yaml get the structured document.
	o.IO.RegisterCustomCodec("text", &resetTokenCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func (o *tokenResetOpts) Validate() error { return o.IO.Validate() }

// resetTokenCodec is the human "text" codec for the reset-token
// cmdio.SingleMutation result: exactly the line the command has always
// printed on stdout. The token-not-returned note stays on stderr as a
// diagnostic.
type resetTokenCodec struct{}

func (c *resetTokenCodec) Format() format.Format { return "text" }

func (c *resetTokenCodec) Decode(io.Reader, any) error {
	return errors.New("text codec does not support decoding")
}

func (c *resetTokenCodec) Encode(w io.Writer, v any) error {
	r, ok := v.(cmdio.SingleMutation)
	if !ok {
		return errors.New("invalid data type for reset-token codec: expected SingleMutation")
	}
	cmdio.Success(w, "Reset auth token for probe %q (id=%s)", r.Target.Name, r.Target.ID)
	return nil
}

func newResetTokenCommand(loader smcfg.Loader) *cobra.Command {
	opts := &tokenResetOpts{}
	cmd := &cobra.Command{
		Use:   "reset-token ID",
		Short: "Reset the auth token of a Synthetic Monitoring probe.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

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

			result := cmdio.NewSingleMutation("reset-token", cmdio.MutationTarget{
				Kind: "Probe",
				Name: updated.Name,
				ID:   strconv.FormatInt(updated.ID, 10),
			})
			changed := true
			result.Changed = &changed

			if err := opts.IO.Encode(cmd.OutOrStdout(), result); err != nil {
				return err
			}
			// Advisory note, not the result — stderr keeps it out of the
			// stdout document.
			cmdio.Warning(cmd.ErrOrStderr(), "The SM API does not return the new token in the reset response. Re-create the probe if you need the token.")
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
