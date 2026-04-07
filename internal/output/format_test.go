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
			name:       "agent mode forces json when no command default set",
			agentMode:  true,
			wantFormat: "json",
		},
		{
			name:          "agent mode forces json when command sets text default",
			agentMode:     true,
			defaultFormat: "text",
			wantFormat:    "json",
		},
		{
			name:           "explicit -o yaml overrides agent mode json default",
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
	tests := []struct {
		name              string
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
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := &cmdio.Options{}
			opts.RegisterCustomCodec("text", &dummyCodec{})
			opts.RegisterCustomCodec("yaml", &dummyCodec{})

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
	tests := []struct {
		name      string
		agentMode bool
		jsonField string // if set, pass --json flag
		wantHint  bool
	}{
		{
			name:      "agent mode without --json emits hint",
			agentMode: true,
			wantHint:  true,
		},
		{
			name:      "agent mode with --json fields suppresses hint",
			agentMode: true,
			jsonField: "name",
			wantHint:  false,
		},
		{
			name:      "non-agent mode does not emit hint",
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

			require.NoError(t, opts.Validate())

			var buf bytes.Buffer
			require.NoError(t, opts.Encode(&buf, map[string]any{"name": "test"}))

			if tc.wantHint {
				assert.Contains(t, errBuf.String(), "--json list")
			} else {
				assert.NotContains(t, errBuf.String(), "--json list")
			}
		})
	}
}

// dummyCodec satisfies format.Codec for testing.
type dummyCodec struct{}

func (*dummyCodec) Encode(_ goio.Writer, _ any) error { return nil }
func (*dummyCodec) Decode(_ goio.Reader, _ any) error { return nil }
func (*dummyCodec) Format() format.Format             { return "text" }
