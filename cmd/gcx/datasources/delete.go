package datasources

import (
	"errors"
	"fmt"
	"io"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/agent"
	dsclient "github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type deleteOpts struct {
	IO     cmdio.Options
	Force  bool
	DryRun bool
}

func (opts *deleteOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&opts.Force, "force", false, "Skip the confirmation prompt")
	flags.BoolVarP(&opts.Force, "yes", "y", false, "Skip the confirmation prompt")
	flags.BoolVar(&opts.DryRun, "dry-run", false, "Report what would be deleted without deleting")
	opts.IO.RegisterCustomCodec("text", &deleteResultCodec{})
	opts.IO.DefaultFormat("text")
	opts.IO.BindFlags(flags)
}

func (opts *deleteOpts) Validate() error {
	return opts.IO.Validate()
}

// deleteResult is the per-UID outcome reported as a mutation summary.
type deleteResult struct {
	UID     string `json:"uid" yaml:"uid"`
	Status  string `json:"status" yaml:"status"`
	Message string `json:"message,omitempty" yaml:"message,omitempty"`
}

func deleteCmd() *cobra.Command {
	configOpts := &cmdconfig.Options{}
	opts := &deleteOpts{}

	cmd := &cobra.Command{
		Use:   "delete UID...",
		Short: "Delete one or more datasources",
		Long: `Delete one or more datasources by UID.

Deletion prompts for confirmation unless --force/--yes, GCX_AUTO_APPROVE, or
agent mode is in effect.

Exit codes: 0 (all deleted), 4 (some deletions failed).`,
		Args: cobra.MinimumNArgs(1),
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "small",
			agent.AnnotationLLMHint:   "<uid> --yes",
		},
		Example: `
	# Delete one datasource (prompts to confirm)
	gcx datasources delete sentry-dev

	# Delete several without prompting
	gcx datasources delete sentry-dev sentry-staging --yes

	# Preview only
	gcx datasources delete sentry-dev --dry-run`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			restCfg, err := configOpts.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}
			transport, err := dsclient.NewTransport(restCfg)
			if err != nil {
				return err
			}

			if !opts.DryRun {
				proceed, err := providers.ConfirmDestructive(
					cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force,
					fmt.Sprintf("Delete %d datasource(s)?", len(args)))
				if err != nil {
					return err
				}
				if !proceed {
					return nil
				}
			}

			results := make([]*deleteResult, 0, len(args))
			failed := 0
			for _, uid := range args {
				res := &deleteResult{UID: uid}
				switch {
				case opts.DryRun:
					res.Status = "dry-run"
					res.Message = "would delete"
				default:
					switch err := transport.Delete(ctx, uid); {
					case err == nil:
						res.Status = "deleted"
					case dsclient.IsNotFound(err):
						res.Status = "failed"
						res.Message = "not found"
						failed++
					default:
						res.Status = "failed"
						res.Message = err.Error()
						failed++
					}
				}
				results = append(results, res)
			}

			if err := opts.IO.Encode(cmd.OutOrStdout(), results); err != nil {
				return err
			}
			if failed > 0 {
				return gcxerrors.NewPartialFailureError("delete", len(args), failed)
			}
			return nil
		},
	}

	configOpts.BindFlags(cmd.Flags())
	opts.setup(cmd.Flags())
	return cmd
}

type deleteResultCodec struct{}

func (c *deleteResultCodec) Format() format.Format { return "text" }

func (c *deleteResultCodec) Encode(w io.Writer, data any) error {
	results, ok := data.([]*deleteResult)
	if !ok {
		return errors.New("invalid data type for text codec")
	}
	t := style.NewTable("UID", "STATUS", "MESSAGE")
	for _, r := range results {
		t.Row(r.UID, r.Status, r.Message)
	}
	return t.Render(w)
}

func (c *deleteResultCodec) Decode(io.Reader, any) error {
	return errors.New("text codec does not support decoding")
}
