package tempo

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	cmdio "github.com/grafana/gcx/internal/output"
)

const (
	// ExceptionsTraceLimit is the most traces this command samples. The
	// Exceptions tab requests 400; this command stays lower.
	ExceptionsTraceLimit = 300
	// ExceptionsSpansPerTrace is the tab's spans-per-span-set cap (spss).
	ExceptionsSpansPerTrace = 10

	exceptionsProjection = "select(resource.service.name, event.exception.message, event.exception.type) with(most_recent=true)"
	groupingEdgeLength   = 20
)

// SpanSearchRequest is a Tempo /api/search call that returns span attributes.
// SpansPerSpanSet 0 selects ExceptionsSpansPerTrace.
type SpanSearchRequest struct {
	Query           string
	Start           time.Time
	End             time.Time
	Limit           int
	SpansPerSpanSet int
}

// SpanSearchResponse is the Tempo search payload for a span projection.
// Trace search (`SearchResponse`) stays free of span sets so its JSON shape
// does not change.
type SpanSearchResponse struct {
	Traces []SpanSearchTrace `json:"traces"`
}

// SpanSearchTrace is one trace in a span search, including its span sets.
type SpanSearchTrace struct {
	StartTimeUnixNano jsonUintString `json:"startTimeUnixNano"`
	SpanSet           *SpanSet       `json:"spanSet,omitempty"`
	SpanSets          []SpanSet      `json:"spanSets,omitempty"`
}

// SpanSet is one Tempo search span set. Span-level attributes override
// span-set attributes of the same key, matching the datasource plugin.
type SpanSet struct {
	Attributes []SpanAttribute `json:"attributes,omitempty"`
	Spans      []SpanHit       `json:"spans"`
}

// SpanHit is one span in a search span set.
type SpanHit struct {
	StartTimeUnixNano jsonUintString  `json:"startTimeUnixNano"`
	Attributes        []SpanAttribute `json:"attributes,omitempty"`
}

// SpanAttribute is one TraceQL attribute on a span or span set.
type SpanAttribute struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

// jsonUintString accepts Tempo's uint64 fields as either a JSON string or a
// number. Protojson emits strings; some fixtures emit numbers.
type jsonUintString string

func (n *jsonUintString) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*n = ""
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*n = jsonUintString(s)
		return nil
	}
	*n = jsonUintString(b)
	return nil
}

func (n jsonUintString) uint() uint64 {
	v, err := strconv.ParseUint(string(n), 10, 64)
	if err != nil {
		return 0
	}
	return v
}

func (n jsonUintString) time() time.Time {
	v := n.uint()
	if v == 0 || v > math.MaxInt64 {
		return time.Time{}
	}
	return time.Unix(0, int64(v)).UTC()
}

// ExceptionGroup is one row of the Exceptions tab table.
type ExceptionGroup struct {
	Service         string    `json:"service"`
	Type            string    `json:"type"`
	Message         string    `json:"message"`
	Occurrences     int       `json:"occurrences"`
	LastSeen        time.Time `json:"last_seen,omitzero" yaml:"last_seen,omitempty"`
	GroupedMessages []string  `json:"grouped_messages"`
	// URL is a Traces Drilldown Exceptions tab link for this row. Empty until
	// the command fills it, and omitted from machine output when unset.
	URL string `json:"url,omitempty" yaml:"url,omitempty"`
}

// ExceptionsResult is the grouped exception sample. list_meta is set only when
// Tempo returned as many traces as --limit. Returned counts groups, not traces.
// Continue raises --limit up to ExceptionsTraceLimit.
type ExceptionsResult struct {
	Query         string           `json:"query"`
	TraceLimit    int              `json:"trace_limit"`
	SpansPerTrace int              `json:"spans_per_trace"`
	TracesSampled int              `json:"traces_sampled"`
	SpansSampled  int              `json:"spans_sampled"`
	Exceptions    []ExceptionGroup `json:"exceptions"`
	ListMeta      *cmdio.ListMeta  `json:"list_meta,omitempty" yaml:"list_meta,omitempty"`
}

