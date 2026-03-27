package agent

// Cobra Annotations map keys for agent-facing command metadata.
const (
	AnnotationTokenCost      = "agent.token_cost"      // "small", "medium", "large"
	AnnotationLLMHint        = "agent.llm_hint"        // scoping hint for agents
	AnnotationRequiredScope  = "agent.required_scope"  // required auth scope
	AnnotationRequiredRole   = "agent.required_role"   // required role
	AnnotationRequiredAction = "agent.required_action" // required action
)
