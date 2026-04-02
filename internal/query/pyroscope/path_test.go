package pyroscope_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/query/pyroscope"
)

func TestBuildPathsEscapeDatasourceUID(t *testing.T) {
	c := &pyroscope.Client{}
	uid := "uid/../admin"
	escapedUID := url.PathEscape(uid)

	tests := []struct {
		name string
		path string
	}{
		{"resourcePath", c.BuildResourcePath(uid, "querier.v1.QuerierService/ProfileTypes")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if strings.Contains(tt.path, uid) && !strings.Contains(tt.path, escapedUID) {
				t.Errorf("path contains unescaped UID: %s", tt.path)
			}
			if !strings.Contains(tt.path, escapedUID) {
				t.Errorf("path missing escaped UID %q: %s", escapedUID, tt.path)
			}
		})
	}
}
