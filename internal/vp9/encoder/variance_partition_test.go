package encoder

import (
	"testing"
)

func TestVP9ChromaCheckMarksOnlyPlanesAboveThreshold(t *testing.T) {
	got := ChromaCheck(ChromaCheckArgs{
		YSAD:  100,
		UVSAD: [2]uint64{26, 25},
		Speed: 8,
		Width: 64, Height: 64,
		VariancePartThreshMult: 1,
	})
	if !got[0] {
		t.Fatal("U sensitivity = false, want true when uv_sad > y_sad >> 2")
	}
	if got[1] {
		t.Fatal("V sensitivity = true, want false when uv_sad == y_sad >> 2")
	}
}

func TestVP9ChromaCheckSpeedAboveEightCanSkipHighLumaSad(t *testing.T) {
	got := ChromaCheck(ChromaCheckArgs{
		YSAD:  1 << 30,
		UVSAD: [2]uint64{1 << 29, 1 << 29},
		Speed: 9,
		Width: 128, Height: 64,
		BaseQIndex:             37,
		VariancePartThreshMult: 1,
	})
	if got[0] || got[1] {
		t.Fatalf("sensitivity = %v, want both false when speed>8 high y_sad skips chroma_check", got)
	}
}

// TestVP9SetVBPThresholdsKeyframe verifies the libvpx set_vbp_thresholds
// keyframe branch (vp9/encoder/vp9_encodeframe.c:582-586) against hand-checked
// constants. The formula is:
//
//	threshold_base = 20 * y_dequant[q][1]
//	thresholds[0] = threshold_base
//	thresholds[1] = threshold_base >> 2
//	thresholds[2] = threshold_base >> 2
//	thresholds[3] = threshold_base << 2
func TestVP9SetVBPThresholdsKeyframe(t *testing.T) {
	// qindex 37 is govpx's default base qindex (vp9DefaultBaseQIndex);
	// AcQLookup8[37] is the libvpx-verbatim AC dequant table entry.
	const q = 37
	ydq := yDequantAC(q)
	if ydq <= 0 {
		t.Fatalf("yDequantAC(%d) = %d, want > 0", q, ydq)
	}
	want0 := int64(20) * int64(ydq)
	got := setVBPThresholds(q, 1, 8, 64, 64,
		true, ContentStateInvalid, false, NoiseLevelLow, 0, false)
	if got[0] != want0 {
		t.Errorf("thresholds[0] = %d, want %d", got[0], want0)
	}
	if got[1] != want0>>2 {
		t.Errorf("thresholds[1] = %d, want %d", got[1], want0>>2)
	}
	if got[2] != want0>>2 {
		t.Errorf("thresholds[2] = %d, want %d", got[2], want0>>2)
	}
	if got[3] != want0<<2 {
		t.Errorf("thresholds[3] = %d, want %d", got[3], want0<<2)
	}
}

// TestVP9SetVBPThresholdsInterLowRes verifies the inter low-res branch
// (vp9/encoder/vp9_encodeframe.c:618-625) for the 64x64 fixture used in
// FuzzVP9EncoderReferenceControlSequences seeds. With speed=8, width=64,
// height=64, content_state=invalid, no noise:
//
//	threshold_base = 1 * y_dequant[q][1]
//	threshold_base = (5 * threshold_base) >> 2  (scale_part_thresh_sumdiff
//	                                              speed>=8 && low_res)
//	thresholds[0] = threshold_base >> 3
//	thresholds[1] = threshold_base >> 1
//	thresholds[2] = threshold_base << 3
//	(no avg_frame_qindex_inter scaling since rc state is zero in the test)
func TestVP9SetVBPThresholdsInterLowRes(t *testing.T) {
	const q = 37
	ydq := int64(yDequantAC(q))
	base := ydq
	base = (5 * base) >> 2
	want0 := base >> 3
	want1 := base >> 1
	want2 := base << 3

	got := setVBPThresholds(q, 1, 8, 64, 64,
		false, ContentStateInvalid, false, NoiseLevelLow, 0, false)
	if got[0] != want0 {
		t.Errorf("thresholds[0] = %d, want %d", got[0], want0)
	}
	if got[1] != want1 {
		t.Errorf("thresholds[1] = %d, want %d", got[1], want1)
	}
	if got[2] != want2 {
		t.Errorf("thresholds[2] = %d, want %d", got[2], want2)
	}
}

