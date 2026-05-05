package common

import "testing"

func TestUpdateLoopFilterSharpness(t *testing.T) {
	var lfi LoopFilterInfo
	UpdateLoopFilterSharpness(&lfi, 0)
	if lfi.Limit[10] != 10 || lfi.BLimit[10] != 30 || lfi.MBLimit[10] != 34 {
		t.Fatalf("sharpness 0 level 10 = lim %d blim %d mblim %d", lfi.Limit[10], lfi.BLimit[10], lfi.MBLimit[10])
	}
	if lfi.Limit[0] != 1 || lfi.BLimit[0] != 1 || lfi.MBLimit[0] != 5 {
		t.Fatalf("sharpness 0 level 0 = lim %d blim %d mblim %d", lfi.Limit[0], lfi.BLimit[0], lfi.MBLimit[0])
	}

	UpdateLoopFilterSharpness(&lfi, 5)
	if lfi.SharpnessLevel != 5 {
		t.Fatalf("sharpness level = %d, want 5", lfi.SharpnessLevel)
	}
	if lfi.Limit[63] != 4 || lfi.BLimit[63] != 130 || lfi.MBLimit[63] != 134 {
		t.Fatalf("sharpness 5 level 63 = lim %d blim %d mblim %d", lfi.Limit[63], lfi.BLimit[63], lfi.MBLimit[63])
	}
}

func TestInitLoopFilterInfoLUTs(t *testing.T) {
	var lfi LoopFilterInfo
	InitLoopFilterInfo(&lfi, 0)

	if lfi.HEVThreshLUT[KeyFrame][14] != 0 || lfi.HEVThreshLUT[KeyFrame][15] != 1 || lfi.HEVThreshLUT[KeyFrame][40] != 2 {
		t.Fatalf("keyframe hev LUT mismatch")
	}
	if lfi.HEVThreshLUT[InterFrame][19] != 1 || lfi.HEVThreshLUT[InterFrame][20] != 2 || lfi.HEVThreshLUT[InterFrame][40] != 3 {
		t.Fatalf("interframe hev LUT mismatch")
	}
	for i := 0; i < 4; i++ {
		if lfi.HEVThresh[i] != byte(i) {
			t.Fatalf("HEVThresh[%d] = %d", i, lfi.HEVThresh[i])
		}
	}

	if lfi.ModeLFLUT[BPred] != 0 || lfi.ModeLFLUT[DCPred] != 1 || lfi.ModeLFLUT[ZeroMV] != 1 {
		t.Fatalf("mode LF LUT intra/zero mismatch")
	}
	if lfi.ModeLFLUT[NearestMV] != 2 || lfi.ModeLFLUT[NearMV] != 2 || lfi.ModeLFLUT[NewMV] != 2 || lfi.ModeLFLUT[SplitMV] != 3 {
		t.Fatalf("mode LF LUT motion mismatch")
	}
}

func TestInitLoopFilterFrameWithoutDeltas(t *testing.T) {
	var lfi LoopFilterInfo
	InitLoopFilterFrame(&lfi, 20, LoopFilterFrameConfig{})

	for seg := 0; seg < MaxMBSegments; seg++ {
		for ref := 0; ref < int(MaxRefFrames); ref++ {
			for mode := 0; mode < MaxModeLFDeltas; mode++ {
				if got := lfi.Level[seg][ref][mode]; got != 20 {
					t.Fatalf("Level[%d][%d][%d] = %d, want 20", seg, ref, mode, got)
				}
			}
		}
	}
}

func TestInitLoopFilterFrameWithSegmentationAndDeltas(t *testing.T) {
	var lfi LoopFilterInfo
	cfg := LoopFilterFrameConfig{
		SegmentationEnabled: true,
		SegmentLF:           [MaxMBSegments]int8{0, 5, -30, 50},
		ModeRefDeltaEnabled: true,
		RefDeltas:           [MaxRefLFDeltas]int8{2, -1, 3, 4},
		ModeDeltas:          [MaxModeLFDeltas]int8{5, 6, 7, 8},
	}

	InitLoopFilterFrame(&lfi, 20, cfg)

	if got := lfi.Level[0][IntraFrame][0]; got != 27 {
		t.Fatalf("intra B_PRED level = %d, want 27", got)
	}
	if got := lfi.Level[0][IntraFrame][1]; got != 22 {
		t.Fatalf("intra non-B level = %d, want 22", got)
	}
	if got := lfi.Level[0][LastFrame][1]; got != 25 {
		t.Fatalf("last mode1 level = %d, want 25", got)
	}
	if got := lfi.Level[1][GoldenFrame][3]; got != 36 {
		t.Fatalf("segment1 golden mode3 level = %d, want 36", got)
	}
	if got := lfi.Level[2][IntraFrame][0]; got != 7 {
		t.Fatalf("segment2 clamped intra B_PRED level = %d, want 7", got)
	}
	if got := lfi.Level[3][AltRefFrame][3]; got != 63 {
		t.Fatalf("segment3 clamped alt mode3 level = %d, want 63", got)
	}
}

func TestLoopFilterInfoAllocatesZero(t *testing.T) {
	var lfi LoopFilterInfo
	cfg := LoopFilterFrameConfig{
		SegmentationEnabled: true,
		SegmentLF:           [MaxMBSegments]int8{0, 5, -30, 50},
		ModeRefDeltaEnabled: true,
		RefDeltas:           [MaxRefLFDeltas]int8{2, -1, 3, 4},
		ModeDeltas:          [MaxModeLFDeltas]int8{5, 6, 7, 8},
	}
	allocs := testing.AllocsPerRun(1000, func() {
		InitLoopFilterInfo(&lfi, 5)
		InitLoopFilterFrame(&lfi, 20, cfg)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func BenchmarkInitLoopFilterFrame(b *testing.B) {
	var lfi LoopFilterInfo
	cfg := LoopFilterFrameConfig{
		SegmentationEnabled: true,
		SegmentLF:           [MaxMBSegments]int8{0, 5, -30, 50},
		ModeRefDeltaEnabled: true,
		RefDeltas:           [MaxRefLFDeltas]int8{2, -1, 3, 4},
		ModeDeltas:          [MaxModeLFDeltas]int8{5, 6, 7, 8},
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		InitLoopFilterFrame(&lfi, 20, cfg)
	}
}
