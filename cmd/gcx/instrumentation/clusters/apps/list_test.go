//nolint:testpackage // tests require access to unexported fakeAppsClient
package apps

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	instoutput "github.com/grafana/gcx/internal/providers/instrumentation/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListCmd(t *testing.T) {
	tests := []struct {
		name       string
		namespaces []instrumentation.App
		wantRows   []string
		wantErr    bool
	}{
		{
			name:       "empty cluster returns empty table",
			namespaces: nil,
			wantRows:   nil, // header only
		},
		{
			name:       "single namespace",
			namespaces: buildNamespaces(true, "grotshop"),
			wantRows:   []string{"grotshop"},
		},
		{
			name:       "multiple namespaces",
			namespaces: buildNamespaces(true, "ns-a", "ns-b", "ns-c"),
			wantRows:   []string{"ns-a", "ns-b", "ns-c"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeAppsClient{
				getResponses: []getResponse{{namespaces: tc.namespaces}},
			}

			cmd := newListCmd(client)
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs([]string{"c1"})

			err := cmd.Execute()
			if (err != nil) != tc.wantErr {
				t.Fatalf("unexpected error: %v", err)
			}

			output := out.String()
			for _, row := range tc.wantRows {
				if !bytes.Contains(out.Bytes(), []byte(row)) {
					t.Errorf("expected row %q in output, got:\n%s", row, output)
				}
			}
		})
	}
}

// TestListCmd_JSONEnvelope_Empty verifies empty apps list outputs
// {"items":[]} not [] or null.
func TestListCmd_JSONEnvelope_Empty(t *testing.T) {
	client := &fakeAppsClient{
		getResponses: []getResponse{{namespaces: nil}},
	}

	cmd := newListCmd(client)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--output", "json", "c1"})

	require.NoError(t, cmd.Execute())
	assert.JSONEq(t, `{"items":[]}`, out.String())
}

// TestListCmd_JSONEnvelope_NonEmpty verifies non-empty apps list
// wraps results in {"items":[...]} envelope.
func TestListCmd_JSONEnvelope_NonEmpty(t *testing.T) {
	client := &fakeAppsClient{
		getResponses: []getResponse{{namespaces: buildNamespaces(true, "checkout")}},
	}

	cmd := newListCmd(client)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--output", "json", "c1"})

	require.NoError(t, cmd.Execute())

	var got map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &got), "output must be a JSON object; got: %s", out.String())
	items, ok := got["items"]
	require.True(t, ok, "output must have 'items' key")
	itemsSlice, ok := items.([]any)
	require.True(t, ok)
	assert.Len(t, itemsSlice, 1)
}

// TestListCmd_Discovered verifies apps list populates the discovered field
// from RunK8sDiscovery — items whose namespace appears in the discovery response get
// discovered:true; others get discovered:false.
func TestListCmd_Discovered(t *testing.T) {
	client := &fakeAppsClient{
		getResponses: []getResponse{{namespaces: buildNamespaces(true, "checkout", "payments")}},
		discoverItems: []instrumentation.DiscoveryItem{
			{ClusterName: "c1", Namespace: "checkout"},
			// "payments" intentionally absent — should surface as discovered:false.
			{ClusterName: "other-cluster", Namespace: "payments"}, // different cluster — must not count.
		},
	}

	cmd := newListCmd(client)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--output", "json", "c1"})

	require.NoError(t, cmd.Execute())

	var envelope instoutput.AppListEnvelope
	require.NoError(t, json.Unmarshal(out.Bytes(), &envelope), "output must be AppListEnvelope; got: %s", out.String())
	require.Len(t, envelope.Items, 2)

	byName := make(map[string]instoutput.AppView, len(envelope.Items))
	for _, item := range envelope.Items {
		byName[item.Name] = item
	}

	assert.True(t, byName["checkout"].Discovered, "checkout is in discoverItems for c1 — should be discovered:true")
	assert.False(t, byName["payments"].Discovered, "payments is absent from c1 discoverItems — should be discovered:false")
}

// TestListCmd_JSONFieldSelection_Unknown verifies --json bogus,name
// on apps list returns UnknownFieldSelectionError.
func TestListCmd_JSONFieldSelection_Unknown(t *testing.T) {
	client := &fakeAppsClient{
		getResponses: []getResponse{{namespaces: buildNamespaces(true, "checkout")}},
	}

	cmd := newListCmd(client)
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"--json", "bogus,name", "c1"})

	err := cmd.Execute()
	require.Error(t, err)
	var fieldErr cmdio.UnknownFieldSelectionError
	require.ErrorAs(t, err, &fieldErr)
	assert.Contains(t, fieldErr.Fields, "bogus")
}

