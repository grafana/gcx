package fail_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/style"
	"github.com/stretchr/testify/assert"
)

func TestDetailedError_Error_OmitsDuplicateParentDetails(t *testing.T) {
	style.SetEnabled(false)
	t.Cleanup(func() { style.SetEnabled(true) })

	err := fail.DetailedError{
		Summary: "Unexpected error",
		Details: "grafana.server is not configured in context \"default\"",
		Parent:  errors.New("grafana.server is not configured in context \"default\""),
	}

	rendered := err.Error()

	assert.Equal(t, 1, strings.Count(rendered, err.Details))
	assert.NotContains(t, rendered, "├─ Details:")
}

func TestDetailedError_Error_KeepsDistinctParentDetails(t *testing.T) {
	style.SetEnabled(false)
	t.Cleanup(func() { style.SetEnabled(true) })

	err := fail.DetailedError{
		Summary: "File not found",
		Details: "could not read './foo.yaml'",
		Parent:  errors.New("open ./foo.yaml: no such file or directory"),
	}

	rendered := err.Error()

	assert.Contains(t, rendered, err.Details)
	assert.Contains(t, rendered, "├─ Details:")
	assert.Contains(t, rendered, "open ./foo.yaml: no such file or directory")
}
