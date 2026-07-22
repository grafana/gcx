package output_test

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/instrumentation/output"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMutationResult_SetsDiscriminators(t *testing.T) {
	r := output.NewMutationResult("configure", output.Target{Cluster: "prod-eu"})
	assert.Equal(t, output.MutationResultType, r.Type)
	assert.Equal(t, "1", r.SchemaVersion)
	assert.Equal(t, "configure", r.Action)
	assert.Equal(t, "prod-eu", r.Target.Cluster)
}

// TestMutationTextCodec_HumanLines pins the exact legacy human one-liner for
// every target shape and changed/no-op combination.
func TestMutationTextCodec_HumanLines(t *testing.T) {
	tests := []struct {
		name    string
		target  output.Target
		action  string
		changed bool
		want    string
	}{
		{
			name:    "cluster changed",
			target:  output.Target{Cluster: "prod-eu"},
			action:  "configure",
			changed: true,
			want:    "configure \"prod-eu\": done\n",
		},
		{
			name:    "cluster no changes",
			target:  output.Target{Cluster: "prod-eu"},
			action:  "configure",
			changed: false,
			want:    "configure \"prod-eu\": no changes\n",
		},
		{
			name:    "namespace changed",
			target:  output.Target{Cluster: "prod-eu", Namespace: "checkout"},
			action:  "remove",
			changed: true,
			want:    "remove \"prod-eu/checkout\": done\n",
		},
		{
			name:    "service no changes",
			target:  output.Target{Cluster: "c", Namespace: "ns", Service: "svc"},
			action:  "exclude",
			changed: false,
			want:    "exclude \"c/ns/svc\": no changes\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := output.NewMutationResult(tt.action, tt.target)
			r.Changed = tt.changed

			var buf bytes.Buffer
			require.NoError(t, output.MutationTextCodec{}.Encode(&buf, r))
			assert.Equal(t, tt.want, buf.String())
		})
	}
}

func TestMutationTextCodec_RejectsWrongType(t *testing.T) {
	err := output.MutationTextCodec{}.Encode(&bytes.Buffer{}, struct{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected MutationResult")
}

// TestBindMutationIO_AgentMode_SingleJSONDocument verifies the wired Options
// emit exactly one JSON value carrying the discriminators in agent mode.
func TestBindMutationIO_AgentMode_SingleJSONDocument(t *testing.T) {
	agent.SetFlag(true)
	t.Cleanup(func() { agent.SetFlag(false) })

	opts := &cmdio.Options{ErrWriter: io.Discard}
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	output.BindMutationIO(opts, flags)
	require.NoError(t, opts.Validate())

	r := output.NewMutationResult("include", output.Target{Cluster: "c", Namespace: "ns", Service: "svc"})
	r.Changed = true

	var buf bytes.Buffer
	require.NoError(t, opts.Encode(&buf, r))

	dec := json.NewDecoder(strings.NewReader(buf.String()))
	var doc map[string]any
	require.NoError(t, dec.Decode(&doc), "agent stdout must be valid JSON: %s", buf.String())
	var second any
	require.ErrorIs(t, dec.Decode(&second), io.EOF, "stdout must contain exactly one JSON value")

	assert.Equal(t, output.MutationResultType, doc["type"])
	assert.Equal(t, "1", doc["schema_version"])
	assert.Equal(t, "include", doc["action"])
	assert.Equal(t, true, doc["changed"])
	target, ok := doc["target"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "c", target["cluster"])
	assert.Equal(t, "svc", target["service"])
	// fields is omitempty — absent when no diff was recorded.
	_, hasFields := doc["fields"]
	assert.False(t, hasFields)
}

// TestBindMutationIO_HumanDefault_TextCodec verifies non-agent default output
// stays the legacy human line.
func TestBindMutationIO_HumanDefault_TextCodec(t *testing.T) {
	agent.SetFlag(false)

	opts := &cmdio.Options{ErrWriter: io.Discard}
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	output.BindMutationIO(opts, flags)
	require.NoError(t, opts.Validate())

	r := output.NewMutationResult("clear", output.Target{Cluster: "c", Namespace: "ns", Service: "svc"})

	var buf bytes.Buffer
	require.NoError(t, opts.Encode(&buf, r))
	assert.Equal(t, "clear \"c/ns/svc\": no changes\n", buf.String())
}
