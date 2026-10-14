//go:build !wasip1

package resources

import (
	"context"
	"errors"
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/browse"
	"github.com/grafana/gcx/internal/resources/remote"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestCheckBrowseInteractive(t *testing.T) {
	tests := []struct {
		name                  string
		agentMode, inTTY, out bool
		wantSummary           string
	}{
		{name: "interactive terminal", inTTY: true, out: true},
		{name: "agent mode", agentMode: true, inTTY: true, out: true, wantSummary: "browse is interactive and cannot run in agent mode"},
		{name: "piped stdin", inTTY: false, out: true, wantSummary: "browse needs an interactive terminal"},
		{name: "piped stdout", inTTY: true, out: false, wantSummary: "browse needs an interactive terminal"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := checkBrowseInteractive(tc.agentMode, tc.inTTY, tc.out)
			if tc.wantSummary == "" {
				require.NoError(t, err)
				return
			}
			var de gcxerrors.DetailedError
			require.ErrorAs(t, err, &de)
			assert.Equal(t, tc.wantSummary, de.Summary)
			assert.NotEmpty(t, de.Suggestions)
		})
	}
}

func TestQuietContext(t *testing.T) {
	ctx := quietContext(context.Background())
	_, isNoop := logging.FromContext(ctx).(*logging.NoOpLogger)
	assert.True(t, isNoop, "puller log lines must not reach the full-screen UI")
}

func TestPullFailures(t *testing.T) {
	var none remote.OperationSummary
	require.NoError(t, pullFailures(&none))
	require.NoError(t, pullFailures(nil))

	var some remote.OperationSummary
	some.RecordFailure(nil, errors.New("403 on v0alpha1"))
	some.RecordFailure(nil, errors.New("500"))
	err := pullFailures(&some)
	var pe *browse.PartialError
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, 2, pe.Failed)
	assert.EqualError(t, pe.Err, "403 on v0alpha1")
}

func TestSchemasBySelector(t *testing.T) {
	d := resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: "dashboard.grafana.app", Version: "v1"},
		Kind:         "Dashboard", Plural: "dashboards",
	}
	missing := resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: "nothing.grafana.app", Version: "v1"},
		Kind:         "Nothing", Plural: "nothings",
	}
	server := map[string]map[string]any{
		"dashboard.grafana.app/v1/Dashboard": {"type": "object"},
	}

	got := schemasBySelector(resources.Descriptors{d, missing}, server)
	assert.Equal(t, map[string]map[string]any{
		"dashboards.v1.dashboard.grafana.app": {"type": "object"},
	}, got)

	// A failed server fetch (nil map) still resolves without panicking, so
	// provider-registered schemas can fill in.
	assert.Empty(t, schemasBySelector(resources.Descriptors{d, missing}, nil))
}
