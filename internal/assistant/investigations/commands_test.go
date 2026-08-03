package investigations_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/assistant/investigations"
	"github.com/grafana/gcx/internal/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListTableCodec_Encode(t *testing.T) {
	summaries := []investigations.InvestigationSummary{
		{
			ID:        "inv-1",
			Title:     "High CPU investigation",
			State:     "running",
			Source:    &investigations.Source{UserID: "admin"},
			CreatedAt: time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
		},
		{
			ID:    "inv-2",
			Title: "",
			State: "completed",
		},
	}

	t.Run("table", func(t *testing.T) {
		codec := &investigations.ListTableCodec{}
		assert.Equal(t, "table", string(codec.Format()))

		var buf bytes.Buffer
		require.NoError(t, codec.Encode(&buf, summaries))
		out := buf.String()
		assert.Contains(t, out, "ID")
		assert.Contains(t, out, "TITLE")
		assert.Contains(t, out, "STATUS")
		assert.Contains(t, out, "UPDATED")
		assert.NotContains(t, out, "CREATED BY")
		assert.Contains(t, out, "inv-1")
		assert.Contains(t, out, "High CPU investigation")
		assert.Contains(t, out, "-") // empty title
	})

	t.Run("wide", func(t *testing.T) {
		codec := &investigations.ListTableCodec{Wide: true}
		assert.Equal(t, "wide", string(codec.Format()))

		var buf bytes.Buffer
		require.NoError(t, codec.Encode(&buf, summaries))
		out := buf.String()
		assert.Contains(t, out, "CREATED BY")
		assert.Contains(t, out, "CREATED")
		assert.Contains(t, out, "admin")
	})

	t.Run("wrong type", func(t *testing.T) {
		codec := &investigations.ListTableCodec{}
		err := codec.Encode(&bytes.Buffer{}, "wrong")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected []InvestigationSummary or *LodestoneList")
	})

	t.Run("decode unsupported", func(t *testing.T) {
		codec := &investigations.ListTableCodec{}
		require.Error(t, codec.Decode(nil, nil))
	})
}

func TestListTableCodec_EncodeLodestone(t *testing.T) {
	list := &investigations.LodestoneList{
		Investigations: []investigations.LodestoneInvestigationSummary{
			{
				ID:        "inv-1",
				Title:     "High CPU investigation",
				State:     "in_progress",
				ChatID:    "chat-1",
				Source:    &investigations.LodestoneSource{UserID: "admin"},
				CreatedAt: time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC),
				UpdatedAt: time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
			},
			{
				ID:          "inv-2",
				State:       "completed",
				OwnerUserID: "owner-2",
			},
		},
		Total: 2,
	}

	t.Run("table", func(t *testing.T) {
		codec := &investigations.ListTableCodec{}
		var buf bytes.Buffer
		require.NoError(t, codec.Encode(&buf, list))
		out := buf.String()
		assert.Contains(t, out, "ID")
		assert.Contains(t, out, "STATUS")
		assert.NotContains(t, out, "CREATED BY")
		assert.Contains(t, out, "inv-1")
		assert.Contains(t, out, "High CPU investigation")
		assert.Contains(t, out, "inv-2")
	})

	t.Run("wide falls back to owner when source is absent", func(t *testing.T) {
		codec := &investigations.ListTableCodec{Wide: true}
		var buf bytes.Buffer
		require.NoError(t, codec.Encode(&buf, list))
		out := buf.String()
		assert.Contains(t, out, "CREATED BY")
		assert.Contains(t, out, "admin")   // inv-1: source.userId wins
		assert.Contains(t, out, "owner-2") // inv-2: ownerUserId fallback
	})
}

func TestListTableCodec_TitleTruncation(t *testing.T) {
	summaries := []investigations.InvestigationSummary{
		{
			ID:    "inv-1",
			Title: "This is a very long title that should be truncated at forty characters",
			State: "running",
		},
	}

	var buf bytes.Buffer
	codec := &investigations.ListTableCodec{}
	require.NoError(t, codec.Encode(&buf, summaries))
	assert.Contains(t, buf.String(), "...")
}

