package irm

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	fail "github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type entityKey struct{ kind, text, extra string }

func newEntityKey(kind, text, extra string) entityKey {
	return entityKey{kind, strings.TrimSuffix(strings.Join(strings.Fields(text), " "), "."), extra}
}

type linkKey struct{ source, target, stance string }

type indexedEntity struct {
	kind    string
	version uint64
}

type modelIndex struct {
	revision  string
	entities  map[string]indexedEntity
	keys      map[entityKey]string
	active    map[linkKey]string
	withdrawn map[linkKey]string
}

func indexModel(body *containerBody) modelIndex {
	index := modelIndex{
		entities:  map[string]indexedEntity{},
		keys:      map[entityKey]string{},
		active:    map[linkKey]string{},
		withdrawn: map[linkKey]string{},
	}
	if body == nil {
		return index
	}
	index.revision = body.Revision
	add := func(id, kind, text, extra string, version uint64) {
		index.entities[id] = indexedEntity{kind, version}
		key := newEntityKey(kind, text, extra)
		if _, ok := index.keys[key]; !ok {
			index.keys[key] = id
		}
	}
	for _, o := range body.Observations {
		add(o.ID, "observation", o.Text, o.SourceRef, o.ContentVersion)
	}
	for _, c := range body.Claims {
		add(c.ID, c.Type, c.Text, "", c.ContentVersion)
	}
	for _, a := range body.Actions {
		origin := ""
		if a.Origin != nil {
			origin = a.Origin.ObservationID
		}
		add(a.ID, "action", a.Text, origin, a.ContentVersion)
	}
	current := func(id string, version uint64) bool {
		e, ok := index.entities[id]
		return ok && e.version == version
	}
	for _, l := range body.EvidenceLinks {
		key := linkKey{l.SourceID, l.TargetID, l.Stance}
		target := index.active
		switch {
		case l.Withdrawal != nil:
			target = index.withdrawn
		case !current(l.SourceID, l.SourceVersion) || !current(l.TargetID, l.TargetVersion):
			continue
		}
		if _, ok := target[key]; !ok {
			target[key] = l.ID
		}
	}
	return index
}

const (
	stepCreateObservation = "create-observation"
	stepCreateClaim       = "create-claim"
	stepAttachLink        = "attach-evidence-link"
	stepPromoteToAction   = "promote-observation-to-action"
	stepCreateAction      = "create-action"
)

type upsertStep struct {
	action string
	entity draftEntity
	link   draftLink
}

type upsertStepRef struct {
	Action string `json:"action"`
	Ref    string `json:"ref,omitempty"`
	Source string `json:"source,omitempty"`
	Target string `json:"target,omitempty"`
}

func (s upsertStep) describe() upsertStepRef {
	if s.action == stepAttachLink {
		return upsertStepRef{Action: s.action, Source: s.link.Source, Target: s.link.Target}
	}
	return upsertStepRef{Action: s.action, Ref: s.entity.Ref}
}

type skippedLink struct {
	Source     string `json:"source"`
	Target     string `json:"target"`
	Stance     string `json:"stance"`
	ExistingID string `json:"existing_id"`
}

type savedLink struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Stance string `json:"stance"`
	ID     string `json:"id"`
}

type upsertPlan struct {
	modelRevision string
	known         map[string]string
	reused        map[string]string
	skippedLinks  []skippedLink
	steps         []upsertStep
}

// draftKey reports false while an action's prompting observation has no id
// yet, because the container keys actions by that observation's id.
func draftKey(e draftEntity, known map[string]string) (entityKey, bool) {
	if e.PromptedBy == "" {
		return newEntityKey(e.Kind, e.Text, e.SourceRef), true
	}
	if id, ok := known[e.PromptedBy]; ok {
		return newEntityKey(e.Kind, e.Text, id), true
	}
	return newEntityKey(e.Kind, e.Text, e.PromptedBy), false
}

