package framework_test

import (
	"errors"
	"testing"

	"github.com/grafana/gcx/internal/setup/framework"
)

func TestErrSetupNotSupported(t *testing.T) {
	if framework.ErrSetupNotSupported == nil {
		t.Fatal("ErrSetupNotSupported must not be nil")
	}
	if !errors.Is(framework.ErrSetupNotSupported, framework.ErrSetupNotSupported) {
		t.Fatal("errors.Is identity check failed")
	}
}

func TestProductStateConstants(t *testing.T) {
	cases := []struct {
		state framework.ProductState
		want  string
	}{
		{framework.StateNotConfigured, "not_configured"},
		{framework.StateConfigured, "configured"},
		{framework.StateActive, "active"},
		{framework.StateError, "error"},
	}
	for _, tc := range cases {
		if string(tc.state) != tc.want {
			t.Errorf("ProductState %q: got %q, want %q", tc.state, string(tc.state), tc.want)
		}
	}
}
