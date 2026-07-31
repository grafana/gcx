package check

import (
	"errors"
	"io"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/style"
	otelutils "github.com/grafana/otel-checker/checks/utils"
)

// CheckTableCodec renders otelutils.Results as a grouped status/component/
// message table.
//
// Default columns: STATUS COMPONENT MESSAGE EXPLAIN_ID
// Wide adds no extra columns today; the flag is reserved for future use
// (e.g. raw severity codes) and accepted to satisfy the gcx output
// convention of having distinct table/wide codecs.
//
// EXPLAIN_ID is left empty for findings that do not carry one (typically
// successful checks). Pass a non-empty ID to `gcx instrumentation explain
// <id>` to see the full explanation.
type CheckTableCodec struct {
	Wide bool
}

var _ format.Codec = (*CheckTableCodec)(nil)

func (c *CheckTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *CheckTableCodec) Encode(w io.Writer, v any) error {
	results, ok := v.(otelutils.Results)
	if !ok {
		return errCheckTableCodecExpectedResults
	}

	t := style.NewTable("STATUS", "COMPONENT", "MESSAGE", "EXPLAIN_ID")
	for _, r := range results.Errors {
		t.Row("FAIL", r.Component, r.Message, r.ExplainID)
	}
	for _, r := range results.Warnings {
		t.Row("WARN", r.Component, r.Message, r.ExplainID)
	}
	for _, r := range results.Checks {
		t.Row("OK", r.Component, r.Message, r.ExplainID)
	}
	return t.Render(w)
}

func (c *CheckTableCodec) Decode(_ io.Reader, _ any) error {
	return errCheckTableCodecNoDecode
}

var (
	errCheckTableCodecExpectedResults = errors.New("CheckTableCodec: expected otelutils.Results")
	errCheckTableCodecNoDecode        = errors.New("table format does not support decoding")
)
