package loki

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/grafana/mcp-grafana/lokiguard"
)

// guardScopeHint is the gcx-specific discovery advice lokiguard.Guidance
// appends to its rewrite instructions.
const guardScopeHint = "use 'gcx logs labels' to find specific label matchers to scope the query first"

// queryGuard is the pre-execution Loki cost guardrail for Grafana Cloud
// targets. The decision logic lives in lokiguard; this type fetches the tenant
// policy, supplies index/stats, and turns a verdict into command behavior.
// The only per-query override is --force (see guardQuery); the tenant policy
// sets the budget.
type queryGuard struct {
	policy  policySource
	client  *loki.Client
	stderr  io.Writer
	agent   bool
	timeout time.Duration
}

// guardStatsTimeout bounds the whole index-stats estimate (all selectors).
const guardStatsTimeout = 5 * time.Second

// newQueryGuard returns nil when the guard does not apply to the target
// (self-hosted Grafana is out of scope by design).
func newQueryGuard(cfgCtx *config.Context, cfg config.NamespacedRESTConfig, client *loki.Client, stderr io.Writer) *queryGuard {
	src, ok := resolveGuardTarget(cfgCtx, cfg)
	if !ok {
		return nil
	}
	return &queryGuard{
		policy:  src,
		client:  client,
		stderr:  stderr,
		agent:   agent.IsAgentMode(),
		timeout: guardStatsTimeout,
	}
}

// check assesses the query and returns a non-nil error when it must not run.
// Warnings go to stderr only: stdout belongs to the command's result.
func (g *queryGuard) check(ctx context.Context, datasourceUID, expr string, isRange bool, start, end, now time.Time) error {
	log := logging.FromContext(ctx)

	policy, fallbackReason := g.policy.fetch(ctx, now)
	if fallbackReason != "" {
		log.Warn("Loki query policy unavailable, using the built-in default (shadow, 50 GiB, 24h)", slog.String("reason", fallbackReason))
	}
	if policy.Mode == modeOff {
		log.Info("Loki query guardrail is off for this tenant", slog.String("policy_source", policy.Source))
		return nil
	}

	stats, cancel := g.statsFunc(ctx, datasourceUID)
	defer cancel()

	// An instant query has no window of its own; it runs over the same
	// now-1m..now the request builder sends.
	if !isRange {
		start, end = now.Add(-time.Minute), now
	}
	d := lokiguard.Assess(ctx, lokiguard.Query{LogQL: expr, Start: start, End: end}, policy.guardPolicy(), stats)

	reasonKinds := make([]string, len(d.Reasons))
	for i, r := range d.Reasons {
		reasonKinds[i] = string(r.Kind)
	}
	log.Info("Loki query guardrail decision",
		slog.String("source", "gcx"),
		slog.String("verdict", d.Verdict.String()),
		slog.Int64("estimated_bytes", d.EstimatedBytes),
		slog.String("reasons", strings.Join(reasonKinds, ",")),
		slog.String("unknown_cause", string(d.Cause)),
		slog.String("mode", string(policy.Mode)),
		slog.String("policy_source", policy.Source),
	)

	enforce := policy.Mode == modeEnforce
	switch d.Verdict {
	case lokiguard.Allow:
		return nil
	case lokiguard.Block:
		if !enforce {
			log.Warn("shadow mode: this query would have been blocked", slog.String("guidance", lokiguard.Guidance(d, policy.guardPolicy(), guardScopeHint)))
			return nil
		}
		return blockedError(d, policy, g.agent)
	case lokiguard.Unknown:
		// A query with no parseable selector is Loki's to reject: its own syntax
		// error tells the caller more than "could not be assessed".
		if d.Cause == lokiguard.CauseUnparseable {
			log.Debug("Loki query guardrail could not parse a stream selector; leaving the query for Loki to validate")
			return nil
		}
		if g.agent && enforce {
			return unassessableError(d)
		}
		if enforce {
			// Interactive callers run the query and are told the cost is unknown.
			cmdio.Warning(g.stderr, "could not estimate the cost of this query (%s); running it anyway", unknownExplanation(d))
		} else {
			log.Warn("shadow mode: this query could not be assessed", slog.String("cause", string(d.Cause)))
		}
		return nil
	}
	return nil
}

// statsFunc adapts index/stats to lokiguard. The returned cancel releases the
// deadline that bounds the whole assessment. The per-call context lokiguard
// passes is deliberately ignored: all selectors share this one deadline.
func (g *queryGuard) statsFunc(ctx context.Context, datasourceUID string) (lokiguard.StatsFunc, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	return func(_ context.Context, selector string, start, end time.Time) (int64, error) {
		resp, err := g.client.IndexStats(ctx, datasourceUID, selector, start, end)
		if err != nil {
			return 0, err
		}
		return int64(resp.Bytes), nil //nolint:gosec // a single selector cannot match over 8 EiB
	}, cancel
}

