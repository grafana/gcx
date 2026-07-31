package investigations_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

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
						"investigations": []map[string]any{
							{"id": "inv-1", "title": "x", "state": "in_progress"},
						},
						"total": 42,
					},
				})
			}))
			out, err := client.ListLodestone(t.Context(), tt.opts)
			require.NoError(t, err)
			assert.Len(t, out.Investigations, tt.wantCount)
			assert.Equal(t, int64(42), out.Total)
		})
	}
}

// TestListLodestone_LosslessSummary pins the full v2 summary shape: every
// field the server emits must survive the decode so json/yaml output is
// lossless (gcx#997).
func TestListLodestone_LosslessSummary(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"data": map[string]any{
				"investigations": []map[string]any{{
					"id":          "inv-1",
					"title":       "High latency",
					"description": "p99 spiked",
					"state":       "in_progress",
					"chatId":      "chat-1",
					"variant":     "lodestone",
					"createdAt":   "2026-07-01T10:00:00Z",
					"updatedAt":   "2026-07-01T12:00:00Z",
					"tokensUsed":  1234,
					"source": map[string]any{
						"type": "user", "value": "https://hook", "prompt": "check latency",
						"chatId": "chat-1", "userId": "admin",
					},
					"agents": []map[string]any{{
						"id": "agent-1", "name": "lead", "task": "investigate",
						"finalMessage": "done", "status": "completed", "audience": "user",
						"createdAt": "2026-07-01T10:00:00Z", "updatedAt": "2026-07-01T11:00:00Z",
						"tokensPerSecondHistory": []float64{10.5, 12.0},
						"tokenCounter":           int64(999),
						"outputPreview":          "tail of output",
					}},
					"progress": map[string]any{
						"pending": 1, "inProgress": 2, "completed": 3, "canceled": 4, "total": 10,
					},
					"completionQuality": "degraded",
					"degradedReason":    "token budget exhausted",
					"labels":            map[string]string{"env": "prod"},
					"ownerUserId":       "owner-1",
					"activeLoopCount":   2,
				}},
				"total": 1,
			},
		})
	}))

	out, err := client.ListLodestone(t.Context(), investigations.ListLodestoneOptions{})
	require.NoError(t, err)
	require.Len(t, out.Investigations, 1)
	assert.Equal(t, int64(1), out.Total)

	s := out.Investigations[0]
	assert.Equal(t, "inv-1", s.ID)
	assert.Equal(t, "High latency", s.Title)
	assert.Equal(t, "p99 spiked", s.Description)
	assert.Equal(t, "in_progress", s.State)
	assert.Equal(t, "chat-1", s.ChatID)
	assert.Equal(t, "lodestone", s.Variant)
	assert.Equal(t, "2026-07-01T10:00:00Z", s.CreatedAt.Format(time.RFC3339))
	assert.Equal(t, "2026-07-01T12:00:00Z", s.UpdatedAt.Format(time.RFC3339))
	require.NotNil(t, s.TokensUsed)
	assert.Equal(t, 1234, *s.TokensUsed)
	require.NotNil(t, s.Source)
	assert.Equal(t, investigations.LodestoneSource{
		Type: "user", Value: "https://hook", Prompt: "check latency",
		ChatID: "chat-1", UserID: "admin",
	}, *s.Source)
	require.Len(t, s.Agents, 1)
	a := s.Agents[0]
	assert.Equal(t, "agent-1", a.ID)
	assert.Equal(t, "lead", a.Name)
	assert.Equal(t, "investigate", a.Task)
	require.NotNil(t, a.FinalMessage)
	assert.Equal(t, "done", *a.FinalMessage)
	assert.Equal(t, "completed", a.Status)
	assert.Equal(t, "user", a.Audience)
	assert.Equal(t, "2026-07-01T10:00:00Z", a.CreatedAt.Format(time.RFC3339))
	assert.Equal(t, "2026-07-01T11:00:00Z", a.UpdatedAt.Format(time.RFC3339))
	assert.Equal(t, []float64{10.5, 12.0}, a.TokensPerSecondHistory)
	require.NotNil(t, a.TokenCounter)
	assert.Equal(t, int64(999), *a.TokenCounter)
	require.NotNil(t, a.OutputPreview)
	assert.Equal(t, "tail of output", *a.OutputPreview)
	require.NotNil(t, s.Progress)
	assert.Equal(t, investigations.LodestoneTodoProgress{
		Pending: 1, InProgress: 2, Completed: 3, Canceled: 4, Total: 10,
	}, *s.Progress)
	require.NotNil(t, s.CompletionQuality)
	assert.Equal(t, "degraded", *s.CompletionQuality)
	require.NotNil(t, s.DegradedReason)
	assert.Equal(t, "token budget exhausted", *s.DegradedReason)
	assert.Equal(t, map[string]string{"env": "prod"}, s.Labels)
	assert.Equal(t, "owner-1", s.OwnerUserID)
	assert.Equal(t, 2, s.ActiveLoopCount)
}

// TestLodestoneSummary_RequiredFieldsSerialized pins the required-vs-omitempty
// split against the server schema: title, description, and chatId are always
// serialized by the API, so gcx json output must keep them (as "") even when
// empty — dropping them would turn "" into null under --json selection.
func TestLodestoneSummary_RequiredFieldsSerialized(t *testing.T) {
	data, err := json.Marshal(investigations.LodestoneInvestigationSummary{ID: "inv-1", State: "pending"})
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))
	for _, key := range []string{"id", "title", "description", "state", "chatId", "createdAt", "updatedAt"} {
		assert.Contains(t, m, key, "required summary field %q must serialize even when empty", key)
	}
	for _, key := range []string{"variant", "tokensUsed", "source", "agents", "progress", "completionQuality", "degradedReason", "labels", "ownerUserId", "activeLoopCount"} {
		assert.NotContains(t, m, key, "optional summary field %q must stay omitempty like the server's", key)
	}

	data, err = json.Marshal(investigations.LodestoneAgent{ID: "agent-1"})
	require.NoError(t, err)
	m = nil
	require.NoError(t, json.Unmarshal(data, &m))
	for _, key := range []string{"id", "name", "task", "status", "audience", "createdAt", "updatedAt"} {
		assert.Contains(t, m, key, "required agent field %q must serialize even when empty", key)
	}
	for _, key := range []string{"finalMessage", "tokensPerSecondHistory", "tokenCounter", "outputPreview"} {
		assert.NotContains(t, m, key, "optional agent field %q must stay omitempty like the server's", key)
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
