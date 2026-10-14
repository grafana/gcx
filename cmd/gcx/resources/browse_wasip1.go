package resources

import (
	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/spf13/cobra"
)

// addBrowseCommand is a no-op on wasip1: browse is a full-screen terminal UI.
func addBrowseCommand(*cobra.Command, *cmdconfig.Options) {}
