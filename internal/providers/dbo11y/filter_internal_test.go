package dbo11y

import (
	"net/url"
	"reflect"
	"strings"
	"testing"
)

// asPairs renders parsed filters as `label<op>value`, which is what the
// assertions below compare against.
func asPairs(filters []filter) []string {
	out := make([]string, 0, len(filters))
	for _, f := range filters {
		out = append(out, f.label+string(f.op)+f.value)
	}
	return out
}

func TestParseFilters(t *testing.T) {
	cases := []struct {
		name string
		raw  []string
		want []string
	}{
		{"none", nil, []string{}},
		{"one pair", []string{"engine=postgres"}, []string{"engine=postgres"}},
		{
			"several are ANDed",
			[]string{"engine=postgres", "provider_name=aws"},
			[]string{"engine=postgres", "provider_name=aws"},
		},
		{"quoted values are unwrapped", []string{`engine="postgres"`}, []string{"engine=postgres"}},
		{
			"a value may contain = itself",
			[]string{"dsn=user:pw@tcp(host:3306)/db?a=b"},
			[]string{"dsn=user:pw@tcp(host:3306)/db?a=b"},
		},
		// The API matches labels on the inventory metric, so a label the
		// response never returns still filters.
		{"a label the response omits", []string{"cluster=dev-eu-west-2"}, []string{"cluster=dev-eu-west-2"}},
		// Cutting at the FIRST operator means a value containing one is part
		// of the value, not a second comparison.
		{"a value containing !=", []string{"dsn=a!=b"}, []string{"dsn=a!=b"}},
		{"a value containing =~", []string{"dsn=a=~b"}, []string{"dsn=a=~b"}},

		{"not equal", []string{"engine!=mysql"}, []string{"engine!=mysql"}},
		{"regex", []string{"engine=~post.*"}, []string{"engine=~post.*"}},
		{"negated regex", []string{"engine!~post.*"}, []string{"engine!~post.*"}},
		// Operators stack on one label, where exact matches cannot: each is
		// evaluated here rather than sent as a single parameter.
		{
			"several operators on one label",
			[]string{"engine!=mysql", "engine!=sql_server"},
			[]string{"engine!=mysql", "engine!=sql_server"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseFilters(c.raw)
			if err != nil {
				t.Fatalf("parseFilters(%v) err = %v", c.raw, err)
			}
			if !reflect.DeepEqual(asPairs(got), c.want) {
				t.Errorf("parseFilters(%v) = %v, want %v", c.raw, asPairs(got), c.want)
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
		{"name!~db-.*", "instance", "db-.*"},
	}
	for _, c := range cases {
		t.Run(c.raw, func(t *testing.T) {
			got, err := parseFilters([]string{c.raw})
			if err != nil {
				t.Fatalf("parseFilters(%q) err = %v", c.raw, err)
			}
			if len(got) != 1 || got[0].label != c.label || got[0].value != c.value {
				t.Errorf("parsed = %+v, want label %q value %q", got, c.label, c.value)
			}
		})
	}
}

func TestParseFilters_Rejects(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"no operator", "engine", "expected label=value"},
		// Both of the API's silent-drop cases, which would otherwise return
		// the unfiltered fleet.
		{"empty value", "engine=", "a value is required"},
		{"invalid label name", "engine-x=postgres", "Prometheus label name"},
		// A regex that does not compile has to fail here, not match nothing.
		{"unparseable regex", "engine=~post(", "error parsing regexp"},
		{"datasource is not a label", "datasource=abc", "use -d/--datasource"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := parseFilters([]string{c.raw})
			if err == nil {
				t.Fatalf("parseFilters(%q) = nil error", c.raw)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %q, want it to mention %q", err, c.want)
			}
		})
	}
}

