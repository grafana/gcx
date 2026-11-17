//go:build !wasip1

package instrumentation

import (
	"github.com/grafana/gcx/cmd/gcx/instrumentation/check"
	"github.com/grafana/gcx/cmd/gcx/instrumentation/explain"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

// localCommands are the subcommands that inspect a local project with
// otel-checker.
func localCommands(loader *providers.ConfigLoader) []*cobra.Command {
	return []*cobra.Command{check.Command(loader), explain.Command(), explain.ListCommand()}
}
