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

type spillSummary struct {
	Kind          string `json:"kind"`
	SpilledTo     string `json:"spilled_to"`
	Bytes         int    `json:"bytes"`
	PreviewSample any    `json:"preview_sample"`
	Message       string `json:"message"`
	TotalItems    *int   `json:"total_items,omitempty"`
}

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
	s, err := spillPayload(c.errWriter, value, payload)
	if err != nil {
		return err
	}

	out := json.NewEncoder(dst)
	out.SetEscapeHTML(false)
	return out.Encode(s)
}

// writeSpillFile writes payload to a temp file matching SpillFilePattern and
// returns its path. Shared by the agents and ndjson codecs.
func writeSpillFile(payload []byte) (string, error) {
	f, err := os.CreateTemp("", SpillFilePattern)
	if err != nil {
		return "", fmt.Errorf("create spill file: %w", err)
	}
	if _, err := f.Write(payload); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", fmt.Errorf("write spill file: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("close spill file: %w", err)
	}
	return f.Name(), nil
}

// spillPayload writes the full payload to a temp file, emits an oversized-output
// hint to errWriter, and returns the summary describing the spill. Shared by the
// agents and ndjson codecs so both produce identical spill files and summaries.
func spillPayload(errWriter io.Writer, value any, payload []byte) (spillSummary, error) {
	path, err := writeSpillFile(payload)
	if err != nil {
		return spillSummary{}, err
	}

	s := spillSummary{
		Kind:          "spill",
		SpilledTo:     path,
		Bytes:         len(payload),
		PreviewSample: previewOf(value),
		Message: fmt.Sprintf(
			"Response too large for stdout (%d bytes). Full data written to %s. Read that file for complete results, or rerun with -o json to force inline output.",
			len(payload), path,
		),
	}
	if n, ok := itemCount(value); ok {
		s.TotalItems = &n
	}

	fmt.Fprintf(errWriter,
		"hint: response too large for stdout (%d bytes) — read %s for full data, or use -o json to force inline\n",
		len(payload), path,
	)

	return s, nil
}

func spillThreshold() int {
	if v := os.Getenv(agentsSpillEnv); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return defaultSpillBytes
}

// listValue resolves value to its list-shaped reflect.Value: the value itself
// for slices/arrays, or its Items field for structs that have a slice/array Items
// field (e.g. unstructured.UnstructuredList). The second return is false for any
// other shape (including nil pointers/interfaces). Shared by itemCount, previewOf
// (agents codec), and collectionElements (ndjson codec).
func listValue(value any) (reflect.Value, bool) {
	v := reflect.ValueOf(value)
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return reflect.Value{}, false
		}
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.Slice, reflect.Array:
		return v, true
	case reflect.Struct:
		items := v.FieldByName("Items")
		if items.IsValid() && (items.Kind() == reflect.Slice || items.Kind() == reflect.Array) {
			return items, true
		}
	}
	return reflect.Value{}, false
}

// itemCount returns the element count of list-shaped values (see listValue) and
// a true bool, or 0/false for other shapes.
func itemCount(value any) (int, bool) {
	if list, ok := listValue(value); ok {
		return list.Len(), true
	}
	return 0, false
}

// previewOf returns the first spillPreviewItems elements for slices/lists,
// the sorted top-level key names for map shapes, or nil for other shapes.
func previewOf(value any) any {
	if list, ok := listValue(value); ok {
		n := min(list.Len(), spillPreviewItems)
		return list.Slice(0, n).Interface()
	}

	v := reflect.ValueOf(value)
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if v.Kind() == reflect.Map {
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
