package suggest_test

import (
	"testing"

	"github.com/grafana/gcx/internal/suggest"
	"github.com/stretchr/testify/assert"
)

func TestCandidates(t *testing.T) {
	commands := []string{"resources", "datasources", "dashboards", "config", "login", "slo", "irm"}

	tests := []struct {
		name       string
		input      string
		vocabulary []string
		want       []string
	}{
		{
			name:       "single character typo",
			input:      "confg",
			vocabulary: commands,
			want:       []string{"config"},
		},
		{
			name:       "transposition",
			input:      "dashbaords",
			vocabulary: commands,
			want:       []string{"dashboards"},
		},
		{
			name:       "prefix match beyond distance threshold",
			input:      "data",
			vocabulary: commands,
			want:       []string{"datasources"},
		},
		{
			name:       "case insensitive",
			input:      "SLO",
			vocabulary: commands,
			want:       []string{"slo"},
		},
		{
			name:       "no match for unrelated input",
			input:      "kubectl",
			vocabulary: commands,
			want:       nil,
		},
		{
			name:       "empty input",
			input:      "",
			vocabulary: commands,
			want:       nil,
		},
		{
			name:       "closest match ranks first",
			input:      "gets",
			vocabulary: []string{"list", "get", "gett"},
			want:       []string{"get", "gett"},
		},
		{
			name:       "ties preserve vocabulary order",
			input:      "ab",
			vocabulary: []string{"ax", "ay", "az", "aw"},
			want:       []string{"ax", "ay", "az"},
		},
		{
			name:       "duplicates removed",
			input:      "get",
			vocabulary: []string{"get", "get", "gett"},
			want:       []string{"get", "gett"},
		},
		{
			name:       "longer input allows larger distance",
			input:      "dashboardses",
			vocabulary: commands,
			want:       []string{"dashboards"},
		},
		{
			name:       "flag names",
			input:      "--formt",
			vocabulary: []string{"--format", "--output", "--context"},
			want:       []string{"--format"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, suggest.Candidates(tt.input, tt.vocabulary))
		})
	}
}
