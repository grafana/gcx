package output

import (
	"errors"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/pflag"
)

// Discriminator values for the instrumentation mutation result. The shape is
// bespoke (rather than cmdio.SingleMutation) because the domain's target is a
// cluster/namespace/service triple that does not fit cmdio.MutationTarget,
// and the result carries the domain-specific Fields diff and Discovered
// enrichment.
const (
	// MutationResultType is the collision-resistant `type` discriminator.
	MutationResultType = "gcx.instrumentation.mutation"

	mutationSchemaVersion = "1"
)

// MutationResult is the structured result emitted by instrumentation mutation
// commands. It is written to stdout through the codec system (cmdio.Options):
// the "text" codec (MutationTextCodec) renders the one-line human summary;
// agent mode and explicit -o json/yaml get the structured document.
type MutationResult struct {
	Type          string        `json:"type" yaml:"type"`
	SchemaVersion string        `json:"schema_version" yaml:"schema_version"`
	Action        string        `json:"action" yaml:"action"`
	Target        Target        `json:"target" yaml:"target"`
	Changed       bool          `json:"changed" yaml:"changed"`
	Fields        []FieldChange `json:"fields,omitempty" yaml:"fields,omitempty"`
	// Discovered is set by apps configure to indicate whether the namespace
	// appears in RunK8sDiscovery at the time of the call. Omitted for non-apps
	// mutation results (cluster, service) where the concept does not apply.
	Discovered *bool `json:"discovered,omitempty" yaml:"discovered,omitempty"`
}

// NewMutationResult returns a MutationResult with the discriminators set.
// Construct results with this so `type`/`schema_version` are never forgotten.
func NewMutationResult(action string, target Target) MutationResult {
	return MutationResult{
		Type:          MutationResultType,
		SchemaVersion: mutationSchemaVersion,
		Action:        action,
		Target:        target,
	}
}

// Target identifies the resource that was mutated.
type Target struct {
	Cluster   string `json:"cluster,omitempty" yaml:"cluster,omitempty"`
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Service   string `json:"service,omitempty" yaml:"service,omitempty"`
}

// FieldChange records one field's before/after values.
type FieldChange struct {
	Name string `json:"name" yaml:"name"`
	From string `json:"from" yaml:"from"`
	To   string `json:"to" yaml:"to"`
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

// MutationTextCodec is the human "text" codec for MutationResult values. It
// renders exactly the one-line summary the mutation commands have always
// printed, so default human stdout stays byte-identical to the pre-codec
// output.
type MutationTextCodec struct{}

// Format returns the codec's format identifier.
func (MutationTextCodec) Format() format.Format { return "text" }

// Decode is not supported for the text format.
func (MutationTextCodec) Decode(io.Reader, any) error {
	return errors.New("text codec does not support decoding")
}

// Encode renders the legacy human one-liner for a MutationResult.
func (MutationTextCodec) Encode(w io.Writer, value any) error {
	r, ok := value.(MutationResult)
	if !ok {
		return fmt.Errorf("invalid data type for mutation text codec: expected MutationResult, got %T", value)
	}

	target := r.targetString()
	if r.Changed {
		_, err := fmt.Fprintf(w, "%s %q: done\n", r.Action, target)
		return err
	}
	_, err := fmt.Fprintf(w, "%s %q: no changes\n", r.Action, target)
	return err
}

// BindMutationIO wires opts for the instrumentation mutation result contract:
// the default "text" codec reproduces the legacy human one-liner, while agent
// mode (agents codec) and explicit -o json/yaml receive the structured
// MutationResult document. Call from each mutation command's flag setup.
func BindMutationIO(opts *cmdio.Options, flags *pflag.FlagSet) {
	opts.RegisterCustomCodec("text", MutationTextCodec{})
	opts.DefaultFormat("text")
	opts.BindFlags(flags)
}
