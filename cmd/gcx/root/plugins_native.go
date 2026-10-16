//go:build !wasip1

package root

import (
	"github.com/grafana/gcx/cmd/gcx/plugins"
	"github.com/spf13/cobra"
)

func addPlugins(c *cobra.Command) { c.AddCommand(plugins.Command()) }
