package errortracking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

type release struct {
	Release    string `json:"release"`
	FirstSeen  string `json:"firstSeen"`
	LastSeen   string `json:"lastSeen"`
	EventCount int64  `json:"eventCount"`
}

type facetsOpts[T any] struct {
	IO      cmdio.Options
	Project projectRef
	Time    timeRange
	window  bool
	table   cmdio.Table[T]
	empty   func(cmd *cobra.Command, c *client, project int64)
	hint    string
}

func (o *facetsOpts[T]) setup(flags *pflag.FlagSet) {
	o.Project.setup(flags)
	if o.window {
		o.Time.SetupTimeFlags(flags)
	}
	bindListTable(&o.IO, flags, o.table)
}

func (o *facetsOpts[T]) Validate(cmd *cobra.Command) error {
	if err := o.Project.validate(cmd); err != nil {
		return err
	}
	if err := o.Time.validate(cmd); err != nil {
		return err
	}
	return validIO(cmd, &o.IO)
}

func (o *facetsOpts[T]) none(what, why string) func(cmd *cobra.Command, c *client, project int64) {
	return func(cmd *cobra.Command, c *client, project int64) {
		summary := fmt.Sprintf("No %s in %s", what, c.projectShown())
		if o.Time.from != "" || o.Time.to != "" {
			summary += " in that window"
		}
		say(cmd.ErrOrStderr(), summary+"; "+why+". Counts can lag a minute or two behind new events; see what arrived",
			fmt.Sprintf("gcx error-tracking raw-events list --project %d", project))
	}
}

const facetCap = 100

type seen struct {
	name        string
	events      int64
	first, last string
}

func seenTable[T any](header string, of func(T) seen) cmdio.Table[T] {
	return cmdio.Table[T]{Columns: []cmdio.Column[T]{
		{Header: header, Content: func(v T) string { return cmdio.OrDash(of(v).name) }},
		{Header: "EVENTS", Content: func(v T) string { return strconv.FormatInt(of(v).events, 10) }},
		{Header: "FIRST SEEN", Visible: when, Content: func(v T) string { return of(v).first }},
		{Header: "LAST SEEN", Visible: when, Content: func(v T) string { return of(v).last }},
	}}
}

func (o *facetsOpts[T]) sdkFacet(cmd *cobra.Command, loader *providers.ConfigLoader, kind string) *cobra.Command {
	cmd.Args = cobra.NoArgs
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := o.Validate(cmd); err != nil {
			return err
		}
		o.empty = o.none(kind+"s", "an event carries one when the SDK sets its "+kind+" option")
		return o.list(cmd, loader, kind+"s", kind+"s", url.Values{})
	}
	o.setup(cmd.Flags())
	return cmd
}

func (o *facetsOpts[T]) list(cmd *cobra.Command, loader *providers.ConfigLoader, route, field string, q url.Values) error {
	ctx := cmd.Context()
	c, id, err := connect(ctx, loader, &o.Project, nil)
	if err != nil {
		return err
	}
	o.Time.set(q)
	q.Set("limit", strconv.Itoa(facetCap))
	var out map[string]json.RawMessage
	if err := c.do(ctx, http.MethodGet, routePath("projects", id)+"/"+route, q, nil, &out); err != nil {
		if o.hint != "" {
			return badRequest(err, o.hint)
		}
		return err
	}
	var items []T
	if raw := out[field]; raw != nil {
		if err := json.Unmarshal(raw, &items); err != nil {
			return fmt.Errorf("error tracking: decode %s response: %w", route, err)
		}
	}
	if err := encodeList(cmd, c, &o.IO, items, cmdio.PagedListMeta(len(items), 0, len(items) >= facetCap, facetCap)); err != nil {
		return err
	}
	if len(items) == 0 && o.empty != nil && !o.IO.JSONDiscovery {
		o.empty(cmd, c, id)
	}
	return nil
}

func newReleasesListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &facetsOpts[release]{window: true, table: seenTable("RELEASE", func(r release) seen {
		return seen{r.Release, r.EventCount, r.FirstSeen, r.LastSeen}
	})}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the releases an Error Tracking project saw events from.",
		Long: `List the releases an Error Tracking project received events from, with
their event counts and first and last event times, as the SDK named them.

Counts are lifetime counts; --since or --from/--to keeps the releases whose
lifetime overlaps that window. The API returns at most 100, newest first, so
there is no --limit; narrow the window instead. Use it after a deploy to check
the SDK sends the release name you expect, and releases status to see what
broke in it.`,
		Example: `  # Releases seen in the last week.
  gcx error-tracking releases list --project 314 --since 7d

  # Every release, by project name.
  gcx et releases list --project checkout-api

  # As an agent: release names and first events only.
  gcx error-tracking releases list --project 314 --since 24h --json release,firstSeen`,
		Annotations: map[string]string{agent.AnnotationTokenCost: "medium", agent.AnnotationLLMHint: "--project 314 --since 7d"},
	}
	return opts.sdkFacet(cmd, loader, "release")
}

type errorsSection = itemList[errorSummary]

type releaseStatus struct {
	Release   *release      `json:"release"`
	Project   projectInfo   `json:"project"`
	New       errorsSection `json:"new"`
	Regressed errorsSection `json:"regressed"`
	LastSeen  errorsSection `json:"lastSeen"`
}

type releasesStatusOpts struct {
	IO      cmdio.Options
	Project projectRef
	Limit   int
	release string
}

func (o *releasesStatusOpts) Validate(cmd *cobra.Command, args []string) error {
	if err := o.Project.validate(cmd); err != nil {
		return err
	}
	if o.Limit < 1 || o.Limit > maxPageSize {
		return usage(cmd, fmt.Sprintf("invalid --limit %d: must be 1 to %d", o.Limit, maxPageSize))
	}
	if len(args) == 1 {
		if o.release = strings.TrimSpace(args[0]); o.release == "" {
			return usage(cmd, "[release] must not be empty")
		}
	}
	return validIO(cmd, &o.IO)
}

func (o *releasesStatusOpts) setup(flags *pflag.FlagSet) {
	o.Project.setup(flags)
	flags.IntVar(&o.Limit, "limit", 20, fmt.Sprintf("Errors to show per section, 1 to %d", maxPageSize))
	bindText(&o.IO, flags, cardText{})
}

func (c *client) section(ctx context.Context, project int64, q url.Values, limit int) (errorsSection, error) {
	q.Set("limit", strconv.Itoa(min(limit+1, maxPageSize)))
	var out struct {
		Errors     []errorSummary `json:"errors"`
		NextCursor string         `json:"nextCursor"`
	}
	if err := c.do(ctx, http.MethodGet, routePath("projects", project, "errors"), q, nil, &out); err != nil {
		return errorsSection{}, err
	}
	items := out.Errors
	if items == nil {
		items = []errorSummary{}
	}
	cut := len(items) > limit || (len(items) == limit && out.NextCursor != "")
	items = items[:min(len(items), limit)]
	return errorsSection{Items: items, ListMeta: cmdio.PagedListMeta(len(items), limit, cut, 0)}, nil
}

func writeSection(w io.Writer, title string, s errorsSection, lastSeen bool) error {
	count := strconv.Itoa(len(s.Items))
	if m := s.ListMeta; m != nil && m.Truncated {
		count = "first " + count
	}
	fmt.Fprintf(w, "\n%s (%s)\n", title, count)
	if len(s.Items) == 0 {
		return nil
	}
	keep := []cmdio.Column[errorSummary]{}
	for _, c := range errorColumns(func(e errorSummary) errorSummary { return e }) {
		if c.Visible == cmdio.WideOnly || (lastSeen && c.Header == "RELEASE") || (!lastSeen && c.Header == "STATUS") {
			continue
		}
		keep = append(keep, c)
	}
	return cleanTable(cmdio.Table[errorSummary]{Columns: keep}).Codec(cmdio.FormatTable).Encode(w, s.Items)
}

func writeRelease(w io.Writer, r releaseStatus) error {
	if r.Release == nil {
		return nil
	}
	fmt.Fprintf(w, "Release %s · %s\n", printable(r.Release.Release), projectLabel(r.Project.ID, r.Project.Name))
	fmt.Fprintf(w, "first event %s · last event %s · %d events\n", seenAt(r.Release.FirstSeen), seenAt(r.Release.LastSeen), r.Release.EventCount)
	for _, s := range []struct {
		title    string
		section  errorsSection
		lastSeen bool
	}{
		{"REGRESSED since its first event", r.Regressed, false},
		{"NEW since its first event", r.New, false},
		{"LAST SEEN IN THIS RELEASE", r.LastSeen, true},
	} {
		if err := writeSection(w, s.title, s.section, s.lastSeen); err != nil {
			return err
		}
	}
	return nil
}

