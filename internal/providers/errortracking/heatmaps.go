package errortracking

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/shared"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type heatmap struct {
	ID             int64       `json:"id"`
	Name           string      `json:"name"`
	CustomFieldKey string      `json:"customFieldKey"`
	WindowDays     int64       `json:"windowDays"`
	ProjectIDs     list[int64] `json:"projectIds"`
	CreatedAt      string      `json:"createdAt"`
	UpdatedAt      string      `json:"updatedAt"`
}

func (h heatmap) GetResourceName() string {
	return resourceName(h.Name, h.ID)
}

func (h heatmap) shown() string {
	if name := printable(h.Name); name != "" {
		return fmt.Sprintf("heatmap %s (ID %d)", name, h.ID)
	}
	return fmt.Sprintf("heatmap %d", h.ID)
}

type heatmapObject = adapter.TypedObject[heatmap]

type heatmapRequest struct {
	Name           *string  `json:"name,omitempty"`
	CustomFieldKey *string  `json:"customFieldKey,omitempty"`
	WindowDays     *int64   `json:"windowDays,omitempty"`
	ProjectIDs     *[]int64 `json:"projectIds,omitempty"`
}

type heatmapCell struct {
	Bucket     string `json:"bucket"`
	Value      string `json:"value"`
	EventCount int64  `json:"eventCount"`
}

type heatmapReport struct {
	Bucket string            `json:"bucket"`
	From   string            `json:"from"`
	To     string            `json:"to"`
	Cells  list[heatmapCell] `json:"cells"`

	ListMeta *cmdio.ListMeta `json:"list_meta,omitempty"`
}

type heatmapBreakdownProject struct {
	ProjectID   int64  `json:"projectId"`
	ProjectName string `json:"projectName"`
	EventCount  int64  `json:"eventCount"`
}

type heatmapBreakdown struct {
	From     string                        `json:"from"`
	To       string                        `json:"to"`
	Value    string                        `json:"value"`
	Projects list[heatmapBreakdownProject] `json:"projects"`
}

func (c *client) getHeatmap(ctx context.Context, id int64) (heatmap, error) {
	var h heatmap
	err := c.do(ctx, http.MethodGet, routePath("heatmaps", id), nil, nil, &h)
	return h, err
}

func (c *client) listHeatmaps(ctx context.Context) ([]heatmap, error) {
	var out struct {
		Heatmaps []heatmap `json:"heatmaps"`
	}
	if err := c.do(ctx, http.MethodGet, "heatmaps", nil, nil, &out); err != nil {
		return nil, err
	}
	return out.Heatmaps, nil
}

type heatmapRef struct {
	ref

	use string
}

func (r *heatmapRef) validate(cmd *cobra.Command) error {
	r.use = cmd.CommandPath() + " {id}"
	return r.parse(cmd, "<heatmap>", "heatmap")
}

func (r *heatmapRef) resolve(ctx context.Context, _ *providers.ConfigLoader, c *client) (int64, error) {
	if r.id != 0 {
		return r.id, nil
	}
	heatmaps, err := c.listHeatmaps(ctx)
	if err != nil {
		return 0, err
	}
	return pick(&r.ref, "<heatmap>", "heatmap", r.use, heatmaps, func(h heatmap) (string, int64) { return h.Name, h.ID })
}

func heatmapTable(names *projectNames) cmdio.Table[heatmapObject] {
	return cmdio.Table[heatmapObject]{
		Columns: []cmdio.Column[heatmapObject]{
			{Header: "NAME", Content: func(o heatmapObject) string { return o.Spec.GetResourceName() }},
			{Header: "FIELD", Content: func(o heatmapObject) string { return o.Spec.CustomFieldKey }},
			{Header: "WINDOW", Content: func(o heatmapObject) string { return strconv.FormatInt(o.Spec.WindowDays, 10) + "d" }},
			{Header: "PROJECTS", Content: func(o heatmapObject) string { return names.shown(o.Spec.ProjectIDs) }},
			{Header: "PROJECT IDS", Visible: cmdio.WideOnly, Content: func(o heatmapObject) string { return joinIDs(o.Spec.ProjectIDs) }},
			{Header: "UPDATED", Visible: cmdio.WideOnly, Content: func(o heatmapObject) string { return stamp(o.Spec.UpdatedAt) }},
			{Header: "CREATED", Visible: cmdio.WideOnly, Content: func(o heatmapObject) string { return stamp(o.Spec.CreatedAt) }},
		},
	}
}

