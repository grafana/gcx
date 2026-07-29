// Package onboard contains the provider-agnostic core for the
// `gcx setup datasources <cloud>` command family: result types surfaced to users, a
// stable naming/collision helper, and a best-effort rollback accumulator used
// to undo partially-created cloud and Grafana artifacts on failure.
package onboard

import (
	"context"
	"fmt"
	"io"
	"slices"
	"sync"

	"github.com/grafana/grafana-app-sdk/logging"
)

// NamePrefix is prepended to every artifact gcx creates (app registrations,
// datasources). It makes gcx-created objects attributable and lets `--cleanup`
// find them reliably.
const NamePrefix = "gcx"

// Datasource lifecycle statuses reported in DatasourceResult.Status.
const (
	// StatusCreated marks a datasource newly created by this run.
	StatusCreated = "created"
	// StatusExisting marks a gcx-managed datasource that already existed and was
	// reused (idempotent re-run) rather than duplicated.
	StatusExisting = "existing"
	// StatusPlanned marks a datasource that would be created (--dry-run); nothing
	// was minted or created.
	StatusPlanned = "planned"
	// StatusRotated marks a datasource whose backing credential was rotated.
	StatusRotated = "rotated"
	// StatusSkipped marks a datasource that was intentionally not acted on (e.g.
	// not gcx-managed during rotate, or an unsupported type).
	StatusSkipped = "skipped"
)

// DatasourceResult describes a single provisioned datasource. Fields common to
// every cloud live at the top level; cloud-specific identity and IAM details are
// grouped under Credential so the shape stays applicable as the GCP and AWS
// providers are added alongside Azure.
type DatasourceResult struct {
	Name string `json:"name" yaml:"name"`
	Type string `json:"type" yaml:"type"`
	UID  string `json:"uid,omitempty" yaml:"uid,omitempty"`
	// Status is one of the Status* constants describing what happened to this
	// datasource in the run (created, existing, planned, rotated, skipped).
	Status string `json:"status,omitempty" yaml:"status,omitempty"`
	// Health is the datasource health status reported by Grafana after a
	// create/rotate (e.g. "OK", "ERROR", "UNKNOWN"). Empty when the health check
	// was skipped or not applicable.
	Health string `json:"health,omitempty" yaml:"health,omitempty"`
	// HealthMessage carries the health-check detail when Health is not OK.
	HealthMessage string `json:"healthMessage,omitempty" yaml:"healthMessage,omitempty"`
	// Credential describes the managed cloud identity and the access granted to
	// it. Nil for datasources that use no gcx-managed identity (e.g. key-based
	// Cosmos DB) or when nothing identity-related applies to this row.
	Credential *CloudCredential `json:"credential,omitempty" yaml:"credential,omitempty"`
	// Note carries a short human-readable explanation for skipped/planned rows.
	Note string `json:"note,omitempty" yaml:"note,omitempty"`
	// Hint is an advisory, non-blocking suggestion about this datasource (e.g.
	// the backing resource is not publicly reachable, so Private Data source
	// Connect is likely needed). It never affects the run outcome.
	Hint string `json:"hint,omitempty" yaml:"hint,omitempty"`
	// HintDocs is a documentation URL backing Hint, so users and agents can
	// follow up. Empty when there is no hint.
	HintDocs string `json:"hintDocs,omitempty" yaml:"hintDocs,omitempty"`
}

// CloudCredential describes the cloud identity gcx provisions for a datasource
// and the access granted to it. It is provider-agnostic so it applies to Azure
// (app registration + RBAC), GCP (service account + IAM), and AWS (IAM role +
// policies) alike.
type CloudCredential struct {
	// ID is the identity the datasource authenticates as: an Azure application
	// (client) ID, a GCP service-account email, or an AWS role ARN. Empty for a
	// --dry-run preview (nothing minted yet) or key-based datasources.
	ID string `json:"id,omitempty" yaml:"id,omitempty"`
	// Roles are the cloud IAM roles bound to the identity (Azure RBAC roles, GCP
	// IAM roles, AWS policies). Populated for --dry-run previews so the user can
	// see the access that would be granted.
	Roles []string `json:"roles,omitempty" yaml:"roles,omitempty"`
	// Scopes are the resources the roles are bound over (Azure ARM scopes, GCP
	// projects/resources, AWS resource ARNs). Populated for --dry-run previews.
	Scopes []string `json:"scopes,omitempty" yaml:"scopes,omitempty"`
}

