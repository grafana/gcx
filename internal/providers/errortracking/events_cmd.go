package errortracking

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func eventArg(cmd *cobra.Command, arg, errID string) (string, error) {
	eventID := strings.TrimSpace(arg)
	see := "See them: " + et("errors", "events", "list", errID, "--project", projectOf(cmd))
	if eventID == "" || eventID == "." || eventID == ".." {
		return "", usage(cmd, "<event> must be an EVENT ID from errors events list", see)
	}
	if _, err := strconv.ParseInt(eventID, 10, 64); err == nil && len(eventID) < 32 {
		return "", usage(cmd, fmt.Sprintf("<event> %q looks like the numeric id; events are addressed by EVENT ID (32 hex characters), not the numeric id", eventID), see)
	}
	return eventID, nil
}

func eventTable() cmdio.Table[eventSummary] {
	return cmdio.Table[eventSummary]{Columns: []cmdio.Column[eventSummary]{
		{Header: "EVENT ID", Content: func(e eventSummary) string { return e.EventID }},
		{Header: "OCCURRED", Visible: when, Content: func(e eventSummary) string { return e.OccurredAt }},
		{Header: "ENVIRONMENT", Content: func(e eventSummary) string { return cmdio.OrDash(e.Environment) }},
		{Header: "RELEASE", Content: func(e eventSummary) string { return cmdio.OrDash(e.Release) }},
		{Header: "LEVEL", Content: func(e eventSummary) string { return cmdio.OrDash(e.Level) }},
		{Header: "TRANSACTION", Visible: cmdio.WideOnly, Content: func(e eventSummary) string { return oneLine(e.Transaction, 60) }},
		{Header: "SERVER", Visible: cmdio.WideOnly, Content: func(e eventSummary) string { return cmdio.OrDash(e.ServerName) }},
		{Header: "TRACE ID", Visible: cmdio.WideOnly, Content: func(e eventSummary) string { return cmdio.OrDash(e.TraceID) }},
	}}
}

type eventsListOpts struct {
	IO      cmdio.Options
	Project projectRef
	Time    timeRange
	Limit   int
	errID   int64

	CustomFields []string
	filter       url.Values
}

func (o *eventsListOpts) setup(flags *pflag.FlagSet) {
	o.Project.setup(flags)
	o.Time.SetupTimeFlags(flags)
	flags.StringArrayVar(&o.CustomFields, "custom-field", nil, "Keep the events that carry this custom field"+customFieldUsage+". The release and environment are custom fields too: release:v1.2.3")
	o.IO.BindListLimit(flags, &o.Limit, "events", 50)
	bindListTable(&o.IO, flags, eventTable())
}

func (o *eventsListOpts) Validate(cmd *cobra.Command, args []string) error {
	var err error
	if o.errID, err = idArg(cmd, args[0]); err != nil {
		return err
	}
	if err := o.Project.validate(cmd); err != nil {
		return err
	}
	if err := o.Time.validate(cmd); err != nil {
		return err
	}
	o.filter = url.Values{}
	if err := customFields(cmd, o.CustomFields, o.filter); err != nil {
		return err
	}
	return validIO(cmd, &o.IO)
}

func newErrorsListEventsCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &eventsListOpts{}
	cmd := &cobra.Command{
		Use:   "list <error>",
		Short: "List the events of one error.",
		Long: `List the events of one error (its ID from errors list), newest first: when
each happened, its environment, release and level. Events carry no payload
here; read one with errors events get, which takes the EVENT ID.

With --since or --from/--to, only events in that window are listed, and with
--custom-field only the events that carry the pair; the release and the
environment are custom fields too (--custom-field release:v1.2.3). --limit 0
pages through every event; a list cut off at --limit says so in list_meta and
on stderr.`,
		Example: `  # The latest events of error 42.
  gcx error-tracking errors events list 42 --project 314

  # Events of the last hour.
  gcx et errors events list 42 --project checkout-api --since 1h --limit 20

  # Events of one release from Chrome.
  gcx et errors events list 42 --project 314 --custom-field release:v1.2.3 --custom-field browser:Chrome

  # As an agent: EVENT IDs and releases only.
  gcx error-tracking errors events list 42 --project 314 --limit 20 --json eventId,release,occurredAt`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(cmd, args); err != nil {
				return err
			}
			ctx := cmd.Context()
			c, project, err := connect(ctx, loader, &opts.Project, nil)
			if err != nil {
				return err
			}
			items, hasMore, err := collectPages(ctx, opts.Limit, func(ctx context.Context, cursor string, size int) ([]eventSummary, string, error) {
				q := maps.Clone(opts.filter)
				opts.Time.set(q)
				q.Set("limit", strconv.Itoa(size))
				if cursor != "" {
					q.Set("cursor", cursor)
				}
				var out struct {
					Events     []eventSummary `json:"events"`
					NextCursor string         `json:"nextCursor"`
				}
				err := c.do(ctx, http.MethodGet, routePath("projects", project, "errors", opts.errID, "events"), q, nil, &out)
				return out.Events, out.NextCursor, err
			})
			if err != nil {
				return err
			}
			if err := encodeList(cmd, c, &opts.IO, items, cmdio.PagedListMeta(len(items), opts.Limit, hasMore, 0)); err != nil {
				return err
			}
			if len(items) == 0 && !opts.IO.JSONDiscovery {
				say(cmd.ErrOrStderr(), fmt.Sprintf("No events of error %d match in %s; see the error", opts.errID, c.projectShown()),
					fmt.Sprintf("gcx error-tracking errors inspect %d --project %d", opts.errID, project))
			}
			return nil
		},
		Annotations: map[string]string{agent.AnnotationTokenCost: "medium", agent.AnnotationLLMHint: "<error> --project 314 --since 1h --limit 20"},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type eventsGetOpts struct {
	IO      cmdio.Options
	Project projectRef
	errID   int64
	event   string
}

func (o *eventsGetOpts) setup(flags *pflag.FlagSet) {
	o.Project.setup(flags)
	bindTable(&o.IO, flags, eventTable(), func(e eventDetail) []eventSummary { return []eventSummary{e.eventSummary} })
}

func (o *eventsGetOpts) Validate(cmd *cobra.Command, args []string) error {
	var err error
	if o.errID, err = idArg(cmd, args[0]); err != nil {
		if n, perr := strconv.ParseInt(strings.TrimSpace(args[1]), 10, 64); perr == nil && n > 0 {
			return usage(cmd, fmt.Sprintf("<error> %q is not an error ID; the error comes first, then the event", printable(args[0])),
				"Swap them: "+et("errors", "events", "get", printable(strings.TrimSpace(args[1])), printable(strings.TrimSpace(args[0])), "--project", projectOf(cmd)))
		}
		return err
	}
	if o.event, err = eventArg(cmd, args[1], strconv.FormatInt(o.errID, 10)); err != nil {
		return err
	}
	if err := o.Project.validate(cmd); err != nil {
		return err
	}
	return validIO(cmd, &o.IO)
}

func (c *client) getEvent(ctx context.Context, project, errID int64, event string) (eventDetail, error) {
	var e eventDetail
	err := c.do(ctx, http.MethodGet, routePath("projects", project, "errors", errID, "events", event), nil, nil, &e)
	return e, err
}

func newErrorsGetEventCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &eventsGetOpts{}
	cmd := &cobra.Command{
		Use:   "get <error> <event>",
		Short: "Get one event of an error with its SDK payload.",
		Long: `Get one event of an error with the full SDK payload (stack trace,
breadcrumbs, tags, contexts); select fields with --json, or read the stack
trace of an error's latest event with errors inspect. <error> is the error
ID from errors list and
<event> the EVENT ID from errors events list: the SDK's 32-character event ID,
not the numeric id the JSON also carries.`,
		Example: `  # Get an event.
  gcx error-tracking errors events get 42 0f6c2b1e9d4a4c3b8e7f6a5b4c3d2e1f --project 314

  # The SDK payload only.
  gcx et errors events get 42 0f6c2b1e9d4a4c3b8e7f6a5b4c3d2e1f --project checkout-api --json payload

  # As an agent: its release, environment and time only.
  gcx error-tracking errors events get 42 0f6c2b1e9d4a4c3b8e7f6a5b4c3d2e1f --project 314 --json eventId,release,environment,occurredAt`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(cmd, args); err != nil {
				return err
			}
			c, project, err := connect(cmd.Context(), loader, &opts.Project, nil)
			if err != nil {
				return err
			}
			event, err := c.getEvent(cmd.Context(), project, opts.errID, opts.event)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), event)
		},
		Annotations: map[string]string{agent.AnnotationTokenCost: "large", agent.AnnotationLLMHint: "<error> <event> --project 314 --json eventId,release,environment,occurredAt"},
	}
	opts.setup(cmd.Flags())
	return cmd
}
