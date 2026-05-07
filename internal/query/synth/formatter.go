package synth

import (
	"io"
	"strconv"
	"time"

	"github.com/grafana/gcx/internal/providers/synth/checks"
	"github.com/grafana/gcx/internal/providers/synth/probes"
	"github.com/grafana/gcx/internal/style"
)

// FormatProbesTable renders a list of probes as a table.
func FormatProbesTable(w io.Writer, ps []probes.Probe) error {
	t := style.NewTable("ID", "NAME", "REGION", "VISIBILITY", "ONLINE", "VERSION")
	for _, p := range ps {
		visibility := "private"
		if p.Public {
			visibility = "public"
		}
		t.Row(
			strconv.FormatInt(p.ID, 10),
			p.Name,
			p.Region,
			visibility,
			strconv.FormatBool(p.Online),
			p.Version,
		)
	}
	return t.Render(w)
}

// FormatChecksTable renders a list of checks as a table.
func FormatChecksTable(w io.Writer, cs []checks.Check) error {
	t := style.NewTable("ID", "JOB", "TARGET", "TYPE", "FREQUENCY", "ENABLED", "PROBES")
	for _, c := range cs {
		t.Row(
			strconv.FormatInt(c.ID, 10),
			c.Job,
			c.Target,
			c.Settings.CheckType(),
			(time.Duration(c.Frequency) * time.Millisecond).String(),
			strconv.FormatBool(c.Enabled),
			strconv.Itoa(len(c.Probes)),
		)
	}
	return t.Render(w)
}

// FormatChecksWideTable renders checks with extra columns (timeout, alerts count).
func FormatChecksWideTable(w io.Writer, cs []checks.Check) error {
	t := style.NewTable("ID", "JOB", "TARGET", "TYPE", "FREQUENCY", "TIMEOUT", "ENABLED", "PROBES", "ALERTS")
	for _, c := range cs {
		t.Row(
			strconv.FormatInt(c.ID, 10),
			c.Job,
			c.Target,
			c.Settings.CheckType(),
			(time.Duration(c.Frequency) * time.Millisecond).String(),
			(time.Duration(c.Timeout) * time.Millisecond).String(),
			strconv.FormatBool(c.Enabled),
			strconv.Itoa(len(c.Probes)),
			strconv.Itoa(len(c.Alerts)),
		)
	}
	return t.Render(w)
}