// CleanupResult describes one artifact removed (or, in --dry-run, that would be
// removed) by `--cleanup`.
type CleanupResult struct {
	Kind string `json:"kind" yaml:"kind"` // "datasource" or "app-registration"
	Name string `json:"name" yaml:"name"`
	ID   string `json:"id,omitempty" yaml:"id,omitempty"`
	// Planned marks a would-remove entry from a --dry-run cleanup; nothing was
	// actually deleted.
	Planned bool `json:"planned,omitempty" yaml:"planned,omitempty"`
}

// Result is the structured outcome of an onboard run, rendered by the output
// codecs.
type Result struct {
	Provider    string             `json:"provider" yaml:"provider"`
	Datasources []DatasourceResult `json:"datasources,omitempty" yaml:"datasources,omitempty"`
	Cleaned     []CleanupResult    `json:"cleaned,omitempty" yaml:"cleaned,omitempty"`
	// DryRun reports that this result is a preview: no cloud or Grafana
	// artifacts were created, modified, or deleted.
	DryRun bool `json:"dryRun,omitempty" yaml:"dryRun,omitempty"`
}

// EnsureUniqueName returns base if available, otherwise base-2, base-3, … until
// taken reports the name as free. taken should return true when a name is
// already in use anywhere it must be unique (e.g. both the cloud directory and
// Grafana). It returns an error if taken errors or no free name is found.
func EnsureUniqueName(base string, taken func(name string) (bool, error)) (string, error) {
	const maxAttempts = 100
	name := base
	for i := 2; i <= maxAttempts+1; i++ {
		used, err := taken(name)
		if err != nil {
			return "", err
		}
		if !used {
			return name, nil
		}
		name = fmt.Sprintf("%s-%d", base, i)
	}
	return "", fmt.Errorf("could not find a unique name for %q after %d attempts", base, maxAttempts)
}

// Rollback accumulates undo steps registered as artifacts are created, and runs
// them in reverse (LIFO) order. Steps are best-effort: a failing step is logged
// and does not stop the remaining steps.
//
// It is safe for concurrent use: Add may be called from multiple goroutines
// (e.g. parallel provisioning), so a mutex guards the step slice.
type Rollback struct {
	mu    sync.Mutex
	steps []step
}

type step struct {
	desc string
	undo func(ctx context.Context) error
}

// Add registers an undo step. desc is a short human description used in logs.
func (r *Rollback) Add(desc string, undo func(ctx context.Context) error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.steps = append(r.steps, step{desc: desc, undo: undo})
}

// Len reports how many undo steps are registered.
func (r *Rollback) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.steps)
}

// Descriptions returns the step descriptions in the order they would be
// reverted (reverse of registration / LIFO). Used to list pending reverts to
// the user before running them.
func (r *Rollback) Descriptions() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.steps))
	for _, s := range slices.Backward(r.steps) {
		out = append(out, s.desc)
	}
	return out
}

// Run executes all registered undo steps in reverse order, best-effort. Each
// step is both logged (Info/Warn, audit trail) and narrated to progress (stderr,
// visible without -v) as it is reverted, so the user can see exactly what is
// being undone. progress may be nil to suppress narration.
func (r *Rollback) Run(ctx context.Context, log logging.Logger, progress io.Writer) {
	r.mu.Lock()
	steps := slices.Clone(r.steps)
	r.mu.Unlock()

	if len(steps) == 0 {
		return
	}
	if log != nil {
		log.Info("rolling back created artifacts", "steps", len(steps))
	}
	narrate(progress, fmt.Sprintf("Reverting %d change(s):", len(steps)))

	for _, s := range slices.Backward(steps) {
		if log != nil {
			log.Info("rollback step", "step", s.desc)
		}
		if err := s.undo(ctx); err != nil {
			if log != nil {
				log.Warn("rollback step failed", "step", s.desc, "error", err.Error())
			}
			narrate(progress, fmt.Sprintf("  ✗ %s — %v", s.desc, err))
			continue
		}
		if log != nil {
			log.Info("rollback step done", "step", s.desc)
		}
		narrate(progress, "  ✓ "+s.desc)
	}

	if log != nil {
		log.Info("rollback complete", "steps", len(steps))
	}
	narrate(progress, "Revert complete.")
}

// Progressf writes a human-readable progress line to w, appending a newline. It
// is a no-op when w is nil, letting callers disable narration (e.g. agent mode,
// where structured output is the contract) by passing a nil writer. Progress is
// informational narration for long-running cloud and Grafana calls — it is never
// the result.
func Progressf(w io.Writer, format string, a ...any) {
	if w == nil {
		return
	}
	fmt.Fprintf(w, format+"\n", a...)
}

func narrate(w io.Writer, msg string) {
	Progressf(w, "%s", msg)
}
