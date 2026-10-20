package dbo11y

import (
	"fmt"
	"maps"
	"net/url"
	"regexp"
	"strings"
)

// labelNamePattern keeps a filter's label to a valid format.
var labelNamePattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// filterLabelAliases maps the field names these commands print onto the
// telemetry label names the API filters by, for the three that differ. The
// API matches labels on the monitored instances themselves, which have no
// notion of the `name`/`host` split this output reports.
//
// So both `name` and `host` resolve to the endpoint. Instance.Name shows
// `service_name` in preference to the endpoint where a stack sets one, and on
// those instances `--filter name=` will not match the reported name —
// `--filter service_name=` is what does. Pointing `name` at Instance.Name
// instead would fix the mismatch but cost the push-down: the telemetry carries
// no `name` label for the API to match, so every `name` filter would fetch the
// whole fleet. Documented on `instances list` rather than changed.
func filterLabelAliases() map[string]string {
	return map[string]string{
		"name":                "instance",
		"host":                "instance",
		"instance_identifier": "db_instance_identifier",
	}
}

// filterOp is a --filter comparison. The API matches exact values and nothing
// else, so only opEqual can travel on the wire; the rest are evaluated against
// the rows it returns.
type filterOp string

const (
	opEqual    filterOp = "="
	opNotEqual filterOp = "!="
	opMatch    filterOp = "=~"
	opNotMatch filterOp = "!~"
)

// pushable reports whether the API can apply this comparison itself.
func (o filterOp) pushable() bool { return o == opEqual }

// filter is one parsed --filter flag.
type filter struct {
	label string
	op    filterOp
	value string

	// re is the compiled value for opMatch and opNotMatch, anchored so a
	// pattern has to match the whole label value — the semantics Prometheus
	// gives `=~`, rather than Go's substring default.
	re *regexp.Regexp

	// raw is the flag as the user wrote it, so an error can quote their input
	// rather than what it resolved to.
	raw string
}

// matches reports whether a label value satisfies this filter.
func (f filter) matches(got string) bool {
	switch f.op {
	case opEqual:
		return got == f.value
	case opNotEqual:
		return got != f.value
	case opMatch:
		return f.re.MatchString(got)
	case opNotMatch:
		return !f.re.MatchString(got)
	}
	return false
}

// splitFilter cuts a flag at its first comparison operator. First rather than
// last, so a value containing one — a DSN with `=~` in a password, say — is
// taken as part of the value.
func splitFilter(s string) (string, filterOp, string, bool) {
	i := strings.IndexAny(s, "=!")
	if i < 0 {
		return "", "", "", false
	}
	switch {
	case strings.HasPrefix(s[i:], string(opNotEqual)):
		return s[:i], opNotEqual, s[i+2:], true
	case strings.HasPrefix(s[i:], string(opNotMatch)):
		return s[:i], opNotMatch, s[i+2:], true
	case strings.HasPrefix(s[i:], string(opMatch)):
		return s[:i], opMatch, s[i+2:], true
	case s[i] == '=':
		return s[:i], opEqual, s[i+1:], true
	}
	return "", "", "", false
}

// parseFilter parses one `label<op>value` flag, resolving the label to the
// name the telemetry carries.
func parseFilter(s string) (filter, error) {
	label, op, value, found := splitFilter(s)
	if !found || !labelNamePattern.MatchString(label) {
		return filter{}, fmt.Errorf("invalid --filter %q: expected label=value, label!=value, label=~regex or label!~regex, where label is a Prometheus label name", s)
	}
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		value = value[1 : len(value)-1]
	}
	// `datasource` selects which datasource to ask, so it is stripped from the
	// filters before they go on the wire. Accepting it here would drop it
	// silently and answer with the whole inventory, which is wider than what
	// was asked for.
	if label == datasourceParam {
		return filter{}, fmt.Errorf("invalid --filter %q: %s selects the datasource to query, not a label to match; use -d/--datasource", s, datasourceParam)
	}
	if value == "" {
		return filter{}, fmt.Errorf("invalid --filter %q: a value is required", s)
	}
	if aliased, ok := filterLabelAliases()[label]; ok {
		label = aliased
	}

	f := filter{label: label, op: op, value: value, raw: s}
	if op == opMatch || op == opNotMatch {
		re, err := regexp.Compile("^(?:" + value + ")$")
		if err != nil {
			return filter{}, fmt.Errorf("invalid --filter %q: %w", s, err)
		}
		f.re = re
	}
	return f, nil
}

