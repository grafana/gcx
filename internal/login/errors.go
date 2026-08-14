package login

import "fmt"

// HealthCheckError is returned when the Grafana /api/health probe fails.
// Status is the HTTP status code, or 0 for transport-level failures where no
// HTTP response was received (dial, TLS, timeout).
type HealthCheckError struct {
	Server string
	Status int
	Cause  error
}

func (e *HealthCheckError) Error() string {
	return fmt.Sprintf("connectivity validation: health check failed: %s", e.Cause)
}

func (e *HealthCheckError) Unwrap() error { return e.Cause }

// K8sDiscoveryError is returned when /apis discovery against the Grafana
// Kubernetes-compatible API fails.
type K8sDiscoveryError struct {
	Server string
	Cause  error
}

func (e *K8sDiscoveryError) Error() string {
	return fmt.Sprintf("connectivity validation: kubernetes API unavailable: %s", e.Cause)
}

func (e *K8sDiscoveryError) Unwrap() error { return e.Cause }

// VersionCheckError is returned when the Grafana version is below the
// supported floor. The wrapped Cause is typically a *grafana.VersionIncompatibleError.
type VersionCheckError struct {
	Cause error
}

func (e *VersionCheckError) Error() string {
	return fmt.Sprintf("connectivity validation: version check failed: %s", e.Cause)
}

func (e *VersionCheckError) Unwrap() error { return e.Cause }

// GCOMStackError is returned when the GCOM stack lookup fails during Cloud
// login. Status holds the HTTP status code when the cause wraps a
// *cloud.GCOMHTTPError (403 typically means the Cloud Access Policy is
// missing the stacks:read scope).
type GCOMStackError struct {
	Slug   string
	Status int
	Cause  error
}

func (e *GCOMStackError) Error() string {
	return fmt.Sprintf("connectivity validation: GCOM check failed: %s", e.Cause)
}

func (e *GCOMStackError) Unwrap() error { return e.Cause }

// PortalServerURLError is returned when the Grafana server URL names a Grafana
// Cloud portal root (grafana.com and its per-environment equivalents) instead
// of a Grafana stack. The portal manages stacks through the GCOM API and
// serves no Grafana instance API, so no authentication method can succeed
// against it.
//
// Login raises this before target detection, before any prompt, and before the
// OAuth browser opens. Without it the portal URL reaches the OAuth flow, which
// opens a plugin route that the portal does not have.
//
// StackSuffix is the stack URL suffix for the same environment (".grafana.net"
// for grafana.com), so the CLI can name the correct URL form.
type PortalServerURLError struct {
	Server      string
	Host        string
	StackSuffix string
}

func (e *PortalServerURLError) Error() string {
	return fmt.Sprintf("server %s resolves to the Grafana Cloud portal %s", e.Server, e.Host)
}
