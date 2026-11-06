//nolint:testpackage // white-box tests require access to unexported IRM command builders
package irm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	fail "github.com/grafana/gcx/internal/gcxerrors"
	"github.com/spf13/cobra"
)

// fakeOpModel is a recording OpModelAPI fake: it captures every request in
// order, mints deterministic ids, and can be scripted to fail at an exact
// 1-based call index (failAt) or on every call (err).
type fakeOpModel struct {
	reqs            []any
	calls           int
	failAt          int
	err             error
	messageResponse *createMessageResponse
}

func (f *fakeOpModel) step(req any) error {
	f.calls++
	f.reqs = append(f.reqs, req)
	if f.err != nil {
		return f.err
	}
	if f.failAt != 0 && f.calls == f.failAt {
		return fmt.Errorf("scripted failure at call %d", f.calls)
	}
	return nil
}

// rev mints an opaque revision string, as the API does.
func (f *fakeOpModel) rev() string { return strconv.Itoa(100 + f.calls) }

// reqAs asserts the i-th recorded request has type T and returns it.
func reqAs[T any](t *testing.T, reqs []any, i int) T {
	t.Helper()
	v, ok := reqs[i].(T)
	if !ok {
		t.Fatalf("request %d has type %T", i, reqs[i])
	}
	return v
}

func (f *fakeOpModel) CreateObservation(_ context.Context, req createObservationRequest) (*createObservationResponse, error) {
	if err := f.step(req); err != nil {
		return nil, err
	}
	return &createObservationResponse{ObservationID: fmt.Sprintf("obs-%d", f.calls), Revision: f.rev()}, nil
}

func (f *fakeOpModel) CreateClaim(_ context.Context, req createClaimRequest) (*createClaimResponse, error) {
	if err := f.step(req); err != nil {
		return nil, err
	}
	return &createClaimResponse{ClaimID: fmt.Sprintf("clm-%d", f.calls), Revision: f.rev()}, nil
}

func (f *fakeOpModel) CreateAction(_ context.Context, req createActionRequest) (*createActionResponse, error) {
	if err := f.step(req); err != nil {
		return nil, err
	}
	return &createActionResponse{ActionID: fmt.Sprintf("act-%d", f.calls), Status: "proposed", Revision: f.rev()}, nil
}

func (f *fakeOpModel) Promote(_ context.Context, req promoteRequest) (*promoteResponse, error) {
	if err := f.step(req); err != nil {
		return nil, err
	}
	return &promoteResponse{ClaimID: "clm-new", Revision: f.rev()}, nil
}

func (f *fakeOpModel) PromoteToAction(_ context.Context, req promoteToActionRequest) (*promoteToActionResponse, error) {
	if err := f.step(req); err != nil {
		return nil, err
	}
	return &promoteToActionResponse{ActionID: "act-new", Status: "proposed", Revision: f.rev()}, nil
}

func (f *fakeOpModel) EditText(_ context.Context, req editTextRequest) (*revisionResponse, error) {
	if err := f.step(req); err != nil {
		return nil, err
	}
	return &revisionResponse{Revision: f.rev()}, nil
}

func (f *fakeOpModel) AssessClaim(_ context.Context, req assessClaimRequest) (*revisionResponse, error) {
	if err := f.step(req); err != nil {
		return nil, err
	}
	return &revisionResponse{Revision: f.rev()}, nil
}

func (f *fakeOpModel) CoordinateAction(_ context.Context, req coordinateActionRequest) (*revisionResponse, error) {
	if err := f.step(req); err != nil {
		return nil, err
	}
	return &revisionResponse{Revision: f.rev()}, nil
}

func (f *fakeOpModel) VerifyObservation(_ context.Context, req verifyObservationRequest) (*revisionResponse, error) {
	if err := f.step(req); err != nil {
		return nil, err
	}
	return &revisionResponse{Revision: f.rev()}, nil
}

