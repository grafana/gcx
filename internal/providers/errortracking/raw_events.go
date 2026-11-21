package errortracking

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type rawEventSummary struct {
	ID         int64  `json:"id"`
	EventID    string `json:"eventId"`
	ReceivedAt string `json:"receivedAt"`
	Outcome    string `json:"outcome"`
	Size       int64  `json:"size"`
}

type rawEventDetail struct {
	rawEventSummary

	Payload json.RawMessage `json:"payload"`
}

func rawEventTable() cmdio.Table[rawEventSummary] {
	return cmdio.Table[rawEventSummary]{Columns: []cmdio.Column[rawEventSummary]{
		{Header: "ID", Content: func(e rawEventSummary) string { return strconv.FormatInt(e.ID, 10) }},
		{Header: "EVENT ID", Content: func(e rawEventSummary) string { return cmdio.OrDash(e.EventID) }},
		{Header: "RECEIVED", Visible: when, Content: func(e rawEventSummary) string { return e.ReceivedAt }},
		{Header: "OUTCOME", Content: func(e rawEventSummary) string { return e.Outcome }},
		{Header: "SIZE", Content: func(e rawEventSummary) string { return strconv.FormatInt(e.Size, 10) }},
	}}
}

type rawEventsListOpts struct {
	IO      cmdio.Options
	Project projectRef
	EventID string
	Limit   int
}

func (o *rawEventsListOpts) setup(flags *pflag.FlagSet) {
	o.Project.setup(flags)
	flags.StringVar(&o.EventID, "event-id", "", "Keep the items with this SDK event ID")
	o.IO.BindListLimit(flags, &o.Limit, "raw events", 50)
	bindListTable(&o.IO, flags, rawEventTable())
}

func (o *rawEventsListOpts) Validate(cmd *cobra.Command) error {
	if err := o.Project.validate(cmd); err != nil {
		return err
	}
	if err := nonEmpty(cmd, "event-id"); err != nil {
		return err
	}
	return validIO(cmd, &o.IO)
}

func newRawEventsListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &rawEventsListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the items an SDK sent to a project, newest first.",
		Long: `List the items an SDK sent to one Error Tracking project, newest first, with
what ingest did with each (outcome) and its size in bytes; Error Tracking
keeps only the latest items.

Use it to check that an SDK event arrived: --event-id keeps the items with
that SDK event ID, dashes and capitals allowed. --limit 0 pages through
every item; a list cut off at --limit says so in list_meta and on stderr.
Get one item's bytes with raw-events get.`,
		Example: `  # The latest items a project received.
  gcx error-tracking raw-events list --project 314

  # Did this event arrive, and what happened to it?
  gcx et raw-events list --project checkout-api --event-id 0f6c2b1e9d4a4c3b8e7f6a5b4c3d2e1f

  # As an agent: the last 20 outcomes only.
  gcx error-tracking raw-events list --project 314 --limit 20 --json id,eventId,outcome`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(cmd); err != nil {
				return err
			}
			ctx := cmd.Context()
			c, project, err := connect(ctx, loader, &opts.Project, nil)
			if err != nil {
				return err
			}
			eventID := strings.TrimSpace(opts.EventID)
			items, hasMore, err := collectPages(ctx, opts.Limit, func(ctx context.Context, cursor string, size int) ([]rawEventSummary, string, error) {
				q := url.Values{}
				if eventID != "" {
					q.Set("eventId", eventID)
				}
				q.Set("limit", strconv.Itoa(size))
				if cursor != "" {
					q.Set("cursor", cursor)
				}
				var out struct {
					RawEvents  []rawEventSummary `json:"rawEvents"`
					NextCursor string            `json:"nextCursor"`
				}
				err := c.do(ctx, http.MethodGet, routePath("projects", project, "rawEvents"), q, nil, &out)
				return out.RawEvents, out.NextCursor, err
			})
			if err != nil {
				return err
			}
			if err := encodeList(cmd, c, &opts.IO, items, cmdio.PagedListMeta(len(items), opts.Limit, hasMore, 0)); err != nil {
				return err
			}
			if len(items) == 0 && !opts.IO.JSONDiscovery {
				w := cmd.ErrOrStderr()
				if eventID != "" {
					say(w, fmt.Sprintf("No item with event ID %q in the latest items of %s. Event IDs are 32 hex characters", printable(eventID), c.projectShown()), "")
				} else {
					say(w, fmt.Sprintf("Nothing has arrived in %s yet; point an SDK at its DSN", c.projectShown()), fmt.Sprintf("gcx error-tracking projects get %d --json spec.dsn", project))
				}
			}
			return nil
		},
		Annotations: map[string]string{agent.AnnotationTokenCost: "medium", agent.AnnotationLLMHint: "--project 314 --event-id <id>"},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type rawEventsGetOpts struct {
	IO      cmdio.Options
	Project projectRef
}

