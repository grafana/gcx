package incidents

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/deeplink"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ---------------------------------------------------------------------------
// list command
// ---------------------------------------------------------------------------

type listOpts struct {
	IO    cmdio.Options
	Limit int
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &IncidentTableCodec{})
	o.IO.RegisterCustomCodec("wide", &IncidentTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.IntVar(&o.Limit, "limit", 50, "Maximum number of incidents to return")
}

func newListCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List incidents.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			crud, restCfg, err := NewTypedCRUD(ctx, loader, IncidentQuery{Limit: opts.Limit})
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

			var objs []unstructured.Unstructured
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

// IncidentTableCodec renders incidents as a tabular table.
type IncidentTableCodec struct {
	Wide bool
}

func (c *IncidentTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *IncidentTableCodec) Encode(w io.Writer, v any) error {
	incs, ok := v.([]Incident)
	if !ok {
		return errors.New("invalid data type for table codec: expected []Incident")
	}

	var tbl *style.TableBuilder
	if c.Wide {
		tbl = style.NewTable("INCIDENTID", "TITLE", "STATUS", "SEVERITY", "TYPE", "CREATED")
	} else {
		tbl = style.NewTable("INCIDENTID", "TITLE", "STATUS", "SEVERITY", "CREATED")
	}

	for _, inc := range incs {
		created := "-"
		t := time.Time(inc.CreatedTime)
		if !t.IsZero() {
			created = t.Format("2006-01-02 15:04")
		}

		severity := inc.Severity
		if severity == "" {
			severity = "-"
		}

		title := inc.Title
		if !c.Wide && len(title) > 50 {
			title = title[:47] + "..."
		}

		if c.Wide {
			incType := inc.IncidentType
			if incType == "" {
				incType = "-"
			}
			tbl.Row(inc.IncidentID, title, inc.Status, severity, incType, created)
		} else {
			tbl.Row(inc.IncidentID, title, inc.Status, severity, created)
		}
	}

	return tbl.Render(w)
}

func (c *IncidentTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// ---------------------------------------------------------------------------
// get command
// ---------------------------------------------------------------------------

type getOpts struct {
	IO cmdio.Options
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

func newGetCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &getOpts{}
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

			client, err := NewClient(restCfg)
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

func newCreateCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &createOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new incident from a file.",
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

			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}

			created, err := client.Create(ctx, inc)
			if err != nil {
				return fmt.Errorf("failed to create incident: %w", err)
			}

			createdRes, err := ToResource(*created, restCfg.Namespace)
			if err != nil {
				return fmt.Errorf("failed to convert created incident to resource: %w", err)
			}

			cmdio.Success(cmd.OutOrStdout(), "Created incident %s (id=%s)", created.Title, created.IncidentID)
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
	loader GrafanaConfigLoader
}

func (o *closeOpts) setup(_ *pflag.FlagSet) {}
func (o *closeOpts) Validate() error        { return nil }

func newCloseCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &closeOpts{loader: loader}
	cmd := &cobra.Command{
		Use:   "close <id>",
		Short: "Close (resolve) an incident.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			id := args[0]

			restCfg, err := opts.loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}

			updated, err := client.UpdateStatus(ctx, id, "resolved")
			if err != nil {
				return fmt.Errorf("failed to close incident %s: %w", id, err)
			}

			cmdio.Success(cmd.OutOrStdout(), "Closed incident %s (%s)", updated.IncidentID, updated.Title)
			return nil
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

func newOpenCommand(loader GrafanaConfigLoader) *cobra.Command {
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

			url := deeplink.Resolve(restCfg.GrafanaURL, staticDescriptor.GroupVersionKind(), id)
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

func newActivityCommand(loader GrafanaConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "activity",
		Short: "Manage incident activity timeline.",
	}
	cmd.AddCommand(
		newActivityListCommand(loader),
		newActivityAddCommand(loader),
	)
	return cmd
}

type activityListOpts struct {
	IO    cmdio.Options
	Limit int
}

func (o *activityListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &ActivityTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.IntVar(&o.Limit, "limit", 50, "Maximum number of activity items to return")
}

func newActivityListCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &activityListOpts{}
	cmd := &cobra.Command{
		Use:   "list <incident-id>",
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

			client, err := NewClient(restCfg)
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

// ActivityTableCodec renders activity items as a table.
type ActivityTableCodec struct{}

func (c *ActivityTableCodec) Format() format.Format { return "table" }

func (c *ActivityTableCodec) Encode(w io.Writer, v any) error {
	items, ok := v.([]ActivityItem)
	if !ok {
		return errors.New("invalid data type for table codec: expected []ActivityItem")
	}

	t := style.NewTable("ID", "KIND", "USER", "TIME", "BODY")
	for _, item := range items {
		body := item.Body
		if len(body) > 60 {
			body = body[:57] + "..."
		}
		// Newlines break the table layout.
		body = strings.ReplaceAll(body, "\n", " ")

		eventTime := item.EventTime
		if eventTime == "" {
			eventTime = item.CreatedTime
		}
		// Truncate to date+time if it's an ISO timestamp.
		if len(eventTime) > 16 {
			eventTime = eventTime[:16]
		}

		t.Row(item.ActivityItemID, item.ActivityKind, item.User.Name, eventTime, body)
	}

	return t.Render(w)
}

func (c *ActivityTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

type activityAddOpts struct {
	Body string
}

func (o *activityAddOpts) setup(flags *pflag.FlagSet) {
	flags.StringVar(&o.Body, "body", "", "Note body to add")
}

func (o *activityAddOpts) Validate() error {
	if o.Body == "" {
		return errors.New("--body is required")
	}
	return nil
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

			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}

			if err := client.AddActivity(ctx, incidentID, opts.Body); err != nil {
				return fmt.Errorf("failed to add activity: %w", err)
			}

			cmdio.Success(cmd.OutOrStdout(), "Added activity note to incident %s", incidentID)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// severities commands
// ---------------------------------------------------------------------------

func newSeveritiesCommand(loader GrafanaConfigLoader) *cobra.Command {
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
	o.IO.RegisterCustomCodec("table", &SeverityTableCodec{})
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

			client, err := NewClient(restCfg)
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

// SeverityTableCodec renders severity levels as a table.
type SeverityTableCodec struct{}

func (c *SeverityTableCodec) Format() format.Format { return "table" }

func (c *SeverityTableCodec) Encode(w io.Writer, v any) error {
	sevs, ok := v.([]Severity)
	if !ok {
		return errors.New("invalid data type for table codec: expected []Severity")
	}

	t := style.NewTable("ID", "LEVEL", "LABEL", "COLOR")
	for _, s := range sevs {
		color := s.Color
		if color == "" {
			color = "-"
		}
		t.Row(s.SeverityID, strconv.Itoa(s.Level), s.DisplayLabel, color)
	}
	return t.Render(w)
}

func (c *SeverityTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}
