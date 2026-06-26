package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"

	"github.com/f-gillmann/demolens/v2/model"
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
			_ = r.AddGoComments("github.com/f-gillmann/demolens/v2", "./")

			// the model package custom-marshals these two types into shapes the
			// reflector cannot infer from their Go fields, so we override them
			// here instead of putting an invopop import into model.
			steamIDList := reflect.TypeOf(model.SteamIDList{})
			multiKills := reflect.TypeOf(model.MultiKills{})
			playerFrame := reflect.TypeOf(model.PlayerFrame{})
			positionStream := reflect.TypeOf(model.PositionStream{})
			groundItemFrame := reflect.TypeOf(model.GroundItemFrame{})
			// one columnar position tuple, shared by the playerFrame and positionStream
			// mappings so the field order / flag-bit doc stays in one place.
			positionTuple := func() *jsonschema.Schema {
				return &jsonschema.Schema{
					Type:        "array",
					MinItems:    u64(uint64(len(model.PositionFields))),
					MaxItems:    u64(uint64(len(model.PositionFields))),
					Description: "columnar position sample, fields in order: " + strings.Join(model.PositionFields, ", ") + " (flags bits: alive=1, airborne=2, scoped=4, ducking=8, has_defuse_kit=16, buyzone=32, walking=64, bomb_zone=128)",
				}
			}
			// one columnar ground-item ground-position tuple (no velocity/state).
			groundItemTuple := func() *jsonschema.Schema {
				return &jsonschema.Schema{
					Type:        "array",
					MinItems:    u64(uint64(len(model.GroundItemPositionFields))),
					MaxItems:    u64(uint64(len(model.GroundItemPositionFields))),
					Description: "columnar ground-item position sample, fields in order: " + strings.Join(model.GroundItemPositionFields, ", "),
				}
			}
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
				case positionStream:
					return &jsonschema.Schema{
						Type:                 "object",
						AdditionalProperties: &jsonschema.Schema{Type: "array", Items: positionTuple()},
						Description:          "per-round position samples grouped by steam_id (decimal string key); each value is that player's time-ordered array of columnar tuples",
					}
				case playerFrame:
					return positionTuple()
				case groundItemFrame:
					return groundItemTuple()
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