func joinIDs(ids []int64) string {
	s := make([]string, len(ids))
	for i, id := range ids {
		s[i] = strconv.FormatInt(id, 10)
	}
	return cmdio.OrDash(strings.Join(s, ","))
}

const maxNamedProjects = 3

type projectNames map[int64]string

func (n *projectNames) shown(ids []int64) string {
	if len(ids) > maxNamedProjects {
		return fmt.Sprintf("%d projects", len(ids))
	}
	s := make([]string, len(ids))
	for i, id := range ids {
		s[i] = cmp.Or(printable((*n)[id]), strconv.FormatInt(id, 10))
	}
	return cmdio.OrDash(strings.Join(s, ", "))
}

func (c *client) projectNamesOrIDs(cmd *cobra.Command, heatmaps ...heatmap) projectNames {
	names, err := c.projectNames(cmd.Context(), heatmaps...)
	if err != nil {
		cmdio.EmitWarn(cmd.ErrOrStderr(), "could not read project names; showing project IDs: "+printable(err.Error()))
		return projectNames{}
	}
	return names
}

func (c *client) projectNames(ctx context.Context, heatmaps ...heatmap) (projectNames, error) {
	if !slices.ContainsFunc(heatmaps, func(h heatmap) bool { return len(h.ProjectIDs) > 0 }) {
		return projectNames{}, nil
	}
	projects, err := c.listProjects(ctx)
	if err != nil {
		return nil, err
	}
	names := make(projectNames, len(projects))
	for _, p := range projects {
		names[p.ID] = p.Name
	}
	return names, nil
}

type heatmapsListOpts struct {
	IO    cmdio.Options
	names projectNames
}

func (o *heatmapsListOpts) setup(flags *pflag.FlagSet) {
	bindListTable(&o.IO, flags, heatmapTable(&o.names))
}

func (o *heatmapsListOpts) Validate(cmd *cobra.Command) error {
	return validIO(cmd, &o.IO)
}

func newHeatmapsListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &heatmapsListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Error Tracking heatmaps.",
		Long: `List the stack's Error Tracking heatmaps with their custom field, window and
projects; the API returns them all, so there is no --limit. PROJECTS names up
to 3 projects, at the cost of one projects list, and counts more; -o wide adds
their IDs and when each heatmap was created and updated.`,
		Example: `  # List every heatmap.
  gcx error-tracking heatmaps list

  # Show project IDs and when each was created and updated too.
  gcx et heatmaps list -o wide

  # As an agent: IDs, names and fields only.
  gcx error-tracking heatmaps list --json spec.id,spec.name,spec.customFieldKey`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(cmd); err != nil {
				return err
			}
			c, err := newClient(cmd.Context(), loader)
			if err != nil {
				return err
			}
			heatmaps, err := c.listHeatmaps(cmd.Context())
			if err != nil {
				return err
			}
			opts.names = c.projectNamesOrIDs(cmd, heatmaps...)
			items := make([]heatmapObject, len(heatmaps))
			for i, h := range heatmaps {
				items[i] = wrap(c, "Heatmap", h)
			}
			if err := encodeList(cmd, c, &opts.IO, items, nil); err != nil {
				return err
			}
			if len(items) == 0 && !opts.IO.JSONDiscovery {
				say(cmd.ErrOrStderr(), "No heatmaps yet; create one", "gcx error-tracking heatmaps create --name <name> --field <key> --window-days 7 --projects <project>")
			}
			return nil
		},
		Annotations: map[string]string{agent.AnnotationTokenCost: "small", agent.AnnotationLLMHint: "--json spec.id,spec.name,spec.customFieldKey"},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type heatmapsGetOpts struct {
	IO      cmdio.Options
	Heatmap heatmapRef
	names   projectNames
}

func (o *heatmapsGetOpts) setup(flags *pflag.FlagSet) {
	bindSingleTable(&o.IO, flags, heatmapTable(&o.names))
}

func (o *heatmapsGetOpts) Validate(cmd *cobra.Command) error {
	if err := o.Heatmap.validate(cmd); err != nil {
		return err
	}
	return validIO(cmd, &o.IO)
}

func newHeatmapsGetCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &heatmapsGetOpts{}
	cmd := &cobra.Command{
		Use:   "get <heatmap>",
		Short: "Get one Error Tracking heatmap.",
		Long: `Get one Error Tracking heatmap's settings: its custom field, window and
projects, named at the cost of one projects list. <heatmap> is its ID from
heatmaps list, name (case-insensitive) or name-ID; a name costs one heatmaps
list. heatmaps report shows its counts.`,
		Example: `  # Get a heatmap by ID.
  gcx error-tracking heatmaps get 3

  # Get it by name.
  gcx et heatmaps get "Checkout by region"

  # As an agent: its field and project IDs only.
  gcx error-tracking heatmaps get 3 --json spec.customFieldKey,spec.projectIds`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Heatmap.value = args[0]
			if err := opts.Validate(cmd); err != nil {
				return err
			}
			ctx := cmd.Context()
			c, id, err := connect(ctx, loader, &opts.Heatmap, nil)
			if err != nil {
				return err
			}
			h, err := c.getHeatmap(ctx, id)
			if err != nil {
				return err
			}
			opts.names = c.projectNamesOrIDs(cmd, h)
			return opts.IO.Encode(cmd.OutOrStdout(), wrap(c, "Heatmap", h))
		},
		Annotations: map[string]string{agent.AnnotationTokenCost: "small", agent.AnnotationLLMHint: "<heatmap> --json spec.projectIds,spec.windowDays"},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type heatmapFields struct {
	name, field string
	windowDays  int64
	projects    []string
	req         heatmapRequest
	refs        []projectRef
}

const (
	maxWindowDays = 90
	fieldsList    = "gcx error-tracking fields list --project <project>"
)

func (f *heatmapFields) setup(flags *pflag.FlagSet, required bool) {
	mark := ""
	if required {
		mark = " (required)"
	}
	flags.StringVar(&f.name, "name", "", "Heatmap name, unique in the stack (case-insensitive), at most 255 characters"+mark)
	flags.StringVar(&f.field, "field", "", "Custom field key whose values are the heatmap's rows, a KEY from "+fieldsList+mark)
	flags.Int64Var(&f.windowDays, "window-days", 0, "Days the heatmap covers, 1 to 90, ending yesterday (UTC)"+mark)
	flags.StringArrayVar(&f.projects, "projects", nil, "Project to include: its ID, name (case-insensitive) or name-ID. Repeat for more; the saved default project does not apply"+mark)
	if required {
		for _, name := range []string{"name", "field", "window-days", "projects"} {
			_ = cobra.MarkFlagRequired(flags, name)
		}
	}
}

func (f *heatmapFields) validate(cmd *cobra.Command) error {
	if err := nonEmpty(cmd, "field"); err != nil {
		return err
	}
	flags := cmd.Flags()
	if flags.Changed("name") {
		f.name = strings.TrimSpace(f.name)
		if err := checkName(cmd, f.name); err != nil {
			return err
		}
		f.req.Name = &f.name
	}
	if flags.Changed("field") {
		f.field = strings.TrimSpace(f.field)
		f.req.CustomFieldKey = &f.field
	}
	if flags.Changed("window-days") {
		if f.windowDays < 1 || f.windowDays > maxWindowDays {
			return usage(cmd, fmt.Sprintf("--window-days %d: use 1 to %d", f.windowDays, maxWindowDays))
		}
		f.req.WindowDays = &f.windowDays
	}
	if !flags.Changed("projects") {
		return nil
	}
	f.refs = make([]projectRef, len(f.projects))
	for i, v := range f.projects {
		f.refs[i].value, f.refs[i].flag = v, "--projects"
		if err := f.refs[i].validate(cmd); err != nil {
			return err
		}
	}
	return nil
}

