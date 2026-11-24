package scores

import (
	"bytes"
	"strings"
	"testing"
	"time"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func TestScoreTableGolden(t *testing.T) {
	testutils.SetAgentMode(t, false)
	number, yes, no, label := 0.95, true, false, "positive"
	rows := []Score{
		{ScoreKey: "relevance", ScoreType: "number", Value: ScoreValue{Number: &number}, Passed: &yes, EvaluatorID: "eval-1", EvaluatorVersion: "v1", RuleID: "rule-1", AgentName: "checkout", GenModel: "model-1", Explanation: strings.Repeat("explanation ", 10), CreatedAt: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)},
		{ScoreKey: "harmful", ScoreType: "bool", Value: ScoreValue{Bool: &no}, Passed: &no, EvaluatorID: "eval-2"},
		{ScoreKey: "sentiment", ScoreType: "string", Value: ScoreValue{String: &label}},
		{ScoreKey: "unset"},
	}
	for _, kind := range []string{"generation", "rule"} {
		for _, name := range []string{"table", "wide"} {
			for _, tc := range []struct {
				name  string
				items []Score
			}{{"rows", rows}, {"empty", []Score{}}} {
				t.Run(kind+"/"+name+"/"+tc.name, func(t *testing.T) {
					flags := pflag.NewFlagSet("list-scores", pflag.ContinueOnError)
					var opts *cmdio.Options
					var payload any = tc.items
					if kind == "generation" {
						o := &listOpts{}
						o.setup(flags)
						opts = &o.IO
					} else {
						o := &listRuleOpts{}
						o.setup(flags)
						opts = &o.IO
						payload = ruleScoresEnvelope{Items: tc.items}
					}
					require.NoError(t, flags.Set("output", name))
					require.NoError(t, opts.Validate())
					var buf bytes.Buffer
					require.NoError(t, opts.Encode(&buf, payload))
					testutils.Golden(t, kind+"_"+name+"_"+tc.name, buf.String())
				})
			}
		}
	}
}