// TestListCmd_JSONFieldSelection_Valid verifies that --json with valid fields
// applies per-item projection wrapped in {"items":[...]}.
func TestListCmd_JSONFieldSelection_Valid(t *testing.T) {
	client := &fakeAppsClient{
		getResponses: []getResponse{{namespaces: buildNamespaces(true, "checkout")}},
	}

	cmd := newListCmd(client)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--json", "name", "c1"})

	require.NoError(t, cmd.Execute())

	var got map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &got), "output must be JSON object; got: %s", out.String())
	items, ok := got["items"]
	require.True(t, ok, "output must have 'items' key")
	itemsSlice, ok := items.([]any)
	require.True(t, ok)
	require.Len(t, itemsSlice, 1)
	firstItem, ok := itemsSlice[0].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, firstItem, "name")
	assert.NotContains(t, firstItem, "autoInstrument")
}

// TestListCmd_JSONList verifies that --json list behavior is unchanged (returns
// field names, one per line, not JSON).
func TestListCmd_JSONList(t *testing.T) {
	client := &fakeAppsClient{
		getResponses: []getResponse{{namespaces: buildNamespaces(true, "checkout")}},
	}

	cmd := newListCmd(client)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--json", "list", "c1"})

	require.NoError(t, cmd.Execute())

	// Output must NOT be JSON; must be field names one per line.
	var decoded any
	require.Error(t, json.Unmarshal(out.Bytes(), &decoded), "--json list output must not be JSON")

	// Must contain known AppView field names.
	fields := strings.Split(strings.TrimSpace(out.String()), "\n")
	fieldSet := make(map[string]bool, len(fields))
	for _, f := range fields {
		fieldSet[strings.TrimSpace(f)] = true
	}
	// All discovered fields from AppView{} must be accepted by --json without error.
	validator := cmdio.MakeFieldValidator(instoutput.AppView{})
	require.NotNil(t, validator)
	require.NoError(t, validator(fields), "all --json list fields must pass the validator")
}

// TestListCmd_JSONDiscovery_NoClusterArg verifies that --json list with no
// cluster positional exits 0, prints non-empty discovery output, and never
// invokes the API client.
//
// AC-F2-1 / AC-F2-4.
func TestListCmd_JSONDiscovery_NoClusterArg(t *testing.T) {
	client := &fakeAppsClient{}

	cmd := newListCmd(client)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--json", "list"})

	require.NoError(t, cmd.Execute(), "expected exit 0 for --json list with no cluster arg")

	// Output must be non-empty field discovery output (not JSON).
	require.NotEmpty(t, out.String(), "expected non-empty discovery output")
	var decoded any
	require.Error(t, json.Unmarshal(out.Bytes(), &decoded), "--json list output must not be JSON")

	// Output must contain at least one known AppView field.
	assert.Contains(t, out.String(), "name", "expected 'name' in discovery output")

	// API client must NEVER have been called.
	client.mu.Lock()
	defer client.mu.Unlock()
	assert.Equal(t, 0, client.getCalls, "GetAppInstrumentation must not be called for --json list discovery")
}

// TestListCmd_NoArgs_NormalOutput verifies that running the command with no
// args and no --json flag returns a cobra-style validation error and exits 1.
//
// AC-F2-2.
func TestListCmd_NoArgs_NormalOutput(t *testing.T) {
	client := &fakeAppsClient{}

	cmd := newListCmd(client)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err, "expected error when no cluster arg is given in normal mode")
	// Error text must match cobra's prior ExactArgs(1) verbatim phrasing.
	assert.Equal(t, "accepts 1 arg(s), received 0", err.Error())
}

// TestListCmd_TooManyArgs verifies that two positionals are rejected by cobra's
// MaximumNArgs(1) validator with the expected error message.
//
// AC-F2-2 (upper-bound).
func TestListCmd_TooManyArgs(t *testing.T) {
	client := &fakeAppsClient{}

	cmd := newListCmd(client)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"a", "b"})

	err := cmd.Execute()
	require.Error(t, err, "expected error when two positional args are given")
	// Error text must match cobra's MaximumNArgs(1) verbatim phrasing.
	assert.Equal(t, "accepts at most 1 arg(s), received 2", err.Error())
}
