package infinity_test

import (
	"testing"

	"github.com/grafana/gcx/internal/datasources/infinity"
	"github.com/stretchr/testify/assert"
)

func TestParseHeaders(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want map[string]string
	}{
		{
			name: "nil slice returns nil",
			in:   nil,
			want: nil,
		},
		{
			name: "empty slice returns nil",
			in:   []string{},
			want: nil,
		},
		{
			name: "single key=value pair",
			in:   []string{"Content-Type=application/json"},
			want: map[string]string{
				"Content-Type": "application/json",
			},
		},
		{
			name: "multiple headers",
			in: []string{
				"Content-Type=application/json",
				"Accept=text/html",
				"X-Custom=foobar",
			},
			want: map[string]string{
				"Content-Type": "application/json",
				"Accept":       "text/html",
				"X-Custom":     "foobar",
			},
		},
		{
			name: "header with no equals sign is skipped",
			in:   []string{"InvalidHeader"},
			want: nil,
		},
		{
			name: "header with multiple equals signs splits at first only",
			in:   []string{"Authorization=Bearer token=abc123=="},
			want: map[string]string{
				"Authorization": "Bearer token=abc123==",
			},
		},
		{
			name: "mix of valid and invalid headers",
			in: []string{
				"Good=value",
				"NoEquals",
				"Also-Good=another",
			},
			want: map[string]string{
				"Good":      "value",
				"Also-Good": "another",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := infinity.ParseHeaders(tt.in)
			if tt.want == nil {
				assert.Nil(t, got)
			} else {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}
