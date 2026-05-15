package stacks

import (
	"bytes"
	"io"

	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

// NewTestListCommand creates a list command for external tests.
func NewTestListCommand() *cobra.Command { return newListCommand(&providers.ConfigLoader{}) }

// NewTestGetCommand creates a get command for external tests.
func NewTestGetCommand() *cobra.Command { return newGetCommand(&providers.ConfigLoader{}) }

// NewTestCreateCommand creates a create command for external tests.
func NewTestCreateCommand() *cobra.Command { return newCreateCommand(&providers.ConfigLoader{}) }

// NewTestUpdateCommand creates an update command for external tests.
func NewTestUpdateCommand() *cobra.Command { return newUpdateCommand(&providers.ConfigLoader{}) }

// NewTestDeleteCommand creates a delete command for external tests.
func NewTestDeleteCommand() *cobra.Command { return newDeleteCommand(&providers.ConfigLoader{}) }

// ExportLabelsFromFlag exposes labelsFromFlag for external tests.
func ExportLabelsFromFlag(labels []string) (map[string]string, error) { return labelsFromFlag(labels) }

// ExportDryRunSummary exposes dryRunSummary for external tests.
func ExportDryRunSummary(w io.Writer, method, endpoint string, body any) {
	dryRunSummary(w, method, endpoint, body)
}

// ExportStackTableCodec returns a stackTableCodec for external tests.
func ExportStackTableCodec(wide bool) interface {
	Encode(w io.Writer, v any) error
} {
	return &stackTableCodec{Wide: wide}
}

// ExportRegionTableCodec returns a regionTableCodec for external tests.
func ExportRegionTableCodec() interface {
	Encode(w io.Writer, v any) error
} {
	return &regionTableCodec{}
}

// ExportEncodeStackTable encodes stacks using the table codec and returns the output.
func ExportEncodeStackTable(stacks []cloud.StackInfo, wide bool) (string, error) {
	var buf bytes.Buffer
	err := (&stackTableCodec{Wide: wide}).Encode(&buf, stacks)
	return buf.String(), err
}

// ExportEncodeStackTableSingle encodes a single stack using the table codec.
func ExportEncodeStackTableSingle(stack cloud.StackInfo) (string, error) {
	var buf bytes.Buffer
	err := (&stackTableCodec{}).Encode(&buf, stack)
	return buf.String(), err
}

// ExportEncodeRegionTable encodes regions using the table codec and returns the output.
func ExportEncodeRegionTable(regions []cloud.Region) (string, error) {
	var buf bytes.Buffer
	err := (&regionTableCodec{}).Encode(&buf, regions)
	return buf.String(), err
}
