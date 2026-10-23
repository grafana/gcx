package search

import (
	"strings"

	cmdio "github.com/grafana/gcx/internal/output"
)

func searchTable() cmdio.TableAdapter[*DashboardSearchResultList, DashboardHit] {
	return cmdio.TableAdapter[*DashboardSearchResultList, DashboardHit]{
		Table: cmdio.Table[DashboardHit]{Columns: []cmdio.Column[DashboardHit]{
			{Header: "NAME", Content: func(h DashboardHit) string { return h.Metadata.Name }},
			{Header: "TITLE", Content: func(h DashboardHit) string { return h.Spec.Title }},
			{Header: "FOLDER", Content: func(h DashboardHit) string {
				if h.Spec.Folder == "" {
					return "General"
				}
				return h.Spec.Folder
			}},
			{Header: "TAGS", Content: func(h DashboardHit) string { return strings.Join(h.Spec.Tags, ", ") }},
			// Search hits carry no per-hit timestamp.
			{Header: "AGE", Content: func(DashboardHit) string { return "-" }},
		}},
		Rows: func(p *DashboardSearchResultList) ([]DashboardHit, error) { return p.Items, nil },
	}
}
