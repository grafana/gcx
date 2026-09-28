package auth_test

import (
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/auth"
	"github.com/stretchr/testify/assert"
)

// TestSuccessPageLinksBackToTheStack pins the link on the page a successful
// callback gets. The consent page redirects its own tab to the callback, so
// the link is how the person gets back to the stack. It is shown only for an
// https origin on a trusted Grafana domain, and it never replaces the claim
// that only the browser step is done.
func TestSuccessPageLinksBackToTheStack(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		instanceEndpoint string
		wantHref         string
	}{
		{name: "trusted stack", instanceEndpoint: "https://mystack.grafana.net", wantHref: "https://mystack.grafana.net"},
		{name: "dev stack", instanceEndpoint: "https://mystack.grafana-dev.net", wantHref: "https://mystack.grafana-dev.net"},
		{name: "path, query and fragment dropped", instanceEndpoint: "https://mystack.grafana.net/a/b?c=d#e", wantHref: "https://mystack.grafana.net"},
		{name: "plain http", instanceEndpoint: "http://mystack.grafana.net"},
		{name: "local address over http", instanceEndpoint: "http://127.0.0.1:3000"},
		{name: "local address over https", instanceEndpoint: "https://127.0.0.1:8443"},
		{name: "localhost over https", instanceEndpoint: "https://localhost"},
		{name: "untrusted host", instanceEndpoint: "https://evil.example.com"},
		{name: "look-alike host", instanceEndpoint: "https://grafana.net.evil.example.com"},
		{name: "userinfo", instanceEndpoint: "https://user:pass@mystack.grafana.net"},
		{name: "script scheme", instanceEndpoint: "javascript:alert(1)"},
		{name: "markup in the value", instanceEndpoint: `https://x.grafana.net"><script>alert(1)</script>`},
		{name: "empty, as for grafana.com login", instanceEndpoint: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			auth.RenderSuccessPage(rec, tt.instanceEndpoint)
			body := rec.Body.String()

			assert.Contains(t, body, "Authorization complete")
			assert.Contains(t, body, "Return to your terminal")
			assert.NotContains(t, body, "Connected")
			assert.NotContains(t, body, "<script>")
			if tt.wantHref == "" {
				assert.NotContains(t, body, "<a ")
				return
			}
			// The page URL carries the callback's code and state, so the link
			// sends no referrer.
			assert.Contains(t, body, `href="`+tt.wantHref+`" rel="noreferrer"`)
			assert.Contains(t, body, "Go to "+tt.wantHref[len("https://"):])
		})
	}
}
