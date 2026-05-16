package datasources

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/agent"
	dsclient "github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type healthOpts struct {
	IO cmdio.Options
}

func (opts *healthOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &healthTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)
}

func (opts *healthOpts) Validate() error {
	return opts.IO.Validate()
}

func healthCmd() *cobra.Command {
	configOpts := &cmdconfig.Options{}
	opts := &healthOpts{}

	cmd := &cobra.Command{
		Use:   "health DATASOURCE_UID",
		Short: "Check datasource health",
		Long:  "Run a health check against a datasource by its UID using the Grafana health check API.",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("required argument: datasource UID\n\nUsage: gcx datasources health <uid>\n\nTip: run 'gcx datasources list' to see available datasource UIDs")
			}
			if len(args) > 1 {
				return fmt.Errorf("expected exactly one datasource UID, got %d", len(args))
			}
			return nil
		},
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "small",
			agent.AnnotationLLMHint:   "-o json",
		},
		Example: `
	# Check datasource health
	gcx datasources health my-prometheus-uid

	# Output as JSON
	gcx datasources health my-prometheus-uid -o json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			uid := args[0]

			restCfg, err := configOpts.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			dsClient, err := dsclient.NewClient(restCfg)
			if err != nil {
				return err
			}

			info := &healthInfo{UID: uid}

			host := strings.TrimRight(restCfg.Host, "/")

			// Fetch datasource metadata — only treat 404 as "not found";
			// propagate auth/transport errors so centralized error conversion
			// can return the correct exit code and guidance.
			ds, dsErr := dsClient.GetByUID(ctx, uid)
			if dsErr != nil {
				var apiErr *dsclient.APIError
				if !errors.As(dsErr, &apiErr) || apiErr.StatusCode != http.StatusNotFound {
					return fmt.Errorf("failed to get datasource: %w", dsErr)
				}
				info.Status = "ERROR"
				info.Message = fmt.Sprintf("datasource %q not found", uid)
				info.Actions = []string{"Run 'gcx datasources list' to see available datasources"}
			}

			if ds != nil {
				info.Name = ds.Name
				info.Type = ds.Type
				info.URL = host + datasourceConfigPath + uid
				info.Actions = []string{"Settings: " + info.URL}

				result, err := dsClient.Health(ctx, uid)
				if err != nil {
					info.Status = "ERROR"
					info.Message = fmt.Sprintf("health check failed: %v", err)
				} else {
					info.Status = result.Status
					info.Message = result.Message
				}
			}

			return opts.IO.Encode(cmd.OutOrStdout(), info)
		},
	}

	configOpts.BindFlags(cmd.Flags())
	opts.setup(cmd.Flags())
	return cmd
}

const datasourceConfigPath = "/connections/datasources/edit/"

type healthInfo struct {
	UID     string   `json:"uid" yaml:"uid"`
	Name    string   `json:"name,omitempty" yaml:"name,omitempty"`
	Type    string   `json:"type,omitempty" yaml:"type,omitempty"`
	Status  string   `json:"status" yaml:"status"`
	Message string   `json:"message" yaml:"message"`
	URL     string   `json:"url,omitempty" yaml:"url,omitempty"`
	Actions []string `json:"actions,omitempty" yaml:"actions,omitempty"`
}

type healthTableCodec struct{}

func (c *healthTableCodec) Format() format.Format {
	return "table"
}

func (c *healthTableCodec) Encode(w io.Writer, data any) error {
	info, ok := data.(*healthInfo)
	if !ok {
		return errors.New("invalid data type for health table codec")
	}

	isErr := info.Status != "OK"
	status := style.ColorCell(info.Status, false, isErr)

	var buf strings.Builder
	for i, a := range info.Actions {
		if i > 0 {
			buf.WriteByte('\n')
		}
		buf.WriteString("• ")
		buf.WriteString(a)
	}
	actions := buf.String()

	t := style.NewTable("UID", "NAME", "TYPE", "STATUS", "MESSAGE", "ACTIONS")
	t.Row(info.UID, info.Name, info.Type, status, info.Message, actions)

	return t.Render(w)
}

func (c *healthTableCodec) Decode(io.Reader, any) error {
	return errors.New("health table codec does not support decoding")
}
