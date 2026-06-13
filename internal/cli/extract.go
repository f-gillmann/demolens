package cli

import (
	"fmt"
	"os"

	"github.com/f-gillmann/demolens/maps"
	"github.com/spf13/cobra"
)

// extractCmd builds a map's .tri collision file from your own CS2 map files via
// Source2Viewer-CLI.
func extractCmd() *cobra.Command {
	var p maps.Params
	cmd := &cobra.Command{
		Use:   "extract-map",
		Short: "extract a CS2 map's collision geometry into a .tri file (needs source2viewer-cli)",
		Long: "Decompiles a CS2 map's physics mesh with Source2Viewer-CLI and writes a\n" +
			"neutral .tri collision file for time-to-damage line of sight. Reads YOUR own\n" +
			"map files; bundles no Valve assets. Install Source2Viewer-CLI from\n" +
			"https://github.com/ValveResourceFormat/ValveResourceFormat and put it on PATH\n" +
			"(or pass --vrf). Or export a .glb/.obj yourself and pass --in.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out, n, err := maps.Extract(p)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(os.Stderr, "wrote %s: %d triangles\n", out, n)
			return nil
		},
	}
	cmd.Flags().StringVar(&p.In, "in", "", "pre-exported .glb/.gltf/.obj (skips Source2Viewer)")
	cmd.Flags().StringVar(&p.VPK, "vpk", "", "map .vpk to extract")
	cmd.Flags().StringVar(&p.CS2Dir, "cs2", "", "CS2 install dir (resolves official maps with --map)")
	cmd.Flags().StringVar(&p.Map, "map", "", "official map name, e.g. de_mirage (with --cs2)")
	cmd.Flags().StringVar(&p.VRF, "vrf", "source2viewer-cli", "path to source2viewer-cli")
	cmd.Flags().StringVar(&p.Key, "key", "", "output name (workshop id or map name); defaults to --map")
	cmd.Flags().StringVar(&p.OutDir, "out", "tris", "maps dir to write <key>.tri into")
	return cmd
}