// TestVP9SetVBPThresholdsInterLowResHighQ exercises the avg_frame_qindex_inter
// scaling (vp9/encoder/vp9_encodeframe.c:622-625).
func TestVP9SetVBPThresholdsInterLowResHighQ(t *testing.T) {
	const q = 37
	ydq := int64(yDequantAC(q))
	base := (5 * ydq) >> 2
	wantT2OverQHigh := (base << 3) << 2 // > 220
	wantT2OverQMid := (base << 3) << 1  // > 200

	gotHigh := setVBPThresholds(q, 1, 8, 64, 64,
		false, ContentStateInvalid, false, NoiseLevelLow, 221, false)
	if gotHigh[2] != wantT2OverQHigh {
		t.Errorf("threshold[2] (q>220) = %d, want %d",
			gotHigh[2], wantT2OverQHigh)
	}

	gotMid := setVBPThresholds(q, 1, 8, 64, 64,
		false, ContentStateInvalid, false, NoiseLevelLow, 201, false)
	if gotMid[2] != wantT2OverQMid {
		t.Errorf("threshold[2] (q>200) = %d, want %d",
			gotMid[2], wantT2OverQMid)
	}
}

// TestVP9SetVariancePartitionAuxThresholdsKeyframe verifies the libvpx
// vp9_set_variance_partition_thresholds keyframe block
// (vp9/encoder/vp9_encodeframe.c:648-651, :674).
func TestVP9SetVariancePartitionAuxThresholdsKeyframe(t *testing.T) {
	const q = 37
	aux := setVariancePartitionAuxThresholds(q, 64, 64, true, false)
	if aux.ThresholdSAD != 0 {
		t.Errorf("ThresholdSAD = %d, want 0", aux.ThresholdSAD)
	}
	if aux.ThresholdCopy != 0 {
		t.Errorf("ThresholdCopy = %d, want 0", aux.ThresholdCopy)
	}
	if !aux.BsizeMin8x8 {
		t.Errorf("BsizeMin8x8 = false, want true (BLOCK_8X8)")
	}
	if aux.ThresholdMinmax != int64(15+(q>>3)) {
		t.Errorf("ThresholdMinmax = %d, want %d",
			aux.ThresholdMinmax, 15+(q>>3))
	}
}

// TestVP9SetVariancePartitionAuxThresholdsInterLowRes verifies the inter
// low-res branch (vp9/encoder/vp9_encodeframe.c:653-654, :659-661, :674).
func TestVP9SetVariancePartitionAuxThresholdsInterLowRes(t *testing.T) {
	const q = 37
	aux := setVariancePartitionAuxThresholds(q, 64, 64, false, false)
	if aux.ThresholdSAD != 10 {
		t.Errorf("ThresholdSAD = %d, want 10", aux.ThresholdSAD)
	}
	if aux.ThresholdCopy != 4000 {
		t.Errorf("ThresholdCopy = %d, want 4000", aux.ThresholdCopy)
	}
	if aux.BsizeMin8x8 {
		t.Errorf("BsizeMin8x8 = true, want false (BLOCK_16X16)")
	}
	if aux.ThresholdMinmax != int64(15+(q>>3)) {
		t.Errorf("ThresholdMinmax = %d, want %d",
			aux.ThresholdMinmax, 15+(q>>3))
	}
}

// TestVP9SetVariancePartitionAuxThresholdsInterHighSourceSAD verifies the
// high_source_sad reset path (vp9/encoder/vp9_encodeframe.c:668-672).
func TestVP9SetVariancePartitionAuxThresholdsInterHighSourceSAD(t *testing.T) {
	const q = 37
	aux := setVariancePartitionAuxThresholds(q, 64, 64, false, true)
	if aux.ThresholdSAD != 0 {
		t.Errorf("ThresholdSAD = %d, want 0 (high_source_sad reset)",
			aux.ThresholdSAD)
	}
	if aux.ThresholdCopy != 0 {
		t.Errorf("ThresholdCopy = %d, want 0 (high_source_sad reset)",
			aux.ThresholdCopy)
	}
}

// TestVP9SetVBPThresholdsDisable16x16PartNonkey verifies the
// disable_16x16part_nonkey override (vp9/encoder/vp9_encodeframe.c:633).
func TestVP9SetVBPThresholdsDisable16x16PartNonkey(t *testing.T) {
	const q = 37
	got := setVBPThresholds(q, 1, 8, 64, 64,
		false, ContentStateInvalid, false, NoiseLevelLow, 0, true)
	if got[2] != vbpThresholdMax {
		t.Errorf("thresholds[2] = %d, want INT64_MAX (disable_16x16part_nonkey)",
			got[2])
	}
}
