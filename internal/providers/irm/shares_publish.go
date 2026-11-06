// shares_publish.go creates model entities and relationships, then shares a
// message through the independent Incident message API. There is no rollback
// or idempotency; the receipt records partial results for recovery.
package irm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	fail "github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
)

// shareReceipt is the single JSON result document `shares publish` emits,
// success or failure.
type shareReceipt struct {
	Type            string            `json:"type"` // "gcx.irm.share_receipt"
	SchemaVersion   string            `json:"schema_version"`
	Container       string            `json:"container"`
	Curated         bool              `json:"curated"`
	EntityIDs       map[string]string `json:"entity_ids"` // package ref -> minted op model id
	EvidenceLinkIDs []string          `json:"evidence_link_ids"`
	Citations       []string          `json:"citations"` // minted ids the message refers to
	OpModelRevision string            `json:"opmodel_revision"`
	Message         shareMessage      `json:"message"`
	// MessageID identifies the saved Incident message. If absent after a failure,
	// inspect Incident before retrying: an ambiguous failure may follow a save.
	MessageID string `json:"message_id,omitempty"`
	// Summary counts publish steps: one per entity, one per evidence link, one for
	// the message. Skipped covers dependent actions or evidence-links whose inputs
	// were never minted because an entity create failed. It reuses the repo's
	// batch vocabulary so agents meet one summary shape everywhere.
	Summary  cmdio.MutationSummary `json:"summary"`
	Failures []shareStepFailure    `json:"failures"`
}

type shareStepFailure struct {
	Step  string `json:"step"`
	Error string `json:"error"`
}

// publishShare runs the publish sequence against an already-validated
// package. It attempts every step whose inputs exist and never stops early:
// the receipt is the complete, honest record of what was minted and said.
func publishShare(ctx context.Context, api OpModelAPI, messages MessageAPI, pkg *sharePackage, agentID, agentName string) shareReceipt {
	receipt := shareReceipt{
		Type:            "gcx.irm.share_receipt",
		SchemaVersion:   "2",
		Container:       pkg.Container,
		Curated:         pkg.Curated != nil && *pkg.Curated,
		EntityIDs:       map[string]string{},
		EvidenceLinkIDs: []string{},
		Citations:       []string{},
		Message:         pkg.Message,
		Failures:        []shareStepFailure{},
	}

	agentName = strings.TrimSpace(agentName)
	if agentName == "" {
		agentName = agentID
	}
	// Entities are created by the agent actor. The authenticated Grafana user
	// is attributed to the update by the server.
	agentActor := opModelActorInput{Kind: "agent", ID: agentID, DisplayName: agentName}

	// Create observations and claims first so linked actions can resolve their
	// prompting observation regardless of package entity order.
	for _, ent := range pkg.Entities {
		if ent.Type == "action" {
			continue
		}
		id, revision, err := createShareEntity(ctx, api, pkg.Container, agentActor, ent)
		if err != nil {
			receipt.fail(fmt.Sprintf("create %s %q", ent.Type, ent.Ref), err)
			continue
		}
		receipt.EntityIDs[ent.Ref] = id
		receipt.OpModelRevision = revision
		receipt.Summary.Succeeded++
	}

	for _, ent := range pkg.Entities {
		if ent.Type != "action" {
			continue
		}
		promptedByID := ""
		if ent.PromptedBy != "" {
			promptedByID = receipt.EntityIDs[ent.PromptedBy]
			if promptedByID == "" {
				receipt.Summary.Skipped++
				continue
			}
		}
		id, revision, err := createShareAction(ctx, api, pkg.Container, agentActor, ent, promptedByID)
		if err != nil {
			receipt.fail(fmt.Sprintf("create action %q", ent.Ref), err)
			continue
		}
		receipt.EntityIDs[ent.Ref] = id
		receipt.OpModelRevision = revision
		receipt.Summary.Succeeded++
	}

	for i, b := range pkg.EvidenceLinks {
		sourceID, targetID := receipt.EntityIDs[b.Source], receipt.EntityIDs[b.Target]
		if sourceID == "" || targetID == "" {
			// An endpoint was never minted (its create failed above); the
			// evidence link is skipped, not failed.
			receipt.Summary.Skipped++
			continue
		}
		resp, err := api.AttachEvidence(ctx, attachEvidenceRequest{
			Container: pkg.Container,
			Actor:     agentActor,
			SourceID:  sourceID,
			TargetID:  targetID,
			Stance:    b.Stance,
			Rationale: b.Rationale,
		})
		if err != nil {
			receipt.fail(fmt.Sprintf("attach evidence link %s->%s (#%d)", b.Source, b.Target, i), err)
			continue
		}
		receipt.EvidenceLinkIDs = append(receipt.EvidenceLinkIDs, resp.EvidenceID)
		receipt.OpModelRevision = resp.Revision
		receipt.Summary.Succeeded++
	}

	// Citations record the minted message_refs in this local receipt only. A ref whose entity failed is dropped
	// rather than cited as nothing.
	for _, ref := range pkg.MessageRefs {
		if id := receipt.EntityIDs[ref]; id != "" {
			receipt.Citations = append(receipt.Citations, id)
		}
	}

	postShareMessage(ctx, messages, pkg, agentActor, &receipt)
	return receipt
}

