package dashboards

import (
	"io"

	"github.com/grafana/gcx/internal/resources"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Exported aliases for unexported types and functions, available to external
// test packages only (codec_test.go, provider_test.go, crud_test.go).

// FormatAgeForTest exposes the unexported formatAge function for table-driven tests.
//
//nolint:gochecknoglobals // test-only export; required to expose unexported function to dashboards_test package.
var FormatAgeForTest = formatAge

// NewDashboardTableCodecForTest constructs a dashboardTableCodec for testing.
func NewDashboardTableCodecForTest(wide bool, grafanaURL string) *dashboardTableCodec {
	return newDashboardTableCodec(wide, grafanaURL)
}

// DecodeManifestForTest exposes the unexported decodeManifest function for table-driven tests.
//
//nolint:gochecknoglobals // test-only export; required to expose unexported function to dashboards_test package.
var DecodeManifestForTest = decodeManifest

// ReadManifestForTest exposes the unexported readManifest function for table-driven tests.
//
//nolint:gochecknoglobals // test-only export; required to expose unexported function to dashboards_test package.
var ReadManifestForTest = readManifest

// WrapUpdateErrorForTest exposes the unexported wrapUpdateError function for
// table-driven tests.
//
//nolint:gochecknoglobals // test-only export; required to expose unexported function to dashboards_test package.
var WrapUpdateErrorForTest = wrapUpdateError

// DefaultDashboardListLimitForTest exposes the list command's default page size.
const DefaultDashboardListLimitForTest = defaultDashboardListLimit

// NewListOptsForTest constructs listOpts with flags bound for validation tests.
func NewListOptsForTest(flags *pflag.FlagSet) *listOpts {
	opts := &listOpts{}
	opts.setup(flags)
	return opts
}

// EmitListPaginationHintForTest exposes the pagination hint helper.
func EmitListPaginationHintForTest(w io.Writer, argv []string, list *unstructured.UnstructuredList, opts *listOpts) {
	emitListPaginationHint(w, argv, list, opts)
}

// NewTestCreateCommand exposes the create command with an injected mutation
// client and descriptor, bypassing real K8s discovery.
func NewTestCreateCommand(client DashboardMutationClient, desc resources.Descriptor) *cobra.Command {
	return newCreateCommandWithDeps(&mutationDeps{client: client, desc: desc})
}

// NewTestUpdateCommand exposes the update command with an injected mutation
// client and descriptor, bypassing real K8s discovery.
func NewTestUpdateCommand(client DashboardMutationClient, desc resources.Descriptor) *cobra.Command {
	return newUpdateCommandWithDeps(&mutationDeps{client: client, desc: desc})
}

// NewTestDeleteCommand exposes the delete command with an injected mutation
// client and descriptor, bypassing real K8s discovery.
func NewTestDeleteCommand(client DashboardMutationClient, desc resources.Descriptor) *cobra.Command {
	return newDeleteCommandWithDeps(&mutationDeps{client: client, desc: desc})
}
