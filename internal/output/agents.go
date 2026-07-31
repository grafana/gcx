package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strconv"

	"github.com/grafana/gcx/internal/format"
)

const (
	agentsFormat      format.Format = "agents"
	agentsSpillEnv                  = "GCX_AGENT_SPILL_BYTES"
	defaultSpillBytes               = 100 * 1024 // 100 KiB
	spillPreviewItems               = 3

	// SpillFilePattern is the glob pattern for agent spill files. Exported so
	// companion commands (e.g. gcx agent prune) can locate them.
	SpillFilePattern = "gcx-results-*.json"
)

type agentsCodec struct {
	errWriter io.Writer
}

// spillSummary is the receipt written to stdout instead of an oversized
// payload. Because the codec's output SHAPE changes at the spill threshold,
// the receipt carries collision-resistant discriminators so a consumer can
// tell it apart from domain results without heuristics: Type is the fixed
// marker, SchemaVersion versions this receipt shape, and ContentFormat names
// the media type of the spilled file's content.
type spillSummary struct {
	Type          string `json:"type"`
	SchemaVersion string `json:"schema_version"`
	SpilledTo     string `json:"spilled_to"`
	Bytes         int    `json:"bytes"`
	ContentFormat string `json:"content_format"`
	PreviewSample any    `json:"preview_sample"`
	Message       string `json:"message"`
	TotalItems    *int   `json:"total_items,omitempty"`
}

const (
	// SpillReferenceType is the value of the spill receipt's "type" field.
	SpillReferenceType = "gcx.spill_reference"
	// spillSchemaVersion versions the spill receipt shape itself.
	spillSchemaVersion = "1"
)

func newAgentsCodec(errWriter io.Writer) *agentsCodec {
	if errWriter == nil {
		errWriter = os.Stderr
	}
	return &agentsCodec{errWriter: errWriter}
}

func (c *agentsCodec) Format() format.Format { return agentsFormat }

func (c *agentsCodec) Decode(io.Reader, any) error {
	return errors.New("agents codec does not support decoding")
}

func (c *agentsCodec) Encode(dst io.Writer, value any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return err
	}

	if buf.Len() <= spillThreshold() {
		_, err := io.Copy(dst, &buf)
		return err
	}

	return c.spill(dst, value, buf.Bytes())
}

func (c *agentsCodec) spill(dst io.Writer, value any, payload []byte) error {
	f, err := os.CreateTemp("", SpillFilePattern)
	if err != nil {
		return fmt.Errorf("create spill file: %w", err)
	}
	if _, err := f.Write(payload); err != nil {
		f.Close()
		os.Remove(f.Name())
		return fmt.Errorf("write spill file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close spill file: %w", err)
	}

	msg := fmt.Sprintf(
		"Response too large for stdout (%d bytes). Full data written to %s. Read that file for complete results, or rerun with -o json to force inline output.",
		len(payload), f.Name(),
	)

	s := spillSummary{
		Type:          SpillReferenceType,
		SchemaVersion: spillSchemaVersion,
		SpilledTo:     f.Name(),
		Bytes:         len(payload),
		ContentFormat: "json",
		PreviewSample: previewOf(value),
		Message:       msg,
	}
	if n, ok := itemCount(value); ok {
		s.TotalItems = &n
	}

	// Typed hint diagnostic: on a TTY this renders the familiar
	// "hint: ..." line; in agent mode it must be a JSONL
	// {"class":"hint",...} record so the stderr stream stays
	// machine-parseable (FR-104). The command argument is empty because the
	// summary already embeds the spill file path.
	emitHint(c.errWriter,
		fmt.Sprintf("response too large for stdout (%d bytes) — read %s for full data, or use -o json to force inline",
			len(payload), f.Name()),
		"")

	out := json.NewEncoder(dst)
	out.SetEscapeHTML(false)
	return out.Encode(s)
}

func spillThreshold() int {
	if v := os.Getenv(agentsSpillEnv); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return defaultSpillBytes
}

// itemCount returns the length of slice/array values and a true bool.
// Also handles structs with an Items slice field (e.g. unstructured.UnstructuredList).
func itemCount(value any) (int, bool) {
	v := reflect.ValueOf(value)
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return 0, false
		}
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.Slice, reflect.Array:
		return v.Len(), true
	case reflect.Struct:
		items := v.FieldByName("Items")
		if items.IsValid() && (items.Kind() == reflect.Slice || items.Kind() == reflect.Array) {
			return items.Len(), true
		}
	}
	return 0, false
}

// previewOf returns the first spillPreviewItems elements for slices/lists,
// the sorted top-level key names for map shapes, or nil for other shapes.
func previewOf(value any) any {
	v := reflect.ValueOf(value)
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	take := func(slice reflect.Value) any {
		n := min(slice.Len(), spillPreviewItems)
		return slice.Slice(0, n).Interface()
	}
	switch v.Kind() {
	case reflect.Slice, reflect.Array:
		return take(v)
	case reflect.Struct:
		items := v.FieldByName("Items")
		if items.IsValid() && (items.Kind() == reflect.Slice || items.Kind() == reflect.Array) {
			return take(items)
		}
	case reflect.Map:
		keys := v.MapKeys()
		names := make([]string, 0, len(keys))
		for _, k := range keys {
			if k.Kind() == reflect.String {
				names = append(names, k.String())
			}
		}
		sort.Strings(names)
		return names
	}
	return nil
}
