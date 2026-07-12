package cli

import (
	"os"

	"github.com/spf13/cobra"
)

const version = "2.4.0"

// closeFile closes f and reports the close error through err only if no earlier error was set.
func closeFile(f *os.File, err *error) {
	if cerr := f.Close(); cerr != nil && *err == nil {
		*err = cerr
	}
}

func NewRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "demolens",
		Short:         "Simple CS2 demo analyzer",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.AddCommand(checkCmd(), analyzeCmd(), extractCmd(), schemaCmd())
	return root
}
