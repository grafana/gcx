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

type fakeOpMem struct {
	reqs            []any
	calls           int
	failAt          int
	err             error
	messageResponse *createMessageResponse
}

func (f *fakeOpMem) step(req any) error {
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

func (f *fakeOpMem) rev() string { return strconv.Itoa(100 + f.calls) }

func reqAs[T any](t *testing.T, reqs []any, i int) T {
	t.Helper()
	v, ok := reqs[i].(T)
	if !ok {
		t.Fatalf("request %d has type %T", i, reqs[i])
	}
	return v
}

func (f *fakeOpMem) CreateObservation(_ context.Context, req createObservationRequest) (*createObservationResponse, error) {
	if err := f.step(req); err != nil {
		return nil, err
	}
	return &createObservationResponse{ObservationID: fmt.Sprintf("obs-%d", f.calls), Revision: f.rev()}, nil
}

func (f *fakeOpMem) CreateClaim(_ context.Context, req createClaimRequest) (*createClaimResponse, error) {
	if err := f.step(req); err != nil {
		return nil, err
	}
	return &createClaimResponse{ClaimID: fmt.Sprintf("clm-%d", f.calls), Revision: f.rev()}, nil
}

func (f *fakeOpMem) CreateAction(_ context.Context, req createActionRequest) (*createActionResponse, error) {
	if err := f.step(req); err != nil {
		return nil, err
	}
	return &createActionResponse{ActionID: fmt.Sprintf("act-%d", f.calls), Status: "proposed", Revision: f.rev()}, nil
}

func (f *fakeOpMem) PromoteToAction(_ context.Context, req promoteToActionRequest) (*promoteToActionResponse, error) {
	if err := f.step(req); err != nil {
		return nil, err
	}
	return &promoteToActionResponse{ActionID: "act-new", Status: "proposed", Revision: f.rev()}, nil
}

func (f *fakeOpMem) AttachEvidence(_ context.Context, req attachEvidenceRequest) (*attachEvidenceResponse, error) {
	if err := f.step(req); err != nil {
		return nil, err
	}
	return &attachEvidenceResponse{EvidenceID: fmt.Sprintf("brg-%d", f.calls), Revision: f.rev()}, nil
}

func (f *fakeOpMem) CreateMessage(_ context.Context, req createMessageRequest) (*createMessageResponse, error) {
	if err := f.step(req); err != nil {
		return nil, err
	}
	if f.messageResponse != nil {
		return f.messageResponse, nil
	}
	return &createMessageResponse{MessageID: fmt.Sprintf("upd-%d", f.calls)}, nil
}

func (f *fakeOpMem) GetContainer(_ context.Context, req getContainerRequest) (*containerBody, error) {
	if err := f.step(req); err != nil {
		return nil, err
	}
	return &containerBody{Container: req.Container, Revision: "42"}, nil
}

func (f *fakeOpMem) ListEvents(_ context.Context, req listEventsRequest) (*eventsBody, error) {
	if err := f.step(req); err != nil {
		return nil, err
	}
	return &eventsBody{
		Events:   []opMemEventDTO{{Revision: "5", ID: "ev-5", Type: "observation.created", Payload: []byte(`{"text":"x"}`)}},
		Revision: "9",
	}, nil
}

type fakeOpMemLoader struct {
	op             *fakeOpMem
	err            error
	loads          int
	sharingAuthErr error
	destination    messageDestination

	gotTimeout time.Duration
}

func (l *fakeOpMemLoader) RequireSharingOAuth(context.Context) (messageDestination, error) {
	return l.destination, l.sharingAuthErr
}

func (l *fakeOpMemLoader) LoadOpMem(_ context.Context, timeout time.Duration) (OpMemAPI, error) {
	l.loads++
	l.gotTimeout = timeout
	if l.err != nil {
		return nil, l.err
	}
	return l.op, nil
}

func (l *fakeOpMemLoader) LoadMessages(_ context.Context, timeout time.Duration) (MessageAPI, error) {
	l.loads++
	l.gotTimeout = timeout
	if l.err != nil {
		return nil, l.err
	}
	return l.op, nil
}

func newOpMemTestCmd() (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetContext(context.Background())
	return cmd, stdout, stderr
}

func testSharedOpts() *opMemSharedOpts {
	return &opMemSharedOpts{Timeout: defaultOpMemTimeout}
}

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

func requireUsageExit(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertDetailedExit(t, err, fail.ExitUsageError)
}

func TestContainersGetEmitsEnvelopeWithEmptyArrays(t *testing.T) {
	loader := &fakeOpMemLoader{op: &fakeOpMem{}}
	cmd, stdout, _ := newOpMemTestCmd()
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
	if doc["type"] != "gcx.irm.opmem.container" || doc["container"] != "INC-1" || doc["revision"] != "42" {
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
	op := &fakeOpMem{}
	loader := &fakeOpMemLoader{op: op}
	cmd, stdout, stderr := newOpMemTestCmd()
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
	var doc opMemEventsEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("stdout not JSON: %v", err)
	}
	if doc.Revision != "9" || len(doc.Events) != 1 {
		t.Errorf("envelope wrong: %+v", doc)
	}
	if !strings.Contains(stderr.String(), "--since-revision 5") {
		t.Errorf("expected catch-up hint on stderr, got %q", stderr.String())
	}
}

func TestContainersGetPropagatesClientError(t *testing.T) {
	wantErr := errors.New("boom")
	loader := &fakeOpMemLoader{op: &fakeOpMem{err: wantErr}}
	cmd, stdout, _ := newOpMemTestCmd()
	opts := &containersGetOpts{}
	opts.IO.OutputFormat = "json"

	err := runContainersGet(cmd, "INC-1", opts, loader, testSharedOpts())
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped client error, got %v", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("no result document may be emitted on failure; got %q", stdout.String())
	}
}

func TestContainersListEventsRejectsExplicitEmptySinceRevision(t *testing.T) {
	op := &fakeOpMem{}
	cmd := newContainersListEventsCmd(&fakeOpMemLoader{op: op}, testSharedOpts())
	cmd.SetArgs([]string{"INC-1", "--since-revision", ""})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	var detailed *fail.DetailedError
	if !errors.As(err, &detailed) || detailed.ExitCode == nil || *detailed.ExitCode != fail.ExitUsageError {
		t.Fatalf("want usage error, got %v", err)
	}
	if len(op.reqs) != 0 {
		t.Errorf("expected no request, got %d", len(op.reqs))
	}
}