func (f *fakeOpModel) AttachEvidence(_ context.Context, req attachEvidenceRequest) (*attachEvidenceResponse, error) {
	if err := f.step(req); err != nil {
		return nil, err
	}
	return &attachEvidenceResponse{EvidenceID: fmt.Sprintf("brg-%d", f.calls), Revision: f.rev()}, nil
}

func (f *fakeOpModel) SetEvidenceVerification(_ context.Context, req setEvidenceVerificationRequest) (*revisionResponse, error) {
	if err := f.step(req); err != nil {
		return nil, err
	}
	return &revisionResponse{Revision: f.rev()}, nil
}

func (f *fakeOpModel) WithdrawEvidence(_ context.Context, req withdrawEvidenceRequest) (*revisionResponse, error) {
	if err := f.step(req); err != nil {
		return nil, err
	}
	return &revisionResponse{Revision: f.rev()}, nil
}

func (f *fakeOpModel) CreateMessage(_ context.Context, req createMessageRequest) (*createMessageResponse, error) {
	if err := f.step(req); err != nil {
		return nil, err
	}
	if f.messageResponse != nil {
		return f.messageResponse, nil
	}
	return &createMessageResponse{MessageID: fmt.Sprintf("upd-%d", f.calls)}, nil
}

func (f *fakeOpModel) GetContainer(_ context.Context, req getContainerRequest) (*containerBody, error) {
	if err := f.step(req); err != nil {
		return nil, err
	}
	return &containerBody{Container: req.Container, Revision: "42"}, nil
}

func (f *fakeOpModel) ListEvents(_ context.Context, req listEventsRequest) (*eventsBody, error) {
	if err := f.step(req); err != nil {
		return nil, err
	}
	return &eventsBody{
		Events:   []opModelEventDTO{{Revision: "5", ID: "ev-5", Type: "observation.created", Payload: []byte(`{"text":"x"}`)}},
		Revision: "9",
	}, nil
}

// fakeOpModelLoader implements OpModelClientLoader over the fake and records
// whether/with what it was invoked, so validation-before-I/O assertions can
// prove no client was ever loaded.
type fakeOpModelLoader struct {
	op             *fakeOpModel
	err            error
	loads          int
	sharingAuthErr error

	gotTimeout time.Duration
}

func (l *fakeOpModelLoader) RequireSharingOAuth(context.Context) error {
	return l.sharingAuthErr
}

func (l *fakeOpModelLoader) LoadOpModel(_ context.Context, timeout time.Duration) (OpModelAPI, error) {
	l.loads++
	l.gotTimeout = timeout
	if l.err != nil {
		return nil, l.err
	}
	return l.op, nil
}

func (l *fakeOpModelLoader) LoadMessages(_ context.Context, timeout time.Duration) (MessageAPI, error) {
	l.loads++
	l.gotTimeout = timeout
	if l.err != nil {
		return nil, l.err
	}
	return l.op, nil
}

// newOpModelTestCmd builds a bare command with captured streams for driving
// run functions directly (bypassing cobra parsing, mirroring runAck).
func newOpModelTestCmd() (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetContext(context.Background())
	return cmd, stdout, stderr
}

func testSharedOpts() *opModelSharedOpts {
	return &opModelSharedOpts{Timeout: defaultOpModelTimeout}
}

func testActor() opModelActorOpts {
	return opModelActorOpts{Kind: "agent", ID: "claude-code"}
}

// assertDetailedExit asserts err is a DetailedError carrying the exit code.
func assertDetailedExit(t *testing.T, err error, want int) {
	t.Helper()
	var derr *fail.DetailedError
	if !errors.As(err, &derr) {
		t.Fatalf("expected *DetailedError, got %T: %v", err, err)
	}
	got := fail.ExitGeneralError
	if derr.ExitCode != nil {
		got = *derr.ExitCode
	}
	if got != want {
		t.Errorf("exit code = %d, want %d (summary %q)", got, want, derr.Summary)
	}
}

// requireUsageExit asserts err is a DetailedError carrying the usage exit
// code (2); the contract for every statically-decidable rejection.
func requireUsageExit(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertDetailedExit(t, err, fail.ExitUsageError)
}

