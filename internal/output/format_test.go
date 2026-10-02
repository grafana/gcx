package output_test

import (
	"bytes"
	"encoding/json"
	goio "io"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBindFlags_AgentModeOverridesDefaultFormat(t *testing.T) {
	tests := []struct {
		name           string
		agentMode      bool
		defaultFormat  string
		explicitOutput string // simulates -o flag; empty = use default
		wantFormat     string
	}{
		{
			name:       "agent mode forces agents when no command default set",
			agentMode:  true,
			wantFormat: "agents",
		},
		{
			name:          "agent mode forces agents when command sets text default",
			agentMode:     true,
			defaultFormat: "text",
			wantFormat:    "agents",
		},
		{
			name:           "explicit -o yaml overrides agent mode agents default",
			agentMode:      true,
			defaultFormat:  "text",
			explicitOutput: "yaml",
			wantFormat:     "yaml",
		},
		{
			name:          "no agent mode uses command default format",
			agentMode:     false,
			defaultFormat: "yaml",
			wantFormat:    "yaml",
		},
		{
			name:       "no agent mode uses json when no command default set",
			agentMode:  false,
			wantFormat: "json",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			agent.SetFlag(tc.agentMode)
			t.Cleanup(func() { agent.SetFlag(false) })

			opts := &cmdio.Options{}
			if tc.defaultFormat != "" {
				opts.DefaultFormat(tc.defaultFormat)
			}

			// Register a dummy text codec so "text" is a valid format.
			opts.RegisterCustomCodec("text", &dummyCodec{})

			flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
			opts.BindFlags(flags)

			if tc.explicitOutput != "" {
				require.NoError(t, flags.Set("output", tc.explicitOutput))
			}

			assert.Equal(t, tc.wantFormat, opts.OutputFormat)
		})
	}
}

func TestJSONFlag_Parsing(t *testing.T) {
	agent.SetFlag(false)
	t.Cleanup(agent.ResetForTesting)

	tests := []struct {
		name              string
		defaultFormat     string // empty = use package default ("json")
		jsonFlagValue     string // empty = flag not set
		outputFlagValue   string // empty = flag not set
		wantJSONFields    []string
		wantJSONDiscovery bool
		wantOutputFormat  string
		wantErr           bool
	}{
		{
			name:             "--json with multiple fields sets JSONFields",
			jsonFlagValue:    "name,namespace,kind",
			wantJSONFields:   []string{"name", "namespace", "kind"},
			wantOutputFormat: "json",
		},
		{
			name:             "--json with single field sets JSONFields",
			jsonFlagValue:    "name",
			wantJSONFields:   []string{"name"},
			wantOutputFormat: "json",
		},
		{
			name:              "--json ? sets JSONDiscovery",
			jsonFlagValue:     "?",
			wantJSONDiscovery: true,
			wantOutputFormat:  "json",
		},
		{
			name:              "--json list sets JSONDiscovery",
			jsonFlagValue:     "list",
			wantJSONDiscovery: true,
			wantOutputFormat:  "json",
		},
		{
			// Regression: when command default is "table", --json ? must still
			// force OutputFormat to "json" so Encode reaches encodeDiscovery.
			name:              "--json ? with table-default command forces OutputFormat to json",
			defaultFormat:     "table",
			jsonFlagValue:     "?",
			wantJSONDiscovery: true,
			wantOutputFormat:  "json",
		},
		{
			// Regression: when command default is "table", --json list must still
			// force OutputFormat to "json" so Encode reaches encodeDiscovery.
			name:              "--json list with table-default command forces OutputFormat to json",
			defaultFormat:     "table",
			jsonFlagValue:     "list",
			wantJSONDiscovery: true,
			wantOutputFormat:  "json",
		},
		{
			name:             "--json not passed leaves JSONFields nil and JSONDiscovery false",
			wantOutputFormat: "json",
		},
		{
			name:            "--json and -o yaml returns error (non-JSON format)",
			jsonFlagValue:   "name",
			outputFlagValue: "yaml",
			wantErr:         true,
		},
		{
			name:             "--json and -o json is allowed",
			jsonFlagValue:    "name",
			outputFlagValue:  "json",
			wantJSONFields:   []string{"name"},
			wantOutputFormat: "json",
		},
		{
			name:            "--json and -o agents is rejected (use --json without -o in agent mode)",
			jsonFlagValue:   "name",
			outputFlagValue: "agents",
			wantErr:         true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := &cmdio.Options{}
			opts.RegisterCustomCodec("text", &dummyCodec{})
			opts.RegisterCustomCodec("yaml", &dummyCodec{})
			opts.RegisterCustomCodec("table", &dummyCodec{})

			if tc.defaultFormat != "" {
				opts.DefaultFormat(tc.defaultFormat)
			}

			flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
			opts.BindFlags(flags)

			if tc.jsonFlagValue != "" {
				require.NoError(t, flags.Set("json", tc.jsonFlagValue))
			}
			if tc.outputFlagValue != "" {
				require.NoError(t, flags.Set("output", tc.outputFlagValue))
			}

			err := opts.Validate()

			if tc.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.wantJSONFields, opts.JSONFields)
			assert.Equal(t, tc.wantJSONDiscovery, opts.JSONDiscovery)
			if tc.wantOutputFormat != "" {
				assert.Equal(t, tc.wantOutputFormat, opts.OutputFormat)
			}
		})
	}
}

