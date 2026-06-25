package cli

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/f-gillmann/demolens/v2"
	"github.com/spf13/cobra"
)

func checkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check <demo.dem>",
		Short: "check a demo and return its hash and header",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			path := args[0]
			absPath, err := filepath.Abs(path)
			if err != nil {
				return err
			}

			file, err := os.Open(absPath)
			if err != nil {
				return err
			}
			defer closeFile(file, &err)

			info, err := demolens.Validate(file)
			if err != nil {
				return err
			}

			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(struct {
				File     string `json:"file"`
				Format   string `json:"format"`
				FileHash string `json:"file_hash"`
			}{absPath, info.Format, info.FileHash})
		},
	}
}
