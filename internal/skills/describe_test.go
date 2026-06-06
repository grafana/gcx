package skills_test

import (
	"testing"
	"testing/fstest"

	"github.com/grafana/gcx/internal/skills"
	"github.com/stretchr/testify/require"
)

func TestShortDescriptionFromBytes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		data string
		want string
	}{
		{
			name: "folded front matter description",
			data: "---\nname: sample\ndescription: >\n  Use this skill to do an example operation\n  in a concise way.\n---\n\n# Sample\n",
			want: "Use this skill to do an example operation in a concise way.",
		},
		{
			name: "plain front matter description",
			data: "---\nname: alpha\ndescription: alpha skill description\n---\n",
			want: "alpha skill description",
		},
		{
			name: "front matter present but no description key yields empty",
			data: "---\nname: beta\n---\n\n# Heading\n\nFirst real line here.\n",
			want: "",
		},
		{
			name: "no front matter falls back to first body line",
			data: "# Title\n\nBody sentence.\n",
			want: "Body sentence.",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, skills.ShortDescriptionFromBytes([]byte(tc.data)))
		})
	}
}

func TestShortDescription_ReadsFromFS(t *testing.T) {
	t.Parallel()

	source := fstest.MapFS{
		"alpha/SKILL.md": {Data: []byte("---\nname: alpha\ndescription: alpha skill description\n---\n")},
	}

	require.Equal(t, "alpha skill description", skills.ShortDescription(source, "alpha"))
	require.Empty(t, skills.ShortDescription(source, "missing"))
}
