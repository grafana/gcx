package errortracking

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"unicode/utf8"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/terminal"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type errorCard struct {
	errorDetail

	RecentActivity []activityEntry `json:"recentActivity"`
	Assignees      *[]assignee     `json:"assignees,omitempty"`

	assigneesNote string
}

type stackFrame struct {
	Filename string `json:"filename"`
	AbsPath  string `json:"abs_path"`
	Function string `json:"function"`
	Module   string `json:"module"`
	Lineno   int    `json:"lineno"`
	Colno    int    `json:"colno"`
	InApp    *bool  `json:"in_app"`
}

type sentryPayload struct {
	Exception struct {
		Values []struct {
			Type       string `json:"type"`
			Value      string `json:"value"`
			Stacktrace *struct {
				Frames []stackFrame `json:"frames"`
			} `json:"stacktrace"`
		} `json:"values"`
	} `json:"exception"`
}

func frames(payload json.RawMessage) []stackFrame {
	var p sentryPayload
	if json.Unmarshal(payload, &p) != nil {
		return nil
	}
	for _, v := range slices.Backward(p.Exception.Values) {
		if st := v.Stacktrace; st != nil && len(st.Frames) > 0 {
			out := slices.Clone(st.Frames)
			slices.Reverse(out)
			return out
		}
	}
	return nil
}

func (f stackFrame) where() string {
	file := cmdio.OrDash(cmp.Or(f.Filename, f.AbsPath, f.Module))
	switch {
	case f.Lineno > 0 && f.Colno > 0:
		return fmt.Sprintf("%s:%d:%d", file, f.Lineno, f.Colno)
	case f.Lineno > 0:
		return fmt.Sprintf("%s:%d", file, f.Lineno)
	}
	return file
}

func shortEventID(id string) string {
	if r := []rune(id); len(r) > 16 {
		return string(r[:8]) + "…" + string(r[len(r)-5:])
	}
	return id
}

func seenAt(ts string) string {
	if _, ok := parseTime(ts); !ok {
		return cmdio.OrDash(printable(ts))
	}
	return minute(ts) + " (" + age(ts) + ")"
}

func writeEvent(w io.Writer, e eventDetail, title string) {
	parts := []string{title + " " + shortEventID(printable(e.EventID)), stamp(e.OccurredAt)}
	for _, s := range []string{e.Environment, e.Release} {
		if s = printable(s); s != "" {
			parts = append(parts, s)
		}
	}
	fmt.Fprintln(w, strings.Join(parts, " · "))
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fs := frames(e.Payload)
	if len(fs) == 0 {
		fmt.Fprintf(w, "  %-11s  %s\n", "frames", "none in the payload")
	} else {
		fmt.Fprintln(w, "  frames (most recent call first)")
	}
	for _, f := range fs[:min(10, len(fs))] {
		inApp := ""
		if f.InApp != nil && *f.InApp {
			inApp = "in app"
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", printable(f.where()), printable(cmdio.OrDash(f.Function)), inApp)
	}
	if len(fs) > 10 {
		fmt.Fprintf(tw, "  …\t%d more frames in the payload\t\n", len(fs)-10)
	}
	_ = tw.Flush()
	for _, kv := range [][2]string{{"level", e.Level}, {"transaction", e.Transaction}, {"server", e.ServerName}, {"trace", e.TraceID}} {
		if v := printable(kv[1]); v != "" {
			fmt.Fprintf(w, "  %-11s  %s\n", kv[0], v)
		}
	}
	if len(e.Tags) > 0 {
		keys := slices.Sorted(maps.Keys(e.Tags))
		tags := make([]string, len(keys))
		for i, k := range keys {
			tags[i] = printable(k + "=" + e.Tags[k])
		}
		fmt.Fprintf(w, "  %-11s  %s\n", "tags", strings.Join(tags, "  "))
	}
}

const messageLines = 8

func cardWidth() int {
	if width := terminal.StdoutWidth(); width >= 40 {
		return width
	}
	return 100
}

func wrapLine(line string, width int) []string {
	runes := []rune(line)
	if len(runes) == 0 {
		return []string{""}
	}
	var out []string
	start := 0
	for len(runes)-start > width {
		seg := runes[start:]
		cut := width
		for i := width; i > width/2; i-- {
			if seg[i] == ' ' {
				cut = i
				break
			}
		}
		out = append(out, strings.TrimRight(string(seg[:cut]), " "))
		start += cut
		for start < len(runes) && runes[start] == ' ' {
			start++
		}
	}
	return append(out, string(runes[start:]))
}

func writeMessage(w io.Writer, text, fields string, chars int) {
	width := cardWidth() - 2
	var lines []string
	for i, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = printable(strings.ReplaceAll(strings.TrimRight(line, " \t"), "\t", "    "))
		for j, l := range wrapLine(line, width) {
			if i > 0 || j > 0 {
				l = "  " + l
			}
			lines = append(lines, l)
		}
	}
	if len(lines) > messageLines {
		lines = append(lines[:messageLines], fmt.Sprintf("  … (%d characters; all of it: --json %s)", chars, fields))
	}
	for _, l := range lines {
		fmt.Fprintln(w, l)
	}
}

