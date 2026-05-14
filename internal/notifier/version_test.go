package notifier_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/notifier"
)

func TestVersionUpdateMessage_NewerRelease(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.4","html_url":"https://github.com/grafana/gcx/releases/tag/v1.2.4"}`))
	}))
	t.Cleanup(server.Close)

	msg, err := notifier.VersionUpdateMessage(context.Background(), server.Client(), server.URL, "v1.2.3")
	if err != nil {
		t.Fatalf("VersionUpdateMessage() error = %v", err)
	}
	if !strings.Contains(msg, "v1.2.4") || !strings.Contains(msg, "v1.2.3") {
		t.Fatalf("message = %q, want current and latest versions", msg)
	}
}

func TestVersionUpdateMessage_NoMessageForSameOlderOrInvalidVersions(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.3","html_url":"https://github.com/grafana/gcx/releases/tag/v1.2.3"}`))
	}))
	t.Cleanup(server.Close)

	for _, current := range []string{"v1.2.3", "v1.2.4", "SNAPSHOT", "(devel)", "v1.2.3 built from abc on now"} {
		t.Run(current, func(t *testing.T) {
			t.Parallel()

			msg, err := notifier.VersionUpdateMessage(context.Background(), server.Client(), server.URL, current)
			if err != nil {
				t.Fatalf("VersionUpdateMessage() error = %v", err)
			}
			if msg != "" {
				t.Fatalf("message = %q, want empty", msg)
			}
		})
	}
}

func TestVersionUpdateMessage_PropagatesFetchError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	if _, err := notifier.VersionUpdateMessage(context.Background(), server.Client(), server.URL, "v1.2.3"); err == nil {
		t.Fatal("VersionUpdateMessage() error = nil, want error")
	}
}

func TestVersionUpdateMessage_RequiresClient(t *testing.T) {
	t.Parallel()

	if _, err := notifier.VersionUpdateMessage(context.Background(), nil, "http://example.test", "v1.2.3"); err == nil {
		t.Fatal("VersionUpdateMessage() error = nil, want error")
	}
}
