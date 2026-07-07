package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/f-gillmann/demolens/v2"
	"github.com/spf13/cobra"
)

func analyzeCmd() *cobra.Command {
	var out string
	var minify, gzipOut bool
	var opts demolens.Options
	opts.Calibration = demolens.DefaultCalibration()

	cmd := &cobra.Command{
		Use:   "analyze <demo.dem>",
		Short: "analyze a demo and print JSON",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			resolveStreams(cmd, &opts)

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
					name := result.FileHash + ".json"
					if gzipOut {
						name += ".gz"
					}
					path = filepath.Join(out, name)
				}
				outFile, ferr := os.Create(path)
				if ferr != nil {
					return ferr
				}
				defer closeFile(outFile, &err)
				w = outFile
			}

			if gzipOut {
				err = demolens.WriteGzJSON(w, result, minify)
			} else {
				err = demolens.WriteJSON(w, result, minify)
			}
			if err != nil {
				return err
			}
			if path != "" {
				_, _ = fmt.Fprintln(os.Stderr, "wrote", path)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&out, "out", "o", "", "write JSON to a file path, or a directory to name it <file_hash>.json (default: stdout)")
	cmd.Flags().BoolVar(&minify, "minify", false, "compact JSON output (no indentation)")
	cmd.Flags().BoolVar(&gzipOut, "gzip", false, "gzip-compress the output (adds .gz when -o names by file_hash)")
	cmd.Flags().StringVar(&opts.Tier, "tier", "full", "stream preset: core (no streams), detail (positions/shots/grenade-paths), full (all streams)")
	cmd.Flags().BoolVarP(&opts.PlayerFrames, "positions", "p", false, "override the 'positions' stream (per-frame player positions + state, large output)")
	cmd.Flags().BoolVarP(&opts.Shots, "shots", "s", false, "override the 'shots' stream (per-shot shooter geometry, large output)")
	cmd.Flags().BoolVarP(&opts.GrenadePaths, "grenade-paths", "g", false, "override the 'grenade_paths' stream (grenade trajectories + bounces, large output)")
	cmd.Flags().BoolVar(&opts.Inventory, "inventory", false, "override the 'inventory' stream (mid-round inventory change log)")
	cmd.Flags().BoolVar(&opts.GroundItems, "ground-items", false, "override the 'ground_items' stream (ground-weapon intervals: dropped guns + when picked up)")
	cmd.Flags().Float64Var(&opts.PositionsHz, "positions-hz", 4, "positions stream sample rate in Hz")
	calibration := &opts.Calibration
	cmd.Flags().StringVar(&opts.MapsDir, "maps-dir", "tris", "dir of .tri map meshes for time-to-damage line of sight")
	cmd.Flags().Float64Var(&calibration.TTDFovDeg, "ttd-fov", calibration.TTDFovDeg, "time-to-damage 'saw enemy' frustum half-FOV (deg)")
	cmd.Flags().Float64Var(&calibration.TTDGapMs, "ttd-gap", calibration.TTDGapMs, "time-to-damage sighting reset gap / re-peek lockout (ms)")
	cmd.Flags().Float64Var(&calibration.TTDDebounceMs, "ttd-debounce", calibration.TTDDebounceMs, "time-to-damage min continuous-visibility before the clock starts (ms)")
	cmd.Flags().Float64Var(&calibration.TTDFloorMs, "ttd-floor", calibration.TTDFloorMs, "time-to-damage min sample (ms)")
	cmd.Flags().Float64Var(&calibration.TTDExcludeMs, "ttd-exclude", calibration.TTDExcludeMs, "time-to-damage trigger-discipline cutoff: drop samples at or above this (ms)")
	cmd.Flags().Float64Var(&calibration.TTDPercentile, "ttd-percentile", calibration.TTDPercentile, "time-to-damage reported percentile of kept samples (50=median)")
	cmd.Flags().Float64Var(&calibration.CrosshairFovDeg, "crosshair-fov", calibration.CrosshairFovDeg, "crosshair-placement 'saw enemy' frustum half-FOV (deg)")
	cmd.Flags().Float64Var(&calibration.CrosshairGapMs, "crosshair-gap", calibration.CrosshairGapMs, "crosshair-placement sighting reset gap (ms)")
	cmd.Flags().Float64Var(&calibration.CrosshairDebounceMs, "crosshair-debounce", calibration.CrosshairDebounceMs, "crosshair-placement min continuous-visibility before the sighting commits (ms)")
	cmd.Flags().Float64Var(&calibration.CrosshairPeekGapMs, "crosshair-peek-gap", calibration.CrosshairPeekGapMs, "crosshair-placement appearance-view re-anchor gap: re-snapshot when a fresh window opens after this unseen gap (ms)")
	cmd.Flags().Float64Var(&calibration.CrosshairWinsorPct, "crosshair-winsor", calibration.CrosshairWinsorPct, "crosshair-placement low-winsor percentile: clamp samples below this up to it, then mean")
	cmd.Flags().Float64Var(&calibration.CSConeDeg, "cs-cone", calibration.CSConeDeg, "counter-strafe / spray 'enemy in vision' frustum half-FOV (deg)")
	cmd.Flags().Float64Var(&calibration.CSRatio, "cs-ratio", calibration.CSRatio, "counter-strafe good-shot speed ratio")
	cmd.Flags().Float64Var(&calibration.CSRecentMs, "cs-recent", calibration.CSRecentMs, "counter-strafe / spotted recently-seen window (ms)")
	cmd.Flags().Float64Var(&calibration.SprayConeDeg, "spray-cone", calibration.SprayConeDeg, "spray 'aiming at enemy' frustum half-FOV (deg)")
	cmd.Flags().Float64Var(&calibration.SprayHitWindowMs, "spray-hit-window", calibration.SprayHitWindowMs, "spray shot-to-impact match window (ms)")
	cmd.Flags().Float64Var(&calibration.FlashBlindScale, "flash-scale", calibration.FlashBlindScale, "scale flash duration to effective blind time")
	cmd.Flags().Float64Var(&calibration.FlashBlindFraction, "flash-blind-fraction", calibration.FlashBlindFraction, "sighting clocks stay paused while this fraction of the shooter's current flash duration still remains (0 = any flash time blinds)")
	cmd.Flags().StringVar(&opts.AimDebugPath, "aim-debug-path", "", "write per-frame aim calibration CSVs to <path>.{cands,dmg,legend}.csv (debug)")
	return cmd
}

// resolveStreams folds the --tier preset and per-stream override flags into the
// final stream booleans: the tier sets the baseline, then any flag the user
// explicitly passed overrides its stream on top.
func resolveStreams(cmd *cobra.Command, opts *demolens.Options) {
	type override struct {
		name string
		flag *bool
	}
	overrides := []override{
		{"positions", &opts.PlayerFrames},
		{"shots", &opts.Shots},
		{"grenade-paths", &opts.GrenadePaths},
		{"inventory", &opts.Inventory},
		{"ground-items", &opts.GroundItems},
	}

	// snapshot the explicit values before ResolveTier stomps every bool.
	explicit := make(map[string]bool, len(overrides))
	for _, o := range overrides {
		explicit[o.name] = *o.flag
	}

	opts.ResolveTier()

	for _, o := range overrides {
		if cmd.Flags().Changed(o.name) {
			*o.flag = explicit[o.name]
		}
	}

	// a non-preset Tier makes the parser keep these booleans instead of re-applying a preset.
	opts.Tier = "custom"
}