func TestEvidenceTableCodec_Encode(t *testing.T) {
	resp := &investigations.EvidenceResponse{
		Evidence: []investigations.EvidenceItem{
			{
				PanelID:   "p3",
				Tool:      "prometheus",
				Query:     "rate(http_requests_total{job=\"api\",cluster=\"prod\"}[5m])",
				Epoch:     2,
				Time:      "2026-07-20T10:00:00Z",
				ToolUseID: "toolu_1",
			},
			{PanelID: "p4", Tool: "loki", Query: "short", Epoch: 3, Time: "2026-07-20T10:05:00Z"},
		},
	}

	t.Run("table", func(t *testing.T) {
		codec := &investigations.EvidenceTableCodec{}
		assert.Equal(t, "table", string(codec.Format()))

		var buf bytes.Buffer
		require.NoError(t, codec.Encode(&buf, resp))
		out := buf.String()
		assert.Contains(t, out, "PANEL")
		assert.Contains(t, out, "TOOL")
		assert.Contains(t, out, "QUERY")
		assert.Contains(t, out, "EPOCH")
		assert.Contains(t, out, "TIME")
		assert.NotContains(t, out, "TOOL USE ID")
		assert.NotContains(t, out, "toolu_1")
		assert.Contains(t, out, "p3")
		assert.Contains(t, out, "prometheus")
		// Long queries are truncated at 40 runes in the default table.
		assert.Contains(t, out, "...")
		assert.NotContains(t, out, "cluster=\"prod\"")
	})

	t.Run("wide", func(t *testing.T) {
		codec := &investigations.EvidenceTableCodec{Wide: true}
		assert.Equal(t, "wide", string(codec.Format()))

		var buf bytes.Buffer
		require.NoError(t, codec.Encode(&buf, resp))
		out := buf.String()
		assert.Contains(t, out, "TOOL USE ID")
		assert.Contains(t, out, "toolu_1")
		// Wide shows the full query and "-" for a missing tool use ID.
		assert.Contains(t, out, "rate(http_requests_total{job=\"api\",cluster=\"prod\"}[5m])")
		assert.Regexp(t, `(?m)^p4\s+loki\s+short\s+3\s+2026-07-20T10:05:00Z\s+-$`, out)
	})

	t.Run("multi-line query flattened", func(t *testing.T) {
		multiline := &investigations.EvidenceResponse{
			Evidence: []investigations.EvidenceItem{
				{PanelID: "p1", Tool: "prometheus", Query: "sum by (pod) (\n\trate(errors_total[5m])\n)", Epoch: 1, Time: "2026-07-20T10:00:00Z"},
			},
		}
		for _, codec := range []*investigations.EvidenceTableCodec{{}, {Wide: true}} {
			var buf bytes.Buffer
			require.NoError(t, codec.Encode(&buf, multiline))
			out := buf.String()
			// Header + one row: embedded newlines/tabs must not split the row.
			assert.Len(t, strings.Split(strings.TrimRight(out, "\n"), "\n"), 2)
			if codec.Wide {
				assert.Contains(t, out, "sum by (pod) ( rate(errors_total[5m]) )")
			}
		}
	})

	t.Run("empty", func(t *testing.T) {
		codec := &investigations.EvidenceTableCodec{}
		var buf bytes.Buffer
		require.NoError(t, codec.Encode(&buf, &investigations.EvidenceResponse{Evidence: []investigations.EvidenceItem{}}))
		assert.Contains(t, buf.String(), "PANEL")
	})

	t.Run("wrong type", func(t *testing.T) {
		codec := &investigations.EvidenceTableCodec{}
		err := codec.Encode(&bytes.Buffer{}, "wrong")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected *EvidenceResponse")
	})

	t.Run("decode unsupported", func(t *testing.T) {
		codec := &investigations.EvidenceTableCodec{}
		require.Error(t, codec.Decode(nil, nil))
	})
}

// --- get command: v2 identifier merge ---

const v1InvestigationsPath = "/api/plugins/grafana-assistant-app/resources/api/v1/investigations"

// newGetLoader builds a ConfigLoader whose current context points at the
// given httptest handler, so the get command's full path (mode detection,
// resolve, snapshot/legacy fetch) runs against a fake stack.
func newGetLoader(t *testing.T, handler http.Handler) *providers.ConfigLoader {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	cfgFile := filepath.Join(t.TempDir(), "config.yaml")
	cfg := fmt.Sprintf("contexts:\n  default:\n    grafana:\n      server: %s\n      org-id: 1\ncurrent-context: default\n", server.URL)
	require.NoError(t, os.WriteFile(cfgFile, []byte(cfg), 0o600))
	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgFile)
	return loader
}

