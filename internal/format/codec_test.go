package format_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/format"
	"github.com/stretchr/testify/require"
)

func TestYAMLCodec_BytesAsBase64_DecodeTrimsPadding(t *testing.T) {
	type doc struct {
		Data []byte `json:"data"`
	}

	for _, input := range []string{"a", "hi", "abc", "abcd", "-----BEGIN CERTIFICATE-----\n"} {
		t.Run(input, func(t *testing.T) {
			codec := &format.YAMLCodec{BytesAsBase64: true}

			var encoded bytes.Buffer
			require.NoError(t, codec.Encode(&encoded, doc{Data: []byte(input)}))

			var got doc
			require.NoError(t, codec.Decode(strings.NewReader(encoded.String()), &got))
			require.Equal(t, input, string(got.Data))
		})
	}
}
