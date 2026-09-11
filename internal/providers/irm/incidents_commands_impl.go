package irm

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/deeplink"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/shared"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ---------------------------------------------------------------------------
// list command
// ---------------------------------------------------------------------------

type incidentListOpts struct {
	IO       cmdio.Options
	Limit    int
	Labels   []string
	DateFrom string
	DateTo   string
	Statuses []string
	Severity string
	Query    string
}

func (o *incidentListOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterTable(&o.IO, IncidentTable())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.IntVar(&o.Limit, "limit", 50, "Maximum number of incidents to return")
	flags.StringSliceVar(&o.Labels, "labels", nil, "Filter by label text or key:value, e.g. squad:mimir (may be repeated)")
	flags.StringVar(&o.DateFrom, "from", "", "Start of time range (RFC3339, unix timestamp, or relative e.g. now-7d)")
	flags.StringVar(&o.DateTo, "to", "", "End of time range (RFC3339, unix timestamp, or relative e.g. now)")
	flags.StringSliceVar(&o.Statuses, "status", nil, "Filter by status (active|resolved; repeatable, comma-separated)")
	flags.StringVar(&o.Severity, "severity", "", "Filter by severity label, e.g. major (see: gcx irm incidents severities list)")
	flags.StringVar(&o.Query, "query", "", "Raw incident query string, e.g. \"isdrill:true\"; cannot be combined with --labels, --status, or --severity")
}

func (o *incidentListOpts) Validate() error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if o.Limit < 1 {
		return fmt.Errorf("invalid --limit value %d: must be at least 1", o.Limit)
	}
	if o.Query != "" && (len(o.Labels) > 0 || len(o.Statuses) > 0 || o.Severity != "") {
		// --query is a raw escape hatch: the user writes the whole
		// query-string expression. The structured flags also compile into
		// query-string terms, so combining them would be ambiguous
		// (e.g. --status active --query status:resolved).
		return errors.New("--query cannot be combined with --labels, --status, or --severity")
	}
	// Labels are matched client-side against preview label objects as either
	// plain label text or key:value pairs.
	for _, l := range o.Labels {
		if strings.TrimSpace(l) == "" {
			return errors.New("invalid --labels value: label must not be empty")
		}
	}
	for _, s := range o.Statuses {
		if !isIncidentStatusFilter(s) {
			return fmt.Errorf("invalid --status value %q: must be active or resolved", s)
		}
	}
	// Severity is also double-quoted into the query-string language, which
	// cannot express a value containing a double quote.
	if strings.Contains(o.Severity, `"`) {
		return fmt.Errorf("invalid --severity value %q: cannot contain double quotes", o.Severity)
	}
	now := time.Now()
	if o.DateFrom != "" {
		if _, err := shared.ParseTime(o.DateFrom, now); err != nil {
			return fmt.Errorf("invalid --from value: %w", err)
		}
	}
	if o.DateTo != "" {
		if _, err := shared.ParseTime(o.DateTo, now); err != nil {
			return fmt.Errorf("invalid --to value: %w", err)
		}
	}
	return nil
}

const incidentListLong = `List incidents, most recent first.

--status and --severity are applied server-side. --labels and --from/--to are
applied client-side, one page at a time, so a highly selective --labels filter
can page through the full incident history before collecting --limit incidents.

--query is a raw query-string escape hatch and cannot be combined with the
structured --labels, --status, or --severity filters.`

func NewListCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &incidentListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List incidents.",
		Long:  incidentListLong,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			q := IncidentQuery{
				Limit:          opts.Limit,
				IncidentLabels: opts.Labels,
				Statuses:       opts.Statuses,
				Severity:       opts.Severity,
				QueryString:    opts.Query,
			}
			now := time.Now()
			if opts.DateFrom != "" {
				t, _ := shared.ParseTime(opts.DateFrom, now)
				ft := FlexTime(t)
				q.DateFrom = &ft
			}
			if opts.DateTo != "" {
				t, _ := shared.ParseTime(opts.DateTo, now)
				ft := FlexTime(t)
				q.DateTo = &ft
			}
			crud, restCfg, err := NewTypedCRUD(ctx, loader, q)
			if err != nil {
				return err
			}

			typedObjs, err := crud.List(ctx, int64(opts.Limit))
			if err != nil {
				return err
			}

			// Extract incidents from TypedObject
			incs := make([]Incident, len(typedObjs))
			for i := range typedObjs {
				incs[i] = typedObjs[i].Spec
			}

			// Table codec operates on raw []Incident for direct field access.
			// Other formats (yaml/json) convert to K8s envelope Resources
			// for consistency with get and round-trip support.
			if opts.IO.OutputFormat == "table" || opts.IO.OutputFormat == "wide" {
				return opts.IO.Encode(cmd.OutOrStdout(), incs)
			}

			// Initialized (not nil) so an empty result encodes as [] — never
			// null — for predictable agent-side parsing.
			objs := make([]unstructured.Unstructured, 0, len(incs))
			for _, inc := range incs {
				res, err := ToResource(inc, restCfg.Namespace)
				if err != nil {
					return fmt.Errorf("failed to convert incident %s to resource: %w", inc.IncidentID, err)
				}
				objs = append(objs, res.ToUnstructured())
			}

			return opts.IO.Encode(cmd.OutOrStdout(), objs)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// IncidentTable declares the incidents table. TITLE is truncated in the
