package stacks

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/style"
)

// dryRunPreviewType is the collision-resistant discriminator carried by
// dryRunPreview so machine consumers can dispatch on the shape without
// heuristics (same convention as the shared cmdio mutation result family).
const dryRunPreviewType = "gcx.stacks.dry_run"

// dryRunPreview is the finite result of a create/update --dry-run: the exact
// request that would have been sent. It flows through the codec system like
// every other result — the default table codec renders the familiar human
// preview byte-identically, while agent mode and explicit -o json/yaml get a
// single structured document. The shape is bespoke (not cmdio.SingleMutation)
// because the load-bearing content is the request body plus the HTTP call
// that would be made, which the shared family does not carry.
type dryRunPreview struct {
	Type          string `json:"type" yaml:"type"`
	SchemaVersion string `json:"schema_version" yaml:"schema_version"`
	Action        string `json:"action" yaml:"action"`
	Method        string `json:"method" yaml:"method"`
	Endpoint      string `json:"endpoint" yaml:"endpoint"`
	DryRun        bool   `json:"dry_run" yaml:"dry_run"`
	Request       any    `json:"request" yaml:"request"`
}

// newDryRunPreview returns a dryRunPreview with the discriminators set.
func newDryRunPreview(action, method, endpoint string, request any) dryRunPreview {
	return dryRunPreview{
		Type:          dryRunPreviewType,
		SchemaVersion: "1",
		Action:        action,
		Method:        method,
		Endpoint:      endpoint,
		DryRun:        true,
		Request:       request,
	}
}

// stackTableCodec renders []cloud.StackInfo as a table. It also renders
// dryRunPreview values (the --dry-run result of create/update) as the
// classic human preview, because "table" is those commands' default format.
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
	if p, ok := v.(dryRunPreview); ok {
		dryRunSummary(w, p.Method, p.Endpoint, p.Request)
		return nil
	}

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

// deleteTextCodec is the human "text" codec for the SingleMutation result of
// stacks delete: it renders exactly the lines the command has always printed
// (the two-line --dry-run preview, or the styled success line), so default
// human stdout stays byte-identical to the pre-codec output.
type deleteTextCodec struct{}

func (c *deleteTextCodec) Format() format.Format { return "text" }

func (c *deleteTextCodec) Encode(w io.Writer, v any) error {
	m, ok := v.(cmdio.SingleMutation)
	if !ok {
		return errors.New("invalid data type for text codec: expected SingleMutation")
	}

	if m.DryRun {
		fmt.Fprintf(w, "Dry run: DELETE %s/%s\n", instancesPath, m.Target.Name)
		fmt.Fprintf(w, "\nStack %q would be permanently deleted. No changes were made.\n", m.Target.Name)
		return nil
	}

	cmdio.Success(w, "Stack %q deleted successfully.", m.Target.Name)
	return nil
}

func (c *deleteTextCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("text format does not support decoding")
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
