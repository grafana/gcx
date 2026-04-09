package assistant

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"
)

// ApprovalHandler handles approval requests during streaming.
// If nil, approvals are auto-denied.
type ApprovalHandler interface {
	// HandleApproval is called when an approval request is received.
	// Returns true if approved, false if denied.
	HandleApproval(req ApprovalRequest) bool
}

// InteractiveApprovalHandler prompts the user via stdin for approval.
type InteractiveApprovalHandler struct {
	Logger Logger
}

// HandleApproval prompts the user for approval via stdin.
func (h *InteractiveApprovalHandler) HandleApproval(req ApprovalRequest) bool {
	prompt := "Approve " + req.ToolName
	if req.Description != "" {
		prompt += " - " + req.Description
	}
	prompt += "? [y/n]: "

	if h.Logger != nil {
		h.Logger.Info(prompt)
	} else {
		fmt.Fprint(os.Stdout, prompt)
	}

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return false
	}

	input = strings.TrimSpace(strings.ToLower(input))
	return input == "y" || input == "yes"
}

// StreamChat sends a message and streams the response via A2A SSE.
func StreamChat(ctx context.Context, baseURL, token, agentID, prompt string, opts StreamOptions, logger Logger, httpClient *http.Client) StreamResult {
	return StreamChatWithApproval(ctx, baseURL, token, agentID, prompt, opts, logger, nil, httpClient)
}

// StreamChatWithApproval sends a message and streams the response, handling approval requests.
func StreamChatWithApproval(ctx context.Context, baseURL, token, agentID, prompt string, opts StreamOptions, logger Logger, approvalHandler ApprovalHandler, httpClient *http.Client) StreamResult {
	if logger == nil {
		logger = NopLogger{}
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 300
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	endpoints := GetA2AEndpoints(baseURL)
	url := endpoints.AgentEndpoint(agentID)

	reqBody, err := CreateMessageStreamRequest(prompt, opts.ContextID)
	if err != nil {
		logger.Warning(fmt.Sprintf("Failed to create request: %v", err))
		return StreamResult{Failed: true, ErrorMessage: err.Error()}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		logger.Warning(fmt.Sprintf("Failed to create HTTP request: %v", err))
		return StreamResult{Failed: true, ErrorMessage: err.Error()}
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-App-Source", "cli")

	logger.Debug("Sending A2A request to " + url)

	resp, err := httpClient.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			logger.Warning(fmt.Sprintf("Timeout after %ds", timeout))
			return StreamResult{TimedOut: true}
		}
		logger.Warning(fmt.Sprintf("Request failed: %v", err))
		return StreamResult{Failed: true, ErrorMessage: err.Error()}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		logger.Warning(fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(respBody)))
		return StreamResult{Failed: true, ErrorMessage: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(respBody))}
	}

	return processA2ASSEStream(ctx, resp.Body, timeout, logger, baseURL, token, approvalHandler, opts.OnEvent, httpClient)
}

// streamState carries mutable state through the SSE stream processing helpers.
type streamState struct {
	result                 StreamResult
	responseTexts          []string
	completedStatusEmitted bool
	failedStatusEmitted    bool
	onEvent                func(StreamEvent)
	logger                 Logger
	baseURL                string
	token                  string
	approvalHandler        ApprovalHandler
	httpClient             *http.Client
}

// handleStatusUpdate processes a status-update event. Returns true if the caller should return immediately.
func (s *streamState) handleStatusUpdate(statusUpdate A2AStatusUpdate) bool {
	s.result.TaskID = statusUpdate.TaskID
	s.result.ContextID = statusUpdate.ContextID

	s.logger.Debug(fmt.Sprintf("Status: %s (final: %v)", statusUpdate.Status.State, statusUpdate.Final))

	switch statusUpdate.Status.State {
	case "completed":
		return s.handleStatusCompleted(statusUpdate)
	case "failed":
		s.handleStatusFailed(statusUpdate)
	case "canceled":
		s.handleStatusCanceled(statusUpdate)
		return true
	default:
		s.handleStatusDefault(statusUpdate)
	}
	return false
}

