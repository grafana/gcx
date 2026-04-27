package search

import (
	"errors"
	"io"
	"strings"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/style"
)

// searchTableCodec renders *DashboardSearchResultList as a human-readable table.
//
// Columns (default): NAME  TITLE  FOLDER  TAGS  AGE
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

	t := style.NewTable("NAME", "TITLE", "FOLDER", "TAGS", "AGE")

	for _, hit := range list.Items {
		name := hit.Metadata.Name
		title := hit.Spec.Title
		folder := hit.Spec.Folder
		if folder == "" {
			folder = "General"
		}
		tags := strings.Join(hit.Spec.Tags, ", ")
		const age = "-" // Search hits carry no per-hit timestamp.

		t.Row(name, title, folder, tags, age)
	}

	return t.Render(w)
}
