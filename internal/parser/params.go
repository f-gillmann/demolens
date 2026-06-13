package parser

// Calibration holds the tunable thresholds behind the estimated aim stats:
// spotted accuracy, counter-strafe, spray, time-to-damage, crosshair. The defaults
// are sensible values; override them via Options.Calibration from the library or
// the analyze CLI flags. Any zero field falls back to its default.
type Calibration struct {
	CrosshairConeDeg float64 // appearance cone the enemy has to reach
	TTDFovDeg        float64 // TTD: horizontal half-fov for "first saw enemy", the clock start
	TTDGapMs         float64 // TTD: enemy must stay unseen this long before a sighting resets. re-peek lockout
	TTDDebounceMs    float64 // TTD: enemy must be visible continuously this long before the clock commits, kills 1-tick grazes
	TTDFloorMs       float64 // TTD: drop samples below this. 0 keeps pre-aim
	TTDClampMs       float64 // TTD: cap kept samples here, applied after the outlier trim
	TTDOutlierFactor float64 // TTD: toss a player's samples past this multiple of their own median. trigger discipline
	CSConeDeg        float64 // counter-strafe: half-fov for "enemy in vision"
	CSRatio          float64 // counter-strafe: speed/maxspeed under which a shot is "good"
	CSRecentMs       float64 // counter-strafe + spotted: a shot still counts if the enemy was seen within this window
	SprayConeDeg     float64 // spray: half-fov for "aiming at the enemy", tighter than CS
	SprayHitWindowMs float64 // spray: pair a shot with a bullet impact inside this window. 0 = same tick
	FlashBlindScale  float64 // flash: scale demoinfocs FlashDuration to effective blind time. 1 = raw
}

// DefaultCalibration returns the tuned defaults.
func DefaultCalibration() Calibration {
	return Calibration{
		CrosshairConeDeg: 12.0,
		TTDFovDeg:        53.0,
		TTDGapMs:         1000.0,
		TTDDebounceMs:    0.0,
		TTDFloorMs:       0.0,
		TTDClampMs:       1300.0,
		TTDOutlierFactor: 2.2,
		CSConeDeg:        53.0,
		CSRatio:          0.40,
		CSRecentMs:       500.0,
		SprayConeDeg:     18.0,
		FlashBlindScale:  0.85,
	}
}

// fill any zero field from the defaults so callers only set the knobs they care about
func (c Calibration) withDefaults() Calibration {
	d := DefaultCalibration()
	if c.CrosshairConeDeg == 0 {
		c.CrosshairConeDeg = d.CrosshairConeDeg
	}
	if c.TTDFovDeg == 0 {
		c.TTDFovDeg = d.TTDFovDeg
	}
	if c.TTDGapMs == 0 {
		c.TTDGapMs = d.TTDGapMs
	}
	if c.TTDClampMs == 0 {
		c.TTDClampMs = d.TTDClampMs
	}
	if c.TTDOutlierFactor == 0 {
		c.TTDOutlierFactor = d.TTDOutlierFactor
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
