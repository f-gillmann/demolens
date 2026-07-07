package demolens

import (
	"io"

	"github.com/f-gillmann/demolens/v2/internal/maps"
	"github.com/f-gillmann/demolens/v2/internal/metrics"
	"github.com/f-gillmann/demolens/v2/internal/parser"
	"github.com/f-gillmann/demolens/v2/model"
)

// Options re-exports parser.Options, mostly so callers can flip on the heavy
// per-frame data without importing the parser package.
type Options = parser.Options

// Calibration re-exports parser.Calibration, the tunable aim-stat thresholds.
type Calibration = parser.Calibration

// ExtractMapParams re-exports maps.Params, the map extraction inputs.
type ExtractMapParams = maps.Params

// DefaultCalibration is the tuned defaults.
func DefaultCalibration() Calibration { return parser.DefaultCalibration() }

// Analyze parses a CS2 demo from r and computes the full match analytics.
func Analyze(r io.Reader, opts Options) (*model.Match, error) {
	match, err := parser.Parse(r, opts)
	if err != nil {
		return nil, err
	}

	metrics.Compute(match)
	return match, nil
}

// ExtractMap writes a map's .tri collision file and returns the path and triangle count.
func ExtractMap(p ExtractMapParams) (string, int, error) { return maps.Extract(p) }
