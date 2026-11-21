package errortracking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/shared"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type errorSummary struct {
	ID                 int64   `json:"id"`
	ProjectID          int64   `json:"projectId"`
	Status             string  `json:"status"`
	Regressed          bool    `json:"regressed"`
	Reviewed           bool    `json:"reviewed"`
	ReviewedAt         *string `json:"reviewedAt,omitempty"`
	ReviewedBy         *string `json:"reviewedBy,omitempty"`
	ReviewedByName     *string `json:"reviewedByName,omitempty"`
	ExceptionType      string  `json:"exceptionType"`
	Message            string  `json:"message"`
	Culprit            string  `json:"culprit"`
	Transaction        string  `json:"transaction"`
	EventCount         int64   `json:"eventCount"`
	EventsInRange      *int64  `json:"eventsInRange,omitempty"`
	FirstSeen          string  `json:"firstSeen"`
	LastSeen           string  `json:"lastSeen"`
	LastRelease        string  `json:"lastRelease"`
	IgnoredUntilAt     *string `json:"ignoredUntilAt,omitempty"`
	IgnoredUntilEvents *int64  `json:"ignoredUntilEvents,omitempty"`

	IgnoredUntilRateEvents        *int64  `json:"ignoredUntilRateEvents,omitempty"`
	IgnoredUntilRateWindowMinutes *int64  `json:"ignoredUntilRateWindowMinutes,omitempty"`
	IgnoredUntilUsers             *int64  `json:"ignoredUntilUsers,omitempty"`
	ReopenedAt                    *string `json:"reopenedAt,omitempty"`

	AffectedUsersCoverage   string `json:"affectedUsersCoverage,omitempty"`
	AffectedUsersInRange    *int64 `json:"affectedUsersInRange,omitempty"`
	IdentifiedEventsInRange *int64 `json:"identifiedEventsInRange,omitempty"`
}

type statusChange struct {
	FromStatus *string `json:"fromStatus,omitempty"`
	ToStatus   string  `json:"toStatus"`
	Actor      string  `json:"actor"`
	ActorName  *string `json:"actorName,omitempty"`
	Release    *string `json:"release,omitempty"`
	CreatedAt  string  `json:"createdAt"`

	Reason     map[string]any `json:"reason,omitempty"`
	Conditions map[string]any `json:"conditions,omitempty"`
}

type errorDetail struct {
	errorSummary

	ProjectName      string             `json:"projectName"`
	SnapshotRelease  string             `json:"snapshotRelease"`
	LatestEvent      *eventDetail       `json:"latestEvent,omitempty"`
	History          list[statusChange] `json:"history"`
	CommentCount     int64              `json:"commentCount"`
	IgnoredSince     *string            `json:"ignoredSince,omitempty"`
	IgnoredUsersSeen *int64             `json:"ignoredUsersSeen,omitempty"`
}

