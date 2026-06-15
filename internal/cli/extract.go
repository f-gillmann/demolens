package cli

import (
	"fmt"
	"os"

	"github.com/f-gillmann/demolens"
	"github.com/f-gillmann/demolens/internal/maps"
	"github.com/spf13/cobra"
)

// extractCmd builds a map's .tri collision file from your own CS2 map files via
// Source2Viewer-CLI.
func extractCmd() *cobra.Command {
	var mapParams maps.Params
	cmd := &cobra.Command{
		Use:   "extract-map",
		Short: "extract a CS2 map's collision geometry into a .tri file (needs source2viewer-cli)",
		Long:  "Decompiles a CS2 map's physics mesh into a neutral .tri collision file for time-to-damage line of sight; needs Source2Viewer-CLI on PATH (or pass --vrf), or pass your own .glb/.obj via --in.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out, n, err := demolens.ExtractMap(mapParams)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(os.Stderr, "wrote %s: %d triangles\n", out, n)
			return nil
		},
	}
	cmd.Flags().StringVar(&mapParams.In, "in", "", "pre-exported .glb/.gltf/.obj (skips Source2Viewer)")
	cmd.Flags().StringVar(&mapParams.VPK, "vpk", "", "map .vpk to extract")
	cmd.Flags().StringVar(&mapParams.CS2Dir, "cs2", "", "CS2 install dir (resolves official maps with --map)")
	cmd.Flags().StringVar(&mapParams.Map, "map", "", "official map name, e.g. de_mirage (with --cs2)")
	cmd.Flags().StringVar(&mapParams.VRF, "vrf", "source2viewer-cli", "path to source2viewer-cli")
	cmd.Flags().StringVar(&mapParams.Key, "key", "", "output name (workshop id or map name); defaults to --map")
	cmd.Flags().StringVar(&mapParams.OutDir, "out", "tris", "maps dir to write <key>.tri into")
	return cmd
}