func (s *streamState) handleStatusCompleted(statusUpdate A2AStatusUpdate) bool {
	s.result.Completed = true
	if statusUpdate.Status.Message != nil {
		text := ExtractTextFromParts(statusUpdate.Status.Message.Parts)
		if text != "" && !containsText(s.responseTexts, text) {
			s.responseTexts = append(s.responseTexts, text)
			if s.onEvent != nil {
				s.onEvent(StreamEvent{
					Type:      "message",
					TaskID:    statusUpdate.TaskID,
					ContextID: statusUpdate.ContextID,
					Text:      text,
				})
			}
		}
	}
	if s.onEvent != nil && (!s.completedStatusEmitted || statusUpdate.Final) {
		s.onEvent(StreamEvent{
			Type:      "status",
			TaskID:    statusUpdate.TaskID,
			ContextID: statusUpdate.ContextID,
			State:     "completed",
			Final:     statusUpdate.Final,
		})
		if statusUpdate.Final {
			s.completedStatusEmitted = true
		}
	}
	if statusUpdate.Final {
		s.logger.Info("Task completed!")
		s.result.Response = strings.Join(s.responseTexts, "\n")
		return true
	}
	return false
}

func (s *streamState) handleStatusFailed(statusUpdate A2AStatusUpdate) {
	s.result.Failed = true
	if statusUpdate.Status.Message != nil {
		s.result.ErrorMessage = ExtractTextFromParts(statusUpdate.Status.Message.Parts)
	}
	if s.onEvent != nil && (!s.failedStatusEmitted || statusUpdate.Final) {
		s.onEvent(StreamEvent{
			Type:      "status",
			TaskID:    statusUpdate.TaskID,
			ContextID: statusUpdate.ContextID,
			State:     "failed",
			Final:     statusUpdate.Final,
			Error:     s.result.ErrorMessage,
		})
		if statusUpdate.Final {
			s.failedStatusEmitted = true
		}
		if s.result.ErrorMessage != "" {
			s.result.ErrorEventEmitted = true
		}
	}
	s.logger.Warning("Task failed: " + s.result.ErrorMessage)
}

func (s *streamState) handleStatusCanceled(statusUpdate A2AStatusUpdate) {
	s.result.Canceled = true
	if s.onEvent != nil {
		s.onEvent(StreamEvent{
			Type:      "status",
			TaskID:    statusUpdate.TaskID,
			ContextID: statusUpdate.ContextID,
			State:     "canceled",
			Final:     statusUpdate.Final,
		})
	}
	s.logger.Warning("Task was canceled")
}

func (s *streamState) handleStatusDefault(statusUpdate A2AStatusUpdate) {
	if s.onEvent != nil {
		s.onEvent(StreamEvent{
			Type:      "status",
			TaskID:    statusUpdate.TaskID,
			ContextID: statusUpdate.ContextID,
			State:     statusUpdate.Status.State,
			Final:     statusUpdate.Final,
		})
	}
	if statusUpdate.Status.State == "working" {
		s.logger.Info("Agent is working...")
	}
}

// handleArtifactUpdate processes an artifact-update event.
func (s *streamState) handleArtifactUpdate(ctx context.Context, artifactUpdate A2AArtifactUpdate) {
	switch artifactUpdate.Artifact.Name {
	case "step.message":
		s.handleArtifactMessage(artifactUpdate)
	case "step.toolCall":
		s.handleArtifactToolCall(artifactUpdate)
	case "step.approval":
		s.handleArtifactApproval(ctx, artifactUpdate)
	}
}

func (s *streamState) handleArtifactMessage(artifactUpdate A2AArtifactUpdate) {
	text := ExtractTextFromParts(artifactUpdate.Artifact.Parts)
	if text != "" {
		s.responseTexts = append(s.responseTexts, text)
		if s.onEvent != nil {
			s.onEvent(StreamEvent{
				Type:      "message",
				TaskID:    artifactUpdate.TaskID,
				ContextID: artifactUpdate.ContextID,
				Text:      text,
			})
		}
		s.logger.Debug("Message: " + truncate(text, 100))
	}
}