type eventSummary struct {
	ID          int64             `json:"id"`
	EventID     string            `json:"eventId"`
	OccurredAt  string            `json:"occurredAt"`
	Environment string            `json:"environment"`
	Release     string            `json:"release"`
	Level       string            `json:"level,omitempty"`
	ServerName  string            `json:"serverName,omitempty"`
	Transaction string            `json:"transaction,omitempty"`
	StatusCode  *int64            `json:"statusCode,omitempty"`
	TraceID     string            `json:"traceId,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
}

type eventDetail struct {
	eventSummary

	Payload json.RawMessage `json:"payload"`
}

func (e errorSummary) shownStatus() string {
	if e.Regressed && e.Status == "unresolved" {
		return "regressed"
	}
	return e.Status
}

func idArg(cmd *cobra.Command, arg string) (int64, error) {
	return positiveID(cmd, "<error>", arg)
}

func oneLine(s string, n int) string {
	s, _, cut := strings.Cut(strings.TrimSpace(s), "\n")
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	if cut {
		return s + " …"
	}
	return cmdio.OrDash(s)
}

func (e errorSummary) ignoredUntil() string {
	if e.Status != "ignored" {
		return "-"
	}
	var parts []string
	if e.IgnoredUntilAt != nil {
		parts = append(parts, minute(*e.IgnoredUntilAt))
	}
	if e.IgnoredUntilEvents != nil {
		until := fmt.Sprintf("%d events", *e.IgnoredUntilEvents)
		if more := *e.IgnoredUntilEvents - e.EventCount; more > 0 {
			until += fmt.Sprintf(" (%d more)", more)
		}
		parts = append(parts, until)
	}
	if e.IgnoredUntilRateEvents != nil && e.IgnoredUntilRateWindowMinutes != nil {
		parts = append(parts, fmt.Sprintf("%d events in %s", *e.IgnoredUntilRateEvents, minutes(*e.IgnoredUntilRateWindowMinutes)))
	}
	if e.IgnoredUntilUsers != nil {
		parts = append(parts, fmt.Sprintf("%d users", *e.IgnoredUntilUsers))
	}
	if len(parts) == 0 {
		return "reopened by hand"
	}
	return strings.Join(parts, ", ")
}

func errorColumns[T any](get func(T) errorSummary) []cmdio.Column[T] {
	col := func(h string, v cmdio.Visibility, fn func(errorSummary) string) cmdio.Column[T] {
		return cmdio.Column[T]{Header: h, Visible: v, Content: func(t T) string { return fn(get(t)) }}
	}
	all := cmdio.VisibleAll
	return []cmdio.Column[T]{
		col("ID", all, func(e errorSummary) string { return strconv.FormatInt(e.ID, 10) }),
		col("STATUS", all, errorSummary.shownStatus),
		col("TYPE", all, func(e errorSummary) string { return oneLine(e.ExceptionType, 40) }),
		col("MESSAGE", cmdio.NarrowOnly, func(e errorSummary) string { return oneLine(e.Message, 60) }),
		col("MESSAGE", cmdio.WideOnly, func(e errorSummary) string { return oneLine(e.Message, 80) }),
		col("EVENTS", all, func(e errorSummary) string { return strconv.FormatInt(e.EventCount, 10) }),
		col("LAST SEEN", when, func(e errorSummary) string { return e.LastSeen }),
		col("RELEASE", all, func(e errorSummary) string { return cmdio.OrDash(e.LastRelease) }),
		col("FIRST SEEN", cmdio.WideOnly, func(e errorSummary) string { return stamp(e.FirstSeen) }),
		col("IGNORED UNTIL", cmdio.WideOnly, errorSummary.ignoredUntil),
		col("CULPRIT", cmdio.WideOnly, func(e errorSummary) string { return oneLine(e.Culprit, 80) }),
		col("TRANSACTION", cmdio.WideOnly, func(e errorSummary) string { return oneLine(e.Transaction, 60) }),
	}
}

type errorsListOpts struct {
	IO      cmdio.Options
	Project projectRef
	Time    timeRange
	Status  string
	Search  string
	Release string
	Sort    string
	Limit   int
	sort    string

	Order        string
	Reviewed     bool
	Transaction  string
	ReopenedFrom string
	ReopenedBy   string
	Assignees    []string
	Assigned     bool
	Mine         bool
	CustomFields []string
	SinceRelease string
	filter       url.Values
	assignment   string
}

func (o *errorsListOpts) query(sinceRelease string) url.Values {
	q := maps.Clone(o.filter)
	o.Time.set(q)
	q.Set("status", o.Status)
	if key := map[string]string{"new": "firstSeenFrom", "regressed": "regressedFrom"}[o.Status]; key != "" {
		if o.Status == "new" {
			q.Set("status", "unresolved")
			q.Set("reviewed", "false")
		}
		if sinceRelease != "" {
			q.Set("from", sinceRelease)
		}
		if from := q.Get("from"); from != "" {
			q.Del("from")
			q.Set(key, from)
		}
	}
	if o.Search != "" {
		q.Set("search", o.Search)
	}
	if o.Release != "" {
		q.Set("release", o.Release)
	}
	q.Set("sort", o.sort)
	return q
}

func (o *errorsListOpts) setup(flags *pflag.FlagSet) {
	o.Project.setup(flags)
	o.Time.SetupTimeFlags(flags)
	flags.StringVar(&o.Status, "status", "unresolved", "Errors to list: unresolved, resolved, ignored, regressed (resolved errors that came back), new (unresolved, first seen since the window start and not reviewed) or all")
	flags.StringVar(&o.Search, "search", "", "Keep errors whose type, message or culprit contains this text (case-insensitive)")
	flags.StringVar(&o.Release, "release", "", "Keep errors whose latest event came from exactly this release, case-sensitive")
	flags.StringVar(&o.SinceRelease, "since-release", "", "With --status new or regressed: the window starts at this release's first event")
	flags.StringVar(&o.Sort, "sort", "last-seen", "Sort by last-seen, event-count, first-seen or events-in-range (events in the window; needs --since or --from/--to)")
	flags.StringVar(&o.Order, "order", "desc", "Sort order: desc (highest first) or asc")
	flags.BoolVar(&o.Reviewed, "reviewed", false, "Keep reviewed errors; --reviewed=false keeps the unreviewed ones")
	flags.StringVar(&o.Transaction, "transaction", "", "Keep errors last seen in exactly this transaction, case-sensitive")
	flags.StringVar(&o.ReopenedFrom, "reopened-from", "", "Keep errors reopened at or after this time, at most 7 days ago (now-1h, 2026-11-16T00:00:00Z)")
	flags.StringVar(&o.ReopenedBy, "reopened-by", "", "With --reopened-from: which reopenings count: condition (the default: an ignore's condition was met), person or any")
	flags.StringArrayVar(&o.Assignees, "assignee", nil, "Keep errors assigned to this user UID. Repeat for any of several"+needsLogin)
	flags.BoolVar(&o.Assigned, "assigned", false, "Keep assigned errors; --assigned=false keeps the unassigned ones"+needsLogin)
	flags.BoolVar(&o.Mine, "mine", false, "Keep the unresolved errors assigned to you"+needsLogin)
	flags.StringArrayVar(&o.CustomFields, "custom-field", nil, "Keep errors with an event in the window that carried this custom field"+customFieldUsage)
	o.IO.BindListLimit(flags, &o.Limit, "errors", 50)
	bindListTable(&o.IO, flags, cmdio.Table[errorSummary]{Columns: errorColumns(func(e errorSummary) errorSummary { return e })})
}

func (o *errorsListOpts) Validate(cmd *cobra.Command) error {
	if err := nonEmpty(cmd, "search", "release"); err != nil {
		return err
	}
	if err := o.Project.validate(cmd); err != nil {
		return err
	}
	statuses := []string{"unresolved", "resolved", "ignored", "regressed", "new", "all"}
	if !slices.Contains(statuses, o.Status) {
		return usage(cmd, fmt.Sprintf("--status %q: use one of %s", o.Status, strings.Join(statuses, ", ")))
	}
	o.sort = map[string]string{"last-seen": "lastSeen", "event-count": "eventCount", "first-seen": "firstSeen", "events-in-range": "eventsInRange"}[o.Sort]
	if o.sort == "" {
		return usage(cmd, fmt.Sprintf("--sort %q: use last-seen, event-count, first-seen or events-in-range", o.Sort))
	}
	if err := o.filters(cmd); err != nil {
		return err
	}
	o.Search, o.Release = strings.TrimSpace(o.Search), strings.TrimSpace(o.Release)
	windowed := o.Status == "new" || o.Status == "regressed"
	if err := nonEmpty(cmd, "since-release"); err != nil {
		return err
	}
	if o.SinceRelease = strings.TrimSpace(o.SinceRelease); o.SinceRelease != "" {
		if !windowed {
			return usage(cmd, "--since-release goes with --status new or regressed")
		}
		if o.Time.Since != "" || o.Time.From != "" {
			return usage(cmd, "--since-release replaces --since and --from: pass one of them")
		}
	}
	o.Time.openStart = windowed
	if err := o.Time.validate(cmd); err != nil {
		return err
	}
	if windowed && o.Time.to != "" && !o.Time.openEnd {
		return usage(cmd, fmt.Sprintf("--status %s takes no --to: its window runs from --since, --from or --since-release to now", o.Status))
	}
	if o.Status == "new" && o.Time.from == "" && o.SinceRelease == "" {
		return usage(cmd, "--status new needs a window start: add --since 30m, --from <time> or --since-release <release>")
	}
	if o.sort == "eventsInRange" && (o.Status == "new" || o.Status == "regressed" || o.Time.from == "" && o.Time.to == "") {
		return usage(cmd, "--sort events-in-range needs a window: add --since or --from/--to (not with --status new or regressed)")
	}
	return validIO(cmd, &o.IO)
}

func (o *errorsListOpts) filters(cmd *cobra.Command) error {
	if err := nonEmpty(cmd, "transaction", "reopened-from", "reopened-by"); err != nil {
		return err
	}
	o.filter = url.Values{}
	flags := cmd.Flags()
	switch {
	case o.Order == "asc":
		o.filter.Set("order", "asc")
	case o.Order != "desc":
		return usage(cmd, fmt.Sprintf("--order %q: use desc or asc", o.Order))
	}
	if flags.Changed("reviewed") {
		if o.Status == "new" {
			return usage(cmd, "--status new already keeps unreviewed errors; drop --reviewed")
		}
		o.filter.Set("reviewed", strconv.FormatBool(o.Reviewed))
	}
	if t := strings.TrimSpace(o.Transaction); t != "" {
		o.filter.Set("transaction", t)
	}
	if o.ReopenedFrom != "" {
		now := time.Now()
		at, err := shared.ParseTime(strings.TrimSpace(o.ReopenedFrom), now)
		if err != nil {
			return usage(cmd, fmt.Sprintf("invalid --reopened-from %q: %v", o.ReopenedFrom, err))
		}
		if at.Before(now.Add(-7 * 24 * time.Hour)) {
			return usage(cmd, fmt.Sprintf("--reopened-from %q is more than 7 days ago", o.ReopenedFrom))
		}
		o.filter.Set("reopenedFrom", at.UTC().Format(time.RFC3339))
	}
	if o.ReopenedBy != "" {
		if o.ReopenedFrom == "" {
			return usage(cmd, "--reopened-by needs --reopened-from")
		}
		if !slices.Contains([]string{"condition", "person", "any"}, o.ReopenedBy) {
			return usage(cmd, fmt.Sprintf("--reopened-by %q: use condition, person or any", o.ReopenedBy))
		}
		o.filter.Set("reopenedBy", o.ReopenedBy)
	}
	kinds := 0
	for _, uid := range o.Assignees {
		if uid = strings.TrimSpace(uid); uid == "" {
			return usage(cmd, "--assignee must not be empty")
		}
		o.filter.Add("assigneeUser", uid)
	}
	if len(o.Assignees) > 0 {
		kinds++
	}
	if flags.Changed("assigned") {
		o.filter.Set("assigned", strconv.FormatBool(o.Assigned))
		kinds++
	}
	if o.Mine {
		if slices.Contains([]string{"resolved", "ignored", "all"}, o.Status) {
			return usage(cmd, "--mine keeps unresolved errors, so it does not go with --status "+o.Status)
		}
		o.filter.Set("assignedToMe", "true")
		kinds++
	}
	if kinds > 1 {
		return usage(cmd, "use only one of --assignee, --assigned and --mine")
	}
	for _, name := range []string{"mine", "assigned", "assignee"} {
		if flags.Changed(name) && (name != "mine" || o.Mine) {
			o.assignment = "errors list --" + name
		}
	}
	return customFields(cmd, o.CustomFields, o.filter)
}

func (o *errorsListOpts) releaseStart(ctx context.Context, c *client, project int64) (string, error) {
	if o.SinceRelease == "" {
		return "", nil
	}
	r, err := c.findRelease(ctx, project, o.SinceRelease)
	if err != nil {
		return "", err
	}
	return r.FirstSeen, nil
}

var errNoReleases = errors.New("error tracking: the project has no releases")

func (c *client) findRelease(ctx context.Context, project int64, name string) (*release, error) {
	var out struct {
		Releases []release `json:"releases"`
	}
	if err := c.do(ctx, http.MethodGet, routePath("projects", project, "releases"), url.Values{"limit": {strconv.Itoa(facetCap)}}, nil, &out); err != nil {
		return nil, err
	}
	i := slices.IndexFunc(out.Releases, func(r release) bool { return name == "" || r.Release == name })
	switch {
	case i < 0 && name == "":
		return nil, errNoReleases
	case i < 0:
		return nil, &gcxerrors.DetailedError{
			Summary: gcxerrors.SummaryResourceNotFound,
			Details: fmt.Sprintf("%s has no release %q among its latest %d. %s", c.projectShown(), printable(name), facetCap, countsLag),
		}
	case out.Releases[i].FirstSeen == "":
		return nil, noFirstEvent(c, project, out.Releases[i].Release)
	}
	return &out.Releases[i], nil
}

const countsLag = "Counts can lag a minute or two behind new events"

func noFirstEvent(c *client, project int64, name string) error {
	return &gcxerrors.DetailedError{
		Summary:     gcxerrors.SummaryResourceNotFound,
		Details:     fmt.Sprintf("release %q in %s has no first event yet, so there is no window to start from", printable(name), c.projectShown()),
		Suggestions: []string{fmt.Sprintf("Pass a start instead: gcx error-tracking errors list --project %d --status new --since 1h", project)},
	}
}

func newErrorsListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &errorsListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the errors of an Error Tracking project.",
		Long: `List the errors of one Error Tracking project, newest last-seen first, with
--status picking them (default unresolved). The other flags narrow them by
text, release, transaction and custom field; by assignee (--mine, --assigned
and --assignee need a user login); by time (new and regressed take a window
start and run to now); and by count (--limit 0 for all). A list cut off at
--limit says so in list_meta and on stderr, and an empty one gets a hint.`,
		Example: `  # The noisiest unresolved errors.
  gcx error-tracking errors list --project 314 --sort event-count --limit 10

  # Errors seen in the last day, by project name.
  gcx et errors list --project checkout-api --since 24h

  # New and regressed errors since a release went out.
  gcx et errors list --project 314 --status new --since-release v1.2.3
  gcx et errors list --project 314 --status regressed --since-release v1.2.3

  # Unreviewed errors assigned to you, the busiest of the last hour first.
  gcx et errors list --project 314 --mine --reviewed=false --since 1h --sort events-in-range

  # As an agent: IDs, messages and counts only.
  gcx error-tracking errors list --project 314 --status unresolved --limit 20 --json id,message,eventCount,lastSeen`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(cmd); err != nil {
				return err
			}
			ctx := cmd.Context()
			var guard func(*client) error
			if opts.assignment != "" {
				guard = humanGuard(cmd, opts.assignment, false, "")
			}
			c, id, err := connect(ctx, loader, &opts.Project, guard)
			if err != nil {
				return err
			}
			start, err := opts.releaseStart(ctx, c, id)
			if err != nil {
				return err
			}
			base := opts.query(start)
			items, hasMore, err := collectPages(ctx, opts.Limit, func(ctx context.Context, cursor string, size int) ([]errorSummary, string, error) {
				q := maps.Clone(base)
				// A windowed query costs the backend more, so cap its page size.
				if q.Has("from") || q.Has("firstSeenFrom") || q.Has("regressedFrom") {
					size = min(size, 50)
				}
				q.Set("limit", strconv.Itoa(size))
				if cursor != "" {
					q.Set("cursor", cursor)
				}
				var page struct {
					Errors     []errorSummary `json:"errors"`
					NextCursor string         `json:"nextCursor"`
				}
				err := c.do(ctx, http.MethodGet, routePath("projects", id, "errors"), q, nil, &page)
				return page.Errors, page.NextCursor, err
			})
			if err != nil && opts.assignment != "" {
				return humanOnly(err, false)
			}
			if err != nil {
				return err
			}
			meta := cmdio.PagedListMeta(len(items), opts.Limit, hasMore, 0)
			if err := encodeList(cmd, c, &opts.IO, items, meta); err != nil {
				return err
			}
			if len(items) == 0 && !opts.IO.JSONDiscovery {
				widen := "No errors match; widen the filters or --since"
				if opts.Status != "all" {
					widen += ", or see every state with --status all"
				}
				say(cmd.ErrOrStderr(), widen+". "+countsLag, "")
			}
			return nil
		},
		Annotations: map[string]string{agent.AnnotationTokenCost: "medium", agent.AnnotationLLMHint: "--project 314 --since 24h --limit 20 --json id,message,eventCount,lastSeen"},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type errorsGetOpts struct{ errorTarget }

func (o *errorsGetOpts) setup(flags *pflag.FlagSet) {
	o.Project.setup(flags)
	bindSingleTable(&o.IO, flags, cmdio.Table[errorDetail]{Columns: append(errorColumns(func(d errorDetail) errorSummary { return d.errorSummary }),
		cmdio.Column[errorDetail]{Header: "LATEST EVENT", Visible: when, Content: func(d errorDetail) string {
			if d.LatestEvent == nil {
				return "-"
			}
			return d.LatestEvent.OccurredAt
		}})})
}

func newErrorsGetCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &errorsGetOpts{}
	cmd := &cobra.Command{
		Use:   "get <error>",
		Short: "Get one error with its latest event.",
		Long: `Get one error of an Error Tracking project: its counts, status, last ten
status changes and its latest event with the full SDK payload (stack trace,
breadcrumbs, tags). The payload can be large; select fields with --json.

<error> is the error ID from errors list. errors inspect shows the same error
as a card with its stack trace, recent activity and assignees.`,
		Example: `  # Get an error.
  gcx error-tracking errors get 42 --project 314

  # The latest event's payload only.
  gcx et errors get 42 --project checkout-api --json latestEvent.payload

  # As an agent: the essentials, without the large payload.
  gcx error-tracking errors get 42 --project 314 --json id,status,message,culprit,eventCount,lastSeen,latestEvent.eventId`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(cmd, args[0]); err != nil {
				return err
			}
			ctx := cmd.Context()
			c, path, err := opts.connect(ctx, loader, nil)
			if err != nil {
				return err
			}
			var detail errorDetail
			if err := c.do(ctx, http.MethodGet, path, nil, nil, &detail); err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), detail)
		},
		Annotations: map[string]string{agent.AnnotationTokenCost: "large", agent.AnnotationLLMHint: "<error> --project 314 --json id,status,message,culprit,eventCount,lastSeen,latestEvent.eventId"},
	}
	opts.setup(cmd.Flags())
	return cmd
}
