package metrics

import "github.com/f-gillmann/demolens/model"

// clutchForSide returns the per-side clutch tally for "CT"/"T", nil otherwise.
func clutchForSide(p *model.Player, side string) *model.ClutchStats {
	switch side {
	case "CT":
		return &p.ClutchCT
	case "T":
		return &p.ClutchT
	default:
		return nil
	}
}

// openingForSide returns the per-side opening tally for "CT"/"T", nil otherwise.
func openingForSide(p *model.Player, side string) *model.OpeningStats {
	switch side {
	case "CT":
		return &p.OpeningCT
	case "T":
		return &p.OpeningT
	default:
		return nil
	}
}

// applyToSides runs fn against the overall tally and the per-side tally, skipping
// the per-side one when it is nil (an unknown side).
func applyToSides[T any](overall, side *T, fn func(*T)) {
	for _, s := range []*T{overall, side} {
		if s == nil {
			continue
		}
		fn(s)
	}
}
