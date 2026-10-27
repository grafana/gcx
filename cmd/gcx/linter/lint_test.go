package linter

import (
	"errors"
	"testing"

	"github.com/grafana/gcx/internal/linter"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/local"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func Test_errorToViolation(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want *linter.Violation
	}{
		{
			name: "deprecated resource",
			err: resources.DeprecatedResourceError{
				Ref: "ref",
				Used: schema.GroupVersion{
					Group:   "folder.grafana.app",
					Version: "v1",
				},
				Alternative: schema.GroupVersion{
					Group:   "folder.grafana.app",
					Version: "v2",
				},
			},
			want: &linter.Violation{
				Rule:         "deprecated-resource",
				ResourceType: "folder",
				Category:     "reader",
				Severity:     "warning",
				Location:     linter.Location{File: "file.json"},
				Description:  "apiVersion \"folder.grafana.app/v1\" is deprecated; use \"folder.grafana.app/v2\" instead",
				Details:      "apiVersion \"folder.grafana.app/v1\" is deprecated; use \"folder.grafana.app/v2\" instead",
			},
		},
		{
			name: "duplicated resource",
			err: resources.DuplicatedResourceError{
				Ref:   "ref",
				Files: []string{"file.json", "other.json"},
			},
			want: &linter.Violation{
				Rule:         "duplicated-resource",
				ResourceType: "file",
				Category:     "reader",
				Severity:     "error",
				Location:     linter.Location{File: "file.json"},
				Description:  "duplicated resource \"ref\" found in file.json, other.json",
				Details:      "duplicated resource \"ref\" found in file.json, other.json",
			},
		},
		{
			name: "syntax error",
			err: local.ParseError{
				File: "file.json",
				Err:  errors.New("some parse error"),
			},
			want: &linter.Violation{
				Rule:         "syntax-error",
				ResourceType: "file",
				Category:     "reader",
				Severity:     "fatal",
				Location:     linter.Location{File: "file.json"},
				Description:  "parse error in 'file.json': some parse error",
				Details:      "parse error in 'file.json': some parse error",
			},
		},
		{
			name: "unrecognized format",
			err: local.UnrecognisedFormatError{
				File:   "file.json",
				Format: "md",
			},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := require.New(t)

			violation := errorToViolation(tt.err, "file.json")

			if tt.want == nil {
				req.Nil(violation)
			} else {
				req.Equal(*tt.want, *violation)
			}
		})
	}
}
