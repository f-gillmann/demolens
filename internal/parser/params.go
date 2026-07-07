package parser

// Calibration holds the tunable thresholds behind the estimated aim stats. Override
// the tuned defaults via Options.Calibration; any zero field falls back to default.
type Calibration struct {
	TTDFovDeg     float64 // TTD: narrow-los half-fov for "first saw enemy", the clock start
	TTDGapMs      float64 // TTD: enemy must stay unseen this long before a sighting resets. re-peek lockout
	TTDDebounceMs float64 // TTD: enemy must be visible continuously this long before the clock commits, kills 1-tick grazes
	TTDFloorMs    float64 // TTD: drop samples below this. 0 keeps pre-aim
	TTDExcludeMs  float64 // TTD: drop samples at or above this. trigger-discipline cutoff
	TTDPercentile float64 // TTD: reported percentile of kept samples (50=median)

	CrosshairFovDeg     float64 // crosshair: dense-los half-fov for "first saw enemy"
	CrosshairGapMs      float64 // crosshair: enemy must stay unseen this long before a sighting resets
	CrosshairDebounceMs float64 // crosshair: enemy must be visible continuously this long before the sighting commits
	CrosshairPeekGapMs  float64 // crosshair: re-anchor the appearance view when a fresh window opens after this unseen gap
	CrosshairWinsorPct  float64 // crosshair: clamp samples below this percentile up to it, then mean

	CSConeDeg        float64 // counter-strafe: half-fov for "enemy in vision"
	CSRatio          float64 // counter-strafe: speed/maxspeed under which a shot is "good"
	CSRecentMs       float64 // counter-strafe + spotted: a shot still counts if the enemy was seen within this window
	SprayConeDeg     float64 // spray: half-fov for "aiming at the enemy", tighter than CS
	SprayHitWindowMs float64 // spray: pair a shot with a bullet impact inside this window. 0 = same tick
	FlashBlindScale  float64 // flash: scale demoinfocs FlashDuration to effective blind time. 1 = raw

	// FlashBlindFraction: a shooter is treated as blind (its sighting clocks cannot
	// start) while the fraction of the current flash's duration still remaining exceeds
	// this. Proportional to the flash length, so short flashes get gated too: 0.45
	// reproduces ~1.5s on a 3.3s flash and scales down on shorter ones. 0 means blind
	// whenever any flash time remains. Used directly, NOT in withDefaults: 0 is a
	// meaningful value so the zero-means-default rule must not rewrite it.
	FlashBlindFraction float64
}

// DefaultCalibration returns the tuned defaults.
func DefaultCalibration() Calibration {
	return Calibration{
		TTDFovDeg:           50.0,
		TTDGapMs:            800.0,
		TTDDebounceMs:       0.0,
		TTDFloorMs:          165.0,
		TTDExcludeMs:        1600.0,
		TTDPercentile:       50.0,
		CrosshairFovDeg:     45.0,
		CrosshairGapMs:      1500.0,
		CrosshairDebounceMs: 120.0,
		CrosshairPeekGapMs:  250.0,
		CrosshairWinsorPct:  30.0,
		CSConeDeg:           53.0,
		CSRatio:             0.40,
		CSRecentMs:          500.0,
		SprayConeDeg:        18.0,
		FlashBlindScale:     0.85,
		FlashBlindFraction:  0.45, // blind while >45% of this flash's duration remains; the long fade tail is see-able. left out of withDefaults
	}
}

// fill any zero field from the defaults so callers only set the knobs they care about
func (c Calibration) withDefaults() Calibration {
	d := DefaultCalibration()
	if c.TTDFovDeg == 0 {
		c.TTDFovDeg = d.TTDFovDeg
	}
	if c.TTDGapMs == 0 {
		c.TTDGapMs = d.TTDGapMs
	}
	if c.TTDDebounceMs == 0 {
		c.TTDDebounceMs = d.TTDDebounceMs
	}
	if c.TTDFloorMs == 0 {
		c.TTDFloorMs = d.TTDFloorMs
	}
	if c.TTDExcludeMs == 0 {
		c.TTDExcludeMs = d.TTDExcludeMs
	}
	if c.TTDPercentile == 0 {
		c.TTDPercentile = d.TTDPercentile
	}
	if c.CrosshairFovDeg == 0 {
		c.CrosshairFovDeg = d.CrosshairFovDeg
	}
	if c.CrosshairGapMs == 0 {
		c.CrosshairGapMs = d.CrosshairGapMs
	}
	if c.CrosshairDebounceMs == 0 {
		c.CrosshairDebounceMs = d.CrosshairDebounceMs
	}
	if c.CrosshairPeekGapMs == 0 {
		c.CrosshairPeekGapMs = d.CrosshairPeekGapMs
	}
	if c.CrosshairWinsorPct == 0 {
		c.CrosshairWinsorPct = d.CrosshairWinsorPct
	}
	if c.CSConeDeg == 0 {
		c.CSConeDeg = d.CSConeDeg
	}
	if c.CSRatio == 0 {
		c.CSRatio = d.CSRatio
	}
	if c.SprayConeDeg == 0 {
		c.SprayConeDeg = d.SprayConeDeg
	}
	if c.FlashBlindScale == 0 {
		c.FlashBlindScale = d.FlashBlindScale
	}
	return c
}
