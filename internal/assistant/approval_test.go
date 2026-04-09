package assistant_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/assistant"
)

func TestApprovalRequest_JSONUnmarshal(t *testing.T) {
	jsonData := `{
		"id": "approval-123",
		"chatId": "chat-456",
		"tenantId": "tenant-789",
		"userId": "user-abc",
		"toolName": "execute_query",
		"toolInput": {"query": "SELECT 1"},
		"description": "Execute a database query"
	}`

	var req assistant.ApprovalRequest
	if err := json.Unmarshal([]byte(jsonData), &req); err != nil {
		t.Fatalf("Failed to unmarshal ApprovalRequest: %v", err)
	}

	if req.ID != "approval-123" {
		t.Errorf("ID = %q, want %q", req.ID, "approval-123")
	}
	if req.ToolName != "execute_query" {
		t.Errorf("ToolName = %q, want %q", req.ToolName, "execute_query")
	}
	if req.Description != "Execute a database query" {
		t.Errorf("Description = %q, want %q", req.Description, "Execute a database query")
	}
}

func TestApprovalResponse_JSONMarshal(t *testing.T) {
	resp := assistant.ApprovalResponse{
		ID:       "approval-123",
		ChatID:   "chat-456",
		TenantID: "tenant-789",
		UserID:   "user-abc",
		Approved: true,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal ApprovalResponse: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if result["id"] != "approval-123" {
		t.Errorf("id = %v, want %q", result["id"], "approval-123")
	}
	if result["approved"] != true {
		t.Errorf("approved = %v, want true", result["approved"])
	}
}

func TestSubmitApproval_Success(t *testing.T) {
	var receivedBody assistant.ApprovalResponse

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Method = %q, want POST", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("Authorization = %q, want Bearer test-token", r.Header.Get("Authorization"))
		}

		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &receivedBody); err != nil {
			t.Fatalf("Failed to unmarshal request body: %v", err)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	err := assistant.SubmitApproval(context.Background(), server.URL, "test-token", "approval-123", "chat-456", "tenant-789", "user-abc", true, http.DefaultClient)
	if err != nil {
		t.Fatalf("SubmitApproval() error = %v", err)
	}

	if receivedBody.ID != "approval-123" {
		t.Errorf("Body ID = %q, want %q", receivedBody.ID, "approval-123")
	}
	if !receivedBody.Approved {
		t.Errorf("Body Approved = %v, want true", receivedBody.Approved)
	}
}

func TestSubmitApproval_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error": "approval not found"}`))
	}))
	defer server.Close()

	err := assistant.SubmitApproval(context.Background(), server.URL, "test-token", "approval-123", "chat-456", "tenant-789", "user-abc", true, http.DefaultClient)
	if err == nil {
		t.Error("SubmitApproval() should return error for non-success status")
	}
}
