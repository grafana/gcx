package irm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	fail "github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
)

type shareReceipt struct {
	Type            string                `json:"type"`
	SchemaVersion   string                `json:"schema_version"`
	Container       string                `json:"container"`
	Curated         bool                  `json:"curated"`
	EntityIDsByRef  map[string]string     `json:"entity_ids"`
	EvidenceLinkIDs []string              `json:"evidence_link_ids"`
	Citations       []string              `json:"citations"`
	OpModelRevision string                `json:"opmodel_revision"`
	Message         shareMessage          `json:"message"`
	MessageID       string                `json:"message_id,omitempty"`
	Summary         cmdio.MutationSummary `json:"summary"`
	Failures        []shareStepFailure    `json:"failures"`
}

type shareStepFailure struct {
	Step  string `json:"step"`
	Error string `json:"error"`
}

func publishShare(ctx context.Context, api OpModelAPI, messages MessageAPI, pkg *sharePackage, agentID, agentName string) shareReceipt {
	receipt := shareReceipt{
		Type:            "gcx.irm.share_receipt",
		SchemaVersion:   "2",
		Container:       pkg.Container,
		Curated:         *pkg.Curated,
		EntityIDsByRef:  map[string]string{},
		EvidenceLinkIDs: []string{},
		Citations:       []string{},
		Message:         pkg.Message,
		Failures:        []shareStepFailure{},
	}

	agentName = strings.TrimSpace(agentName)
	if agentName == "" {
		agentName = agentID
	}
	agentActor := opModelActorInput{Kind: "agent", ID: agentID, DisplayName: agentName}

	for _, ent := range pkg.Entities {
		if ent.Type == "action" {
			continue
		}
		id, revision, err := createShareEntity(ctx, api, pkg.Container, agentActor, ent)
		if err != nil {
			receipt.fail(fmt.Sprintf("create %s %q", ent.Type, ent.Ref), err)
			continue
		}
		receipt.EntityIDsByRef[ent.Ref] = id
		receipt.OpModelRevision = revision
		receipt.Summary.Succeeded++
	}

	for _, ent := range pkg.Entities {
		if ent.Type != "action" {
			continue
		}
		promptedByID := ""
		if ent.PromptedBy != "" {
			promptedByID = receipt.EntityIDsByRef[ent.PromptedBy]
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
		receipt.EntityIDsByRef[ent.Ref] = id
		receipt.OpModelRevision = revision
		receipt.Summary.Succeeded++
	}

	for i, b := range pkg.EvidenceLinks {
		sourceID, targetID := receipt.EntityIDsByRef[b.Source], receipt.EntityIDsByRef[b.Target]
		if sourceID == "" || targetID == "" {
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

	for _, ref := range pkg.MessageRefs {
		if id := receipt.EntityIDsByRef[ref]; id != "" {
			receipt.Citations = append(receipt.Citations, id)
		}
	}

	postShareMessage(ctx, messages, pkg, agentActor, &receipt)
	return receipt
}

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

func (r *shareReceipt) fail(step string, err error) {
	r.Summary.Failed++
	r.Failures = append(r.Failures, shareStepFailure{Step: step, Error: stepErrorText(err)})
}

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
