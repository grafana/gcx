package datasources

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/query/dsabstraction"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// SQLCmd returns the `sql` command group, which mounts the abstraction-API
// subcommands (query, schema) so all cross-datasource SQL tooling stays
// together under `gcx datasources sql query`.
func SQLCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sql",
		Short: "Cross-datasource SQL via the dsabstraction.grafana.app API",
		Long: `Tooling for the dsabstraction.grafana.app API: execute cross-datasource
SQL queries and introspect the schema datasources expose to that engine.`,
	}
	cmd.AddCommand(sqlQueryCmd())
	cmd.AddCommand(sqlSchemaCmd())
	return cmd
}

// sqlQueryCmd returns the `sql query` subcommand for executing
// cross-datasource SQL via the dsabstraction.grafana.app API.
func sqlQueryCmd() *cobra.Command {
	configOpts := &cmdconfig.Options{}
	opts := &sqlOpts{}

	cmd := &cobra.Command{
		Use:   "query [SQL]",
		Short: "Execute a SQL query against the dsabstraction API",
		Long: `Execute a SQL query that can reference one or more datasources via
the dsabstraction.grafana.app API.

The SQL string can be passed as a positional argument, via --query, via
--query-file, or piped on stdin. Datasources are referenced inside the FROM
clause as ` + "`<type>::<uid>`.`<table>`" + `, e.g.
` + "`prometheus::abc123`.`up`" + `.

Requires a Grafana that exposes the dsabstraction.grafana.app/v1alpha1 API.`,
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "medium",
			agent.AnnotationLLMHint:   "gcx datasources sql query 'SELECT timestamp, value, job FROM `prometheus::UID`.`up` LIMIT 10' --from now-5m --to now",
		},
		Example: `
  # Inline SQL with a relative time range
  gcx datasources sql query 'SELECT timestamp, value, job FROM ` + "`prometheus::UID`.`up`" + ` LIMIT 10' --from now-5m --to now

  # Read SQL from a file
  gcx datasources sql query --query-file query.sql --since 1h

  # Pipe SQL on stdin
  echo 'SELECT 1' | gcx datasources sql query --from now-5m --to now

  # Disable server-side pushdown (for A/B comparison)
  gcx datasources sql query 'SELECT job, SUM(value) FROM ` + "`prometheus::UID`.`up`" + ` GROUP BY job' \
      --from now-5m --to now --pushdown=false

  # Show the pushdown plan that the server reports
  gcx datasources sql query 'SELECT 1' --from now-5m --to now --show-plan`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			sql, err := opts.ResolveSQL(cmd.InOrStdin(), args)
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			cfg, err := configOpts.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := dsabstraction.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			req := dsabstraction.SQLRequest{
				SQL:      sql,
				From:     opts.From,
				To:       opts.To,
				Pushdown: opts.pushdownPtr(),
				Cookie:   opts.Cookie,
			}

			resp, err := client.Query(ctx, req)
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			view := &sqlView{
				Schema: resp.Schema,
				Data:   resp.Data,
			}
			if opts.ShowPlan {
				plan, err := resp.ParsePushdownPlan()
				if err != nil {
					return fmt.Errorf("failed to parse pushdown plan: %w", err)
				}
				view.PushdownPlan = plan
				view.HasPlan = true
			}

			return opts.IO.Encode(cmd.OutOrStdout(), view)
		},
	}

	configOpts.BindFlags(cmd.Flags())
	opts.setup(cmd.Flags())
	return cmd
}

type sqlOpts struct {
	IO        cmdio.Options
	From      string
	To        string
	Since     string
	Query     string
	QueryFile string
	Pushdown  string // "" (unset), "true", "false"
	ShowPlan  bool
	Cookie    string
}

func (opts *sqlOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &sqlTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVar(&opts.From, "from", "", "Start time (RFC3339, Unix timestamp, or relative like 'now-1h')")
	flags.StringVar(&opts.To, "to", "", "End time (RFC3339, Unix timestamp, or relative like 'now')")
	flags.StringVar(&opts.Since, "since", "", "Duration before --to (or now if omitted); mutually exclusive with --from")
	flags.StringVar(&opts.Query, "query", "", "SQL query (alternative to positional argument or stdin)")
	flags.StringVar(&opts.QueryFile, "query-file", "", "Path to a file containing the SQL query")
	flags.StringVar(&opts.Pushdown, "pushdown", "", "Override server-side pushdown ('true' or 'false'); leave unset for server default")
	flags.BoolVar(&opts.ShowPlan, "show-plan", false, "Print the pushdown plan reported by the server before the result")
	flags.StringVar(&opts.Cookie, "cookie", "", "Literal Cookie header to attach to the request (e.g. 'grafana_session=abc123'); intended for local dev where the apiserver expects cookie auth")
}

