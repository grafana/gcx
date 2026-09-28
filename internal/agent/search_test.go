package agent_test

import (
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/stretchr/testify/require"
)

func TestSearchCommands(t *testing.T) {
	documents := []agent.SearchDocument{
		{Path: "gcx checks create", Aliases: "synthetics", Summary: "Create an uptime check", Terms: "monitor websites"},
		{Path: "gcx checks list", Summary: "List checks"},
		{Path: "gcx resources pull", Summary: "Export resources", Terms: "export dashboards", Description: "Export dashboards to files"},
	}
	for _, tt := range []struct {
		name, query string
		want        []string
	}{
		{"natural language", "please create an uptime check", []string{"gcx checks create"}},
		{"exact path", "gcx checks list", []string{"gcx checks list"}},
		{"alias", "synthetics", []string{"gcx checks create"}},
		{"case and punctuation", "EXPORT: DASHBOARDS!", []string{"gcx resources pull"}},
		{"prefix", "dashb", []string{"gcx resources pull"}},
		{"substitution", "dashboarxs", []string{"gcx resources pull"}},
		{"insertion", "dashbboards", []string{"gcx resources pull"}},
		{"deletion", "dashbards", []string{"gcx resources pull"}},
		{"transposition", "dashbaords", []string{"gcx resources pull"}},
		{"intent terms", "websites", []string{"gcx checks create"}},
		{"unknown", "zzzxxyyqq", []string{}},
		{"filler", "how do i", []string{}},
		{"empty", "", []string{}},
	} {
		t.Run(tt.name, func(t *testing.T) { require.Equal(t, tt.want, agent.SearchCommands(documents, tt.query)) })
	}
}

func TestSearchRanking(t *testing.T) {
	for _, tt := range []struct {
		name, query string
		docs        []agent.SearchDocument
		want        []string
	}{
		{"primary coverage before supporting text", "export dashboards", []agent.SearchDocument{
			{Path: "gcx export", Description: "export dashboards"},
			{Path: "gcx resources pull", Summary: "Export dashboards"},
		}, []string{"gcx resources pull"}},
		{"long help cannot qualify", "printer", []agent.SearchDocument{{Path: "gcx widgets list", Description: "printer", Context: "printer"}}, []string{}},
		{"common action cannot qualify", "install a printer", []agent.SearchDocument{{Path: "gcx skills install", Description: "install printer"}}, []string{}},
		{"unknown subject counts against coverage", "create a Kubernetes deployment", []agent.SearchDocument{{Path: "gcx widgets create", Summary: "Create widgets"}}, []string{}},
		{"generic subject cannot qualify", "create JSON files", []agent.SearchDocument{{Path: "gcx widgets create", Summary: "Create JSON files"}}, []string{}},
		{"unique correction", "list cats", []agent.SearchDocument{{Path: "gcx bats list"}}, []string{"gcx bats list"}},
		{"ambiguous correction", "list cats", []agent.SearchDocument{{Path: "gcx bats list"}, {Path: "gcx hats list"}}, []string{}},
		{"exact term suppresses fuzzy", "list cats", []agent.SearchDocument{{Path: "gcx cats list"}, {Path: "gcx bats list"}}, []string{"gcx cats list"}},
		{"short typo is not corrected", "cts", []agent.SearchDocument{{Path: "gcx cats list"}}, []string{}},
		{"stable ties", "widgets", []agent.SearchDocument{{Path: "gcx widgets get"}, {Path: "gcx widgets add"}}, []string{"gcx widgets add", "gcx widgets get"}},
		{"unicode typo", "métrics", []agent.SearchDocument{{Path: "gcx metrics"}}, []string{"gcx metrics"}},
		{"compound datasource", "show data sources", []agent.SearchDocument{{Path: "gcx datasources list"}}, []string{"gcx datasources list"}},
		{"compound oncall", "list on call schedules", []agent.SearchDocument{{Path: "gcx oncall schedules list"}}, []string{"gcx oncall schedules list"}},
		{"explicit exclusion", "list widgets but do not delete them", []agent.SearchDocument{{Path: "gcx widgets list"}, {Path: "gcx widgets delete", Terms: "list widgets"}}, []string{"gcx widgets list"}},
		{"gerund exclusion", "list widgets without deleting", []agent.SearchDocument{{Path: "gcx widgets list"}, {Path: "gcx widgets delete", Terms: "list widgets"}}, []string{"gcx widgets list"}},
		{"contraction exclusion", "list widgets don't delete", []agent.SearchDocument{{Path: "gcx widgets list"}, {Path: "gcx widgets delete", Terms: "list widgets"}}, []string{"gcx widgets list"}},
		{"workflow uses distinct key", "investigate CPU", []agent.SearchDocument{{Path: "gcx skills get", Key: "cpu-guide", Workflow: true, Terms: "investigate CPU"}}, []string{"cpu-guide"}},
		{"workflow path cannot qualify", "skills get", []agent.SearchDocument{{Path: "gcx skills get", Key: "cpu-guide", Workflow: true, Terms: "investigate CPU"}}, []string{}},
		{"action prefix cannot qualify", "creat", []agent.SearchDocument{{Path: "gcx widgets create"}}, []string{}},
		{"read request excludes write", "show widgets", []agent.SearchDocument{{Path: "gcx widgets list"}, {Path: "gcx widgets delete", Terms: "show widgets"}}, []string{"gcx widgets list"}},
		{"compound whitespace and punctuation", "show data--sources", []agent.SearchDocument{{Path: "gcx datasources list"}}, []string{"gcx datasources list"}},
		{"compound cannot match inside word", "operation callback", []agent.SearchDocument{{Path: "gcx oncall list"}}, []string{}},

		{"canonical command before workflow", "widgets list", []agent.SearchDocument{{Path: "gcx widgets list"}, {Path: "gcx skills get", Key: "widget-guide", Workflow: true, Terms: "widgets list"}}, []string{"gcx widgets list", "widget-guide"}},
	} {
		t.Run(tt.name, func(t *testing.T) { require.Equal(t, tt.want, agent.SearchCommands(tt.docs, tt.query)) })
	}
}

func TestSearchRepeatedTextDoesNotImproveRanking(t *testing.T) {
	docs := []agent.SearchDocument{{Path: "gcx widgets get", Summary: "Get widgets"}, {Path: "gcx widgets create", Summary: "Create widgets"}}
	before := agent.SearchCommands(docs, "widgets")
	docs[0].Summary += " Get widgets Get widgets"
	require.Equal(t, before, agent.SearchCommands(docs, "widgets widgets")) //nolint:dupword // Deliberate repeated query terms.
}
