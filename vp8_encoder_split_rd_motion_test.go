package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
	"testing"
)

func TestSelectInterFrameSplitMotionDecisionRDAccountsForChromaResidual(t *testing.T) {
	const w, h = 32, 32
	src := testImage(w, h)
	fillImage(src, 0, 128, 128)
	ref := testVP8Frame(t, w, h, 0, 128, 128)
	// Vary luma so the partitioned MV search has a unique optimum, and vary
	// chroma so the derived 8x8 chroma MVs leave non-trivial UV residual
	// (rd_inter4x4_uv only contributes when vp8_mbuverror is non-zero).
	for row := range h {
		for col := range w {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((row*row*11 + col*col*23 + row*col*5 + 7) & 255)
		}
	}
	uvWidth := (w + 1) >> 1
	uvHeight := (h + 1) >> 1
	for row := range uvHeight {
		for col := range uvWidth {
			ref.Img.U[row*ref.Img.UStride+col] = byte((row*19 ^ col*13) & 255)
			ref.Img.V[row*ref.Img.VStride+col] = byte((row*7 + col*29 + 41) & 255)
		}
	}
	// Top 16x8 luma half: shift dx=1 (MV col=+8 in 1/8-pel units, MV(0,1)).
	// Bottom 16x8 luma half: identity (MV(0,0)). Apply a strong DC offset
	// to the source so the forward DCT lands above the inter zbin and the
	// per-block EOBs are populated — this is what we are asserting.
	copyShiftedBlockFromReference(src, &ref.Img, 0, 0, 16, 8, 0, 1)
	copyShiftedBlockFromReference(src, &ref.Img, 8, 0, 16, 8, 0, 0)
	// Drop the source by a per-4x4-block DC offset so the forward DCT
	// concentrates energy at the DC coefficient that survives the inter
	// zbin and leaves a populated EOB on each block.
	for row := range h {
		for col := range w {
			block := (row>>2)*4 + (col >> 2)
			delta := 60
			if block&1 == 0 {
				delta = -60
			}
			pixel := int(src.Y[row*src.YStride+col]) + delta
			if pixel < 0 {
				pixel = 0
			} else if pixel > 255 {
				pixel = 255
			}
			src.Y[row*src.YStride+col] = byte(pixel)
		}
	}
	// Match chroma so the test only depends on encoder-derived UV MVs and
	// the sixtap/bilinear residual from chroma sub-pel filtering.
	for row := range uvHeight {
		for col := range uvWidth {
			src.U[row*src.UStride+col] = ref.Img.U[row*ref.Img.UStride+col]
			src.V[row*src.VStride+col] = ref.Img.V[row*ref.Img.VStride+col]
		}
	}
	ref.ExtendBorders()

	var pred vp8common.FrameBuffer
	if err := pred.Resize(w, h, 32, 32); err != nil {
		t.Fatalf("pred.Resize: %v", err)
	}

	const splitRDQIndex = testInterSearchQIndex
	var (
		dequantTables vp8common.FrameDequantTables
		dequant       vp8common.MacroblockDequant
		quant         vp8enc.MacroblockQuant
	)
	vp8common.BuildFrameDequantTables(vp8common.QuantDeltas{}, &dequantTables)
	vp8common.InitMacroblockDequant(&dequantTables, splitRDQIndex, &dequant)
	vp8enc.InitRegularMacroblockQuant(splitRDQIndex, &dequant, &quant)
	decision, ok := selectInterFrameSplitMotionDecisionRD(
		sourceImageFromPublic(src), &ref.Img, vp8common.LastFrame,
		0, 0, vp8enc.MotionVector{}, splitRDQIndex, 0,
		&quant, nil, nil, &vp8tables.DefaultCoefProbs, &pred.Img,
		0, false, true,
	)
	if !ok {
		t.Fatalf("selectInterFrameSplitMotionDecisionRD returned false")
	}
	if decision.Mode.Mode != vp8common.SplitMV || decision.Mode.Partition != 0 {
		t.Fatalf("decision.Mode = %+v, want SPLITMV partition 0", decision.Mode)
	}
	if decision.Mode.BlockMV[0] == decision.Mode.BlockMV[8] {
		t.Fatalf("expected distinct top/bottom MVs, got %+v / %+v", decision.Mode.BlockMV[0], decision.Mode.BlockMV[8])
	}

	// Per-4x4 luma EOB storage: at least one block in the moving top half
	// quantises to non-zero coefficients. Without this storage the SPLITMV
	// packet writer would have to re-quantise to recover the EOBs.
	nonZeroLumaEOBs := 0
	for block := range 16 {
		if decision.LumaEOB(block) > 0 {
			nonZeroLumaEOBs++
		}
	}
	if nonZeroLumaEOBs == 0 {
		var snap [16]int
		for i := range 16 {
			snap[i] = decision.LumaEOB(i)
		}
		t.Fatalf("expected at least one populated luma EOB, got %v", snap)
	}

	// UV rate must be non-zero: the chroma 8x8 MVs derived from the luma
	// partition (MV col=+4 half-pel for the top half, zero for the bottom
	// half) leave residual through the chroma sub-pixel filter taps. Prior
	// to this change selectInterFrameSplitMotionMode returned only the Y
	// mode and the SPLITMV RD score never charged any UV rate.
	if decision.UVRate <= 0 {
		t.Fatalf("expected non-zero UV rate, got %d (uv dist=%d)", decision.UVRate, decision.UVDist)
	}
	if decision.YRate <= 0 {
		t.Fatalf("expected non-zero Y rate, got %d", decision.YRate)
	}
}

