package output_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestOptions_JQ_FormatAndFlagOrder(t *testing.T) {
	for _, agentMode := range []bool{false, true} {
		for _, defaultFormat := range []string{"json", "text", "yaml", "agents"} {
			t.Run(fmt.Sprintf("agent=%t/default=%s", agentMode, defaultFormat), func(t *testing.T) {
				testutils.SetAgentMode(t, agentMode)
				t.Setenv("GCX_AGENT_SPILL_BYTES", "1") // JSON jq must still stay inline.
				t.Setenv("TMPDIR", t.TempDir())
				for _, output := range []string{"", "json", "agents", "text", "table", "yaml", "bogus"} {
					for _, jqFirst := range []bool{false, true} {
						t.Run(fmt.Sprintf("output=%s/jqFirst=%t", output, jqFirst), func(t *testing.T) {
							opts := &cmdio.Options{ErrWriter: io.Discard}
							opts.DefaultFormat(defaultFormat)
							opts.RegisterCustomCodec("text", &dummyCodec{})
							opts.RegisterCustomCodec("table", &dummyCodec{})
							flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
							opts.BindFlags(flags)
							args := []string{"--jq", "."}
							if output != "" {
								if jqFirst {
									args = append(args, "-o", output)
								} else {
									args = append([]string{"-o", output}, args...)
								}
							}
							require.NoError(t, flags.Parse(args))
							for range 2 { // Validation must be idempotent, too.
								err := opts.Validate()
								switch output {
								case "text", "table", "yaml":
									require.ErrorContains(t, err, "--jq requires JSON output (json or agents)")
								case "bogus":
									require.ErrorContains(t, err, "unknown output format")
								default:
									require.NoError(t, err)
									wantFormat := output
									if wantFormat == "" {
										wantFormat = "json"
									}
									require.Equal(t, wantFormat, opts.OutputFormat)
									require.True(t, opts.JQActive())
								}
							}
							if output == "" || output == "json" {
								var out bytes.Buffer
								require.NoError(t, opts.Encode(&out, map[string]string{"name": "<a>&"}))
								//nolint:testifylint // JSONEq would hide changes to indentation and HTML escaping.
								assert.Equal(t, "{\n  \"name\": \"\\u003ca\\u003e\\u0026\"\n}\n", out.String())
							}
						})
					}
				}
			})
		}
	}
}

func TestOptions_JQ_JSONStreamCompatibility(t *testing.T) {
	t.Setenv("GCX_AGENT_SPILL_BYTES", "1")
	t.Setenv("TMPDIR", t.TempDir())
	tests := []struct {
		query   string
		want    string
		wantErr string
	}{
		{"empty", "", ""},
		{"1, {name: \"<a>\"}, []", "1\n{\n  \"name\": \"\\u003ca\\u003e\"\n}\n[]\n", ""},
		{"1, error(\"boom\")", "1\n", "jq runtime: error: boom"},
		{"", "", "jq runtime: missing query"},
		{"  ", "", "jq runtime: missing query"},
	}
	for _, tt := range tests {
		for _, output := range []string{"", "json"} {
			t.Run(tt.query+"/output="+output, func(t *testing.T) {
				opts := &cmdio.Options{ErrWriter: io.Discard}
				flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
				opts.BindFlags(flags)
				args := []string{"--jq", tt.query}
				if output != "" {
					args = append(args, "-o", output)
				}
				require.NoError(t, flags.Parse(args))
				require.NoError(t, opts.Validate())
				var stdout bytes.Buffer
				err := opts.Encode(&stdout, nil)
				if tt.wantErr != "" {
					require.ErrorContains(t, err, tt.wantErr)
				} else {
					require.NoError(t, err)
				}
				assert.Equal(t, tt.want, stdout.String())
			})
		}
	}
}

func TestOptions_JQ_ConflictingAndRepeatedFlags(t *testing.T) {
	testutils.SetAgentMode(t, false)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"agents then json", []string{"-o", "agents", "--jq", ".", "-o", "json"}, ""},
		{"json then agents", []string{"-o", "json", "--jq", ".", "-o", "agents"}, ""},
		{"last format is invalid", []string{"-o", "agents", "--jq", ".", "-o", "yaml"}, "--jq requires JSON output"},
		{"selection", []string{"--json", "name", "--jq", "."}, "--jq and --json cannot be used together"},
		{"discovery", []string{"--json", "?", "--jq", "."}, "--jq and --json cannot be used together"},
		{"list discovery", []string{"--json", "list", "--jq", "."}, "--jq and --json cannot be used together"},
		{"empty selection", []string{"--json", "", "--jq", "."}, "--jq and --json cannot be used together"},
		{"agents and selection", []string{"-o", "agents", "--json", "name", "--jq", "."}, "--json requires JSON output"},
		{"agents and invalid jq", []string{"-o", "agents", "--jq", "broken["}, "invalid --jq expression"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orders := [][]string{tt.args}
			if tt.want != "" { // Reverse flag/value pairs, not repeated-output precedence.
				var reversed []string
				for i := len(tt.args) - 2; i >= 0; i -= 2 {
					reversed = append(reversed, tt.args[i:i+2]...)
				}
				if tt.name != "last format is invalid" {
					orders = append(orders, reversed)
				}
			}
			for _, args := range orders {
				opts := &cmdio.Options{}
				flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
				opts.BindFlags(flags)
				require.NoError(t, flags.Parse(args))
				for range 2 {
					err := opts.Validate()
					if tt.want != "" {
						require.ErrorContains(t, err, tt.want)
					} else {
						require.NoError(t, err)
						assert.Equal(t, args[len(args)-1], opts.OutputFormat)
					}
				}
			}
		})
	}
}

