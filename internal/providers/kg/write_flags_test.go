package kg_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/kg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseEntityRefToken(t *testing.T) {
	ref, err := kg.ParseEntityRefToken("myapp/Service/checkout")
	require.NoError(t, err)
	assert.Equal(t, "myapp", ref.Domain)
	assert.Equal(t, "Service", ref.Type)
	assert.Equal(t, "checkout", ref.Name)

	// The name keeps every "/" after the second one.
	ref, err = kg.ParseEntityRefToken("sourcecode/Repository/github.com/contoso/shop")
	require.NoError(t, err)
	assert.Equal(t, "sourcecode", ref.Domain)
	assert.Equal(t, "Repository", ref.Type)
	assert.Equal(t, "github.com/contoso/shop", ref.Name)

	for _, bad := range []string{"", "a/b", "a//c", "/b/c", "a/b/", "a//c/d"} {
		_, err := kg.ParseEntityRefToken(bad)
		require.Error(t, err, "expected error for %q", bad)
	}
}

func TestParseEntityRefTokenTenantDomain(t *testing.T) {
	ref, err := kg.ParseEntityRefToken("tenant/github/Repository/gcx")
	require.NoError(t, err)
	assert.Equal(t, "tenant/github", ref.Domain)
	assert.Equal(t, "Repository", ref.Type)
	assert.Equal(t, "gcx", ref.Name)

	// The name of a tenant ref also keeps every "/" after the type.
	ref, err = kg.ParseEntityRefToken("tenant/github/Repository/github.com/contoso/shop")
	require.NoError(t, err)
	assert.Equal(t, "tenant/github", ref.Domain)
	assert.Equal(t, "Repository", ref.Type)
	assert.Equal(t, "github.com/contoso/shop", ref.Name)

	// Only a tenant/ prefix uses two segments for the domain.
	ref, err = kg.ParseEntityRefToken("myapp/github/Repository/gcx")
	require.NoError(t, err)
	assert.Equal(t, "myapp", ref.Domain)
	assert.Equal(t, "github", ref.Type)
	assert.Equal(t, "Repository/gcx", ref.Name)

	// A bare "tenant" domain is an ordinary slug.
	ref, err = kg.ParseEntityRefToken("tenant/Service/checkout")
	require.NoError(t, err)
	assert.Equal(t, "tenant", ref.Domain)
	assert.Equal(t, "Service", ref.Type)
	assert.Equal(t, "checkout", ref.Name)

	for _, bad := range []string{"tenant//Repository/gcx", "tenant/github/Repository/", "tenant/github//gcx"} {
		_, err := kg.ParseEntityRefToken(bad)
		require.Error(t, err, "expected error for %q", bad)
	}
}

func TestParseTTL(t *testing.T) {
	cases := map[string]int64{"": -1, "1h": 3600, "30m": 1800, "7d": 604800, "0": 0, "-1h": -3600}
	for in, want := range cases {
		got, err := kg.ParseTTL(in)
		require.NoError(t, err, "input %q", in)
		assert.Equal(t, want, got, "input %q", in)
	}
	_, err := kg.ParseTTL("nonsense")
	require.Error(t, err)

	// Day-suffix inputs with trailing junk must error, not silently truncate.
	for _, bad := range []string{"1.5d", "7 d", "7days", "d"} {
		_, err := kg.ParseTTL(bad)
		require.Error(t, err, "input %q must be rejected, not truncated", bad)
	}
}

func TestValidateDomains(t *testing.T) {
	require.NoError(t, kg.ValidateWritableDomain("myapp"))
	require.Error(t, kg.ValidateWritableDomain("kg"))
	require.Error(t, kg.ValidateWritableDomain("MyApp"))
	require.NoError(t, kg.ValidateDomain("kg"))

	// A tenant domain names a schema the stack publishes itself (tenant/<name>).
	require.NoError(t, kg.ValidateWritableDomain("tenant/github"))
	require.NoError(t, kg.ValidateDomain("tenant/a1"))
	for _, bad := range []string{"tenant/", "tenant/foo-bar", "tenant/Github", "tenant/1abc", "tenant/a/b"} {
		require.Error(t, kg.ValidateWritableDomain(bad), "expected error for %q", bad)
	}
	// A bare "tenant" is an ordinary slug, as before.
	require.NoError(t, kg.ValidateDomain("tenant"))
	require.ErrorContains(t, kg.ValidateDomain("tenant/foo-bar"), "tenant domain")
	require.Error(t, kg.ValidateIdentifier("9bad", "type"))
	require.NoError(t, kg.ValidateIdentifier("Service", "type"))
}
