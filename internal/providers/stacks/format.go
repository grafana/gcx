package stacks

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/style"
)

// stackTableCodec renders []cloud.StackInfo as a table.
type stackTableCodec struct {
	Wide bool
}

func (c *stackTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *stackTableCodec) Encode(w io.Writer, v any) error {
	stacks, ok := v.([]cloud.StackInfo)
	if !ok {
		if s, ok := v.(cloud.StackInfo); ok {
			stacks = []cloud.StackInfo{s}
		} else {
			return errors.New("invalid data type for table codec: expected []cloud.StackInfo or cloud.StackInfo")
		}
	}

	var tbl *style.TableBuilder
	if c.Wide {
		tbl = style.NewTable("SLUG", "NAME", "STATUS", "REGION", "URL", "PLAN", "DELETE-PROTECTION", "CREATED")
	} else {
		tbl = style.NewTable("SLUG", "NAME", "STATUS", "REGION", "URL")
	}

	for _, s := range stacks {
		if c.Wide {
			dp := "false"
			if s.DeleteProtection {
				dp = "true"
			}
			created := s.CreatedAt
			if len(created) > 10 {
				created = created[:10]
			}
			tbl.Row(s.Slug, s.Name, s.Status, s.RegionSlug, s.URL, s.PlanName, dp, created)
		} else {
			tbl.Row(s.Slug, s.Name, s.Status, s.RegionSlug, s.URL)
		}
	}

	return tbl.Render(w)
}

func (c *stackTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// regionTableCodec renders []cloud.Region as a table.
type regionTableCodec struct{}

func (c *regionTableCodec) Format() format.Format { return "table" }

func (c *regionTableCodec) Encode(w io.Writer, v any) error {
	regions, ok := v.([]cloud.Region)
	if !ok {
		return errors.New("invalid data type for table codec: expected []cloud.Region")
	}

	tbl := style.NewTable("SLUG", "NAME", "DESCRIPTION", "PROVIDER", "STATUS")
	for _, r := range regions {
		tbl.Row(r.Slug, r.Name, r.Description, r.Provider, r.Status)
	}
	return tbl.Render(w)
}

func (c *regionTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// dryRunSummary prints a human-readable dry-run preview.
func dryRunSummary(w io.Writer, method, endpoint string, body any) {
	fmt.Fprintf(w, "Dry run: %s %s\n", method, endpoint)
	if body != nil {
		fmt.Fprintln(w)
		codec := format.NewJSONCodec()
		_ = codec.Encode(w, body)
	}
}

// labelsFromFlag parses a slice of "key=value" strings into a map.
func labelsFromFlag(labels []string) (map[string]string, error) {
	if len(labels) == 0 {
		return nil, nil //nolint:nilnil // nil signals "no labels specified" so omitempty omits the field.
	}
	m := make(map[string]string, len(labels))
	for _, l := range labels {
		k, v, ok := strings.Cut(l, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("invalid label %q: must be in key=value format", l)
		}
		m[k] = v
	}
	return m, nil
}