// postShareMessage publishes independently of the model. A
// failure here counts as a failed step, so the command exits 4: the entities
// landed but the finding was never said.
func postShareMessage(ctx context.Context, api MessageAPI, pkg *sharePackage, agentActor opModelActorInput, receipt *shareReceipt) {
	resp, err := api.CreateMessage(ctx, createMessageRequest{
		IncidentID: pkg.Container,
		DraftedBy:  &messageAgent{ID: agentActor.ID, DisplayName: agentActor.DisplayName},
		Body:       pkg.Message.Text,
		See:        pkg.Message.See,
	})
	if err != nil {
		receipt.fail("share message", err)
		return
	}
	receipt.MessageID = resp.MessageID
	receipt.Summary.Succeeded++
}

// fail records one failed step against the receipt.
func (r *shareReceipt) fail(step string, err error) {
	r.Summary.Failed++
	r.Failures = append(r.Failures, shareStepFailure{Step: step, Error: stepErrorText(err)})
}

// stepErrorText renders one step's error for a JSON field. Every op model
// client error is a DetailedError, whose Error() is a terminal rendering
// (colour, box-drawing rules, suggestions); legible on stderr, garbage inside
// a machine document. The receipt takes the summary and details only.
func stepErrorText(err error) string {
	var detailed *fail.DetailedError
	if !errors.As(err, &detailed) {
		return err.Error()
	}
	if detailed.Details == "" {
		return detailed.Summary
	}
	return detailed.Summary + ": " + detailed.Details
}

// createShareEntity maps one package entity onto its op model create call and
// returns the minted id and the new revision.
func createShareEntity(ctx context.Context, api OpModelAPI, container string, actor opModelActorInput, ent shareEntity) (string, string, error) {
	switch ent.Type {
	case "observation":
		resp, err := api.CreateObservation(ctx, createObservationRequest{
			Container: container,
			Actor:     actor,
			Text:      ent.Text,
			SourceRef: ent.SourceRef,
		})
		if err != nil {
			return "", "", err
		}
		return resp.ObservationID, resp.Revision, nil
	case "impact", "hypothesis":
		resp, err := api.CreateClaim(ctx, createClaimRequest{
			Container: container,
			Actor:     actor,
			Type:      ent.Type,
			Text:      ent.Text,
		})
		if err != nil {
			return "", "", err
		}
		return resp.ClaimID, resp.Revision, nil
	default:
		// Unreached after validateSharePackage; kept as a guard for run
		// functions driven directly.
		return "", "", fmt.Errorf("unknown entity type %q", ent.Type)
	}
}

func createShareAction(ctx context.Context, api OpModelAPI, container string, actor opModelActorInput, ent shareEntity, promptedByID string) (string, string, error) {
	if promptedByID != "" {
		resp, err := api.PromoteToAction(ctx, promoteToActionRequest{
			Container:     container,
			ObservationID: promptedByID,
			Actor:         actor,
			Text:          ent.Text,
		})
		if err != nil {
			return "", "", err
		}
		return resp.ActionID, resp.Revision, nil
	}

	resp, err := api.CreateAction(ctx, createActionRequest{
		Container: container,
		Actor:     actor,
		Text:      ent.Text,
	})
	if err != nil {
		return "", "", err
	}
	return resp.ActionID, resp.Revision, nil
}
