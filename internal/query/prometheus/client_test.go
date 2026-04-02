package prometheus

import (
	"net/url"
	"strings"
	"testing"
)

func TestBuildPathsEscapeDatasourceUID(t *testing.T) {
	c := &Client{}
	uid := "uid/../admin"
	escapedUID := url.PathEscape(uid)

	tests := []struct {
		name string
		path string
	}{
		{"labels", c.buildLabelsPath(uid)},
		{"labelValues", c.buildLabelValuesPath(uid, "job")},
		{"metadata", c.buildMetadataPath(uid)},
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