func (f *heatmapFields) resolve(ctx context.Context, c *client) error {
	if f.refs == nil {
		return nil
	}
	var projects []project
	if slices.ContainsFunc(f.refs, func(r projectRef) bool { return r.id == 0 }) {
		var err error
		if projects, err = c.listProjects(ctx); err != nil {
			return err
		}
	}
	ids := make([]int64, 0, len(f.refs))
	for i := range f.refs {
		id, err := f.refs[i].pick(projects)
		if err != nil {
			return err
		}
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	f.req.ProjectIDs = &ids
	return nil
}

type heatmapsCreateOpts struct {
	IO     cmdio.Options
	Fields heatmapFields
}

func (o *heatmapsCreateOpts) setup(flags *pflag.FlagSet) {
	o.Fields.setup(flags, true)
	bindText(&o.IO, flags, mutationText{})
}

func (o *heatmapsCreateOpts) Validate(cmd *cobra.Command) error {
	if err := o.Fields.validate(cmd); err != nil {
		return err
	}
	return validIO(cmd, &o.IO)
}

func newHeatmapsCreateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &heatmapsCreateOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an Error Tracking heatmap.",
		Long: `Create a heatmap: the summed event counts of a set of projects, one row per
value of a custom field and one column per day of a window that ends
yesterday (UTC). Every flag is required and checked before the API is called,
and names are unique in the stack. The result names the new heatmap's ID. ` + noDryRun,
		Example: `  # Errors by region across two projects, last 14 days.
  gcx error-tracking heatmaps create --name "Checkout by region" --field region --window-days 14 --projects 314 --projects 18

  # Projects by name.
  gcx et heatmaps create --name tenants --field tenant --window-days 7 --projects checkout-api --projects web

  # As an agent: create it and keep the new heatmap's ID.
  gcx error-tracking heatmaps create --name releases --field release --window-days 30 --projects 314 -o json --jq .target.id`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(cmd); err != nil {
				return err
			}
			ctx := cmd.Context()
			c, err := newClient(ctx, loader)
			if err != nil {
				return err
			}
			if err := opts.Fields.resolve(ctx, c); err != nil {
				return err
			}
			var h heatmap
			if err := c.do(ctx, http.MethodPost, "heatmaps", nil, opts.Fields.req, &h); err != nil {
				return c.nameTaken(ctx, err, "heatmap", opts.Fields.name)
			}
			m := c.mutation("created", "Heatmap", h.ID, true)
			m.Target.Name = h.Name
			m.text = "Created " + h.shown()
			return emit(cmd, &opts.IO, m)
		},
		Annotations: map[string]string{agent.AnnotationTokenCost: "small", agent.AnnotationLLMHint: "--name <name> --field <key> --window-days 7 --projects 314 -o json"},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type heatmapsUpdateOpts struct {
	IO      cmdio.Options
	Heatmap heatmapRef
	Fields  heatmapFields
}

func (o *heatmapsUpdateOpts) setup(flags *pflag.FlagSet) {
	o.Fields.setup(flags, false)
	bindText(&o.IO, flags, mutationText{})
}

func (o *heatmapsUpdateOpts) Validate(cmd *cobra.Command) error {
	if err := o.Heatmap.validate(cmd); err != nil {
		return err
	}
	if err := o.Fields.validate(cmd); err != nil {
		return err
	}
	if o.Fields.req == (heatmapRequest{}) && o.Fields.refs == nil {
		return usage(cmd, "pass at least one of --name, --field, --window-days or --projects")
	}
	return validIO(cmd, &o.IO)
}

func newHeatmapsUpdateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &heatmapsUpdateOpts{}
	cmd := &cobra.Command{
		Use:   "update <heatmap>",
		Short: "Change an Error Tracking heatmap's settings.",
		Long: `Change a heatmap's name, custom field, window or projects; only the flags
you pass change, and --projects replaces the whole set. <heatmap> is its ID,
name (case-insensitive) or name-ID, and the flags follow the rules of
heatmaps create. ` + noDryRun,
		Example: `  # Widen the window to 30 days.
  gcx error-tracking heatmaps update 3 --window-days 30

  # Replace the projects.
  gcx et heatmaps update 3 --projects 314 --projects 18 --projects 21

  # Rename it and change its field.
  gcx et heatmaps update 3 --name "Checkout by country" --field country

  # As an agent: change the window and read the result.
  gcx error-tracking heatmaps update 3 --window-days 30 -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Heatmap.value = args[0]
			if err := opts.Validate(cmd); err != nil {
				return err
			}
			ctx := cmd.Context()
			c, id, err := connect(ctx, loader, &opts.Heatmap, nil)
			if err != nil {
				return err
			}
			if err := opts.Fields.resolve(ctx, c); err != nil {
				return err
			}
			var h heatmap
			if err := c.do(ctx, http.MethodPatch, routePath("heatmaps", id), nil, opts.Fields.req, &h); err != nil {
				if opts.Fields.req.Name != nil {
					return c.nameTaken(ctx, err, "heatmap", opts.Fields.name)
				}
				return err
			}
			m := c.mutation("updated", "Heatmap", h.ID, true)
			m.Target.Name = h.Name
			m.text = "Updated " + h.shown()
			return emit(cmd, &opts.IO, m)
		},
		Annotations: map[string]string{agent.AnnotationTokenCost: "small", agent.AnnotationLLMHint: "<heatmap> --window-days 30 -o json"},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type heatmapsDeleteOpts struct {
	IO      cmdio.Options
	Heatmap heatmapRef
	Force   bool
}

func (o *heatmapsDeleteOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.Force, "force", false, "Delete without asking")
	bindText(&o.IO, flags, mutationText{})
}

func (o *heatmapsDeleteOpts) Validate(cmd *cobra.Command) error {
	if err := o.Heatmap.validate(cmd); err != nil {
		return err
	}
	return validIO(cmd, &o.IO)
}

func newHeatmapsDeleteCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &heatmapsDeleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete <heatmap>",
		Short: "Delete an Error Tracking heatmap.",
		Long: `Delete a heatmap; its projects and their errors and events stay.

<heatmap> is the heatmap ID from heatmaps list, name (case-insensitive) or
name-ID.

gcx asks first; --force skips the question, agent mode refuses without
--force, and declining exits 5. ` + noDryRun,
		Example: `  # Delete after a prompt.
  gcx error-tracking heatmaps delete 3

  # Delete by name.
  gcx et heatmaps delete "Checkout by region" --force

  # As an agent: delete without a prompt, as scripts and agents must.
  gcx error-tracking heatmaps delete 3 --force -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Heatmap.value = args[0]
			if err := opts.Validate(cmd); err != nil {
				return err
			}
			force, err := deleteBypass(cmd, opts.Force)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			c, id, err := connect(ctx, loader, &opts.Heatmap, nil)
			if err != nil {
				return err
			}
			shown := fmt.Sprintf("heatmap %d", id)
			if opts.Heatmap.name != "" {
				shown = heatmap{ID: id, Name: opts.Heatmap.name}.shown()
			}
			if !force || opts.Heatmap.name == "" {
				h, err := c.getHeatmap(cmd.Context(), id)
				if err != nil {
					return err
				}
				shown, opts.Heatmap.name = h.shown(), h.Name
			}
			if !force {
				if err := confirm(cmd, fmt.Sprintf("Delete Error Tracking %s?", shown),
					"delete cancelled", fmt.Sprintf("The prompt was declined; %s was not deleted.", shown)); err != nil {
					return err
				}
			}
			if err := c.do(ctx, http.MethodDelete, routePath("heatmaps", id), nil, nil, nil); err != nil {
				return err
			}
			m := c.mutation("deleted", "Heatmap", id, true)
			m.Target.Name = opts.Heatmap.name
			m.text = "Deleted " + shown
			return emit(cmd, &opts.IO, m)
		},
		Annotations: map[string]string{agent.AnnotationTokenCost: "small", agent.AnnotationLLMHint: "<heatmap> --force -o json"},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type heatmapsReportOpts struct {
	IO      cmdio.Options
	Heatmap heatmapRef
	Day     string
	day     string
}

func (o *heatmapsReportOpts) setup(flags *pflag.FlagSet) {
	flags.StringVar(&o.Day, "day", "", "Show this UTC day (2026-11-16) per hour instead of the window per day")
	o.IO.RegisterCustomCodec(cmdio.FormatTable, heatmapGrid{name: cmdio.FormatTable, opts: o})
	o.IO.RegisterCustomCodec(cmdio.FormatWide, heatmapGrid{name: cmdio.FormatWide, opts: o})
	o.IO.DefaultFormat(cmdio.FormatTable)
	o.IO.BindFlags(flags)
}

