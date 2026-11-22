package errortracking

import (
	"cmp"
	"context"
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
	"golang.org/x/sync/errgroup"
)

type errorsUpdateRequest struct {
	Status         string  `json:"status"`
	IgnoredUntilAt *string `json:"ignoredUntilAt,omitempty"`
	MoreEvents     *int64  `json:"moreEvents,omitempty"`

	RateEvents        *int64 `json:"rateEvents,omitempty"`
	RateWindowMinutes *int64 `json:"rateWindowMinutes,omitempty"`
	MoreUsers         *int64 `json:"moreUsers,omitempty"`
}

type errorsUpdateOpts struct {
	IO         cmdio.Options
	Project    projectRef
	Status     string
	Until      string
	MoreEvents int64
	RateEvents int64
	RateWindow string
	MoreUsers  int64
	Reviewed   bool
	req        errorsUpdateRequest
	ids        []int64
}

func (o *errorsUpdateOpts) setup(flags *pflag.FlagSet) {
	o.Project.setup(flags)
	flags.StringVar(&o.Status, "status", "", "New status: resolved, unresolved or ignored")
	flags.StringVar(&o.Until, "until", "", "With --status ignored: ignore until this time or for this long (7d, 2026-12-01T00:00:00Z, now+7d)")
	flags.Int64Var(&o.MoreEvents, "more-events", 0, "With --status ignored: ignore until this many more events arrive")
	flags.Int64Var(&o.RateEvents, "rate-events", 0, "With --status ignored and --rate-window: ignore until this many events arrive within the window")
	flags.StringVar(&o.RateWindow, "rate-window", "", "With --rate-events: the window, 15m to 7d in steps of 5m (15m, 1h, 1d)")
	flags.Int64Var(&o.MoreUsers, "more-users", 0, "With --status ignored: ignore until about this many more users are affected")
	flags.BoolVar(&o.Reviewed, "reviewed", false, "Mark the errors reviewed instead of changing their status, so they no longer count as new. "+humanOnlyNote)
	bindText(&o.IO, flags, mutationText{})
}

func (o *errorsUpdateOpts) Validate(cmd *cobra.Command, args []string) error {
	seen := map[int64]bool{}
	o.ids = nil
	for _, a := range args {
		id, err := idArg(cmd, a)
		if err != nil {
			return err
		}
		if !seen[id] {
			seen[id] = true
			o.ids = append(o.ids, id)
		}
	}
	if err := o.Project.validate(cmd); err != nil {
		return err
	}
	if flags := cmd.Flags(); flags.Changed("reviewed") {
		if !o.Reviewed {
			return usage(cmd, "--reviewed=false is not possible: a review cannot be undone")
		}
		if slices.ContainsFunc([]string{"status", "until", "more-events", "rate-events", "rate-window", "more-users"}, flags.Changed) {
			return usage(cmd, "--reviewed goes alone: drop --status and the ignore conditions")
		}
		return validIO(cmd, &o.IO)
	}
	if err := nonEmpty(cmd, "until", "rate-window"); err != nil {
		return err
	}
	var err error
	if o.req, err = o.request(cmd, time.Now()); err != nil {
		return err
	}
	return validIO(cmd, &o.IO)
}

