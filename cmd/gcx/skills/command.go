package skills

import (
	"embed"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	internalskills "github.com/grafana/gcx/internal/skills"
	"github.com/spf13/cobra"
)

// embeddedSkillsFS contains the shipped skill assets.
//
// The folder lives under cmd/gcx/skills/skills so ownership stays with
// the command package and release artifacts remain self-contained.
//
//go:embed skills
var embeddedSkillsFS embed.FS

var skillsManager = internalskills.NewManager(embeddedSkillsFS, "skills")

// Command returns the skills command group.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "skills",
		Aliases: []string{"skill"},
		Short:   "List and install bundled gcx agent skills",
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "small",
		},
	}

	cmd.AddCommand(newListCommand())
	cmd.AddCommand(newInstallCommand())

	return cmd
}

type listItem struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type listOpts struct {
	IO cmdio.Options
}

func (o *listOpts) setup(cmd *cobra.Command) {
	o.IO.DefaultFormat("text")
	o.IO.RegisterCustomCodec("text", &listTextCodec{})
	o.IO.BindFlags(cmd.Flags())
}

func (o *listOpts) Validate() error {
	return o.IO.Validate()
}

func newListCommand() *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List bundled skills available for installation",
		Args:  cobra.NoArgs,
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "small",
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			source, err := skillsManager.ResolveSource()
			if err != nil {
				return err
			}

			skills, err := skillsManager.List(source)
			if err != nil {
				return err
			}

			items := make([]listItem, 0, len(skills))
			for _, s := range skills {
				items = append(items, listItem{Name: s.Name, Description: s.Description})
			}

			return opts.IO.Encode(cmd.OutOrStdout(), items)
		},
	}
	opts.setup(cmd)
	return cmd
}

type installResult struct {
	Name   string `json:"name"`
	Target string `json:"target"`
	Force  bool   `json:"force"`
}

type installOpts struct {
	IO     cmdio.Options
	Target string
	Force  bool
}

func (o *installOpts) setup(cmd *cobra.Command) {
	o.IO.DefaultFormat("text")
	o.IO.RegisterCustomCodec("text", &installTextCodec{})
	o.IO.BindFlags(cmd.Flags())
	cmd.Flags().StringVar(&o.Target, "dir", skillsManager.DefaultInstallDir(), "Install directory for skills")
	cmd.Flags().BoolVar(&o.Force, "force", false, "Overwrite the destination skill directory if it already exists")
}

func (o *installOpts) Validate() error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(o.Target) == "" {
		return errors.New("install directory cannot be empty")
	}
	return nil
}

func newInstallCommand() *cobra.Command {
	opts := &installOpts{}
	cmd := &cobra.Command{
		Use:   "install <skill-name>",
		Short: "Install a bundled skill into your local skills directory",
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "small",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			source, err := skillsManager.ResolveSource()
			if err != nil {
				return err
			}

			skillName := strings.TrimSpace(args[0])
			if err := internalskills.ValidateSkillName(skillName); err != nil {
				return err
			}
			if !skillsManager.Exists(source, skillName) {
				return fmt.Errorf("skill %q not found. Run `gcx skills list` to see available skills", skillName)
			}

			targetDir := filepath.Join(opts.Target, skillName)
			if err := skillsManager.Install(source, skillName, targetDir, opts.Force); err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), installResult{
				Name:   skillName,
				Target: targetDir,
				Force:  opts.Force,
			})
		},
	}
	opts.setup(cmd)
	return cmd
}

type listTextCodec struct{}

func (c *listTextCodec) Format() format.Format { return "text" }

func (c *listTextCodec) Encode(w io.Writer, v any) error {
	items, ok := v.([]listItem)
	if !ok {
		return fmt.Errorf("unexpected payload type %T", v)
	}
	if len(items) == 0 {
		_, err := fmt.Fprintln(w, "No skills found.")
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', tabwriter.TabIndent|tabwriter.DiscardEmptyColumns)
	_, _ = fmt.Fprintln(tw, "NAME\tDESCRIPTION")
	for _, it := range items {
		_, _ = fmt.Fprintf(tw, "%s\t%s\n", it.Name, it.Description)
	}
	return tw.Flush()
}

func (c *listTextCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("skills list text codec does not support decoding")
}

type installTextCodec struct{}

func (c *installTextCodec) Format() format.Format { return "text" }

func (c *installTextCodec) Encode(w io.Writer, v any) error {
	res, ok := v.(installResult)
	if !ok {
		return fmt.Errorf("unexpected payload type %T", v)
	}
	_, err := fmt.Fprintf(w, "Installed skill %q to %s\n", res.Name, res.Target)
	return err
}

func (c *installTextCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("skills install text codec does not support decoding")
}
