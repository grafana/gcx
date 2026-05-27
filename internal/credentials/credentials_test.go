package credentials_test

import (
	"testing"

	"github.com/grafana/gcx/internal/credentials"
)

func TestFormatSentinel(t *testing.T) {
	got := credentials.FormatSentinel("production", credentials.FieldOAuthToken)
	want := "keychain:gcx:production:oauth-token"
	if got != want {
		t.Errorf("FormatSentinel: got %q, want %q", got, want)
	}
}

func TestIsSentinel(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"keychain:gcx:default:oauth-token", true},
		{"keychain:gcx:", true}, // prefix match only — ParseSentinel rejects malformed
		{"plaintext-token", false},
		{"", false},
		{"keychain:", false},
		{"keychain:other:foo:bar", false}, // wrong service
	}
	for _, tc := range cases {
		if got := credentials.IsSentinel(tc.in); got != tc.want {
			t.Errorf("IsSentinel(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestParseSentinel(t *testing.T) {
	cases := []struct {
		in        string
		wantCtx   string
		wantField credentials.Field
		wantOK    bool
	}{
		{"keychain:gcx:production:oauth-token", "production", credentials.FieldOAuthToken, true},
		{"keychain:gcx:default:cloud-token", "default", credentials.FieldCloudToken, true},
		{"keychain:gcx:my-ctx:grafana-password", "my-ctx", credentials.FieldGrafanaPassword, true},
		{"keychain:gcx:weird:name:oauth-token", "weird:name", credentials.FieldOAuthToken, true},
		{"keychain:gcx:", "", "", false},
		{"keychain:gcx:nofield", "", "", false},
		{"keychain:gcx:ctx:", "", "", false},
		{"plain-string", "", "", false},
	}
	for _, tc := range cases {
		gotCtx, gotField, gotOK := credentials.ParseSentinel(tc.in)
		if gotCtx != tc.wantCtx || gotField != tc.wantField || gotOK != tc.wantOK {
			t.Errorf("ParseSentinel(%q) = (%q, %q, %v); want (%q, %q, %v)",
				tc.in, gotCtx, gotField, gotOK, tc.wantCtx, tc.wantField, tc.wantOK)
		}
	}
}

func TestAccountKey(t *testing.T) {
	got := credentials.AccountKey("default", credentials.FieldGrafanaToken)
	want := "default:grafana-token"
	if got != want {
		t.Errorf("AccountKey: got %q, want %q", got, want)
	}
}

func TestRoundTrip(t *testing.T) {
	for _, field := range credentials.AllFields {
		sentinel := credentials.FormatSentinel("ctx-1", field)
		ctx, parsed, ok := credentials.ParseSentinel(sentinel)
		if !ok {
			t.Errorf("round-trip failed for %s: ParseSentinel(%q) returned ok=false", field, sentinel)
			continue
		}
		if ctx != "ctx-1" {
			t.Errorf("round-trip ctx for %s: got %q, want %q", field, ctx, "ctx-1")
		}
		if parsed != field {
			t.Errorf("round-trip field: got %q, want %q", parsed, field)
		}
	}
}