func TestOptions_JQ_AgentsStream(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		value     any
		threshold string
		want      string
		values    int
		spill     bool
	}{
		{name: "compact object without HTML escaping", query: ".", value: map[string]string{"name": "<a>&"}, threshold: "100", want: "{\"name\":\"<a>&\"}\n", values: 1},
		{name: "at aggregate threshold", query: ".[]", value: []string{"a", "b"}, threshold: "8", want: "\"a\"\n\"b\"\n", values: 2},
		{name: "over aggregate threshold", query: ".[]", value: []string{"a", "b"}, threshold: "7", want: "\"a\"\n\"b\"\n", values: 2, spill: true},
		{name: "empty stream", query: "empty", threshold: "1", want: ""},
		{name: "empty input array", query: ".[]", value: []any{}, threshold: "1", want: ""},
		{name: "null is a value", query: "null", threshold: "5", want: "null\n", values: 1},
		{name: "spilled null", query: "null", threshold: "4", want: "null\n", values: 1, spill: true},
		{name: "mixed values", query: "null, true, 1.5, \"a\\nb\", {a: 1}, []", threshold: "100", want: "null\ntrue\n1.5\n\"a\\nb\"\n{\"a\":1}\n[]\n", values: 6},
		{name: "large to small", query: ".payload | length", value: map[string]string{"payload": strings.Repeat("x", 102400)}, threshold: "8", want: "102400\n", values: 1},
		{name: "small to large primitive", query: "\"x\" * 2048", threshold: "1024", want: "\"" + strings.Repeat("x", 2048) + "\"\n", values: 1, spill: true},
		{name: "large array is still one value", query: "[\"x\" * 2048]", threshold: "1024", want: "[\"" + strings.Repeat("x", 2048) + "\"]\n", values: 1, spill: true},
		{name: "many small values share one budget", query: "range(0; 10000) | 0", threshold: "1024", want: strings.Repeat("0\n", 10000), values: 10000, spill: true},
		{name: "continue after spilling", query: "(\"x\" * 2048), 1, 2", threshold: "1024", want: "\"" + strings.Repeat("x", 2048) + "\"\n1\n2\n", values: 3, spill: true},
		{name: "large integer inline", query: ".spec.id + 1", value: unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{"id": int64(9007199254740993)}}}, threshold: "100", want: "9007199254740994\n", values: 1},
		{name: "big integer spilled", query: "18446744073709551616 + 1", threshold: "1", want: "18446744073709551617\n", values: 1, spill: true},
		{name: "default threshold", query: ".", value: strings.Repeat("x", 102397), threshold: "", want: "\"" + strings.Repeat("x", 102397) + "\"\n", values: 1},
		{name: "default threshold exceeded", query: ".", value: strings.Repeat("x", 102398), threshold: "", want: "\"" + strings.Repeat("x", 102398) + "\"\n", values: 1, spill: true},
		{name: "invalid threshold falls back", query: "1", threshold: "invalid", want: "1\n", values: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testutils.SetAgentMode(t, true)
			t.Setenv("GCX_AGENT_SPILL_BYTES", tt.threshold)
			dir := t.TempDir()
			t.Setenv("TMPDIR", dir)
			var stdout, stderr bytes.Buffer
			opts := jqAgentsOptions(t, tt.query, &stderr)
			require.NoError(t, opts.Encode(&stdout, tt.value))

			files, err := os.ReadDir(dir)
			require.NoError(t, err)
			if !tt.spill {
				assert.Equal(t, tt.want, stdout.String())
				assert.Empty(t, files)
				assert.Empty(t, stderr.String(), "jq suppresses the parsing hint")
				return
			}

			require.Len(t, files, 1, "one file for the whole stream")
			var receipt map[string]any
			require.NoError(t, json.Unmarshal(stdout.Bytes(), &receipt), "stdout is only one receipt")
			assert.Equal(t, cmdio.SpillReferenceType, receipt["type"])
			assert.Equal(t, "1", receipt["schema_version"])
			assert.Equal(t, "jsonl", receipt["content_format"])
			assert.EqualValues(t, len(tt.want), receipt["bytes"])
			assert.EqualValues(t, tt.values, receipt["total_values"])
			assert.NotContains(t, receipt, "total_items", "array elements are not stream values")
			require.Contains(t, receipt, "preview_sample")
			assert.Nil(t, receipt["preview_sample"], "receipt must not embed unbounded values")
			assert.Less(t, stdout.Len(), 2048, "receipt size must not grow with the stream")
			path, ok := receipt["spilled_to"].(string)
			require.True(t, ok)
			assert.Equal(t, filepath.Join(dir, files[0].Name()), path)
			assert.Equal(t, ".jsonl", filepath.Ext(path))
			payload, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(payload), "no array wrapping, number rounding, or receipt in the file")
			info, err := os.Stat(path)
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
			var hint map[string]any
			require.NoError(t, json.Unmarshal(stderr.Bytes(), &hint), "one typed spill hint")
			assert.Equal(t, "hint", hint["class"])
			assert.Contains(t, hint["summary"], path)
		})
	}
}