func (o *errorsUpdateOpts) request(cmd *cobra.Command, now time.Time) (errorsUpdateRequest, error) {
	req := errorsUpdateRequest{Status: strings.TrimSpace(o.Status)}
	switch req.Status {
	case "resolved", "unresolved", "ignored":
	case "":
		return req, usage(cmd, "pass --status (resolved, unresolved or ignored) or --reviewed")
	default:
		return req, usage(cmd, fmt.Sprintf("--status %q: use resolved, unresolved or ignored", o.Status))
	}
	until := strings.TrimSpace(o.Until)
	flags := cmd.Flags()
	if req.Status != "ignored" {
		if until != "" || slices.ContainsFunc([]string{"more-events", "rate-events", "rate-window", "more-users"}, flags.Changed) {
			return req, usage(cmd, "--until, --more-events, --rate-events, --rate-window and --more-users only go with --status ignored")
		}
		return req, nil
	}
	if until != "" {
		at, err := shared.ParseTime(until, now)
		if d, derr := shared.ParseDuration(until); derr == nil {
			at, err = now.Add(d), nil
		}
		if err != nil {
			return req, usage(cmd, fmt.Sprintf("invalid --until %q: use a duration (7d) or a time (RFC 3339, now+7d)", o.Until))
		}
		if !at.After(now) {
			return req, usage(cmd, fmt.Sprintf("--until %q must be in the future", o.Until))
		}
		s := at.UTC().Format(time.RFC3339)
		req.IgnoredUntilAt = &s
	}
	if flags.Changed("more-events") {
		if o.MoreEvents < 1 || o.MoreEvents > 1_000_000_000 {
			return req, usage(cmd, "--more-events must be 1 to 1,000,000,000")
		}
		req.MoreEvents = &o.MoreEvents
	}
	if flags.Changed("rate-events") != flags.Changed("rate-window") {
		return req, usage(cmd, "--rate-events and --rate-window go together")
	}
	if flags.Changed("rate-events") {
		if o.RateEvents < 1 || o.RateEvents > 1_000_000_000 {
			return req, usage(cmd, "--rate-events must be 1 to 1,000,000,000")
		}
		d, err := shared.ParseDuration(strings.TrimSpace(o.RateWindow))
		minutes := int64(d / time.Minute)
		if err != nil || d%(5*time.Minute) != 0 || minutes < 15 || minutes > 10080 {
			return req, usage(cmd, fmt.Sprintf("invalid --rate-window %q: use 15m to 7d in steps of 5m", o.RateWindow))
		}
		req.RateEvents, req.RateWindowMinutes = &o.RateEvents, &minutes
	}
	if flags.Changed("more-users") {
		if o.MoreUsers < 1 || o.MoreUsers > 1_000_000 {
			return req, usage(cmd, "--more-users must be 1 to 1,000,000")
		}
		req.MoreUsers = &o.MoreUsers
	}
	return req, nil
}

type updateOutcome struct {
	ID             int64  `json:"id"`
	PreviousStatus string `json:"previousStatus,omitempty"`
	Status         string `json:"status,omitempty"`
	Changed        bool   `json:"changed"`

	kind, message, projectName string
	err                        error
}

type batchUpdate struct {
	cmdio.BatchMutation `json:",inline"`

	Project *projectInfo    `json:"project,omitempty"`
	Results []updateOutcome `json:"results"`

	text string
}

func batchTable() cmdio.Table[updateOutcome] {
	return cmdio.Table[updateOutcome]{Columns: []cmdio.Column[updateOutcome]{
		{Header: "ID", Content: func(u updateOutcome) string { return strconv.FormatInt(u.ID, 10) }},
		{Header: "TYPE", Content: func(u updateOutcome) string { return oneLine(u.kind, 30) }},
		{Header: "MESSAGE", Content: func(u updateOutcome) string { return oneLine(u.message, 40) }},
		{Header: "WAS", Content: func(u updateOutcome) string { return cmdio.OrDash(u.PreviousStatus) }},
		{Header: "NOW", Content: func(u updateOutcome) string {
			if !u.Changed {
				return u.Status + " (no change)"
			}
			return u.Status
		}},
	}}
}

func statusVerb(status string) string {
	return map[string]string{"resolved": "Resolved", "unresolved": "Reopened", "ignored": "Ignored"}[status]
}

func (o *errorsUpdateOpts) target() string {
	if o.Reviewed {
		return "reviewed"
	}
	return o.req.Status
}

