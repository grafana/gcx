package output_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/itchyny/gojq"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestJQCodec_Encode(t *testing.T) {
	t.Parallel()

	itemsValue := map[string]any{
		"items": []any{
			map[string]any{"name": "a", "ns": "x"},
			map[string]any{"name": "b", "ns": "y"},
		},
	}

	tests := []struct {
		name       string
		query      string
		value      any
		wantValues []any // each element is one yielded value, decoded from a JSON line
		wantNDJSON bool  // when true, also assert one line per yielded value
		wantErr    string
	}{
		{
			name:       "identity filter on map",
			query:      ".",
			value:      map[string]any{"k": "v"},
			wantValues: []any{map[string]any{"k": "v"}},
		},
		{
			name:       "projection over items",
			query:      "[.items[].name]",
			value:      itemsValue,
			wantValues: []any{[]any{"a", "b"}},
		},
		{
			name:  "streaming items yields multiple values",
			query: ".items[]",
			value: itemsValue,
			wantValues: []any{
				map[string]any{"name": "a", "ns": "x"},
				map[string]any{"name": "b", "ns": "y"},
			},
		},
		{
			// NDJSON line-count check (scalars fit on one line; indented objects don't).
			name:       "streaming scalars emit one line each",
			query:      ".[]",
			value:      []any{1.0, 2.0, 3.0},
			wantValues: []any{1.0, 2.0, 3.0},
			wantNDJSON: true,
		},
		{
			name:  "construction with object literal",
			query: "[.items[] | {n: .name}]",
			value: itemsValue,
			wantValues: []any{
				[]any{
					map[string]any{"n": "a"},
					map[string]any{"n": "b"},
				},
			},
		},
		{
			name:       "empty result writes nothing",
			query:      ".items[] | select(false)",
			value:      itemsValue,
			wantValues: nil,
		},
		{
			name:  "unstructured list is seen as items collection",
			query: ".items[].metadata.name",
			value: unstructured.UnstructuredList{Items: []unstructured.Unstructured{
				{Object: map[string]any{"metadata": map[string]any{"name": "dash-1"}}},
				{Object: map[string]any{"metadata": map[string]any{"name": "dash-2"}}},
			}},
			wantValues: []any{"dash-1", "dash-2"},
		},
		{
			name:       "single unstructured object",
			query:      ".metadata.name",
			value:      unstructured.Unstructured{Object: map[string]any{"metadata": map[string]any{"name": "only"}}},
			wantValues: []any{"only"},
		},
		{
			name:    "runtime error on type mismatch",
			query:   ".foo | .bar",
			value:   map[string]any{"foo": 42},
			wantErr: "jq runtime",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			query, err := gojq.Parse(tt.query)
			require.NoError(t, err, "test query must parse")

			var buf bytes.Buffer
			err = cmdio.NewJQCodec(query).Encode(&buf, tt.value)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)

			got := decodeJSONLines(t, buf.Bytes())
			assert.Equal(t, tt.wantValues, got)

			if tt.wantNDJSON {
				lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
				assert.Len(t, lines, len(tt.wantValues), "expected one line per yielded value")
			}
		})
	}
}

// decodeJSONLines splits NDJSON output and decodes each non-empty line.
func decodeJSONLines(t *testing.T, data []byte) []any {
	t.Helper()
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	var out []any
	for {
		var v any
		err := dec.Decode(&v)
		if err != nil {
			if err.Error() == "EOF" {
				return out
			}
			require.NoError(t, err)
		}
		out = append(out, v)
	}
}

func TestJQCodec_FormatIsJSON(t *testing.T) {
	t.Parallel()

	query, err := gojq.Parse(".")
	require.NoError(t, err)
	assert.Equal(t, "json", string(cmdio.NewJQCodec(query).Format()))
}

// --- Options-level integration tests -----------------------------------------

func TestOptions_JQ_AutoFlipsToJSON(t *testing.T) {
	t.Parallel()

	opts := &cmdio.Options{}
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	opts.BindFlags(flags)
	require.NoError(t, flags.Parse([]string{"--jq", "."}))

	require.NoError(t, opts.Validate())
	assert.Equal(t, "json", opts.OutputFormat)
}

func TestOptions_JQ_ExplicitJSONOK(t *testing.T) {
	t.Parallel()

	opts := &cmdio.Options{}
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	opts.BindFlags(flags)
	require.NoError(t, flags.Parse([]string{"-o", "json", "--jq", ".items[].name"}))

	require.NoError(t, opts.Validate())
}

func TestOptions_JQ_RejectsNonJSONOutput(t *testing.T) {
	t.Parallel()

	opts := &cmdio.Options{}
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	opts.BindFlags(flags)
	require.NoError(t, flags.Parse([]string{"-o", "yaml", "--jq", "."}))

	err := opts.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--jq requires JSON output")
}

func TestOptions_JQ_RejectsJSONFieldSelection(t *testing.T) {
	t.Parallel()

	opts := &cmdio.Options{}
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	opts.BindFlags(flags)
	require.NoError(t, flags.Parse([]string{"--json", "metadata.name", "--jq", "."}))

	err := opts.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--jq and --json cannot be used together")
}

func TestOptions_JQ_RejectsJSONDiscovery(t *testing.T) {
	t.Parallel()

	opts := &cmdio.Options{}
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	opts.BindFlags(flags)
	require.NoError(t, flags.Parse([]string{"--json", "?", "--jq", "."}))

	err := opts.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--jq and --json cannot be used together")
}

func TestOptions_JQ_RejectsInvalidExpression(t *testing.T) {
	t.Parallel()

	opts := &cmdio.Options{}
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	opts.BindFlags(flags)
	require.NoError(t, flags.Parse([]string{"--jq", "broken["}))

	err := opts.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --jq expression")
}

func TestOptions_JQ_EncodeAppliesFilter(t *testing.T) {
	t.Parallel()

	opts := &cmdio.Options{}
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	opts.BindFlags(flags)
	require.NoError(t, flags.Parse([]string{"--jq", ".items | length"}))
	require.NoError(t, opts.Validate())

	var buf bytes.Buffer
	value := map[string]any{"items": []any{1, 2, 3, 4}}
	require.NoError(t, opts.Encode(&buf, value))

	assert.Equal(t, "4", strings.TrimSpace(buf.String()))
}
