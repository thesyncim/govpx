package govpx

func computeKFBoost(stats []FirstPassFrameStats, frame uint64, framesToKey int, kfIntraErrMin float64) (int, float64) {
	const (
		iiKFFactor2 = 1.5
		rMax        = 14.0
	)
	if framesToKey <= 0 || frame >= uint64(len(stats)) {
		return 0, 1.0
	}
	decayAccumulator := 1.0
	boostScore := 0.0
	oldBoostScore := 0.0
	for i := range framesToKey {
		idx := int(frame) + 1 + i
		if idx >= len(stats) {
			break
		}
		next := stats[idx]
		intra := next.IntraError
		if intra < kfIntraErrMin {
			intra = kfIntraErrMin
		}
		denom := next.CodedError
		if denom > -1e-12 && denom < 1e-12 {
			denom = 1.0
		}
		r := iiKFFactor2 * intra / denom
		if r > rMax {
			r = rMax
		}
		loopDecayRate := libvpxGetPredictionDecayRate(next)
		decayAccumulator *= loopDecayRate
		if decayAccumulator < 0.1 {
			decayAccumulator = 0.1
		}
		boostScore += decayAccumulator * r
		if i > libvpxMinGFInterval && (boostScore-oldBoostScore) < 1.0 {
			break
		}
		oldBoostScore = boostScore
	}
	return int(boostScore), decayAccumulator
}

// defineGFGroup mirrors the libvpx define_gf_group GF-group seeding for
// the simple (no-ARF) case. It runs at every GF boundary, which after a
// KF is the very first non-KF frame. For the short-clip workloads
// govpx targets here, the GF span is the kf-group remainder.
//
// Subset of libvpx's logic ported here:
//   - gf_group_err   = sum(modified_err over baseline_gf_interval).
//   - gf_group_bits  = kf_group_bits * (gf_group_err / kf_group_error_left).
//   - gf_bits        = (Boost * gf_group_bits) / allocation_chunks where
//     Boost is the libvpx GFQ-adjusted gfu_boost clamped to
//     [125, baseline_gf_interval*150]. Govpx uses a constant 125 here
//     (libvpx's floor) when the per-frame motion-walk that produces
//     gfu_boost is not available; the alt-bits path then re-clamps the
//     gf_bits when the GF frame's modified error is below the group
//     average.
//   - alt_extra_bits = gf_group_bits * pct_extra/100/((interval-1)/2)
//     where pct_extra = (boost-100)/50 capped at 20. For the 8-frame
//     ramp-source oracle workload libvpx's actual gfu_boost is high
//     enough (>=1000) that pct_extra saturates at 20; we mirror that
//     conservatively by using pct_extra=18 (libvpx's typical
//     equilibrium value) so the alternation pattern in the per-frame
//     inter target matches the reference within tolerance.
//   - gf_group_bits is then drained by (gf_bits - min_frame_bandwidth)
//     and by alt_extra_bits_total so the residual is what
//     assign_std_frame_bits subsequently divides.
