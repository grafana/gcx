package assistant

import (
	"context"
	"testing"
	"time"
)

func TestNewAssistantStreamingHTTPClient_TimeoutMatchesStreamSeconds(t *testing.T) {
	c := newAssistantStreamingHTTPClient(context.Background(), 420)
	if c.Timeout != 420*time.Second {
		t.Fatalf("Timeout: got %v, want %v", c.Timeout, 420*time.Second)
	}
}

func TestNewAssistantStreamingHTTPClient_DefaultsWhenNonPositive(t *testing.T) {
	c := newAssistantStreamingHTTPClient(context.Background(), 0)
	if c.Timeout != 300*time.Second {
		t.Fatalf("Timeout: got %v, want default 300s", c.Timeout)
	}
}
