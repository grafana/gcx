package datasources

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/agent"
	dsclient "github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type healthOpts struct {
	IO   cmdio.Options
	Type string
}

func (opts *healthOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &healthTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)
	flags.StringVarP(&opts.Type, "type", "t", "", "Filter by datasource type (e.g., prometheus, grafana-sentry-datasource)")
}

func (opts *healthOpts) Validate() error {
	return opts.IO.Validate()
}

// healthRow is the per-datasource health result for output.
type healthRow struct {
	UID     string `json:"uid" yaml:"uid"`
	Name    string `json:"name" yaml:"name"`
	Type    string `json:"type" yaml:"type"`
	Status  string `json:"status" yaml:"status"`
	Message string `json:"message" yaml:"message"`
}

// healthResult is the single shape passed to every codec for `datasources
// health`. JSON/YAML serialize the envelope; the table codec extracts .Results
// to render rows (Pattern 13: format-agnostic data).
type healthResult struct {
	Results []*healthRow `json:"results" yaml:"results"`
}

func healthCmd() *cobra.Command {
	configOpts := &cmdconfig.Options{}
	opts := &healthOpts{}

	cmd := &cobra.Command{
		Use:   "health [UID]",
		Short: "Check the health of one or more datasources",
		Long: `Check datasource health via the Grafana datasource health endpoint.

With a UID, checks a single datasource. Without arguments, checks all
datasources. Use --type to check all datasources of a given plugin type.

Exit codes distinguish resource failure from command failure:
  0 - all checked datasources are healthy
  4 - the check ran but one or more datasources are unhealthy (resource failure)
  1/2/3 - the check could not run (operational, usage, or auth failure)`,
		Args: cobra.MaximumNArgs(1),
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "medium",
			agent.AnnotationLLMHint:   "--type prometheus -o json",
		},
		Example: `
	# Check a single datasource
	gcx datasources health my-ds-uid

	# Check all datasources
	gcx datasources health

	# Check all datasources of a given type
	gcx datasources health --type grafana-sentry-datasource`,
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

			// Resolve the set of datasources to check.
			targets, err := resolveHealthTargets(ctx, transport, args, opts.Type)
			if err != nil {
				return err
			}
			if len(targets) == 0 {
				return errors.New("no datasources found to check")
			}

			rows := make([]*healthRow, 0, len(targets))
			failed := 0
			for _, ds := range targets {
				row := &healthRow{UID: ds.UID, Name: ds.Name, Type: ds.Type}
				result, herr := transport.Health(ctx, ds.UID)
				if herr != nil {
					row.Status = "ERROR"
					row.Message = herr.Error()
					failed++
				} else {
					row.Status = strings.ToUpper(result.Status)
					row.Message = result.Message
					if !isHealthy(result.Status) {
						failed++
					}
				}
				rows = append(rows, row)
			}

			if err := opts.IO.Encode(cmd.OutOrStdout(), &healthResult{Results: rows}); err != nil {
				return err
			}

			if failed > 0 {
				return gcxerrors.NewPartialFailureError("health-check", len(targets), failed)
			}
			return nil
		},
	}

	configOpts.BindFlags(cmd.Flags())
	opts.setup(cmd.Flags())
	return cmd
}

// resolveHealthTargets returns the datasources to health-check based on the
// optional UID argument and --type filter.
func resolveHealthTargets(ctx context.Context, client dsclient.Transport, args []string, typeFilter string) ([]*dsclient.Datasource, error) {
	// Single datasource by UID.
	if len(args) == 1 {
		ds, err := client.GetByUID(ctx, args[0])
		if err != nil {
			return nil, fmt.Errorf("failed to get datasource %q: %w", args[0], err)
		}
		return []*dsclient.Datasource{ds}, nil
	}

	// All datasources, optionally filtered by type.
	all, err := client.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list datasources: %w", err)
	}

	if typeFilter == "" {
		return all, nil
	}

	filtered := make([]*dsclient.Datasource, 0, len(all))
	for _, ds := range all {
		if strings.EqualFold(ds.Type, typeFilter) {
			filtered = append(filtered, ds)
		}
	}
	return filtered, nil
}

func isHealthy(status string) bool {
	switch strings.ToLower(status) {
	case "ok", "success", "healthy":
		return true
	default:
		return false
	}
}

type healthTableCodec struct{}

func (c *healthTableCodec) Format() format.Format { return "table" }

func (c *healthTableCodec) Encode(w io.Writer, data any) error {
	result, ok := data.(*healthResult)
	if !ok {
		return errors.New("invalid data type for table codec")
	}
	t := style.NewTable("UID", "NAME", "TYPE", "STATUS", "MESSAGE")
	for _, r := range result.Results {
		t.Row(r.UID, r.Name, r.Type, r.Status, r.Message)
	}
	return t.Render(w)
}

func (c *healthTableCodec) Decode(io.Reader, any) error {
	return errors.New("table codec does not support decoding")
}
