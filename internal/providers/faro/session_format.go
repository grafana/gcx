package faro

import (
	"bytes"
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-logfmt/logfmt"
	"github.com/grafana/gcx/internal/query/loki"
	querysql "github.com/grafana/gcx/internal/query/sql"
)

const (
	sessionDumpMetadataHeader = "=== session metadata ==="
	sessionDumpEventsHeader   = "=== events ==="
)

func formatSessionDump(metadata, events string) string {
	var b strings.Builder
	b.WriteString(sessionDumpMetadataHeader)
	b.WriteByte('\n')
	b.WriteString(strings.TrimRight(metadata, "\n"))
	b.WriteString("\n\n")
	b.WriteString(sessionDumpEventsHeader)
	b.WriteByte('\n')
	b.WriteString(strings.TrimRight(events, "\n"))
	b.WriteByte('\n')
	return b.String()
}

func pinotMetadataTimeKeys() []string {
	return []string{"session_start", "session_last_event", "session_replay_start"}
}

func isPinotMetadataTimeKey(key string) bool {
	return slices.Contains(pinotMetadataTimeKeys(), key)
}

func pinotInt64(v any) (int64, bool) {
	switch val := v.(type) {
	case int:
		return int64(val), true
	case int64:
		return val, true
	case float64:
		return int64(val), true
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64)
		return n, err == nil
	default:
		return 0, false
	}
}

func formatPinotMetaValue(name string, v any) string {
	if v == nil {
		return ""
	}
	if isPinotMetadataTimeKey(name) {
		ms, ok := pinotInt64(v)
		if !ok || ms <= 0 || ms == math.MaxInt64 {
			return ""
		}
		return time.UnixMilli(ms).UTC().Format(time.RFC3339)
	}
	s := strings.TrimSpace(tsvCell(v))
	if s == "" || strings.EqualFold(s, "null") {
		return ""
	}
	return s
}

func pinotMetadataFields(resps ...*querysql.QueryResponse) map[string]string {
	fields := make(map[string]string)
	for _, resp := range resps {
		if resp == nil || len(resp.Rows) == 0 {
			continue
		}
		row := resp.Rows[0]
		for i, col := range resp.Columns {
			name := strings.TrimSpace(col.Name)
			if name == "" || i >= len(row) {
				continue
			}
			if val := formatPinotMetaValue(name, row[i]); val != "" {
				fields[name] = val
			}
		}
	}
	return fields
}

// formatPinotMetadata prints Pinot session metadata as one key=value per line,
// the same shape as Loki. Empty, "null", and sentinel time values are omitted.
func formatPinotMetadata(resps ...*querysql.QueryResponse) string {
	fields := pinotMetadataFields(resps...)
	if len(fields) == 0 {
		return ""
	}
	var b strings.Builder
	written := make(map[string]struct{})
	for _, key := range append(lokiMetadataKeys(), pinotMetadataTimeKeys()...) {
		if v := fields[key]; v != "" {
			writeLogfmtKV(&b, key, v)
			written[key] = struct{}{}
		}
	}
	extra := make([]string, 0, len(fields))
	for key := range fields {
		if _, ok := written[key]; !ok {
			extra = append(extra, key)
		}
	}
	sort.Strings(extra)
	for _, key := range extra {
		writeLogfmtKV(&b, key, fields[key])
	}
	return b.String()
}

