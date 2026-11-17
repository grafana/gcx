package alert

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/dustin/go-humanize"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/style"
	"github.com/grafana/gcx/internal/terminal"
	"sigs.k8s.io/yaml"
)

const alertmanagerImportTableCellWidth = 40

var alertmanagerTemplateDefinePattern = regexp.MustCompile(`{{-?\s*define\s+"([^"]+)"\s*-?}}`)

type alertmanagerImportTableCodec struct {
	identifier string
	wide       bool
}

func (c *alertmanagerImportTableCodec) Format() format.Format {
	if c.wide {
		return "wide"
	}
	return "table"
}

func (c *alertmanagerImportTableCodec) Decode(io.Reader, any) error {
	return errors.New("alertmanager import table format does not support decoding")
}

func (c *alertmanagerImportTableCodec) Encode(w io.Writer, v any) error {
	userConfig, ok := v.(*AlertmanagerUserConfig)
	if !ok {
		return fmt.Errorf("alertmanager import table: unsupported type %T", v)
	}

	fmt.Fprintf(w, "Staged Alertmanager configuration %q (read-only until promoted; original names, before renames)\n\n", c.identifier)

	var config alertmanagerTableConfig
	if err := yaml.Unmarshal([]byte(userConfig.AlertmanagerConfig), &config); err != nil {
		fmt.Fprintln(w, "The configuration could not be parsed. Use -o yaml to inspect it.")
		fmt.Fprintln(w)
		if err := c.renderTemplates(w, userConfig.TemplateFiles); err != nil {
			return err
		}
		fmt.Fprintln(w)
		return writeAlertmanagerImportFooter(w)
	}

	sections := []func(io.Writer) error{
		func(w io.Writer) error { return c.renderRoutes(w, config.Route) },
		func(w io.Writer) error { return renderAlertmanagerReceivers(w, config.Receivers) },
		func(w io.Writer) error {
			intervals := append(slices.Clone(config.MuteTimeIntervals), config.TimeIntervals...)
			return renderAlertmanagerTimeIntervals(w, intervals)
		},
		func(w io.Writer) error { return renderAlertmanagerInhibitionRules(w, config.InhibitRules) },
		func(w io.Writer) error { return c.renderTemplates(w, userConfig.TemplateFiles) },
	}
	for _, render := range sections {
		if err := render(w); err != nil {
			return err
		}
		fmt.Fprintln(w)
	}
	return writeAlertmanagerImportFooter(w)
}

func writeAlertmanagerImportFooter(w io.Writer) error {
	_, err := fmt.Fprintln(w, "Secrets are masked and global settings are dropped: use -o yaml to inspect the configuration, not to import it again.")
	return err
}

type alertmanagerTableConfig struct {
	Route             *alertmanagerTableRoute        `json:"route"`
	Receivers         []map[string]any               `json:"receivers"`
	TimeIntervals     []MuteTiming                   `json:"time_intervals"`
	MuteTimeIntervals []MuteTiming                   `json:"mute_time_intervals"`
	InhibitRules      []alertmanagerTableInhibitRule `json:"inhibit_rules"`
}

type alertmanagerTableRoute struct {
	Receiver          string                   `json:"receiver"`
	GroupBy           []string                 `json:"group_by"`
	GroupWait         string                   `json:"group_wait"`
	GroupInterval     string                   `json:"group_interval"`
	RepeatInterval    string                   `json:"repeat_interval"`
	Matchers          []string                 `json:"matchers"`
	Match             map[string]string        `json:"match"`
	MatchRE           map[string]string        `json:"match_re"`
	Continue          bool                     `json:"continue"`
	MuteTimeIntervals []string                 `json:"mute_time_intervals"`
	ActiveIntervals   []string                 `json:"active_time_intervals"`
	Routes            []alertmanagerTableRoute `json:"routes"`
}

