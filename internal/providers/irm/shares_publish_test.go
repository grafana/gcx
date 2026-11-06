//nolint:testpackage // white-box tests require access to unexported publish internals
package irm

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	fail "github.com/grafana/gcx/internal/gcxerrors"
)

func TestPublishShareHappyPath(t *testing.T) {
	op := &fakeOpModel{}
	pkg := validBasePackage()

	receipt := publishShare(context.Background(), op, op, &pkg, "claude-code", "Claude Code")

	if receipt.Summary.Succeeded != 7 || receipt.Summary.Failed != 0 || receipt.Summary.Skipped != 0 {
		t.Errorf("summary wrong: %+v", receipt.Summary)
	}
	if len(receipt.EntityIDsByRef) != 4 || receipt.EntityIDsByRef["obs1"] != "obs-1" {
		t.Errorf("entity ids wrong: %v", receipt.EntityIDsByRef)
	}
	if len(receipt.EvidenceLinkIDs) != 2 {
		t.Errorf("bearing ids wrong: %v", receipt.EvidenceLinkIDs)
	}
	if receipt.OpModelRevision != "106" {
		t.Error("opmodel revision not carried")
	}
	if receipt.MessageID == "" {
		t.Error("the message must be recorded as an update")
	}
	if len(receipt.Citations) != 1 || receipt.Citations[0] != receipt.EntityIDsByRef["hyp1"] {
		t.Errorf("citations must be minted ids for message_refs: %v", receipt.Citations)
	}

	attach := reqAs[attachEvidenceRequest](t, op.reqs, 4)
	if attach.SourceID != "obs-1" || attach.TargetID != receipt.EntityIDsByRef["hyp1"] {
		t.Errorf("bearing endpoints not remapped to minted ids: %+v", attach)
	}
	if attach.Stance != "supports" || attach.Rationale != "timing aligns" {
		t.Errorf("bearing fields not mapped: %+v", attach)
	}

	promote := reqAs[promoteToActionRequest](t, op.reqs, 3)
	if promote.ObservationID != "obs-1" || promote.Text != "bump pool size" {
		t.Errorf("linked action not promoted from its minted observation: %+v", promote)
	}

	create := reqAs[createObservationRequest](t, op.reqs, 0)
	if create.Actor.Kind != "agent" || create.Actor.ID != "claude-code" || create.Actor.DisplayName != "Claude Code" {
		t.Errorf("entity actor wrong: %+v", create.Actor)
	}
}

func TestPublishSharePostsUpdateWithAgentAttribution(t *testing.T) {
	op := &fakeOpModel{messageResponse: &createMessageResponse{MessageID: "upd-7"}}
	pkg := validBasePackage()
	pkg.Message.See = "payments/db/pool.go:112"

	receipt := publishShare(context.Background(), op, op, &pkg, "claude-code", "Claude Code")

	upd := reqAs[createMessageRequest](t, op.reqs, 6)
	if upd.IncidentID != "INC-1" {
		t.Errorf("update misrouted: %+v", upd)
	}
	if upd.DraftedBy == nil || upd.DraftedBy.ID != "claude-code" || upd.DraftedBy.DisplayName != "Claude Code" {
		t.Errorf("the composing agent must be recorded as drafted_by: %+v", upd.DraftedBy)
	}
	if upd.Body != pkg.Message.Text || upd.See != "payments/db/pool.go:112" {
		t.Errorf("message not mapped onto the update: %+v", upd)
	}
	if strings.Contains(upd.Body, "obs-1") || strings.Contains(upd.Body, "clm-") {
		t.Errorf("entity ids belong in citations, not in the prose: %q", upd.Body)
	}
	if receipt.MessageID != "upd-7" {
		t.Errorf("receipt must report the minted update id, got %q", receipt.MessageID)
	}
}

func TestPublishShareDefaultsAgentNameToID(t *testing.T) {
	op := &fakeOpModel{}
	pkg := validBasePackage()

	publishShare(context.Background(), op, op, &pkg, "codex", "")

	create := reqAs[createObservationRequest](t, op.reqs, 0)
	if create.Actor.DisplayName != "codex" {
		t.Errorf("agent display name = %q, want agent id", create.Actor.DisplayName)
	}
}

func TestPublishShareCreatesStandaloneActionWithoutPrompt(t *testing.T) {
	op := &fakeOpModel{}
	pkg := validBasePackage()
	pkg.Entities[3].PromptedBy = ""

	publishShare(context.Background(), op, op, &pkg, "codex", "Codex")

	create := reqAs[createActionRequest](t, op.reqs, 3)
	if create.Text != "bump pool size" {
		t.Errorf("standalone action text = %q", create.Text)
	}
}

func TestPublishShareResolvesPromptingObservationRegardlessOfEntityOrder(t *testing.T) {
	op := &fakeOpModel{}
	pkg := validBasePackage()
	pkg.Entities[0], pkg.Entities[3] = pkg.Entities[3], pkg.Entities[0]

	receipt := publishShare(context.Background(), op, op, &pkg, "codex", "Codex")

	promote := reqAs[promoteToActionRequest](t, op.reqs, 3)
	if promote.ObservationID != receipt.EntityIDsByRef["obs1"] {
		t.Errorf("prompting observation id = %q, want minted observation", promote.ObservationID)
	}
}

