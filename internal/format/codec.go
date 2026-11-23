package format

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"regexp"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/parser"
	"github.com/goccy/go-yaml/token"
	sigsyaml "sigs.k8s.io/yaml"
)

type Format string

const (
	JSON Format = "json"
	YAML Format = "yaml"
)

// Codecs return a list of default codecs.
func Codecs() map[Format]Codec {
	return map[Format]Codec{
		JSON: NewJSONCodec(),
		YAML: NewYAMLCodec(),
	}
}

// Encoder encodes values to an io.Writer in a specific format.
type Encoder interface {
	Encode(dst io.Writer, value any) error
}

// Decoder decodes values from an io.Reader in a specific format.
type Decoder interface {
	Decode(src io.Reader, value any) error
}

// Codec takes care of encoding and decoding resources to and from a given format.
// It writes and reads data to provided io.Writer and io.Reader.
type Codec interface {
	Encoder
	Decoder

	Format() Format
}

var _ Codec = (*YAMLCodec)(nil)

// YAMLCodec is a Codec that encodes and decodes resources to and from YAML.
type YAMLCodec struct {
	BytesAsBase64 bool

	// PromoteExponentFloats decodes unquoted exponent-only numbers such as
	// 1e-07 as floats instead of strings. It is meant for unstructured
	// resource manifests, matching kubectl; typed decoders keep go-yaml's
	// YAML 1.1 behaviour so values like 1e10 or a short SHA stay strings.
	PromoteExponentFloats bool
}

// NewYAMLCodec returns a new YAMLCodec.
func NewYAMLCodec() *YAMLCodec {
	return &YAMLCodec{}
}

func (c *YAMLCodec) Format() Format {
	return YAML
}

func (c *YAMLCodec) Encode(dst io.Writer, value any) error {
	// Use encoding/json → sigs.k8s.io/yaml for the JSON-marshal path.
	// go-yaml's UseJSONMarshaler chokes on strings containing literal \r bytes
	// (e.g. PromQL expressions with Windows line endings), whereas the
	// JSON→YAML round-trip handles them correctly.
	if !c.BytesAsBase64 {
		jsonBytes, err := json.Marshal(value)
		if err != nil {
			return err
		}
		yamlBytes, err := sigsyaml.JSONToYAML(jsonBytes)
		if err != nil {
			return err
		}
		_, err = dst.Write(yamlBytes)
		return err
	}

	opts := []yaml.EncodeOption{
		yaml.Indent(2),
		yaml.IndentSequence(true),
		yaml.UseJSONMarshaler(),
		yaml.CustomMarshaler(func(data []byte) ([]byte, error) {
			out := make([]byte, base64.StdEncoding.EncodedLen(len(data)))
			base64.StdEncoding.Encode(out, data)
			return out, nil
		}),
	}

	return yaml.NewEncoder(dst, opts...).Encode(value)
}

func (c *YAMLCodec) Decode(src io.Reader, value any) error {
	opts := []yaml.DecodeOption{
		yaml.Strict(),
		yaml.UseJSONUnmarshaler(),
	}

	if c.BytesAsBase64 {
		opts = append(opts, yaml.CustomUnmarshaler(func(dest *[]byte, raw []byte) error {
			dst := make([]byte, base64.StdEncoding.DecodedLen(len(raw)))
			_, err := base64.StdEncoding.Decode(dst, raw)
			if err != nil {
				return err
			}

			*dest = dst

			return nil
		}))
	}

	data, err := io.ReadAll(src)
	if err != nil {
		return err
	}

	tokens := lexer.Tokenize(string(data))
	if !c.PromoteExponentFloats || !promoteExponentFloats(tokens) {
		return yaml.NewDecoder(bytes.NewReader(data), opts...).Decode(value)
	}

	file, err := parser.Parse(tokens, 0)
	if err != nil {
		return err
	}

	// Only the first document is decoded, as with Decoder.Decode.
	if len(file.Docs) == 0 || file.Docs[0].Body == nil {
		return nil
	}

	return yaml.NewDecoder(bytes.NewReader(data), opts...).DecodeFromNode(file.Docs[0].Body, value)
}

// exponentFloat matches a plain scalar that is a number written with an
// exponent but no decimal point, such as 1e-07 or 1E+21.
var exponentFloat = regexp.MustCompile(`^[-+]?[0-9]+[eE][-+]?[0-9]+$`)

// promoteExponentFloats retypes unquoted exponent-only numbers as floats and
// reports whether it changed any token.
//
// go-yaml follows YAML 1.1, where a float needs a decimal point, so it decodes
// 1e-07 as a string. The JSON-to-YAML encoder (and every YAML 1.2 reader)
// writes and reads such values as numbers, so without this a pulled file
// holding 1e-07 would come back as the string "1e-07". Mapping keys are left
// alone.
func promoteExponentFloats(tokens token.Tokens) bool {
	changed := false

	for _, tk := range tokens {
		if tk.Type != token.StringType || !exponentFloat.MatchString(tk.Value) {
			continue
		}

		// Content of a | or > block scalar is text, not a number.
		if tk.Prev != nil && (tk.Prev.Type == token.LiteralType || tk.Prev.Type == token.FoldedType) {
			continue
		}

		if tk.Next != nil && tk.Next.Type == token.MappingValueType {
			continue
		}

		// "1e-07" -> "1.0e-07" so the parser accepts it as a float.
		i := strings.IndexAny(tk.Value, "eE")
		tk.Value = tk.Value[:i] + ".0" + tk.Value[i:]
		tk.Type = token.FloatType
		changed = true
	}

	return changed
}

var _ Codec = (*JSONCodec)(nil)

// JSONCodec is a Codec that encodes and decodes resources to and from JSON.
type JSONCodec struct{}

// NewJSONCodec returns a new JSONCodec.
func NewJSONCodec() *JSONCodec {
	return &JSONCodec{}
}

func (c *JSONCodec) Format() Format {
	return JSON
}

func (c *JSONCodec) Encode(dst io.Writer, value any) error {
	encoder := json.NewEncoder(dst)
	encoder.SetIndent("", "  ")

	return encoder.Encode(value)
}

func (c *JSONCodec) Decode(src io.Reader, value any) error {
	return json.NewDecoder(src).Decode(value)
}