// runGetJSON executes `investigations get <id> -o json` and returns the
// decoded output object.
func runGetJSON(t *testing.T, loader *providers.ConfigLoader, id string) map[string]any {
	t.Helper()
	cmd := investigations.Commands(loader)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"get", id, "-o", "json"})
	require.NoError(t, cmd.Execute())

	var got map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &got))
	return got
}

// TestGetCommand_V2ExposesBothIdentifiers verifies that on a v2 stack, get
// output carries both investigationId (from the snapshot) and the backing
// chatId (from the resolve step, which the snapshot itself does not include).
func TestGetCommand_V2ExposesBothIdentifiers(t *testing.T) {
	t.Setenv("GCX_ASSISTANT_API_VERSION", "v2")
	t.Setenv("GCX_AGENT_MODE", "false")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case v2InvestigationsPath + "/inv-1":
			writeJSON(w, map[string]any{
				"data": investigations.ResolveByIDResponse{InvestigationID: "inv-1", ChatID: "chat-1"},
			})
		case v2InvestigationsPath + "/inv-1/snapshot":
			writeJSON(w, map[string]any{
				"data": investigations.LodestoneState{"investigationId": "inv-1", "sessionStatus": "active"},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	got := runGetJSON(t, newGetLoader(t, handler), "inv-1")
	assert.Equal(t, "inv-1", got["investigationId"])
	assert.Equal(t, "chat-1", got["chatId"])
}

// TestGetCommand_V2ServerProvidedChatIDWins verifies the client-side chatId
// injection never clobbers a chatId the snapshot already carries.
func TestGetCommand_V2ServerProvidedChatIDWins(t *testing.T) {
	t.Setenv("GCX_ASSISTANT_API_VERSION", "v2")
	t.Setenv("GCX_AGENT_MODE", "false")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case v2InvestigationsPath + "/inv-1":
			writeJSON(w, map[string]any{
				"data": investigations.ResolveByIDResponse{InvestigationID: "inv-1", ChatID: "chat-resolved"},
			})
		case v2InvestigationsPath + "/inv-1/snapshot":
			writeJSON(w, map[string]any{
				"data": investigations.LodestoneState{"investigationId": "inv-1", "chatId": "chat-server"},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	got := runGetJSON(t, newGetLoader(t, handler), "inv-1")
	assert.Equal(t, "chat-server", got["chatId"])
}

// TestGetCommand_V1FallbackUnchanged verifies that when resolve returns 404
// (not a v2 investigation), get falls back to legacy detail verbatim — no
// chatId injection.
func TestGetCommand_V1FallbackUnchanged(t *testing.T) {
	t.Setenv("GCX_ASSISTANT_API_VERSION", "v2")
	t.Setenv("GCX_AGENT_MODE", "false")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case v2InvestigationsPath + "/inv-legacy":
			w.WriteHeader(http.StatusNotFound)
		case v1InvestigationsPath + "/inv-legacy":
			writeJSON(w, map[string]any{
				"data": investigations.Investigation{"id": "inv-legacy", "title": "Legacy detail"},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	got := runGetJSON(t, newGetLoader(t, handler), "inv-legacy")
	assert.Equal(t, map[string]any{"id": "inv-legacy", "title": "Legacy detail"}, got)
	assert.NotContains(t, got, "chatId")
}

// TestGetCommand_V2NilSnapshotDoesNotPanic verifies a 200 snapshot response
// with a null data envelope (decoded as a nil map) doesn't panic on the
// chatId injection and still surfaces the resolved chatId.
func TestGetCommand_V2NilSnapshotDoesNotPanic(t *testing.T) {
	t.Setenv("GCX_ASSISTANT_API_VERSION", "v2")
	t.Setenv("GCX_AGENT_MODE", "false")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case v2InvestigationsPath + "/inv-1":
			writeJSON(w, map[string]any{
				"data": investigations.ResolveByIDResponse{InvestigationID: "inv-1", ChatID: "chat-1"},
			})
		case v2InvestigationsPath + "/inv-1/snapshot":
			writeJSON(w, map[string]any{"data": nil})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	got := runGetJSON(t, newGetLoader(t, handler), "inv-1")
	assert.Equal(t, map[string]any{"chatId": "chat-1"}, got)
}
