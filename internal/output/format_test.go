package output_test

import (
	"bytes"
	goio "io"
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
	// The agent-mode hint nudges agents toward --json field selection and
	// --jq transformation, steering them away from external Python pipelines.
	// It is emitted to stderr (not stdout) and suppressed when --json or --jq
	// is already in use, or outside agent mode.
	tests := []struct {
		name      string
		agentMode bool
		jsonField string // if set, pass --json flag
		jqExpr    string // if set, pass --jq flag
		wantHint  bool
	}{
		{
			name:      "agent mode without --json or --jq: emits hint",
			agentMode: true,
			wantHint:  true,
		},
		{
			name:      "agent mode + --json field selection: hint still fires (nudges toward --jq)",
			agentMode: true,
			jsonField: "name",
			wantHint:  true,
		},
		{
			name:      "agent mode + --json list (discovery): hint suppressed",
			agentMode: true,
			jsonField: "list",
			wantHint:  false,
		},
		{
			name:      "agent mode + --jq: hint suppressed",
			agentMode: true,
			jqExpr:    ".name",
			wantHint:  false,
		},
		{
			name:      "non-agent mode: no hint",
			agentMode: false,
			wantHint:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			agent.SetFlag(tc.agentMode)
			t.Cleanup(func() { agent.SetFlag(false) })

			var errBuf bytes.Buffer
			opts := &cmdio.Options{ErrWriter: &errBuf}
			flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
			opts.BindFlags(flags)

			if tc.jsonField != "" {
				require.NoError(t, flags.Set("json", tc.jsonField))
			}
			if tc.jqExpr != "" {
				require.NoError(t, flags.Set("jq", tc.jqExpr))
			}

			require.NoError(t, opts.Validate())

			var buf bytes.Buffer
			require.NoError(t, opts.Encode(&buf, map[string]any{"name": "test"}))

			// Hint never lands on stdout.
			assert.NotContains(t, buf.String(), "hint:")

			if tc.wantHint {
				assert.Contains(t, errBuf.String(), "--json list")
				assert.Contains(t, errBuf.String(), "--jq")
				assert.Contains(t, errBuf.String(), "no external parsing needed")
			} else {
				assert.Empty(t, errBuf.String())
			}
		})
	}
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