func reconcileModel(draft modelDraft, index modelIndex) (upsertPlan, error) {
	plan := upsertPlan{modelRevision: index.revision, known: map[string]string{}, reused: map[string]string{}, skippedLinks: []skippedLink{}}

	var promptable, actions []draftEntity
	for _, e := range draft.Entities {
		if e.Kind == "action" {
			actions = append(actions, e)
		} else {
			promptable = append(promptable, e)
		}
	}
	seen := map[entityKey]string{}
	owners := map[string]string{}
	for _, e := range slices.Concat(promptable, actions) {
		key, resolved := draftKey(e, plan.known)
		if other, dup := seen[key]; dup {
			return upsertPlan{}, invalidModelDraft(fmt.Sprintf("%s and %s repeat each other; merge them", other, e.Ref))
		}
		seen[key] = e.Ref
		switch id, match := index.keys[key]; {
		case e.ExistingID != "":
			if index.entities[e.ExistingID].kind != e.Kind {
				return upsertPlan{}, invalidModelDraft(fmt.Sprintf("%s: no %s %s in the current model", e.Ref, e.Kind, e.ExistingID))
			}
			plan.known[e.Ref] = e.ExistingID
		case resolved && match:
			plan.known[e.Ref] = id
			plan.reused[e.Ref] = id
		}
		if id, ok := plan.known[e.Ref]; ok {
			if other, taken := owners[id]; taken {
				return upsertPlan{}, invalidModelDraft(fmt.Sprintf("%s and %s both resolve to %s; merge them", other, e.Ref, id))
			}
			owners[id] = e.Ref
		}
	}

	var links []draftLink
	for _, l := range draft.EvidenceLinks {
		source, sourceKnown := plan.known[l.Source]
		target, targetKnown := plan.known[l.Target]
		if sourceKnown && targetKnown {
			key := linkKey{source, target, l.Stance}
			if id, ok := index.active[key]; ok {
				plan.skippedLinks = append(plan.skippedLinks, skippedLink{l.Source, l.Target, l.Stance, id})
				continue
			}
			if id, ok := index.withdrawn[key]; ok {
				return upsertPlan{}, invalidModelDraft(fmt.Sprintf("%s->%s repeats withdrawn evidence link %s; remove it from the draft", l.Source, l.Target, id))
			}
		}
		links = append(links, l)
	}

	for _, kind := range []string{"observation", "hypothesis", "impact"} {
		action := stepCreateClaim
		if kind == "observation" {
			action = stepCreateObservation
		}
		for _, e := range promptable {
			if _, ok := plan.known[e.Ref]; !ok && e.Kind == kind {
				plan.steps = append(plan.steps, upsertStep{action: action, entity: e})
			}
		}
	}
	for _, l := range links {
		plan.steps = append(plan.steps, upsertStep{action: stepAttachLink, link: l})
	}
	for _, e := range actions {
		if _, ok := plan.known[e.Ref]; ok {
			continue
		}
		action := stepCreateAction
		if e.PromptedBy != "" {
			action = stepPromoteToAction
		}
		plan.steps = append(plan.steps, upsertStep{action: action, entity: e})
	}
	return plan, nil
}

type modelUpsertReceipt struct {
	Type          string            `json:"type"`
	SchemaVersion string            `json:"schema_version"`
	Status        string            `json:"status"`
	Container     string            `json:"container"`
	ModelRevision *string           `json:"model_revision"`
	Revision      *string           `json:"revision"`
	Entities      map[string]string `json:"entities"`
	Reused        map[string]string `json:"reused"`
	EvidenceLinks []savedLink       `json:"evidence_links"`
	SkippedLinks  []skippedLink     `json:"skipped_links"`
	Planned       []upsertStepRef   `json:"planned,omitzero"`
	StoppedAt     *upsertStepRef    `json:"stopped_at,omitempty"`
	Error         string            `json:"error,omitempty"`
}

func optionalRevision(revision string) *string {
	if revision == "" {
		return nil
	}
	return &revision
}