func TestEncode_AgentModeHint(t *testing.T) {
	// The agent-mode field-selection hint goes to stderr only when it can
	// help: a large payload without --json or --jq. A small payload gets no
	// hint, because "2>&1 | jq" then sees an extra JSONL line and fails.
	large := strings.Repeat("x", 9*1024)
	tests := []struct {
		name      string
		agentMode bool
		output    string // if set, pass -o
		jsonField string // if set, pass --json flag
		jqExpr    string // if set, pass --jq flag
		pinned    bool   // if set, pin the default format (file-writing command)
		value     string // value of the "name" field in the payload
		wantHint  bool
	}{
		{
			name:      "agent mode + small payload: no hint",
			agentMode: true,
			value:     "test",
			wantHint:  false,
		},
		{
			name:      "agent mode + large payload: emits hint",
			agentMode: true,
			value:     large,
			wantHint:  true,
		},
		{
			name:      "agent mode + -o json + large payload: emits hint",
			agentMode: true,
			output:    "json",
			value:     large,
			wantHint:  true,
		},
		{
			name:      "agent mode + -o json + small payload: no hint",
			agentMode: true,
			output:    "json",
			value:     "test",
			wantHint:  false,
		},
		{
			name:      "agent mode + pinned default (file-writing command): no hint",
			agentMode: true,
			pinned:    true,
			value:     large,
			wantHint:  false,
		},
		{
			name:      "agent mode + --json field selection: no hint",
			agentMode: true,
			jsonField: "name",
			value:     large,
			wantHint:  false,
		},
		{
			name:      "agent mode + --json list (discovery): no hint",
			agentMode: true,
			jsonField: "list",
			value:     large,
			wantHint:  false,
		},
		{
			name:      "agent mode + --jq: no hint",
			agentMode: true,
			jqExpr:    ".name",
			value:     large,
			wantHint:  false,
		},
		{
			name:      "non-agent mode + large payload: no hint",
			agentMode: false,
			value:     large,
			wantHint:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			agent.SetFlag(tc.agentMode)
			t.Cleanup(func() { agent.SetFlag(false) })
			t.Setenv("GCX_AGENT_SPILL_BYTES", "")

			var errBuf bytes.Buffer
			opts := &cmdio.Options{ErrWriter: &errBuf}
			if tc.pinned {
				opts.PinDefaultFormat("json")
			}
			flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
			opts.BindFlags(flags)

			if tc.output != "" {
				require.NoError(t, flags.Set("output", tc.output))
			}
			if tc.jsonField != "" {
				require.NoError(t, flags.Set("json", tc.jsonField))
			}
			if tc.jqExpr != "" {
				require.NoError(t, flags.Set("jq", tc.jqExpr))
			}

			require.NoError(t, opts.Validate())

			var buf bytes.Buffer
			require.NoError(t, opts.Encode(&buf, map[string]any{"name": tc.value}))

			// Hint never lands on stdout.
			assert.NotContains(t, buf.String(), "no external parsing needed")

			if tc.wantHint {
				line := strings.TrimSpace(errBuf.String())
				var event map[string]any
				require.NoError(t, json.Unmarshal([]byte(line), &event), "agent-mode hint must be one JSONL record, got %q", line)
				assert.Equal(t, "hint", event["class"])
				assert.Contains(t, event["summary"], "--json list")
				assert.Contains(t, event["summary"], "--jq")
			} else {
				assert.Empty(t, errBuf.String())
			}
		})
	}
}