func (opts *sqlOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	tr := dsquery.TimeRangeOpts{From: opts.From, To: opts.To, Since: opts.Since}
	if err := tr.ValidateTimeRange(); err != nil {
		return err
	}
	opts.From, opts.To = tr.From, tr.To

	if opts.From == "" || opts.To == "" {
		return errors.New("--from and --to (or --since) are required")
	}

	if opts.Pushdown != "" && opts.Pushdown != "true" && opts.Pushdown != "false" {
		return fmt.Errorf("--pushdown must be 'true' or 'false', got %q", opts.Pushdown)
	}
	return nil
}

func (opts *sqlOpts) pushdownPtr() *bool {
	switch opts.Pushdown {
	case "true":
		v := true
		return &v
	case "false":
		v := false
		return &v
	default:
		return nil
	}
}

// ResolveSQL collects the SQL from the available sources, in priority order:
// --query-file, --query, positional arg, stdin (when not a TTY). At most one
// source may be specified.
func (opts *sqlOpts) ResolveSQL(stdin io.Reader, args []string) (string, error) {
	sources := 0
	if opts.QueryFile != "" {
		sources++
	}
	if opts.Query != "" {
		sources++
	}
	if len(args) > 0 {
		sources++
	}
	if sources > 1 {
		return "", errors.New("provide SQL via exactly one of: positional arg, --query, --query-file, or stdin")
	}

	switch {
	case opts.QueryFile != "":
		b, err := os.ReadFile(opts.QueryFile)
		if err != nil {
			return "", fmt.Errorf("failed to read --query-file: %w", err)
		}
		return strings.TrimSpace(string(b)), nil
	case opts.Query != "":
		return strings.TrimSpace(opts.Query), nil
	case len(args) > 0:
		return strings.TrimSpace(args[0]), nil
	}

	if s, ok, err := readPipedSQL(stdin); err != nil {
		return "", err
	} else if ok {
		return s, nil
	}

	return "", errors.New("SQL is required: provide it as a positional argument, --query, --query-file, or piped on stdin")
}

// readPipedSQL reads SQL from a non-TTY stdin. Returns (sql, true, nil) on a
// usable read, (_, false, nil) when stdin is a TTY or empty, and a wrapped
// error on read failure.
func readPipedSQL(stdin io.Reader) (string, bool, error) {
	f, ok := stdin.(*os.File)
	if !ok {
		return "", false, nil
	}
	// If Stat fails for any reason, treat stdin as not piped — we don't want
	// a stat error on /dev/stdin to break commands that already supplied the
	// SQL via a flag or arg.
	fi, statErr := f.Stat()
	if statErr != nil || (fi.Mode()&os.ModeCharDevice) != 0 {
		return "", false, nil //nolint:nilerr // see comment above
	}
	b, err := io.ReadAll(stdin)
	if err != nil {
		return "", false, fmt.Errorf("failed to read SQL from stdin: %w", err)
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "", false, nil
	}
	return s, true, nil
}

// sqlView is the codec-agnostic shape for a SQL query response. The frame
// itself (Schema/Data) is always present; PushdownPlan is set only when the
// user passed --show-plan and is parsed from schema.meta.custom.pushdownPlan.
// HasPlan disambiguates "plan present but empty" (server reports no
// pushdown — common when --pushdown=false) from "plan not requested".
type sqlView struct {
	PushdownPlan []dsabstraction.PushdownPlanEntry `json:"pushdownPlan,omitempty" yaml:"pushdownPlan,omitempty"`
	HasPlan      bool                              `json:"-" yaml:"-"`
	Schema       dsabstraction.FrameSchema         `json:"schema" yaml:"schema"`
	Data         dsabstraction.FrameData           `json:"data" yaml:"data"`
}

type sqlTableCodec struct{}

func (c *sqlTableCodec) Format() format.Format { return "table" }

func (c *sqlTableCodec) Encode(w io.Writer, data any) error {
	v, ok := data.(*sqlView)
	if !ok {
		return errors.New("invalid data type for table codec")
	}

	if v.HasPlan {
		if err := renderPushdownPlanTable(w, v.PushdownPlan); err != nil {
			return err
		}
	}

	fields := make([]dsquery.FrameField, len(v.Schema.Fields))
	for i, f := range v.Schema.Fields {
		fields[i] = dsquery.FrameField{Name: f.Name, Type: f.Type}
	}
	return dsquery.FormatFrameTable(w, fields, v.Data.Values)
}

func renderPushdownPlanTable(w io.Writer, plan []dsabstraction.PushdownPlanEntry) error {
	if len(plan) == 0 {
		_, err := fmt.Fprintln(w, "(no pushdown plan reported)")
		return err
	}
	t := style.NewTable("HANDLER", "NODE", "PUSHED", "REASON")
	for _, e := range plan {
		pushed := "no"
		if e.Pushed {
			pushed = "yes"
		}
		t.Row(e.Handler, e.Node, pushed, e.Reason)
	}
	if err := t.Render(w); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w)
	return err
}

func (c *sqlTableCodec) Decode(io.Reader, any) error {
	return errors.New("table codec does not support decoding")
}
