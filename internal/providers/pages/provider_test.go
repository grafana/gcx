package pages_test

import (
	"testing"

	"github.com/grafana/gcx/internal/agent"
	pagesprovider "github.com/grafana/gcx/internal/providers/pages"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderRegistration(t *testing.T) {
	p := &pagesprovider.PagesProvider{}
	assert.Equal(t, "pages", p.Name())
	assert.NotEmpty(t, p.ShortDesc())
	require.NoError(t, p.Validate(nil))
	assert.Empty(t, p.ConfigKeys())
	assert.Empty(t, p.TypedRegistrations(), "pages are commands-only")

	cmds := p.Commands()
	require.Len(t, cmds, 1)
	root := cmds[0]
	assert.Equal(t, "pages", root.Use)
	assert.Equal(t, agent.StabilityExperimental, root.Annotations[agent.AnnotationStability])
	assert.NotNil(t, root.PersistentFlags().Lookup("config"), "--config is bound on the group")

	tree := map[string][]string{}
	for _, group := range root.Commands() {
		assert.Equal(t, agent.StabilityExperimental, group.Annotations[agent.AnnotationStability], group.Name())
		for _, sub := range group.Commands() {
			tree[group.Name()] = append(tree[group.Name()], sub.Name())
			assert.Equal(t, agent.StabilityExperimental, sub.Annotations[agent.AnnotationStability], group.Name()+" "+sub.Name())
		}
	}
	assert.Len(t, tree, 2)
	assert.ElementsMatch(t, []string{"list", "get"}, tree["catalog"], "published pages")
	assert.ElementsMatch(t, []string{"create", "pull", "push", "snapshot", "publish"}, tree["drafts"], "local projects")
}
