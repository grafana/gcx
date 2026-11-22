package errortracking

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

type ref struct {
	value string
	id    int64
	name  string
}

func (r *ref) parse(cmd *cobra.Command, label, kind string) error {
	r.value = strings.TrimSpace(r.value)
	if r.value == "" {
		return usage(cmd, label+" must not be empty")
	}
	if id, numeric, ok := parseID(r.value); numeric {
		if !ok {
			return usage(cmd, fmt.Sprintf("%s must be a positive %s ID up to %d or a %s name", label, kind, maxID, kind))
		}
		r.id = id
	}
	return nil
}

func resourceName(name string, id int64) string {
	return adapter.ComposeName(adapter.SlugifyName(name), strconv.FormatInt(id, 10))
}

type named struct {
	name string
	id   int64
}

func pick[T any](r *ref, label, kind, useID string, items []T, ident func(T) (string, int64)) (int64, error) {
	if r.id != 0 {
		return r.id, nil
	}
	var matches []named
	all := make([]named, 0, len(items))
	for _, item := range items {
		name, id := ident(item)
		all = append(all, named{name, id})
		if strings.EqualFold(resourceName(name, id), r.value) || strings.EqualFold(name, r.value) {
			matches = append(matches, named{name, id})
		}
	}
	switch len(matches) {
	case 1:
		r.id, r.name = matches[0].id, matches[0].name
		return r.id, nil
	case 0:
		if s, ok := adapter.ExtractIDFromSlug(r.value); ok && s != r.value {
			id, _ := strconv.ParseInt(s, 10, 64)
			for _, n := range all {
				if n.id == id {
					return 0, &gcxerrors.DetailedError{
						Summary:     gcxerrors.SummaryResourceNotFound,
						Details:     fmt.Sprintf("no %s is called %q; %s %d is now %q", kind, r.value, kind, id, printable(n.name)),
						Suggestions: []string{"Use the ID: " + strings.ReplaceAll(useID, "{id}", s)},
					}
				}
			}
		}
		return 0, &gcxerrors.DetailedError{
			Summary:     gcxerrors.SummaryResourceNotFound,
			Details:     fmt.Sprintf("no Error Tracking %s has the ID or name %q", kind, r.value),
			Suggestions: []string{fmt.Sprintf("List what exists: gcx error-tracking %ss list", kind)},
		}
	}
	shown := make([]string, len(matches))
	ids := make([]string, len(matches))
	for i, m := range matches {
		shown[i] = fmt.Sprintf("%s (%d)", printable(m.name), m.id)
		ids[i] = strconv.FormatInt(m.id, 10)
	}
	return 0, &gcxerrors.DetailedError{
		Summary:     gcxerrors.SummaryInvalidCommandUsage,
		Details:     fmt.Sprintf("%s %q matches %d %ss: %s", label, r.value, len(matches), kind, strings.Join(shown, ", ")),
		Suggestions: []string{fmt.Sprintf("Pass one of these %s IDs instead: %s", kind, strings.Join(ids, ", "))},
		ExitCode:    new(gcxerrors.ExitUsageError),
	}
}

type target interface {
	resolve(ctx context.Context, loader *providers.ConfigLoader, c *client) (int64, error)
}

const (
	providerName = "errortracking"
)

type projectRef struct {
	ref

	cmd *cobra.Command
}

func (p *projectRef) label() string { return "<project>" }

func (p *projectRef) validate(cmd *cobra.Command) error {
	p.cmd = cmd
	return p.parse(cmd, p.label(), "project")
}

func (p *projectRef) useID() string {
	args := strings.Fields(p.cmd.Use)[1:]
	return strings.Join(append([]string{p.cmd.CommandPath(), "{id}"}, args[1:]...), " ")
}

func (p *projectRef) pick(projects []project) (int64, error) {
	return pick(&p.ref, p.label(), "project", p.useID(), projects, func(pr project) (string, int64) { return pr.Name, pr.ID })
}

func (p *projectRef) resolve(ctx context.Context, loader *providers.ConfigLoader, c *client) (int64, error) {
	c.project = p
	if p.id == 0 {
		projects, err := c.listProjects(ctx)
		if err != nil {
			return 0, err
		}
		if _, err := p.pick(projects); err != nil {
			return 0, err
		}
	}
	return p.id, nil
}

func (p *projectRef) shown() string { return projectLabel(p.id, p.name) }

func (p *projectRef) argv() []string {
	return os.Args
}

func nonEmpty(cmd *cobra.Command, names ...string) error {
	for _, name := range names {
		if f := cmd.Flags().Lookup(name); f != nil && f.Changed && strings.TrimSpace(f.Value.String()) == "" {
			return usage(cmd, "--"+name+" must not be empty")
		}
	}
	return nil
}
