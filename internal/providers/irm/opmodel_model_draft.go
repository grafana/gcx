package irm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	fail "github.com/grafana/gcx/internal/gcxerrors"
)

type modelDraft struct {
	Entities      []draftEntity
	EvidenceLinks []draftLink
}

type draftEntity struct {
	Ref        string
	Kind       string
	Text       string
	SourceRef  string
	PromptedBy string
	ExistingID string
}

type draftLink struct {
	Source    string `json:"source"`
	Target    string `json:"target"`
	Stance    string `json:"stance"`
	Rationale string `json:"rationale"`
}

type modelDraftFile struct {
	Entities      *[]draftEntityFile `json:"entities"`
	EvidenceLinks *[]draftLink       `json:"evidence_links"`
}

type draftEntityFile struct {
	Ref        string  `json:"ref"`
	Type       string  `json:"type"`
	Text       string  `json:"text"`
	SourceRef  *string `json:"source_ref"`
	PromptedBy *string `json:"prompted_by"`
	ExistingID *string `json:"existing_id"`
}

func (f *modelDraftFile) UnmarshalJSON(data []byte) error {
	type plain modelDraftFile
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	return dec.Decode((*plain)(f))
}

type draftViolations []string

func (v *draftViolations) check(ok bool, path, message string) bool {
	if !ok {
		*v = append(*v, path+": "+message)
	}
	return ok
}

func nonBlank(s string) bool { return strings.TrimSpace(s) != "" }

func parseModelDraft(file modelDraftFile) (modelDraft, error) {
	var v draftViolations
	var entities []draftEntityFile
	if v.check(file.Entities != nil, "/entities", "expected an array") {
		entities = *file.Entities
	}
	var links []draftLink
	if v.check(file.EvidenceLinks != nil, "/evidence_links", "expected an array") {
		links = *file.EvidenceLinks
	}

	kinds := map[string]string{}
	existing := map[string]bool{}
	for i, e := range entities {
		path := fmt.Sprintf("/entities/%d", i)
		if v.check(nonBlank(e.Ref), path+"/ref", "must be nonempty") {
			if _, dup := kinds[e.Ref]; v.check(!dup, path+"/ref", "repeats an earlier ref") {
				kinds[e.Ref] = e.Type
			}
		}
		v.check(slices.Contains(entityTypeVocab, e.Type), path+"/type", "must be one of "+strings.Join(entityTypeVocab, ", "))
		v.check(nonBlank(e.Text), path+"/text", "must be nonempty")
		v.check(e.SourceRef == nil || e.Type == "observation", path+"/source_ref", "only observations take source_ref")
		v.check(e.PromptedBy == nil || e.Type == "action", path+"/prompted_by", "only actions take prompted_by")
		for _, field := range []struct {
			name  string
			value *string
		}{{"existing_id", e.ExistingID}, {"source_ref", e.SourceRef}, {"prompted_by", e.PromptedBy}} {
			v.check(field.value == nil || nonBlank(*field.value), path+"/"+field.name, "must be nonempty; omit it when absent")
		}
		if e.ExistingID != nil && nonBlank(*e.ExistingID) {
			v.check(!existing[*e.ExistingID], path+"/existing_id", "names the same entity as an earlier existing_id")
			existing[*e.ExistingID] = true
		}
	}
	for i, e := range entities {
		if e.PromptedBy != nil && nonBlank(*e.PromptedBy) {
			v.check(kinds[*e.PromptedBy] == "observation", fmt.Sprintf("/entities/%d/prompted_by", i), "must name an observation ref in this file")
		}
	}

	pairs := map[[2]string]bool{}
	for i, l := range links {
		path := fmt.Sprintf("/evidence_links/%d", i)
		if v.check(nonBlank(l.Source), path+"/source", "must be nonempty") {
			v.check(kinds[l.Source] == "observation", path+"/source", "must name an observation ref in this file")
		}
		if v.check(nonBlank(l.Target), path+"/target", "must be nonempty") {
			v.check(slices.Contains(claimTypeVocab, kinds[l.Target]), path+"/target", "must name a hypothesis or impact ref in this file")
		}
		v.check(slices.Contains(stanceVocab, l.Stance), path+"/stance", "must be one of "+strings.Join(stanceVocab, ", "))
		if nonBlank(l.Source) && nonBlank(l.Target) {
			pair := [2]string{l.Source, l.Target}
			v.check(!pairs[pair], path, "repeats the source and target of an earlier link")
			pairs[pair] = true
		}
	}

	if len(v) > 0 {
		return modelDraft{}, invalidModelDraft(strings.Join(v, "\n"))
	}
	draft := modelDraft{EvidenceLinks: links}
	for _, e := range entities {
		draft.Entities = append(draft.Entities, draftEntity{
			Ref: e.Ref, Kind: e.Type, Text: e.Text,
			SourceRef: deref(e.SourceRef), PromptedBy: deref(e.PromptedBy), ExistingID: deref(e.ExistingID),
		})
	}
	return draft, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func invalidModelDraft(details string) error {
	exit2 := fail.ExitUsageError
	return &fail.DetailedError{
		Summary:     "model draft is invalid; nothing was written",
		Details:     details,
		ExitCode:    &exit2,
		Suggestions: []string{"Fix the draft file and re-run; see gcx irm opmodel containers upsert-model --help for its format"},
	}
}
