package output_test

import (
	"bytes"
	goio "io"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/terminal"
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

func TestEncode_JSONFieldsHint(t *testing.T) {
	// The --json field-selection hint is pipe-aware: it is emitted to stderr
	// only on an interactive TTY. When stdout is piped or agent mode is active
	// it is suppressed, so it never contaminates a tool's merged stdout+stderr
	// capture. See docs/design/agent-mode.md.
	tests := []struct {
		name      string
		agentMode bool
		piped     bool   // simulates non-TTY stdout
		jsonField string // if set, pass --json flag
		wantHint  bool
	}{
		{
			name:     "interactive TTY without --json: hint emitted",
			wantHint: true,
		},
		{
			name:     "piped stdout: no hint",
			piped:    true,
			wantHint: false,
		},
		{
			name:      "agent mode: no hint",
			agentMode: true,
			wantHint:  false,
		},
		{
			name:      "interactive TTY with --json field selection: no hint",
			jsonField: "name",
			wantHint:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			agent.SetFlag(tc.agentMode)
			terminal.SetPiped(tc.piped)
			t.Cleanup(func() {
				agent.SetFlag(false)
				terminal.ResetForTesting()
			})

			opts := &cmdio.Options{}
			flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
			opts.BindFlags(flags)

			var errBuf bytes.Buffer
			opts.ErrWriter = &errBuf

			if tc.jsonField != "" {
				require.NoError(t, flags.Set("json", tc.jsonField))
			}

			require.NoError(t, opts.Validate())

			var buf bytes.Buffer
			require.NoError(t, opts.Encode(&buf, map[string]any{"name": "test"}))

			// The hint must never reach stdout regardless of mode.
			assert.NotContains(t, buf.String(), "--json list")

			if tc.wantHint {
				assert.Contains(t, errBuf.String(), "--json list",
					"interactive TTY should emit the field-selection hint to stderr")
			} else {
				assert.NotContains(t, errBuf.String(), "--json list",
					"hint must be suppressed when piped, in agent mode, or when --json is already used")
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

// dummyCodec satisfies format.Codec for testing.
type dummyCodec struct{}

func (*dummyCodec) Encode(_ goio.Writer, _ any) error { return nil }
func (*dummyCodec) Decode(_ goio.Reader, _ any) error { return nil }
func (*dummyCodec) Format() format.Format             { return "text" }
