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

// stackTableCodec renders []cloud.StackV1 as a table.
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
	stacks, ok := v.([]cloud.StackV1)
	if !ok {
		if s, ok := v.(cloud.StackV1); ok {
			stacks = []cloud.StackV1{s}
		} else {
			return errors.New("invalid data type for table codec: expected []cloud.StackV1 or cloud.StackV1")
		}
	}

	var tbl *style.TableBuilder
	if c.Wide {
		tbl = style.NewTable("SLUG", "NAME", "REGION", "URL", "ORG", "DELETE-PROTECTION", "ID")
	} else {
		tbl = style.NewTable("SLUG", "NAME", "REGION", "URL")
	}

	for _, s := range stacks {
		if c.Wide {
			dp := "false"
			if s.DeleteProtection {
				dp = "true"
			}
			tbl.Row(s.Slug, s.Name, s.Region, s.URL, s.OrgSlug, dp, fmt.Sprintf("%d", s.ID))
		} else {
			tbl.Row(s.Slug, s.Name, s.Region, s.URL)
		}
	}

	return tbl.Render(w)
}

func (c *stackTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// regionTableCodec renders []cloud.RegionV1 as a table.
type regionTableCodec struct{}

func (c *regionTableCodec) Format() format.Format { return "table" }

func (c *regionTableCodec) Encode(w io.Writer, v any) error {
	regions, ok := v.([]cloud.RegionV1)
	if !ok {
		return errors.New("invalid data type for table codec: expected []cloud.RegionV1")
	}

	tbl := style.NewTable("SLUG", "NAME", "DESCRIPTION", "PROVIDER", "VISIBILITY")
	for _, r := range regions {
		tbl.Row(r.Slug, r.Name, deref(r.Description), r.Provider, r.Visibility)
	}
	return tbl.Render(w)
}

// deref returns the pointed-to string, or empty string when nil.
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
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
