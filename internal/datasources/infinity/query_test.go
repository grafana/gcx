package infinity_test

import (
	"testing"

	"github.com/grafana/gcx/internal/datasources/infinity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestResolveSource(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		inline  string
		wantSrc string
		wantURL string
		wantErr string
	}{
		{
			name:    "URL argument sets source to url",
			args:    []string{"https://example.com"},
			inline:  "",
			wantSrc: "url",
			wantURL: "https://example.com",
		},
		{
			name:    "inline data sets source to inline",
			args:    []string{},
			inline:  `[{"a":1}]`,
			wantSrc: "inline",
			wantURL: "",
		},
		{
			name:    "both URL and inline returns error",
			args:    []string{"https://example.com"},
			inline:  "data",
			wantErr: "provide either a URL argument or --inline, not both",
		},
		{
			name:    "neither URL nor inline defaults to url with empty target",
			args:    []string{},
			inline:  "",
			wantSrc: "url",
			wantURL: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, targetURL, err := infinity.ResolveSource(tt.args, tt.inline)
			if tt.wantErr != "" {
				assert.EqualError(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantSrc, src)
				assert.Equal(t, tt.wantURL, targetURL)
			}
		})
	}
}
