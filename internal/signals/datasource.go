package signals

import (
	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

// DatasourceProvider returns a datasource provider backed by the same command
// builder shape used by top-level signal commands.
func DatasourceProvider(kind, short string, query CommandBuilder, extras ...CommandBuilder) datasources.DatasourceProvider {
	return datasourceProvider{
		kind:   kind,
		short:  short,
		query:  query,
		extras: extras,
	}
}

type datasourceProvider struct {
	kind   string
	short  string
	query  CommandBuilder
	extras []CommandBuilder
}

func (p datasourceProvider) Kind() string { return p.kind }

func (p datasourceProvider) ShortDesc() string { return p.short }

func (p datasourceProvider) QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	if p.query == nil {
		return nil
	}
	return p.query(loader)
}

func (p datasourceProvider) ExtraCommands(loader *providers.ConfigLoader) []*cobra.Command {
	cmds := make([]*cobra.Command, 0, len(p.extras))
	for _, build := range p.extras {
		if build == nil {
			continue
		}
		cmds = append(cmds, build(loader))
	}
	return cmds
}