func TestParseFilters_RejectsConflictingExactMatches(t *testing.T) {
	// Two exact matches on one label cannot both hold, and the API keeps the
	// first and drops the rest without saying so.
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
			_, err := parseFilters(c.raw)
			if err == nil {
				t.Fatalf("parseFilters(%v) = nil error", c.raw)
			}
			msg := err.Error()
			if !strings.Contains(msg, "conflicting") || !strings.Contains(msg, c.want) {
				t.Errorf("error = %q, want it to name the conflict on %q", msg, c.want)
			}
			for _, f := range c.raw {
				if !strings.Contains(msg, f) {
					t.Errorf("error = %q, want it to quote %q", msg, f)
				}
			}
		})
	}

	if _, err := parseFilters([]string{"engine=mysql", "engine=mysql"}); err == nil {
		t.Error("expected a repeated identical filter to be rejected too")
	}

	// Mixing an exact match with an operator on one label is satisfiable, so
	// it is allowed where two exact matches are not.
	if _, err := parseFilters([]string{"engine=postgres", "engine!~my.*"}); err != nil {
		t.Errorf("an exact match alongside an operator should be allowed, got %v", err)
	}
}

func TestAPIFilters_SendsOnlyWhatTheAPICanApply(t *testing.T) {
	filters, err := parseFilters([]string{"engine=postgres", "cluster!=dev", "provider_name=~aws|gcp"})
	if err != nil {
		t.Fatalf("parseFilters() err = %v", err)
	}
	// Only the exact match travels. Sending `cluster` with a bare value would
	// match the literal string "!=dev", and the API has no regex at all.
	want := url.Values{"engine": {"postgres"}}
	if got := apiFilters(filters); !reflect.DeepEqual(got, want) {
		t.Errorf("apiFilters() = %v, want %v", got, want)
	}
	// The two operators are what is left for the client-side pass.
	if remaining := clientFilters(filters); len(remaining) != 2 {
		t.Errorf("clientFilters() kept %d filters, want the 2 operators", len(remaining))
	}

	exactOnly, err := parseFilters([]string{"engine=postgres"})
	if err != nil {
		t.Fatalf("parseFilters() err = %v", err)
	}
	if remaining := clientFilters(exactOnly); len(remaining) != 0 {
		t.Errorf("clientFilters() kept %d filters, want none: exact matches need no client-side pass", len(remaining))
	}
}

func TestFilterInstances(t *testing.T) {
	items := []Instance{
		{Name: "pg-1", Host: "pg-1", Engine: "postgres", Labels: map[string]string{"cluster": "dev-eu"}},
		{Name: "pg-2", Host: "pg-2", Engine: "postgres", Labels: map[string]string{"cluster": "prod-eu"}},
		{Name: "my-1", Host: "my-1", Engine: "mysql", Labels: map[string]string{"cluster": "prod-us"}},
		{Name: "ms-1", Host: "ms-1", Engine: "sql_server"},
	}
	names := func(in []Instance) []string {
		out := make([]string, 0, len(in))
		for _, i := range in {
			out = append(out, i.Name)
		}
		return out
	}

	cases := map[string]struct {
		raw  []string
		want []string
	}{
		"no filters returns everything": {nil, []string{"pg-1", "pg-2", "my-1", "ms-1"}},
		"exact is left to the API":      {[]string{"engine=postgres"}, []string{"pg-1", "pg-2", "my-1", "ms-1"}},
		// `dsn` is one of the labels the API filters on but withholds from the
		// response. Re-applying it here would compare every row against the
		// empty string and discard the whole fleet the API just matched.
		"exact on a withheld label keeps the API's rows": {[]string{"dsn=postgres://x"}, []string{"pg-1", "pg-2", "my-1", "ms-1"}},
		"not equal excludes":                             {[]string{"engine!=postgres"}, []string{"my-1", "ms-1"}},
		"regex matches whole value":                      {[]string{"engine=~post.*"}, []string{"pg-1", "pg-2"}},
		"regex alternation":                              {[]string{"engine=~postgres|mysql"}, []string{"pg-1", "pg-2", "my-1"}},
		"negated regex":                                  {[]string{"engine!~post.*"}, []string{"my-1", "ms-1"}},
		"a collector label":                              {[]string{"cluster=~prod-.*"}, []string{"pg-2", "my-1"}},
		"operators stack on one label":                   {[]string{"engine!=postgres", "engine!=mysql"}, []string{"ms-1"}},
		"operators across labels are AND":                {[]string{"engine!=mysql", "cluster=~.*-eu"}, []string{"pg-1", "pg-2"}},
		// Prometheus semantics: an absent label compares as the empty string,
		// so a negation holds for a row that does not carry it at all.
		"an absent label is empty, so != holds": {[]string{"cluster!=prod-eu"}, []string{"pg-1", "my-1", "ms-1"}},
		"an absent label is empty, so =~ fails": {[]string{"cluster=~.+"}, []string{"pg-1", "pg-2", "my-1"}},
		// `name` and `host` both resolve to the instance label.
		"name is aliased onto instance": {[]string{"name=~pg-.*"}, []string{"pg-1", "pg-2"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			filters, err := parseFilters(c.raw)
			if err != nil {
				t.Fatalf("parseFilters(%v) err = %v", c.raw, err)
			}
			if got := names(filterInstances(items, filters)); !reflect.DeepEqual(got, c.want) {
				t.Errorf("filterInstances(%v) = %v, want %v", c.raw, got, c.want)
			}
		})
	}
}

