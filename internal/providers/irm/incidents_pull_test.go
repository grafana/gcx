package irm_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/irm"
	"github.com/grafana/gcx/internal/providers/irm/portable"
	"github.com/grafana/gcx/internal/resources"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIncidentDescriptionPullStripper(t *testing.T) {
	for _, tc := range []struct {
		name       string
		apiVersion string
		kind       string
		wantField  bool
	}{
		{name: "incident", apiVersion: irm.IncidentAPIVersion, kind: irm.IncidentKind},
		{name: "other resource", apiVersion: "example.com/v1", kind: "Example", wantField: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := resources.MustFromObject(map[string]any{
				"apiVersion": tc.apiVersion,
				"kind":       tc.kind,
				"metadata":   map[string]any{"name": "item"},
				"spec":       map[string]any{"description": "Visible details", "title": "Title"},
			}, resources.SourceInfo{})
			require.NoError(t, (portable.IncidentDescriptionStripper{}).Process(res))
			spec, ok := res.Object.Object["spec"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, tc.wantField, spec["description"] != nil)
			assert.Equal(t, "Title", spec["title"])
		})
	}
}
