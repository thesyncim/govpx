package govpx

func (rc *rateControlState) applyPass2CBRBufferAdjustment(targetBits int, keyFrame bool) int {
	if rc.mode != RateControlCBR || keyFrame || targetBits <= 0 || rc.bufferOptimalBits <= 0 {
		return targetBits
	}
	return rc.bufferAdjustedFrameTargetBits(targetBits)
}

// applyCQFloor ports the libvpx vp8/encoder/firstpass.c estimate_max_q
// CQ floor (`USAGE_CONSTRAINED_QUALITY -> Q = max(Q, cq_target_quality)`)
// applied AFTER the second-pass Q regulation. govpx's selectQuantizer
// regulation already clamps via `clampedCQQuantizerValue` for the
// one-pass path; this helper makes the post-regulation floor explicit so
// callers (e.g. the per-frame two-pass wiring) can re-assert the floor
// even after recode-style adjustments push currentQuantizer below
// cqLevel. Mirrors libvpx's `if (Q < cpi->cq_target_quality) Q =
// cpi->cq_target_quality` clamp.
func (rc *rateControlState) applyCQFloor() {
	if rc.mode != RateControlCQ {
		return
	}
	if rc.currentQuantizer < rc.cqLevel {
		rc.currentQuantizer = rc.cqLevel
		if rc.currentQuantizer < vp8MaxQIndex {
			rc.currentZbinOverQuant = 0
		}
	}
}

func (rc *rateControlState) bufferAdjustedFrameTargetBits(targetBits int) int {
	if targetBits <= 0 || rc.bufferOptimalBits <= 0 {
		return targetBits
	}
	onePercentBits := 1 + rc.bufferOptimalBits/100
	if onePercentBits <= 0 {
		return targetBits
	}
	target := int64(targetBits)
	switch {
	case rc.bufferLevelBits < rc.bufferOptimalBits:
		var percentLow int
		if rc.mode == RateControlCBR {
			percentLow = (rc.bufferOptimalBits - rc.bufferLevelBits) / onePercentBits
		} else {
			if rc.bufferLevelBits >= 0 || rc.totalActualBits <= 0 {
				return targetBits
			}
			percentLow = int((100 * int64(-rc.bufferLevelBits)) / rc.totalActualBits)
		}
		if percentLow > rc.undershootPct {
			percentLow = rc.undershootPct
		}
		if percentLow < 0 {
			percentLow = 0
		}
		target -= target * int64(percentLow) / 200
	case rc.bufferLevelBits > rc.bufferOptimalBits:
		var percentHigh int
		if rc.mode == RateControlCBR {
			percentHigh = (rc.bufferLevelBits - rc.bufferOptimalBits) / onePercentBits
		} else if rc.totalActualBits > 0 {
			percentHigh = int((100 * int64(rc.bufferLevelBits)) / rc.totalActualBits)
		} else {
			percentHigh = rc.overshootPct
		}
		if percentHigh > rc.overshootPct {
			percentHigh = rc.overshootPct
		}
		if percentHigh < 0 {
			percentHigh = 0
		}
		target += target * int64(percentHigh) / 200
	default:
		return targetBits
	}
	if target > int64(maxInt()) {
		return maxInt()
	}
	if target < 1 {
		return 1
	}
	return int(target)
}

func encodedSizeBits(sizeBytes int) int {
	if sizeBytes <= 0 {
		return 0
	}
	if sizeBytes > maxInt()/8 {
		return maxInt()
	}
	return sizeBytes * 8
}

func libvpxRollingBits(previous int, current int, weight int, shift uint) int {
	if previous < 0 {
		previous = 0
	}
	if current < 0 {
		current = 0
	}
	if weight <= 0 {
		return current
	}
	round := 0
	if shift > 0 {
		round = 1 << (shift - 1)
	}
	if current > maxInt()-round {
		return maxInt()
	}
	limit := (maxInt() - current - round) / weight
	if previous > limit {
		return maxInt()
	}
	value := previous*weight + current + round
	return value >> shift
}

func saturatingAdd(a int, b int) int {
	if b > 0 && a > maxInt()-b {
		return maxInt()
	}
	if b < 0 && a < -maxInt()-b {
		return -maxInt()
	}
	return a + b
}

func saturatingSub(a int, b int) int {
	if b == -maxInt() {
		return saturatingAdd(a, maxInt())
	}
	return saturatingAdd(a, -b)
}

func computeBitsPerFrame(targetBandwidthBits int, timing timingState) int {
	if targetBandwidthBits <= 0 || timing.timebaseNum <= 0 || timing.timebaseDen <= 0 || timing.frameDuration <= 0 {
		return 0
	}
	num := int64(targetBandwidthBits) * int64(timing.timebaseNum) * int64(timing.frameDuration)
	den := int64(timing.timebaseDen)
	if den <= 0 {
		return 0
	}
	v := (num + den/2) / den
	if v > int64(maxInt()) {
		return 0
	}
	return int(v)
}

