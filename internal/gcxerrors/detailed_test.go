package gcxerrors_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
)

func TestDetailedError_Unwrap(t *testing.T) {
	err := gcxerrors.DetailedError{
		Summary: "Authentication failed",
		Parent:  context.Canceled,
	}

	assert.ErrorIs(t, err, context.Canceled)
}

func TestDetailedError_Error_OmitsDuplicateParentDetails(t *testing.T) {
	oldNoColor := color.NoColor
	color.NoColor = true
	t.Cleanup(func() { color.NoColor = oldNoColor })

	err := gcxerrors.DetailedError{
		Summary: "Unexpected error",
		Details: "grafana.server is not configured in context \"default\"",
		Parent:  errors.New("grafana.server is not configured in context \"default\""),
	}

	rendered := err.Error()

	assert.Equal(t, 1, strings.Count(rendered, err.Details))
	assert.NotContains(t, rendered, "├─ Details:")
}

func TestDetailedError_Error_SummaryOnlyHasNoScaffolding(t *testing.T) {
	oldNoColor := color.NoColor
	color.NoColor = true
	t.Cleanup(func() { color.NoColor = oldNoColor })

	err := gcxerrors.DetailedError{
		Summary: "1 of 1 rule group(s) failed to apply",
	}

	rendered := err.Error()

	assert.Equal(t, "Error: 1 of 1 rule group(s) failed to apply\n", rendered)
	assert.NotContains(t, rendered, "│")
	assert.NotContains(t, rendered, "└─")
}

func TestDetailedError_Error_KeepsDistinctParentDetails(t *testing.T) {
	oldNoColor := color.NoColor
	color.NoColor = true
	t.Cleanup(func() { color.NoColor = oldNoColor })

	err := gcxerrors.DetailedError{
		Summary: "File not found",
		Details: "could not read './foo.yaml'",
		Parent:  errors.New("open ./foo.yaml: no such file or directory"),
	}

	rendered := err.Error()

	assert.Contains(t, rendered, err.Details)
	assert.Contains(t, rendered, "├─ Details:")
	assert.Contains(t, rendered, "open ./foo.yaml: no such file or directory")
}