func TestValidateTopQueryFilters(t *testing.T) {
	// The queries route ignores label filters, so `instances get` applies them
	// to the fetched rows and can only honour the fields they carry.
	for _, label := range []string{"database", "datname", "schema", "query_id", "digest"} {
		filters, err := parseFilters([]string{label + "=x"})
		if err != nil {
			t.Fatalf("parseFilters() err = %v", err)
		}
		if err := validateTopQueryFilters(filters); err != nil {
			t.Errorf("%s should be filterable, got %v", label, err)
		}
	}

	filters, err := parseFilters([]string{"engine=postgres"})
	if err != nil {
		t.Fatalf("parseFilters() err = %v", err)
	}
	err = validateTopQueryFilters(filters)
	if err == nil {
		t.Fatal("expected an error for a field the rows do not carry")
	}
	if !strings.Contains(err.Error(), "datname") {
		t.Errorf("error should list what is filterable, got %q", err)
	}
}

func TestValidateTopQueryFilters_NamesTheTypedFlag(t *testing.T) {
	// `name` resolves to `instance`; the error has to say `name`, which is
	// what the user passed.
	filters, err := parseFilters([]string{"name=db-1"})
	if err != nil {
		t.Fatalf("parseFilters() err = %v", err)
	}
	err = validateTopQueryFilters(filters)
	if err == nil {
		t.Fatal("expected an error for a field the rows do not carry")
	}
	if !strings.Contains(err.Error(), "name=db-1") {
		t.Errorf("error should quote the flag as typed, got %q", err)
	}
	if strings.Contains(err.Error(), `--filter "instance`) {
		t.Errorf("error should not name the resolved label, got %q", err)
	}
}

func TestFilterTopQueries(t *testing.T) {
	items := []TopQuery{
		{QueryID: "1", Datname: "payments"},
		{QueryID: "2", Datname: "orders"},
		{QueryID: "3", Datname: "payments_archive"},
	}
	ids := func(in []TopQuery) []string {
		out := make([]string, 0, len(in))
		for _, q := range in {
			out = append(out, q.QueryID)
		}
		return out
	}

	cases := map[string]struct {
		raw  []string
		want []string
	}{
		"no filters":    {nil, []string{"1", "2", "3"}},
		"exact":         {[]string{"datname=payments"}, []string{"1"}},
		"not equal":     {[]string{"datname!=payments"}, []string{"2", "3"}},
		"regex":         {[]string{"datname=~payments.*"}, []string{"1", "3"}},
		"negated regex": {[]string{"datname!~payments.*"}, []string{"2"}},
		"by query id":   {[]string{"query_id=2"}, []string{"2"}},
		// Every filter is applied here, including exact ones: the queries
		// route takes none of them.
		"exact and operator": {[]string{"datname=~payments.*", "query_id!=3"}, []string{"1"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			filters, err := parseFilters(c.raw)
			if err != nil {
				t.Fatalf("parseFilters(%v) err = %v", c.raw, err)
			}
			if got := ids(filterTopQueries(items, filters)); !reflect.DeepEqual(got, c.want) {
				t.Errorf("filterTopQueries(%v) = %v, want %v", c.raw, got, c.want)
			}
		})
	}
}
