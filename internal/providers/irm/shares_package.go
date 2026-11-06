// shares_package.go; the share package format and its validation. All
// violations are aggregated into one usage error; nothing is published from an
// invalid package.
package irm

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	fail "github.com/grafana/gcx/internal/gcxerrors"
)

// sharePackage is the on-disk share package a skill assembles. The container
// travels in the package (not on a flag): the finding was gathered against a
// specific alert group or incident, and the file records it.
type sharePackage struct {
	Version       int                 `json:"version"`
	Container     string              `json:"container"`
	Message       shareMessage        `json:"message"`
	Entities      []shareEntity       `json:"entities"`
	EvidenceLinks []shareEvidenceLink `json:"evidence_links"`
	MessageRefs   []string            `json:"message_refs"`
	// Curated is a *bool so a package that omits the field is rejected: a
	// missing curation verdict is not the same as an uncurated one.
	Curated *bool `json:"curated"`
}

// UnmarshalJSON rejects legacy relationships even if a caller changes only version.
func (p *sharePackage) UnmarshalJSON(data []byte) error {
	type plain sharePackage
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if _, legacy := fields["bearings"]; legacy {
		return opModelUsage("share package bearings is retired; review and migrate to version 2 evidence_links")
	}
	if err := json.Unmarshal(data, (*plain)(p)); err != nil {
		return err
	}
	if p.EvidenceLinks == nil {
		return opModelUsage("evidence_links must be an array; use [] when there are no links")
	}
	return nil
}

type shareMessage struct {
	Text string `json:"text"`
	See  string `json:"see"`
}

type shareEntity struct {
	Ref        string `json:"ref"`
	Type       string `json:"type"` // observation | hypothesis | impact | action
	Text       string `json:"text"`
	SourceRef  string `json:"source_ref"`
	PromptedBy string `json:"prompted_by,omitempty"`
}

type shareEvidenceLink struct {
	Source    string `json:"source"`
	Target    string `json:"target"`
	Stance    string `json:"stance"` // supports | challenges | context
	Rationale string `json:"rationale"`
}

// Share-package vocabulary and caps for actions, impacts, and observations.
// Hypotheses are uncapped so a finding can include alternative explanations.
//
//nolint:gochecknoglobals // static validation vocabulary
var (
	shareEntityTypes         = []string{"observation", "hypothesis", "impact", "action"}
	shareEvidenceLinkSources = []string{"observation"}
	shareEvidenceLinkTargets = []string{"hypothesis", "impact"}
	// shareEntityCaps is ordered, not a map: it fixes the order violations are
	// reported in without a sort at every call.
	shareEntityCaps = []struct {
		Type string
		Max  int
	}{
		{"action", 2}, {"impact", 1}, {"observation", 3},
	}
)

