package agent

// Cobra Annotations map keys for agent-facing command metadata.
const (
	AnnotationTokenCost      = "agent.token_cost"      // "small", "medium", "large"
	AnnotationLLMHint        = "agent.llm_hint"        // scoping hint for agents
	AnnotationRequiredScope  = "agent.required_scope"  // required auth scope
	AnnotationRequiredRole   = "agent.required_role"   // required role
	AnnotationRequiredAction = "agent.required_action" // required action
	AnnotationSkill          = "agent.skill"           // comma-joined related Agent Skill names
	AnnotationAvailability   = "agent.availability"    // "grafana-cloud-only" (absent = available on self-hosted + cloud)
)

// AvailabilityCloudOnly marks a command as only usable against Grafana Cloud.
// It is the sole value emitted for AnnotationAvailability; absence of the
// annotation means the command works everywhere (OSS, Enterprise, and Cloud).
const AvailabilityCloudOnly = "grafana-cloud-only"
