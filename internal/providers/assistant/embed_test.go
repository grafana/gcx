//nolint:testpackage // white-box testing: exercises unexported rejectApprovalHandler.
package assistant

import (
	"testing"

	"github.com/grafana/gcx/internal/assistant"
	"github.com/stretchr/testify/assert"
)

// TestAlwaysApprove_Approves is trivial but pins the public contract: any
// embed caller that opts in via PromptRequest.ApprovalHandler = AlwaysApprove{}
// gets every tool call approved. A silent flip to "sometimes reject" would
// re-introduce the HTTP 500 "Permission check failed" that motivated exposing
// this handler in the first place.
func TestAlwaysApprove_Approves(t *testing.T) {
	assert.True(t, AlwaysApprove{}.HandleApproval(assistant.ApprovalRequest{ToolName: "any"}))
	assert.True(t, AlwaysApprove{}.HandleApproval(assistant.ApprovalRequest{}))
}

// TestRejectApprovalHandler_Rejects pins the fail-closed default. Nil
// ApprovalHandler in RunPrompt swaps in rejectApprovalHandler{} — if this
// flips to true, callers that forgot to think about approval policy would
// silently grant everything.
func TestRejectApprovalHandler_Rejects(t *testing.T) {
	assert.False(t, rejectApprovalHandler{}.HandleApproval(assistant.ApprovalRequest{ToolName: "any"}))
	assert.False(t, rejectApprovalHandler{}.HandleApproval(assistant.ApprovalRequest{}))
}