// TestSelectInterFrameSplitMotionLabelLevelTrials mirrors
// rd_check_segment's per-label NEAREST/NEAR/ZERO/NEW exploration.
// The macroblock is split as BLOCK_16X8 (partition 0): the top 16x8 half
// is identical to the reference (best mode is ZERO4X4 with MV=0) and the
// bottom 16x8 half is shifted by a non-trivial MV that the per-label
// NEW4X4 motion search must locate. The two labels therefore commit to
// different sub-MV modes — exactly the ZERO+NEW combo libvpx's
// rd_check_segment supports — and the picker must explore both so that
// neither label is forced into a single shared mode.

func TestSelectInterFrameSplitMotionLabelLevelTrials(t *testing.T) {
	const w, h = 32, 32
	src := testImage(w, h)
	fillImage(src, 0, 128, 128)
	ref := testVP8Frame(t, w, h, 0, 128, 128)
	for row := range h {
		for col := range w {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((row*row*13 + col*col*29 + row*col*5 + 11) & 255)
		}
	}
	// Top 16x8 half: identity copy from ref (subset 0 wants MV=0, which
	// the ZERO4X4 label trial covers without running a motion search).
	copyShiftedBlockFromReference(src, &ref.Img, 0, 0, 16, 8, 0, 0)
	// Bottom 16x8 half: shift by (dy=2, dx=3) — this MV is not zero, not
	// the LEFT4X4 predictor (MV from subset 0 = 0), and not the ABOVE4X4
	// predictor (no above-MB MV in this test). The NEW4X4 motion search
	// must locate it.
	copyShiftedBlockFromReference(src, &ref.Img, 8, 0, 16, 8, 2, 3)
	ref.ExtendBorders()

	mode, ok := selectInterFrameSplitMotionMode(sourceImageFromPublic(src), &ref.Img, vp8common.LastFrame, 0, 0, vp8enc.MotionVector{}, testInterSearchQIndex, 0)
	if !ok {
		t.Fatalf("selectInterFrameSplitMotionMode returned ok=false")
	}
	if mode.Partition != 0 {
		t.Fatalf("partition = %d, want 0 (16x8)", mode.Partition)
	}

	// Subset 0: blocks 0..7 — wants MV=0. Either LEFT4X4 (which inherits
	// from bestRefMV=0 in the absence of an in-MB left predictor) or
	// ZERO4X4 satisfies the label-trial result; libvpx prefers LEFT4X4
	// when its MV equals ZERO4X4's per labels2mode's tie-breaking. We
	// only assert the chosen MV is zero — the per-label loop must have
	// considered both LEFT/ABOVE/ZERO trials on top of NEW.
	topMV := mode.BlockMV[0]
	topBMode := mode.BModes[0]
	if topMV != (vp8enc.MotionVector{}) {
		t.Fatalf("subset 0 (top) MV = %+v, want zero (LEFT4X4/ZERO4X4 label trial)", topMV)
	}
	if topBMode != vp8common.Zero4x4 && topBMode != vp8common.Left4x4 {
		t.Fatalf("subset 0 (top) BMode = %v, want Zero4x4 or Left4x4", topBMode)
	}

	// Subset 1: blocks 8..15 — wants NEW4X4 with MV=(16,24) in 1/8-pel
	// units, which is the (dy=2,dx=3) full-pel shift the NEW search
	// finds.
	bottomMV := mode.BlockMV[8]
	bottomBMode := mode.BModes[8]
	if bottomMV == (vp8enc.MotionVector{}) {
		t.Fatalf("subset 1 (bottom) MV = zero, want NEW4X4 search to find shift")
	}
	if bottomBMode != vp8common.New4x4 {
		t.Fatalf("subset 1 (bottom) BMode = %v, want New4x4 (label trial chose NEW)", bottomBMode)
	}
	if bottomMV != (vp8enc.MotionVector{Row: 16, Col: 24}) {
		t.Fatalf("subset 1 (bottom) MV = %+v, want {Row:16, Col:24} for (dy=2, dx=3) full-pel shift", bottomMV)
	}
}