func (o *heatmapsReportOpts) Validate(cmd *cobra.Command) error {
	if err := o.Heatmap.validate(cmd); err != nil {
		return err
	}
	if err := nonEmpty(cmd, "day"); err != nil {
		return err
	}
	if o.Day != "" {
		o.Day = strings.TrimSpace(o.Day)
		day, err := time.Parse(time.DateOnly, o.Day)
		if err != nil {
			return usage(cmd, fmt.Sprintf("invalid --day %q: use a UTC date like 2026-11-16", o.Day))
		}
		o.day = day.Format(time.RFC3339)
	}
	return validIO(cmd, &o.IO)
}

func (o *heatmapsReportOpts) emptyHint(w io.Writer, id int64, r heatmapReport) {
	if o.Day != "" {
		say(w, fmt.Sprintf("No events on %s. %s", o.Day, countsLag), "")
		return
	}
	first, last := printable(r.From), printable(r.To)
	if from, ok := parseTime(r.From); ok {
		first = from.UTC().Format(time.DateOnly)
	}
	if to, ok := parseTime(r.To); ok {
		last = to.UTC().Add(-time.Nanosecond).Format(time.DateOnly)
	}
	say(w, fmt.Sprintf("No events in %s … %s. The window ends yesterday (UTC); for today", first, last),
		fmt.Sprintf("gcx error-tracking heatmaps report %d --day %s", id, time.Now().UTC().Format(time.DateOnly)))
}

const (
	maxGridColumns   = 200
	maxHeatmapValues = 50
)

type heatmapGrid struct {
	name string
	opts *heatmapsReportOpts
}

func (g heatmapGrid) Format() format.Format { return format.Format(g.name) }

func (heatmapGrid) Decode(io.Reader, any) error {
	return errors.New("heatmap grid does not support decoding")
}

func (g heatmapGrid) Encode(w io.Writer, v any) error {
	r, ok := v.(heatmapReport)
	if !ok {
		return fmt.Errorf("invalid data type for %s codec: expected a heatmap report, got %T", g.name, v)
	}
	step := 24 * time.Hour
	if g.opts.day != "" {
		step = time.Hour
	}
	if d, err := shared.ParseDuration(r.Bucket); err == nil && d > 0 {
		step = d
	}
	buckets := gridBuckets(r, step)
	columns := make([]cmdio.Column[gridRow], 0, len(buckets)+2)
	columns = append(columns, cmdio.Column[gridRow]{Header: "VALUE", Content: func(row gridRow) string {
		if row.value == "" {
			return "(no fields)"
		}
		return printable(row.value)
	}})
	for _, b := range buckets {
		columns = append(columns, cmdio.Column[gridRow]{Header: b.header(step, g.name == cmdio.FormatWide), Content: func(row gridRow) string {
			if n, ok := row.counts[b.key]; ok {
				return strconv.FormatInt(n, 10)
			}
			return "-"
		}})
	}
	columns = append(columns, cmdio.Column[gridRow]{Header: "TOTAL", Content: func(row gridRow) string { return strconv.FormatInt(row.total, 10) }})
	return cmdio.Table[gridRow]{Columns: columns}.Codec(g.name).Encode(w, gridRows(r))
}

type gridBucket struct {
	key string
	at  time.Time
	ok  bool
}

func bucketOf(s string) gridBucket {
	if t, ok := parseTime(s); ok {
		t = t.UTC()
		return gridBucket{key: t.Format(time.RFC3339), at: t, ok: true}
	}
	return gridBucket{key: s}
}

func (b gridBucket) header(step time.Duration, wide bool) string {
	switch {
	case !b.ok:
		return printable(b.key)
	case step < 24*time.Hour && wide:
		return b.at.Format("2006-01-02 15:04Z")
	case step < 24*time.Hour:
		return fmt.Sprintf("%02dh", b.at.Hour())
	case wide:
		return b.at.Format(time.DateOnly)
	}
	return b.at.Format("01-02")
}

func gridBuckets(r heatmapReport, step time.Duration) []gridBucket {
	var out []gridBucket
	seen := map[string]bool{}
	add := func(b gridBucket) {
		if !seen[b.key] {
			seen[b.key] = true
			out = append(out, b)
		}
	}
	from, fromOK := parseTime(r.From)
	to, toOK := parseTime(r.To)
	if fromOK && toOK && to.After(from) && to.Sub(from)/step <= maxGridColumns {
		for t := from; t.Before(to); t = t.Add(step) {
			add(bucketOf(t.Format(time.RFC3339)))
		}
	}
	for _, c := range r.Cells {
		add(bucketOf(c.Bucket))
	}
	slices.SortStableFunc(out, func(a, b gridBucket) int {
		switch {
		case a.ok && b.ok:
			return a.at.Compare(b.at)
		case a.ok:
			return -1
		case b.ok:
			return 1
		}
		return strings.Compare(a.key, b.key)
	})
	return out
}

