package output

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/agent"
)

// MutationResult is the structured result emitted by mutation commands.
// In agent mode it is serialized as JSON to stdout; in non-agent mode
// it is rendered as a one-line human summary.
type MutationResult struct {
	Action  string        `json:"action"`
	Target  Target        `json:"target"`
	Changed bool          `json:"changed"`
	Fields  []FieldChange `json:"fields,omitempty"`
	// Discovered is set by apps configure to indicate whether the namespace
	// appears in RunK8sDiscovery at the time of the call. Omitted for non-apps
	// mutation results (cluster, service) where the concept does not apply.
	Discovered *bool `json:"discovered,omitempty"`
}

// Target identifies the resource that was mutated.
type Target struct {
	Cluster   string `json:"cluster,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Service   string `json:"service,omitempty"`
}

// FieldChange records one field's before/after values.
type FieldChange struct {
	Name string `json:"name"`
	From string `json:"from"`
	To   string `json:"to"`
}

// Emit writes the MutationResult to w.
// In agent mode: writes a single JSON object followed by newline.
// In non-agent mode: writes a one-line human summary.
func (r MutationResult) Emit(w io.Writer) error {
	if agent.IsAgentMode() {
		data, err := json.Marshal(r)
		if err != nil {
			return fmt.Errorf("mutation result: marshal: %w", err)
		}
		_, err = fmt.Fprintln(w, string(data))
		return err
	}

	target := r.targetString()
	if r.Changed {
		fmt.Fprintf(w, "%s %q: done\n", r.Action, target)
	} else {
		fmt.Fprintf(w, "%s %q: no changes\n", r.Action, target)
	}
	return nil
}

func (r MutationResult) targetString() string {
	if r.Target.Service != "" {
		return fmt.Sprintf("%s/%s/%s", r.Target.Cluster, r.Target.Namespace, r.Target.Service)
	}
	if r.Target.Namespace != "" {
		return fmt.Sprintf("%s/%s", r.Target.Cluster, r.Target.Namespace)
	}
	return r.Target.Cluster
}
