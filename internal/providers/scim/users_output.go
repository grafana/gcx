package scim

import (
	"errors"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
)

type userMutation struct {
	cmdio.SingleMutation `yaml:",inline"`

	User    userResource   `json:"user,omitempty" yaml:"user,omitempty"`
	Request map[string]any `json:"request,omitempty" yaml:"request,omitempty"`
}

func newUserMutation(action, id, name string) userMutation {
	return userMutation{SingleMutation: cmdio.NewSingleMutation(action, cmdio.MutationTarget{Kind: "SCIMUser", ID: id, Name: name})}
}

type userMutationCodec struct{}

func (userMutationCodec) Format() format.Format { return "text" }
func (userMutationCodec) Decode(io.Reader, any) error {
	return errors.New("text codec does not support decoding")
}
func (userMutationCodec) Encode(w io.Writer, v any) error {
	result, ok := v.(userMutation)
	if !ok {
		return errors.New("invalid SCIM mutation result for text output")
	}
	if result.DryRun {
		if _, err := fmt.Fprintf(w, "Dry run: %s SCIM user %q (id=%s). No write was sent.\n", result.Action, result.Target.Name, result.Target.ID); err != nil {
			return err
		}
		return format.NewYAMLCodec().Encode(w, result.Request)
	}
	_, err := fmt.Fprintf(w, "%s SCIM user %q (id=%s).\n", map[string]string{"created": "Created", "updated": "Updated", "deactivated": "Deactivated"}[result.Action], result.Target.Name, result.Target.ID)
	return err
}
