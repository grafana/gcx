package agent_test

import (
	"reflect"
	"testing"

	"github.com/grafana/gcx/internal/agent"
)

func TestSearchCommands(t *testing.T) {
	documents := []agent.SearchDocument{
		{Path: "gcx checks create", Aliases: "synthetics", Summary: "Create an uptime check", Context: "monitor websites"},
		{Path: "gcx checks list", Summary: "List checks"},
		{Path: "gcx resources pull", Summary: "Export resources", Description: "Export dashboards to files"},
		{Path: "gcx checks list-history", Summary: "List checks history"},
	}
	for _, tt := range []struct {
		name, query string
		want        []string
	}{
		{"natural language", "please create an uptime check", []string{"gcx checks create", "gcx checks list", "gcx checks list-history"}},
		{"exact path", "gcx checks list", []string{"gcx checks list", "gcx checks list-history", "gcx checks create"}},
		{"alias", "synthetics", []string{"gcx checks create"}},
		{"case and punctuation", "EXPORT: DASHBOARDS!", []string{"gcx resources pull"}},
		{"prefix", "dashb", []string{"gcx resources pull"}},
		{"substitution", "dashboarxs", []string{"gcx resources pull"}},
		{"insertion", "dashbboards", []string{"gcx resources pull"}},
		{"deletion", "dashbards", []string{"gcx resources pull"}},
		{"context", "websites", []string{"gcx checks create"}},
		{"unknown", "zzzxxyyqq", []string{}},
		{"filler", "how do i", []string{}},
		{"empty", "", []string{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := agent.SearchCommands(documents, tt.query)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("SearchCommands(%q) = %v, want %v", tt.query, got, tt.want)
			}
		})
	}
}

func TestSearchRanking(t *testing.T) {
	for _, tt := range []struct {
		name, query string
		docs        []agent.SearchDocument
		want        []string
	}{
		{"coverage before weight", "export dashboards", []agent.SearchDocument{
			{Path: "gcx export"}, {Path: "gcx pull", Description: "export dashboards"},
		}, []string{"gcx pull", "gcx export"}},
		{"no fuzzy when exact exists", "alerts", []agent.SearchDocument{
			{Path: "gcx alert"}, {Path: "gcx alerts"},
		}, []string{"gcx alerts"}},
		{"no short fuzzy", "cats", []agent.SearchDocument{{Path: "gcx bats"}}, []string{}},
		{"stable ties and repeated terms", "query query", []agent.SearchDocument{ //nolint:dupword // Repeated terms must not affect ranking.
			{Path: "gcx z query"}, {Path: "gcx a query"},
		}, []string{"gcx a query", "gcx z query"}},
		{"unicode typo", "métrics", []agent.SearchDocument{{Path: "gcx metrics"}}, []string{"gcx metrics"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := agent.SearchCommands(tt.docs, tt.query); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
