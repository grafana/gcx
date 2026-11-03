package feedback_test

import (
	"testing"

	"github.com/grafana/gcx/internal/feedback"
	"github.com/stretchr/testify/assert"
)

// All credentials below are synthetic: made-up strings in the shape of the
// real formats, never issued by any service.
func TestScrub(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "stack url in a transport error",
			in:   `Get "https://example-stack.grafana.net/apis/dashboard.grafana.app/v1/namespaces/stacks-12345/dashboards/abc123": context deadline exceeded`,
			want: `Get "<redacted:url>": context deadline exceeded`,
		},
		{
			name: "quoted secrets with spaces",
			in:   `password="synthetic multi word password" and token: 'two words' left`,
			want: `password="<redacted:secret>" and token: '<redacted:secret>' left`,
		},
		{
			name: "secret in a JSON object",
			in:   `{"password":"synthetic-password","user":"x"}`,
			want: `{"password":"<redacted:secret>","user":"x"}`,
		},
		{
			name: "ipv4-mapped ipv6 and zone suffix",
			in:   "fe80::1%en0 and ::ffff:10.0.0.1 and [::dead:beef]:443",
			want: "<redacted:ip> and <redacted:ip> and [<redacted:ip>]:443",
		},
		{
			name: "escaped quotes and backslashes inside a quoted secret",
			in:   `{"password":"prefix\"remaining-secret","api_key":"a\\b"} done`,
			want: `{"password":"<redacted:secret>","api_key":"<redacted:secret>"} done`,
		},
		{
			name: "quoted flag values in a pasted command",
			in:   `gcx login --token "synthetic opaque credential" --password 'two words' failed`,
			want: `gcx login --token "<redacted:secret>" --password '<redacted:secret>' failed`,
		},
		{
			name: "legacy grafana api key",
			in:   "key eyJrIjoiT0tTcG1pUlY2RnVKZTFVaDFsNFZXdE9ZWmNrMkZYbk used",
			want: "key <redacted:token> used",
		},
		{
			name: "curl basic auth",
			in:   "curl -u admin:hunter2 http and curl --user=bob:pw2",
			want: "curl -u <redacted:userinfo> http and curl --user=<redacted:userinfo>",
		},
		{
			name: "cookie header",
			in:   "Cookie: grafana_session=abcdef0123456789; other=1",
			want: "Cookie: <redacted:secret>",
		},
		{
			name: "ipv6 after a word and colon",
			in:   "listen addr:2001:db8::1 failed",
			want: "listen addr:<redacted:ip> failed",
		},
		{
			name: "url with credentials",
			in:   "server: https://admin:hunter2@grafana.internal.example:3000/api/health",
			want: "server: <redacted:url>",
		},
		{
			name: "credentials in a non-http url",
			in:   "dial postgres://grafana:fake-pass-123@db.example:5432/grafana failed",
			want: "dial postgres://<redacted:userinfo>@db.example:5432/grafana failed",
		},
		{
			name: "bare stack hostnames",
			in:   "context prod points at example-stack.grafana.net, dev at mystack.grafana-dev.net and ops at x.grafana-ops.net",
			want: "context prod points at <redacted:host>, dev at <redacted:host> and ops at <redacted:host>",
		},
		{
			name: "grafana service account token",
			in:   "gcx login --token glsa_FAKEfakeFAKEfake0123456789_abcdef01 failed",
			want: "gcx login --token <redacted:token> failed",
		},
		{
			name: "grafana cloud access policy token",
			in:   "GRAFANA_CLOUD_TOKEN=glc_ZmFrZS10b2tlbi1mb3ItdGVzdHMtb25seQ==",
			want: "GRAFANA_CLOUD_TOKEN=<redacted:token>",
		},
		{
			name: "jwt",
			in:   "token eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ0ZXN0In0.c2lnbmF0dXJlLWZha2U expired",
			want: "token <redacted:token> expired",
		},
		{
			name: "authorization header",
			in:   "request had Authorization: Bearer abc.def-ghi_123 and was rejected",
			want: "request had Authorization: <redacted:auth> and was rejected",
		},
		{
			name: "bearer and basic credentials",
			in:   "tried Bearer fAkE1234token then Basic ZmFrZTpmYWtl",
			want: "tried Bearer <redacted:auth> then Basic <redacted:auth>",
		},
		{
			name: "basic as an ordinary word",
			in:   "basic authentication worked but bearer tokens did not",
			want: "basic authentication worked but bearer tokens did not",
		},
		{
			name: "github slack and aws keys",
			in:   "ghp_FAKEfakeFAKEfake0123 xoxb-0000-fake-fake AKIAFAKEFAKEFAKE0000 github_pat_FAKE_fake_0123456789",
			want: "<redacted:token> <redacted:token> <redacted:token> <redacted:token>",
		},
		{
			name: "key value secrets",
			in:   "set GRAFANA_TOKEN=not-a-real-value, password: fake123 and api_key=xyz",
			want: "set GRAFANA_TOKEN=<redacted:secret>, password: <redacted:secret> and api_key=<redacted:secret>",
		},
		{
			name: "json secret",
			in:   `{"access_token": "fake-access", "user": "x"}`,
			want: `{"access_token": "<redacted:secret>", "user": "x"}`,
		},
		{
			name: "secret flag value",
			in:   "gcx cloud login --cloud-token fakevalue123",
			want: "gcx cloud login --cloud-token <redacted:secret>",
		},
		{
			name: "email",
			in:   "logged in as jane.doe+gcx@example.com",
			want: "logged in as <redacted:email>",
		},
		{
			name: "ipv4 and ipv6",
			in:   "dial tcp 203.0.113.7:443 and [2001:db8::1]:443 and ::1",
			want: "dial tcp <redacted:ip>:443 and [<redacted:ip>]:443 and <redacted:ip>",
		},
		{
			name: "times versions and scope operators survive",
			in:   "gcx 0.9.0 failed at 12:30:45 in std::string",
			want: "gcx 0.9.0 failed at 12:30:45 in std::string",
		},
		{
			name: "home paths",
			in:   "open /Users/jdoe/.config/gcx/config.yaml, /home/jdoe/x and C:\\Users\\jdoe\\AppData\\gcx",
			want: "open ~/.config/gcx/config.yaml, ~/x and ~/AppData\\gcx",
		},
		{
			name: "full gcx error",
			in: "Error: Authentication failed\n│ GET https://example-stack.grafana.net/api/user returned 401\n" +
				"│ config: /Users/jdoe/.config/gcx/config.yaml (token=glsa_FAKEfakeFAKEfake0123456789_abcdef01)",
			want: "Error: Authentication failed\n│ GET <redacted:url> returned 401\n" +
				"│ config: ~/.config/gcx/config.yaml (token=<redacted:token>)",
		},
		{
			name: "plain note is unchanged",
			in:   "The error didn't say which flag was wrong",
			want: "The error didn't say which flag was wrong",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, feedback.Scrub(tc.in))
		})
	}
}