type alertmanagerTableInhibitRule struct {
	SourceMatchers []string          `json:"source_matchers"`
	SourceMatch    map[string]string `json:"source_match"`
	SourceMatchRE  map[string]string `json:"source_match_re"`
	TargetMatchers []string          `json:"target_matchers"`
	TargetMatch    map[string]string `json:"target_match"`
	TargetMatchRE  map[string]string `json:"target_match_re"`
	Equal          []string          `json:"equal"`
}

type alertmanagerRouteRow struct {
	prefix string
	route  *alertmanagerTableRoute
	root   bool
}

func (c *alertmanagerImportTableCodec) renderRoutes(w io.Writer, root *alertmanagerTableRoute) error {
	rows := alertmanagerRouteRows(root)
	headers := []string{"MATCHERS", "RECEIVER", "GROUP BY", "CONTINUE", "INTERVALS"}
	if c.wide {
		headers = append(headers, "GROUP_WAIT", "GROUP_INTERVAL", "REPEAT_INTERVAL")
	}
	table := style.NewTable(headers...)
	for _, row := range rows {
		route := row.route
		groupBy := "(inherited)"
		if row.root {
			groupBy = "-"
		}
		if len(route.GroupBy) > 0 {
			groupBy = strings.Join(route.GroupBy, ",")
		}
		continueValue := "-"
		if !row.root {
			continueValue = strconv.FormatBool(route.Continue)
		}
		matchers := alertmanagerMatchers(route.Matchers, route.Match, route.MatchRE)
		if row.root {
			matchers = "(root)"
		} else if !c.wide && !terminal.NoTruncate() {
			matchers = truncateAlertmanagerCell(matchers, alertmanagerImportTableCellWidth)
		}
		cells := []string{
			row.prefix + matchers,
			alertmanagerDash(route.Receiver),
			groupBy,
			continueValue,
			alertmanagerRouteIntervals(route.MuteTimeIntervals, route.ActiveIntervals),
		}
		if c.wide {
			cells = append(cells,
				alertmanagerDash(route.GroupWait),
				alertmanagerDash(route.GroupInterval),
				alertmanagerDash(route.RepeatInterval),
			)
		}
		table.Row(cells...)
	}
	return renderAlertmanagerSection(w, "Routes", len(rows), table)
}

func alertmanagerRouteRows(root *alertmanagerTableRoute) []alertmanagerRouteRow {
	if root == nil {
		return nil
	}
	rows := []alertmanagerRouteRow{{route: root, root: true}}
	var walk func([]alertmanagerTableRoute, string)
	walk = func(routes []alertmanagerTableRoute, prefix string) {
		for i := range routes {
			last := i == len(routes)-1
			branch := "├─ "
			nextPrefix := prefix + "│  "
			if last {
				branch = "└─ "
				nextPrefix = prefix + "   "
			}
			rows = append(rows, alertmanagerRouteRow{prefix: prefix + branch, route: &routes[i]})
			walk(routes[i].Routes, nextPrefix)
		}
	}
	walk(root.Routes, "")
	return rows
}

func alertmanagerMatchers(matchers []string, match, matchRE map[string]string) string {
	parts := slices.Clone(matchers)
	for _, name := range slices.Sorted(maps.Keys(match)) {
		parts = append(parts, name+"="+strconv.Quote(match[name]))
	}
	for _, name := range slices.Sorted(maps.Keys(matchRE)) {
		parts = append(parts, name+"=~"+strconv.Quote(matchRE[name]))
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ", ")
}

func alertmanagerRouteIntervals(mute, active []string) string {
	parts := make([]string, 0, 2)
	if len(mute) > 0 {
		parts = append(parts, "mute: "+strings.Join(mute, ","))
	}
	if len(active) > 0 {
		parts = append(parts, "active: "+strings.Join(active, ","))
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, "; ")
}

