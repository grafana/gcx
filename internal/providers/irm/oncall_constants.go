package irm

const (
	// APIGroup is the API group for all OnCall resources.
	APIGroup = "oncall.ext.grafana.app"
	// APIVersion is the full API version string.
	APIVersion = APIGroup + "/v1alpha1"
	// Version is the API version.
	Version = "v1alpha1"
)

//nolint:gochecknoglobals // constant-like slice shared by commands and adapter registrations
var defaultStripFields = []string{"id", "pk", "password", "authorization_header"}
