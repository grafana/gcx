package loki

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-logfmt/logfmt"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/style"
)

type rawQueryCodec struct{}

// NewRawQueryCodec returns a codec that prints the original log line bodies
// without adding timestamps, labels, or structured parsing.
func NewRawQueryCodec() format.Codec { //nolint:ireturn
	return &rawQueryCodec{}
}

func (c *rawQueryCodec) Format() format.Format {
	return "raw"
}

func (c *rawQueryCodec) Encode(w io.Writer, data any) error {
	switch resp := data.(type) {
	case *QueryResponse:
		return FormatQueryRaw(w, resp)
	case QueryResponse:
		return FormatQueryRaw(w, &resp)
	default:
		return errors.New("invalid data type for raw log query codec")
	}
}

func (c *rawQueryCodec) Decode(io.Reader, any) error {
	return errors.New("raw log query codec does not support decoding")
}

type displayLogEntry struct {
	Timestamp string
	Level     string
	Source    string
	Message   string
	Details   string
	Stream    map[string]string
}

func FormatQueryTable(w io.Writer, resp *QueryResponse) error {
	entries := buildDisplayEntries(resp)
	if len(entries) == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}

	hasLevel := anyEntry(entries, func(e displayLogEntry) string { return e.Level })
	hasSource := anyEntry(entries, func(e displayLogEntry) string { return e.Source })
	hasDetails := anyEntry(entries, func(e displayLogEntry) string { return e.Details })
	hasStream := hasMultipleVisibleStreams(resp.Data.Result)

	header := []string{"TIME"}
	if hasLevel {
		header = append(header, "LEVEL")
	}
	if hasSource {
		header = append(header, "SOURCE")
	}
	if hasStream {
		header = append(header, "STREAM")
	}
	header = append(header, "MESSAGE")
	if hasDetails {
		header = append(header, "DETAILS")
	}

	t := style.NewTable(header...)
	for _, entry := range entries {
		row := []string{entry.Timestamp}
		if hasLevel {
			row = append(row, entry.Level)
		}
		if hasSource {
			row = append(row, entry.Source)
		}
		if hasStream {
			row = append(row, formatVisibleLabels(entry.Stream))
		}
		row = append(row, entry.Message)
		if hasDetails {
			row = append(row, entry.Details)
		}
		t.Row(row...)
	}

	return t.Render(w)
}

func FormatQueryTableWide(w io.Writer, resp *QueryResponse) error {
	entries := buildDisplayEntries(resp)
	if len(entries) == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}

	labelNames := collectStreamLabelNames(resp.Data.Result)
	hasLevel := anyEntry(entries, func(e displayLogEntry) string { return e.Level })
	hasSource := anyEntry(entries, func(e displayLogEntry) string { return e.Source })
	hasDetails := anyEntry(entries, func(e displayLogEntry) string { return e.Details })

	header := []string{"TIME"}
	if hasLevel {
		header = append(header, "LEVEL")
	}
	if hasSource {
		header = append(header, "SOURCE")
	}
	for _, name := range labelNames {
		header = append(header, strings.ToUpper(name))
	}
	header = append(header, "MESSAGE")
	if hasDetails {
		header = append(header, "DETAILS")
	}

	t := style.NewTable(header...)
	for _, entry := range entries {
		row := []string{entry.Timestamp}
		if hasLevel {
			row = append(row, entry.Level)
		}
		if hasSource {
			row = append(row, entry.Source)
		}
		for _, name := range labelNames {
			row = append(row, entry.Stream[name])
		}
		row = append(row, entry.Message)
		if hasDetails {
			row = append(row, entry.Details)
		}
		t.Row(row...)
	}

	return t.Render(w)
}

// FormatQueryRaw prints only the original log line bodies.
func FormatQueryRaw(w io.Writer, resp *QueryResponse) error {
	if resp == nil || len(resp.Data.Result) == 0 {
		return nil
	}

	for _, stream := range resp.Data.Result {
		for _, value := range stream.Values {
			if len(value) < 2 {
				continue
			}
			fmt.Fprintln(w, value[1])
		}
	}

	return nil
}

func FormatLabelsTable(w io.Writer, resp *LabelsResponse) error {
	t := style.NewTable("LABEL")
	for _, label := range resp.Data {
		t.Row(label)
	}
	return t.Render(w)
}

func FormatSeriesTable(w io.Writer, resp *SeriesResponse) error {
	if len(resp.Data) == 0 {
		fmt.Fprintln(w, "No series found")
		return nil
	}

	labelNames := collectLabelNames(resp.Data)

	header := make([]string, 0, len(labelNames))
	for _, name := range labelNames {
		header = append(header, strings.ToUpper(name))
	}
	t := style.NewTable(header...)

	for _, series := range resp.Data {
		row := make([]string, 0, len(labelNames))
		for _, name := range labelNames {
			if val, ok := series[name]; ok {
				row = append(row, val)
			} else {
				row = append(row, "")
			}
		}
		t.Row(row...)
	}

	return t.Render(w)
}