func (o *rawEventsGetOpts) setup(flags *pflag.FlagSet) {
	o.Project.setup(flags)
	bindTable(&o.IO, flags, rawEventTable(), func(e rawEventDetail) []rawEventSummary { return []rawEventSummary{e.rawEventSummary} })
}

func (o *rawEventsGetOpts) Validate(cmd *cobra.Command) error {
	if err := o.Project.validate(cmd); err != nil {
		return err
	}
	return validIO(cmd, &o.IO)
}

func (c *client) rawEventByEventID(ctx context.Context, project int64, eventID string) (int64, error) {
	var out struct {
		RawEvents []rawEventSummary `json:"rawEvents"`
	}
	q := url.Values{"eventId": {eventID}, "limit": {strconv.Itoa(eventIDMatches)}}
	if err := c.do(ctx, http.MethodGet, routePath("projects", project, "rawEvents"), q, nil, &out); err != nil {
		return 0, err
	}
	switch len(out.RawEvents) {
	case 0:
		return 0, &gcxerrors.DetailedError{
			Summary:     gcxerrors.SummaryResourceNotFound,
			Details:     fmt.Sprintf("no item with event ID %q in the latest items of %s; Error Tracking keeps only the latest items, and event IDs are 32 hex characters", printable(eventID), c.projectShown()),
			Suggestions: []string{fmt.Sprintf("List what arrived: gcx error-tracking raw-events list --project %d", project)},
		}
	case 1:
		return out.RawEvents[0].ID, nil
	}
	ids := make([]string, 0, shownMatches)
	for _, e := range out.RawEvents[:min(len(out.RawEvents), shownMatches)] {
		ids = append(ids, fmt.Sprintf("%d (%s, %s)", e.ID, printable(e.Outcome), minute(e.ReceivedAt)))
	}
	return 0, &gcxerrors.DetailedError{
		Summary:     gcxerrors.SummaryInvalidCommandUsage,
		Details:     fmt.Sprintf("event ID %q matches %s: %s", printable(eventID), matchCount(len(out.RawEvents)), strings.Join(ids, ", ")),
		Suggestions: []string{"Pass one of these raw event IDs instead"},
		ExitCode:    new(gcxerrors.ExitUsageError),
	}
}

const (
	eventIDMatches = 50
	shownMatches   = 10
)

func matchCount(n int) string {
	switch {
	case n >= eventIDMatches:
		return fmt.Sprintf("%d or more items; the first %d are shown", n, shownMatches)
	case n > shownMatches:
		return fmt.Sprintf("%d items; the first %d are shown", n, shownMatches)
	}
	return fmt.Sprintf("%d items", n)
}

func newRawEventsGetCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &rawEventsGetOpts{}
	cmd := &cobra.Command{
		Use:   "get <raw-event>",
		Short: "Get one item an SDK sent, with its bytes.",
		Long: `Get one item an SDK sent to an Error Tracking project, with the item itself
in payload: embedded when it is JSON, else as a string. The payload can be
large; select fields with --json.

<raw-event> is the ID from raw-events list, or the SDK's EVENT ID, which
costs one raw-events list --event-id first; when several items carry that
EVENT ID, they are listed so you can pass one ID.`,
		Example: `  # Get an item.
  gcx error-tracking raw-events get 981 --project 314

  # Get the item an SDK event arrived in, by its EVENT ID.
  gcx et raw-events get 0f6c2b1e9d4a4c3b8e7f6a5b4c3d2e1f --project checkout-api

  # As an agent: what ingest did with it, and its size.
  gcx error-tracking raw-events get 981 --project 314 --json outcome,size`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			arg := strings.TrimSpace(args[0])
			id, numeric, ok := parseID(arg)
			byEventID := len(arg) >= 32 || !numeric
			if (!byEventID && !ok) || arg == "" || arg == "." || arg == ".." {
				return positiveIDErr(cmd, "<raw-event>", arg)
			}
			if err := opts.Validate(cmd); err != nil {
				return err
			}
			ctx := cmd.Context()
			c, project, err := connect(ctx, loader, &opts.Project, nil)
			if err != nil {
				return err
			}
			if byEventID {
				if id, err = c.rawEventByEventID(ctx, project, arg); err != nil {
					return err
				}
			}
			var item rawEventDetail
			if err := c.do(ctx, http.MethodGet, routePath("projects", project, "rawEvents", id), nil, nil, &item); err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), item)
		},
		Annotations: map[string]string{agent.AnnotationTokenCost: "large", agent.AnnotationLLMHint: "<raw-event> --project 314 --json outcome,size"},
	}
	opts.setup(cmd.Flags())
	return cmd
}
