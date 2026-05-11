package govpx

import (
	"testing"
)

func makeTwoPassSpikyStats(count int) []FirstPassFrameStats {
	stats := make([]FirstPassFrameStats, count)
	for i := range stats {
		stats[i] = FirstPassFrameStats{CodedError: 1}
	}
	if len(stats) > 0 {
		stats[0].CodedError = 1000000
	}
	return stats
}

// TestLibvpxFrameMaxBitsReturnsZeroForExhaustedInputs pins the
// guards: zero/negative bits_left, frames_left, vbrmax_section, or
// av_per_frame_bandwidth all return 0.
func TestLibvpxFrameMaxBitsReturnsZeroForExhaustedInputs(t *testing.T) {
	if got := libvpxFrameMaxBitsCBR(0, 200, 5000, 5000); got != 0 {
		t.Fatalf("CBR av=0 -> %d, want 0", got)
	}
	if got := libvpxFrameMaxBitsCBR(1000, 0, 5000, 5000); got != 0 {
		t.Fatalf("CBR vbrmax=0 -> %d, want 0", got)
	}
	if got := libvpxFrameMaxBitsVBR(0, 100, 200); got != 0 {
		t.Fatalf("VBR bits_left=0 -> %d, want 0", got)
	}
	if got := libvpxFrameMaxBitsVBR(100000, 0, 200); got != 0 {
		t.Fatalf("VBR frames_left=0 -> %d, want 0", got)
	}
}

// TestLibvpxGFGroupBitsAllocatesByErrorRatio pins the libvpx
// gf_group_bits = kf_group_bits * (gf_group_err / kf_group_error_left)
// with the kf_group_bits ceiling.
func TestLibvpxGFGroupBitsAllocatesByErrorRatio(t *testing.T) {
	got := libvpxGFGroupBits(10000, 30.0, 100.0, 0, 0)
	want := int64(10000.0 * (30.0 / 100.0))
	if got != want {
		t.Fatalf("libvpxGFGroupBits = %d, want %d", got, want)
	}
}

// TestLibvpxGFGroupBitsCapsAtKFGroupBits pins the libvpx clamp:
// gf_group_bits cannot exceed kf_group_bits even if the error ratio
// exceeds 1.0.
func TestLibvpxGFGroupBitsCapsAtKFGroupBits(t *testing.T) {
	got := libvpxGFGroupBits(1000, 200.0, 100.0, 0, 0)
	if got != 1000 {
		t.Fatalf("libvpxGFGroupBits with err_ratio>1 = %d, want kf_group_bits=1000", got)
	}
}

// TestLibvpxGFGroupBitsClampsAtMaxBits pins the libvpx
// `max_bits * baseline_gf_interval` ceiling.
func TestLibvpxGFGroupBitsClampsAtMaxBits(t *testing.T) {
	got := libvpxGFGroupBits(100000, 50.0, 100.0, 1000, 8)
	// raw = 50000, max=8000.
	if got != 8000 {
		t.Fatalf("libvpxGFGroupBits with max_bits cap = %d, want 8000", got)
	}
}

// TestLibvpxGFGroupBitsReturnsZeroWhenInputsZero pins the libvpx
// `if (kf_group_bits > 0 && kf_group_error_left > 0)` gate.
func TestLibvpxGFGroupBitsReturnsZeroWhenInputsZero(t *testing.T) {
	if got := libvpxGFGroupBits(0, 50.0, 100.0, 0, 0); got != 0 {
		t.Fatalf("kf_group_bits=0 -> %d, want 0", got)
	}
	if got := libvpxGFGroupBits(1000, 50.0, 0, 0, 0); got != 0 {
		t.Fatalf("kf_group_error_left=0 -> %d, want 0", got)
	}
}