func (p upsertPlan) receipt(container, status string) modelUpsertReceipt {
	return modelUpsertReceipt{
		Type:          "gcx.irm.opmodel.model_upsert",
		SchemaVersion: "1",
		Status:        status,
		Container:     container,
		ModelRevision: optionalRevision(p.modelRevision),
		Revision:      optionalRevision(p.modelRevision),
		Entities:      p.known,
		Reused:        p.reused,
		EvidenceLinks: []savedLink{},
		SkippedLinks:  p.skippedLinks,
	}
}

func (p upsertPlan) dryRun(container string) modelUpsertReceipt {
	r := p.receipt(container, "planned")
	r.Planned = []upsertStepRef{}
	for _, s := range p.steps {
		r.Planned = append(r.Planned, s.describe())
	}
	return r
}

func (p upsertPlan) execute(ctx context.Context, api OpModelAPI, container string, actor opModelActorInput) (modelUpsertReceipt, error) {
	r := p.receipt(container, "saved")
	ids := r.Entities
	for _, s := range p.steps {
		id, revision, err := runUpsertStep(ctx, api, container, actor, s, ids)
		if err == nil && id == "" {
			err = errors.New("the server returned no id")
		}
		if err == nil && s.action == stepPromoteToAction && id == ids[s.entity.PromptedBy] {
			err = errors.New("the server returned the observation id as the promoted action id")
		}
		if err != nil {
			stoppedAt := s.describe()
			r.Status, r.StoppedAt, r.Error = "stopped", &stoppedAt, stepErrorText(err)
			return r, err
		}
		r.Revision = &revision
		if s.action == stepAttachLink {
			r.EvidenceLinks = append(r.EvidenceLinks, savedLink{s.link.Source, s.link.Target, s.link.Stance, id})
		} else {
			ids[s.entity.Ref] = id
		}
	}
	return r, nil
}

func runUpsertStep(ctx context.Context, api OpModelAPI, container string, actor opModelActorInput, s upsertStep, ids map[string]string) (string, string, error) {
	e := s.entity
	switch s.action {
	case stepCreateObservation:
		resp, err := api.CreateObservation(ctx, createObservationRequest{Container: container, Actor: actor, Text: e.Text, SourceRef: e.SourceRef})
		if err != nil {
			return "", "", err
		}
		return resp.ObservationID, resp.Revision, nil
	case stepCreateClaim:
		resp, err := api.CreateClaim(ctx, createClaimRequest{Container: container, Actor: actor, Type: e.Kind, Text: e.Text})
		if err != nil {
			return "", "", err
		}
		return resp.ClaimID, resp.Revision, nil
	case stepAttachLink:
		resp, err := api.AttachEvidence(ctx, attachEvidenceRequest{
			Container: container, Actor: actor,
			SourceID: ids[s.link.Source], TargetID: ids[s.link.Target],
			Stance: s.link.Stance, Rationale: s.link.Rationale,
		})
		if err != nil {
			return "", "", err
		}
		return resp.EvidenceID, resp.Revision, nil
	case stepPromoteToAction:
		resp, err := api.PromoteToAction(ctx, promoteToActionRequest{Container: container, ObservationID: ids[e.PromptedBy], Actor: actor, Text: e.Text})
		if err != nil {
			return "", "", err
		}
		return resp.ActionID, resp.Revision, nil
	default:
		resp, err := api.CreateAction(ctx, createActionRequest{Container: container, Actor: actor, Text: e.Text})
		if err != nil {
			return "", "", err
		}
		return resp.ActionID, resp.Revision, nil
	}
}

func stepErrorText(err error) string {
	var detailed *fail.DetailedError
	switch {
	case !errors.As(err, &detailed):
		return err.Error()
	case detailed.Details == "":
		return detailed.Summary
	default:
		return detailed.Summary + ": " + detailed.Details
	}
}