// narrow table and shown in full in wide, so it appears once per format.
func IncidentTable() cmdio.Table[Incident] {
	created := func(inc Incident) string {
		t := time.Time(inc.CreatedTime)
		if t.IsZero() {
			return "-"
		}
		return t.Format("2006-01-02 15:04")
	}

	return cmdio.Table[Incident]{
		Columns: []cmdio.Column[Incident]{
			{Header: "INCIDENTID", Content: func(inc Incident) string { return inc.IncidentID }},
			{Header: "TITLE", Visible: cmdio.NarrowOnly, Content: func(inc Incident) string {
				return truncate(inc.Title, 50)
			}},
			{Header: "TITLE", Visible: cmdio.WideOnly, Content: func(inc Incident) string { return inc.Title }},
			{Header: "STATUS", Content: func(inc Incident) string { return inc.Status }},
			{Header: "SEVERITY", Content: func(inc Incident) string { return orDash(inc.Severity) }},
			{Header: "TYPE", Visible: cmdio.WideOnly, Content: func(inc Incident) string {
				return orDash(inc.IncidentType)
			}},
			{Header: "CREATED", Content: created},
		},
	}
}

// ---------------------------------------------------------------------------
// get command
// ---------------------------------------------------------------------------

type incidentGetOpts struct {
	IO cmdio.Options
}

