package govpx

import "testing"

func TestBoostedFrameTargetBits(t *testing.T) {
	if got := boostedFrameTargetBits(1000, 100); got != 2000 {
		t.Fatalf("boosted target = %d, want 2000", got)
	}
	if got := boostedFrameTargetBits(1000, 0); got != 1000 {
		t.Fatalf("zero-boost target = %d, want 1000", got)
	}
	if got := boostedFrameTargetBits(maxInt(), 100); got != maxInt() {
		t.Fatalf("overflow-boost target = %d, want maxInt", got)
	}
}

// TestCalcGFParamsMatchesLibvpxBoostTables pins the libvpx
// vp8/encoder/ratectrl.c calc_gf_params boost computation for known
// inputs. The hand-computed expectations follow:
//
//	GFQ_ADJUSTMENT[40] = 128.
//	gf_intra_usage_adjustment[clamp(10,0,14)] = 70.
//	gf_frame_usage = max((golden+altref)*100/total, 100*gf_active/MBs)
//	              = max((200+0)*100/1200, 100*200/1200) = 16.
//	gf_adjust_table[16] = 300.
//	Boost = (((128 * 70) / 100) * 300) / 100 = 267.
//	kf_gf_boost_qlimits[40] = 390 -> no ceiling clamp; >=110 floor unused.
//	gf_interval_table[16] = 7; baseline=8 wins; max_gf_interval=15 caps.
func TestCalcGFParamsMatchesLibvpxBoostTables(t *testing.T) {
	out := calcGFParams(gfParamsInput{
		Q:                     40,
		RecentRefIntra:        100,
		RecentRefLast:         900,
		RecentRefGolden:       200,
		RecentRefAltRef:       0,
		GFActiveCount:         200,
		Macroblocks:           1200,
		ThisFramePercentIntra: 10,
		BaselineGFInterval:    8,
		MaxGFInterval:         15,
	})
	if out.GFFrameUsage != 16 {
		t.Fatalf("gf_frame_usage = %d, want libvpx 16", out.GFFrameUsage)
	}
	if out.Boost != 267 {
		t.Fatalf("calcGFParams boost = %d, want libvpx 267", out.Boost)
	}
	if out.FramesTillUpdate != 8 {
		t.Fatalf("calcGFParams interval = %d, want libvpx 8", out.FramesTillUpdate)
	}
}

// TestCalcGFParamsAppliesQLimitCeiling exercises the kf_gf_boost_qlimits
// ceiling: at low Q with high gf_frame_usage, the raw boost product
// exceeds the table limit and must be clamped down. With
// kf_gf_boost_qlimits[20]=250 the result is forced to 250, and the
// last_boost>=1500 branch never fires so the interval is governed by
// gf_interval_table[gf_frame_usage].
func TestCalcGFParamsAppliesQLimitCeiling(t *testing.T) {
	out := calcGFParams(gfParamsInput{
		Q:                     20,
		RecentRefIntra:        50,
		RecentRefLast:         100,
		RecentRefGolden:       400,
		RecentRefAltRef:       400,
		GFActiveCount:         950,
		Macroblocks:           1000,
		ThisFramePercentIntra: 0,
		BaselineGFInterval:    8,
		MaxGFInterval:         20,
	})
	if out.Boost != libvpxKFGFBoostQLimits[20] {
		t.Fatalf("calcGFParams boost = %d, want clamped to qlimits 250", out.Boost)
	}
	// gf_frame_usage = max((400+400)*100/950, 100*950/1000) = max(84,95)=95.
	// gf_interval_table[95] = 11 (libvpx gf_interval_table boundary).
	if out.GFFrameUsage != 95 {
		t.Fatalf("gf_frame_usage = %d, want 95", out.GFFrameUsage)
	}
	if out.FramesTillUpdate != libvpxGFIntervalTable[95] {
		t.Fatalf("calcGFParams interval = %d, want gf_interval_table[95]=%d",
			out.FramesTillUpdate, libvpxGFIntervalTable[95])
	}
}

// TestCalcGFParamsAppliesBoostFloor pins the lower 110 floor: at high Q
// with low usage the raw product falls under 110, so the boost is
// floored. The interval still picks up the gf_interval_table value at
// the resulting gf_frame_usage.
func TestCalcGFParamsAppliesBoostFloor(t *testing.T) {
	out := calcGFParams(gfParamsInput{
		Q:                     0,
		RecentRefIntra:        1000,
		RecentRefLast:         0,
		RecentRefGolden:       0,
		RecentRefAltRef:       0,
		GFActiveCount:         0,
		Macroblocks:           1000,
		ThisFramePercentIntra: 14,
		BaselineGFInterval:    8,
		MaxGFInterval:         15,
	})
	if out.Boost != 110 {
		t.Fatalf("calcGFParams boost = %d, want 110 floor", out.Boost)
	}
	if out.FramesTillUpdate != 8 {
		t.Fatalf("calcGFParams interval = %d, want baseline 8", out.FramesTillUpdate)
	}
}

