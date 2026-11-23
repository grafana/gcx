package errortracking

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type project struct {
	ID                     int64        `json:"id"`
	Name                   string       `json:"name"`
	Description            string       `json:"description"`
	SourceRepoURL          string       `json:"sourceRepoUrl"`
	IngestKey              string       `json:"ingestKey,omitempty"`
	DSN                    string       `json:"dsn,omitempty"`
	SourceMapUploadURL     string       `json:"sourceMapUploadUrl,omitempty"`
	CreatedAt              string       `json:"createdAt"`
	IngestKeyRotatedAt     *string      `json:"ingestKeyRotatedAt,omitempty"`
	PreviousIngestKeyUntil *string      `json:"previousIngestKeyUntil,omitempty"`
	Stats                  projectStats `json:"stats"`
}

type projectStats struct {
	UnresolvedErrors int64                 `json:"unresolvedErrors"`
	EventsLast24h    int64                 `json:"eventsLast24h"`
	LastEventAt      *string               `json:"lastEventAt,omitempty"`
	DailyEvents      list[dailyEventCount] `json:"dailyEvents"`
}

type dailyEventCount struct {
	Day        string `json:"day"`
	EventCount int64  `json:"eventCount"`
}

func (p project) GetResourceName() string {
	return resourceName(p.Name, p.ID)
}

func (p project) shown() string { return projectLabel(p.ID, p.Name) }

func (c *client) getProject(ctx context.Context, id int64) (project, error) {
	var p project
	err := c.do(ctx, http.MethodGet, routePath("projects", id), nil, nil, &p)
	return p, err
}

type projectObject = adapter.TypedObject[project]

func (c *client) listProjects(ctx context.Context) ([]project, error) {
	var out struct {
		Projects []project `json:"projects"`
	}
	if err := c.do(ctx, http.MethodGet, "projects", nil, nil, &out); err != nil {
		return nil, err
	}
	return out.Projects, nil
}

func projectTable(dsn bool) cmdio.Table[projectObject] {
	columns := []cmdio.Column[projectObject]{
		{Header: "NAME", Content: func(o projectObject) string { return o.Spec.GetResourceName() }},
		{Header: "UNRESOLVED", Content: func(o projectObject) string { return strconv.FormatInt(o.Spec.Stats.UnresolvedErrors, 10) }},
		{Header: "EVENTS 24H", Content: func(o projectObject) string { return strconv.FormatInt(o.Spec.Stats.EventsLast24h, 10) }},
		{Header: "LAST EVENT", Visible: when, Content: func(o projectObject) string { return optTime(o.Spec.Stats.LastEventAt) }},
	}
	if dsn {
		columns = append(columns, cmdio.Column[projectObject]{Header: "DSN", Visible: cmdio.WideOnly, Content: func(o projectObject) string { return cmdio.OrDash(o.Spec.DSN) }})
	}
	return cmdio.Table[projectObject]{Columns: append(columns,
		cmdio.Column[projectObject]{Header: "REPO", Visible: cmdio.WideOnly, Content: func(o projectObject) string { return cmdio.OrDash(o.Spec.SourceRepoURL) }},
		cmdio.Column[projectObject]{Header: "CREATED", Visible: cmdio.WideOnly, Content: func(o projectObject) string { return stamp(o.Spec.CreatedAt) }},
	)}
}

type projectsListOpts struct {
	IO     cmdio.Options
	Search string
}

func (o *projectsListOpts) setup(flags *pflag.FlagSet) {
	flags.StringVar(&o.Search, "search", "", "Keep projects whose name contains this text (case-insensitive)")
	bindListTable(&o.IO, flags, projectTable(false))
}

func (o *projectsListOpts) Validate(cmd *cobra.Command) error {
	if err := nonEmpty(cmd, "search"); err != nil {
		return err
	}
	o.Search = strings.TrimSpace(o.Search)
	return validIO(cmd, &o.IO)
}

func storedDefault(ctx context.Context, loader *providers.ConfigLoader) (string, error) {
	cfg, _, err := loader.LoadProviderConfig(ctx, providerName)
	return strings.TrimSpace(cfg["project"]), err
}

// fileDefault reads the default project as saved in the config file, ignoring
// the environment override that storedDefault applies.
func fileDefault(ctx context.Context, loader *providers.ConfigLoader) (string, error) {
	cfg, err := loader.LoadFileProviderConfig(ctx, providerName)
	return strings.TrimSpace(cfg["project"]), err
}

func newProjectsListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &projectsListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Error Tracking projects.",
		Long: `List the stack's Error Tracking projects with their error and event counts;
the ID, the name (case-insensitive) or name-ID is what --project and
projects get take.

The API returns every project in one response, so there is no --limit, and
--search keeps the projects whose name contains the text, case-insensitive.
The list leaves out the ingest key and the DSN in every format; projects get
returns them.`,
		Example: `  # List every project.
  gcx error-tracking projects list

  # Projects whose name contains "checkout".
  gcx et projects list --search checkout

  # Show the repository and creation time too.
  gcx et projects list -o wide

  # As an agent: IDs and names only.
  gcx error-tracking projects list --json spec.id,spec.name`,
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
			projects, err := c.listProjects(ctx)
			if err != nil {
				return err
			}
			items := make([]projectObject, 0, len(projects))
			for _, p := range projects {
				if opts.Search != "" && !strings.Contains(strings.ToLower(p.Name), strings.ToLower(opts.Search)) {
					continue
				}
				p.IngestKey, p.DSN = "", ""
				items = append(items, wrap(c, "Project", p))
			}
			if err := encodeList(cmd, c, &opts.IO, items, nil); err != nil {
				return err
			}
			if opts.IO.JSONDiscovery {
				return nil
			}
			w := cmd.ErrOrStderr()
			switch {
			case len(projects) == 0:
				say(w, "No Error Tracking projects yet; create one", "gcx error-tracking projects create --name <name>")
			case len(items) == 0:
				say(w, fmt.Sprintf("No project name contains %q; see all", printable(opts.Search)), "gcx error-tracking projects list")
			}
			return nil
		},
		Annotations: map[string]string{agent.AnnotationTokenCost: "medium", agent.AnnotationLLMHint: "--search <text> --json spec.id,spec.name"},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type defaultProject struct {
	Stack   string       `json:"stack,omitempty"`
	Project *projectInfo `json:"project"`
	Source  string       `json:"source,omitempty"`

	text string
}

type useMutation struct {
	mutationBase `json:",inline"`

	Stack  string `json:"stack,omitempty"`
	Source string `json:"source,omitempty"`
}

type projectsUseOpts struct {
	IO      cmdio.Options
	Project projectRef
	Unset   bool
}

func (o *projectsUseOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.Unset, "unset", false, "Clear the default project")
	bindText(&o.IO, flags, useText{})
}

func (o *projectsUseOpts) Validate(cmd *cobra.Command, args []string) error {
	if o.Unset && len(args) > 0 {
		return usage(cmd, "--unset takes no <project>")
	}
	if len(args) == 1 {
		o.Project.value = args[0]
		if err := o.Project.validate(cmd); err != nil {
			return err
		}
	}
	return validIO(cmd, &o.IO)
}

type useText struct{ mutationText }

func (u useText) Encode(w io.Writer, v any) error {
	d, ok := v.(defaultProject)
	if !ok {
		return u.mutationText.Encode(w, v)
	}
	cmdio.Info(w, "%s", printable(d.text))
	return nil
}

func stackName(ctx context.Context, loader *providers.ConfigLoader) string {
	cfg, err := loader.LoadConfigTolerant(ctx)
	if err != nil {
		return ""
	}
	return cfg.GetCurrentContext().Stack
}

// namespaceOf returns the stack namespace for mutation targets, or "" when the
// config does not resolve one.
func namespaceOf(ctx context.Context, loader *providers.ConfigLoader) string {
	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return ""
	}
	return cfg.Namespace
}

func showDefault(ctx context.Context, loader *providers.ConfigLoader, stack string) (defaultProject, error) {
	v, err := storedDefault(ctx, loader)
	d := defaultProject{Stack: stack}
	forStack := ""
	if stack != "" {
		forStack = " for stack " + stack
	}
	d.text = "No default project" + forStack
	if err != nil || v == "" {
		return d, err
	}
	id, numeric, ok := parseID(v)
	if numeric && !ok {
		return d, configError(fmt.Sprintf("the default project %q (%s) is not a project ID or name", printable(v), fromDefault()),
			"Pick another: gcx error-tracking projects use <id>",
			"If "+projectEnv+" is set, unset it or set it to a project")
	}
	d.Project = &projectInfo{ID: id}
	if id == 0 {
		d.Project.Name = v
	}
	d.Source = defaultSource()
	d.text = fmt.Sprintf("Default project%s: %s (from %s)", forStack, printable(v), defaultSource())
	return d, nil
}

func useTarget(ctx context.Context, loader *providers.ConfigLoader, opts *projectsUseOpts) (int64, project, error) {
	c, err := newClient(ctx, loader)
	if err != nil {
		return 0, project{}, err
	}
	id, err := opts.Project.resolve(ctx, loader, c)
	if err != nil {
		return 0, project{}, err
	}
	if opts.Project.name != "" {
		return id, project{ID: id, Name: opts.Project.name}, nil
	}
	saved, err := c.getProject(ctx, id)
	return id, saved, err
}

