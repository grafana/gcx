//nolint:testpackage // white-box tests require access to unexported package types
package irm

import (
	"strings"
	"testing"
)

func validBasePackage() sharePackage {
	return sharePackage{
		Version:   2,
		Container: "INC-1",
		Message:   shareMessage{Text: "DB connection pool exhaustion is the likely cause."},
		Entities: []shareEntity{
			{Ref: "obs1", Type: "observation", Text: "pool at 100% since 14:02", SourceRef: "https://x"},
			{Ref: "hyp1", Type: "hypothesis", Text: "pool exhaustion causes the errors"},
			{Ref: "imp1", Type: "impact", Text: "checkout failing for EU"},
			{Ref: "act1", Type: "action", Text: "bump pool size", PromptedBy: "obs1"},
		},
		EvidenceLinks: []shareEvidenceLink{
			{Source: "obs1", Target: "hyp1", Stance: "supports", Rationale: "timing aligns"},
			{Source: "obs1", Target: "hyp1", Stance: "challenges"},
		},
		MessageRefs: []string{"hyp1"},
		Curated:     new(true),
	}
}

func TestValidateSharePackage(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(p *sharePackage)
		wantErr string
	}{
		{"valid package", func(_ *sharePackage) {}, ""},
		{"wrong version", func(p *sharePackage) { p.Version = 1 }, "version must be 2"},
		{"missing container", func(p *sharePackage) { p.Container = "  " }, "container is required"},
		{"missing message text", func(p *sharePackage) { p.Message.Text = "" }, "message.text is required"},
		{"missing entity ref", func(p *sharePackage) { p.Entities[0].Ref = "" }, "missing ref"},
		{"duplicate ref", func(p *sharePackage) { p.Entities[1].Ref = "obs1" }, "duplicate ref"},
		{"invalid entity type", func(p *sharePackage) { p.Entities[0].Type = "finding" }, `invalid type "finding"`},
		{"missing entity text", func(p *sharePackage) { p.Entities[0].Text = " " }, "text is required"},
		{"multiple hypotheses", func(p *sharePackage) {
			p.Entities = append(p.Entities,
				shareEntity{Ref: "hyp2", Type: "hypothesis", Text: "cache eviction causes the errors"},
				shareEntity{Ref: "hyp3", Type: "hypothesis", Text: "database outage causes the errors"})
			p.EvidenceLinks = append(p.EvidenceLinks,
				shareEvidenceLink{Source: "obs1", Target: "hyp2", Stance: "challenges"},
				shareEvidenceLink{Source: "obs1", Target: "hyp3", Stance: "context"})
			p.MessageRefs = append(p.MessageRefs, "hyp2", "hyp3")
		}, ""},
		{"too many actions", func(p *sharePackage) {
			p.Entities = append(p.Entities, shareEntity{Ref: "a2", Type: "action", Text: "x"}, shareEntity{Ref: "a3", Type: "action", Text: "x"})
		}, "too many action entities"},
		{"too many impacts", func(p *sharePackage) {
			p.Entities = append(p.Entities, shareEntity{Ref: "i2", Type: "impact", Text: "x"})
		}, "too many impact entities"},
		{"too many observations", func(p *sharePackage) {
			p.Entities = append(p.Entities,
				shareEntity{Ref: "o2", Type: "observation", Text: "x"},
				shareEntity{Ref: "o3", Type: "observation", Text: "x"},
				shareEntity{Ref: "o4", Type: "observation", Text: "x"})
		}, "too many observation entities"},
		{"unknown bearing source", func(p *sharePackage) { p.EvidenceLinks[0].Source = "ghost" }, `unknown source ref "ghost"`},
		{"unknown bearing target", func(p *sharePackage) { p.EvidenceLinks[0].Target = "ghost" }, `unknown target ref "ghost"`},
		{"self bearing", func(p *sharePackage) { p.EvidenceLinks[0].Target = "obs1"; p.EvidenceLinks[0].Source = "obs1" }, "self-evidence link"},
		{"impact as source", func(p *sharePackage) { p.EvidenceLinks[0].Source = "imp1" }, "source imp1 is a impact"},
		{"hypothesis as source", func(p *sharePackage) { p.EvidenceLinks[0].Source = "hyp1"; p.EvidenceLinks[0].Target = "imp1" }, "source hyp1 is a hypothesis"},
		{"action as target", func(p *sharePackage) { p.EvidenceLinks[0].Target = "act1" }, "target act1 is a action"},
		{"invalid stance", func(p *sharePackage) { p.EvidenceLinks[0].Stance = "proves" }, `invalid stance "proves"`},
		{"prompted by unknown ref", func(p *sharePackage) { p.Entities[3].PromptedBy = "ghost" }, `prompted_by references unknown ref "ghost"`},
		{"prompted by non-observation", func(p *sharePackage) { p.Entities[3].PromptedBy = "hyp1" }, `prompted_by ref "hyp1" is a hypothesis`},
		{"observation and prompted action without a claim", func(p *sharePackage) {
			p.Entities = []shareEntity{p.Entities[0], p.Entities[3]}
			p.EvidenceLinks = nil
			p.MessageRefs = []string{"act1"}
		}, ""},
		{"prompted by on non-action", func(p *sharePackage) { p.Entities[1].PromptedBy = "obs1" }, "prompted_by is only valid for actions"},
		{"standalone action", func(p *sharePackage) { p.Entities[3].PromptedBy = "" }, ""},
		{"second evidence link is optional", func(p *sharePackage) { p.EvidenceLinks = p.EvidenceLinks[:1] }, ""},
		{"unknown message ref", func(p *sharePackage) { p.MessageRefs = []string{"ghost"} }, `message_refs: unknown ref "ghost"`},
		{"missing curated", func(p *sharePackage) { p.Curated = nil }, "curated must be true or false"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := validBasePackage()
			tt.mutate(&pkg)
			errs := validateSharePackage(&pkg)
			if tt.wantErr == "" {
				if len(errs) != 0 {
					t.Fatalf("expected valid, got %v", errs)
				}
				return
			}
			if len(errs) == 0 {
				t.Fatal("expected violations, got none")
			}
			found := false
			for _, e := range errs {
				if strings.Contains(e, tt.wantErr) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("violations %v do not contain %q", errs, tt.wantErr)
			}
		})
	}
}

func TestValidateSharePackageAggregatesAllViolations(t *testing.T) {
	pkg := validBasePackage()
	pkg.Version = 1
	pkg.Container = ""
	pkg.Message.Text = ""
	pkg.Curated = nil
	errs := validateSharePackage(&pkg)
	if len(errs) < 4 {
		t.Errorf("expected all violations aggregated (>=4), got %d: %v", len(errs), errs)
	}
}