const releaseLegend = "each section is its own query, so an error can show in more than one; RELEASE is the error's latest release, and since means since this release's first event, from any release; new leaves out errors someone already reviewed"

func newReleasesStatusCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &releasesStatusOpts{}
	cmd := &cobra.Command{
		Use:   "status [release]",
		Short: "Show what broke in a release: its new, regressed and latest errors.",
		Long: `Show one release (the newest by default): its first and last event, the
errors that regressed since its first event, the errors new since then, and
the errors last seen in it. Each section is its own query, so an error can
show in more than one; new leaves out errors someone reviewed, and RELEASE
shows each error's latest release because since counts events from any
release. Each section lists at most --limit errors, says "first 3" when it is
cut and points at the errors list command that shows the rest; a project
with no releases prints nothing and says so on stderr.`,
		Example: `  # The newest release of a project.
  gcx error-tracking releases status --project 314

  # One release, by project name.
  gcx et releases status v1.2.3 --project checkout-api

  # As an agent: the IDs of new and regressed errors.
  gcx error-tracking releases status v1.2.3 --project 314 -o json --jq '{new: [.new.items[].id], regressed: [.regressed.items[].id]}'`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(cmd, args); err != nil {
				return err
			}
			name := opts.release
			ctx := cmd.Context()
			c, project, err := connect(ctx, loader, &opts.Project, nil)
			if err != nil {
				return err
			}
			rel, err := c.findRelease(ctx, project, name)
			if errors.Is(err, errNoReleases) {
				st := releaseStatus{Project: projectInfo{ID: project, Name: opts.Project.name},
					New: errorsSection{Items: []errorSummary{}}, Regressed: errorsSection{Items: []errorSummary{}}, LastSeen: errorsSection{Items: []errorSummary{}}}
				return emit(cmd, &opts.IO, st, hint{capital(c.projectShown()) + " has no releases yet. " + countsLag,
					fmt.Sprintf("gcx error-tracking raw-events list --project %d", project)})
			}
			if err != nil {
				return err
			}
			st := releaseStatus{Release: rel, Project: projectInfo{ID: project, Name: opts.Project.name}}
			start, release := rel.FirstSeen, printable(rel.Release)
			quotable := !strings.ContainsAny(release, "\u2018\u2019\u201a\u201b")
			list := func(status, flag string) string {
				return et("errors", "list", "--project", strconv.FormatInt(project, 10), "--status", status, flag, release, "--limit", "0")
			}
			if st.Regressed, err = c.section(ctx, project, url.Values{"status": {"regressed"}, "regressedFrom": {start}, "sort": {"lastSeen"}}, opts.Limit); err != nil {
				return err
			}
			if st.New, err = c.section(ctx, project, url.Values{"status": {"unresolved"}, "reviewed": {"false"}, "firstSeenFrom": {start}, "sort": {"lastSeen"}}, opts.Limit); err != nil {
				return err
			}
			if st.LastSeen, err = c.section(ctx, project, url.Values{"status": {"all"}, "release": {rel.Release}, "sort": {"lastSeen"}}, opts.Limit); err != nil {
				return err
			}
			var hints []hint
			if opts.IO.OutputFormat == "text" && len(st.Regressed.Items)+len(st.New.Items)+len(st.LastSeen.Items) > 0 {
				hints = append(hints, hint{summary: releaseLegend})
			}
			for _, s := range []struct {
				name    string
				section errorsSection
				command string
			}{
				{"regressed", st.Regressed, list("regressed", "--since-release")},
				{"new", st.New, list("new", "--since-release")},
				{"last-seen", st.LastSeen, list("all", "--release")},
			} {
				if s.section.ListMeta != nil && quotable {
					hints = append(hints, hint{fmt.Sprintf("the %s section was cut at --limit; see the rest", s.name), s.command})
				}
			}
			return emit(cmd, &opts.IO, st, hints...)
		},
		Annotations: map[string]string{agent.AnnotationTokenCost: "medium", agent.AnnotationLLMHint: "[release] --project 314 --limit 5 -o json"},
	}
	opts.setup(cmd.Flags())
	return cmd
}
