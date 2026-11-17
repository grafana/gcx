package stacks

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
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

// createdStackSummary is the concise result of successful stack creation.
type createdStackSummary struct {
	Name    string `json:"name" yaml:"name"`
	OrgSlug string `json:"orgSlug" yaml:"orgSlug"`
	Slug    string `json:"slug" yaml:"slug"`
	Status  string `json:"status" yaml:"status"`
	URL     string `json:"url" yaml:"url"`
}

// stackTableCodec retains the create/update dry-run preview alongside their table receipts.
type stackTableCodec struct{}

func (c *stackTableCodec) Format() format.Format { return cmdio.FormatTable }

func (c *stackTableCodec) Encode(w io.Writer, v any) error {
	if p, ok := v.(dryRunPreview); ok {
		dryRunSummary(w, p.Method, p.Endpoint, p.Request)
		return nil
	}

	if s, ok := v.(createdStackSummary); ok {
		return createdStackTable().Codec(cmdio.FormatTable).Encode(w, s)
	}
	if s, ok := v.(cloud.StackInfo); ok {
		return stackGetTable().Codec(cmdio.FormatTable).Encode(w, s)
	}
	return fmt.Errorf("invalid data type for table codec: expected dryRunPreview, createdStackSummary or cloud.StackInfo, got %T", v)
}

func (c *stackTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

func stackTable() cmdio.Table[cloud.StackInfo] {
	return cmdio.Table[cloud.StackInfo]{Columns: []cmdio.Column[cloud.StackInfo]{
		{Header: "SLUG", Content: func(s cloud.StackInfo) string { return s.Slug }},
		{Header: "NAME", Content: func(s cloud.StackInfo) string { return s.Name }},
		{Header: "STATUS", Content: func(s cloud.StackInfo) string { return s.Status }},
		{Header: "REGION", Content: func(s cloud.StackInfo) string { return s.RegionSlug }},
		{Header: "URL", Content: func(s cloud.StackInfo) string { return s.URL }},
		{Header: "PLAN", Visible: cmdio.WideOnly, Content: func(s cloud.StackInfo) string { return s.PlanName }},
		{Header: "DELETE-PROTECTION", Visible: cmdio.WideOnly, Content: func(s cloud.StackInfo) string { return strconv.FormatBool(s.DeleteProtection) }},
		{Header: "CREATED", Visible: cmdio.WideOnly, Content: func(s cloud.StackInfo) string { return s.CreatedAt[:min(len(s.CreatedAt), 10)] }},
	}}
}

func stackGetTable() cmdio.TableAdapter[cloud.StackInfo, cloud.StackInfo] {
	return cmdio.TableAdapter[cloud.StackInfo, cloud.StackInfo]{Table: stackTable(), Rows: func(s cloud.StackInfo) ([]cloud.StackInfo, error) { return []cloud.StackInfo{s}, nil }}
}

func createdStackTable() cmdio.TableAdapter[createdStackSummary, createdStackSummary] {
	return cmdio.TableAdapter[createdStackSummary, createdStackSummary]{
		Table: cmdio.Table[createdStackSummary]{Columns: []cmdio.Column[createdStackSummary]{
			{Header: "NAME", Content: func(s createdStackSummary) string { return s.Name }},
			{Header: "ORGSLUG", Content: func(s createdStackSummary) string { return s.OrgSlug }},
			{Header: "SLUG", Content: func(s createdStackSummary) string { return s.Slug }},
			{Header: "STATUS", Content: func(s createdStackSummary) string { return s.Status }},
			{Header: "URL", Content: func(s createdStackSummary) string { return s.URL }},
		}},
		Rows: func(s createdStackSummary) ([]createdStackSummary, error) { return []createdStackSummary{s}, nil },
	}
}

func regionTable() cmdio.Table[cloud.Region] {
	return cmdio.Table[cloud.Region]{Columns: []cmdio.Column[cloud.Region]{
		{Header: "SLUG", Content: func(r cloud.Region) string { return r.Slug }},
		{Header: "NAME", Content: func(r cloud.Region) string { return r.Name }},
		{Header: "DESCRIPTION", Content: func(r cloud.Region) string { return r.Description }},
		{Header: "PROVIDER", Content: func(r cloud.Region) string { return r.Provider }},
	}}
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