// ListItemsKey preserves exception rows when --json selects item fields.
func (*ExceptionsResult) ListItemsKey() string { return "exceptions" }

// BuildExceptionsQuery turns an optional flat spanset into the Exceptions tab
// span query. An empty filter or "{}" covers every service. The command owns
// the status filter and the select projection; a pipeline in filter is rejected.
func BuildExceptionsQuery(filter string) (string, error) {
	filter = strings.TrimSpace(filter)
	if filter == "" || filter == "{}" {
		return "{ status = error } | " + exceptionsProjection, nil
	}
	inner, rest, ok := splitSpanset(filter)
	if !ok || strings.TrimSpace(rest) != "" {
		return "", fmt.Errorf("exceptions filter must be one TraceQL spanset such as '{ resource.service.name = \"checkout\" }' (this command adds status = error and the exception select); got %q", filter)
	}
	inner = strings.TrimSpace(inner)
	if inner == "" {
		return "{ status = error } | " + exceptionsProjection, nil
	}
	return "{ (" + inner + ") && status = error } | " + exceptionsProjection, nil
}

// AggregateExceptions groups span-search rows the way the Exceptions tab's
// aggregateExceptions does. The representative message is normalized; grouped
// messages keep the whitespace-collapsed originals. LastSeen is the latest
// span time, not a relative label.
func AggregateExceptions(resp *SpanSearchResponse, query string, traceLimit int) *ExceptionsResult {
	result := &ExceptionsResult{
		Query:         query,
		TraceLimit:    traceLimit,
		SpansPerTrace: ExceptionsSpansPerTrace,
		Exceptions:    []ExceptionGroup{},
	}
	if resp == nil {
		return result
	}
	result.TracesSampled = len(resp.Traces)
	result.SpansSampled, result.Exceptions = groupExceptions(resp)
	return result
}

// ExceptionListMeta reports a full trace sample. groups is the number of
// exception rows. Continue, when set, is a larger trace --limit derived from
// argv, capped at ExceptionsTraceLimit.
func ExceptionListMeta(groups, tracesSampled, limit int, argv []string) *cmdio.ListMeta {
	if limit <= 0 || tracesSampled < limit {
		return nil
	}
	meta := &cmdio.ListMeta{
		Truncated: true,
		Returned:  groups,
	}
	if limit >= ExceptionsTraceLimit {
		meta.Cap = ExceptionsTraceLimit
		return meta
	}
	meta.Continue = cmdio.BuildListLimitCommand(argv, min(limit*2, ExceptionsTraceLimit))
	return meta
}

func applyAttrs(dst map[string]string, attrs []SpanAttribute) {
	for _, attr := range attrs {
		if value, ok := attributeString(attr.Value); ok {
			dst[attr.Key] = value
		}
	}
}

func firstAttr(attrs map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := attrs[key]; value != "" {
			return value
		}
	}
	return ""
}

func attributeString(raw json.RawMessage) (string, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return "", false
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, text != ""
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return "", false
	}
	if nested, ok := obj["Value"].(map[string]any); ok {
		maps.Copy(obj, nested)
	}
	for _, key := range []string{
		"stringValue", "string_value",
		"intValue", "int_value", "doubleValue", "double_value",
		"boolValue", "bool_value",
	} {
		value, ok := obj[key]
		if !ok || value == nil {
			continue
		}
		if s := scalarString(value); s != "" {
			return s, true
		}
	}
	return "", false
}

func scalarString(v any) string {
	switch n := v.(type) {
	case string:
		return n
	case bool:
		return strconv.FormatBool(n)
	case float64:
		if n == math.Trunc(n) && !math.IsInf(n, 0) {
			return strconv.FormatInt(int64(n), 10)
		}
		return strconv.FormatFloat(n, 'f', -1, 64)
	default:
		return ""
	}
}

type exceptionAcc struct {
	message string
	typ     string
	service string
	count   int
	last    time.Time
	raw     []string
	seen    map[string]struct{}
}

