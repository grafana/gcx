package policies

import (
	"errors"
	"fmt"
	"io"
	"strings"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/policies"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/client-go/dynamic"
)

type listOpts struct {
	IO cmdio.Options
}

func (opts *listOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("text", &policyTableCodec{})
	opts.IO.DefaultFormat("text")
	opts.IO.BindFlags(flags)
}

func (opts *listOpts) Validate() error {
	return opts.IO.Validate()
}

func listCmd(configOpts *cmdconfig.Options) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Args:  cobra.NoArgs,
		Short: "[experimental] List validation policies and their bindings",
		Long: experimentalNotice + `

List the namespace's validation policies, what they apply to, and how each binding enforces them.`,
		Example: `
	# List validation policies
	gcx policies validation-policies list

	# As JSON
	gcx policies validation-policies list -o json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
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
			list, err := policies.Load(ctx, client, cfg.Namespace)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), list)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type policyTableCodec struct{}

func (c *policyTableCodec) Format() format.Format { return "text" }

func (c *policyTableCodec) Encode(w io.Writer, input any) error {
	list, ok := input.([]policies.Policy)
	if !ok {
		return fmt.Errorf("unexpected input %T", input)
	}
	t := style.NewTable("NAME", "APPLIES TO", "PARAMS", "BINDINGS")
	for _, p := range list {
		var targets []string
		for _, m := range p.Match {
			targets = append(targets, fmt.Sprintf("%s %s", strings.Join(m.Kinds, ","), m.Group))
		}
		params := "-"
		if p.ParamKind != nil {
			params = p.ParamKind.Kind
		}
		var bindings []string
		for _, b := range p.Bindings {
			actions := make([]string, 0, len(b.Actions))
			for _, a := range b.Actions {
				actions = append(actions, string(a))
			}
			desc := fmt.Sprintf("%s (%s)", b.Name, strings.Join(actions, ","))
			if b.ParamRef != nil {
				desc += " params=" + b.ParamRef.Name
			}
			bindings = append(bindings, desc)
		}
		if len(bindings) == 0 {
			bindings = []string{"-"}
		}
		t.Row(p.Name, strings.Join(targets, "; "), params, strings.Join(bindings, "; "))
	}
	return t.Render(w)
}

func (c *policyTableCodec) Decode(io.Reader, any) error {
	return errors.New("codec does not support decoding")
}
