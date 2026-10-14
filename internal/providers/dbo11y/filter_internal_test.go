package dbo11y

import (
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestParseFilters(t *testing.T) {
	cases := []struct {
		name string
		raw  []string
		want url.Values
	}{
		{"none", nil, nil},
		{"one pair", []string{"engine=postgres"}, url.Values{"engine": {"postgres"}}},
		{
			"several are ANDed by the API",
			[]string{"engine=postgres", "provider_name=aws"},
			url.Values{"engine": {"postgres"}, "provider_name": {"aws"}},
		},
		{"quoted values are unwrapped", []string{`engine="postgres"`}, url.Values{"engine": {"postgres"}}},
		{
			"a value may contain = itself",
			[]string{"dsn=user:pw@tcp(host:3306)/db?a=b"},
			url.Values{"dsn": {"user:pw@tcp(host:3306)/db?a=b"}},
		},
		// The API matches labels on the inventory metric, so a label the
		// response never returns still filters.
		{"a label the response omits", []string{"cluster=dev-eu-west-2"}, url.Values{"cluster": {"dev-eu-west-2"}}},
		// The operator check must not fire on a value that happens to contain
		// one of those sequences.
		{"a value containing !=", []string{"dsn=a!=b"}, url.Values{"dsn": {"a!=b"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _, err := parseFilters(c.raw)
			if err != nil {
				t.Fatalf("parseFilters(%v) err = %v", c.raw, err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("parseFilters(%v) = %v, want %v", c.raw, got, c.want)
			}
		})
	}
}

func TestParseFilters_MapsOutputNamesOntoLabelNames(t *testing.T) {
	// These commands print `name`, `host` and `instance_identifier`; the
	// metric the API filters on calls them `instance` and
	// `db_instance_identifier`.
	cases := []struct{ raw, label, value string }{
		{"name=db-1", "instance", "db-1"},
		{"host=db-1", "instance", "db-1"},
		{"instance_identifier=rds1", "db_instance_identifier", "rds1"},
	}
	for _, c := range cases {
		t.Run(c.raw, func(t *testing.T) {
			got, _, err := parseFilters([]string{c.raw})
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("parseFilters(%q) = %v, want exactly one label", c.raw, got)
			}
			if got.Get(c.label) != c.value {
				t.Errorf("parseFilters(%q) = %v, want %s=%s", c.raw, got, c.label, c.value)
			}
		})
	}

	// service_name is a label the telemetry itself can carry, so it must
	// reach the API as written rather than being rewritten to `instance` —
	// which would answer a different question and, where the label exists,
	// a wrong one.
	got, _, err := parseFilters([]string{"service_name=db-1"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got.Get("service_name") != "db-1" || got.Has("instance") {
		t.Errorf("service_name must be forwarded as itself, got %v", got)
	}
}

func TestParseFilters_Rejects(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"no operator", "engine", "expected label=value"},
		// The API has no negation or regex, so these have to fail loudly
		// rather than be reinterpreted as an exact match.
		{"not-equal", "engine!=mysql", "exact match only"},
		{"regex", "engine=~post.*", "exact match only"},
		{"negated regex", "engine!~post.*", "exact match only"},
		// Both of the API's silent-drop cases, which would otherwise return
		// the unfiltered fleet.
		{"empty value", "engine=", "a value is required"},
		{"invalid label name", "engine-x=postgres", "Prometheus label name"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := parseFilters([]string{c.raw})
			if err == nil {
				t.Fatalf("parseFilters(%q) = nil error", c.raw)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %q, want it to mention %q", err, c.want)
			}
		})
	}
}

func TestValidateTopQueryFilters(t *testing.T) {
	// The queries route ignores label filters, so `instances get` applies them
	// to the fetched rows and can only honour the fields they carry.
	for _, label := range []string{"database", "datname", "schema", "query_id", "digest"} {
		if err := validateTopQueryFilters(url.Values{label: {"x"}}, nil); err != nil {
			t.Errorf("%s should be filterable, got %v", label, err)
		}
	}
	err := validateTopQueryFilters(url.Values{"engine": {"postgres"}}, nil)
	if err == nil {
		t.Fatal("expected an error for a field the rows do not carry")
	}
	if !strings.Contains(err.Error(), "datname") {
		t.Errorf("error should list what is filterable, got %q", err)
	}
}

func TestFilterTopQueries(t *testing.T) {
	items := []TopQuery{
		{QueryID: "1", Datname: "payments"},
		{QueryID: "2", Datname: "orders"},
	}

	// datname was the Postgres spelling and schema the MySQL one; database is
	// the API's own. All three scope to the same field.
	for _, label := range []string{"database", "datname", "schema"} {
		t.Run(label, func(t *testing.T) {
			got := filterTopQueries(items, url.Values{label: {"payments"}})
			if len(got) != 1 || got[0].QueryID != "1" {
				t.Errorf("filterTopQueries with %s=payments = %+v", label, got)
			}
		})
	}

	t.Run("by query id", func(t *testing.T) {
		got := filterTopQueries(items, url.Values{"digest": {"2"}})
		if len(got) != 1 || got[0].QueryID != "2" {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("no filters passes everything through", func(t *testing.T) {
		if got := filterTopQueries(items, nil); len(got) != 2 {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("filters are ANDed", func(t *testing.T) {
		got := filterTopQueries(items, url.Values{"datname": {"payments"}, "query_id": {"2"}})
		if len(got) != 0 {
			t.Errorf("expected no row to satisfy both, got %+v", got)
		}
	})
}

func TestParseFilters_RejectsConflictingLabels(t *testing.T) {
	// The API keeps the first value for a label and drops the rest without
	// saying so, so two values for one label would report a result as though
	// both had applied.
	cases := []struct {
		name string
		raw  []string
		want string
	}{
		{"same label twice", []string{"engine=postgres", "engine=mysql"}, "engine"},
		// Aliasing makes it reachable without repeating a flag.
		{"name and host both resolve to instance", []string{"name=a", "host=b"}, "instance"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := parseFilters(c.raw)
			if err == nil {
				t.Fatalf("parseFilters(%v) = nil error", c.raw)
			}
			msg := err.Error()
			if !strings.Contains(msg, "conflicting") || !strings.Contains(msg, c.want) {
				t.Errorf("error = %q, want it to name the conflict on %q", msg, c.want)
			}
			// The message must quote what the user typed, not what it resolved to.
			for _, f := range c.raw {
				if !strings.Contains(msg, f) {
					t.Errorf("error = %q, want it to quote %q", msg, f)
				}
			}
		})
	}

	// The same label with the same value twice is still a conflict: the API
	// cannot be told to apply it twice, and silently keeping one is the
	// behaviour being guarded against.
	if _, _, err := parseFilters([]string{"engine=mysql", "engine=mysql"}); err == nil {
		t.Error("expected a repeated identical filter to be rejected too")
	}
}

func TestValidateTopQueryFilters_NamesTheTypedFlag(t *testing.T) {
	// `name` resolves to `instance`; the error has to say `name`, which is
	// what the user passed.
	filters, typedAs, err := parseFilters([]string{"name=db-1"})
	if err != nil {
		t.Fatalf("parseFilters() err = %v", err)
	}
	err = validateTopQueryFilters(filters, typedAs)
	if err == nil {
		t.Fatal("expected an error for a field the rows do not carry")
	}
	if !strings.Contains(err.Error(), "name=db-1") {
		t.Errorf("error should quote the flag as typed, got %q", err)
	}
	if strings.Contains(err.Error(), "--filter \"instance") {
		t.Errorf("error should not name the resolved label, got %q", err)
	}
}