// TestLibvpxGFBitsAllocationGoldenFrameMatchesLibvpx pins the libvpx
// GF (non-ARF) allocation. With gfu_boost=200, gfq_adjustment=128,
// baseline_gf_interval=8:
//
//	Boost = (200 * 128) / 100 = 256.
//	cap = 8 * 150 = 1200; 256 < 1200; floor 125 not active.
//	allocation_chunks = 8 * 100 + (256 - 100) = 956.
//	gf_bits = int(256 * (10000/956)) = int(256 * 10.46) = 2677.
func TestLibvpxGFBitsAllocationGoldenFrameMatchesLibvpx(t *testing.T) {
	got := libvpxGFBitsAllocation(false, 200, 128, 10000, 8)
	wantF := 256.0 * (10000.0 / 956.0)
	want := int(wantF)
	if got != want {
		t.Fatalf("libvpxGFBitsAllocation GF = %d, want %d", got, want)
	}
}

// TestLibvpxGFBitsAllocationARFMatchesLibvpx pins the libvpx ARF
// allocation: Boost = (gfu_boost * 3 * gfq_adjustment) / (2 * 100) +
// interval*50. With gfu_boost=200, gfq_adjustment=128, interval=8:
//
//	Boost = (200 * 3 * 128) / 200 + 400 = 384 + 400 = 784.
//	cap = (8+1)*200 = 1800; 784 < cap; floor 125 not active.
//	allocation_chunks = (8+1)*100 + 784 = 1684.
//	gf_bits = int(784 * (10000/1684)) = int(784 * 5.937) = 4654.
func TestLibvpxGFBitsAllocationARFMatchesLibvpx(t *testing.T) {
	got := libvpxGFBitsAllocation(true, 200, 128, 10000, 8)
	wantF := 784.0 * (10000.0 / 1684.0)
	want := int(wantF)
	if got != want {
		t.Fatalf("libvpxGFBitsAllocation ARF = %d, want %d", got, want)
	}
}

// TestLibvpxGFBitsAllocationAppliesBoostFloor pins the libvpx 125
// floor on Boost.
func TestLibvpxGFBitsAllocationAppliesBoostFloor(t *testing.T) {
	// Boost = (10 * 50) / 100 = 5; floor -> 125.
	got := libvpxGFBitsAllocation(false, 10, 50, 10000, 8)
	// allocation_chunks = 800 + (125-100) = 825. gf_bits = int(125 * 10000/825).
	wantF := 125.0 * (10000.0 / 825.0)
	want := int(wantF)
	if got != want {
		t.Fatalf("libvpxGFBitsAllocation with boost floor = %d, want %d", got, want)
	}
}

// TestLibvpxGFBitsAllocationHalvesLargeBoost pins the libvpx
// `while (Boost > 1000) Boost /= 2; allocation_chunks /= 2;` overflow
// guard.
func TestLibvpxGFBitsAllocationHalvesLargeBoost(t *testing.T) {
	// gfu_boost=2000, gfq_adjustment=200 -> Boost=4000 (before clamp).
	// Cap is interval*150 = 8*150 = 1200, so Boost clamps to 1200 first
	// (libvpx applies the cap *before* the halving). After cap=1200,
	// halving runs once: Boost=600, alloc_chunks=(800+1100)/2=950.
	got := libvpxGFBitsAllocation(false, 2000, 200, 10000, 8)
	// Boost=1200 (cap), alloc=800+1100=1900. Halve: B=600, alloc=950.
	// gf_bits = int(600*10000/950).
	wantF := 600.0 * (10000.0 / 950.0)
	want := int(wantF)
	if got != want {
		t.Fatalf("libvpxGFBitsAllocation halved = %d, want %d", got, want)
	}
}

// TestTwoPassKFGroupBitsReturnsZeroWhenBitsExhausted pins the libvpx
// `if (bits_left > 0 && modified_error_left > 0.0)` gate.
func TestTwoPassKFGroupBitsReturnsZeroWhenBitsExhausted(t *testing.T) {
	stats := []FirstPassFrameStats{{IntraError: 1000, CodedError: 100, PcntInter: 0.9}}
	var ts twoPassState
	ts.configure(stats, 1000, 50, 50, 200)
	ts.bitsLeft = 0
	if got := ts.kfGroupBits(0, 1, 0); got != 0 {
		t.Fatalf("kfGroupBits with bits_left=0 = %d, want 0", got)
	}
}