type gridRow struct {
	value  string
	counts map[string]int64
	total  int64
}

func gridRows(r heatmapReport) []gridRow {
	byValue := map[string]*gridRow{}
	rows := []*gridRow{}
	for _, c := range r.Cells {
		row := byValue[c.Value]
		if row == nil {
			row = &gridRow{value: c.Value, counts: map[string]int64{}}
			byValue[c.Value] = row
			rows = append(rows, row)
		}
		row.counts[bucketOf(c.Bucket).key] += c.EventCount
		row.total += c.EventCount
	}
	slices.SortFunc(rows, func(a, b *gridRow) int {
		return cmp.Or(cmp.Compare(b.total, a.total), strings.Compare(a.value, b.value))
	})
	out := make([]gridRow, len(rows))
	for i, row := range rows {
		out[i] = *row
	}
	return out
}

func newHeatmapsReportCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &heatmapsReportOpts{}
	cmd := &cobra.Command{
		Use:   "report <heatmap>",
		Short: "Show a heatmap's event counts per custom field value and day or hour.",
		Long: `Show a heatmap's grid: its projects' summed event count per value of its
custom field, per UTC day over its window (ending yesterday), or per hour of
one day with --day. Tables have one row per value, most events first, a
column per bucket and a TOTAL, and JSON keeps the API's shape (bucket, from,
to, cells, plus list_meta when 50 values came back). The API keeps only the 50
values with the most events, so list_meta.returned counts values, not cells,
and "(no fields)" ("" in JSON) counts events without custom fields.`,
		Example: `  # The daily grid over the heatmap's window.
  gcx error-tracking heatmaps report 3

  # One day, per hour.
  gcx et heatmaps report 3 --day 2026-11-16

  # As an agent: the cells only.
  gcx error-tracking heatmaps report 3 --json cells`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Heatmap.value = args[0]
			if err := opts.Validate(cmd); err != nil {
				return err
			}
			ctx := cmd.Context()
			c, id, err := connect(ctx, loader, &opts.Heatmap, nil)
			if err != nil {
				return err
			}
			q := url.Values{}
			if opts.day != "" {
				q.Set("day", opts.day)
			}
			var r heatmapReport
			if err := c.do(ctx, http.MethodGet, routePath("heatmaps", id, "data"), q, nil, &r); err != nil {
				return err
			}
			values := map[string]bool{}
			for _, cell := range r.Cells {
				values[cell.Value] = true
			}
			r.ListMeta = cmdio.PagedListMeta(len(values), 0, len(values) >= maxHeatmapValues, maxHeatmapValues)
			if err := opts.IO.Encode(cmd.OutOrStdout(), r); err != nil {
				return err
			}
			if opts.IO.JSONDiscovery {
				return nil
			}
			if len(r.Cells) == 0 {
				opts.emptyHint(cmd.ErrOrStderr(), id, r)
				return nil
			}
			cmdio.EmitListTruncationHint(cmd.ErrOrStderr(), r.ListMeta)
			return nil
		},
		Annotations: map[string]string{agent.AnnotationTokenCost: "large", agent.AnnotationLLMHint: "<heatmap> --day 2026-11-16"},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type heatmapsBreakdownOpts struct {
	IO      cmdio.Options
	Heatmap heatmapRef
	Value   string
	Time    timeRange
}

func (o *heatmapsBreakdownOpts) setup(flags *pflag.FlagSet) {
	flags.StringVar(&o.Value, "value", "", `Custom field value of the cell; "" for events without custom fields (required)`)
	_ = cobra.MarkFlagRequired(flags, "value")
	o.Time.SetupTimeFlags(flags)
	bindTable(&o.IO, flags, cmdio.Table[heatmapBreakdownProject]{Columns: []cmdio.Column[heatmapBreakdownProject]{
		{Header: "PROJECT ID", Content: func(p heatmapBreakdownProject) string { return strconv.FormatInt(p.ProjectID, 10) }},
		{Header: "PROJECT", Content: func(p heatmapBreakdownProject) string { return p.ProjectName }},
		{Header: "EVENTS", Content: func(p heatmapBreakdownProject) string { return strconv.FormatInt(p.EventCount, 10) }},
	}}, func(b heatmapBreakdown) []heatmapBreakdownProject { return b.Projects })
}

