package demolens

import (
	"io"

	"github.com/f-gillmann/demolens/internal/demofile"
	"github.com/f-gillmann/demolens/internal/metrics"
	"github.com/f-gillmann/demolens/internal/parser"
	"github.com/f-gillmann/demolens/model"
)

// Analyze reads a CS2 demo from io.Reader and returns the match analytics.
func Analyze(r io.Reader) (*model.Match, error) {
	match, err := parser.Parse(r)
	if err != nil {
		return nil, err
	}

	metrics.Compute(match)
	return match, nil
}

// Validate validates the CS2 demo header and returns the format stamp and SHA-256.
func Validate(r io.Reader) (demofile.Info, error) {
	return demofile.Validate(r)
}
