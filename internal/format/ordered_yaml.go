package format

import (
	"io"

	goyaml "github.com/goccy/go-yaml"
)

// orderedYAMLCodec encodes via go-yaml directly (instead of the default
// JSON→YAML round trip used by YAMLCodec). The default path goes through
// sigs.k8s.io/yaml.JSONToYAML which loses object-key order; this codec
// preserves Go struct field declaration order via go-yaml's UseJSONMarshaler
// — required when an envelope's field order is deliberately non-alphabetical
// and optimized for human readability (e.g., the rich AlertGroup status block).
//
// Decode delegates to YAMLCodec because ordering only matters on output.
type orderedYAMLCodec struct {
	dec *YAMLCodec
}

// NewOrderedYAMLCodec returns a Codec that preserves Go struct field
// declaration order on encode (via go-yaml) while decoding with the standard
// YAMLCodec. Use as a drop-in replacement for NewYAMLCodec() wherever key
// order in YAML output must match struct field order.
func NewOrderedYAMLCodec() Codec { //nolint:ireturn
	return &orderedYAMLCodec{dec: NewYAMLCodec()}
}

func (c *orderedYAMLCodec) Format() Format { return YAML }

func (c *orderedYAMLCodec) Encode(w io.Writer, v any) error {
	return goyaml.NewEncoder(w,
		goyaml.Indent(2),
		goyaml.IndentSequence(true),
		goyaml.UseJSONMarshaler(),
	).Encode(v)
}

func (c *orderedYAMLCodec) Decode(src io.Reader, value any) error {
	return c.dec.Decode(src, value)
}
