package pyroscope //nolint:testpackage // Tests the unexported request encoder shared by JSON and protobuf formats.

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"
)

func TestEncodeQueryRequestSharedSelectors(t *testing.T) {
	req := QueryRequest{
		ProfileTypeID: "cpu", LabelSelector: `{service_name="app"}`,
		Start: time.UnixMilli(1000), End: time.UnixMilli(2000), MaxNodes: 512,
		ProfileIDs: []string{"profile"}, TraceIDs: []string{"trace"},
		StackTraceSelector: &StackTraceSelector{CallSite: []Location{{Name: "root"}, {Name: "leaf"}}},
	}
	body, contentType, err := encodeQueryRequest(req)
	require.NoError(t, err)
	require.Equal(t, "application/json", contentType)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &fields))
	require.JSONEq(t, `{"callSite":[{"name":"root"},{"name":"leaf"}]}`, string(fields["stackTraceSelector"]))
	require.JSONEq(t, `["profile"]`, string(fields["profileIdSelector"]))
	require.JSONEq(t, `["trace"]`, string(fields["traceIdSelector"]))

	req.Format = ProfileFormatPprof
	body, contentType, err = encodeQueryRequest(req)
	require.NoError(t, err)
	require.Equal(t, "application/proto", contentType)
	bytesFields := map[protowire.Number][][]byte{}
	intFields := map[protowire.Number]uint64{}
	for len(body) > 0 {
		number, typ, n := protowire.ConsumeTag(body)
		require.Positive(t, n)
		body = body[n:]
		if typ == protowire.BytesType {
			value, n := protowire.ConsumeBytes(body)
			require.Positive(t, n)
			bytesFields[number] = append(bytesFields[number], value)
			body = body[n:]
		} else {
			require.Equal(t, protowire.VarintType, typ)
			value, n := protowire.ConsumeVarint(body)
			require.Positive(t, n)
			intFields[number] = value
			body = body[n:]
		}
	}
	require.Equal(t, []byte("cpu"), bytesFields[1][0])
	require.Equal(t, []byte(req.LabelSelector), bytesFields[2][0])
	require.Equal(t, map[protowire.Number]uint64{3: 1000, 4: 2000, 5: 512, 6: 4}, intFields)
	require.Equal(t, [][]byte{[]byte("profile")}, bytesFields[8])
	require.Equal(t, [][]byte{[]byte("trace")}, bytesFields[10])
	// StackTraceSelector.call_site and Location.name are both field 1.
	var expected []byte
	for _, name := range []string{"root", "leaf"} {
		location := protowire.AppendTag(nil, 1, protowire.BytesType)
		location = protowire.AppendString(location, name)
		expected = protowire.AppendTag(expected, 1, protowire.BytesType)
		expected = protowire.AppendBytes(expected, location)
	}
	require.Equal(t, expected, bytesFields[7][0])
}