func (o *errorsUpdateOpts) apply(ctx context.Context, c *client, project, id int64) updateOutcome {
	out := updateOutcome{ID: id}
	path := routePath("projects", project, "errors", id)
	var before errorDetail
	if out.err = c.do(ctx, http.MethodGet, path, nil, nil, &before); out.err != nil {
		return out
	}
	out.kind, out.message, out.projectName = before.ExceptionType, before.Message, before.ProjectName
	out.PreviousStatus, out.Status = before.Status, before.Status
	if o.Reviewed {
		if before.Reviewed {
			return out
		}
		if err := c.do(ctx, http.MethodPost, path+"/review", nil, nil, nil); err != nil {
			out.err = humanOnly(err, true)
			return out
		}
		out.Changed = true
		return out
	}
	if before.Status == o.req.Status && o.req.Status != "ignored" {
		return out
	}
	var after errorDetail
	if err := c.do(ctx, http.MethodPatch, path, nil, o.req, &after); err != nil {
		status, ok := errors.AsType[*gcxerrors.HTTPStatusError](err)
		current, hasCurrent := errors.AsType[*statusConflictError](err)
		switch {
		case ok && status.Status == http.StatusConflict && hasCurrent && current.CurrentStatus == o.req.Status:
			out.Status = o.req.Status
		case ok && status.Status == http.StatusConflict:
			out.err = &gcxerrors.DetailedError{
				Summary:     gcxerrors.SummaryResourceConflict,
				Details:     status.ServerMessage,
				Suggestions: []string{fmt.Sprintf("See its status: gcx error-tracking errors get %d --project %d", id, project)},
			}
		case ok && o.MoreUsers > 0 && strings.Contains(strings.ToLower(status.ServerMessage), "user"):
			out.err = badRequest(err, "--more-users: ignore by --more-events, --rate-events with --rate-window, or --until instead")
		default:
			out.err = err
		}
		return out
	}
	out.Status, out.Changed = after.Status, true
	return out
}

func (o *errorsUpdateOpts) batch(cmd *cobra.Command, c *client, outcomes []updateOutcome) error {
	b := batchUpdate{BatchMutation: cmdio.NewBatchMutation("updated"), Project: c.projectInfo(), Results: []updateOutcome{}}
	if o.Reviewed {
		b.Action = "reviewed"
	}
	var codes []int
	for _, u := range outcomes {
		t := cmdio.MutationTarget{Kind: "Error", ID: strconv.FormatInt(u.ID, 10), Namespace: c.namespace}
		switch {
		case u.err != nil:
			msg := u.err.Error()
			code := gcxerrors.ExitGeneralError
			if s, ok := errors.AsType[*gcxerrors.HTTPStatusError](u.err); ok && (s.Status == http.StatusUnauthorized || s.Status == http.StatusForbidden) {
				code = gcxerrors.ExitAuthFailure
			}
			if d, ok := errors.AsType[*gcxerrors.DetailedError](u.err); ok {
				msg = cmp.Or(d.Details, d.Summary)
				if d.ExitCode != nil {
					code = *d.ExitCode
				}
			}
			b.Failures = append(b.Failures, cmdio.MutationFailure{Target: t, Error: printable(strings.ReplaceAll(msg, "\n", " "))})
			codes = append(codes, code)
			b.Summary.Failed++
		case !u.Changed:
			b.Results = append(b.Results, u)
			b.Summary.Skipped++
			if b.SkippedReasons == nil {
				b.SkippedReasons = map[string]int{}
			}
			b.SkippedReasons["already "+o.target()]++
		default:
			b.Summary.Succeeded++
		}
	}
	if len(outcomes) == 1 && b.Summary.Failed == 1 {
		return outcomes[0].err
	}
	if o.Reviewed {
		b.text = fmt.Sprintf("Marked %d of %d errors in %s reviewed", b.Summary.Succeeded, len(outcomes), c.projectShown())
	} else {
		b.text = fmt.Sprintf("%s %d of %d errors in %s", statusVerb(o.target()), b.Summary.Succeeded, len(outcomes), c.projectShown())
	}
	if n := b.Summary.Skipped; n > 0 {
		b.text += fmt.Sprintf("; %d %s already %s", n, map[bool]string{true: "was", false: "were"}[n == 1], o.target())
	}
	if n := b.Summary.Failed; n > 0 {
		b.text += fmt.Sprintf("; %d failed", n)
	}
	if err := emit(cmd, &o.IO, b); err != nil {
		return err
	}
	if b.Summary.Failed > 0 {
		code := gcxerrors.ExitPartialFailure
		if b.Summary.Failed == len(outcomes) {
			code = gcxerrors.ExitGeneralError
			if slices.Min(codes) == slices.Max(codes) {
				code = codes[0]
			}
		}
		return gcxerrors.NewEmittedError(code, gcxerrors.NewPartialFailureError("update", len(outcomes), b.Summary.Failed))
	}
	return nil
}

func newErrorsUpdateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &errorsUpdateOpts{}
	cmd := &cobra.Command{
		Use:   "update <error>...",
		Short: "Resolve, reopen, ignore or review errors.",
		Long: `Change the status of one or more errors of one project, or mark them
reviewed. --status ignored hides them until a person or a regression reopens
them or the first condition given is met (--until, --more-events,
--rate-events with --rate-window, --more-users). An error already in that
status is a success with changed: false, except with --status ignored, which
replaces its conditions and so always changes it. The result counts the
changes and lists only the errors that failed or were already in that status,
whatever the number of IDs. The command exits 4 if only some failed, and
with the failures' own exit code if every one failed the same way (1 if
they differ). ` + noDryRun,
		Example: `  # Resolve an error.
  gcx error-tracking errors update 42 --project 314 --status resolved

  # Ignore it for a week, or until 100 more events arrive.
  gcx et errors update 42 --project checkout-api --status ignored --until 7d --more-events 100

  # Resolve every unresolved error last seen in a release.
  gcx et errors list --project 314 --release v1.2.3 --limit 0 --jq '.items[].id' | xargs -r gcx et errors update --project 314 --status resolved

  # Mark two errors reviewed.
  gcx et errors update 42 43 --project 314 --reviewed

  # As an agent: resolve and check what changed.
  gcx error-tracking errors update 42 --project 314 --status resolved -o json --jq '.summary'`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(cmd, args); err != nil {
				return err
			}
			ctx := cmd.Context()
			var guard func(*client) error
			if opts.Reviewed {
				guard = humanGuard(cmd, "errors update --reviewed", true, args[0])
			}
			c, project, err := connect(ctx, loader, &opts.Project, guard)
			if err != nil {
				return err
			}
			outcomes := make([]updateOutcome, len(opts.ids))
			g, gctx := errgroup.WithContext(ctx)
			g.SetLimit(10)
			for i, id := range opts.ids {
				g.Go(func() error {
					outcomes[i] = opts.apply(gctx, c, project, id)
					return nil
				})
			}
			_ = g.Wait()
			for _, u := range outcomes {
				c.knowProject(project, u.projectName)
			}
			return opts.batch(cmd, c, outcomes)
		},
		Annotations: map[string]string{agent.AnnotationTokenCost: "small", agent.AnnotationLLMHint: "<error>... --project 314 --status resolved -o json"},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type timeline struct {
	Bucket  string               `json:"bucket"`
	Buckets list[timelineBucket] `json:"buckets"`

	AffectedUsersCoverage   string `json:"affectedUsersCoverage,omitempty"`
	AffectedUsersInRange    *int64 `json:"affectedUsersInRange,omitempty"`
	IdentifiedEventsInRange *int64 `json:"identifiedEventsInRange,omitempty"`
}

type timelineBucket struct {
	Bucket     string `json:"bucket"`
	EventCount int64  `json:"eventCount"`
}

type timelineWindow struct {
	IO     cmdio.Options
	Time   timeRange
	Bucket string
	filter url.Values
}

func bar(n, peak int64) string {
	if n <= 0 || peak <= 0 {
		return ""
	}
	return strings.Repeat("█", max(1, int(n*20/peak)))
}

