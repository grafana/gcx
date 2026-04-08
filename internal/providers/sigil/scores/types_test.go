package scores_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/sigil/scores"
	"github.com/stretchr/testify/assert"
)

func TestScoreValue_Display(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	b := func(v bool) *bool { return &v }
	s := func(v string) *string { return &v }

	tests := []struct {
		name  string
		value scores.ScoreValue
		want  string
	}{
		{name: "number", value: scores.ScoreValue{Number: f(0.95)}, want: "0.95"},
		{name: "number integer", value: scores.ScoreValue{Number: f(1)}, want: "1"},
		{name: "bool true", value: scores.ScoreValue{Bool: b(true)}, want: "true"},
		{name: "bool false", value: scores.ScoreValue{Bool: b(false)}, want: "false"},
		{name: "string", value: scores.ScoreValue{String: s("good")}, want: "good"},
		{name: "empty", value: scores.ScoreValue{}, want: "-"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.value.Display())
		})
	}
}
