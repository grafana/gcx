package output

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// WaitBanner is the one-time start banner emitted to stderr before polling begins.
// In agent mode it is written as an NDJSON line; in non-agent mode
// it writes the human-readable "Waiting for …" message.
type WaitBanner struct {
	Event   string `json:"event"` // always "waiting_started"
	Target  Target `json:"target"`
	Timeout string `json:"timeout,omitempty"` // e.g. "5m0s"
}

// EmitTo writes the WaitBanner to w. agentMode true → NDJSON line; false → human text.
func (b WaitBanner) EmitTo(w io.Writer, agentMode bool) error {
	if agentMode {
		data, err := json.Marshal(b)
		if err != nil {
			return fmt.Errorf("WaitBanner.EmitTo: %w", err)
		}
		_, err = fmt.Fprintln(w, string(data))
		return err
	}
	if b.Target.Namespace != "" {
		_, err := fmt.Fprintf(w, "Waiting for namespace %q in cluster %q (timeout: %s)...\n",
			b.Target.Namespace, b.Target.Cluster, b.Timeout)
		return err
	}
	_, err := fmt.Fprintf(w, "Waiting for cluster %q to reach INSTRUMENTED status (timeout: %s)...\n",
		b.Target.Cluster, b.Timeout)
	return err
}

// WaitProgress is the per-poll progress event emitted to stderr during wait
// commands. In agent mode it is written as an NDJSON line; in
// non-agent mode it writes the existing human-readable progress text.
type WaitProgress struct {
	Event     string `json:"event"` // always "waiting"
	Target    Target `json:"target"`
	Status    string `json:"status"`
	ElapsedMs int64  `json:"elapsed_ms,omitempty"`
}

// EmitTo writes the WaitProgress to w. agentMode true → NDJSON line on stderr;
// false → human-readable progress text.
func (p WaitProgress) EmitTo(w io.Writer, agentMode bool) error {
	if agentMode {
		data, err := json.Marshal(p)
		if err != nil {
			return fmt.Errorf("WaitProgress.EmitTo: %w", err)
		}
		_, err = fmt.Fprintln(w, string(data))
		return err
	}
	if p.Target.Namespace != "" {
		_, err := fmt.Fprintf(w, "waiting: namespace %q status is %s...\n", p.Target.Namespace, p.Status)
		return err
	}
	_, err := fmt.Fprintf(w, "  status: %s\n", p.Status)
	return err
}

// WaitError carries error details inside the fused timeout envelope.
// Populated only when Outcome is "timeout" or "error".
type WaitError struct {
	Summary  string `json:"summary"`
	Details  string `json:"details"`
	ExitCode int    `json:"exitCode"`
}

// WaitResult is the agent-mode JSON envelope emitted by wait commands. The
// Error field is populated for fused timeout/error envelopes.
// In non-agent mode, a one-line human summary is written instead.
type WaitResult struct {
	Outcome   string     `json:"outcome"` // "success", "timeout", or "error"
	Target    Target     `json:"target"`
	Status    string     `json:"status,omitempty"` // last observed proto enum value
	ElapsedMs int64      `json:"elapsed_ms,omitempty"`
	Error     *WaitError `json:"error,omitempty"` // populated on timeout/error
}

// Emit writes the WaitResult to w. agentMode true → JSON; false → one-line summary.
func (r WaitResult) Emit(w io.Writer, agentMode bool) error {
	if agentMode {
		data, err := json.Marshal(r)
		if err != nil {
			return fmt.Errorf("WaitResult.Emit: %w", err)
		}
		_, err = fmt.Fprintln(w, string(data))
		return err
	}
	switch r.Outcome {
	case "success":
		if r.Target.Namespace != "" {
			_, err := fmt.Fprintf(w, "namespace %q in cluster %q: status is %s\n", r.Target.Namespace, r.Target.Cluster, r.Status)
			return err
		}
		_, err := fmt.Fprintf(w, "Cluster %q reached terminal state %s.\n", r.Target.Cluster, r.Status)
		return err
	case "timeout":
		if r.Target.Namespace != "" {
			_, err := fmt.Fprintf(w, "timeout: namespace %q in cluster %q still in %s\n", r.Target.Namespace, r.Target.Cluster, r.Status)
			return err
		}
		_, err := fmt.Fprintf(w, "timeout: %s still in %s\n", r.Target.Cluster, r.Status)
		return err
	default:
		_, err := fmt.Fprintf(w, "error at %s\n", r.Target.Cluster)
		return err
	}
}

// WaitResultForCluster builds a WaitResult for cluster-level waits (no namespace).
func WaitResultForCluster(outcome, clusterName, status string, start time.Time) WaitResult {
	return WaitResult{
		Outcome:   outcome,
		Target:    Target{Cluster: clusterName},
		Status:    status,
		ElapsedMs: time.Since(start).Milliseconds(),
	}
}