func (w *timelineWindow) setup(flags *pflag.FlagSet) {
	w.Time.SetupTimeFlags(flags)
	flags.StringVar(&w.Bucket, "step", "", "Bucket width: 5m, 15m, 1h or 1d (default: 1h up to 24 hours, 1d beyond)")
	var peak int64
	bindTable(&w.IO, flags, cmdio.Table[timelineBucket]{Columns: []cmdio.Column[timelineBucket]{
		{Header: "BUCKET", Content: func(b timelineBucket) string { return minute(b.Bucket) }},
		{Header: "EVENTS", Content: func(b timelineBucket) string { return strconv.FormatInt(b.EventCount, 10) }},
		{Header: "", Content: func(b timelineBucket) string { return bar(b.EventCount, peak) }},
	}}, func(t timeline) []timelineBucket {
		peak = 0
		for _, b := range t.Buckets {
			peak = max(peak, b.EventCount)
		}
		return t.Buckets
	})
}

func (w *timelineWindow) encode(cmd *cobra.Command, tl timeline) error {
	return w.IO.Encode(cmd.OutOrStdout(), tl)
}

func (w *timelineWindow) validate(cmd *cobra.Command) error {
	if f := cmd.Flags(); !f.Changed("from") && !f.Changed("to") && !f.Changed("since") {
		w.Time.Since = "24h"
	}
	if err := nonEmpty(cmd, "step"); err != nil {
		return err
	}
	if err := w.Time.validate(cmd); err != nil {
		return err
	}
	from, _ := time.Parse(time.RFC3339, w.Time.from)
	to, _ := time.Parse(time.RFC3339, w.Time.to)
	if to.Sub(from) > 30*24*time.Hour {
		if w.Time.Since != "" {
			return usage(cmd, fmt.Sprintf("--since %s: a timeline covers at most 30 days", w.Time.Since))
		}
		return usage(cmd, fmt.Sprintf("--from %s --to %s: a timeline covers at most 30 days", w.Time.From, w.Time.To))
	}
	if w.Bucket != "" && !slices.Contains([]string{"5m", "15m", "1h", "1d"}, w.Bucket) {
		return usage(cmd, fmt.Sprintf("--step %q: use 5m, 15m, 1h or 1d", w.Bucket))
	}
	return validIO(cmd, &w.IO)
}

func (w *timelineWindow) get(ctx context.Context, c *client, path string) (timeline, error) {
	q := url.Values{}
	maps.Copy(q, w.filter)
	w.Time.setClosed(q)
	if w.Bucket != "" {
		q.Set("bucket", w.Bucket)
	}
	var out timeline
	err := c.do(ctx, http.MethodGet, path, q, nil, &out)
	return out, err
}

type errorsTimelineOpts struct {
	timelineWindow

	Project projectRef
}

func (o *errorsTimelineOpts) setup(flags *pflag.FlagSet) {
	o.Project.setup(flags)
	o.timelineWindow.setup(flags)
}

func (o *errorsTimelineOpts) Validate(cmd *cobra.Command) error {
	if err := o.Project.validate(cmd); err != nil {
		return err
	}
	return o.validate(cmd)
}

func newErrorsTimelineCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &errorsTimelineOpts{}
	cmd := &cobra.Command{
		Use:   "timeline <error>",
		Short: "Show an error's event counts over time.",
		Long: `Show how many events one error got per time bucket, over the last 24 hours
unless --since or --from/--to say otherwise (at most 30 days).`,
		Example: `  # Events per bucket over the last day.
  gcx error-tracking errors timeline 42 --project 314

  # The last week.
  gcx et errors timeline 42 --project checkout-api --since 7d

  # As an agent: a fixed window as JSON.
  gcx error-tracking errors timeline 42 --project 314 --from 2026-11-16T00:00:00Z --to 2026-11-17T00:00:00Z -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := idArg(cmd, args[0])
			if err != nil {
				return err
			}
			if err := opts.Validate(cmd); err != nil {
				return err
			}
			ctx := cmd.Context()
			c, project, err := connect(ctx, loader, &opts.Project, nil)
			if err != nil {
				return err
			}
			tl, err := opts.get(ctx, c, routePath("projects", project, "errors", id, "timeline"))
			if err != nil {
				return err
			}
			return opts.encode(cmd, tl)
		},
		Annotations: map[string]string{agent.AnnotationTokenCost: "medium", agent.AnnotationLLMHint: "<error> --project 314 --since 24h"},
	}
	opts.setup(cmd.Flags())
	return cmd
}
