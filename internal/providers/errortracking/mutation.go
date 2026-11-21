package errortracking

import (
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type projectInfo struct {
	ID   int64  `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

type mutationBase struct {
	cmdio.SingleMutation `json:",inline"`

	Project *projectInfo `json:"project,omitempty"`

	text string
}

func (m mutationBase) line() string { return m.text }

type assignMutation struct {
	mutationBase `json:",inline"`

	User string `json:"user,omitempty"`
}

type projectDeleteMutation struct {
	mutationBase `json:",inline"`

	ClearedDefault bool `json:"clearedDefault,omitempty"`
}

type commentMutation struct {
	mutationBase `json:",inline"`

	ErrorID string `json:"errorId"`
}

type ingestKeyMutation struct {
	mutationBase `json:",inline"`

	PreviousIngestKeyUntil string `json:"previousIngestKeyUntil,omitempty"`
}

type noted interface {
	line() string
}

type hint struct{ summary, command string }

func (c *client) mutation(action, kind string, id int64, changed bool) mutationBase {
	m := cmdio.NewSingleMutation(action, cmdio.MutationTarget{Kind: kind, Namespace: c.namespace})
	m.Changed = &changed
	if id != 0 {
		m.Target.ID = strconv.FormatInt(id, 10)
	}
	return mutationBase{SingleMutation: m, Project: c.projectInfo()}
}

func (c *client) projectInfo() *projectInfo {
	if c.project == nil || c.project.id == 0 {
		return nil
	}
	return &projectInfo{ID: c.project.id, Name: c.project.name}
}

func (c *client) projectShown() string {
	if c.project == nil {
		return ""
	}
	return c.project.shown()
}

func (c *client) projectArg() string {
	if c.project == nil || c.project.id == 0 {
		return "<project>"
	}
	return strconv.FormatInt(c.project.id, 10)
}

func (c *client) knowProject(id int64, name string) {
	if c.project != nil && c.project.id == id && name != "" {
		c.project.name = name
	}
}

func bindText(opts *cmdio.Options, flags *pflag.FlagSet, codec format.Codec) {
	opts.RegisterCustomCodec("text", codec)
	opts.DefaultFormat("text")
	opts.BindFlags(flags)
}

type mutationText struct{}

func (mutationText) Format() format.Format { return "text" }

func (mutationText) Decode(io.Reader, any) error {
	return errors.New("text codec does not support decoding")
}

func (mutationText) Encode(w io.Writer, v any) error {
	switch m := v.(type) {
	case noted:
		cmdio.Success(w, "%s", printable(m.line()))
		return nil
	case batchUpdate:
		switch {
		case m.Summary.Failed > 0 && m.Summary.Succeeded == 0:
			cmdio.Error(w, "%s", printable(m.text))
		case m.Summary.Failed > 0:
			cmdio.Warning(w, "%s", printable(m.text))
		default:
			cmdio.Success(w, "%s", printable(m.text))
		}
		for _, f := range m.Failures {
			fmt.Fprintf(w, "  %s: %s\n", printable(f.Target.ID), printable(f.Error))
		}
		if len(m.Results) == 0 {
			return nil
		}
		return cleanTable(batchTable()).Codec("table").Encode(w, m.Results)
	}
	return fmt.Errorf("invalid data type for text codec: expected a mutation result, got %T", v)
}

func emit(cmd *cobra.Command, opts *cmdio.Options, v any, hints ...hint) error {
	if err := opts.Encode(cmd.OutOrStdout(), v); err != nil {
		return err
	}
	for _, h := range hints {
		say(cmd.ErrOrStderr(), h.summary, h.command)
	}
	return nil
}

func deleteBypass(cmd *cobra.Command, force bool) (bool, error) {
	ok, err := providers.CheckDestructiveBypass(force)
	if errors.Is(err, providers.ErrAgentModeRequiresForce) {
		return false, gcxerrors.NewCommandUsageError(cmd, err.Error(), err)
	}
	return ok, err
}

func confirm(cmd *cobra.Command, question, summary, details string) error {
	ok, err := providers.Prompt(cmd.InOrStdin(), cmd.ErrOrStderr(), printable(question))
	if err != nil {
		return gcxerrors.NewCommandUsageError(cmd, err.Error(), err)
	}
	if ok {
		return nil
	}
	return &gcxerrors.DetailedError{Summary: summary, Details: printable(details), ExitCode: new(gcxerrors.ExitCancelled)}
}