func jqAgentsOptions(t *testing.T, query string, stderr io.Writer) *cmdio.Options {
	t.Helper()
	opts := &cmdio.Options{ErrWriter: stderr}
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	opts.BindFlags(flags)
	require.NoError(t, flags.Parse([]string{"--jq", query, "-o", "agents"}))
	require.NoError(t, opts.Validate())
	return opts
}

func TestOptions_JQ_AgentsErrors(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		value   any
		wantErr string
	}{
		{"marshal input", ".", make(chan int), "jq: marshal input"},
		{"runtime before output", ".items.name", map[string]any{"items": []any{map[string]any{"name": "a"}}}, "jq runtime"},
		{"runtime after output", "1, error(\"boom\")", map[string]int{"original": 42}, "jq runtime: error: boom"},
		{"compile failure", "no_such_function", nil, "jq runtime"},
		{"empty expression", "", nil, "jq runtime: missing query"},
		{"whitespace expression", "  ", nil, "jq runtime: missing query"},
		{"encoding failure", "1, nan", nil, "json: unsupported value: NaN"},
	}
	for _, tt := range tests {
		for _, threshold := range []string{"1", "102400"} {
			t.Run(tt.name+"/threshold="+threshold, func(t *testing.T) {
				t.Setenv("GCX_AGENT_SPILL_BYTES", threshold)
				dir := t.TempDir()
				t.Setenv("TMPDIR", dir)
				var stdout, stderr bytes.Buffer
				err := jqAgentsOptions(t, tt.query, &stderr).Encode(&stdout, tt.value)
				require.ErrorContains(t, err, tt.wantErr)
				if strings.HasPrefix(tt.wantErr, "jq runtime") {
					var jqErr cmdio.JQRuntimeError
					require.ErrorAs(t, err, &jqErr)
					assert.NotEmpty(t, jqErr.Shape)
					require.Error(t, jqErr.Unwrap())
					if tt.name == "runtime after output" {
						assert.Equal(t, []string{"original"}, jqErr.Fields, "error describes the original input")
					}
				}
				assert.Empty(t, stdout.String(), "no partial output or success receipt on failure")
				assert.Empty(t, stderr.String(), "no spill hint for a failed stream")
				files, err := os.ReadDir(dir)
				require.NoError(t, err)
				assert.Empty(t, files, "failed evaluation removes any partial spill file")
			})
		}
	}
}

func TestOptions_JQ_AgentsIOErrors(t *testing.T) {
	t.Run("create spill file", func(t *testing.T) {
		t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
		t.Setenv("GCX_AGENT_SPILL_BYTES", "1")
		var stdout bytes.Buffer
		err := jqAgentsOptions(t, "1", io.Discard).Encode(&stdout, nil)
		require.ErrorContains(t, err, "create spill file")
		assert.Empty(t, stdout.String())
	})
	for _, threshold := range []string{"1", "102400"} {
		t.Run("stdout/threshold="+threshold, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("TMPDIR", dir)
			t.Setenv("GCX_AGENT_SPILL_BYTES", threshold)
			wantErr := errors.New("stdout failed")
			var stderr bytes.Buffer
			err := jqAgentsOptions(t, "1", &stderr).Encode(jqErrorWriter{err: wantErr}, nil)
			require.ErrorIs(t, err, wantErr)
			assert.Empty(t, stderr.String(), "no success hint if stdout failed")
			files, err := os.ReadDir(dir)
			require.NoError(t, err)
			assert.Empty(t, files, "no orphaned spill file if receipt cannot be written")
		})
	}
}

type jqErrorWriter struct{ err error }

func (w jqErrorWriter) Write([]byte) (int, error) { return 0, w.err }
