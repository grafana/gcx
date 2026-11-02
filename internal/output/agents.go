package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"maps"
	"os"
	"slices"
	"strconv"

	"github.com/grafana/gcx/internal/format"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const agentsFormat format.Format = "agents"

const (
	agentsSpillEnv = "GCX_AGENT_SPILL_BYTES"
	// SpillFilePattern matches document spills for gcx agent prune.
	SpillFilePattern = "gcx-results-*.json"
	// SpillStreamFilePattern is the equivalent pattern for jq JSONL streams.
	SpillStreamFilePattern = "gcx-results-*.jsonl"
)

const (
	// defaultSpillBytes is 24 KiB. An agent host can keep a large tool
	// result out of the model context. Claude Code, for example, writes a
	// tool result above approximately 30 KB to a file and shows the model
	// only a preview of approximately 2 KB. The agents codec writes one
	// line of JSON, thus "| head" cannot make it shorter. The threshold is
	// below the limit of the host, thus gcx spills first and the receipt
	// gets to the model complete.
	defaultSpillBytes = 24 * 1024
	// spillPreviewItems is the maximum number of items in preview_sample.
	spillPreviewItems = 3
	// spillPreviewBytes is the maximum encoded size of preview_sample. A
	// preview skips entries that do not fit in this budget.
	spillPreviewBytes = 2 * 1024
)

type agentsCodec struct {
	errWriter io.Writer
}

// spillSummary replaces oversized output with a typed, versioned file reference.
type spillSummary struct {
	Type             string `json:"type"`
	SchemaVersion    string `json:"schema_version"`
	SpilledTo        string `json:"spilled_to"`
	Bytes            int    `json:"bytes"`
	ContentFormat    string `json:"content_format"`
	PreviewSample    any    `json:"preview_sample"`
	PreviewTruncated bool   `json:"preview_truncated,omitempty"`
	Message          string `json:"message"`
	Hint             string `json:"hint"`
	TotalItems       *int   `json:"total_items,omitempty"`
	TotalValues      *int   `json:"total_values,omitempty"` // jq stream values, not array elements
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

	if threshold := SpillThreshold(); threshold == 0 || buf.Len() <= threshold || isRegularFile(dst) {
		_, err := io.Copy(dst, &buf)
		return err
	}

	return c.spill(dst, value, buf.Bytes())
}

