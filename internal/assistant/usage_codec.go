package assistant

import (
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/style"
	"github.com/grafana/gcx/internal/terminal"
)

// UsageTextCodec renders monthly counters as a terminal usage overview.
type UsageTextCodec struct {
	// Top limits only the human attention list; nil or zero shows all at-risk users.
	Top *int
}

func (c *UsageTextCodec) Format() format.Format { return "text" }

func (c *UsageTextCodec) Encode(dst io.Writer, value any) error {
	usage, ok := value.(Usage)
	if !ok {
		return fmt.Errorf("expected Usage, got %T", value)
	}
	var out strings.Builder
	out.WriteString(usageHeading("GRAFANA ASSISTANT") + "\n")
	period := usage.Tokens.Month
	if month, err := time.Parse("2006-01", period); err == nil {
		period = month.Format("January 2006") + "  /  resets " + month.AddDate(0, 1, 0).Format("Jan 2, 15:04") + " UTC"
	}
	fmt.Fprintln(&out, style.ColorMutedText("Monthly usage  /  "+period))
	fmt.Fprintln(&out, "\n"+usageHeading("YOUR USAGE"))
	rows := usageSummaryRows(usage)
	summary := style.NewTable("METRIC", "USED / LIMIT", "UTILIZATION", "LEFT").Compact()
	for _, row := range rows {
		if row.scope == "user" {
			addUsageSummary(summary, row.name, row.total, row.limit)
		}
	}

	if err := summary.Render(&out); err != nil {
		return err
	}

	if usage.Admin != nil {
		if err := c.writeAdmin(&out, usage.Admin, rows); err != nil {
			return err
		}
	} else {
		for _, row := range rows {
			if row.scope != "user" && row.limit > 0 && row.total >= row.limit {
				fmt.Fprintf(&out, "\nShared %s limit reached. Contact your administrator.\n", strings.TrimSuffix(strings.ToLower(row.name), "s"))
			}
		}
	}
	_, err := io.WriteString(dst, out.String())
	return err
}

func usageHeading(text string) string {
	if !style.IsStylingEnabled() {
		return text
	}
	return lipgloss.NewStyle().Bold(true).Foreground(style.ColorPrimary).Render(text)
}

func addUsageSummary(table *style.TableBuilder, name string, total, limit int64) {
	values := compactUsageNumber(total) + " / " + compactUsageNumber(limit)
	left := compactUsageNumber(limit - total)
	bar := usageBar(total, limit, 12)
	if limit == 0 {
		values = compactUsageNumber(total)
		bar = style.ColorMutedText("No cap reported")
		left = "-"
	} else if total >= limit {
		left = "Reached"
	}
	table.Row(name, values, bar, style.ColorMutedText(left))
}

// compactUsageNumber is a display approximation only; JSON retains exact values.
func compactUsageNumber(value int64) string {
	if value < 10_000 {
		raw := strconv.FormatInt(value, 10)
		if value >= 1000 {
			return raw[:len(raw)-3] + "," + raw[len(raw)-3:]
		}
		return raw
	}
	units := []string{"", "K", "M", "B", "T", "P", "E"}
	scaled, unit := float64(value), 0
	for scaled >= 999.95 && unit < len(units)-1 {
		scaled /= 1000
		unit++
	}
	return strings.TrimSuffix(strconv.FormatFloat(scaled, 'f', 1, 64), ".0") + units[unit]
}

func usageBar(total, limit int64, width int) string {
	if limit == 0 {
		return style.ColorMutedText("No cap")
	}
	ratio := float64(total) / float64(limit)
	fraction := min(1, max(0, ratio))
	percent := fmt.Sprintf("%5.1f%%", ratio*100)
	if !style.IsStylingEnabled() {
		filled := int(math.Round(fraction * float64(width)))
		return fmt.Sprintf("[%s%s] %s", strings.Repeat("#", filled), strings.Repeat("-", width-filled), percent)
	}
	// Eighth-cell resolution keeps small but nonzero usage visible.
	steps := int(math.Round(fraction * float64(width) * 8))
	if total > 0 {
		steps = max(1, steps)
	}
	full, partial := steps/8, steps%8
	fill := strings.Repeat("█", full)
	if partial > 0 {
		fill += string([]rune(" ▏▎▍▌▋▊▉")[partial])
		full++
	}
	color := style.ColorPrimary
	if ratio >= 1 {
		color = style.GradientAccentFrom
	} else if ratio >= .8 {
		color = style.GradientBrandTo
	}
	paint := lipgloss.NewStyle().Foreground(color)
	return paint.Render(fill) + style.ColorMutedText(strings.Repeat("░", width-full)) + " " + paint.Render(percent)
}

func (c *UsageTextCodec) writeAdmin(out *strings.Builder, admin *AdminUsage, rows []usageSummaryRow) error {
	fmt.Fprintln(out, "\n"+usageHeading("ORGANIZATION"))
	summary := style.NewTable("METRIC", "USED / LIMIT", "UTILIZATION", "LEFT").Compact()
	for _, row := range rows {
		if row.scope == "user" {
			continue
		}
		name := row.name
		if row.scope == "service_account" {
			name = "Service-account " + strings.ToLower(name)
		}
		addUsageSummary(summary, name, row.total, row.limit)
	}
	if err := summary.Render(out); err != nil {
		return err
	}
	if admin.TeamDataPartial {
		fmt.Fprintln(out, style.ColorCell("Warning: team data is incomplete; team-derived limits may be incomplete.", false, true))
	}
	members := admin.UsersNeedingAttention()
	count := len(members)
	if c.Top != nil && *c.Top > 0 {
		count = min(count, *c.Top)
	}
	if count == 0 {
		fmt.Fprintln(out, "\nNo users at or above 80% of their reported token limit.")
		return nil
	}
	fmt.Fprintln(out, "\n"+usageHeading("USERS NEAR THEIR LIMIT"))
	fmt.Fprintln(out, style.ColorMutedText(fmt.Sprintf("At least 80%% used · showing %d of %d", count, len(members))))
	table := style.NewTable("USER", "USED / LIMIT", "UTILIZATION", "STATUS").Compact()
	barWidth := 12
	if width := terminal.StdoutWidth(); width > 0 && width < 100 {
		barWidth = 8
	}
	for _, member := range members[:count] {
		name := member.Email
		if name == "" {
			name = member.ID
		}
		limit := member.EffectiveLimitMtok * tokensPerMillion
		values := compactUsageNumber(member.UsedThisMonthTokens) + " / " + compactUsageNumber(limit)
		status := "Near limit"
		if member.UsedThisMonthTokens >= limit {
			status = "Limit reached"
		}
		table.Row(name, values, usageBar(member.UsedThisMonthTokens, limit, barWidth), status)
	}
	if err := table.Render(out); err != nil {
		return err
	}
	fmt.Fprintln(out, "\nReview per-user limits in Grafana Assistant > Usage.")
	if count < len(members) {
		fmt.Fprintln(out, style.ColorMutedText("Show all users needing attention: gcx assistant usage get --top 0"))
	}
	return nil
}

func (c *UsageTextCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("decode not supported for text format")
}
