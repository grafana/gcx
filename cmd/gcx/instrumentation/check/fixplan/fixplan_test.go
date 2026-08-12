//nolint:testpackage // white-box testing: uses unexported promptRunner/cloudChecker seams on Options.
package fixplan

import (
	"context"
	"errors"
	"testing"

	"github.com/grafana/gcx/internal/providers"
	otelexplain "github.com/grafana/otel-checker/checks/explain"
	otelutils "github.com/grafana/otel-checker/checks/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// firstRealExplainID returns any real explain ID from the registry. Reading
// from otelexplain.All() (rather than a hardcoded ID) keeps the tests robust
// across doc renames.
func firstRealExplainID(t *testing.T) string {
	t.Helper()
	ids := otelexplain.All()
	require.NotEmpty(t, ids, "explain registry must be non-empty")
	return ids[0]
}

func TestGenerate_EmptyResults(t *testing.T) {
	plan, err := Generate(context.Background(), otelutils.Results{}, Options{})
	require.NoError(t, err)
	assert.True(t, plan.Empty)
	assert.Empty(t, plan.Content)
}

func TestGenerate_LocalWhenNoLoader(t *testing.T) {
	id := firstRealExplainID(t)
	results := otelutils.Results{
		Errors: []otelutils.ComponentResult{
			{Component: "Grafana Cloud", Message: "no headers", ExplainID: id},
		},
	}
	plan, err := Generate(context.Background(), results, Options{Loader: nil})
	require.NoError(t, err)
	assert.Equal(t, SourceLocal, plan.Source)
	assert.False(t, plan.Fallback)
	assert.NotEmpty(t, plan.Content)
	assert.Contains(t, plan.DocsUsed, id)
}

func TestGenerate_AssistantHappyPath(t *testing.T) {
	id := firstRealExplainID(t)
	results := otelutils.Results{
		Errors: []otelutils.ComponentResult{
			{Component: "Grafana Cloud", Message: "no headers", ExplainID: id},
		},
	}
	var gotMessage string
	opts := Options{
		Loader: &providers.ConfigLoader{},
		promptRunner: func(_ context.Context, _ *providers.ConfigLoader, message string) (string, error) {
			gotMessage = message
			return "# Fix plan\n\n1. Do X.\n", nil
		},
		cloudChecker: func(context.Context, *providers.ConfigLoader) error { return nil },
	}
	plan, err := Generate(context.Background(), results, opts)
	require.NoError(t, err)
	assert.Equal(t, SourceAssistant, plan.Source)
	assert.Contains(t, plan.Content, "1. Do X.")
	assert.Contains(t, gotMessage, "# Findings", "prompt runner should receive the built prompt")
	assert.False(t, plan.Fallback)
}

func TestGenerate_FallsBackWhenNotCloud(t *testing.T) {
	id := firstRealExplainID(t)
	results := otelutils.Results{
		Errors: []otelutils.ComponentResult{
			{Component: "Grafana Cloud", Message: "no headers", ExplainID: id},
		},
	}
	runnerCalled := false
	opts := Options{
		Loader: &providers.ConfigLoader{},
		promptRunner: func(context.Context, *providers.ConfigLoader, string) (string, error) {
			runnerCalled = true
			return "", nil
		},
		cloudChecker: func(context.Context, *providers.ConfigLoader) error {
			return errors.New("current context is not a Grafana Cloud stack")
		},
	}
	plan, err := Generate(context.Background(), results, opts)
	require.NoError(t, err)
	assert.Equal(t, SourceLocal, plan.Source)
	assert.True(t, plan.Fallback)
	assert.Contains(t, plan.Reason, "not a Grafana Cloud stack")
	assert.False(t, runnerCalled, "Assistant must not be called when Cloud check fails")
	assert.Contains(t, plan.Content, "# Combined fix")
}

func TestGenerate_ContextCanceledPropagates(t *testing.T) {
	id := firstRealExplainID(t)
	results := otelutils.Results{
		Errors: []otelutils.ComponentResult{
			{Component: "Grafana Cloud", Message: "no headers", ExplainID: id},
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	opts := Options{
		Loader: &providers.ConfigLoader{},
		promptRunner: func(context.Context, *providers.ConfigLoader, string) (string, error) {
			return "", context.Canceled
		},
		cloudChecker: func(context.Context, *providers.ConfigLoader) error { return nil },
	}
	_, err := Generate(ctx, results, opts)
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled),
		"context.Canceled from the runner must propagate rather than silently fall back to local, got %v", err)
}

func TestGenerate_FallsBackWhenAssistantFails(t *testing.T) {
	id := firstRealExplainID(t)
	results := otelutils.Results{
		Errors: []otelutils.ComponentResult{
			{Component: "Grafana Cloud", Message: "no headers", ExplainID: id},
		},
	}
	opts := Options{
		Loader: &providers.ConfigLoader{},
		promptRunner: func(context.Context, *providers.ConfigLoader, string) (string, error) {
			return "", errors.New("network unreachable")
		},
		cloudChecker: func(context.Context, *providers.ConfigLoader) error { return nil },
	}
	plan, err := Generate(context.Background(), results, opts)
	require.NoError(t, err, "runner errors should surface via Fallback, not a fatal error")
	assert.Equal(t, SourceLocal, plan.Source)
	assert.True(t, plan.Fallback)
	assert.Contains(t, plan.Reason, "network unreachable")
}
