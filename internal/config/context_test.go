package config_test

import (
	"context"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestConfigSelectionContextValuesAreIndependent(t *testing.T) {
	base := context.Background()
	withName := config.ContextWithName(base, "staging")
	withBoth := config.ContextWithConfigFile(withName, "/tmp/staging.yaml")

	assert.Empty(t, config.ContextNameFromCtx(base))
	assert.Empty(t, config.ConfigFileFromCtx(base))
	assert.Equal(t, "staging", config.ContextNameFromCtx(withName))
	assert.Empty(t, config.ConfigFileFromCtx(withName))
	assert.Equal(t, "staging", config.ContextNameFromCtx(withBoth))
	assert.Equal(t, "/tmp/staging.yaml", config.ConfigFileFromCtx(withBoth))
	assert.Equal(t, "/tmp/staging.yaml", config.ConfigFileFromCtx(config.ContextWithConfigFile(withBoth, "")))
}
