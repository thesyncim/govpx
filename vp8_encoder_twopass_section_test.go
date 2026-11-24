package govpx

import (
	"testing"
)

// TestLibvpxSectionStatsAccumulatesAndAverages pins the libvpx
// FIRSTPASS_STATS accumulate_stats/avg_stats pattern: addFrame sums,
// avg divides by count.
func TestLibvpxSectionStatsAccumulatesAndAverages(t *testing.T) {
	var s libvpxSectionStats
	s.addFrame(1000, 100)
	s.addFrame(2000, 200)
	s.addFrame(3000, 300)
	s.avg()
	if s.sectionIntra != 2000 || s.sectionCoded != 200 {
		t.Fatalf("avg = (%v, %v), want (2000, 200)", s.sectionIntra, s.sectionCoded)
	}
}

// TestLibvpxSectionIntraRatingDivisionByZeroFallback pins libvpx's
// DOUBLE_DIVIDE_CHECK fallback: when sectionCoded is ~0, the helper
// substitutes 1.0 so the division does not blow up.
func TestLibvpxSectionIntraRatingDivisionByZeroFallback(t *testing.T) {
	if got := libvpxSectionIntraRating(100.0, 0.0); got != 100 {
		t.Fatalf("intra_rating with coded=0 = %d, want 100 (DOUBLE_DIVIDE_CHECK fallback)", got)
	}
}

// TestLibvpxSectionIntraRatingTruncatesToInt pins the libvpx
// (unsigned int) cast.
func TestLibvpxSectionIntraRatingTruncatesToInt(t *testing.T) {
	if got := libvpxSectionIntraRating(1500.0, 200.0); got != 7 {
		t.Fatalf("intra_rating(1500,200) = %d, want 7 (truncated 7.5)", got)
	}
}

// TestLibvpxSectionMaxQFactorMatchesLibvpxFormula pins the libvpx
// section_max_qfactor formula with the 0.80 floor:
//
//	factor = 1.0 - (Ratio - 10.0) * 0.025
//	clamp(factor, 0.80, +inf)
func TestLibvpxSectionMaxQFactorMatchesLibvpxFormula(t *testing.T) {
	// Ratio=12 -> factor = 1.0 - 2.0*0.025 = 0.95 (no floor).
	if got := libvpxSectionMaxQFactor(1200.0, 100.0); got != 0.95 {
		t.Fatalf("section_max_qfactor(ratio=12) = %v, want 0.95", got)
	}
	// Ratio=10 -> factor = 1.0 (no scaling).
	if got := libvpxSectionMaxQFactor(1000.0, 100.0); got != 1.0 {
		t.Fatalf("section_max_qfactor(ratio=10) = %v, want 1.0", got)
	}
	// Ratio=20 -> factor = 1.0 - 10*0.025 = 0.75 -> floored to 0.80.
	if got := libvpxSectionMaxQFactor(2000.0, 100.0); got != 0.80 {
		t.Fatalf("section_max_qfactor(ratio=20) = %v, want 0.80 (floored)", got)
	}
}

// TestLibvpxSectionMaxQFactorDivisionByZeroFallback pins the
// DOUBLE_DIVIDE_CHECK fallback path.
func TestLibvpxSectionMaxQFactorDivisionByZeroFallback(t *testing.T) {
	if got := libvpxSectionMaxQFactor(10.0, 0.0); got != 1.0 {
		t.Fatalf("section_max_qfactor(coded=0, intra=10) = %v, want 1.0", got)
	}
}

// TestLibvpxAssignStdFrameBitsErrorFraction pins the libvpx
// vp8/encoder/firstpass.c assign_std_frame_bits formula:
//
//	target = gf_group_bits * (modified_err / gf_group_error_left)
//	      + min_frame_bandwidth
//
// With modified_err=20, gf_group_error_left=100, gf_group_bits=10000,
// target = int(10000 * 0.2) + 0 = 2000.
func TestLibvpxAssignStdFrameBitsErrorFraction(t *testing.T) {
	got := libvpxAssignStdFrameBits(20.0, 100.0, 10000, 0, 0, 0, 0, 0)
	if got != 2000 {
		t.Fatalf("assign_std_frame_bits = %d, want 2000", got)
	}
}

// TestLibvpxAssignStdFrameBitsClampAtMaxBits pins the libvpx
// `if (target > max_bits) target = max_bits` clamp.
func TestLibvpxAssignStdFrameBitsClampAtMaxBits(t *testing.T) {
	got := libvpxAssignStdFrameBits(50.0, 100.0, 10000, 1500, 0, 0, 0, 0)
	if got != 1500 {
		t.Fatalf("assign_std_frame_bits with max_bits cap = %d, want 1500", got)
	}
}

// TestLibvpxAssignStdFrameBitsClampAtGFGroupBits pins the libvpx
// `if (target > gf_group_bits) target = gf_group_bits` clamp.
func TestLibvpxAssignStdFrameBitsClampAtGFGroupBits(t *testing.T) {
	got := libvpxAssignStdFrameBits(200.0, 100.0, 10000, 0, 0, 0, 0, 0)
	if got != 10000 {
		t.Fatalf("assign_std_frame_bits with gf_group_bits cap = %d, want 10000", got)
	}
}

// TestLibvpxAssignStdFrameBitsAddsMinFrameBandwidth pins the libvpx
// `target += min_frame_bandwidth` add.
func TestLibvpxAssignStdFrameBitsAddsMinFrameBandwidth(t *testing.T) {
	got := libvpxAssignStdFrameBits(20.0, 100.0, 10000, 0, 500, 0, 0, 0)
	if got != 2500 {
		t.Fatalf("assign_std_frame_bits with min_frame_bandwidth = %d, want 2500", got)
	}
}

