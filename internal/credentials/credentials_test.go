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

func TestBoundCredentialBinding(t *testing.T) {
	binding := credentials.Binding{
		Source:      "/canonical/user/config.yaml",
		Owner:       credentials.StackOwner("production"),
		Field:       credentials.FieldGrafanaToken,
		Destination: "grafana|server=https://prod.example|proxy=",
	}
	sentinel := credentials.FormatBoundSentinel(binding)
	if !credentials.IsBoundSentinel(sentinel) {
		t.Fatalf("FormatBoundSentinel produced invalid sentinel %q", sentinel)
	}
	if !credentials.MatchesBoundSentinel(sentinel, binding) {
		t.Fatal("bound sentinel did not match its complete binding")
	}
	if _, _, ok := credentials.ParseSentinel(sentinel); ok {
		t.Fatal("v2 bound sentinel must not be parsed as a legacy owner-selected reference")
	}

	changes := []credentials.Binding{
		{Source: "/canonical/repo/.gcx.yaml", Owner: binding.Owner, Field: binding.Field, Destination: binding.Destination},
		{Source: binding.Source, Owner: credentials.StackOwner("other"), Field: binding.Field, Destination: binding.Destination},
		{Source: binding.Source, Owner: binding.Owner, Field: credentials.FieldGrafanaPassword, Destination: binding.Destination},
		{Source: binding.Source, Owner: binding.Owner, Field: binding.Field, Destination: "grafana|server=https://other.example|proxy="},
	}
	for _, changed := range changes {
		if credentials.BoundAccountKey(changed) == credentials.BoundAccountKey(binding) {
			t.Errorf("binding change did not isolate account: %#v", changed)
		}
		if credentials.MatchesBoundSentinel(sentinel, changed) {
			t.Errorf("sentinel matched changed binding: %#v", changed)
		}
	}
}

func TestNewBoundReferenceUsesUniqueGenerationsForSameBinding(t *testing.T) {
	binding := credentials.Binding{
		Source:      "/canonical/user/config.yaml",
		Owner:       credentials.StackOwner("production"),
		Field:       credentials.FieldGrafanaToken,
		Destination: "grafana|server=https://prod.example|proxy=",
	}
	first, err := credentials.NewBoundReference(binding)
	if err != nil {
		t.Fatalf("first reference: %v", err)
	}
	second, err := credentials.NewBoundReference(binding)
	if err != nil {
		t.Fatalf("second reference: %v", err)
	}
	if first == second || first.Account == second.Account || first.Sentinel == second.Sentinel {
		t.Fatalf("rotations reused a generation: first=%#v second=%#v", first, second)
	}
	for _, ref := range []credentials.BoundReference{first, second} {
		if !credentials.MatchesBoundAccount(ref.Account, binding) || !credentials.MatchesBoundSentinel(ref.Sentinel, binding) {
			t.Fatalf("generated reference does not match binding: %#v", ref)
		}
		account, ok := credentials.AccountForBoundSentinel(ref.Sentinel, binding)
		if !ok || account != ref.Account {
			t.Fatalf("sentinel selected %q, %t; want %q", account, ok, ref.Account)
		}
	}
}

func TestParseSentinelRejectsUnknownLegacyField(t *testing.T) {
	if _, _, ok := credentials.ParseSentinel("keychain:gcx:production:not-a-secret-field"); ok {
		t.Fatal("unknown legacy field must not be accepted")
	}
}