// TestEncode_AgentModeHint_OncePerOptions makes sure that repeated Encode
// calls on one Options value emit the hint one time only.
func TestEncode_AgentModeHint_OncePerOptions(t *testing.T) {
	agent.SetFlag(true)
	t.Cleanup(func() { agent.SetFlag(false) })
	t.Setenv("GCX_AGENT_SPILL_BYTES", "")

	var errBuf bytes.Buffer
	opts := &cmdio.Options{ErrWriter: &errBuf}
	opts.BindFlags(pflag.NewFlagSet("test", pflag.ContinueOnError))
	require.NoError(t, opts.Validate())

	value := map[string]any{"name": strings.Repeat("x", 9*1024)}
	var buf bytes.Buffer
	require.NoError(t, opts.Encode(&buf, value))
	require.NoError(t, opts.Encode(&buf, value))

	assert.Equal(t, 1, strings.Count(errBuf.String(), "\n"), "hint must appear one time, got %q", errBuf.String())
}

// TestEncode_AgentModeSpill_HintInReceipt makes sure that a spill in agent
// mode writes nothing to stderr. The receipt carries the file path and the
// field-selection hint.
func TestEncode_AgentModeSpill_HintInReceipt(t *testing.T) {
	agent.SetFlag(true)
	t.Cleanup(func() { agent.SetFlag(false) })
	t.Setenv("GCX_AGENT_SPILL_BYTES", "64")
	t.Setenv("TMPDIR", t.TempDir())

	var errBuf bytes.Buffer
	opts := &cmdio.Options{ErrWriter: &errBuf}
	opts.BindFlags(pflag.NewFlagSet("test", pflag.ContinueOnError))
	require.NoError(t, opts.Validate())

	var buf bytes.Buffer
	require.NoError(t, opts.Encode(&buf, map[string]any{"name": strings.Repeat("x", 256)}))

	assert.Empty(t, errBuf.String(), "agent-mode spill must not write to stderr")

	var receipt map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &receipt), "stdout must be one receipt")
	assert.Equal(t, cmdio.SpillReferenceType, receipt["type"])
	assert.NotEmpty(t, receipt["spilled_to"])
	assert.Contains(t, receipt["hint"], "--json list")
}

func TestEncodeDiscovery_EmptyTypedSlice(t *testing.T) {
	type ClusterView struct {
		Name                  string `json:"name"`
		CostMetrics           *bool  `json:"costMetrics,omitempty"`
		InstrumentationStatus string `json:"instrumentationStatus,omitempty"`
		Selection             string `json:"-"` // excluded by json:"-"
	}

	opts := &cmdio.Options{}
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	opts.BindFlags(flags)
	require.NoError(t, flags.Set("json", "list"))
	require.NoError(t, opts.Validate())

	var buf bytes.Buffer
	err := opts.Encode(&buf, []ClusterView{})
	require.NoError(t, err, "field discovery on empty typed slice must not error")

	out := buf.String()
	assert.Contains(t, out, "name", "field 'name' must appear in discovered fields")
	assert.Contains(t, out, "costMetrics", "field 'costMetrics' must appear")
	assert.Contains(t, out, "instrumentationStatus", "field 'instrumentationStatus' must appear")
	assert.NotContains(t, out, "Selection", "json:\"-\" fields must be excluded")
}

func TestEncodeDiscovery_EmptySingleKeyEnvelope(t *testing.T) {
	// A single-key list envelope with no rows (nil or empty slice) must still
	// discover item-level fields via reflection on the element type.
	type row struct {
		UID    string `json:"uid"`
		Status string `json:"status,omitempty"`
		Hidden string `json:"-"` // excluded by json:"-"
	}
	type envelope struct {
		Results []row `json:"results"`
	}

	tests := []struct {
		name  string
		value any
	}{
		{name: "nil slice", value: &envelope{}},
		{name: "empty slice", value: &envelope{Results: []row{}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &cmdio.Options{}
			flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
			opts.BindFlags(flags)
			require.NoError(t, flags.Set("json", "list"))
			require.NoError(t, opts.Validate())

			var buf bytes.Buffer
			require.NoError(t, opts.Encode(&buf, tt.value))

			out := buf.String()
			assert.Contains(t, out, "uid", "field 'uid' must appear in discovered fields")
			assert.Contains(t, out, "status", "field 'status' must appear")
			assert.NotContains(t, out, "results", "wrapper key must not appear")
			assert.NotContains(t, out, "Hidden", "json:\"-\" fields must be excluded")
		})
	}
}

// dummyCodec satisfies format.Codec for testing.
type dummyCodec struct{}

func (*dummyCodec) Encode(_ goio.Writer, _ any) error { return nil }
func (*dummyCodec) Decode(_ goio.Reader, _ any) error { return nil }
func (*dummyCodec) Format() format.Format             { return "text" }
