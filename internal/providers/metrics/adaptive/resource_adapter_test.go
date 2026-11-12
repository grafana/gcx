//nolint:testpackage // White-box test: needs access to unexported types.
package metrics

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMetricRule_Identity(t *testing.T) {
	r := MetricRule{Metric: "original"}
	assert.Equal(t, "original", r.GetResourceName())

	r.SetResourceName("renamed")
	assert.Equal(t, "renamed", r.Metric)
	assert.Equal(t, "renamed", r.GetResourceName())
}
