package cli

import (
	"github.com/spf13/cobra"
)

const version = "0.0.1"

func NewRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "demolens",
		Short:         "CS2 demo inspection tool",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.AddCommand(checkCmd(), analyzeCmd())
	return root
}