// formatPinotTSV prints a Pinot result as tab-separated values with a header
// row. Empty results still emit the header when columns are present.
func formatPinotTSV(resp *querysql.QueryResponse) string {
	if resp == nil || len(resp.Columns) == 0 {
		return ""
	}

	var b strings.Builder
	for i, col := range resp.Columns {
		if i > 0 {
			b.WriteByte('\t')
		}
		b.WriteString(col.Name)
	}
	b.WriteByte('\n')

	for _, row := range resp.Rows {
		for i := range resp.Columns {
			if i > 0 {
				b.WriteByte('\t')
			}
			if i < len(row) {
				b.WriteString(tsvCell(row[i]))
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func tsvCell(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return escapeTSV(val)
	case float64:
		if val == float64(int64(val)) {
			return strconv.FormatInt(int64(val), 10)
		}
		return strconv.FormatFloat(val, 'f', -1, 64)
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	default:
		return escapeTSV(fmt.Sprintf("%v", val))
	}
}

func escapeTSV(s string) string {
	if !strings.ContainsAny(s, "\\\t\r\n") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func formatLokiLines(resp *loki.QueryResponse) string {
	if resp == nil || len(resp.Data.Result) == 0 {
		return ""
	}
	type timedLine struct {
		ts, line string
	}
	var lines []timedLine
	for _, stream := range resp.Data.Result {
		for _, entry := range stream.Values {
			lines = append(lines, timedLine{ts: entry.Timestamp, line: entry.Line})
		}
	}
	sort.SliceStable(lines, func(i, j int) bool {
		return lokiTimestampLess(lines[i].ts, lines[j].ts)
	})
	var b strings.Builder
	for _, entry := range lines {
		if entry.ts != "" {
			b.WriteString(entry.ts)
			b.WriteByte('\t')
		}
		b.WriteString(dropMetadataLogfmt(entry.line))
		b.WriteString("\n\n")
	}
	return b.String()
}

func lokiTimestampLess(a, b string) bool {
	ai, aErr := strconv.ParseInt(a, 10, 64)
	bi, bErr := strconv.ParseInt(b, 10, 64)
	if aErr == nil && bErr == nil {
		return ai < bi
	}
	return a < b
}

// Session-level Faro logfmt keys printed first. Remaining envelope keys
// matching sessionMetadataPrefixes() are appended in sorted order.
func lokiMetadataKeys() []string {
	return []string{
		"sdk_name",
		"sdk_version",
		"os_name",
		"os_version",
		"browser_name",
		"browser_version",
		"browser_os",
		"device_model_name",
		"app_name",
		"app_version",
		"app_environment",
		"user_id",
		"user_username",
		"user_email",
		"session_id",
		"geo_country_iso",
		"geo_city",
	}
}

func sessionMetadataPrefixes() []string {
	return []string{
		"sdk_",
		"app_",
		"user_",
		"os_",
		"geo_",
		"browser_",
		"device_",
		"session_attr_",
	}
}

func isSessionMetadataKey(key string) bool {
	switch key {
	case "session_id", "event_data_session.id", "event_data_user.id", "faro_sdk_version":
		return true
	}
	for _, p := range sessionMetadataPrefixes() {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return false
}

func formatLokiMetadata(metaResp, replayResp *loki.QueryResponse) string {
	fields := parseLogfmt(firstLokiLine(metaResp))
	var b strings.Builder
	written := make(map[string]struct{})
	for _, key := range lokiMetadataKeys() {
		if v := fields[key]; v != "" {
			writeLogfmtKV(&b, key, v)
			written[key] = struct{}{}
		}
	}
	extra := make([]string, 0, len(fields))
	for key, v := range fields {
		if v == "" {
			continue
		}
		if _, ok := written[key]; ok {
			continue
		}
		if key == "event_data_session.id" || key == "event_data_user.id" {
			continue
		}
		if isSessionMetadataKey(key) {
			extra = append(extra, key)
		}
	}
	sort.Strings(extra)
	for _, key := range extra {
		writeLogfmtKV(&b, key, fields[key])
	}
	if ts := firstLokiTimestamp(replayResp); ts != "" {
		writeLogfmtKV(&b, "session_replay_start", ts)
	}
	return b.String()
}

func parseLogfmt(line string) map[string]string {
	fields := make(map[string]string)
	d := logfmt.NewDecoder(strings.NewReader(line))
	for d.ScanRecord() {
		for d.ScanKeyval() {
			fields[string(d.Key())] = string(d.Value())
		}
	}
	return fields
}

func writeLogfmtKV(b *strings.Builder, key, value string) {
	writeLogfmtField(b, key, value)
	b.WriteByte('\n')
}

func writeLogfmtField(b *strings.Builder, key, value string) {
	// Decoder turns quoted `\n` into a real newline. Re-emit with the
	// logfmt encoder so backslashes and control characters stay escaped
	// and a multiline exception stays one dump record.
	var buf bytes.Buffer
	if err := logfmt.NewEncoder(&buf).EncodeKeyval(key, value); err != nil {
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(escapeTSV(value))
		return
	}
	b.Write(buf.Bytes())
}

func dropMetadataLogfmt(line string) string {
	var b strings.Builder
	seen, kept := 0, 0
	d := logfmt.NewDecoder(strings.NewReader(line))
	for d.ScanRecord() {
		for d.ScanKeyval() {
			seen++
			key := string(d.Key())
			if isSessionMetadataKey(key) {
				continue
			}
			if kept > 0 {
				b.WriteByte(' ')
			}
			writeLogfmtField(&b, key, string(d.Value()))
			kept++
		}
	}
	if seen == 0 {
		return line
	}
	return b.String()
}

func logfmtValue(line, key string) string {
	return parseLogfmt(line)[key]
}

func firstLokiLine(resp *loki.QueryResponse) string {
	if resp == nil {
		return ""
	}
	for _, stream := range resp.Data.Result {
		for _, entry := range stream.Values {
			if strings.TrimSpace(entry.Line) != "" {
				return entry.Line
			}
		}
	}
	return ""
}

func firstLokiTimestamp(resp *loki.QueryResponse) string {
	if resp == nil {
		return ""
	}
	for _, stream := range resp.Data.Result {
		for _, entry := range stream.Values {
			if entry.Timestamp != "" {
				return entry.Timestamp
			}
		}
	}
	return ""
}

func pinotCell(resp *querysql.QueryResponse, name string) string {
	if resp == nil || len(resp.Rows) == 0 {
		return ""
	}
	idx := -1
	for i, col := range resp.Columns {
		if col.Name == name {
			idx = i
			break
		}
	}
	if idx < 0 || idx >= len(resp.Rows[0]) {
		return ""
	}
	return tsvCell(resp.Rows[0][idx])
}

func pinotResponseHasValues(resp *querysql.QueryResponse) bool {
	if resp == nil {
		return false
	}
	for _, row := range resp.Rows {
		for _, cell := range row {
			if strings.TrimSpace(tsvCell(cell)) != "" {
				return true
			}
		}
	}
	return false
}
