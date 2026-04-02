package loki_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/query/loki"
)

func TestBuildPathsEscapeDatasourceUID(t *testing.T) {
	c := &loki.Client{}
	uid := "uid/../admin"
	escapedUID := url.PathEscape(uid)

	tests := []struct {
		name string
		path string
	}{
		{"labels", c.BuildLabelsPath(uid)},
		{"labelValues", c.BuildLabelValuesPath(uid, "job")},
		{"series", c.BuildSeriesPath(uid)},
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
