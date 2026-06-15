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
			defer closeFile(file, &err)

			result, err := demolens.Analyze(file, opts)
			if err != nil {
				return err
			}

			var w io.Writer = os.Stdout
			var path string
			if out != "" {
				path = out
				if info, serr := os.Stat(out); serr == nil && info.IsDir() {
					path = filepath.Join(out, result.FileHash+".json")
				}
				outFile, ferr := os.Create(path)
				if ferr != nil {
					return ferr
				}
				defer closeFile(outFile, &err)
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
	calibration := &opts.Calibration
	cmd.Flags().StringVar(&opts.MapsDir, "maps-dir", "tris", "dir of .tri map meshes for time-to-damage line of sight")
	cmd.Flags().Float64Var(&calibration.CrosshairConeDeg, "crosshair-cone", calibration.CrosshairConeDeg, "crosshair appearance cone (deg)")
	cmd.Flags().Float64Var(&calibration.TTDFovDeg, "ttd-fov", calibration.TTDFovDeg, "time-to-damage 'saw enemy' frustum half-FOV (deg)")
	cmd.Flags().Float64Var(&calibration.TTDGapMs, "ttd-gap", calibration.TTDGapMs, "time-to-damage sighting reset gap / re-peek lockout (ms)")
	cmd.Flags().Float64Var(&calibration.TTDDebounceMs, "ttd-debounce", calibration.TTDDebounceMs, "time-to-damage min continuous-visibility before the clock starts (ms)")
	cmd.Flags().Float64Var(&calibration.TTDFloorMs, "ttd-floor", calibration.TTDFloorMs, "time-to-damage min sample (ms)")
	cmd.Flags().Float64Var(&calibration.TTDClampMs, "ttd-clamp", calibration.TTDClampMs, "time-to-damage kept-sample cap (ms)")
	cmd.Flags().Float64Var(&calibration.TTDOutlierFactor, "ttd-outlier", calibration.TTDOutlierFactor, "time-to-damage drop samples over N x player median")
	cmd.Flags().Float64Var(&calibration.CSConeDeg, "cs-cone", calibration.CSConeDeg, "counter-strafe / spray 'enemy in vision' frustum half-FOV (deg)")
	cmd.Flags().Float64Var(&calibration.CSRatio, "cs-ratio", calibration.CSRatio, "counter-strafe good-shot speed ratio")
	cmd.Flags().Float64Var(&calibration.CSRecentMs, "cs-recent", calibration.CSRecentMs, "counter-strafe / spotted recently-seen window (ms)")
	cmd.Flags().Float64Var(&calibration.SprayConeDeg, "spray-cone", calibration.SprayConeDeg, "spray 'aiming at enemy' frustum half-FOV (deg)")
	cmd.Flags().Float64Var(&calibration.SprayHitWindowMs, "spray-hit-window", calibration.SprayHitWindowMs, "spray shot-to-impact match window (ms)")
	cmd.Flags().Float64Var(&calibration.FlashBlindScale, "flash-scale", calibration.FlashBlindScale, "scale flash duration to effective blind time")
	return cmd
}