func TestPublishShareSkipsLinkedActionWhenPromptingObservationFails(t *testing.T) {
	op := &fakeOpModel{failAt: 1}
	pkg := validBasePackage()

	receipt := publishShare(context.Background(), op, op, &pkg, "codex", "Codex")

	if _, minted := receipt.EntityIDsByRef["act1"]; minted {
		t.Error("action whose prompting observation failed must not be minted")
	}
	if receipt.Summary.Skipped != 3 {
		t.Errorf("linked action and its observation bearing should be skipped: %+v", receipt.Summary)
	}
	for _, req := range op.reqs {
		if _, promoted := req.(promoteToActionRequest); promoted {
			t.Error("must not promote an action without a minted prompting observation")
		}
	}
}

func TestPublishShareUpdateFailureIsAFailedStep(t *testing.T) {
	op := &fakeOpModel{failAt: 7}
	pkg := validBasePackage()

	receipt := publishShare(context.Background(), op, op, &pkg, "claude-code", "Claude Code")

	if receipt.Summary.Succeeded != 6 || receipt.Summary.Failed != 1 {
		t.Errorf("summary wrong: %+v", receipt.Summary)
	}
	if receipt.MessageID != "" {
		t.Errorf("a failed update must not report an id: %q", receipt.MessageID)
	}
	if len(receipt.Failures) != 1 || receipt.Failures[0].Step != "share message" {
		t.Errorf("failures wrong: %+v", receipt.Failures)
	}
}

func TestShareStepErrorTextStripsTerminalRendering(t *testing.T) {
	exit2 := fail.ExitUsageError
	got := stepErrorText(&fail.DetailedError{
		Summary:     "op model entity or container not found",
		Details:     "route not found",
		Suggestions: []string{"List the container's current entities"},
		ExitCode:    &exit2,
	})
	if got != "op model entity or container not found: route not found" {
		t.Errorf("step error text = %q", got)
	}
	if strings.ContainsAny(got, "│└├") {
		t.Errorf("terminal rendering leaked into the receipt: %q", got)
	}
	if plain := stepErrorText(errors.New("boom")); plain != "boom" {
		t.Errorf("plain error text = %q", plain)
	}
}

func TestPublishShareEntityFailureSkipsDependentEvidenceLinks(t *testing.T) {
	op := &fakeOpModel{failAt: 2}
	pkg := validBasePackage()

	receipt := publishShare(context.Background(), op, op, &pkg, "claude-code", "Claude Code")

	if receipt.Summary.Succeeded != 4 || receipt.Summary.Failed != 1 || receipt.Summary.Skipped != 2 {
		t.Errorf("summary wrong: %+v", receipt.Summary)
	}
	if _, minted := receipt.EntityIDsByRef["hyp1"]; minted {
		t.Error("failed entity must not report a minted id")
	}
	if len(receipt.EntityIDsByRef) != 3 {
		t.Errorf("survivors must be reported honestly: %v", receipt.EntityIDsByRef)
	}
	if len(receipt.Failures) != 1 || receipt.Failures[0].Step != `create hypothesis "hyp1"` {
		t.Errorf("failures wrong: %+v", receipt.Failures)
	}
	if len(receipt.Citations) != 0 {
		t.Errorf("unminted message_refs must not be cited: %v", receipt.Citations)
	}
}

