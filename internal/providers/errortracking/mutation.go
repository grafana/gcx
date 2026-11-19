package errortracking

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type projectInfo struct {
	ID   int64  `json:"id,omitempty" yaml:"id,omitempty"`
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
}

type mutationBase struct {
	cmdio.SingleMutation `json:",inline" yaml:",inline"`

	Project *projectInfo `json:"project,omitempty" yaml:"project,omitempty"`

	text string
}

func (m mutationBase) line() string { return m.text }

type assignMutation struct {
	mutationBase `json:",inline" yaml:",inline"`

	User string `json:"user,omitempty" yaml:"user,omitempty"`
}

type projectDeleteMutation struct {
	mutationBase `json:",inline" yaml:",inline"`

	ClearedDefault bool `json:"clearedDefault,omitempty" yaml:"clearedDefault,omitempty"`
}

type commentMutation struct {
	mutationBase `json:",inline" yaml:",inline"`

	ErrorID string `json:"errorId" yaml:"errorId"`
}

type ingestKeyMutation struct {
	mutationBase `json:",inline" yaml:",inline"`

	PreviousIngestKeyUntil string `json:"previousIngestKeyUntil,omitempty" yaml:"previousIngestKeyUntil,omitempty"`
}

type sourceMapMutation struct {
	mutationBase `json:",inline" yaml:",inline"`

	Release      string        `json:"release" yaml:"release"`
	Host         string        `json:"host,omitempty" yaml:"host,omitempty"`
	Files        []uploadedMap `json:"files,omitempty" yaml:"files,omitempty"`
	Skipped      []string      `json:"skipped,omitempty" yaml:"skipped,omitempty"`
	SkippedLinks []string      `json:"skippedLinks,omitempty" yaml:"skippedLinks,omitempty"`
}

type noted interface {
	line() string
}

type uploadedMap struct {
	File   string `json:"file" yaml:"file"`
	Bundle string `json:"bundle" yaml:"bundle"`
	Size   int64  `json:"size" yaml:"size"`
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
	case sourceMapMutation:
		cmdio.Success(w, "%s", printable(m.text))
		if len(m.Files) > 0 {
			if err := cleanTable(uploadTable()).Codec("table").Encode(w, m.Files); err != nil {
				return err
			}
		}
		return nil
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
	ok, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), false, printable(question))
	if err != nil {
		return gcxerrors.NewCommandUsageError(cmd, err.Error(), err)
	}
	if ok {
		return nil
	}
	return &gcxerrors.DetailedError{Summary: summary, Details: printable(details), ExitCode: new(gcxerrors.ExitCancelled)}
}

func joinShort(items []string) string {
	const n = 5
	if len(items) <= n {
		return strings.Join(items, ", ")
	}
	return strings.Join(items[:n], ", ") + fmt.Sprintf(" and %d more", len(items)-n)
}