func validateRateControlConfig(cfg RateControlConfig) error {
	if cfg.Mode < RateControlVBR || cfg.Mode > RateControlCQ {
		return ErrInvalidConfig
	}
	if cfg.TargetBitrateKbps <= 0 {
		return ErrInvalidBitrate
	}
	if cfg.MinBitrateKbps < 0 || cfg.MaxBitrateKbps < 0 {
		return ErrInvalidBitrate
	}
	if cfg.MinBitrateKbps > 0 && cfg.MaxBitrateKbps > 0 && cfg.MinBitrateKbps > cfg.MaxBitrateKbps {
		return ErrInvalidBitrate
	}
	if cfg.TargetBitrateKbps < cfg.MinBitrateKbps {
		return ErrInvalidBitrate
	}
	if cfg.MaxBitrateKbps > 0 && cfg.TargetBitrateKbps > cfg.MaxBitrateKbps {
		return ErrInvalidBitrate
	}
	if cfg.MinQuantizer < 0 || cfg.MaxQuantizer < 0 || cfg.MinQuantizer > maxQuantizer || cfg.MaxQuantizer > maxQuantizer {
		return ErrInvalidQuantizer
	}
	if cfg.MinQuantizer > cfg.MaxQuantizer {
		return ErrInvalidQuantizer
	}
	if cfg.CQLevel < 0 || cfg.CQLevel > maxQuantizer {
		return ErrInvalidQuantizer
	}
	cqLevel := normalizedCQLevel(cfg.CQLevel, cfg.MinQuantizer)
	if cfg.Mode == RateControlCQ && (cqLevel < cfg.MinQuantizer || cqLevel > cfg.MaxQuantizer) {
		return ErrInvalidQuantizer
	}
	if cfg.UndershootPct < 0 || cfg.OvershootPct < 0 {
		return ErrInvalidConfig
	}
	if cfg.BufferSizeMs <= 0 || cfg.BufferInitialSizeMs < 0 || cfg.BufferOptimalSizeMs < 0 {
		return ErrInvalidConfig
	}
	if cfg.MaxIntraBitratePct < 0 {
		return ErrInvalidConfig
	}
	if cfg.GFCBRBoostPct < 0 {
		return ErrInvalidConfig
	}
	return nil
}

func defaultRateControlConfig(opts EncoderOptions) RateControlConfig {
	minQ := opts.MinQuantizer
	maxQ := opts.MaxQuantizer
	if minQ == 0 && maxQ == 0 {
		minQ = 4
		maxQ = 56
	}

	undershoot := opts.UndershootPct
	if undershoot == 0 {
		undershoot = defaultRateControlUndershootPct
	}
	overshoot := opts.OvershootPct
	if overshoot == 0 {
		overshoot = defaultRateControlOvershootPct
	}

	bufferSize := opts.BufferSizeMs
	if bufferSize == 0 {
		bufferSize = libvpxDefaultBufferSizeMs
	}
	bufferInitial := opts.BufferInitialSizeMs
	if bufferInitial == 0 {
		bufferInitial = libvpxDefaultBufferInitialMs
	}
	bufferOptimal := opts.BufferOptimalSizeMs
	if bufferOptimal == 0 {
		bufferOptimal = libvpxDefaultBufferOptimalMs
	}

	return RateControlConfig{
		Mode:                opts.RateControlMode,
		TargetBitrateKbps:   opts.TargetBitrateKbps,
		MinBitrateKbps:      opts.MinBitrateKbps,
		MaxBitrateKbps:      opts.MaxBitrateKbps,
		MinQuantizer:        minQ,
		MaxQuantizer:        maxQ,
		CQLevel:             opts.CQLevel,
		UndershootPct:       undershoot,
		OvershootPct:        overshoot,
		BufferSizeMs:        bufferSize,
		BufferInitialSizeMs: bufferInitial,
		BufferOptimalSizeMs: bufferOptimal,
		DropFrameAllowed:    opts.DropFrameAllowed,
		DropFrameWaterMark:  opts.DropFrameWaterMark,
		MaxIntraBitratePct:  opts.MaxIntraBitratePct,
		GFCBRBoostPct:       opts.GFCBRBoostPct,
	}
}

func boostedFrameTargetBits(baseTargetBits int, boostPct int) int {
	if baseTargetBits <= 0 || boostPct <= 0 {
		return baseTargetBits
	}
	if boostPct > (maxInt()/baseTargetBits)-100 {
		return maxInt()
	}
	return baseTargetBits * (100 + boostPct) / 100
}

func normalizedCQLevel(level int, minQuantizer int) int {
	if level == 0 && minQuantizer > 0 {
		return defaultCQLevel
	}
	return level
}

func libvpxPublicQuantizerToQIndex(q int) int {
	if q < 0 {
		return 0
	}
	if q > maxQuantizer {
		return libvpxQuantizerTranslation[maxQuantizer]
	}
	return libvpxQuantizerTranslation[q]
}

func libvpxQIndexToPublicQuantizer(qIndex int) int {
	for q, translated := range libvpxQuantizerTranslation {
		if translated >= qIndex {
			return q
		}
	}
	return maxQuantizer
}

func normalizeRateControlPct(value int, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

func percentOf(value int, pct int) int {
	if value <= 0 || pct <= 0 {
		return 0
	}
	if value > maxInt()/pct {
		return maxInt()
	}
	return (value * pct) / 100
}

func checkedMul(a int, b int) (int, bool) {
	if a < 0 || b < 0 {
		return 0, false
	}
	if a == 0 || b == 0 {
		return 0, true
	}
	if a > maxInt()/b {
		return 0, false
	}
	return a * b, true
}

func maxInt() int {
	return int(^uint(0) >> 1)
}

// libvpxKeyFrameBoostQAdjustment ports vp8/encoder/ratectrl.c
// kf_boost_qadjustment.