// encodeJQ budgets the whole JSONL stream and delays stdout until evaluation
// succeeds, so late errors leave neither partial output nor a success receipt.
func (c *agentsCodec) encodeJQ(dst io.Writer, results iter.Seq2[any, error]) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	threshold := SpillThreshold()
	if isRegularFile(dst) {
		threshold = 0
	}
	var f *os.File
	success := false
	defer func() {
		if f != nil {
			f.Close()
			if !success {
				os.Remove(f.Name())
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
		if f == nil && threshold > 0 && buf.Len() > threshold {
			f, err = os.CreateTemp("", SpillStreamFilePattern)
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
	err := c.writeSpillSummary(dst, spillSummary{
		SpilledTo:     f.Name(),
		Bytes:         size,
		ContentFormat: "jsonl",
		TotalValues:   &count,
		Hint:          spillStreamHint,
	})
	success = err == nil
	return err
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

	s := spillSummary{
		SpilledTo:     f.Name(),
		Bytes:         len(payload),
		ContentFormat: "json",
		Hint:          spillDocumentHint,
	}
	s.PreviewSample, s.TotalItems, s.PreviewTruncated = previewOf(value, payload)
	return c.writeSpillSummary(dst, s)
}

func (c *agentsCodec) writeSpillSummary(dst io.Writer, s spillSummary) error {
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

	emitHint(c.errWriter,
		fmt.Sprintf("response too large for stdout (%d bytes) — read %s for full data, or use -o json to force inline",
			s.Bytes, s.SpilledTo),
		"")

	return nil
}

// isRegularFile reports whether w writes to a regular file on disk, which is
// what a shell redirect such as `gcx ... > out.json` produces. A terminal, a
// pipe and /dev/null are not regular files, so a harness capturing stdout —
// as os/exec does for any non-*os.File writer — reads false here.
//
// The agents codec spills oversized payloads to protect an agent's context
// window, but a caller who named a destination file has already said where the
// bytes belong, and a receipt pointing at a different temp file is never what
// they asked for. Any stat error reports false, keeping the protective
// behavior as the default.
func isRegularFile(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}

// SpillThreshold is the encoded-payload size in bytes above which the agents
// codec spills to a file, or 0 when GCX_AGENT_SPILL_BYTES=0 disables
// spilling (e.g. when gcx is embedded and its caller cannot read the spill
// file). Exported so tests outside this package can assert the resolved value.
func SpillThreshold() int {
	if v := os.Getenv(agentsSpillEnv); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return defaultSpillBytes
}

// The receipt hints tell the agent how to get a result that is small enough
// to stay inline.
const (
	spillDocumentHint = "To get a smaller inline result, select fields with --json <fields> or --jq '<expr>'. Run with --json list to see the fields."
	spillStreamHint   = "To get a smaller inline result, select fewer values or fields in the --jq expression."
)

// previewIDFields returns the fields that identify an item in most gcx
// results. The preview keeps only these fields of each item.
func previewIDFields() []string {
	return []string{"metadata.name", "spec.title", "name", "title", "uid", "id"}
}

// previewOf makes the preview_sample and total_items of a document spill from
// the encoded payload. The value supplies the items key of a ListEnvelope.
//
// For a list shape, the preview contains the identifying fields of the first
// spillPreviewItems items, and total_items is the item count. For an object
// that is not a list, the preview contains the sorted top-level key names,
// and total_items is nil. Entries that exceed the byte budget are skipped.
// preview_truncated reports these omissions. If no entry fits, the preview is nil.
func previewOf(value any, payload []byte) (any, *int, bool) {
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return nil, nil, false
	}

	items, ok := spillItems(value, doc)
	if ok {
		n := len(items)
		preview := make([]any, 0, spillPreviewItems)
		for _, item := range items[:min(n, spillPreviewItems)] {
			preview = append(preview, previewItem(item))
		}
		sample, truncated := fitPreview(preview)
		return sample, &n, truncated
	}

	m, ok := doc.(map[string]any)
	if !ok {
		return nil, nil, false
	}
	names := make([]any, 0, len(m))
	for _, k := range slices.Sorted(maps.Keys(m)) {
		names = append(names, k)
	}
	sample, truncated := fitPreview(names)
	return sample, nil, truncated
}

// spillItems follows field-selection envelope rules. Dynamic maps must opt
// in with valid list_meta; raw API objects keep whole-object selection.
func spillItems(value any, doc any) ([]any, bool) {
	switch value.(type) {
	case unstructured.Unstructured, *unstructured.Unstructured:
		return nil, false
	}
	switch d := doc.(type) {
	case []any:
		return d, true
	case map[string]any:
		if env, ok := value.(ListEnvelope); ok {
			raw := d[env.ListItemsKey()]
			if raw == nil {
				return []any{}, true
			}
			arr, ok := raw.([]any)
			return arr, ok
		}
		if _, dynamic := value.(map[string]any); dynamic && !hasListMetaEntry(d) {
			return nil, false
		}
		if raw, exists := d["items"]; exists {
			if raw == nil {
				return []any{}, true
			}
			if arr, ok := raw.([]any); ok {
				return arr, true
			}
		}
		if key, _, ok := singleKeyItems(d); ok {
			arr, ok := d[key].([]any)
			return arr, ok
		}
	}
	return nil, false
}

// previewItem returns the identifying fields of an object item. If the item
// has no identifying field, previewItem returns its top-level scalar fields.
// If the item also has no scalar field, or is not an object, previewItem
// returns the item unchanged. fitPreview enforces the size limit.
func previewItem(item any) any {
	m, ok := item.(map[string]any)
	if !ok {
		return item
	}
	proj := extractFields(m, previewIDFields())
	maps.DeleteFunc(proj, func(_ string, v any) bool { return v == nil })
	if len(proj) > 0 {
		return proj
	}
	for k, v := range m {
		switch v.(type) {
		case string, json.Number, bool:
			proj[k] = v
		}
	}
	if len(proj) > 0 {
		return proj
	}
	return item
}

// fitPreview skips entries that exceed the remaining byte budget and reports
// whether it skipped any. A large first entry does not hide later small entries.
func fitPreview(preview []any) (any, bool) {
	if len(preview) == 0 {
		return preview, false
	}
	var fitted []any
	for _, entry := range preview {
		fitted = append(fitted, entry)
		if encodedLen(fitted) > spillPreviewBytes {
			fitted = fitted[:len(fitted)-1]
		}
	}
	if len(fitted) == 0 {
		return nil, true
	}
	return fitted, len(fitted) < len(preview)
}

// encodedLen returns the size of v in the encoding that the receipt uses.
func encodedLen(v any) int {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	// Preview values came from valid JSON, so they always encode.
	_ = enc.Encode(v)    //nolint:errchkjson // Values came from valid JSON, so encoding cannot fail.
	return buf.Len() - 1 // Encode appends a newline.
}
