package cli

import (
	"encoding/json"
	"io"
	"os"

	"github.com/f-gillmann/demolens"
	"github.com/spf13/cobra"
)

func analyzeCmd() *cobra.Command {
	var output string

	cmd := &cobra.Command{
		Use:   "analyze <demo.dem>",
		Short: "analyze a demo and print JSON",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			file, err := os.Open(args[0])
			if err != nil {
				return err
			}
			defer func() {
				if cerr := file.Close(); cerr != nil && err == nil {
					err = cerr
				}
			}()

			result, err := demolens.Analyze(file)
			if err != nil {
				return err
			}

			w := io.Writer(os.Stdout)
			if output != "" {
				out, ferr := os.Create(output)
				if ferr != nil {
					return ferr
				}
				defer func() {
					if cerr := out.Close(); cerr != nil && err == nil {
						err = cerr
					}
				}()
				w = out
			}

			enc := json.NewEncoder(w)
			enc.SetIndent("", "  ")
			return enc.Encode(result)
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "", "write JSON to this file instead of stdout")
	return cmd
}