func (s *streamState) handleArtifactToolCall(artifactUpdate A2AArtifactUpdate) {
	for _, part := range artifactUpdate.Artifact.Parts {
		if part.Kind != "data" || part.Data == nil {
			continue
		}
		var toolData struct {
			ToolName string `json:"toolName"`
		}
		if json.Unmarshal(part.Data, &toolData) == nil && toolData.ToolName != "" {
			if s.onEvent != nil {
				s.onEvent(StreamEvent{
					Type:      "tool_call",
					TaskID:    artifactUpdate.TaskID,
					ContextID: artifactUpdate.ContextID,
					ToolName:  toolData.ToolName,
				})
			}
			s.logger.Info("Using tool: " + toolData.ToolName)
		}
	}
}

func (s *streamState) handleArtifactApproval(ctx context.Context, artifactUpdate A2AArtifactUpdate) {
	for _, part := range artifactUpdate.Artifact.Parts {
		if part.Kind != "data" || part.Data == nil {
			continue
		}
		var approvalReq ApprovalRequest
		if json.Unmarshal(part.Data, &approvalReq) != nil || approvalReq.ID == "" {
			continue
		}
		if s.onEvent != nil {
			s.onEvent(StreamEvent{
				Type:      "approval",
				TaskID:    artifactUpdate.TaskID,
				ContextID: artifactUpdate.ContextID,
				ToolName:  approvalReq.ToolName,
				Text:      approvalReq.Description,
			})
		}

		approved := false
		if s.approvalHandler != nil {
			approved = s.approvalHandler.HandleApproval(approvalReq)
		} else {
			s.logger.Warning(fmt.Sprintf("Approval required for %s but no handler available - auto-denying", approvalReq.ToolName))
		}

		if err := SubmitApproval(ctx, s.baseURL, s.token, approvalReq.ID, approvalReq.ChatID, approvalReq.TenantID, approvalReq.UserID, approved, s.httpClient); err != nil {
			s.logger.Warning(fmt.Sprintf("Failed to submit approval: %v", err))
		} else if approved {
			s.logger.Info("Approved - continuing...")
		} else {
			s.logger.Info("Denied - skipping tool")
		}
	}
}

// handleTask processes a task event.
func (s *streamState) handleTask(task A2ATask) {
	s.result.TaskID = task.ID
	s.result.ContextID = task.ContextID

	if task.Metadata != nil && task.Metadata.Error != "" {
		s.result.ErrorMessage = task.Metadata.Error
	}

	switch task.Status.State {
	case "completed":
		s.handleTaskCompleted(task)
	case "failed":
		s.handleTaskFailed(task)
	case "canceled":
		s.handleTaskCanceled(task)
	}
}

func (s *streamState) handleTaskCompleted(task A2ATask) {
	s.result.Completed = true
	for _, artifact := range task.Artifacts {
		if artifact.Name == "step.message" {
			text := ExtractTextFromParts(artifact.Parts)
			if text != "" && !containsText(s.responseTexts, text) {
				s.responseTexts = append(s.responseTexts, text)
				if s.onEvent != nil {
					s.onEvent(StreamEvent{
						Type:      "message",
						TaskID:    task.ID,
						ContextID: task.ContextID,
						Text:      text,
					})
				}
			}
		}
	}
	if s.onEvent != nil && !s.completedStatusEmitted {
		s.onEvent(StreamEvent{
			Type:      "status",
			TaskID:    task.ID,
			ContextID: task.ContextID,
			State:     "completed",
			Final:     true,
		})
		s.completedStatusEmitted = true
	}
}

