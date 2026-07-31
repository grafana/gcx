package output

// This file defines the shared result family for mutation commands — the
// finite structured values a mutation writes to stdout through the codec
// system (Options.Encode). Commands register a small text codec that renders
// the human one-liner they always printed, keeping human output
// byte-identical, while agent mode (agents codec) and explicit -o json/yaml
// get the structured value for free.
//
// The family deliberately has more than one shape — a single-target verb, a
// batch verb, and a files-on-disk receipt carry genuinely different
// information. Do NOT force new mutations into one universal struct, and do
// NOT reuse these types where a provider has already locked its own result
// contract (IRM OnCall's single/bulk envelopes predate this family and stay
// as shipped).
//
// Every shape carries collision-resistant discriminators (`type`,
// `schema_version`) so consumers dispatch on shape without heuristics.
// Construct values with the New* constructors so the discriminators are
// never forgotten.

// Discriminator values for the mutation result family.
const (
	SingleMutationType  = "gcx.mutation"
	BatchMutationType   = "gcx.mutation_batch"
	ArtifactReceiptType = "gcx.artifact_receipt"

	mutationSchemaVersion = "1"
)

// MutationTarget identifies the object a mutation acted on. All fields are
// optional — providers populate what their domain actually has.
type MutationTarget struct {
	Kind      string `json:"kind,omitempty" yaml:"kind,omitempty"`
	Name      string `json:"name,omitempty" yaml:"name,omitempty"`
	UID       string `json:"uid,omitempty" yaml:"uid,omitempty"`
	ID        string `json:"id,omitempty" yaml:"id,omitempty"`
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
}

// MutationSummary aggregates per-target outcomes for batch shapes.
// Invariant: every matched target is counted exactly once —
// succeeded + failed + skipped covers the whole batch.
type MutationSummary struct {
	Succeeded int `json:"succeeded" yaml:"succeeded"`
	Failed    int `json:"failed" yaml:"failed"`
	Skipped   int `json:"skipped,omitempty" yaml:"skipped,omitempty"`
}

// MutationFailure is one failed target with the reason. Successes and skips
// are counted, not enumerated — failures are what a consumer must act on.
type MutationFailure struct {
	Target MutationTarget `json:"target" yaml:"target"`
	Error  string         `json:"error" yaml:"error"`
}

// SingleMutation is the finite result of one verb applied to one target.
type SingleMutation struct {
	Type          string         `json:"type" yaml:"type"`
	SchemaVersion string         `json:"schema_version" yaml:"schema_version"`
	Action        string         `json:"action" yaml:"action"`
	Target        MutationTarget `json:"target" yaml:"target"`
	// Changed distinguishes a real state change from an idempotent no-op.
	// Nil means the command cannot tell (omitted from output).
	Changed *bool  `json:"changed,omitempty" yaml:"changed,omitempty"`
	DryRun  bool   `json:"dry_run,omitempty" yaml:"dry_run,omitempty"`
	Error   string `json:"error,omitempty" yaml:"error,omitempty"`
}

// NewSingleMutation returns a SingleMutation with the discriminators set.
func NewSingleMutation(action string, target MutationTarget) SingleMutation {
	return SingleMutation{
		Type:          SingleMutationType,
		SchemaVersion: mutationSchemaVersion,
		Action:        action,
		Target:        target,
	}
}

// BatchMutation is the finite result of one verb applied across many targets.
type BatchMutation struct {
	Type          string          `json:"type" yaml:"type"`
	SchemaVersion string          `json:"schema_version" yaml:"schema_version"`
	Action        string          `json:"action" yaml:"action"`
	Summary       MutationSummary `json:"summary" yaml:"summary"`
	// Failures is always present — [] when nothing failed — so consumers
	// never need a nil check before ranging.
	Failures []MutationFailure `json:"failures" yaml:"failures"`
	DryRun   bool              `json:"dry_run,omitempty" yaml:"dry_run,omitempty"`
}

// NewBatchMutation returns a BatchMutation with the discriminators set and
// Failures initialized to an empty, always-serialized slice.
func NewBatchMutation(action string) BatchMutation {
	return BatchMutation{
		Type:          BatchMutationType,
		SchemaVersion: mutationSchemaVersion,
		Action:        action,
		Failures:      []MutationFailure{},
	}
}

// ArtifactFile is one file an artifact-writing command produced.
type ArtifactFile struct {
	Path string `json:"path" yaml:"path"`
	Kind string `json:"kind,omitempty" yaml:"kind,omitempty"`
	// Count is the number of resources in the file when a file groups
	// several (0 is omitted for single-resource files).
	Count int `json:"count,omitempty" yaml:"count,omitempty"`
}

// ArtifactReceipt is the finite terminal result of a command whose real
// output is files on disk (e.g. resources pull). The receipt — not the file
// content — is what belongs on stdout: paths, format, counts, failures.
type ArtifactReceipt struct {
	Type          string `json:"type" yaml:"type"`
	SchemaVersion string `json:"schema_version" yaml:"schema_version"`
	Action        string `json:"action" yaml:"action"`
	Dir           string `json:"dir,omitempty" yaml:"dir,omitempty"`
	// Format is the on-disk content format of the written files.
	Format   string            `json:"format" yaml:"format"`
	Files    []ArtifactFile    `json:"files" yaml:"files"`
	Summary  MutationSummary   `json:"summary" yaml:"summary"`
	Failures []MutationFailure `json:"failures" yaml:"failures"`
}

// NewArtifactReceipt returns an ArtifactReceipt with the discriminators set
// and the always-serialized slices initialized.
func NewArtifactReceipt(action, format string) ArtifactReceipt {
	return ArtifactReceipt{
		Type:          ArtifactReceiptType,
		SchemaVersion: mutationSchemaVersion,
		Action:        action,
		Format:        format,
		Files:         []ArtifactFile{},
		Failures:      []MutationFailure{},
	}
}