func renderAlertmanagerReceivers(w io.Writer, receivers []map[string]any) error {
	table := style.NewTable("NAME", "INTEGRATIONS")
	for _, receiver := range receivers {
		name, _ := receiver["name"].(string)
		integrations := make([]string, 0)
		for key, value := range receiver {
			if !strings.HasSuffix(key, "_configs") {
				continue
			}
			items := reflect.ValueOf(value)
			if items.Kind() != reflect.Slice || items.Len() == 0 {
				continue
			}
			integration := strings.TrimSuffix(key, "_configs")
			if items.Len() > 1 {
				integration += "×" + strconv.Itoa(items.Len())
			}
			integrations = append(integrations, integration)
		}
		slices.Sort(integrations)
		if len(integrations) == 0 {
			integrations = append(integrations, "none")
		}
		table.Row(alertmanagerDash(name), strings.Join(integrations, ", "))
	}
	return renderAlertmanagerSection(w, "Receivers", len(receivers), table)
}

func renderAlertmanagerTimeIntervals(w io.Writer, intervals []MuteTiming) error {
	table := style.NewTable("NAME", "INTERVALS", "SUMMARY")
	for _, interval := range intervals {
		summary := summarizeIntervals(interval.TimeIntervals)
		if len(interval.TimeIntervals) > 0 && interval.TimeIntervals[0].Location != "" {
			location := "(" + interval.TimeIntervals[0].Location + ")"
			if summary == "" {
				summary = location
			} else {
				summary += " " + location
			}
		}
		table.Row(alertmanagerDash(interval.Name), strconv.Itoa(len(interval.TimeIntervals)), alertmanagerDash(summary))
	}
	return renderAlertmanagerSection(w, "Time intervals", len(intervals), table)
}

func renderAlertmanagerInhibitionRules(w io.Writer, rules []alertmanagerTableInhibitRule) error {
	table := style.NewTable("#", "SOURCE MATCHERS", "TARGET MATCHERS", "EQUAL")
	for i, rule := range rules {
		table.Row(
			strconv.Itoa(i+1),
			alertmanagerMatchers(rule.SourceMatchers, rule.SourceMatch, rule.SourceMatchRE),
			alertmanagerMatchers(rule.TargetMatchers, rule.TargetMatch, rule.TargetMatchRE),
			alertmanagerDash(strings.Join(rule.Equal, ",")),
		)
	}
	return renderAlertmanagerSection(w, "Inhibition rules", len(rules), table)
}

func (c *alertmanagerImportTableCodec) renderTemplates(w io.Writer, templates map[string]string) error {
	table := style.NewTable("NAME", "DEFINES", "SIZE")
	for _, name := range slices.Sorted(maps.Keys(templates)) {
		content := templates[name]
		defines := alertmanagerTemplateDefines(content)
		if !c.wide && !terminal.NoTruncate() {
			defines = truncateAlertmanagerCell(defines, alertmanagerImportTableCellWidth)
		}
		table.Row(name, alertmanagerDash(defines), strings.ToUpper(humanize.Bytes(uint64(len(content)))))
	}
	return renderAlertmanagerSection(w, "Templates", len(templates), table)
}

func alertmanagerTemplateDefines(content string) string {
	matches := alertmanagerTemplateDefinePattern.FindAllStringSubmatch(content, -1)
	defines := make([]string, 0, len(matches))
	for _, match := range matches {
		defines = append(defines, match[1])
	}
	return strings.Join(defines, ", ")
}

func renderAlertmanagerSection(w io.Writer, title string, count int, table *style.TableBuilder) error {
	fmt.Fprintf(w, "%s (%d):\n", title, count)
	if count == 0 {
		return nil
	}
	return table.Render(w)
}

func truncateAlertmanagerCell(value string, maxLen int) string {
	runes := []rune(value)
	if len(runes) <= maxLen || maxLen < 3 {
		return value
	}
	return string(runes[:maxLen-3]) + "..."
}

func alertmanagerDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
