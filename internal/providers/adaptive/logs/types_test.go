package logs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestQueryIngestLabel(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "N/A", queryIngestLabel(0, 0))
	assert.Equal(t, "Never", queryIngestLabel(0, 100))
	assert.Equal(t, "Rarely", queryIngestLabel(1, 1000))   // 0.1%
	assert.Equal(t, "Rarely", queryIngestLabel(10, 1000))  // 1%
	assert.Equal(t, "Sometimes", queryIngestLabel(11, 1000))
	assert.Equal(t, "Sometimes", queryIngestLabel(400, 1000)) // 40%
	assert.Equal(t, "Often", queryIngestLabel(401, 1000))
	assert.Equal(t, "Often", queryIngestLabel(999, 1000))
	assert.Equal(t, "Always", queryIngestLabel(1000, 1000))
	assert.Equal(t, "Always", queryIngestLabel(2000, 1000))
}
