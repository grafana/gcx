package k6

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSlugifyK8sName(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"My Load Test", "my-load-test"},
		{"  --api / checkout--  ", "api-checkout"},
		{strings.Repeat("a", 70), strings.Repeat("a", 63)},
		{"テスト", "k6-test"},
		{"!!!", "k6-test"},
		{"", "k6-test"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, slugifyK8sName(tt.in))
		})
	}
}

func TestTestrunK8sManifestsNeverEmitsEmptyNames(t *testing.T) {
	out := testrunK8sManifests("ns", "secret", "テスト", "export default function() {}", 1, 7, false)
	assert.Contains(t, out, "name: k6-test\n")
	assert.Contains(t, out, "name: k6-test-script\n")
	assert.NotContains(t, out, "name: \n")
	assert.NotContains(t, out, "name: -script")
	assert.NotContains(t, out, "token: \n")
}
