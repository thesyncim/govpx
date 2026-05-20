package govpx

// clampBitrateKbps applies the libvpx VP9 rc_min_bitrate / rc_max_bitrate
// bounds to a requested kbps update. Zero bounds disable that side of the
// clamp. The returned value is guaranteed to be > 0 when kbps is > 0.
func (rc *vp9RateControlState) clampBitrateKbps(kbps int) int {
	if rc == nil || kbps <= 0 {
		return kbps
	}
	if rc.minBitrateKbps > 0 && kbps < rc.minBitrateKbps {
		kbps = rc.minBitrateKbps
	}
	if rc.maxBitrateKbps > 0 && kbps > rc.maxBitrateKbps {
		kbps = rc.maxBitrateKbps
	}
	return kbps
}

// applyVP9UndershootBound caps a per-frame target from below by
// undershoot_pct% of the per-frame bandwidth, matching libvpx VP9's
// rc_undershoot_pct adjustment.
func (rc *vp9RateControlState) applyVP9UndershootBound(target int) int {
	if rc == nil || rc.bitsPerFrame <= 0 || rc.undershootPct == 0 {
		return target
	}
	floor := percentOf(rc.bitsPerFrame, int(rc.undershootPct))
	if floor > 0 && target < floor {
		return floor
	}
	return target
}

// applyVP9OvershootBound caps a per-frame target from above by
// overshoot_pct% of the per-frame bandwidth, matching libvpx VP9's
// rc_overshoot_pct adjustment.
func (rc *vp9RateControlState) applyVP9OvershootBound(target int) int {
	if rc == nil || rc.bitsPerFrame <= 0 || rc.overshootPct == 0 {
		return target
	}
	ceil := vp9OvershootCeil(rc.bitsPerFrame, int(rc.overshootPct))
	if ceil > 0 && target > ceil {
		return ceil
	}
	return target
}

// applyVP9MaxIntraBound caps a key-frame target by max_intra_bitrate_pct%
// of the per-frame bandwidth when configured. Mirrors libvpx's
// rc_max_intra_bitrate_pct.
func (rc *vp9RateControlState) applyVP9MaxIntraBound(target int) int {
	if rc == nil || rc.bitsPerFrame <= 0 || rc.maxIntraBitratePct <= 0 {
		return target
	}
	cap := percentOf(rc.bitsPerFrame, rc.maxIntraBitratePct)
	if cap > 0 && target > cap {
		return cap
	}
	return target
}

// applyVP9MaxInterBound caps an inter-frame target by
// max_inter_bitrate_pct% of the per-frame bandwidth when configured.
// Mirrors libvpx's VP9E_SET_MAX_INTER_BITRATE_PCT control.
func (rc *vp9RateControlState) applyVP9MaxInterBound(target int) int {
	if rc == nil || rc.bitsPerFrame <= 0 || rc.maxInterBitratePct <= 0 {
		return target
	}
	cap := percentOf(rc.bitsPerFrame, rc.maxInterBitratePct)
	if cap > 0 && target > cap {
		return cap
	}
	return target
}

// vp9OvershootCeil computes the per-frame ceiling used by
// applyVP9OvershootBound. Public for parity tests.
func vp9OvershootCeil(bitsPerFrame, overshootPct int) int {
	if bitsPerFrame <= 0 || overshootPct <= 0 {
		return 0
	}
	ceil := percentOf(bitsPerFrame, 100+overshootPct)
	if ceil < bitsPerFrame {
		return bitsPerFrame
	}
	return ceil
}
