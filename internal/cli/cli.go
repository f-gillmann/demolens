package cli

import (
	"github.com/spf13/cobra"
)

const version = "1.0.1"

func NewRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "demolens",
		Short:         "Simple CS2 demo analyzer",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.AddCommand(checkCmd(), analyzeCmd(), extractCmd())
	return root
}
