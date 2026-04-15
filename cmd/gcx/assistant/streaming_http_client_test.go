package assistant_test

import (
	"context"
	"testing"
	"time"

	"github.com/grafana/gcx/cmd/gcx/assistant"
)

func TestNewAssistantStreamingHTTPClient(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input int
		want  time.Duration
	}{
		{"positive value", 420, 420 * time.Second},
		{"non-positive defaults to 300s", 0, 300 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := assistant.NewAssistantStreamingHTTPClient(context.Background(), tc.input)
			if c.Timeout != tc.want {
				t.Fatalf("Timeout: got %v, want %v", c.Timeout, tc.want)
			}
		})
	}
}