// blockedError names --force only to interactive callers: an agent is
// expected to narrow the query from the guidance, not to reach for the override.
func blockedError(d lokiguard.Decision, p queryPolicy, agentMode bool) error {
	reasons := make([]string, len(d.Reasons))
	for i, r := range d.Reasons {
		reasons[i] = r.Message
	}
	details := strings.Join(reasons, "; ")
	if d.EstimatedBytes > 0 {
		details = fmt.Sprintf("estimated scan %s (budget %s): %s", humanize.IBytes(uint64(d.EstimatedBytes)), budgetText(p), details)
	}

	// The first sentence of the guidance restates the reasons already in Details.
	advice := splitGuidance(lokiguard.Guidance(d, p.guardPolicy(), guardScopeHint))
	if len(advice) > 1 {
		advice = advice[1:]
	}
	suggestions := make([]string, 0, len(advice)+2)
	suggestions = append(suggestions, "Scope the selector first: use gcx logs labels to find specific label matchers")
	suggestions = append(suggestions, advice...)
	if agentMode {
		suggestions = append(suggestions, "A tenant admin sets the budget in the Grafana Assistant settings")
	} else {
		suggestions = append(suggestions, "To run it anyway, pass --force; a tenant admin sets the budget in the Grafana Assistant settings")
	}

	return gcxerrors.DetailedError{
		Summary:     gcxerrors.SummaryQueryBlocked,
		Details:     "The Loki cost guardrail refused this query: " + details,
		Suggestions: suggestions,
	}
}

func unassessableError(d lokiguard.Decision) error {
	suggestions := []string{
		"Retry the query; the index/stats estimate may have failed transiently",
		"Check that the datasource is healthy: gcx datasources get <uid>",
		"A persistent failure means index/stats is unavailable on this datasource, so agent-mode queries against it stay blocked unless you pass --force",
	}
	return gcxerrors.DetailedError{
		Summary:     gcxerrors.SummaryQueryBlocked,
		Details:     "The Loki cost guardrail could not assess this query in agent mode: " + unknownExplanation(d),
		Suggestions: suggestions,
	}
}

func unknownExplanation(d lokiguard.Decision) string {
	if d.Cause == lokiguard.CauseEstimateFailed {
		if d.Err != nil {
			return "the index/stats estimate failed: " + d.Err.Error()
		}
		return "the index/stats estimate failed"
	}
	return "unknown cause"
}

func budgetText(p queryPolicy) string {
	if p.MaxBytes <= 0 {
		return "unlimited"
	}
	return humanize.IBytes(uint64(p.MaxBytes))
}

// splitGuidance breaks lokiguard.Guidance into one suggestion per sentence,
// without splitting on the "e.g." abbreviations it contains.
func splitGuidance(guidance string) []string {
	var out []string
	rest := guidance
	for {
		i := strings.Index(rest, ". ")
		for i >= 0 && strings.HasSuffix(rest[:i], "e.g") {
			j := strings.Index(rest[i+2:], ". ")
			if j < 0 {
				i = -1
				break
			}
			i += 2 + j
		}
		if i < 0 {
			break
		}
		out = append(out, strings.TrimSpace(rest[:i]))
		rest = rest[i+2:]
	}
	if s := strings.TrimSuffix(strings.TrimSpace(rest), "."); s != "" {
		out = append(out, s)
	}
	return out
}

// guardQuery runs the Cloud guardrail when it applies to the target. It is a
// no-op for self-hosted targets, which are not guarded by design.
//
// force is the --force flag: a forced query runs without a policy fetch or a
// stats probe. GCX_AUTO_APPROVE deliberately does not bypass the guard: it
// approves destructive operations, and an agent should correct the query from
// the guidance instead. See docs/design/safety.md § 3.6.
func guardQuery(ctx context.Context, cfgCtx *config.Context, cfg config.NamespacedRESTConfig, client *loki.Client, stderr io.Writer, force bool, datasourceUID, expr string, isRange bool, start, end, now time.Time) error {
	g := newQueryGuard(cfgCtx, cfg, client, stderr)
	if g == nil {
		return nil
	}
	if force {
		logging.FromContext(ctx).Info("Loki query guardrail bypassed by --force")
		return nil
	}
	return g.check(ctx, datasourceUID, expr, isRange, start, end, now)
}
