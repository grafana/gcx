package instrumentation

import (
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

// localCommands is empty in wasip1 builds: they run embedded, with no local
// project to inspect, and leaving otel-checker out shortens module start-up.
func localCommands(*providers.ConfigLoader) []*cobra.Command {
	return nil
}
