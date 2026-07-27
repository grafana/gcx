package kg

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// defaultQualityEntityType is the only entity type that currently has quality
// reports.
const defaultQualityEntityType = "Service"

// newQualityCommand builds the 'kg quality' command tree for reading KG entity
// quality reports.
func newQualityCommand(loader RESTConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "quality",
		Short: "Inspect Knowledge Graph entity quality reports.",
		Long: `Inspect Knowledge Graph entity quality reports.

Quality reports grade how well an entity is instrumented — each report is a set
of checks (request metrics, service-map data, deployment environment, span
cardinality, logs, profiles, ...) with an overall quality percentage. Use
'list' to rank entities by quality, and 'get' to see the failing checks and
remediation links for one entity.`,
	}
	cmd.AddCommand(newQualityListCommand(loader), newQualityGetCommand(loader))
	return cmd
}

// ---------------------------------------------------------------------------
// quality list
// ---------------------------------------------------------------------------

type qualityListOpts struct {
	IO           cmdio.Options
	Type         string
	Env          string
	Namespace    string
	Site         string
	EntityName   string
	FailedChecks []string
	Sort         string
	Page         int
	PageSize     int
}

func (o *qualityListOpts) setup(flags *pflag.FlagSet) {
	flags.StringVar(&o.Type, "type", defaultQualityEntityType, "Entity type to filter by")
	flags.StringVar(&o.Env, "env", "", "Environment scope")
	flags.StringVar(&o.Namespace, "namespace", "", "Namespace scope")
	flags.StringVar(&o.Site, "site", "", "Site scope")
	flags.StringVar(&o.EntityName, "entity", "", "Filter by entity name")
	flags.StringArrayVar(&o.FailedChecks, "failed-check", nil, "Only report entities with these failed check IDs; repeatable")
	flags.StringVar(&o.Sort, "sort", "asc", "Sort by quality percent: asc (worst first) or desc (best first)")
	flags.IntVar(&o.Page, "page", 0, "Page number (0-based)")
	flags.IntVar(&o.PageSize, "page-size", 25, "Page size (1-100)")

	o.IO.RegisterCustomCodec("table", &QualityReportListTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func (o *qualityListOpts) sortDirection() (string, error) {
	switch strings.ToLower(o.Sort) {
	case "asc":
		return "ASC", nil
	case "desc":
		return "DESC", nil
	default:
		return "", fmt.Errorf("invalid --sort %q: must be 'asc' or 'desc'", o.Sort)
	}
}

func newQualityListCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &qualityListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List entity quality reports, ranked by quality percent.",
		Example: `  gcx kg quality list --env <env>
  gcx kg quality list --env <env> --namespace <namespace> --sort asc
  gcx kg quality list --env <env> --failed-check span-metrics --failed-check service-graph-metrics
  gcx kg quality list --env <env> --json entityName,qualityPercent,failedCheckIds`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			dir, err := opts.sortDirection()
			if err != nil {
				return err
			}
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			page, err := client.ListQualityReports(cmd.Context(), QualityReportQuery{
				Type:           opts.Type,
				Env:            opts.Env,
				Namespace:      opts.Namespace,
				Site:           opts.Site,
				EntityName:     opts.EntityName,
				FailedCheckIDs: opts.FailedChecks,
				SortDirection:  dir,
				Page:           opts.Page,
				PageSize:       opts.PageSize,
			})
			if err != nil {
				return err
			}
			// Return the items slice (not the page envelope) so --json/--jq
			// field selection operates on report fields; surface pagination
			// as a stderr hint, mirroring 'kg entities list'.
			if page.TotalPages > 1 {
				fmt.Fprintf(cmd.ErrOrStderr(), "hint: page %d of %d (%d total reports) — use --page/--page-size to see more\n",
					page.Number+1, page.TotalPages, page.TotalElements)
			}
			return opts.IO.Encode(cmd.OutOrStdout(), page.Content)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// quality get
// ---------------------------------------------------------------------------

type qualityGetOpts struct {
	IO        cmdio.Options
	Type      string
	Env       string
	Namespace string
	Site      string
}

func (o *qualityGetOpts) setup(flags *pflag.FlagSet) {
	flags.StringVar(&o.Type, "type", defaultQualityEntityType, "Entity type")
	flags.StringVar(&o.Env, "env", "", "Environment scope (required)")
	flags.StringVar(&o.Namespace, "namespace", "", "Namespace scope")
	flags.StringVar(&o.Site, "site", "", "Site scope")

	o.IO.RegisterCustomCodec("table", &QualityReportTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func (o *qualityGetOpts) Validate() error {
	if o.Env == "" {
		return errors.New("--env is required")
	}
	return nil
}

func newQualityGetCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &qualityGetOpts{}
	cmd := &cobra.Command{
		Use:   "get <entity-name>",
		Short: "Get the full quality report for a single entity.",
		Args:  cobra.ExactArgs(1),
		Example: `  gcx kg quality get my-service --env <env>
  gcx kg quality get my-service --env <env> --namespace <namespace>
  gcx kg quality get my-service --env <env> --yaml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			if err := opts.Validate(); err != nil {
				return err
			}
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			report, err := client.GetEntityQualityReport(cmd.Context(), opts.Type, args[0], opts.Env, opts.Namespace, opts.Site)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), report)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// Codecs
// ---------------------------------------------------------------------------

// qualityScopeStr formats the env/namespace/site scope fields as a compact
// "key=value" string.
func qualityScopeStr(env, namespace, site string) string {
	var parts []string
	if env != "" {
		parts = append(parts, "env="+env)
	}
	if namespace != "" {
		parts = append(parts, "namespace="+namespace)
	}
	if site != "" {
		parts = append(parts, "site="+site)
	}
	return strings.Join(parts, ", ")
}

// QualityReportListTableCodec renders a list of quality reports as a table.
type QualityReportListTableCodec struct{}

func (c *QualityReportListTableCodec) Format() format.Format { return "table" }

func (c *QualityReportListTableCodec) Encode(w io.Writer, v any) error {
	items, ok := v.([]QualityReportListItem)
	if !ok {
		return errors.New("invalid data type for table codec: expected []QualityReportListItem")
	}
	t := style.NewTable("ENTITY", "TYPE", "SCOPE", "QUALITY", "FAILED CHECKS")
	for _, item := range items {
		t.Row(
			item.EntityName,
			item.EntityType,
			qualityScopeStr(item.Env, item.Namespace, item.Site),
			strconv.Itoa(item.QualityPercent)+"%",
			strings.Join(item.FailedCheckIDs, ", "),
		)
	}
	return t.Render(w)
}

func (c *QualityReportListTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// QualityReportTableCodec renders a single entity quality report as a summary
// header plus a table of checks.
type QualityReportTableCodec struct{}

func (c *QualityReportTableCodec) Format() format.Format { return "table" }

func (c *QualityReportTableCodec) Encode(w io.Writer, v any) error {
	r, ok := v.(*QualityReport)
	if !ok {
		return errors.New("invalid data type for table codec: expected *QualityReport")
	}
	fmt.Fprintf(w, "Entity:  %s (%s)\n", r.EntityName, r.EntityType)
	if scope := qualityScopeStr(r.Env, r.Namespace, r.Site); scope != "" {
		fmt.Fprintf(w, "Scope:   %s\n", scope)
	}
	fmt.Fprintf(w, "Quality: %d%%\n\n", r.QualityPercent)

	t := style.NewTable("CHECK", "STATE", "IMPACT", "TITLE")
	if r.ReportData != nil {
		for _, chk := range r.ReportData.Results {
			t.Row(chk.ID, string(chk.State), chk.Impact, chk.Title)
		}
	}
	if err := t.Render(w); err != nil {
		return err
	}
	if len(r.FailedCheckIDs) > 0 {
		fmt.Fprintf(w, "\nFailed checks: %s\n", strings.Join(r.FailedCheckIDs, ", "))
	}

	// Detail footer: the table columns can't hold the description and
	// remediation links without truncating, so print them as a block for each
	// non-passing check. Query templates stay JSON/YAML-only.
	writeQualityCheckDetails(w, r)
	return nil
}

// writeQualityCheckDetails prints a description + doc/reference footer for each
// non-SUCCESS check, giving the default table the actionable metadata that was
// previously only reachable via -o json/yaml.
func writeQualityCheckDetails(w io.Writer, r *QualityReport) {
	if r.ReportData == nil {
		return
	}
	printed := false
	for _, chk := range r.ReportData.Results {
		if chk.State == QualityStateSuccess {
			continue
		}
		if chk.Description == "" && chk.DocURL == "" && chk.Reference == nil {
			continue
		}
		if !printed {
			fmt.Fprintln(w, "\nDetails:")
			printed = true
		}
		fmt.Fprintf(w, "\n  %s — %s\n", chk.ID, chk.Title)
		if chk.Description != "" {
			fmt.Fprintf(w, "    %s\n", chk.Description)
		}
		if chk.DocURL != "" {
			fmt.Fprintf(w, "    docs: %s\n", chk.DocURL)
		}
		if chk.Reference != nil && chk.Reference.URL != "" {
			if chk.Reference.Title != "" {
				fmt.Fprintf(w, "    ref:  %s (%s)\n", chk.Reference.Title, chk.Reference.URL)
			} else {
				fmt.Fprintf(w, "    ref:  %s\n", chk.Reference.URL)
			}
		}
	}
}

func (c *QualityReportTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}
