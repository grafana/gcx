package output_test

import (
	"testing"
	"time"

	"github.com/grafana/gcx/internal/output"
	"github.com/stretchr/testify/assert"
)

func TestRelativeAge(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "just now"},
		{59 * time.Second, "just now"},
		{-30 * time.Second, "in under a minute"},
		{5 * time.Minute, "5m ago"},
		{-5 * time.Minute, "in 5m"},
		{3 * time.Hour, "3h ago"},
		{2 * 24 * time.Hour, "2d ago"},
		{15 * 24 * time.Hour, "2w ago"},
		{-15 * 24 * time.Hour, "in 2w"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, output.RelativeAge(tt.d), tt.d.String())
	}
}