func saveDefault(cmd *cobra.Command, loader *providers.ConfigLoader, opts *projectsUseOpts) (string, project, error) {
	ctx := cmd.Context()
	value := ""
	var saved project
	if !opts.Unset {
		id, p, err := useTarget(ctx, loader, opts)
		if err != nil {
			return "", p, err
		}
		saved, value = p, strconv.FormatInt(id, 10)
	}
	if err := loader.SaveProviderConfig(ctx, providerName, "project", value); err != nil {
		return "", saved, err
	}
	inFile, err := fileDefault(ctx, loader)
	if err != nil {
		return "", saved, err
	}
	if inFile != value {
		return "", saved, configError(fmt.Sprintf("the default project reads as %q in the config file after saving %q: no gcx config file took it", inFile, value),
			"Create a context first: gcx login")
	}
	after, err := storedDefault(ctx, loader)
	if err != nil {
		return "", saved, err
	}
	if after != value {
		cmdio.EmitWarn(cmd.ErrOrStderr(), fmt.Sprintf("%s=%s wins over the stack's saved default, so commands here use project %s; unset it to use what projects use saves",
			projectEnv, printable(after), printable(after)))
	}
	return value, saved, nil
}

func newProjectsUseCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &projectsUseOpts{}
	cmd := &cobra.Command{
		Use:   "use [project]",
		Short: "Set the project Error Tracking commands use when --project is not given.",
		Long: `Remember one Error Tracking project for the stack, so commands that take
--project use it, silently, when the flag is not given; write results name
the project in their output. With no argument it shows the default, --unset
clears it, and ` + projectEnv + `
overrides it in one environment, with a warning when it hides what was
saved; leaves that take a positional <project> never use it. It is saved by
ID in the gcx config file already in use, and when there is none gcx creates
the user config file; if no file takes it, the command fails. With no
argument the JSON is the default's own shape: the stack, the project and
where the default comes from. Setting or clearing returns the mutation
result with those same fields: target.id is what was saved, while project
and source are the default in effect, which ` + projectEnv + ` can
override. ` + noDryRun,
		Example: `  # Use a project by name from now on.
  gcx error-tracking projects use checkout-api

  # Show the default and where it comes from.
  gcx et projects use

  # Clear it.
  gcx et projects use --unset

  # As an agent: set it and read the result.
  gcx error-tracking projects use 314 -o json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(cmd, args); err != nil {
				return err
			}
			ctx := cmd.Context()
			value, write := "", len(args) > 0 || opts.Unset
			var saved project
			before := ""
			if write {
				var err error
				if before, err = fileDefault(ctx, loader); err != nil {
					return err
				}
				if value, saved, err = saveDefault(cmd, loader, opts); err != nil {
					return err
				}
			}
			d, err := showDefault(ctx, loader, stackName(ctx, loader))
			if err != nil {
				return err
			}
			if saved.ID != 0 && d.Project != nil && d.Project.ID == saved.ID {
				d.Project.Name = saved.Name
				d.text = strings.Replace(d.text, " (from ", " ("+printable(saved.Name)+") (from ", 1)
			}
			var out any = d
			if write {
				m := cmdio.NewSingleMutation("use", cmdio.MutationTarget{Kind: "Project", ID: value, Namespace: namespaceOf(ctx, loader)})
				changed := value != before
				m.Changed = &changed
				if opts.Unset {
					m.Action = "unset"
				}
				out = useMutation{mutationBase: mutationBase{SingleMutation: m, Project: d.Project, text: d.text}, Stack: d.Stack, Source: d.Source}
			}
			if err := opts.IO.Encode(cmd.OutOrStdout(), out); err != nil {
				return err
			}
			if d.Project == nil {
				say(cmd.ErrOrStderr(), "set one", "gcx error-tracking projects use <project>")
			}
			return nil
		},
		Annotations: map[string]string{agent.AnnotationTokenCost: "small", agent.AnnotationLLMHint: "<project> -o json"},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type projectsGetOpts struct {
	IO      cmdio.Options
	Project projectRef
}

func (o *projectsGetOpts) setup(flags *pflag.FlagSet) {
	bindSingleTable(&o.IO, flags, projectTable(true))
}

func (o *projectsGetOpts) Validate(cmd *cobra.Command) error {
	if err := o.Project.validate(cmd); err != nil {
		return err
	}
	return validIO(cmd, &o.IO)
}

func newProjectsGetCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &projectsGetOpts{}
	cmd := &cobra.Command{
		Use:   "get <project>",
		Short: "Get one Error Tracking project.",
		Long: `Get one Error Tracking project with its counts and the last seven days of
events. The table hides the ingest key and the DSN, and -o wide adds the DSN.
Every data format (-o json, -o yaml, --json, --jq) includes spec.ingestKey and
spec.dsn, so treat that output as holding the ingest key; projects list leaves
both out.

<project> is the project ID from projects list (314), name (case-insensitive)
or name-ID. An ID costs no lookup; a name costs one projects list first.`,
		Example: `  # Get a project by ID.
  gcx error-tracking projects get 314

  # Get it by name, with its DSN and repository.
  gcx et projects get checkout-api -o wide

  # As an agent: the DSN for an SDK, and nothing else.
  gcx error-tracking projects get 314 --json spec.dsn`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Project.value = args[0]
			if err := opts.Validate(cmd); err != nil {
				return err
			}
			c, id, err := connect(cmd.Context(), loader, &opts.Project, nil)
			if err != nil {
				return err
			}
			p, err := c.getProject(cmd.Context(), id)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), wrap(c, "Project", p))
		},
		Annotations: map[string]string{agent.AnnotationTokenCost: "small", agent.AnnotationLLMHint: "<project> --json spec.id,spec.name,spec.dsn"},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type projectsTimelineOpts struct {
	timelineWindow

	Project      projectRef
	Status       string
	Search       string
	Transaction  string
	Release      string
	CustomFields []string
}

func (o *projectsTimelineOpts) setup(flags *pflag.FlagSet) {
	o.timelineWindow.setup(flags)
	flags.StringVar(&o.Status, "status", "all", "Count the events of errors in this status: unresolved, regressed, resolved, ignored or all")
	flags.StringVar(&o.Search, "search", "", "Count the events of errors whose type, message or culprit contains this text (case-insensitive)")
	flags.StringVar(&o.Transaction, "transaction", "", "Count the events of errors last seen in exactly this transaction, case-sensitive")
	flags.StringVar(&o.Release, "release", "", "Count only the events of exactly this release, case-sensitive")
	flags.StringArrayVar(&o.CustomFields, "custom-field", nil, "Count the events of errors that carry this custom field"+customFieldUsage)
}

func (o *projectsTimelineOpts) Validate(cmd *cobra.Command) error {
	if err := o.Project.validate(cmd); err != nil {
		return err
	}
	if err := nonEmpty(cmd, "search", "transaction", "release"); err != nil {
		return err
	}
	statuses := []string{"unresolved", "regressed", "resolved", "ignored", "all"}
	if !slices.Contains(statuses, o.Status) {
		return usage(cmd, fmt.Sprintf("--status %q: use one of %s", o.Status, strings.Join(statuses, ", ")))
	}
	o.filter = url.Values{}
	for key, v := range map[string]string{"status": o.Status, "search": o.Search, "transaction": o.Transaction, "release": o.Release} {
		if v = strings.TrimSpace(v); v != "" && (key != "status" || v != "all") {
			o.filter.Set(key, v)
		}
	}
	if err := customFields(cmd, o.CustomFields, o.filter); err != nil {
		return err
	}
	return o.validate(cmd)
}

func newProjectsTimelineCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &projectsTimelineOpts{}
	cmd := &cobra.Command{
		Use:   "timeline <project>",
		Short: "Show an Error Tracking project's event counts over time.",
		Long: `Show how many events an Error Tracking project received per time bucket
across all its errors, over the last 24 hours unless --since or --from/--to
say otherwise (at most 30 days). The filter flags count only the events of the
errors they match.`,
		Example: `  # Events per hour over the last day.
  gcx error-tracking projects timeline 314

  # Events per day over the last week, by name.
  gcx et projects timeline checkout-api --since 7d

  # Unresolved errors' events per 5 minutes, from one browser.
  gcx et projects timeline 314 --since 1h --step 5m --status unresolved --custom-field browser:Chrome

  # As an agent: a fixed window as JSON.
  gcx error-tracking projects timeline 314 --from 2026-11-16T00:00:00Z --to 2026-11-17T00:00:00Z -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Project.value = args[0]
			if err := opts.Validate(cmd); err != nil {
				return err
			}
			ctx := cmd.Context()
			c, id, err := connect(ctx, loader, &opts.Project, nil)
			if err != nil {
				return err
			}
			tl, err := opts.get(ctx, c, routePath("projects", id, "timeline"))
			if err != nil {
				return err
			}
			return opts.encode(cmd, tl)
		},
		Annotations: map[string]string{agent.AnnotationTokenCost: "medium", agent.AnnotationLLMHint: "<project> --since 7d"},
	}
	opts.setup(cmd.Flags())
	return cmd
}