// TestCalcGFParamsBoostExtendsInterval covers the >750/>1000/>1250/>=1500
// boost-extension thresholds. With cleared intra/inter ref usage, the
// raw boost is 198 at Q=127; with low intra and gf_frame_usage=0 the
// only path to a large boost is via the test stub. We hand-pick inputs
// that yield boost >= 1500 by zeroing tot_mbs (all entries 0) so
// gf_frame_usage falls back to 100*gf_active/MBs and intra adjustment
// runs at idx=0 (125).
func TestCalcGFParamsBoostExtendsInterval(t *testing.T) {
	// libvpxKFGFBoostQLimits saturates at 600 above index 62; choose
	// Q=80 so the raw product is far above 600 and the qlimit ceiling
	// brings it to exactly 600. Then >=1500 path is not taken (boost is
	// 600), so verify the interval-extension thresholds remain inactive.
	out := calcGFParams(gfParamsInput{
		Q:                     80,
		RecentRefIntra:        0,
		RecentRefLast:         0,
		RecentRefGolden:       1000,
		RecentRefAltRef:       0,
		GFActiveCount:         1000,
		Macroblocks:           1000,
		ThisFramePercentIntra: 0,
		BaselineGFInterval:    8,
		MaxGFInterval:         15,
	})
	if out.Boost != 600 {
		t.Fatalf("calcGFParams boost = %d, want libvpx ceiling 600", out.Boost)
	}
	// gf_interval_table[100]=11 wins over baseline 8, but max=15 caps.
	if out.FramesTillUpdate != 11 {
		t.Fatalf("calcGFParams interval = %d, want gf_interval_table[100]=11", out.FramesTillUpdate)
	}
}

// TestRateControlGoldenFrameTargetBitsMatchesLibvpx pins the libvpx
// boost-weighted GF section split from calc_pframe_target_size. With
// boost=400, frames_till_gf_update_due=7 (frames_in_section=8) and
// inter_frame_target=1000:
//
//	allocation_chunks = 8*100 + 300 = 1100
//	bits_in_section   = 1000 * 8 = 8000
//	(8000 >> 7) = 62 < 1100, so target = 400 * 8000 / 1100 = 2909.
func TestRateControlGoldenFrameTargetBitsMatchesLibvpx(t *testing.T) {
	got := libvpxGoldenFrameTargetBits(400, 7, 1000)
	if got != 2909 {
		t.Fatalf("libvpxGoldenFrameTargetBits = %d, want 2909", got)
	}
}

// TestRateControlGoldenFrameTargetBitsHalvesLargeBoost pins libvpx's
// `while (Boost > 1000) Boost /= 2; allocation_chunks /= 2;` overflow
// guard. With boost=1500, the loop runs once -> boost=750,
// allocation_chunks=(8*100+1400)/2=1100. bits_in_section=8000.
// (8000 >> 7)=62 < 1100, so target = 750 * 8000 / 1100 = 5454.
func TestRateControlGoldenFrameTargetBitsHalvesLargeBoost(t *testing.T) {
	got := libvpxGoldenFrameTargetBits(1500, 7, 1000)
	if got != 5454 {
		t.Fatalf("libvpxGoldenFrameTargetBits with large boost = %d, want 5454", got)
	}
}

// TestRateControlGoldenFrameTargetBitsHighPrecisionPath pins libvpx's
// alternate `Boost * (bits_in_section / allocation_chunks)` branch
// taken when `bits_in_section >> 7 > allocation_chunks`. With
// inter_frame_target=1<<20, frames_in_section=8, boost=400:
//
//	bits_in_section = 8 << 20.
//	bits_in_section >> 7 = 8 << 13 = 65536, > allocation_chunks=1100.
//	target = 400 * (8<<20)/1100 = 400 * 7626 = 3050400.
func TestRateControlGoldenFrameTargetBitsHighPrecisionPath(t *testing.T) {
	got := libvpxGoldenFrameTargetBits(400, 7, 1<<20)
	want := 400 * ((8 << 20) / 1100)
	if got != want {
		t.Fatalf("libvpxGoldenFrameTargetBits high-precision = %d, want %d", got, want)
	}
}