// ---------------------------------------------------------------------------
// Reads: envelope shape, empty arrays, pagination hints.
// ---------------------------------------------------------------------------

func TestContainersGetEmitsEnvelopeWithEmptyArrays(t *testing.T) {
	loader := &fakeOpModelLoader{op: &fakeOpModel{}}
	cmd, stdout, _ := newOpModelTestCmd()
	opts := &containersGetOpts{}
	opts.IO.OutputFormat = "json"

	if err := runContainersGet(cmd, "INC-1", opts, loader, testSharedOpts()); err != nil {
		t.Fatalf("run: %v", err)
	}
	raw := stdout.String()
	var doc map[string]any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("stdout not JSON: %v", err)
	}
	if doc["type"] != "gcx.irm.opmodel.container" || doc["container"] != "INC-1" || doc["revision"] != "42" {
		t.Errorf("envelope wrong: %v", doc)
	}
	for _, key := range []string{"observations", "claims", "evidence_links", "actions"} {
		arr, ok := doc[key].([]any)
		if !ok {
			t.Errorf("%s must be [] (never null); got %T", key, doc[key])
			continue
		}
		if len(arr) != 0 {
			t.Errorf("%s expected empty, got %v", key, arr)
		}
	}
}

func TestContainersListEventsMapsRequestAndHints(t *testing.T) {
	op := &fakeOpModel{}
	loader := &fakeOpModelLoader{op: op}
	cmd, stdout, stderr := newOpModelTestCmd()
	opts := &containersListEventsOpts{SinceRevision: "4", PageSize: 1}
	opts.IO.OutputFormat = "json"
	opts.IO.ErrWriter = stderr

	if err := runContainersListEvents(cmd, "INC-1", opts, loader, testSharedOpts()); err != nil {
		t.Fatalf("run: %v", err)
	}
	req := reqAs[listEventsRequest](t, op.reqs, 0)
	if req.Container != "INC-1" || req.SinceRevision != "4" || req.PageSize != 1 {
		t.Errorf("request fields not mapped: %+v", req)
	}
	var doc opModelEventsEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("stdout not JSON: %v", err)
	}
	// The fake's last event revision (5) sits below head (9): a hint fires.
	if doc.Revision != "9" || len(doc.Events) != 1 {
		t.Errorf("envelope wrong: %+v", doc)
	}
	if !strings.Contains(stderr.String(), "--since-revision 5") {
		t.Errorf("expected catch-up hint on stderr, got %q", stderr.String())
	}
}

// ---------------------------------------------------------------------------
// Backend errors surface unchanged (already DetailedError-mapped by the
// client layer; the command must not double-wrap or swallow them).
// ---------------------------------------------------------------------------

func TestMutationPropagatesClientError(t *testing.T) {
	wantErr := errors.New("boom")
	loader := &fakeOpModelLoader{op: &fakeOpModel{err: wantErr}}
	cmd, stdout, _ := newOpModelTestCmd()
	opts := &actionsCreateOpts{Container: "INC-1", Text: "t", Actor: testActor()}
	opts.IO.OutputFormat = "json"

	err := runActionsCreate(cmd, opts, loader, testSharedOpts())
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped client error, got %v", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("no result document may be emitted on failure; got %q", stdout.String())
	}
}

func (f *fakeOpModel) EditObservation(_ context.Context, req editObservationRequest) (*revisionResponse, error) {
	if err := f.step(req); err != nil {
		return nil, err
	}
	return &revisionResponse{Revision: f.rev()}, nil
}

func (f *fakeOpModel) EditAction(_ context.Context, req editActionRequest) (*revisionResponse, error) {
	if err := f.step(req); err != nil {
		return nil, err
	}
	return &revisionResponse{Revision: f.rev()}, nil
}

func (f *fakeOpModel) UpdateRecovery(_ context.Context, req updateRecoveryRequest) (*revisionResponse, error) {
	if err := f.step(req); err != nil {
		return nil, err
	}
	return &revisionResponse{Revision: f.rev()}, nil
}
