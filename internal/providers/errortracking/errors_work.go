package errortracking

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

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
