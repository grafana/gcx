package search

import (
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/style"
)

// searchTableCodec renders *DashboardSearchResultList as a human-readable table.
//
// Lexical columns: NAME TITLE FOLDER TAGS AGE. Hybrid columns: NAME TITLE FOLDER SCORE.
// Wide hybrid output also includes the best matching chunk.
// The AGE column always renders as "-" because search hits carry no per-hit timestamp.
type searchTableCodec struct {
	Wide bool
}

// Format implements format.Codec.
func (c *searchTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

// Decode is a no-op: table format is display-only.
func (c *searchTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// Encode writes the search results table to w.
// Accepts *DashboardSearchResultList.
func (c *searchTableCodec) Encode(w io.Writer, v any) error {
	list, ok := v.(*DashboardSearchResultList)
	if !ok {
		return errors.New("searchTableCodec: expected *DashboardSearchResultList")
	}

	columns := []string{"NAME", "TITLE", "FOLDER", "TAGS", "AGE"}
	if list.Limit > 0 {
		columns = []string{"NAME", "TITLE", "FOLDER", "SCORE"}
		if c.Wide {
			columns = append(columns, "MATCH")
		}
	}
	t := style.NewTable(columns...).MultilineCells(list.Limit > 0 && c.Wide)

	for _, hit := range list.Items {
		name := hit.Metadata.Name
		title := hit.Spec.Title
		folder := hit.Spec.Folder
		if folder == "" {
			folder = "General"
		}
		if list.Limit > 0 {
			score := "-"
			if hit.Spec.Score != nil {
				score = strconv.FormatFloat(*hit.Spec.Score, 'g', 4, 64)
			}
			row := []string{name, title, folder, score}
			if c.Wide {
				match := ""
				if len(hit.Spec.Chunks) > 0 {
					match = hit.Spec.Chunks[0].Content
				}
				row = append(row, match)
			}
			t.Row(row...)
			continue
		}
		tags := strings.Join(hit.Spec.Tags, ", ")
		const age = "-" // Search hits carry no per-hit timestamp.

		t.Row(name, title, folder, tags, age)
	}

	return t.Render(w)
}