func TestSelectInterFrameSplitMotionSkipsOutOfRangeInheritedLabelMV(t *testing.T) {
	const w, h = 64, 64
	src := testImage(w, h)
	fillImage(src, 0, 128, 128)
	ref := testVP8Frame(t, w, h, 0, 128, 128)
	for row := range h {
		for col := range w {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((row*17 + col*29 + row*col*3 + 11) & 255)
		}
	}
	ref.ExtendBorders()

	invalidAbove := vp8enc.MotionVector{Row: 142, Col: -352}
	if vp8enc.InterFrameUMVFullPixelInRange(invalidAbove, 3, 3, 4, 4) {
		t.Fatalf("test MV unexpectedly in UMV range: %+v", invalidAbove)
	}
	var pred [16]byte
	if !predictSplitMotionBlock4x4(&ref.Img, 3, 3, 3, invalidAbove, &pred) {
		t.Fatalf("predictSplitMotionBlock4x4 returned false")
	}
	baseY := 3 * 16
	baseX := 3*16 + 12
	for row := range 4 {
		copy(src.Y[(baseY+row)*src.YStride+baseX:], pred[row*4:row*4+4])
	}

	mode := vp8enc.InterFrameMacroblockMode{
		RefFrame:  vp8common.LastFrame,
		Mode:      vp8common.SplitMV,
		Partition: 3,
	}
	mode.BlockMV[2] = vp8enc.MotionVector{Row: 8, Col: 16}
	above := vp8enc.InterFrameMacroblockMode{
		RefFrame:  vp8common.LastFrame,
		Mode:      vp8common.SplitMV,
		Partition: 3,
	}
	above.BlockMV[15] = invalidAbove

	mv, bMode := selectInterFrameSplitSubsetMotionModeWithSearchThresholdAndLabelRD(
		sourceImageFromPublic(src), &ref.Img, 3, 3,
		&mode, 3, 4, 4,
		vp8enc.MotionVector{}, vp8enc.MotionVector{}, 0, false,
		testInterSearchQIndex, nil, &above, defaultInterAnalysisSearchConfig(),
		&vp8tables.DefaultMVContext, 1<<30, nil, nil, nil,
	)
	if bMode == vp8common.Above4x4 || mv == invalidAbove {
		t.Fatalf("selected out-of-range inherited ABOVE label: mode=%v mv=%+v", bMode, mv)
	}
}

// TestSelectInterFrameSplitMotionTHRNEWGatingSkipsSearch covers libvpx
// rd_check_segment's NEW4X4 gate:
//
//	if (best_label_rd < label_mv_thresh) break;
//
// where label_mv_thresh = bsi->mvthresh / label_count and bsi->mvthresh
// is x->rd_threshes[THR_NEW{1,2,3}]. With the gate disabled (mvthresh
// == 0) the picker locates the (dy=2, dx=3) motion via NEW4X4. With
// mvthresh set high enough that label_mv_thresh exceeds the running
// best label cost on every label, the NEW4X4 search is skipped and the
// per-label picker falls back to LEFT4X4/ABOVE4X4/ZERO4X4 only — so
// neither label commits to a non-zero NEW vector.

