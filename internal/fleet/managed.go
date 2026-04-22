package fleet

// ManagedPipelinePrefix is the prefix used by instrumentation-owned Fleet pipelines.
// Fleet provider commands use this to guard against accidental direct edits, and
// the instrumentation provider uses it to name the pipelines it manages.
//
// Kept in the shared internal/fleet package (not in either provider) so both
// providers can consume it without importing each other — required by the
// CONSTITUTION rule that providers may not import other providers.
const ManagedPipelinePrefix = "beyla_k8s_appo11y_"