func (o *heatmapsBreakdownOpts) Validate(cmd *cobra.Command) error {
	if err := o.Heatmap.validate(cmd); err != nil {
		return err
	}
	if err := o.Time.validate(cmd); err != nil {
		return err
	}
	return validIO(cmd, &o.IO)
}

func heatmapWindow(days int64, now time.Time) (string, string) {
	now = now.UTC()
	to := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	from := to.AddDate(0, 0, -int(days))
	return from.Format(time.RFC3339), to.Format(time.RFC3339)
}

func (o *heatmapsBreakdownOpts) defaultWindow(cmd *cobra.Command, c *client, id int64) error {
	h, err := c.getHeatmap(cmd.Context(), id)
	if err != nil {
		return err
	}
	o.Time.from, o.Time.to = heatmapWindow(h.WindowDays, time.Now())
	if !o.IO.JSONDiscovery && !agent.IsAgentMode() {
		last, _ := parseTime(o.Time.to)
		cmdio.Info(cmd.ErrOrStderr(), "%s's %d-day window: %s … %s (UTC)", capital(h.shown()), h.WindowDays,
			o.Time.from[:len(time.DateOnly)], last.Add(-time.Nanosecond).Format(time.DateOnly))
	}
	return nil
}

func newHeatmapsBreakdownCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &heatmapsBreakdownOpts{}
	cmd := &cobra.Command{
		Use:   "breakdown <heatmap>",
		Short: "Split one heatmap cell's event count across the heatmap's projects.",
		Long: `Split the event count of one heatmap row (one custom field value) across the
heatmap's projects, most events first. --value is the VALUE column of heatmaps
report ("" for events without custom fields). Without a window flag it uses
the heatmap's own window, ending yesterday (UTC), which costs one heatmaps
get; to match one cell, pass its bucket as --from and the next bucket as
--to.`,
		Example: `  # Which projects sent region=eu events over the heatmap's window.
  gcx error-tracking heatmaps breakdown 3 --value eu

  # One day's cell, by name.
  gcx et heatmaps breakdown "Checkout by region" --value eu --from 2026-11-16T00:00:00Z --to 2026-11-17T00:00:00Z

  # Events without custom fields in the last 24 hours.
  gcx et heatmaps breakdown 3 --value "" --since 24h

  # As an agent: project IDs and counts only.
  gcx error-tracking heatmaps breakdown 3 --value eu --json projects`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Heatmap.value = args[0]
			if err := opts.Validate(cmd); err != nil {
				return err
			}
			ctx := cmd.Context()
			c, id, err := connect(ctx, loader, &opts.Heatmap, nil)
			if err != nil {
				return err
			}
			if opts.Time.from == "" || opts.Time.to == "" {
				if err := opts.defaultWindow(cmd, c, id); err != nil {
					return err
				}
			}
			q := url.Values{"value": {opts.Value}}
			opts.Time.setClosed(q)
			var b heatmapBreakdown
			if err := c.do(ctx, http.MethodGet, routePath("heatmaps", id, "data", "breakdown"), q, nil, &b); err != nil {
				return err
			}
			if err := opts.IO.Encode(cmd.OutOrStdout(), b); err != nil {
				return err
			}
			if len(b.Projects) == 0 && !opts.IO.JSONDiscovery {
				say(cmd.ErrOrStderr(), fmt.Sprintf("No events with value %q from %s to %s; the values with events", printable(opts.Value), minute(opts.Time.from), minute(opts.Time.to)),
					fmt.Sprintf("gcx error-tracking heatmaps report %d", id))
			}
			return nil
		},
		Annotations: map[string]string{agent.AnnotationTokenCost: "medium", agent.AnnotationLLMHint: "<heatmap> --value <value>"},
	}
	opts.setup(cmd.Flags())
	return cmd
}