func TestSelectInterFrameSplitMotionTHRNEWGatingSkipsSearch(t *testing.T) {
	const w, h = 32, 32
	src := testImage(w, h)
	fillImage(src, 0, 128, 128)
	ref := testVP8Frame(t, w, h, 0, 128, 128)
	for row := range h {
		for col := range w {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((row*row*13 + col*col*29 + row*col*5 + 11) & 255)
		}
	}
	copyShiftedBlockFromReference(src, &ref.Img, 0, 0, 16, 8, 0, 0)
	copyShiftedBlockFromReference(src, &ref.Img, 8, 0, 16, 8, 2, 3)
	ref.ExtendBorders()

	source := sourceImageFromPublic(src)
	// Sanity check: with the gate disabled, the picker still finds the
	// NEW vector. (Same setup as TestSelectInterFrameSplitMotionLabelLevelTrials.)
	open, ok := selectInterFrameSplitMotionModeWithSearchAndThreshold(
		source, &ref.Img, vp8common.LastFrame, 0, 0, vp8enc.MotionVector{},
		testInterSearchQIndex, 0,
		nil, nil, defaultInterAnalysisSearchConfig(), 0, nil,
		&vp8tables.DefaultMVContext, 0,
	)
	if !ok {
		t.Fatalf("ungated picker returned ok=false")
	}
	if open.BModes[8] != vp8common.New4x4 {
		t.Fatalf("ungated bottom BMode = %v, want New4x4", open.BModes[8])
	}

	// Now set mvthresh so high that label_mv_thresh = mvthresh/label_count
	// exceeds the running best label cost (SAD + sub-MV-rate) on every
	// label trial — this fires the gate and the NEW4X4 motion search is
	// skipped. The picker still has to commit non-NEW labels for both
	// subsets, so we just verify (a) the gated picker returned ok and
	// (b) no subset committed NEW4X4. To keep the partition non-trivial
	// (the picker rejects all-equal-MV returns), we supply a SplitMV
	// left-MB whose right-edge per-4x4 MVs differ between block 3
	// (subset 0's left predictor) and block 11 (subset 1's left
	// predictor). With NEW gated, subset 0 then commits LEFT4X4 with
	// the upper-half left-edge MV, and subset 1 commits LEFT4X4 with
	// the lower-half left-edge MV — both non-NEW, both distinct.
	left := vp8enc.InterFrameMacroblockMode{
		RefFrame:  vp8common.LastFrame,
		Mode:      vp8common.SplitMV,
		Partition: 0,
	}
	for block := range 16 {
		left.BModes[block] = vp8common.Left4x4
		if block < 8 {
			left.BlockMV[block] = vp8enc.MotionVector{Row: 8, Col: 0}
		} else {
			left.BlockMV[block] = vp8enc.MotionVector{Row: 0, Col: 8}
		}
	}
	left.MV = left.BlockMV[15]
	const huge = 1 << 30
	gated, ok := selectInterFrameSplitMotionModeWithSearchAndThreshold(
		source, &ref.Img, vp8common.LastFrame, 0, 0, vp8enc.MotionVector{},
		testInterSearchQIndex, 0,
		&left, nil, defaultInterAnalysisSearchConfig(), 0, nil,
		&vp8tables.DefaultMVContext, huge,
	)
	if !ok {
		// The gated picker rejects returns where every subset has the
		// same MV. With a SplitMV left-MB that has distinct per-half
		// right-edge MVs, the LEFT4X4 trials for subset 0 and subset
		// 1 differ, so the gated picker should return a valid SPLITMV.
		t.Fatalf("gated picker returned ok=false (synthetic left-MB SplitMV did not break label symmetry)")
	}
	for block := range 16 {
		if gated.BModes[block] == vp8common.New4x4 {
			t.Fatalf("block %d BMode = New4x4 with gate fired (mvthresh=%d), want non-NEW (LEFT/ABOVE/ZERO)", block, huge)
		}
	}
}

// TestSelectInterFrameSplitMotionOtherCostBreakdown asserts the
// rate-decomposition invariant from update_best_mode in
// vp8_rd_pick_inter_mode:
//
//	rd.rate2 = rd.rate_y (label tree + sub-MV-mode + MV cost) +
//	           rd.rate_uv (rd_inter4x4_uv) +
//	           other_cost (default no-skip / skip backout) +
//	           x->ref_frame_cost[ref_frame]
//
// The govpx port plumbs all four contributors through
// interSplitMVRDDecision so callers can verify the breakdown without
// rerunning the picker. This test sets explicit otherCost / refCost
// values and asserts decision.TotalRate sums to YRate + UVRate +
// OtherCost + RefCost, mirroring update_best_mode's accounting.
