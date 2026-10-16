package root

import "github.com/spf13/cobra"

func addExtensions(_ *cobra.Command)                                {}
func RewriteExtensionArgs(_ *cobra.Command, args []string) []string { return args }
