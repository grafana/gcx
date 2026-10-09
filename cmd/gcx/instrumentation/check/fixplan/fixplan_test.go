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
	plan, err := Generate(context.Background(), otelutils.Results{}, Options{Mode: ModeLocal})
	require.NoError(t, err)
	assert.True(t, plan.Empty)
	assert.Empty(t, plan.Content)
}

func TestGenerate_InvalidMode(t *testing.T) {
	id := firstRealExplainID(t)
	results := otelutils.Results{
		Errors: []otelutils.ComponentResult{
			{Component: "Grafana Cloud", Message: "no headers", ExplainID: id},
		},
	}
	_, err := Generate(context.Background(), results, Options{Mode: "auto"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Mode")
}

func TestGenerate_LocalMode(t *testing.T) {
	id := firstRealExplainID(t)
	results := otelutils.Results{
		Errors: []otelutils.ComponentResult{
			{Component: "Grafana Cloud", Message: "no headers", ExplainID: id},
		},
	}
	runnerCalled := false
	opts := Options{
		Mode:   ModeLocal,
		Loader: nil, // local mode must not require a loader
		promptRunner: func(context.Context, *providers.ConfigLoader, string) (string, error) {
			runnerCalled = true
			return "", nil
		},
	}
	plan, err := Generate(context.Background(), results, opts)
	require.NoError(t, err)
	assert.Equal(t, SourceLocal, plan.Source)
	assert.NotEmpty(t, plan.Content)
	assert.Contains(t, plan.DocsUsed, id)
	assert.False(t, runnerCalled, "local mode must not call the Assistant runner")
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
		Mode:   ModeAssistant,
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
}

func TestGenerate_AssistantModeRequiresLoader(t *testing.T) {
	id := firstRealExplainID(t)
	results := otelutils.Results{
		Errors: []otelutils.ComponentResult{
			{Component: "Grafana Cloud", Message: "no headers", ExplainID: id},
		},
	}
	_, err := Generate(context.Background(), results, Options{Mode: ModeAssistant, Loader: nil})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no config loader available")
}

// TestGenerate_AssistantErrorsWhenNotCloud pins the "no silent fallback"
// contract: when --fix-plan=assistant is selected and the current context
// is not Grafana Cloud, Generate must return a clear error naming both
// the failure reason and the --fix-plan=local escape hatch.
func TestGenerate_AssistantErrorsWhenNotCloud(t *testing.T) {
	id := firstRealExplainID(t)
	results := otelutils.Results{
		Errors: []otelutils.ComponentResult{
			{Component: "Grafana Cloud", Message: "no headers", ExplainID: id},
		},
	}
	runnerCalled := false
	opts := Options{
		Mode:   ModeAssistant,
		Loader: &providers.ConfigLoader{},
		promptRunner: func(context.Context, *providers.ConfigLoader, string) (string, error) {
			runnerCalled = true
			return "", nil
		},
		cloudChecker: func(context.Context, *providers.ConfigLoader) error {
			return errors.New("current context is not a Grafana Cloud stack")
		},
	}
	_, err := Generate(context.Background(), results, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Grafana Assistant not available")
	assert.Contains(t, err.Error(), "not a Grafana Cloud stack")
	assert.Contains(t, err.Error(), "--fix-plan=local")
	assert.False(t, runnerCalled, "Assistant must not be called when Cloud check fails")
}

// TestGenerate_AssistantErrorsWhenRequestFails pins the "no silent
// fallback" contract for the runner-error branch: a live Assistant call
// that returns an error propagates as a clean error, not a Fallback plan.
func TestGenerate_AssistantErrorsWhenRequestFails(t *testing.T) {
	id := firstRealExplainID(t)
	results := otelutils.Results{
		Errors: []otelutils.ComponentResult{
			{Component: "Grafana Cloud", Message: "no headers", ExplainID: id},
		},
	}
	opts := Options{
		Mode:   ModeAssistant,
		Loader: &providers.ConfigLoader{},
		promptRunner: func(context.Context, *providers.ConfigLoader, string) (string, error) {
			return "", errors.New("network unreachable")
		},
		cloudChecker: func(context.Context, *providers.ConfigLoader) error { return nil },
	}
	_, err := Generate(context.Background(), results, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Grafana Assistant request failed")
	assert.Contains(t, err.Error(), "network unreachable")
	assert.Contains(t, err.Error(), "--fix-plan=local")
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
		Mode:   ModeAssistant,
		Loader: &providers.ConfigLoader{},
		promptRunner: func(context.Context, *providers.ConfigLoader, string) (string, error) {
			return "", context.Canceled
		},
		cloudChecker: func(context.Context, *providers.ConfigLoader) error { return nil },
	}
	_, err := Generate(ctx, results, opts)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled,
		"context.Canceled from the runner must propagate as-is, not wrapped in a generic Assistant-failed error")
}