func groupExceptions(resp *SpanSearchResponse) (int, []ExceptionGroup) {
	traces := slices.Clone(resp.Traces)
	slices.SortStableFunc(traces, func(a, b SpanSearchTrace) int {
		return cmp.Compare(b.StartTimeUnixNano.uint(), a.StartTimeUnixNano.uint())
	})

	var sampled int
	order := []string{}
	groups := map[string]*exceptionAcc{}
	for _, tr := range traces {
		sets := tr.SpanSets
		if sets == nil && tr.SpanSet != nil {
			sets = []SpanSet{*tr.SpanSet}
		}
		for _, set := range sets {
			for _, span := range set.Spans {
				sampled++
				attrs := map[string]string{}
				applyAttrs(attrs, set.Attributes)
				applyAttrs(attrs, span.Attributes)
				message := firstAttr(attrs, "event.exception.message", "exception.message")
				if message == "" {
					continue
				}
				normalized := normalizeExceptionMessage(message)
				if normalized == "" {
					continue
				}
				raw := collapseSpace(message)
				typ := firstAttr(attrs, "event.exception.type", "exception.type")
				key := exceptionGroupingKey(normalized, typ)
				group := groups[key]
				if group == nil {
					group = &exceptionAcc{message: normalized, typ: typ, seen: map[string]struct{}{}}
					groups[key] = group
					order = append(order, key)
				}
				group.count++
				if service := firstAttr(attrs, "resource.service.name", "service.name"); group.service == "" && service != "" {
					group.service = service
				}
				if _, ok := group.seen[raw]; raw != "" && !ok {
					group.seen[raw] = struct{}{}
					group.raw = append(group.raw, raw)
				}
				if when := span.StartTimeUnixNano.time(); !when.IsZero() && when.After(group.last) {
					group.last = when
				}
			}
		}
	}

	slices.SortStableFunc(order, func(a, b string) int {
		return cmp.Compare(groups[b].count, groups[a].count)
	})
	out := make([]ExceptionGroup, 0, len(order))
	for _, key := range order {
		group := groups[key]
		if group.typ == "" {
			group.typ = "Unknown"
		}
		out = append(out, ExceptionGroup{
			Service:         group.service,
			Type:            group.typ,
			Message:         group.message,
			Occurrences:     group.count,
			LastSeen:        group.last,
			GroupedMessages: group.raw,
		})
	}
	return sampled, out
}

var spaceRE = regexp.MustCompile(`\s+`)

type placeholder struct {
	re   *regexp.Regexp
	repl string
}

// placeholder order matches the Exceptions tab: specific tokens before bare numbers.
var exceptionPlaceholders = []placeholder{ //nolint:gochecknoglobals // constant patterns
	{regexp.MustCompile(`(?i)\bhttps?://\S+`), "<url>"},
	{regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\b`), "<uuid>"},
	{regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`), "<ip>"},
	{regexp.MustCompile(`(?i)\b0x[0-9a-f]+\b`), "<hex>"},
	{regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z?\b`), "<timestamp>"},
	{regexp.MustCompile(`(?i)\b([a-z][a-z0-9._-]*id)=([a-z0-9_-]+)\b`), "${1}=<id>"},
	{regexp.MustCompile(`\b\d+\b`), "<num>"},
}

func collapseSpace(message string) string {
	return strings.TrimSpace(spaceRE.ReplaceAllString(message, " "))
}

func normalizeExceptionMessage(message string) string {
	normalized := collapseSpace(message)
	for _, pattern := range exceptionPlaceholders {
		normalized = pattern.re.ReplaceAllString(normalized, pattern.repl)
	}
	return normalized
}

func exceptionGroupingKey(normalized, typ string) string {
	trimmed := strings.TrimSpace(normalized)
	runes := []rune(trimmed)
	prefix := trimmed
	suffix := trimmed
	if len(runes) > groupingEdgeLength {
		prefix = string(runes[:groupingEdgeLength])
		suffix = string(runes[len(runes)-groupingEdgeLength:])
	}
	return typ + "|" + prefix + "|" + suffix + "|" + strconv.Itoa(len(runes))
}
