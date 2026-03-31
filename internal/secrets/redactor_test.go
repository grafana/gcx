package secrets_test

import (
	"testing"

	"github.com/grafana/gcx/internal/secrets"
	"github.com/stretchr/testify/require"
)

type testStruct struct {
	Public      string
	Secret      string `datapolicy:"secret"`
	SecretBytes []byte `datapolicy:"secret"`
}

func TestRedact_withStruct(t *testing.T) {
	req := require.New(t)

	input := testStruct{Public: "public", Secret: "secret", SecretBytes: []byte("secret bytes")}

	err := secrets.Redact(&input)
	req.NoError(err)

	req.Equal("public", input.Public)
	req.Equal("**REDACTED**", input.Secret)
	req.Equal([]byte("**REDACTED**"), input.SecretBytes)
}

func TestRedact_withEmptySecret(t *testing.T) {
	req := require.New(t)

	input := testStruct{Public: "public", Secret: ""}

	err := secrets.Redact(&input)
	req.NoError(err)

	req.Equal("public", input.Public)
	req.Empty(input.Secret)
	req.Nil(input.SecretBytes)
}

func TestRedact_withMap(t *testing.T) {
	req := require.New(t)

	input := map[string]*testStruct{
		"foo": {Public: "public", Secret: "secret"},
	}

	err := secrets.Redact(&input)
	req.NoError(err)

	req.Equal("public", input["foo"].Public)
	req.Equal("**REDACTED**", input["foo"].Secret)
}

func TestRedact_withSlice(t *testing.T) {
	req := require.New(t)

	input := []*testStruct{
		{Public: "public", Secret: "secret"},
	}

	err := secrets.Redact(&input)
	req.NoError(err)

	req.Equal("public", input[0].Public)
	req.Equal("**REDACTED**", input[0].Secret)
}
