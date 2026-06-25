package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"

	"github.com/f-gillmann/demolens/model"
	"github.com/invopop/jsonschema"
	"github.com/spf13/cobra"
)

// u64 returns a pointer to n for the *uint64 MinItems/MaxItems schema fields.
func u64(n uint64) *uint64 { return &n }

func schemaCmd() *cobra.Command {
	var out string

	cmd := &cobra.Command{
		Use:   "schema",
		Short: "print the JSON Schema of the analyze output",
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			r := &jsonschema.Reflector{}

			// best-effort: Go doc comments only resolve when run from the repo
			// root, so a failure here just means no descriptions.
			_ = r.AddGoComments("github.com/f-gillmann/demolens", "./")

			// the model package custom-marshals these two types into shapes the
			// reflector cannot infer from their Go fields, so we override them
			// here instead of putting an invopop import into model.
			steamIDList := reflect.TypeOf(model.SteamIDList{})
			multiKills := reflect.TypeOf(model.MultiKills{})
			r.Mapper = func(t reflect.Type) *jsonschema.Schema {
				switch t {
				case steamIDList:
					return &jsonschema.Schema{
						Type:        "array",
						Items:       &jsonschema.Schema{Type: "string"},
						Description: "SteamID64 values encoded as decimal strings",
					}
				case multiKills:
					return &jsonschema.Schema{
						Type:        "array",
						Items:       &jsonschema.Schema{Type: "integer"},
						MinItems:    u64(5),
						MaxItems:    u64(5),
						Description: "rounds with exactly [1,2,3,4,5] kills (index 0 = 1k ... index 4 = 5k)",
					}
				}
				return nil
			}

			s := r.Reflect(&model.Match{})
			data, err := json.MarshalIndent(s, "", "  ")
			if err != nil {
				return err
			}

			var w io.Writer = os.Stdout
			if out != "" {
				outFile, ferr := os.Create(out)
				if ferr != nil {
					return ferr
				}
				defer closeFile(outFile, &err)
				w = outFile
			}

			if _, err := w.Write(append(data, '\n')); err != nil {
				return err
			}
			if out != "" {
				_, _ = fmt.Fprintln(os.Stderr, "wrote", out)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&out, "out", "o", "", "write the schema to a file path (default: stdout)")
	return cmd
}
