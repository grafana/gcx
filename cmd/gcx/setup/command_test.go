package setup_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/setup"
	"github.com/grafana/gcx/internal/agent"
	"github.com/spf13/pflag"
)

// These tests pin the agent output contract for `gcx setup status`. Before
// the migration the command bypassed output.Options entirely and printed a
// fixed ASCII table on stdout even in agent mode; it now routes the status
// document through the codec system: the default text codec renders the
// byte-identical table, agent mode gets exactly one JSON value, and explicit
// -o json/yaml always win.

// statusTableEnabled is the byte-exact human table for an enabled
// instrumentation product with 3 clusters, pinned against the pre-migration
// writeSetupStatusTable output.
const statusTableEnabled = "PRODUCT          ENABLED  HEALTH   DETAILS\n" +
	"instrumentation  yes      healthy  3 clusters\n"

// statusTableDisabled is the byte-exact human table when no clusters exist.
const statusTableDisabled = "PRODUCT          ENABLED  HEALTH   DETAILS\n" +
	"instrumentation  no       healthy  0 clusters\n"

func TestSetupStatus_OutputContract(t *testing.T) {
	tests := []struct {
		name       string
		agentMode  bool
		output     string // explicit -o value; empty = default
		enabled    bool
		clusters   int
		wantStdout string // exact stdout; empty = use check
		check      func(t *testing.T, stdout string)
	}{
		{
			name:       "human default enabled table is byte-identical",
			enabled:    true,
			clusters:   3,
			wantStdout: statusTableEnabled,
		},
		{
			name:       "human default disabled table is byte-identical",
			enabled:    false,
			clusters:   0,
			wantStdout: statusTableDisabled,
		},
		{
			name:      "agent mode emits exactly one JSON document",
			agentMode: true,
			enabled:   true,
			clusters:  3,
			check: func(t *testing.T, stdout string) {
				t.Helper()
				doc := decodeSingleJSONValue(t, stdout)
				if doc["type"] != "gcx.setup.status" {
					t.Fatalf("type = %v, want gcx.setup.status", doc["type"])
				}
				if doc["schema_version"] != "1" {
					t.Fatalf("schema_version = %v, want 1", doc["schema_version"])
				}
				products, ok := doc["products"].([]any)
				if !ok || len(products) != 1 {
					t.Fatalf("products = %v, want one entry", doc["products"])
				}
				product, ok := products[0].(map[string]any)
				if !ok {
					t.Fatalf("products[0] is %T, want object", products[0])
				}
				if product["product"] != "instrumentation" || product["enabled"] != true {
					t.Fatalf("unexpected product row: %v", product)
				}
			},
		},
		{
			name:     "explicit -o json wins in human mode",
			enabled:  true,
			clusters: 3,
			output:   "json",
			check: func(t *testing.T, stdout string) {
				t.Helper()
				doc := decodeSingleJSONValue(t, stdout)
				if doc["type"] != "gcx.setup.status" {
					t.Fatalf("type = %v, want gcx.setup.status", doc["type"])
				}
			},
		},
		{
			name:      "explicit -o yaml wins in agent mode",
			agentMode: true,
			enabled:   true,
			clusters:  3,
			output:    "yaml",
			check: func(t *testing.T, stdout string) {
				t.Helper()
				if !strings.Contains(stdout, "type: gcx.setup.status") ||
					!strings.Contains(stdout, "product: instrumentation") {
					t.Fatalf("yaml output missing expected fields:\n%s", stdout)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			agent.SetFlag(tc.agentMode)
			t.Cleanup(func() { agent.SetFlag(false) })

			flags := pflag.NewFlagSet("status", pflag.ContinueOnError)
			opts := setup.NewStatusOptsForTest(flags)
			if tc.output != "" {
				if err := flags.Set("output", tc.output); err != nil {
					t.Fatalf("set -o %s: %v", tc.output, err)
				}
			}
			if err := opts.Validate(); err != nil {
				t.Fatalf("Validate() = %v", err)
			}

			var stdout, stderr bytes.Buffer
			opts.IO.ErrWriter = &stderr

			doc := setup.StatusDocForTest(tc.enabled, tc.clusters)
			if err := opts.IO.Encode(&stdout, doc); err != nil {
				t.Fatalf("Encode() = %v", err)
			}

			if tc.wantStdout != "" {
				if stdout.String() != tc.wantStdout {
					t.Fatalf("stdout not byte-identical:\ngot:  %q\nwant: %q", stdout.String(), tc.wantStdout)
				}
				return
			}
			tc.check(t, stdout.String())
		})
	}
}

// decodeSingleJSONValue asserts that raw holds exactly one JSON object
// followed by EOF, and returns it decoded.
func decodeSingleJSONValue(t *testing.T, raw string) map[string]any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(raw))
	var first any
	if err := dec.Decode(&first); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, raw)
	}
	var second any
	if err := dec.Decode(&second); !errors.Is(err, io.EOF) {
		t.Fatalf("stdout must contain exactly one JSON value, second decode = %v\n%s", err, raw)
	}
	doc, ok := first.(map[string]any)
	if !ok {
		t.Fatalf("document is %T, want object", first)
	}
	return doc
}
