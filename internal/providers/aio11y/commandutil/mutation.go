package commandutil

// Agent-output-contract helpers for Agent Observability mutation commands.
//
// These commands historically wrote nothing to stdout: confirmation prompts
// and per-target "Deleted ..." receipts go to stderr, and success meant an
// empty stdout with exit 0. The agent contract requires a finite command to
// emit exactly one JSON result document on stdout in agent mode, so the
// mutation verbs now build a cmdio result value and write it through the
// codec system. [SilentTextCodec] keeps the human default byte-identical
// (still nothing on stdout); the agents codec and explicit -o json/yaml get
// the structured document.

import (
	"errors"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
)

// SilentTextCodec is the human "text" codec for mutation results whose human
// stdout has always been empty — receipts and prompts live on stderr. It
// encodes any value as zero bytes so default human invocations stay
// byte-identical to the pre-contract behavior.
type SilentTextCodec struct{}

// Format returns the codec's format name.
func (SilentTextCodec) Format() format.Format { return "text" }

// Encode writes nothing: the human result of these mutations is the stderr
// receipt stream, not a stdout document.
func (SilentTextCodec) Encode(io.Writer, any) error { return nil }

// Decode is unsupported.
func (SilentTextCodec) Decode(io.Reader, any) error {
	return errors.New("text codec does not support decoding")
}

// RunBatchDelete executes the stop-on-first-error delete loop shared by the
// Agent Observability delete verbs and writes the BatchMutation result
// document through opts.
//
// Per-id semantics are unchanged from the pre-contract loop: each successful
// delete prints a receipt (successFormat, one %s verb for the id) on stderr,
// and the loop stops at the first failure (errorFormat, one %s verb for the
// id, wraps the cause). What changed is how outcomes reach stdout and the
// exit code:
//
//   - all succeeded: the BatchMutation document is encoded to stdout
//     (nothing, for the human text default) and the command exits 0;
//   - first id failed: nothing succeeded and nothing was written to stdout,
//     so the wrapped error returns raw — the standard error path renders the
//     single fused error document (agent mode) or stderr error (human);
//   - later id failed: a genuine partial failure. The complete document —
//     succeeded count, the failed target, unattempted ids as skipped — is
//     encoded to stdout, a warn diagnostic goes to stderr, and the returned
//     EmittedError carries ExitPartialFailure without a second document.
func RunBatchDelete(stdout, stderr io.Writer, opts *cmdio.Options, kind, successFormat, errorFormat string, ids []string, del func(id string) error) error {
	result := cmdio.NewBatchMutation("deleted")
	for i, id := range ids {
		err := del(id)
		if err == nil {
			result.Summary.Succeeded++
			cmdio.Success(stderr, successFormat, id)
			continue
		}

		wrapped := fmt.Errorf("%s: %w", fmt.Sprintf(errorFormat, id), err)
		if result.Summary.Succeeded == 0 {
			return wrapped
		}

		result.Summary.Failed = 1
		result.Summary.Skipped = len(ids) - i - 1
		result.Failures = append(result.Failures, cmdio.MutationFailure{
			Target: cmdio.MutationTarget{Kind: kind, ID: id},
			Error:  err.Error(),
		})
		if encErr := opts.Encode(stdout, result); encErr != nil {
			return encErr
		}
		cmdio.EmitWarn(stderr, wrapped.Error())
		return gcxerrors.NewEmittedError(gcxerrors.ExitPartialFailure, wrapped)
	}
	return opts.Encode(stdout, result)
}
