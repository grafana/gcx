package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/grafana/gcx/internal/format"
)

const ndjsonFormat format.Format = "ndjson"

// ndjsonCodec emits newline-delimited JSON: one complete JSON object per line.
// Every data line is wrapped as {"kind":"result","data":<value>} so that a
// stream merged with stderr diagnostics (2>&1) stays uniformly parseable
// line-by-line, with each line distinguished by its "kind" field. This is the
// default codec whenever stdout is not a TTY (see Options.Validate).
type ndjsonCodec struct {
	errWriter io.Writer
}

type ndjsonLine struct {
	Kind string `json:"kind"`
	Data any    `json:"data"`
}

func newNDJSONCodec(errWriter io.Writer) *ndjsonCodec {
	if errWriter == nil {
		errWriter = os.Stderr
	}
	return &ndjsonCodec{errWriter: errWriter}
}

func (c *ndjsonCodec) Format() format.Format { return ndjsonFormat }

func (c *ndjsonCodec) Decode(io.Reader, any) error {
	return errors.New("ndjson codec does not support decoding")
}

func (c *ndjsonCodec) Encode(dst io.Writer, value any) error {
	// Measure the full payload once. Oversized output spills to a temp file and
	// emits a single {"kind":"spill",...} line, matching the agents codec.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return err
	}

	if buf.Len() > spillThreshold() {
		s, err := spillPayload(c.errWriter, value, buf.Bytes())
		if err != nil {
			return err
		}
		return c.writeLine(dst, s)
	}

	if elems, ok := collectionElements(value); ok {
		for _, elem := range elems {
			if err := c.writeLine(dst, ndjsonLine{Kind: "result", Data: elem}); err != nil {
				return err
			}
		}
		return nil
	}

	return c.writeLine(dst, ndjsonLine{Kind: "result", Data: value})
}

// writeLine encodes v as a single compact JSON line. encoding/json's Encode
// already appends the trailing newline.
func (c *ndjsonCodec) writeLine(dst io.Writer, v any) error {
	enc := json.NewEncoder(dst)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// collectionElements returns the elements of list-shaped values (slices, arrays,
// or structs with an Items slice such as unstructured.UnstructuredList) so they
// can be emitted one per line. Single objects and wrapper maps are not treated
// as collections and emit as a single line.
func collectionElements(value any) ([]any, bool) {
	list, ok := listValue(value)
	if !ok {
		return nil, false
	}
	out := make([]any, list.Len())
	for i := range out {
		out[i] = list.Index(i).Interface()
	}
	return out, true
}
