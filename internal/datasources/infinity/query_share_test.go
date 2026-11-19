package infinity_test

import (
	"testing"

	"github.com/grafana/gcx/internal/datasources/infinity"
	"github.com/grafana/gcx/internal/providers"
	"github.com/stretchr/testify/assert"
)

func TestQueryCmd_ExploreLinkFlags(t *testing.T) {
	cmd := infinity.QueryCmd(&providers.ConfigLoader{})
	assert.NotNil(t, cmd.Flags().Lookup("share-link"))
	assert.NotNil(t, cmd.Flags().Lookup("open"))
}
