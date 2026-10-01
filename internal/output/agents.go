package output

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"math"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/host"
)

const (
	agentsFormat      format.Format = "agents"
	agentsSpillEnv                  = "GCX_AGENT_SPILL_BYTES"
	defaultSpillBytes               = 100 * 1024 // 100 KiB
	spillPreviewItems               = 3

	// SpillFilePattern matches document spills for gcx agent prune.
	SpillFilePattern = "gcx-results-*.json"
	// SpillStreamFilePattern is the equivalent pattern for jq JSONL streams.
	SpillStreamFilePattern = "gcx-results-*.jsonl"
)

type agentsCodec struct {
	errWriter io.Writer
}

// spillSummary replaces oversized output with a typed, versioned file reference.
type spillSummary struct {
	Type          string `json:"type"`
	SchemaVersion string `json:"schema_version"`
	SpilledTo     string `json:"spilled_to"`
	Bytes         int    `json:"bytes"`
	ContentFormat string `json:"content_format"`
	PreviewSample any    `json:"preview_sample"`
	Message       string `json:"message"`
	TotalItems    *int   `json:"total_items,omitempty"`
	TotalValues   *int   `json:"total_values,omitempty"` // jq stream values, not array elements
}

const (
	// SpillReferenceType is the value of the spill receipt's "type" field.
	SpillReferenceType = "gcx.spill_reference"
	// spillSchemaVersion versions the spill receipt shape itself.
	spillSchemaVersion = "1"
)

// newAgentsCodec returns the agents codec. A nil errWriter sends hints to the
// invocation's stderr.
func newAgentsCodec(errWriter io.Writer) *agentsCodec {
	return &agentsCodec{errWriter: errWriter}
}

func (c *agentsCodec) hintWriter(ctx context.Context) io.Writer {
	if c.errWriter == nil {
		return host.Stderr(ctx)
	}
	return c.errWriter
}

func (c *agentsCodec) Format() format.Format { return agentsFormat }

func (c *agentsCodec) Decode(io.Reader, any) error {
	return errors.New("agents codec does not support decoding")
}

func (c *agentsCodec) Encode(ctx context.Context, dst io.Writer, value any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return err
	}

	if buf.Len() <= SpillThreshold(ctx) {
		_, err := io.Copy(dst, &buf)
		return err
	}

	return c.spill(ctx, dst, value, buf.Bytes())
}

// encodeJQ budgets the whole JSONL stream and delays stdout until evaluation
// succeeds, so late errors leave neither partial output nor a success receipt.
func (c *agentsCodec) encodeJQ(ctx context.Context, dst io.Writer, results iter.Seq2[any, error]) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	threshold := SpillThreshold(ctx)
	var f *os.File
	success := false
	defer func() {
		if f != nil {
			f.Close()
			if !success {
				_ = host.Remove(ctx, f.Name())
			}
		}
	}()

	count, size := 0, 0
	for value, err := range results {
		if err != nil {
			return err
		}
		if err := enc.Encode(value); err != nil {
			return err
		}
		count++
		if f == nil && buf.Len() > threshold {
			f, err = host.CreateTemp(ctx, "", SpillStreamFilePattern)
			if err != nil {
				return fmt.Errorf("create spill file: %w", err)
			}
		}
		if f != nil {
			size += buf.Len()
			if _, err := io.Copy(f, &buf); err != nil {
				return fmt.Errorf("write spill file: %w", err)
			}
		}
	}

	if f == nil {
		_, err := io.Copy(dst, &buf)
		return err
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close spill file: %w", err)
	}
	// Omit previews: a single yielded value could make the receipt unbounded.
	err := c.writeSpillSummary(ctx, dst, spillSummary{
		SpilledTo:     f.Name(),
		Bytes:         size,
		ContentFormat: "jsonl",
		TotalValues:   &count,
	})
	success = err == nil
	return err
}

func (c *agentsCodec) spill(ctx context.Context, dst io.Writer, value any, payload []byte) error {
	f, err := host.CreateTemp(ctx, "", SpillFilePattern)
	if err != nil {
		return fmt.Errorf("create spill file: %w", err)
	}
	if _, err := f.Write(payload); err != nil {
		f.Close()
		_ = host.Remove(ctx, f.Name())
		return fmt.Errorf("write spill file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close spill file: %w", err)
	}

	s := spillSummary{
		SpilledTo:     f.Name(),
		Bytes:         len(payload),
		ContentFormat: "json",
		PreviewSample: previewOf(value),
	}
	if n, ok := itemCount(value); ok {
		s.TotalItems = &n
	}
	return c.writeSpillSummary(ctx, dst, s)
}

func (c *agentsCodec) writeSpillSummary(ctx context.Context, dst io.Writer, s spillSummary) error {
	s.Type = SpillReferenceType
	s.SchemaVersion = spillSchemaVersion
	s.Message = fmt.Sprintf(
		"Response too large for stdout (%d bytes). Full data written to %s. Read that file for complete results, or rerun with -o json to force inline output.",
		s.Bytes, s.SpilledTo,
	)

	out := json.NewEncoder(dst)
	out.SetEscapeHTML(false)
	if err := out.Encode(s); err != nil {
		return err
	}

	emitHint(c.hintWriter(ctx),
		fmt.Sprintf("response too large for stdout (%d bytes) — read %s for full data, or use -o json to force inline",
			s.Bytes, s.SpilledTo),
		"")

	return nil
}

// SpillThreshold is the encoded-payload size in bytes above which the agents
// codec spills to a file. Exported so commands that can shrink a response
// server-side (e.g. gcx traces get --prune) can budget against the same
// number the codec uses.
func SpillThreshold(ctx context.Context) int {
	// A sandboxed invocation has no filesystem to spill to; the embedder gets
	// the full output and decides how much of it to keep.
	if host.Sandboxed(ctx) {
		return math.MaxInt
	}
	if v := host.Getenv(ctx, agentsSpillEnv); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return defaultSpillBytes
}

// listEnvelopeItemsValue resolves the item slice of a ListEnvelope value by
// its declared JSON key. Returns an invalid Value when value is not a
// ListEnvelope or has no matching exported slice field.
func listEnvelopeItemsValue(value any) reflect.Value {
	env, ok := value.(ListEnvelope)
	if !ok {
		return reflect.Value{}
	}
	v := reflect.ValueOf(value)
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return reflect.Value{}
	}
	key := env.ListItemsKey()
	t := v.Type()
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() || f.Type.Kind() != reflect.Slice {
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name == "" {
			name = f.Name
		}
		if name == key {
			return v.Field(i)
		}
	}
	return reflect.Value{}
}

// itemCount returns the length of slice/array values and a true bool.
// Also handles structs with an Items slice field (e.g. unstructured.UnstructuredList)
// and ListEnvelope wrappers (item slice under the declared key).
func itemCount(value any) (int, bool) {
	if items := listEnvelopeItemsValue(value); items.IsValid() {
		return items.Len(), true
	}
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

// previewOf returns the first spillPreviewItems elements for slices/lists
// (including ListEnvelope item slices), the sorted top-level key names for
// map shapes, or nil for other shapes.
func previewOf(value any) any {
	take := func(slice reflect.Value) any {
		n := min(slice.Len(), spillPreviewItems)
		return slice.Slice(0, n).Interface()
	}
	if items := listEnvelopeItemsValue(value); items.IsValid() {
		return take(items)
	}
	v := reflect.ValueOf(value)
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
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