// parseFilters turns repeated `label<op>value` flags into filters.
//
// `=` goes on the wire, where the API applies it as an exact-match label
// matcher ANDed with the rest. `!=`, `=~` and `!~` have no equivalent there,
// so they are applied to the rows the API returns — which is why they cost a
// wider response rather than a narrower request.
func parseFilters(raw []string) ([]filter, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]filter, 0, len(raw))
	exact := make(map[string]string, len(raw))

	for _, s := range raw {
		f, err := parseFilter(s)
		if err != nil {
			return nil, err
		}
		// Two exact matches on one label cannot both hold, and only one can go
		// on the wire. Operator filters stack fine, so they are not restricted.
		if f.op == opEqual {
			if prev, dup := exact[f.label]; dup {
				return nil, fmt.Errorf("conflicting --filter %q and %q: both match on the %s label, and only one value per label can be applied", prev, s, f.label)
			}
			exact[f.label] = s
		}
		out = append(out, f)
	}
	return out, nil
}

// apiFilters returns the query parameters for the filters the API can apply
// itself. The rest are left to clientFilters.
func apiFilters(filters []filter) url.Values {
	out := url.Values{}
	for _, f := range filters {
		if f.op.pushable() {
			out.Set(f.label, f.value)
		}
	}
	return out
}

// clientFilters returns the filters the API cannot apply, which have to be
// evaluated against the rows it returns. Any of them makes a request fetch
// more than it returns.
//
// The exact matches have to be dropped here rather than harmlessly re-applied:
// the API matches them against every label the collector emitted, but withholds
// some of those from the response — `dsn`, because it can carry credentials,
// and `job`. Re-applying `--filter dsn=…` to a row that cannot carry `dsn`
// would compare it against the empty string and discard every row the API just
// matched.
func clientFilters(filters []filter) []filter {
	var out []filter
	for _, f := range filters {
		if !f.op.pushable() {
			out = append(out, f)
		}
	}
	return out
}

// matchFilters reports whether fields satisfies every filter.
//
// A label fields does not carry is compared as the empty string, which is the
// semantics Prometheus gives an absent label: `engine!="mysql"` holds for a row
// with no engine at all, and `engine=~"my.*"` does not.
func matchFilters(fields map[string]string, filters []filter) bool {
	for _, f := range filters {
		if !f.matches(fields[f.label]) {
			return false
		}
	}
	return true
}

// instanceFilterFields are the label values an inventory row carries, which is
// what a client-side filter matches against. The collector's own labels are
// included, so any label the API could have filtered on is also one the
// operators can.
func instanceFilterFields(i Instance) map[string]string {
	fields := make(map[string]string, len(i.Labels)+8)
	maps.Copy(fields, i.Labels)
	fields["instance"] = i.Host
	fields["engine"] = i.Engine
	fields["engine_version"] = i.EngineVersion
	fields["db_instance_identifier"] = i.InstanceIdentifier
	fields["provider_name"] = i.ProviderName
	fields["provider_account"] = i.ProviderAccount
	fields["provider_region"] = i.ProviderRegion
	fields["server_id"] = i.ServerID
	return fields
}

// filterInstances returns the rows matching every filter the API did not apply.
func filterInstances(items []Instance, filters []filter) []Instance {
	remaining := clientFilters(filters)
	if len(remaining) == 0 {
		return items
	}
	out := make([]Instance, 0, len(items))
	for _, i := range items {
		if matchFilters(instanceFilterFields(i), remaining) {
			out = append(out, i)
		}
	}
	return out
}

// topQueryFilterFields are the fields `instances get --filter` can match on.
// Unlike the inventory route, the queries route ignores label filters, so
// these are applied to the fetched rows — which limits them to what the
// response carries.
func topQueryFilterFields(q TopQuery) map[string]string {
	return map[string]string{
		"database": q.Datname,
		"datname":  q.Datname,
		"schema":   q.Datname,
		"query_id": q.QueryID,
		"digest":   q.QueryID,
	}
}

// validateTopQueryFilters rejects a filter `instances get` cannot apply,
// before any request rather than as an empty result afterwards. The zero row
// supplies the accepted label set, so it cannot drift from what the filters
// are actually matched against.
func validateTopQueryFilters(filters []filter) error {
	fields := topQueryFilterFields(TopQuery{})
	for _, f := range filters {
		if _, ok := fields[f.label]; !ok {
			return fmt.Errorf("--filter %q cannot be applied to the top-queries view; use one of: database, datname, schema, query_id, digest", f.raw)
		}
	}
	return nil
}

// filterTopQueries returns the rows matching every filter. Every one is
// applied here: the queries route takes none of them.
func filterTopQueries(items []TopQuery, filters []filter) []TopQuery {
	if len(filters) == 0 {
		return items
	}
	out := make([]TopQuery, 0, len(items))
	for _, q := range items {
		if matchFilters(topQueryFilterFields(q), filters) {
			out = append(out, q)
		}
	}
	return out
}
