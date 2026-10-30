package resources

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources/search"
)

func searchTable() cmdio.TableAdapter[*search.Results, search.Item] {
	return cmdio.TableAdapter[*search.Results, search.Item]{
		Table: cmdio.Table[search.Item]{
			Columns: []cmdio.Column[search.Item]{
				{Header: "TYPE", Content: func(item search.Item) string {
					return safeSearchCell(item.Resource.Resource + "." + item.Resource.Group)
				}},
				{Header: "NAME", Content: func(item search.Item) string { return safeSearchCell(item.Resource.Name) }},
				{Header: "TITLE", Content: func(item search.Item) string { return safeSearchCell(item.Title) }},
				{Header: "FOLDER", Content: func(item search.Item) string {
					if item.FolderTitle != "" {
						return safeSearchCell(item.FolderTitle)
					}
					if item.Folder == "" || item.Folder == "general" {
						return "General"
					}
					return safeSearchCell(item.Folder)
				}},
				{Header: "SCORE", Content: func(item search.Item) string { return strconv.FormatFloat(item.Score, 'f', 4, 64) }},
				{Header: "EXCERPT", Visible: cmdio.WideOnly, Content: func(item search.Item) string {
					if len(item.Chunks) == 0 {
						return ""
					}
					return safeSearchCell(strings.Join(strings.Fields(item.Chunks[0].Content), " "))
				}},
				{Header: "SUBRESOURCE", Visible: cmdio.WideOnly, Content: func(item search.Item) string {
					if len(item.Chunks) == 0 {
						return ""
					}
					return safeSearchCell(item.Chunks[0].Subresource)
				}},
			},
			Empty: func(w io.Writer) error { _, err := fmt.Fprintln(w, "No resources found"); return err },
		},
		Rows: func(result *search.Results) ([]search.Item, error) { return result.Items, nil },
	}
}

func safeSearchCell(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
}