// FormatMetricQueryTable formats a MetricQueryResponse as a table with TIMESTAMP, VALUE, and label columns.
func FormatMetricQueryTable(w io.Writer, resp *MetricQueryResponse) error {
	if len(resp.Data.Result) == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}

	labelNames := collectMetricLabelNames(resp.Data.Result)

	header := make([]string, 0, len(labelNames)+2)
	header = append(header, "TIMESTAMP", "VALUE")
	for _, name := range labelNames {
		header = append(header, strings.ToUpper(name))
	}
	t := style.NewTable(header...)

	for _, sample := range resp.Data.Result {
		if len(sample.Values) > 0 {
			for _, v := range sample.Values {
				if len(v) < 2 {
					continue
				}
				row := make([]string, 0, len(labelNames)+2)
				row = append(row, fmt.Sprintf("%v", v[0]), fmt.Sprintf("%v", v[1]))
				for _, name := range labelNames {
					row = append(row, sample.Metric[name])
				}
				t.Row(row...)
			}
		} else if len(sample.Value) >= 2 {
			row := make([]string, 0, len(labelNames)+2)
			row = append(row, fmt.Sprintf("%v", sample.Value[0]), fmt.Sprintf("%v", sample.Value[1]))
			for _, name := range labelNames {
				row = append(row, sample.Metric[name])
			}
			t.Row(row...)
		}
	}

	return t.Render(w)
}

func buildDisplayEntries(resp *QueryResponse) []displayLogEntry {
	if resp == nil {
		return nil
	}

	entries := make([]displayLogEntry, 0)
	for _, stream := range resp.Data.Result {
		for _, value := range stream.Values {
			if len(value) < 2 {
				continue
			}
			entries = append(entries, newDisplayLogEntry(stream.Stream, value[0], value[1]))
		}
	}
	return entries
}

func newDisplayLogEntry(stream map[string]string, rawTimestamp, rawLine string) displayLogEntry {
	entry := displayLogEntry{
		Timestamp: formatHumanTimestamp(rawTimestamp),
		Message:   rawLine,
		Stream:    stream,
	}

	fields, ok := parseStructuredLogBody(rawLine)
	if !ok {
		return entry
	}

	entry.Level = popFirst(fields, "level", "lvl", "severity")
	entry.Source = popFirst(fields, "component", "caller", "logger", "source")
	entry.Message = popFirst(fields, "msg", "message")
	_ = popFirst(fields, "ts", "time", "timestamp")

	methodKey, method := pickFirst(fields, "method")
	pathKey, path := pickFirst(fields, "path", "url", "uri")
	if path != "" && (entry.Message == "" || entry.Message == path) {
		delete(fields, pathKey)
		if method != "" {
			delete(fields, methodKey)
			entry.Message = method + " " + path
		} else {
			entry.Message = path
		}
	}

	if entry.Message == "" {
		entry.Message = rawLine
	}
	entry.Details = formatDetails(fields)

	return entry
}

func anyEntry(entries []displayLogEntry, field func(displayLogEntry) string) bool {
	for _, entry := range entries {
		if field(entry) != "" {
			return true
		}
	}
	return false
}

func hasMultipleVisibleStreams(streams []StreamEntry) bool {
	seen := make(map[string]struct{})
	for _, stream := range streams {
		formatted := formatVisibleLabels(stream.Stream)
		if formatted == "" {
			continue
		}
		seen[formatted] = struct{}{}
		if len(seen) > 1 {
			return true
		}
	}
	return false
}

func parseStructuredLogBody(line string) (map[string]string, bool) {
	if fields, ok := parseJSONLogBody(line); ok {
		return fields, true
	}
	return parseLogfmtBody(line)
}

func parseJSONLogBody(line string) (map[string]string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || !strings.HasPrefix(trimmed, "{") {
		return nil, false
	}

	var raw map[string]any
	if err := json.Unmarshal([]byte(trimmed), &raw); err != nil || len(raw) == 0 {
		return nil, false
	}

	fields := make(map[string]string, len(raw))
	for key, value := range raw {
		fields[key] = stringifyAny(value)
	}
	return fields, true
}

func parseLogfmtBody(line string) (map[string]string, bool) {
	if !strings.Contains(line, "=") || !isUnambiguousLogfmt(line) {
		return nil, false
	}

	dec := logfmt.NewDecoder(strings.NewReader(line))
	if !dec.ScanRecord() {
		return nil, false
	}

	fields := make(map[string]string)
	for dec.ScanKeyval() {
		fields[string(dec.Key())] = string(dec.Value())
	}
	if err := dec.Err(); err != nil || !shouldTreatAsStructured(fields) {
		return nil, false
	}
	return fields, true
}

