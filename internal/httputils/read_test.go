package httputils_test

import (
	"io"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/httputils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadResponseBody(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		limit     int64
		wantData  string
		wantErr   string
		readerErr error
	}{
		{
			name:     "within limit",
			input:    "hello world",
			limit:    100,
			wantData: "hello world",
		},
		{
			name:     "at exactly the limit",
			input:    strings.Repeat("x", 50),
			limit:    50,
			wantData: strings.Repeat("x", 50),
		},
		{
			name:    "exceeds limit by one byte",
			input:   strings.Repeat("x", 51),
			limit:   50,
			wantErr: "response body exceeds 50 bytes limit",
		},
		{
			name:    "exceeds limit at MB scale",
			input:   strings.Repeat("x", (1<<20)+1),
			limit:   1 << 20,
			wantErr: "response body exceeds 1 MB limit",
		},
		{
			name:      "propagates reader error",
			limit:     100,
			readerErr: io.ErrUnexpectedEOF,
			wantErr:   "unexpected EOF",
		},
		{
			name:     "empty body",
			input:    "",
			limit:    100,
			wantData: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var r io.Reader
			if tt.readerErr != nil {
				r = &errReader{err: tt.readerErr}
			} else {
				r = strings.NewReader(tt.input)
			}

			data, err := httputils.ReadResponseBody(r, tt.limit)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantData, string(data))
		})
	}
}

type errReader struct {
	err error
}

func (r *errReader) Read([]byte) (int, error) {
	return 0, r.err
}
