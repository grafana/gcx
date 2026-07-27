package services_test

// These tests pin the pre-GA agent output contract for the services mutation
// commands (include, exclude, clear):
//
//   - default human stdout stays byte-identical to the pre-migration
//     MutationResult.Emit one-liner;
//   - agent mode emits EXACTLY ONE JSON value (a
//     gcx.instrumentation.mutation document) on stdout, then EOF;
//   - explicit -o json / -o yaml overrides are honored.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/instrumentation/services"
	"github.com/grafana/gcx/internal/agent"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	instoutput "github.com/grafana/gcx/internal/providers/instrumentation/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runServiceMutation drives one of the services run* functions against a fake
// server whose namespace "grotshop" has autoinstrument=true and an INCLUDED
// override for "frontend". With that state:
//
//	include frontend → idempotent no-op (autoinstrument already on; the DWIM
//	  mutation removes no EXCLUDED override and adds nothing) → changed:false
//	exclude frontend → removes INCLUDED, adds EXCLUDED → changed:true
//	clear frontend   → removes the INCLUDED override → changed:true
func runServiceMutation(t *testing.T, verb string, outOpts *cmdio.Options) (string, error) {
	t.Helper()

	auto := true
	srv := &includeTestServer{
		getAppResp: buildGetAppResp(t, "c1", "grotshop", &auto,
			[]map[string]any{{"name": "frontend", "selection": "SELECTION_INCLUDED"}}),
		discoveryItems: []map[string]any{
			{"clusterName": "c1", "namespace": "grotshop", "name": "frontend"},
		},
	}
	ts := srv.start(t)
	client := makeIncludeClient(t, ts.URL)

	var out bytes.Buffer
	var err error
	switch verb {
	case "include":
		err = services.RunInclude(context.Background(), outOpts, client, "c1", "grotshop", "frontend",
			instrumentation.BackendURLs{}, instrumentation.PromHeaders{}, &out)
	case "exclude":
		err = services.RunExclude(context.Background(), outOpts, client, "c1", "grotshop", "frontend",
			instrumentation.BackendURLs{}, instrumentation.PromHeaders{}, &out)
	case "clear":
		err = services.RunClear(context.Background(), outOpts, client, "c1", "grotshop", "frontend",
			instrumentation.BackendURLs{}, instrumentation.PromHeaders{}, &out)
	default:
		t.Fatalf("unknown verb %q", verb)
	}
	return out.String(), err
}

// serviceMutationCases is the shared table for the three services mutation
// commands against the fixed fake state.
func serviceMutationCases() []struct {
	verb        string
	wantHuman   string
	wantChanged bool
} {
	return []struct {
		verb        string
		wantHuman   string
		wantChanged bool
	}{
		{verb: "include", wantHuman: "include \"c1/grotshop/frontend\": no changes\n", wantChanged: false},
		{verb: "exclude", wantHuman: "exclude \"c1/grotshop/frontend\": done\n", wantChanged: true},
		{verb: "clear", wantHuman: "clear \"c1/grotshop/frontend\": done\n", wantChanged: true},
	}
}

func TestServicesMutations_HumanDefault_ByteIdentical(t *testing.T) {
	for _, tc := range serviceMutationCases() {
		t.Run(tc.verb, func(t *testing.T) {
			t.Setenv("GCX_AGENT_MODE", "false")
			agent.ResetForTesting()
			t.Cleanup(agent.ResetForTesting)

			out, err := runServiceMutation(t, tc.verb, services.NewMutationTestIO(t))
			require.NoError(t, err)
			assert.Equal(t, tc.wantHuman, out, "default human stdout must stay byte-identical")
		})
	}
}

func TestServicesMutations_AgentMode_SingleJSONDocument(t *testing.T) {
	for _, tc := range serviceMutationCases() {
		t.Run(tc.verb, func(t *testing.T) {
			// The agents default is resolved when the options bind their
			// flags, so agent mode must be on before building the options.
			agent.SetFlag(true)
			t.Cleanup(func() { agent.SetFlag(false) })

			out, err := runServiceMutation(t, tc.verb, services.NewMutationTestIO(t))
			require.NoError(t, err)

			dec := json.NewDecoder(strings.NewReader(out))
			var doc map[string]any
			require.NoError(t, dec.Decode(&doc), "stdout is not valid JSON:\n%s", out)
			var second any
			require.ErrorIs(t, dec.Decode(&second), io.EOF, "stdout must contain exactly one JSON value:\n%s", out)

			assert.Equal(t, instoutput.MutationResultType, doc["type"])
			assert.Equal(t, "1", doc["schema_version"])
			assert.Equal(t, tc.verb, doc["action"])
			assert.Equal(t, tc.wantChanged, doc["changed"])
			target, ok := doc["target"].(map[string]any)
			require.True(t, ok, "target must be an object")
			assert.Equal(t, "c1", target["cluster"])
			assert.Equal(t, "grotshop", target["namespace"])
			assert.Equal(t, "frontend", target["service"])
		})
	}
}

func TestServicesMutations_ExplicitOutputOverrides(t *testing.T) {
	t.Run("-o yaml wins over agent default", func(t *testing.T) {
		agent.SetFlag(true)
		t.Cleanup(func() { agent.SetFlag(false) })

		out, err := runServiceMutation(t, "exclude", services.NewMutationTestIO(t, "-o", "yaml"))
		require.NoError(t, err)
		assert.Contains(t, out, "action: exclude\n")
		assert.Contains(t, out, "type: gcx.instrumentation.mutation\n")
		assert.Contains(t, out, "service: frontend\n")
	})

	t.Run("-o json in human mode", func(t *testing.T) {
		t.Setenv("GCX_AGENT_MODE", "false")
		agent.ResetForTesting()
		t.Cleanup(agent.ResetForTesting)

		out, err := runServiceMutation(t, "clear", services.NewMutationTestIO(t, "-o", "json"))
		require.NoError(t, err)
		var doc map[string]any
		require.NoError(t, json.Unmarshal([]byte(out), &doc))
		assert.Equal(t, "clear", doc["action"])
	})
}