func (o *incidentGetOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

func NewGetCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &incidentGetOpts{}
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get a single incident by ID.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			id := args[0]

			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := NewIncidentClient(restCfg)
			if err != nil {
				return err
			}

			inc, err := client.Get(ctx, id)
			if err != nil {
				return err
			}

			res, err := ToResource(*inc, restCfg.Namespace)
			if err != nil {
				return fmt.Errorf("failed to convert incident to resource: %w", err)
			}

			obj := res.ToUnstructured()
			return opts.IO.Encode(cmd.OutOrStdout(), &obj)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// get-pir command
// ---------------------------------------------------------------------------

type incidentPIROpts struct {
	IO cmdio.Options
}

func (o *incidentPIROpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("text", &IncidentPIRTextCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func (o *incidentPIROpts) Validate() error { return o.IO.Validate() }

const incidentGetPIRLong = `Resolve the post-incident review (PIR) document URL for an incident.

The URL is not carried in the incident payload. PIRs are optional and only the
Google Workspace integration creates them, so the link exists only where that
integration ran. It is recorded on the hook run that copied the PIR template,
which this command reads and resolves.

An incident can have more than one PIR document if the template was copied
again; the most recently created one is reported.

An incident without a PIR document prints nothing and exits 0.`

// NewGetPIRCommand builds `incidents get-pir <incident-id>`. A PIR is derived
// from its incident and has no independently addressable ID, so it is an
// operation-subject compound directly under `incidents` rather than a nested
// noun group.
func NewGetPIRCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &incidentPIROpts{}
	cmd := &cobra.Command{
		Use:   "get-pir <incident-id>",
		Short: "Get the post-incident review (PIR) document URL for an incident.",
		Long:  incidentGetPIRLong,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			incidentID := args[0]

			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := NewIncidentClient(restCfg)
			if err != nil {
				return err
			}

			url, err := client.GetPIRURL(ctx, incidentID)
			if err != nil {
				return err
			}

			// Absence is a normal outcome, not an error — the note is a
			// diagnostic so it stays out of the stdout result. Agents read the
			// empty pirURL instead, and stderr stays quiet for them.
			if url == "" && !agent.IsAgentMode() {
				cmdio.Info(cmd.ErrOrStderr(),
					"Incident %s has no PIR document (only the Google Workspace integration creates them).",
					incidentID)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), IncidentPIR{IncidentID: incidentID, PIRURL: url})
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// IncidentPIRTextCodec prints the bare PIR URL so the result can be piped;
// an incident without a PIR document produces no output.
type IncidentPIRTextCodec struct{}

func (c *IncidentPIRTextCodec) Format() format.Format { return "text" }

func (c *IncidentPIRTextCodec) Encode(w io.Writer, v any) error {
	pir, ok := v.(IncidentPIR)
	if !ok {
		return errors.New("invalid data type for text codec: expected IncidentPIR")
	}
	if pir.PIRURL == "" {
		return nil
	}
	_, err := fmt.Fprintln(w, pir.PIRURL)
	return err
}

func (c *IncidentPIRTextCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("text format does not support decoding")
}

// ---------------------------------------------------------------------------
// create command
// ---------------------------------------------------------------------------

type createOpts struct {
	IO   cmdio.Options
	File string
}

func (o *createOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the incident manifest (use - for stdin)")
}

func NewCreateCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &createOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new incident from a file.",
		Example: `  # Create an incident from a YAML manifest:
  cat <<EOF | gcx irm incidents create -f -
  apiVersion: incident.ext.grafana.app/v1alpha1
  kind: Incident
  metadata:
    name: my-incident
  spec:
    title: "Service degradation in production"
    status: active
    # The display label, not the identifier. Run
    # 'gcx irm incidents severities list' for the valid values.
    severity: Minor
    isDrill: false
    incidentType: internal
    labels:
      - key: team
        label: platform
      - key: env
        label: production
  EOF

  # Create from a file:
  gcx irm incidents create -f incident.yaml`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			if opts.File == "" {
				return errors.New("--filename/-f is required")
			}

			ctx := cmd.Context()

			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			// Read input from file or stdin.
			var reader io.Reader
			if opts.File == "-" {
				reader = cmd.InOrStdin()
			} else {
				f, err := os.Open(opts.File)
				if err != nil {
					return fmt.Errorf("failed to open file %s: %w", opts.File, err)
				}
				defer f.Close()
				reader = f
			}

			yamlCodec := format.NewYAMLCodec()
			var obj unstructured.Unstructured
			if err := yamlCodec.Decode(reader, &obj); err != nil {
				return fmt.Errorf("failed to parse input: %w", err)
			}

			res, err := resources.FromUnstructured(&obj)
			if err != nil {
				return fmt.Errorf("failed to build resource from input: %w", err)
			}

			inc, err := FromResource(res)
			if err != nil {
				return fmt.Errorf("failed to convert resource to incident: %w", err)
			}

			client, err := NewIncidentClient(restCfg)
			if err != nil {
				return err
			}

			created, err := client.Create(ctx, inc)
			if err != nil {
				// Create reports the incident next to the error when the
				// incident exists but a later step failed. That error already
				// names the incident and the repair command, so a
				// "failed to create incident" prefix would contradict it.
				if created != nil {
					return err
				}
				return fmt.Errorf("failed to create incident: %w", err)
			}

			createdRes, err := ToResource(*created, restCfg.Namespace)
			if err != nil {
				return fmt.Errorf("failed to convert created incident to resource: %w", err)
			}

			// The confirmation one-liner is a diagnostic, not the result —
			// stderr keeps it out of the stdout document (the echoed
			// incident below is the result).
			cmdio.Success(cmd.ErrOrStderr(), "Created incident %s (id=%s)", created.Title, created.IncidentID)
			createdObj := createdRes.ToUnstructured()
			return opts.IO.Encode(cmd.OutOrStdout(), &createdObj)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// close command
// ---------------------------------------------------------------------------

type closeOpts struct {
	IO     cmdio.Options
	loader GrafanaConfigLoader
}

func (o *closeOpts) setup(flags *pflag.FlagSet) {
	// The close result is a SingleMutation document through the codec
	// system: the default text codec reproduces the familiar
	// "Closed incident <id> (<title>)" line byte-for-byte; agent mode and
	// explicit -o json/yaml get the structured document.
	o.IO.RegisterCustomCodec("text", &singleMutationTextCodec{
		render: func(w io.Writer, m cmdio.SingleMutation) {
			cmdio.Success(w, "Closed incident %s (%s)", m.Target.ID, m.Target.Name)
		},
	})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func (o *closeOpts) Validate() error { return o.IO.Validate() }

func NewCloseCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &closeOpts{loader: loader}
	cmd := &cobra.Command{
		Use:   "close <id>",
		Short: "Close (resolve) an incident.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			id := args[0]

			restCfg, err := opts.loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := NewIncidentClient(restCfg)
			if err != nil {
				return err
			}

			updated, err := client.UpdateStatus(ctx, id, "resolved")
			if err != nil {
				return fmt.Errorf("failed to close incident %s: %w", id, err)
			}

			result := cmdio.NewSingleMutation("closed", cmdio.MutationTarget{
				Kind: "Incident",
				ID:   updated.IncidentID,
				Name: updated.Title,
			})
			changed := true
			result.Changed = &changed
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// open command
// ---------------------------------------------------------------------------

type openOpts struct {
	loader GrafanaConfigLoader
}

func (o *openOpts) setup(_ *pflag.FlagSet) {}
func (o *openOpts) Validate() error        { return nil }

func NewOpenCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &openOpts{loader: loader}
	cmd := &cobra.Command{
		Use:   "open <id>",
		Short: "Open an incident in the browser.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			id := args[0]

			restCfg, err := opts.loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			url := deeplink.Resolve(restCfg.GrafanaURL, incidentStaticDescriptor.GroupVersionKind(), id)
			if url == "" {
				return fmt.Errorf("no deep link URL available for incident %s", id)
			}

			cmdio.Info(cmd.ErrOrStderr(), "Opening %s", url)
			return deeplink.Open(url)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// activity commands
// ---------------------------------------------------------------------------

func NewActivityCommand(loader GrafanaConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "activity",
		Short: "Manage incident activity timeline.",
	}
	cmd.AddCommand(
		newActivityAddCommand(loader),
	)
	return cmd
}

type activityListOpts struct {
	IO    cmdio.Options
	Limit int
}

func (o *activityListOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterTable(&o.IO, ActivityTable())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.IntVar(&o.Limit, "limit", 50, "Maximum number of activity items to return")
}

// NewListActivityCommand builds `incidents list-activity <incident-id>`.
// Activity items are addressed by the parent incident's ID (they have no
// independently addressable ID of their own), so the collection is an
// operation-subject compound directly under `incidents` rather than a
// nested noun group.
func NewListActivityCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &activityListOpts{}
	cmd := &cobra.Command{
		Use:   "list-activity <incident-id>",
		Short: "List activity items for an incident.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			incidentID := args[0]

			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := NewIncidentClient(restCfg)
			if err != nil {
				return err
			}

			items, err := client.QueryActivity(ctx, incidentID, opts.Limit)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), items)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ActivityTable declares the incident activity table.
func ActivityTable() cmdio.Table[ActivityItem] {
	return cmdio.Table[ActivityItem]{
		Columns: []cmdio.Column[ActivityItem]{
			{Header: "ID", Content: func(a ActivityItem) string { return a.ActivityItemID }},
			{Header: "KIND", Content: func(a ActivityItem) string { return a.ActivityKind }},
			{Header: "USER", Content: func(a ActivityItem) string { return a.User.Name }},
			{Header: "TIME", Content: func(a ActivityItem) string {
				eventTime := a.EventTime
				if eventTime == "" {
					eventTime = a.CreatedTime
				}
				// Truncate to date+time if it is an ISO timestamp.
				if len(eventTime) > 16 {
					eventTime = eventTime[:16]
				}
				return eventTime
			}},
			{Header: "BODY", Content: func(a ActivityItem) string {
				// Newlines break the table layout.
				return strings.ReplaceAll(truncate(a.Body, 60), "\n", " ")
			}},
		},
	}
}

type activityAddOpts struct {
	IO   cmdio.Options
	Body string
}

func (o *activityAddOpts) setup(flags *pflag.FlagSet) {
	flags.StringVar(&o.Body, "body", "", "Note body to add")
	// The add result is a SingleMutation document through the codec system:
	// the default text codec reproduces the familiar "Added activity note to
	// incident <id>" line byte-for-byte; agent mode and explicit -o json/yaml
	// get the structured document.
	o.IO.RegisterCustomCodec("text", &singleMutationTextCodec{
		render: func(w io.Writer, m cmdio.SingleMutation) {
			cmdio.Success(w, "Added activity note to incident %s", m.Target.ID)
		},
	})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func (o *activityAddOpts) Validate() error {
	if o.Body == "" {
		return errors.New("--body is required")
	}
	return o.IO.Validate()
}

func newActivityAddCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &activityAddOpts{}
	cmd := &cobra.Command{
		Use:   "add <incident-id>",
		Short: "Add a note to an incident's activity timeline.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			incidentID := args[0]

			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := NewIncidentClient(restCfg)
			if err != nil {
				return err
			}

			if err := client.AddActivity(ctx, incidentID, opts.Body); err != nil {
				return fmt.Errorf("failed to add activity: %w", err)
			}

			result := cmdio.NewSingleMutation("add-activity", cmdio.MutationTarget{
				Kind: "Incident",
				ID:   incidentID,
			})
			changed := true
			result.Changed = &changed
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// severities commands
// ---------------------------------------------------------------------------

func NewSeveritiesCommand(loader GrafanaConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "severities",
		Short:   "Manage incident severity levels.",
		Aliases: []string{"severity"},
	}
	cmd.AddCommand(newSeveritiesListCommand(loader))
	return cmd
}

type severitiesListOpts struct {
	IO cmdio.Options
}

func (o *severitiesListOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterTable(&o.IO, SeverityTable())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newSeveritiesListCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &severitiesListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List severity levels for the organization.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := NewIncidentClient(restCfg)
			if err != nil {
				return err
			}

			severities, err := client.GetSeverities(ctx)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), severities)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// SeverityTable declares the severity levels table.
func SeverityTable() cmdio.Table[Severity] {
	return cmdio.Table[Severity]{
		Columns: []cmdio.Column[Severity]{
			{Header: "ID", Content: func(s Severity) string { return s.SeverityID }},
			{Header: "LEVEL", Content: func(s Severity) string { return strconv.Itoa(s.Level) }},
			{Header: "LABEL", Content: func(s Severity) string { return s.DisplayLabel }},
			{Header: "COLOR", Content: func(s Severity) string { return orDash(s.Color) }},
		},
	}
}

// ---------------------------------------------------------------------------
// list-contexts command
// ---------------------------------------------------------------------------

type contextsListOpts struct {
	IO           cmdio.Options
	Limit        int
	Type         string
	Status       string
	AlertGroupID string
}

func (o *contextsListOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterTable(&o.IO, IncidentContextTable())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.IntVar(&o.Limit, "limit", 0, "Maximum number of contexts to return (0 = server default)")
	flags.StringVar(&o.Type, "type", "", "Filter by context type (e.g. genericURL, grafana.dashboard, code.github.pr). Note: alert-group links are encoded as genericURL contexts with alertGroupID set — use --alert-group-id to filter those.")
	flags.StringVar(&o.Status, "status", "", "Filter by context status")
	flags.StringVar(&o.AlertGroupID, "alert-group-id", "", "Filter by linked alert group ID")
}

// NewListContextsCommand builds `incidents list-contexts <incident-id>`.
// Contexts are addressed by the parent incident's ID (there is no read-one
// or ContextID addressing in the exposed surface), so the collection is an
// operation-subject compound directly under `incidents` rather than a
// nested noun group.
func NewListContextsCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &contextsListOpts{}
	cmd := &cobra.Command{
		Use:   "list-contexts <incident-id>",
		Short: "List contexts attached to an incident.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			incidentID := args[0]

			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := NewIncidentClient(restCfg)
			if err != nil {
				return err
			}

			contexts, err := client.QueryIncidentContext(ctx, IncidentContextQuery{
				IncidentID:   incidentID,
				Limit:        opts.Limit,
				Type:         opts.Type,
				Status:       opts.Status,
				AlertGroupID: opts.AlertGroupID,
			})
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), contexts)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// IncidentContextTable declares the incident contexts table. TITLE is
// truncated in the narrow table and shown in full in wide.
func IncidentContextTable() cmdio.Table[IncidentContext] {
	title := func(ctx IncidentContext) string { return orDash(ctx.Title) }

	return cmdio.Table[IncidentContext]{
		Columns: []cmdio.Column[IncidentContext]{
			{Header: "CONTEXTID", Content: func(ctx IncidentContext) string { return ctx.ContextID }},
			{Header: "TYPE", Content: func(ctx IncidentContext) string { return orDash(ctx.Type) }},
			{Header: "STATUS", Content: func(ctx IncidentContext) string { return orDash(ctx.Status) }},
			{Header: "ALERTGROUPID", Content: func(ctx IncidentContext) string {
				if ctx.AlertGroupID == nil {
					return "-"
				}
				return orDash(*ctx.AlertGroupID)
			}},
			{Header: "TITLE", Visible: cmdio.NarrowOnly, Content: func(ctx IncidentContext) string {
				return truncate(title(ctx), 50)
			}},
			{Header: "TITLE", Visible: cmdio.WideOnly, Content: title},
			{Header: "CREATED", Visible: cmdio.WideOnly, Content: func(ctx IncidentContext) string {
				created := ctx.CreatedTime
				if created == "" {
					return "-"
				}
				if len(created) > 16 {
					created = created[:16]
				}
				return created
			}},
		},
	}
}