// validateSharePackage returns every violation in the package, in a stable
// order. An empty slice means publishable (curation gating is separate).
func validateSharePackage(pkg *sharePackage) []string {
	var errs []string

	if pkg.Version != 2 {
		errs = append(errs, "version must be 2; v1 packages must be reviewed and migrated, not replayed")
	}
	if strings.TrimSpace(pkg.Container) == "" {
		errs = append(errs, "container is required; the alert-group id or incident id the finding belongs to")
	}
	if strings.TrimSpace(pkg.Message.Text) == "" {
		errs = append(errs, "message.text is required and must be non-empty")
	}

	refs := map[string]string{} // ref -> entity type
	counts := map[string]int{}
	for i, ent := range pkg.Entities {
		switch {
		case ent.Ref == "":
			errs = append(errs, fmt.Sprintf("entities[%d]: missing ref", i))
			continue
		case refs[ent.Ref] != "":
			errs = append(errs, fmt.Sprintf("entities[%d]: duplicate ref %q", i, ent.Ref))
			continue
		case !slices.Contains(shareEntityTypes, ent.Type):
			errs = append(errs, fmt.Sprintf("entities[%d] (%s): invalid type %q", i, ent.Ref, ent.Type))
			continue
		}
		if strings.TrimSpace(ent.Text) == "" {
			errs = append(errs, fmt.Sprintf("entities[%d] (%s): text is required", i, ent.Ref))
		}
		refs[ent.Ref] = ent.Type
		counts[ent.Type]++
	}

	for _, c := range shareEntityCaps {
		if counts[c.Type] > c.Max {
			errs = append(errs, fmt.Sprintf("too many %s entities: %d (cap %d); split into separate shares", c.Type, counts[c.Type], c.Max))
		}
	}

	claimEvidenceLinkSources := map[string]bool{}
	for i, b := range pkg.EvidenceLinks {
		srcType, srcKnown := refs[b.Source]
		tgtType, tgtKnown := refs[b.Target]
		if !srcKnown {
			errs = append(errs, fmt.Sprintf("evidence_links[%d]: unknown source ref %q", i, b.Source))
		}
		if !tgtKnown {
			errs = append(errs, fmt.Sprintf("evidence_links[%d]: unknown target ref %q", i, b.Target))
		}
		if srcKnown && tgtKnown {
			if b.Source == b.Target {
				errs = append(errs, fmt.Sprintf("evidence_links[%d]: self-evidence link %q", i, b.Source))
			}
			if !slices.Contains(shareEvidenceLinkSources, srcType) {
				errs = append(errs, fmt.Sprintf("evidence_links[%d]: source %s is a %s; must be one of [%s]", i, b.Source, srcType, strings.Join(shareEvidenceLinkSources, " ")))
			}
			if !slices.Contains(shareEvidenceLinkTargets, tgtType) {
				errs = append(errs, fmt.Sprintf("evidence_links[%d]: target %s is a %s; must be one of [%s]", i, b.Target, tgtType, strings.Join(shareEvidenceLinkTargets, " ")))
			}
			if slices.Contains(shareEvidenceLinkTargets, tgtType) {
				claimEvidenceLinkSources[b.Source] = true
			}
		}
		if !slices.Contains(stanceVocab, b.Stance) {
			errs = append(errs, fmt.Sprintf("evidence_links[%d]: invalid stance %q", i, b.Stance))
		}
	}

	for i, ent := range pkg.Entities {
		if ent.PromptedBy == "" {
			continue
		}
		if ent.Type != "action" {
			errs = append(errs, fmt.Sprintf("entities[%d] (%s): prompted_by is only valid for actions", i, ent.Ref))
			continue
		}
		promptType, known := refs[ent.PromptedBy]
		if !known {
			errs = append(errs, fmt.Sprintf("entities[%d] (%s): prompted_by references unknown ref %q", i, ent.Ref, ent.PromptedBy))
			continue
		}
		if promptType != "observation" {
			errs = append(errs, fmt.Sprintf("entities[%d] (%s): prompted_by ref %q is a %s; must be an observation", i, ent.Ref, ent.PromptedBy, promptType))
			continue
		}
		if !claimEvidenceLinkSources[ent.PromptedBy] {
			errs = append(errs, fmt.Sprintf("entities[%d] (%s): prompted_by observation %q must have an evidence link to a claim", i, ent.Ref, ent.PromptedBy))
		}
	}

	for _, ref := range pkg.MessageRefs {
		if refs[ref] == "" {
			errs = append(errs, fmt.Sprintf("message_refs: unknown ref %q", ref))
		}
	}

	if pkg.Curated == nil {
		errs = append(errs, "curated must be true or false")
	}

	return errs
}

// invalidSharePackageError aggregates every violation into one exit-2 error.
func invalidSharePackageError(violations []string) error {
	exit2 := fail.ExitUsageError
	return &fail.DetailedError{
		Summary:  fmt.Sprintf("share package is invalid (%d problem(s)); nothing was published", len(violations)),
		Details:  strings.Join(violations, "\n"),
		ExitCode: &exit2,
		Suggestions: []string{
			"Fix the package file and re-run; the package format is defined by the share-finding skill",
		},
	}
}

// uncuratedSharePackageError is the curation gate: a curated:false package
// comes from a non-interactive run a human has not approved.
func uncuratedSharePackageError() error {
	exit2 := fail.ExitUsageError
	return &fail.DetailedError{
		Summary:  "package is curated:false; a human has not approved it for publication",
		Details:  "Uncurated packages come from non-interactive runs and are never published by default. Nothing was published.",
		ExitCode: &exit2,
		Suggestions: []string{
			"Have a human review the package, set curated:true, and re-run",
			"Testing only: re-run with --allow-uncurated",
		},
	}
}
