package httputils

import (
	"fmt"
	"io"
)

// DefaultResponseLimit is the standard response body size cap (50 MB).
// Individual callers may pass a smaller limit for resource-listing endpoints.
const DefaultResponseLimit int64 = 50 << 20

// ReadResponseBody reads up to limit bytes from r. If the response exceeds the
// limit, it returns an error instead of silently truncating.
func ReadResponseBody(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		if limit >= 1<<20 {
			return nil, fmt.Errorf("response body exceeds %d MB limit; try narrowing your query or adding filters", limit>>20)
		}
		return nil, fmt.Errorf("response body exceeds %d bytes limit; try narrowing your query or adding filters", limit)
	}
	return data, nil
}