type containersUpsertModelOpts struct {
	IO        cmdio.Options
	Container string
	Filename  string
	DryRun    bool
	Actor     opModelActorOpts
}

func (o *containersUpsertModelOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.Filename, "filename", "f", "", "Path to the model draft JSON or YAML (use - for stdin) (required)")
	flags.BoolVar(&o.DryRun, "dry-run", false, "Read the container and print the planned writes without making them")
	o.Actor.bind(flags)
	setupOpModelReadIO(&o.IO, flags)
}

func (o *containersUpsertModelOpts) Validate() error {
	if err := requireOpModelArg("container", o.Container); err != nil {
		return err
	}
	if err := requireOpModelFlag("filename", o.Filename); err != nil {
		return err
	}
	return o.Actor.validate()
}

func newContainersUpsertModelCmd(loader OpModelClientLoader, shared *opModelSharedOpts) *cobra.Command {
	opts := &containersUpsertModelOpts{}
	cmd := &cobra.Command{
		Use:   "upsert-model <container>",
		Short: "Save a model draft: create what is missing, reuse what is already there.",
		Long: `Save a model draft file into the container's operational model. The command
writes observations, then claims, then evidence links, then actions, with one
request each, and stops at the first failed write. An entity that exactly
repeats one already in the container (same kind and source or prompting
observation, and the same text apart from spacing and one trailing period) is
reused, and an evidence link that is already active is skipped, so rerunning
the same file after a stopped run creates only what is still missing. Existing
entities are never edited. --dry-run reads the container and lists the planned
writes without making them.

The draft is a JSON or YAML object with two arrays. Each "entities" item has a
unique "ref", a "type" (observation, hypothesis, impact or action) and "text";
an observation may add "source_ref", an action may add "prompted_by" (the ref
of an observation it was prompted by), and any entity may name a container
entity of its kind with "existing_id". Each "evidence_links" item has a
"source" observation ref, a "target" hypothesis or impact ref, a "stance"
(supports, challenges or context) and an optional "rationale".`,
		Example: `  gcx irm opmodel containers upsert-model INC-123 -f draft.json --dry-run \
    --actor-kind agent --actor-id claude-code
  gcx irm opmodel containers upsert-model INC-123 -f draft.json \
    --actor-kind agent --actor-id claude-code --actor-name "Claude Code"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Container = args[0]
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			return runContainersUpsertModel(cmd, opts, loader, shared)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func runContainersUpsertModel(cmd *cobra.Command, opts *containersUpsertModelOpts, loader OpModelClientLoader, shared *opModelSharedOpts) error {
	if err := validateOpModelInputs(shared, opts); err != nil {
		return err
	}
	var file modelDraftFile
	if err := providers.ReadFileOrStdin(opts.Filename, cmd.InOrStdin(), &file); err != nil {
		return invalidModelDraft(err.Error())
	}
	draft, err := parseModelDraft(file)
	if err != nil {
		return err
	}
	return withOpModel(cmd, loader, shared, func(ctx context.Context, api OpModelAPI) error {
		body, err := api.GetContainer(ctx, getContainerRequest{Container: opts.Container})
		if err != nil && !isContainerNotFound(err, opts.Container) {
			return err
		}
		plan, err := reconcileModel(draft, indexModel(body))
		if err != nil {
			return err
		}
		if opts.DryRun {
			return opts.IO.Encode(cmd.OutOrStdout(), plan.dryRun(opts.Container))
		}
		receipt, writeErr := plan.execute(ctx, api, opts.Container, opts.Actor.actor())
		if err := opts.IO.Encode(cmd.OutOrStdout(), receipt); err != nil {
			return err
		}
		if writeErr != nil {
			return fail.NewEmittedError(fail.ExitPartialFailure, writeErr)
		}
		if receipt.Revision != nil {
			cmdio.EmitHint(cmd.ErrOrStderr(), "Inspect the saved model", "gcx irm opmodel containers get "+opts.Container)
		}
		return nil
	})
}
