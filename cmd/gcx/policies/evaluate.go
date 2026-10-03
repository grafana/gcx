package policies

import (
	"errors"
	"fmt"
	"io"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	cmdresources "github.com/grafana/gcx/cmd/gcx/resources"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/policies"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/discovery"
	"github.com/grafana/gcx/internal/resources/local"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/cel/openapi/resolver"
	k8sdiscovery "k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
)

type evaluateOpts struct {
	IO cmdio.Options

	Paths         []string
	MaxConcurrent int
}

func (opts *evaluateOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("text", &reportTableCodec{})
	opts.IO.DefaultFormat("text")
	opts.IO.BindFlags(flags)

	flags.StringSliceVarP(&opts.Paths, "path", "p", nil, "Paths on disk from which to read the resources to evaluate. Without it, resources are read from Grafana.")
	flags.IntVar(&opts.MaxConcurrent, "max-concurrent", 10, "Maximum number of concurrent operations")
}

func (opts *evaluateOpts) Validate(args []string) error {
	if len(opts.Paths) == 0 && len(args) == 0 {
		return errors.New("pass resource selectors to evaluate resources stored in Grafana, or --path to evaluate local manifests")
	}
	if opts.MaxConcurrent < 1 {
		return errors.New("max-concurrent must be greater than zero")
	}
	return opts.IO.Validate()
}

func evaluateCmd(configOpts *cmdconfig.Options) *cobra.Command {
	opts := &evaluateOpts{}
	cmd := &cobra.Command{
		Use:   "evaluate [RESOURCE_SELECTOR]...",
		Args:  cobra.ArbitraryArgs,
		Short: "[experimental] Evaluate validation policies against resources without writing them",
		Long: experimentalNotice + `

Evaluate the namespace's validation policies in-process, against local manifests (--path) or
against resources already stored in Grafana (resource selectors). Nothing is written.

Policies, bindings and their parameter objects are read from Grafana, and policies are
type-checked against Grafana's schemas, so the outcome matches what Grafana would decide if each
resource were created now. That also finds stored resources that predate a policy.

Exits with code 4 when any resource breaks a policy whose binding denies writes.`,
		Example: `
	# Would these manifests be admitted?
	gcx policies validation-policies evaluate -p ./resources

	# Only the alert rules among them
	gcx policies validation-policies evaluate -p ./resources alertrules

	# Which stored alert rules and folders break a policy?
	gcx policies validation-policies evaluate alertrules folders`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(args); err != nil {
				return err
			}
			ctx := cmd.Context()
			cfg, _, err := configOpts.LoadGrafanaConfigWithContext(ctx)
			if err != nil {
				return err
			}

			client, err := dynamic.NewForConfig(&cfg.Config)
			if err != nil {
				return err
			}
			disco, err := k8sdiscovery.NewDiscoveryClientForConfig(&cfg.Config)
			if err != nil {
				return err
			}
			reg, err := discovery.NewDefaultRegistry(ctx, cfg)
			if err != nil {
				return err
			}

			list, err := readResources(cmd, opts, cfg, reg, args)
			if err != nil {
				return err
			}

			loaded, err := policies.Load(ctx, client, cfg.Namespace)
			if err != nil {
				return err
			}
			evaluator, err := policies.NewEvaluator(cfg.Namespace, loaded,
				&resolver.ClientDiscoveryResolver{Discovery: disco}, client, resourceLookup(reg))
			if err != nil {
				return err
			}
			report := evaluator.Evaluate(ctx, list)

			if err := opts.IO.Encode(cmd.OutOrStdout(), report); err != nil {
				return err
			}
			if denied := report.Denied(); denied > 0 {
				perr := gcxerrors.NewPartialFailureError("evaluate", report.Evaluated, denied)
				cmdio.EmitWarn(cmd.ErrOrStderr(), fmt.Sprintf("%d policy violations would deny the write", denied))
				return gcxerrors.NewEmittedError(gcxerrors.ExitPartialFailure, perr)
			}
			return nil
		},
	}
	cmd.Annotations = map[string]string{
		agent.AnnotationLLMHint: "gcx policies validation-policies evaluate -p ./resources -o json",
	}
	opts.setup(cmd.Flags())
	return cmd
}

// readResources reads local manifests when --path is set, filtered by any selectors, and
// otherwise fetches the selected resources from Grafana.
func readResources(cmd *cobra.Command, opts *evaluateOpts, cfg config.NamespacedRESTConfig, reg *discovery.Registry, args []string) ([]*resources.Resource, error) {
	ctx := cmd.Context()
	if len(opts.Paths) == 0 {
		resp, err := cmdresources.FetchResources(ctx, cmdresources.FetchRequest{Config: cfg}, args)
		if err != nil {
			return nil, err
		}
		return resp.Resources.AsList(), nil
	}

	sels, err := resources.ParseSelectors(args)
	if err != nil {
		return nil, err
	}
	filters, err := reg.MakeFilters(discovery.MakeFiltersOptions{Selectors: sels})
	if err != nil {
		return nil, err
	}
	reader := local.FSReader{Decoders: format.Codecs(), MaxConcurrentReads: opts.MaxConcurrent}
	list := resources.NewResources()
	if err := reader.Read(ctx, list, filters, opts.Paths); err != nil {
		return nil, err
	}
	return list.AsList(), nil
}

// resourceLookup maps a kind to the API resource the instance serves it under, which is needed
// to read a binding's parameter object.
func resourceLookup(reg *discovery.Registry) func(schema.GroupVersionKind) (schema.GroupVersionResource, bool) {
	byGVK := map[schema.GroupVersionKind]schema.GroupVersionResource{}
	for _, d := range reg.SupportedResources() {
		byGVK[d.GroupVersionKind()] = d.GroupVersionResource()
	}
	return func(gvk schema.GroupVersionKind) (schema.GroupVersionResource, bool) {
		gvr, ok := byGVK[gvk]
		return gvr, ok
	}
}

type reportTableCodec struct{}

func (c *reportTableCodec) Format() format.Format { return "text" }

func (c *reportTableCodec) Encode(w io.Writer, input any) error {
	report, ok := input.(policies.Report)
	if !ok {
		return fmt.Errorf("unexpected input %T", input)
	}
	for _, pe := range report.PolicyErrors {
		cmdio.Warning(w, "policy %s was not evaluated: %s", pe.Policy, pe.Error)
	}
	if len(report.Findings) == 0 {
		cmdio.Success(w, "%d resources checked, %d covered by a policy, no violations.", report.Checked, report.Evaluated)
		return nil
	}
	t := style.NewTable("KIND", "NAME", "ACTION", "POLICY", "MESSAGE")
	for _, f := range report.Findings {
		t.Row(f.Kind, f.Name, string(f.Action), f.Policy, f.Message)
	}
	if err := t.Render(w); err != nil {
		return err
	}
	cmdio.Info(w, "%d resources checked, %d covered by a policy, %d violations (%d would be denied).",
		report.Checked, report.Evaluated, len(report.Findings), report.Denied())
	return nil
}

func (c *reportTableCodec) Decode(io.Reader, any) error {
	return errors.New("codec does not support decoding")
}
