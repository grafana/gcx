package alert //nolint:testpackage // Tests use private command factories with server responses.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTemplateGroupsUnknownServerKind(t *testing.T) {
	templateTestMode(t)
	for _, version := range []string{"v0alpha1", "v1beta1"} {
		for _, tc := range []struct {
			name      string
			args      []string
			errorText string
			wantKind  string
		}{
			{name: "table lists unknown", args: []string{"list"}, wantKind: "future"},
			{name: "wide lists unknown", args: []string{"list", "-o", "wide"}, wantKind: "future"},
			{name: "json preserves unknown", args: []string{"list", "-o", "json"}, wantKind: "future"},
			{name: "grafana filter skips unknown", args: []string{"get", "shared", "--kind", "grafana", "-o", "json"}, wantKind: "grafana"},
			{name: "mimir filter skips unknown", args: []string{"get", "shared", "--kind", "mimir", "-o", "json"}, wantKind: "mimir"},
			{name: "unfiltered ambiguity includes raw kind", args: []string{"get", "shared"}, errorText: "future/opaque-future"},
			{name: "unfiltered unknown candidate remains readable", args: []string{"get", "future-only", "-o", "json"}, wantKind: "future"},
			{name: "known filter leaves unknown candidate unmatched", args: []string{"get", "future-only", "--kind", "grafana"}, errorText: "not found"},
		} {
			t.Run(version+"/"+tc.name, func(t *testing.T) {
				api := newTemplateNativeAPI(t, version)
				for _, f := range []struct{ name, title string }{{"opaque-future", "shared"}, {"only-future", "future-only"}} {
					var obj map[string]any
					require.NoError(t, json.Unmarshal([]byte(templateManifest(version, f.name, f.title, "future", "7")), &obj))
					api.items[f.name] = obj
				}
				result := runTemplateGroups(t, templateAPIBinding(t, api), "", tc.args...)
				if tc.errorText != "" {
					require.ErrorContains(t, result.err, tc.errorText)
					if tc.name == "unfiltered ambiguity includes raw kind" {
						assert.Contains(t, result.err.Error(), "grafana/opaque-grafana")
						assert.Contains(t, result.err.Error(), "mimir/opaque-mimir")
					}
					return
				}
				require.NoError(t, result.err)
				assert.Contains(t, result.stdout, tc.wantKind)
				if tc.name == "table lists unknown" || tc.name == "wide lists unknown" {
					for line := range strings.SplitSeq(result.stdout, "\n") {
						if strings.Contains(line, "opaque-future") {
							assert.Regexp(t, `future\s+unknown`, line)
						}
					}
				}
				if strings.Contains(tc.name, "filter skips") {
					assert.NotContains(t, result.stdout, "opaque-future")
				}
				wantRequests := 1
				if tc.args[0] == "get" {
					wantRequests = 2
				}
				assert.Len(t, api.requests, wantRequests)
				for _, request := range api.requests {
					assert.True(t, strings.HasPrefix(request, "GET "), "read command issued mutation: %s", request)
				}
			})
		}
	}
}
