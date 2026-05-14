package dashboards

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
