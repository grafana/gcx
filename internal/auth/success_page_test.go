package auth_test

import (
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/grafana/gcx/internal/auth"
	"github.com/stretchr/testify/assert"
)

// TestSuccessPageLinksBackToTheStack pins the link on the page a successful
// callback gets. The consent page redirects its own tab to the callback, so
// the link is an easy way back to the stack in that tab. It is shown only for
// an https origin on a trusted Grafana domain, and it never replaces the claim
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

			assert.Contains(t, body, "<html lang=\"en\">")
			assert.Contains(t, body, "<h1>You've authorized gcx</h1>")
			assert.Contains(t, body, "Return to your terminal to see the connection result.")
			assert.Contains(t, body, "You can close this tab.")
			assert.NotContains(t, body, "Connected")
			assert.NotContains(t, body, "<script>")
			// The page URL carries the callback's code and state, so the page
			// sends no referrer at all, not only from its link.
			assert.Contains(t, body, `<meta name="referrer" content="no-referrer">`)
			if tt.wantHref == "" {
				assert.NotContains(t, body, "<a ")
				return
			}
			// The page URL carries the callback's code and state, so the link
			// sends no referrer.
			assert.Contains(t, body, `href="`+tt.wantHref+`" rel="noreferrer">Open Grafana</a>`)
			assert.Contains(t, body, `<p class="stack-host">`+tt.wantHref[len("https://"):]+`</p>`)
		})
	}
}

// TestSuccessPageTemplateEscapesTheLink pins the template's own guard under
// stackLink's: html/template escapes the href by context, so a script URL that
// got past stackLink still renders as an inert link. The nosemgrep comment on
// the link in success.html relies on this.
func TestSuccessPageTemplateEscapesTheLink(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	auth.RenderSuccessPageWithLink(rec, &url.URL{Scheme: "javascript", Opaque: "alert(1)"})
	body := rec.Body.String()

	assert.Contains(t, body, `href="#ZgotmplZ"`)
	assert.NotContains(t, body, "javascript:")
}