func writeError(w io.Writer, c errorCard) {
	head := []string{fmt.Sprintf("Error %d", c.ID), printable(c.shownStatus()), projectLabel(c.ProjectID, c.ProjectName)}
	fmt.Fprintln(w, strings.Join(head, " · "))
	kind, msg := strings.TrimSpace(c.ExceptionType), strings.TrimSpace(c.Message)
	title, fields := printable(kind), "exceptionType"
	switch {
	case kind == "":
		title, fields = msg, "message"
	case msg != "":
		title, fields = title+": "+msg, "exceptionType,message"
	}
	writeMessage(w, cmdio.OrDash(title), fields, utf8.RuneCountInString(c.ExceptionType)+utf8.RuneCountInString(c.Message))
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	row := func(k, v string) { fmt.Fprintf(tw, "  %s\t%s\n", k, printable(cmdio.OrDash(v))) }
	row("culprit", c.Culprit)
	row("transaction", c.Transaction)
	row("events", fmt.Sprintf("%d · first seen %s · last seen %s", c.EventCount, seenAt(c.FirstSeen), seenAt(c.LastSeen)))
	row("release", c.LastRelease)
	reviewed := "no"
	if c.Reviewed {
		reviewed = "yes"
	}
	row("reviewed", reviewed)
	if c.Status == "ignored" {
		row("ignored", "until "+c.ignoredUntil())
	}
	switch {
	case c.Assignees == nil:
		row("assignees", c.assigneesNote)
	case len(*c.Assignees) == 0:
		row("assignees", "nobody")
	default:
		names := make([]string, len(*c.Assignees))
		for i, a := range *c.Assignees {
			names[i] = firstOf(a.Name, a.Login, a.UserUID)
		}
		row("assignees", strings.Join(names, ", "))
	}
	row("comments", strconv.FormatInt(c.CommentCount, 10))
	_ = tw.Flush()
	if c.LatestEvent != nil {
		fmt.Fprintln(w)
		writeEvent(w, *c.LatestEvent, "Latest event")
	}
	fmt.Fprintln(w)
	if len(c.RecentActivity) == 0 {
		fmt.Fprintln(w, "Activity: none")
		return
	}
	fmt.Fprintf(w, "Activity (latest %d)\n", len(c.RecentActivity))
	tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, a := range c.RecentActivity {
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", minute(a.CreatedAt), printable(firstOf(a.ActorName, a.Actor, "-")), printable(a.what()))
	}
	_ = tw.Flush()
}

func firstOf(values ...string) string {
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

type cardText struct{ mutationText }

func (cardText) Encode(w io.Writer, v any) error {
	switch c := v.(type) {
	case errorCard:
		writeError(w, c)
	case releaseStatus:
		return writeRelease(w, c)
	default:
		return fmt.Errorf("invalid data type for text codec: expected a card, got %T", v)
	}
	return nil
}

type errorsInspectOpts struct{ errorTarget }

func (o *errorsInspectOpts) setup(flags *pflag.FlagSet) {
	o.Project.setup(flags)
	bindText(&o.IO, flags, cardText{})
}

func newErrorsInspectCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &errorsInspectOpts{}
	cmd := &cobra.Command{
		Use:   "inspect <error>",
		Short: "Show one error with its stack trace, recent activity and assignees.",
		Long: `Show one error (its ID from errors list) as a card: its status, counts and
release, the frames of its latest event, its last three activity entries and
who is assigned. It reads errors get, errors activity list --limit 3 and errors
assignees list in one go; JSON is the errors get object plus recentActivity and
assignees.

Assignees need a signed-in Grafana user: with a service account token the
card says so instead of failing, and JSON leaves assignees out.`,
		Example: `  # Look at an error.
  gcx error-tracking errors inspect 42 --project 314

  # By project name, as YAML.
  gcx et errors inspect 42 --project checkout-api -o yaml

  # As an agent: the essentials, without the large payload.
  gcx error-tracking errors inspect 42 --project 314 --json id,status,message,culprit,eventCount,lastSeen,latestEvent.eventId,recentActivity`,
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
			project := opts.Project.id
			var card errorCard
			if err := c.do(ctx, http.MethodGet, path, nil, nil, &card.errorDetail); err != nil {
				return err
			}
			c.knowProject(project, card.ProjectName)
			var activity struct {
				Activity []activityEntry `json:"activity"`
			}
			if err := c.do(ctx, http.MethodGet, path+"/activity", url.Values{"limit": {"3"}}, nil, &activity); err != nil {
				return err
			}
			card.RecentActivity = activity.Activity
			if card.RecentActivity == nil {
				card.RecentActivity = []activityEntry{}
			}
			card.assigneesNote = "not shown: needs a user login with permission to read assignees (gcx login)"
			assigneesHint := hint{"assignees are not in the output: reading them needs a user login with permission to read assignees", "gcx login"}
			if !c.robot {
				card.assigneesNote = "not shown: your role cannot read assignees"
				assigneesHint = hint{summary: "assignees are not in the output: your role cannot read assignees"}
				var out struct {
					Assignees []assignee `json:"assignees"`
				}
				err := c.do(ctx, http.MethodGet, path+"/assignees", nil, nil, &out)
				status, forbidden := errors.AsType[*gcxerrors.HTTPStatusError](err)
				switch {
				case err == nil:
					list := out.Assignees
					if list == nil {
						list = []assignee{}
					}
					card.Assignees = &list
				case !forbidden || status.Status != http.StatusForbidden:
					return err
				}
			}
			if card.Assignees == nil && opts.IO.OutputFormat != "text" && !opts.IO.JSONDiscovery {
				return emit(cmd, &opts.IO, card, assigneesHint)
			}
			return emit(cmd, &opts.IO, card)
		},
		Annotations: map[string]string{agent.AnnotationTokenCost: "large", agent.AnnotationLLMHint: "<error> --project 314 --json id,status,message,culprit,eventCount,lastSeen,latestEvent.eventId"},
	}
	opts.setup(cmd.Flags())
	return cmd
}