func writeSharePackage(t *testing.T, pkg sharePackage) string {
	t.Helper()
	b, err := json.Marshal(pkg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join(t.TempDir(), "package.json")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestSharesPublishRefusesInvalidPackageBeforeIO(t *testing.T) {
	pkg := validBasePackage()
	pkg.Version = 1
	pkg.Message.Text = ""
	loader := &fakeOpModelLoader{op: &fakeOpModel{}}
	cmd, _, _ := newOpModelTestCmd()
	opts := &sharesPublishOpts{Filename: writeSharePackage(t, pkg), AgentID: "claude-code"}
	opts.IO.OutputFormat = "json"

	err := runSharesPublish(cmd, opts, loader, testSharedOpts())
	requireUsageExit(t, err)
	var derr *fail.DetailedError
	_ = errors.As(err, &derr)
	if !containsAll(derr.Details, "version must be 2", "message.text is required") {
		t.Errorf("violations not aggregated: %q", derr.Details)
	}
	if loader.loads != 0 {
		t.Error("invalid package must be refused before any client load")
	}
}

func TestSharesPublishRefusesInvalidActionLineageBeforeIO(t *testing.T) {
	pkg := validBasePackage()
	pkg.Entities[3].PromptedBy = "ghost"
	loader := &fakeOpModelLoader{op: &fakeOpModel{}}
	cmd, _, _ := newOpModelTestCmd()
	opts := &sharesPublishOpts{Filename: writeSharePackage(t, pkg), AgentID: "claude-code"}
	opts.IO.OutputFormat = "json"

	err := runSharesPublish(cmd, opts, loader, testSharedOpts())
	requireUsageExit(t, err)
	if loader.loads != 0 {
		t.Error("invalid action lineage must be refused before any client load")
	}
}

func TestSharesPublishRefusesUncurated(t *testing.T) {
	pkg := validBasePackage()
	pkg.Curated = new(false)
	loader := &fakeOpModelLoader{op: &fakeOpModel{}}
	cmd, _, _ := newOpModelTestCmd()
	opts := &sharesPublishOpts{Filename: writeSharePackage(t, pkg), AgentID: "claude-code"}
	opts.IO.OutputFormat = "json"

	err := runSharesPublish(cmd, opts, loader, testSharedOpts())
	requireUsageExit(t, err)
	if loader.loads != 0 {
		t.Error("uncurated package must be refused before any client load")
	}

	cmd2, stdout, _ := newOpModelTestCmd()
	opts2 := &sharesPublishOpts{Filename: writeSharePackage(t, pkg), AgentID: "claude-code", AllowUncurated: true}
	opts2.IO.OutputFormat = "json"
	if err := runSharesPublish(cmd2, opts2, loader, testSharedOpts()); err != nil {
		t.Fatalf("allow-uncurated publish: %v", err)
	}
	var receipt shareReceipt
	if err := json.Unmarshal(stdout.Bytes(), &receipt); err != nil {
		t.Fatalf("stdout not a receipt: %v", err)
	}
	if receipt.Curated {
		t.Error("receipt must report curated:false honestly")
	}
}

func TestSharesPublishPartialFailureEmitsReceiptAndExit4(t *testing.T) {
	pkg := validBasePackage()
	loader := &fakeOpModelLoader{op: &fakeOpModel{failAt: 2}}
	cmd, stdout, _ := newOpModelTestCmd()
	opts := &sharesPublishOpts{Filename: writeSharePackage(t, pkg), AgentID: "claude-code"}
	opts.IO.OutputFormat = "json"

	err := runSharesPublish(cmd, opts, loader, testSharedOpts())
	var emitted *fail.EmittedError
	if !errors.As(err, &emitted) {
		t.Fatalf("expected EmittedError sentinel, got %T: %v", err, err)
	}
	if emitted.Code != fail.ExitPartialFailure {
		t.Errorf("exit code = %d, want %d", emitted.Code, fail.ExitPartialFailure)
	}
	var receipt shareReceipt
	if err := json.Unmarshal(stdout.Bytes(), &receipt); err != nil {
		t.Fatalf("stdout must carry exactly the receipt: %v\nstdout=%q", err, stdout.String())
	}
	if receipt.Summary.Failed != 1 || receipt.Summary.Skipped != 2 {
		t.Errorf("receipt summary wrong: %+v", receipt.Summary)
	}
}

func TestSharesPublishSuccessEmitsReceiptAndHint(t *testing.T) {
	pkg := validBasePackage()
	loader := &fakeOpModelLoader{op: &fakeOpModel{}}
	cmd, stdout, stderr := newOpModelTestCmd()
	opts := &sharesPublishOpts{Filename: writeSharePackage(t, pkg), AgentID: "claude-code"}
	opts.IO.OutputFormat = "json"
	opts.IO.ErrWriter = stderr

	if err := runSharesPublish(cmd, opts, loader, testSharedOpts()); err != nil {
		t.Fatalf("run: %v", err)
	}
	var receipt shareReceipt
	if err := json.Unmarshal(stdout.Bytes(), &receipt); err != nil {
		t.Fatalf("stdout not a receipt: %v", err)
	}
	if receipt.Type != "gcx.irm.share_receipt" || receipt.Container != "INC-1" || !receipt.Curated {
		t.Errorf("receipt wrong: %+v", receipt)
	}
	if got := stderr.String(); !containsAll(got, "containers get INC-1") {
		t.Errorf("expected containers-get hint on stderr, got %q", got)
	}
}

func TestSharingRejectsCredentialsBeforeLoadingClient(t *testing.T) {
	for _, command := range []string{"shares publish", "incidents share"} {
		t.Run(command, func(t *testing.T) {
			authErr := errors.New("sharing requires an OAuth user login")
			loader := &fakeOpModelLoader{op: &fakeOpModel{}, sharingAuthErr: authErr}
			cmd, stdout, _ := newOpModelTestCmd()
			var err error
			if command == "shares publish" {
				opts := &sharesPublishOpts{Filename: writeSharePackage(t, validBasePackage()), AgentID: "codex"}
				err = runSharesPublish(cmd, opts, loader, testSharedOpts())
			} else {
				opts := &messageShareOpts{Body: "finding"}
				err = runIncidentShare(cmd, "INC-1", opts, loader, testSharedOpts())
			}
			if !errors.Is(err, authErr) || loader.loads != 0 || stdout.Len() != 0 {
				t.Fatalf("expected rejection before client loading, got err=%v loads=%d stdout=%s", err, loader.loads, stdout)
			}
		})
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
