package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/f-gillmann/demolens"
	"github.com/spf13/cobra"
)

func analyzeCmd() *cobra.Command {
	var out string
	var opts demolens.Options
	opts.Calibration = demolens.DefaultCalibration()

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

			result, err := demolens.Analyze(file, opts)
			if err != nil {
				return err
			}

			var w io.Writer = os.Stdout
			var path string
			if out != "" {
				// if out is a dir (like "."), name the file by hash. otherwise it's the path.
				path = out
				if info, serr := os.Stat(out); serr == nil && info.IsDir() {
					path = filepath.Join(out, result.FileHash+".json")
				}
				outFile, ferr := os.Create(path)
				if ferr != nil {
					return ferr
				}
				defer func() {
					if cerr := outFile.Close(); cerr != nil && err == nil {
						err = cerr
					}
				}()
				w = outFile
			}

			enc := json.NewEncoder(w)
			enc.SetIndent("", "  ")
			if err := enc.Encode(result); err != nil {
				return err
			}
			if path != "" {
				_, _ = fmt.Fprintln(os.Stderr, "wrote", path)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&out, "out", "o", "", "write JSON to a file path, or a directory to name it <file_hash>.json (default: stdout)")
	cmd.Flags().BoolVarP(&opts.PlayerFrames, "positions", "p", false, "include per-frame player positions + state (large output)")
	cmd.Flags().BoolVarP(&opts.Shots, "shots", "s", false, "include per-shot shooter geometry (large output)")
	cmd.Flags().BoolVarP(&opts.GrenadePaths, "grenade-paths", "g", false, "include grenade trajectories + bounces (large output)")
	c := &opts.Calibration
	cmd.Flags().StringVar(&opts.MapsDir, "maps-dir", "tris", "dir of .tri map meshes for time-to-damage line of sight")
	cmd.Flags().Float64Var(&c.CrosshairConeDeg, "crosshair-cone", c.CrosshairConeDeg, "crosshair appearance cone (deg)")
	cmd.Flags().Float64Var(&c.TTDFovDeg, "ttd-fov", c.TTDFovDeg, "time-to-damage 'saw enemy' frustum half-FOV (deg)")
	cmd.Flags().Float64Var(&c.TTDGapMs, "ttd-gap", c.TTDGapMs, "time-to-damage sighting reset gap / re-peek lockout (ms)")
	cmd.Flags().Float64Var(&c.TTDDebounceMs, "ttd-debounce", c.TTDDebounceMs, "time-to-damage min continuous-visibility before the clock starts (ms)")
	cmd.Flags().Float64Var(&c.TTDFloorMs, "ttd-floor", c.TTDFloorMs, "time-to-damage min sample (ms)")
	cmd.Flags().Float64Var(&c.TTDClampMs, "ttd-clamp", c.TTDClampMs, "time-to-damage kept-sample cap (ms)")
	cmd.Flags().Float64Var(&c.TTDOutlierFactor, "ttd-outlier", c.TTDOutlierFactor, "time-to-damage drop samples over N x player median")
	cmd.Flags().Float64Var(&c.CSConeDeg, "cs-cone", c.CSConeDeg, "counter-strafe / spray 'enemy in vision' frustum half-FOV (deg)")
	cmd.Flags().Float64Var(&c.CSRatio, "cs-ratio", c.CSRatio, "counter-strafe good-shot speed ratio")
	cmd.Flags().Float64Var(&c.CSRecentMs, "cs-recent", c.CSRecentMs, "counter-strafe / spotted recently-seen window (ms)")
	cmd.Flags().Float64Var(&c.SprayConeDeg, "spray-cone", c.SprayConeDeg, "spray 'aiming at enemy' frustum half-FOV (deg)")
	cmd.Flags().Float64Var(&c.SprayHitWindowMs, "spray-hit-window", c.SprayHitWindowMs, "spray shot-to-impact match window (ms)")
	cmd.Flags().Float64Var(&c.FlashBlindScale, "flash-scale", c.FlashBlindScale, "scale flash duration to effective blind time")
	return cmd
}