func (s *streamState) handleTaskFailed(task A2ATask) {
	s.result.Failed = true
	if s.onEvent != nil && !s.failedStatusEmitted {
		s.onEvent(StreamEvent{
			Type:      "status",
			TaskID:    task.ID,
			ContextID: task.ContextID,
			State:     "failed",
			Final:     true,
			Error:     s.result.ErrorMessage,
		})
		s.failedStatusEmitted = true
		if s.result.ErrorMessage != "" {
			s.result.ErrorEventEmitted = true
		}
	}
}

func (s *streamState) handleTaskCanceled(task A2ATask) {
	s.result.Canceled = true
	if s.onEvent != nil {
		s.onEvent(StreamEvent{
			Type:      "status",
			TaskID:    task.ID,
			ContextID: task.ContextID,
			State:     "canceled",
			Final:     true,
		})
	}
}

// processA2ASSEStream processes the A2A SSE stream and returns the result.
func processA2ASSEStream(ctx context.Context, body io.Reader, timeout int, logger Logger, baseURL, token string, approvalHandler ApprovalHandler, onEvent func(StreamEvent), httpClient *http.Client) StreamResult {
	s := &streamState{
		onEvent:         onEvent,
		logger:          logger,
		baseURL:         baseURL,
		token:           token,
		approvalHandler: approvalHandler,
		httpClient:      httpClient,
	}

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" {
			continue
		}

		logger.Debug("SSE: " + line)

		if !strings.HasPrefix(line, "data:") {
			continue
		}

		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}

		event, err := parseA2ASSEEvent(data)
		if err != nil {
			logger.Debug(fmt.Sprintf("Failed to parse SSE event: %v", err))
			continue
		}

		if event.Error != nil {
			errorEventEmitted := false
			if onEvent != nil {
				onEvent(StreamEvent{Type: "error", Error: event.Error.Message})
				errorEventEmitted = true
			}
			logger.Warning("JSON-RPC error: " + event.Error.Message)
			return StreamResult{
				Failed:            true,
				ErrorMessage:      event.Error.Message,
				ErrorEventEmitted: errorEventEmitted,
			}
		}

		if event.Result == nil {
			continue
		}

		var kindCheck struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(event.Result, &kindCheck); err != nil {
			continue
		}

		switch kindCheck.Kind {
		case "status-update":
			var statusUpdate A2AStatusUpdate
			if err := json.Unmarshal(event.Result, &statusUpdate); err != nil {
				logger.Debug(fmt.Sprintf("Failed to parse status-update: %v", err))
				continue
			}
			if s.handleStatusUpdate(statusUpdate) {
				return s.result
			}

		case "artifact-update":
			var artifactUpdate A2AArtifactUpdate
			if err := json.Unmarshal(event.Result, &artifactUpdate); err != nil {
				logger.Debug(fmt.Sprintf("Failed to parse artifact-update: %v", err))
				continue
			}
			s.handleArtifactUpdate(ctx, artifactUpdate)

		case "task":
			var task A2ATask
			if err := json.Unmarshal(event.Result, &task); err != nil {
				logger.Debug(fmt.Sprintf("Failed to parse task: %v", err))
				continue
			}
			s.handleTask(task)
		}
	}

	if err := scanner.Err(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			logger.Warning(fmt.Sprintf("Timeout after %ds", timeout))
			return StreamResult{TimedOut: true}
		}
		logger.Warning(fmt.Sprintf("SSE error: %v", err))
		return StreamResult{Failed: true, ErrorMessage: err.Error()}
	}

	if !s.result.Completed && !s.result.Failed && !s.result.Canceled {
		if len(s.responseTexts) > 0 {
			s.result.Completed = true
			if onEvent != nil {
				onEvent(StreamEvent{
					Type:      "status",
					TaskID:    s.result.TaskID,
					ContextID: s.result.ContextID,
					State:     "completed",
					Final:     true,
				})
			}
		}
	}

	s.result.Response = strings.Join(s.responseTexts, "\n")
	return s.result
}

func parseA2ASSEEvent(data string) (*JSONRPCResponse, error) {
	var response JSONRPCResponse
	if err := json.Unmarshal([]byte(data), &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func containsText(slice []string, text string) bool {
	return slices.Contains(slice, text)
}