func shouldTreatAsStructured(fields map[string]string) bool {
	if len(fields) == 0 {
		return false
	}

	for _, key := range []string{"level", "lvl", "severity", "msg", "message", "caller", "component", "source", "logger", "ts", "time", "timestamp"} {
		if _, ok := fields[key]; ok {
			return true
		}
	}

	return len(fields) >= 3
}

func isUnambiguousLogfmt(line string) bool {
	tokens, ok := splitLogfmtTokens(line)
	if !ok || len(tokens) == 0 {
		return false
	}

	for _, token := range tokens {
		idx := strings.IndexByte(token, '=')
		if idx <= 0 {
			return false
		}
	}

	return true
}

func splitLogfmtTokens(line string) ([]string, bool) {
	tokens := make([]string, 0)
	for i := 0; i < len(line); {
		for i < len(line) && isLogfmtSpace(line[i]) {
			i++
		}
		if i >= len(line) {
			break
		}

		start := i
		inQuotes := false
		escaped := false
		for i < len(line) {
			c := line[i]
			switch {
			case inQuotes:
				switch c {
				case '\\':
					escaped = !escaped
				case '"':
					if !escaped {
						inQuotes = false
					}
					escaped = false
				default:
					escaped = false
				}
			case c == '"':
				inQuotes = true
			case isLogfmtSpace(c):
				tokens = append(tokens, line[start:i])
				goto nextToken
			}
			i++
		}
		if inQuotes {
			return nil, false
		}
		tokens = append(tokens, line[start:i])
		break

	nextToken:
		for i < len(line) && isLogfmtSpace(line[i]) {
			i++
		}
	}

	return tokens, true
}

func isLogfmtSpace(c byte) bool {
	return c <= ' '
}

func stringifyAny(v any) string {
	switch value := v.(type) {
	case nil:
		return ""
	case string:
		return value
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(value)
	case json.Number:
		return value.String()
	default:
		b, err := json.Marshal(value)
		if err != nil {
			return fmt.Sprint(value)
		}
		return string(b)
	}
}

func popFirst(fields map[string]string, keys ...string) string {
	key, value := pickFirst(fields, keys...)
	if key != "" {
		delete(fields, key)
	}
	return value
}

func pickFirst(fields map[string]string, keys ...string) (string, string) {
	for _, key := range keys {
		if value, ok := fields[key]; ok {
			return key, value
		}
	}
	return "", ""
}

func formatDetails(fields map[string]string) string {
	if len(fields) == 0 {
		return ""
	}

	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, formatKeyValue(key, fields[key]))
	}
	return strings.Join(parts, " ")
}

func formatVisibleLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}

	keys := make([]string, 0, len(labels))
	for key := range labels {
		if isHiddenLabel(key) {
			continue
		}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return ""
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, formatKeyValue(key, labels[key]))
	}
	return strings.Join(parts, " ")
}

func formatKeyValue(key, value string) string {
	if value == "" {
		return key + `=""`
	}
	if strings.ContainsAny(value, " \t\n\r\"=") {
		return key + "=" + strconv.Quote(value)
	}
	return key + "=" + value
}

func formatHumanTimestamp(raw string) string {
	nanos, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return raw
	}
	return time.Unix(0, nanos).UTC().Format(time.RFC3339Nano)
}

func collectMetricLabelNames(samples []MetricQuerySample) []string {
	nameSet := make(map[string]struct{})
	for _, s := range samples {
		for name := range s.Metric {
			if !isHiddenLabel(name) {
				nameSet[name] = struct{}{}
			}
		}
	}

	names := make([]string, 0, len(nameSet))
	for name := range nameSet {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func collectStreamLabelNames(streams []StreamEntry) []string {
	nameSet := make(map[string]struct{})
	for _, stream := range streams {
		for name := range stream.Stream {
			if !isHiddenLabel(name) {
				nameSet[name] = struct{}{}
			}
		}
	}

	names := make([]string, 0, len(nameSet))
	for name := range nameSet {
		names = append(names, name)
	}
	sort.Strings(names)

	return names
}

func isHiddenLabel(name string) bool {
	return strings.HasPrefix(name, "__")
}

func collectLabelNames(series []map[string]string) []string {
	nameSet := make(map[string]struct{})
	for _, s := range series {
		for name := range s {
			nameSet[name] = struct{}{}
		}
	}

	names := make([]string, 0, len(nameSet))
	for name := range nameSet {
		names = append(names, name)
	}
	sort.Strings(names)

	return names
}
