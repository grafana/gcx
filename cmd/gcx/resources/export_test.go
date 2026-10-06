package resources

import (
	"io"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources/remote"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Exported aliases for unexported types, available to external test packages only.

// TableCodecForTest wraps tableCodec for use in _test packages.
type TableCodecForTest = tableCodec

// NewTableCodecForTest creates a tableCodec with the given wide setting.
func NewTableCodecForTest(wide bool) *tableCodec {
	return &tableCodec{wide: wide}
}

// TabCodecForTest wraps tabCodec for use in _test packages.
type TabCodecForTest = tabCodec

// NewPullOptsForTest constructs pullOpts with flags bound for format tests.
func NewPullOptsForTest(flags *pflag.FlagSet) *pullOpts {
	opts := &pullOpts{}
	opts.setup(flags)
	return opts
}

// NewEditOptsForTest constructs editOpts with flags bound for format tests.
func NewEditOptsForTest(flags *pflag.FlagSet) *editOpts {
	opts := &editOpts{}
	opts.setup(flags)
	return opts
}

// NewGetOptsForTest constructs getOpts with flags bound for output-path tests.
func NewGetOptsForTest(flags *pflag.FlagSet) *getOpts {
	opts := &getOpts{}
	opts.setup(flags)
	return opts
}

// WriteGetOutputForTest exposes the unexported writeGetOutput (the RunE
// output tail: encode + truncation hint; the partial-failure error fires on
// the non-field-select path, while the --json field-select path delegates
// partial-failure handling to writeFieldSelect).
func WriteGetOutputForTest(stdout, stderr io.Writer, opts *getOpts, res *FetchResponse, output unstructured.UnstructuredList) error {
	return writeGetOutput(stdout, stderr, opts, res, output)
}

// BatchMutationForTest exposes the shared mutation conversion for wire tests.
func BatchMutationForTest(action string, summary *remote.OperationSummary, dryRun bool) cmdio.BatchMutation {
	return batchMutationFromSummary(action, summary, dryRun)
}

// NewPushOptsForTest binds the real push options for presentation tests.
func NewPushOptsForTest(flags *pflag.FlagSet) *pushOpts {
	opts := &pushOpts{}
	opts.setup(flags)
	return opts
}

// PushMutationForTest exposes the actual push presentation path.
func PushMutationForTest(opts *pushOpts, summary *remote.OperationSummary) cmdio.BatchMutation {
	return opts.mutationResult(summary)
}
