package dbo11y

import (
	"fmt"
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
func filterLabelAliases() map[string]string {
	return map[string]string{
		"name":                "instance",
		"host":                "instance",
		"instance_identifier": "db_instance_identifier",
	}
}

// parseFilters turns repeated `label=value` flags into query parameters. The
// API applies every parameter but `datasource` as an exact-match label
// matcher on the inventory metric, ANDed with the rest, so filtering needs
// no client-side evaluation: the filters go on the wire as given.
//
// Exact match is the only operator the API has — no regex, negation or OR —
// so `!=`, `=~` and `!~` are rejected rather than silently reinterpreted.
// The second return maps each resolved label back to the flag the user
// typed, so a caller rejecting a label can quote their input rather than
// what it resolved to.
func parseFilters(raw []string) (url.Values, map[string]string, error) {
	if len(raw) == 0 {
		return nil, nil, nil
	}
	out := url.Values{}
	from := make(map[string]string, len(raw))
	aliases := filterLabelAliases()
	for _, f := range raw {
		label, value, found := strings.Cut(f, "=")
		// The three bad forms in one check, because they fail for one reason:
		// the API takes label=value and nothing else. `engine!=x` and
		// `engine!~x` leave a label the grammar rejects, and `engine=~x` a
		// value the API would match literally. Cutting on the first "=" means
		// a value containing one of those sequences — a DSN, say — is
		// unaffected.
		if !found || !labelNamePattern.MatchString(label) || strings.HasPrefix(value, "~") {
			return nil, nil, fmt.Errorf("invalid --filter %q: expected label=value, where label is a Prometheus label name — the Database Observability API filters by exact match only, with no regex or negation", f)
		}
		if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
			value = value[1 : len(value)-1]
		}
		// The API ignores an empty value rather than matching on it.
		if value == "" {
			return nil, nil, fmt.Errorf("invalid --filter %q: a value is required, since the API ignores an empty one rather than matching on it", f)
		}
		if aliased, ok := aliases[label]; ok {
			label = aliased
		}
		if prev, dup := from[label]; dup {
			return nil, nil, fmt.Errorf("conflicting --filter %q and %q: both match on the %s label, and only one value per label can be applied", prev, f, label)
		}
		from[label] = f
		out.Set(label, value)
	}
	return out, from, nil
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
// before any request rather than as an empty result afterwards.
func validateTopQueryFilters(filters url.Values, typedAs map[string]string) error {
	fields := topQueryFilterFields(TopQuery{})
	for label := range filters {
		if _, ok := fields[label]; ok {
			continue
		}
		typed := typedAs[label]
		if typed == "" {
			typed = label
		}
		return fmt.Errorf("--filter %q cannot be applied to the top-queries view; use one of: database, datname, schema, query_id, digest", typed)
	}
	return nil
}

// filterTopQueries returns the rows matching every filter.
func filterTopQueries(items []TopQuery, filters url.Values) []TopQuery {
	if len(filters) == 0 {
		return items
	}
	out := make([]TopQuery, 0, len(items))
	for _, q := range items {
		fields := topQueryFilterFields(q)
		matched := true
		for label, values := range filters {
			for _, want := range values {
				if fields[label] != want {
					matched = false
					break
				}
			}
			if !matched {
				break
			}
		}
		if matched {
			out = append(out, q)
		}
	}
	return out
}
