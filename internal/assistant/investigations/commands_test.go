package investigations_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