// TestLibvpxAssignStdFrameBitsAltExtraOnOddFrames pins the libvpx
// `if ((frames_since_golden & 1) && frames_till_gf_update_due > 0)`
// alt_extra_bits add.
func TestLibvpxAssignStdFrameBitsAltExtraOnOddFrames(t *testing.T) {
	got := libvpxAssignStdFrameBits(20.0, 100.0, 10000, 0, 0, 1, 5, 100)
	if got != 2100 {
		t.Fatalf("assign_std_frame_bits with alt_extra (odd) = %d, want 2100", got)
	}
	got = libvpxAssignStdFrameBits(20.0, 100.0, 10000, 0, 0, 2, 5, 100)
	if got != 2000 {
		t.Fatalf("assign_std_frame_bits with alt_extra (even) = %d, want 2000", got)
	}
	got = libvpxAssignStdFrameBits(20.0, 100.0, 10000, 0, 0, 1, 0, 100)
	if got != 2000 {
		t.Fatalf("assign_std_frame_bits with alt_extra (no update due) = %d, want 2000", got)
	}
}

// TestLibvpxAssignStdFrameBitsEmptyGroupUsesMinOnly pins the libvpx
// `target_frame_size=0; target_frame_size += min_frame_bandwidth` path.
func TestLibvpxAssignStdFrameBitsEmptyGroupUsesMinOnly(t *testing.T) {
	if got := libvpxAssignStdFrameBits(20.0, 100.0, 0, 1500, 500, 0, 0, 0); got != 500 {
		t.Fatalf("assign_std_frame_bits with gf_group_bits=0 = %d, want 500", got)
	}
}

// TestLibvpxAssignStdFrameBitsZeroErrorFractionUsesMinOnly pins the
// libvpx `if (gf_group_error_left <= 0) err_fraction = 0` branch.
func TestLibvpxAssignStdFrameBitsZeroErrorFractionUsesMinOnly(t *testing.T) {
	got := libvpxAssignStdFrameBits(20.0, 0, 10000, 0, 500, 0, 0, 0)
	if got != 500 {
		t.Fatalf("assign_std_frame_bits with err_left=0 = %d, want 500", got)
	}
}

// TestLibvpxFrameMaxBitsCBRBasicAllocation pins the libvpx CBR
// branch of frame_max_bits when buffer is at optimal:
//
//	max_bits = av_per_frame_bandwidth * vbrmax / 100.
func TestLibvpxFrameMaxBitsCBRBasicAllocation(t *testing.T) {
	got := libvpxFrameMaxBitsCBR(1000, 200, 5000, 5000)
	if got != 2000 {
		t.Fatalf("frame_max_bits CBR optimal = %d, want 2000", got)
	}
}

// TestLibvpxFrameMaxBitsCBRScalesWithBufferRatio pins the libvpx
// buffer-fullness scaling: when buffer_level < optimal, max_bits is
// scaled by (buffer_level / optimal), with a floor of
// min(av_per_frame_bandwidth>>2, max_bits>>2 (pre-scale)).
func TestLibvpxFrameMaxBitsCBRScalesWithBufferRatio(t *testing.T) {
	// av=1000, vbrmax=200 -> max_bits=2000 pre-scale.
	// buffer=2500, optimal=5000 -> ratio=0.5 -> max_bits=1000.
	// min_floor = min(1000>>2=250, 2000>>2=500) = 250. 1000 > 250.
	got := libvpxFrameMaxBitsCBR(1000, 200, 2500, 5000)
	if got != 1000 {
		t.Fatalf("frame_max_bits CBR half-buffer = %d, want 1000", got)
	}
}

// TestLibvpxFrameMaxBitsCBRFloorsAtMinMaxBits pins the libvpx
// `if (max_bits < min_max_bits) max_bits = min_max_bits` floor.
func TestLibvpxFrameMaxBitsCBRFloorsAtMinMaxBits(t *testing.T) {
	// av=1000, vbrmax=200 -> max_bits=2000 pre-scale.
	// buffer=100, optimal=5000 -> ratio=0.02 -> max_bits=40.
	// min_floor = min(250, 500) = 250. 40 < 250 -> 250.
	got := libvpxFrameMaxBitsCBR(1000, 200, 100, 5000)
	if got != 250 {
		t.Fatalf("frame_max_bits CBR low-buffer floor = %d, want 250", got)
	}
}

// TestLibvpxFrameMaxBitsVBRBasicAllocation pins the libvpx VBR branch:
//
//	max_bits = (bits_left / frames_left) * vbrmax / 100.
func TestLibvpxFrameMaxBitsVBRBasicAllocation(t *testing.T) {
	// bits_left=100000, frames_left=100 -> per-frame=1000.
	// vbrmax=200 -> max_bits = int(1000 * 2.0) = 2000.
	got := libvpxFrameMaxBitsVBR(100000, 100, 200)
	if got != 2000 {
		t.Fatalf("frame_max_bits VBR = %d, want 2000", got)
	}
}

// TestTwoPassFrameTargetBitsUsesLiveVBRMaxWhenBudgetAhead pins the
// libvpx second-pass VBR ceiling:
//
//	max_bits = (bits_left / frames_left) * two_pass_vbrmax_section / 100
//
// When the encode is ahead of budget, the cap rises with bits_left
// instead of staying pinned to the initial average frame target.
