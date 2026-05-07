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
	spillFilePattern                = "gcx-results-*.json"
)

type agentsCodec struct{}

func newAgentsCodec() *agentsCodec { return &agentsCodec{} }

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
	f, err := os.CreateTemp("", spillFilePattern)
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

	summary := map[string]any{
		"spilled_to": f.Name(),
		"bytes":      len(payload),
		"preview":    previewOf(value),
	}
	if n, ok := itemCount(value); ok {
		summary["items"] = n
	}

	out := json.NewEncoder(dst)
	out.SetEscapeHTML(false)
	return out.Encode(summary)
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
