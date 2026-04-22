package check

import (
	"errors"
	"io"

	"github.com/grafana/gcx/internal/fleet"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// CheckResult holds the outcome of a single preflight check.
type CheckResult struct {
	Check  string `json:"check"`
	Status string `json:"status"` // "OK" or "FAIL"
	Error  string `json:"error,omitempty"`
}

// checkTableCodec renders []CheckResult as a table.
type checkTableCodec struct{}

func (c *checkTableCodec) Format() format.Format { return "table" }

func (c *checkTableCodec) Encode(w io.Writer, v any) error {
	results, ok := v.([]CheckResult)
	if !ok {
		return errors.New("invalid data type for table codec: expected []CheckResult")
	}
	t := style.NewTable("CHECK", "STATUS", "ERROR")
	for _, r := range results {
		t.Row(r.Check, r.Status, r.Error)
	}
	return t.Render(w)
}

func (c *checkTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

type checkOpts struct {
	IO cmdio.Options
}

func (o *checkOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &checkTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

// TODO: add more preflight checks.
func newCommand(loader fleet.ConfigLoader) *cobra.Command {
	opts := &checkOpts{}
	cmd := &cobra.Command{
		Use:   "check <cluster>",
		Short: "Run a preflight check for a cluster's instrumentation configuration.",
		Long: `Run observable checks against a cluster's instrumentation state.

Exits zero when all checks pass, non-zero on any failure.

Checks performed:
  - cluster agent registered: verifies the cluster is known to the Fleet Management
    service by fetching its K8s instrumentation configuration.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			clusterName := args[0]
			ctx := cmd.Context()

			r, err := fleet.LoadClientWithStack(ctx, loader)
			if err != nil {
				return err
			}

			client := instrumentation.NewClient(r.Client)

			var results []CheckResult

			_, checkErr := client.GetK8SInstrumentation(ctx, clusterName)
			if checkErr != nil {
				results = append(results, CheckResult{
					Check:  "cluster agent registered",
					Status: "FAIL",
					Error:  checkErr.Error(),
				})
			} else {
				results = append(results, CheckResult{
					Check:  "cluster agent registered",
					Status: "OK",
				})
			}

			if err := opts.IO.Encode(cmd.OutOrStdout(), results); err != nil {
				return err
			}

			for _, r := range results {
				if r.Status == "FAIL" {
					var footer instrumentation.Footer
					footer.Hint("to register this cluster", "gcx instrumentation clusters setup "+clusterName)
					footer.Hint("to list registered clusters", "gcx instrumentation status")
					footer.Print(cmd.ErrOrStderr())
					return errors.New("instrumentation: one or more preflight checks failed")
				}
			}
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// Command returns the check cobra command.
func Command(loader *providers.ConfigLoader) *cobra.Command {
	return newCommand(loader)
}
