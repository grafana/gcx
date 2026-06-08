package investigations_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/grafana/gcx/internal/assistant/investigations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const v2InvestigationsPath = "/api/plugins/grafana-assistant-app/resources/api/v2/investigations"

func TestCreateLodestone(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, v2InvestigationsPath, r.URL.Path)
		body, _ := io.ReadAll(r.Body)
		var req investigations.CreateLodestoneRequest
		assert.NoError(t, json.Unmarshal(body, &req))
		assert.Equal(t, "Debug API latency", req.Instruction)
		assert.Equal(t, []string{"sre"}, req.TeamNames)

		w.WriteHeader(http.StatusOK)
		writeJSON(w, map[string]any{
			"data": investigations.CreateLodestoneResponse{
				InvestigationID: "inv-1", ChatID: "chat-1", AgentProfileID: "default",
			},
		})
	}))

	resp, err := client.CreateLodestone(t.Context(), investigations.CreateLodestoneRequest{
		Instruction: "Debug API latency",
		TeamNames:   []string{"sre"},
	})
	require.NoError(t, err)
	assert.Equal(t, "inv-1", resp.InvestigationID)
	assert.Equal(t, "chat-1", resp.ChatID)
}

func TestListLodestone(t *testing.T) {
	tests := []struct {
		name      string
		opts      investigations.ListLodestoneOptions
		assertReq func(t *testing.T, r *http.Request)
		wantCount int
	}{
		{
			name: "default",
			assertReq: func(t *testing.T, r *http.Request) {
				t.Helper()
				assert.Empty(t, r.URL.RawQuery)
			},
			wantCount: 1,
		},
		{
			name: "all filters",
			opts: investigations.ListLodestoneOptions{
				State: "in_progress", Q: "latency", Scope: "teams",
				TeamName: "sre", From: "2026-01-01T00:00:00Z", To: "2026-01-02T00:00:00Z",
				Sort: "updatedAt", Order: "desc", View: "full", Label: "env:prod",
				Limit: 10, Offset: 20, IncludeLegacy: true,
			},
			assertReq: func(t *testing.T, r *http.Request) {
				t.Helper()
				q := r.URL.Query()
				assert.Equal(t, "in_progress", q.Get("state"))
				assert.Equal(t, "latency", q.Get("q"))
				assert.Equal(t, "teams", q.Get("scope"))
				assert.Equal(t, "sre", q.Get("teamName"))
				assert.Equal(t, "desc", q.Get("order"))
				assert.Equal(t, "true", q.Get("includeLegacy"))
				assert.Equal(t, "10", q.Get("limit"))
				assert.Equal(t, "20", q.Get("offset"))
			},
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, v2InvestigationsPath, r.URL.Path)
				tt.assertReq(t, r)
				writeJSON(w, map[string]any{
					"data": map[string]any{
						"investigations": []investigations.InvestigationSummary{
							{ID: "inv-1", Title: "x", State: "in_progress"},
						},
					},
				})
			}))
			out, err := client.ListLodestone(t.Context(), tt.opts)
			require.NoError(t, err)
			assert.Len(t, out, tt.wantCount)
		})
	}
}

func TestResolveByID(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, v2InvestigationsPath+"/inv-1", r.URL.Path)
			writeJSON(w, map[string]any{
				"data": investigations.ResolveByIDResponse{InvestigationID: "inv-1", ChatID: "chat-1"},
			})
		}))
		resp, status, err := client.ResolveByID(t.Context(), "inv-1")
		require.NoError(t, err)
		assert.Equal(t, "chat-1", resp.ChatID)
		assert.Equal(t, "inv-1", resp.InvestigationID)
		assert.Equal(t, http.StatusOK, status)
	})

	t.Run("not found", func(t *testing.T) {
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		resp, status, err := client.ResolveByID(t.Context(), "inv-1")
		require.NoError(t, err)
		assert.Empty(t, resp.ChatID)
		assert.Equal(t, http.StatusNotFound, status)
	})
}

func TestGetState(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, v2InvestigationsPath+"/inv-1/snapshot", r.URL.Path)
		writeJSON(w, map[string]any{
			"data": investigations.LodestoneState{"sessionStatus": "active", "mode": "medium", "epoch": 3},
		})
	}))
	state, err := client.GetState(t.Context(), "inv-1")
	require.NoError(t, err)
	assert.Equal(t, "active", state["sessionStatus"])
	assert.Equal(t, "medium", state["mode"])
}

func TestPauseResumeRegenerate(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		call     func(c *investigations.Client) (*investigations.Message, error)
	}{
		{
			name:     "pause",
			expected: v2InvestigationsPath + "/inv-1/pause",
			call:     func(c *investigations.Client) (*investigations.Message, error) { return c.Pause(t.Context(), "inv-1") },
		},
		{
			name:     "resume",
			expected: v2InvestigationsPath + "/inv-1/resume",
			call: func(c *investigations.Client) (*investigations.Message, error) {
				return c.Resume(t.Context(), "inv-1")
			},
		},
		{
			name:     "regenerate-report",
			expected: v2InvestigationsPath + "/inv-1/report/regenerate",
			call: func(c *investigations.Client) (*investigations.Message, error) {
				return c.RegenerateReport(t.Context(), "inv-1")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, tt.expected, r.URL.Path)
				writeJSON(w, map[string]any{"data": investigations.Message{Message: tt.name + " ok"}})
			}))
			msg, err := tt.call(client)
			require.NoError(t, err)
			assert.Contains(t, msg.Message, "ok")
		})
	}
}

func TestSetMode(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, v2InvestigationsPath+"/inv-1/mode", r.URL.Path)
		body, _ := io.ReadAll(r.Body)
		var req investigations.ModeRequest
		assert.NoError(t, json.Unmarshal(body, &req))
		assert.Equal(t, "high", req.Mode)
		writeJSON(w, map[string]any{
			"data": investigations.ModeResponse{Message: "Mode updated.", Mode: "high"},
		})
	}))
	resp, err := client.SetMode(t.Context(), "inv-1", "high")
	require.NoError(t, err)
	assert.Equal(t, "high", resp.Mode)
}

func TestScope(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, v2InvestigationsPath+"/inv-1/share", r.URL.Path)
		body, _ := io.ReadAll(r.Body)
		var req investigations.ScopeRequest
		assert.NoError(t, json.Unmarshal(body, &req))
		assert.Equal(t, []string{"sre", "ops"}, req.TeamNames)
		writeJSON(w, map[string]any{
			"data": investigations.ScopeResponse{
				InvestigationID: "inv-1", TeamNames: []string{"sre", "ops"}, AddedTeamNames: []string{"ops"},
			},
		})
	}))
	resp, err := client.Scope(t.Context(), "inv-1", []string{"sre", "ops"})
	require.NoError(t, err)
	assert.Equal(t, []string{"ops"}, resp.AddedTeamNames)
}

func TestV2_ServerError(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	_, _, err := client.ResolveByID(t.Context(), "inv-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}
