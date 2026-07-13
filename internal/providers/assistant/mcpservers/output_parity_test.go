package mcpservers //nolint:testpackage

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/assistant/mcpserver"
	assistantmcp "github.com/grafana/gcx/internal/assistant/mcpservers"
	"github.com/grafana/gcx/internal/config"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

const parityCollectionPath = "/api/plugins/grafana-assistant-app/resources/api/v1/integrations"

// newParityTestClient serves the fixed set of integrations from the
// collection endpoint and 404s any by-ref lookup, forcing Client.Get to fall
// through to its list+name-match path -- matching how a real Assistant
// backend rejects a non-ID reference.
func newParityTestClient(t *testing.T, integrations []map[string]any) *assistantmcp.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != parityCollectionPath {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"integrations": integrations},
		}))
	}))
	t.Cleanup(server.Close)

	cfg := config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}, Namespace: "default"}
	base, err := assistanthttp.NewClient(cfg)
	require.NoError(t, err)
	return assistantmcp.NewClient(base)
}

// resourcesPathBytes replicates cmd/gcx/resources/get.go's exact single-target
// encoding for a resource fetched through the registered TypedCRUD adapter:
// output.Items[0].Object (the bare unstructured envelope) encoded via the
// shared cmdio.Options codec -- the same encoder gcx resources get uses.
func resourcesPathBytes(t *testing.T, client *assistantmcp.Client, namespace, name, outputFormat string) []byte {
	t.Helper()
	crud := mcpserver.NewTypedCRUDForClient(client, namespace)
	obj, err := crud.AsAdapter().Get(t.Context(), name, metav1.GetOptions{})
	require.NoError(t, err)

	var buf bytes.Buffer
	io := cmdio.Options{OutputFormat: outputFormat}
	require.NoError(t, io.Encode(&buf, obj.Object))
	return buf.Bytes()
}

// TestGetOutputParityWithResourcesPath: the JSON and
// YAML output of "gcx assistant mcp-servers get <ref>" must be byte-identical
// to "gcx resources get mcpservers/<name>" for the same underlying server.
func TestGetOutputParityWithResourcesPath(t *testing.T) {
	client := newParityTestClient(t, []map[string]any{
		{
			"id": "srv-abc123", "name": "GitHub", "type": "mcp", "enabled": true, "scope": "tenant",
			"configuration":  map[string]any{"url": "https://api.githubcopilot.com/mcp/", "timeout": "30s"},
			"custom_headers": []map[string]any{{"name": "Authorization"}},
		},
	})

	for _, outputFormat := range []string{"json", "yaml"} {
		t.Run(outputFormat, func(t *testing.T) {
			opts := &getOpts{}
			opts.setup(pflag.NewFlagSet("get", pflag.ContinueOnError))
			opts.IO.OutputFormat = outputFormat

			cmd := &cobra.Command{}
			cmd.SetContext(t.Context())
			var out bytes.Buffer
			cmd.SetOut(&out)

			require.NoError(t, runGet(cmd, client, "default", opts, "GitHub"))

			want := resourcesPathBytes(t, client, "default", "tenant-github", outputFormat)
			require.Equal(t, string(want), out.String(),
				"gcx assistant mcp-servers get -o %s must be byte-identical to gcx resources get mcpservers/<name> -o %s",
				outputFormat, outputFormat)
		})
	}
}

// TestListOutputParityWithResourcesPath covers the list path: the
// JSON/YAML "items" envelope produced by "gcx assistant mcp-servers list"
// must match the per-item envelope shape "gcx resources get mcpservers"
// would render for each of the same underlying servers.
func TestListOutputParityWithResourcesPath(t *testing.T) {
	client := newParityTestClient(t, []map[string]any{
		{"id": "srv-user1", "name": "My Server", "type": "mcp", "enabled": true, "scope": "user",
			"configuration": map[string]any{"url": "https://mcp.example.com/user"}},
		{
			"id": "srv-abc123", "name": "GitHub", "type": "mcp", "enabled": true, "scope": "tenant",
			"configuration":  map[string]any{"url": "https://api.githubcopilot.com/mcp/", "timeout": "30s"},
			"custom_headers": []map[string]any{{"name": "Authorization"}},
		},
	})

	for _, outputFormat := range []string{"json", "yaml"} {
		t.Run(outputFormat, func(t *testing.T) {
			opts := &listOpts{}
			opts.setup(pflag.NewFlagSet("list", pflag.ContinueOnError))
			opts.IO.OutputFormat = outputFormat
			opts.Limit = 50

			cmd := &cobra.Command{}
			cmd.SetContext(t.Context())
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&bytes.Buffer{})

			require.NoError(t, runList(cmd, client, "default", opts))

			wantUser := resourcesPathBytesAsMap(t, client, "default", "user-my-server", outputFormat)
			wantGitHub := resourcesPathBytesAsMap(t, client, "default", "tenant-github", outputFormat)

			var got envelopeList
			decodeInto(t, outputFormat, out.Bytes(), &got)
			require.Len(t, got.Items, 2)
			require.Equal(t, wantUser, got.Items[0])
			require.Equal(t, wantGitHub, got.Items[1])
		})
	}
}

// resourcesPathBytesAsMap decodes resourcesPathBytes back into a map so list
// items (which are re-encoded inside an "items" wrapper) can be compared by
// value rather than by exact wrapper bytes.
func resourcesPathBytesAsMap(t *testing.T, client *assistantmcp.Client, namespace, name, outputFormat string) map[string]any {
	t.Helper()
	data := resourcesPathBytes(t, client, namespace, name, outputFormat)
	var m map[string]any
	decodeInto(t, outputFormat, data, &m)
	return m
}

func decodeInto(t *testing.T, outputFormat string, data []byte, v any) {
	t.Helper()
	io := cmdio.Options{OutputFormat: outputFormat}
	codec, err := io.Codec()
	require.NoError(t, err)
	require.NoError(t, codec.Decode(bytes.NewReader(data), v))
}
