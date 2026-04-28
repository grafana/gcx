package login_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/grafana/gcx/internal/cloud"
	intgrafana "github.com/grafana/gcx/internal/grafana"
	"github.com/grafana/gcx/internal/login"
)

func TestHealthCheckError(t *testing.T) {
	cause := errors.New("connection refused")
	e := &login.HealthCheckError{Server: "https://example.grafana.net", Status: 401, Cause: cause}

	if !strings.Contains(e.Error(), "health check failed") {
		t.Errorf("Error() must contain step name; got %q", e.Error())
	}
	if !errors.Is(e, cause) {
		t.Error("HealthCheckError must unwrap to its cause")
	}
}

func TestK8sDiscoveryError(t *testing.T) {
	cause := errors.New("connection refused")
	e := &login.K8sDiscoveryError{Server: "https://example.grafana.net", Cause: cause}

	if !strings.Contains(e.Error(), "kubernetes API unavailable") {
		t.Errorf("Error() must contain step name; got %q", e.Error())
	}
	if !errors.Is(e, cause) {
		t.Error("K8sDiscoveryError must unwrap to its cause")
	}
}

func TestVersionCheckError(t *testing.T) {
	v, _ := semver.NewVersion("11.5.0")
	cause := &intgrafana.VersionIncompatibleError{Version: v}
	e := &login.VersionCheckError{Cause: cause}

	if !strings.Contains(e.Error(), "version check failed") {
		t.Errorf("Error() must contain step name; got %q", e.Error())
	}

	var vErr *intgrafana.VersionIncompatibleError
	if !errors.As(e, &vErr) {
		t.Error("VersionCheckError must allow errors.As to *VersionIncompatibleError")
	}
}

func TestGCOMStackError(t *testing.T) {
	cause := &cloud.GCOMHTTPError{Status: 403, Body: "forbidden"}
	e := &login.GCOMStackError{Slug: "mystack", Status: 403, Cause: cause}

	if !strings.Contains(e.Error(), "GCOM check failed") {
		t.Errorf("Error() must contain step name; got %q", e.Error())
	}

	var httpErr *cloud.GCOMHTTPError
	if !errors.As(e, &httpErr) {
		t.Error("GCOMStackError must allow errors.As to *cloud.GCOMHTTPError")
	}
	if httpErr.Status != 403 {
		t.Errorf("Status: got %d, want 403", httpErr.Status)
	}
}
