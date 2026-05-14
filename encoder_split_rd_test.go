package govpx

import (
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

// TestSelectInterFrameSplitMotionDecisionRDAccountsForChromaResidual exercises
// the SPLITMV RD picker after the Y partition is committed. libvpx's
// vp8_rd_pick_inter_mode SPLITMV branch invokes rd_inter4x4_uv to add the
// chroma rate/distortion on top of vp8_rd_pick_best_mbsegmentation's Y RD;
// this test asserts our port does the same and stores per-4x4-block luma
// EOBs so packet writers can reuse the chosen partition's coefficients.
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
	if interFrameUMVFullPixelInRange(invalidAbove, 3, 3, 4, 4) {
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
func TestSelectInterFrameSplitMotionOtherCostBreakdown(t *testing.T) {
	const w, h = 32, 32
	src := testImage(w, h)
	fillImage(src, 0, 128, 128)
	ref := testVP8Frame(t, w, h, 0, 128, 128)
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
	// Use the same shape as the existing chroma residual test so the
	// picker commits a non-trivial SPLITMV decision with both Y and UV
	// rate populated.
	copyShiftedBlockFromReference(src, &ref.Img, 0, 0, 16, 8, 0, 1)
	copyShiftedBlockFromReference(src, &ref.Img, 8, 0, 16, 8, 0, 0)
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

	const otherCost = 40
	const refCost = 90
	decision, ok := selectInterFrameSplitMotionDecisionRDWithThreshold(
		sourceImageFromPublic(src), &ref.Img, vp8common.LastFrame,
		0, 0, vp8enc.MotionVector{}, splitRDQIndex, 0,
		&quant, nil, nil, &vp8tables.DefaultCoefProbs, &pred.Img,
		0, false, true,
		0, otherCost, refCost,
	)
	if !ok {
		t.Fatalf("selectInterFrameSplitMotionDecisionRDWithThreshold returned false")
	}
	if decision.OtherCost != otherCost {
		t.Fatalf("OtherCost = %d, want %d", decision.OtherCost, otherCost)
	}
	if decision.RefCost != refCost {
		t.Fatalf("RefCost = %d, want %d", decision.RefCost, refCost)
	}
	if decision.YRate <= 0 {
		t.Fatalf("YRate = %d, want > 0", decision.YRate)
	}
	if decision.UVRate < 0 {
		t.Fatalf("UVRate = %d, want >= 0", decision.UVRate)
	}
	want := decision.YRate + decision.UVRate + decision.OtherCost + decision.RefCost
	if decision.TotalRate != want {
		t.Fatalf("TotalRate = %d, want YRate+UVRate+OtherCost+RefCost = %d", decision.TotalRate, want)
	}
	if decision.Rate2 != decision.TotalRate {
		t.Fatalf("Rate2 = %d, want TotalRate %d", decision.Rate2, decision.TotalRate)
	}
	// Y-only RD must be strictly less than full RD because UV rate /
	// distortion both contribute non-negatively (and UV rate > 0 in
	// this synthetic case via TestSelectInterFrameSplitMotionDecisionRDAccountsForChromaResidual's
	// shape).
	if decision.UVRate > 0 && decision.YRD >= decision.RD {
		t.Fatalf("YRD = %d, RD = %d, want YRD < RD when UV rate is non-zero", decision.YRD, decision.RD)
	}
}

// TestSelectInterFrameModeDecisionInactiveMacroblockMatchesLibvpxModeLoop
// asserts that active_map[r][c]==0 is handled at libvpx's evaluate_inter_mode /
// evaluate_inter_mode_rd point inside the candidate loop. The first tested
// ZEROMV/LAST candidate wins with skip=1 and segment=0, and its mode-test
// accounting still mutates exactly like libvpx.
func TestSelectInterFrameModeDecisionInactiveMacroblockMatchesLibvpxModeLoop(t *testing.T) {
	const w, h = 32, 16
	e := newSizedTestEncoder(t, w, h)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	const mbRows, mbCols = 1, 2
	mask := []uint8{0, 1}
	if err := e.SetActiveMap(mask, mbRows, mbCols); err != nil {
		t.Fatalf("SetActiveMap returned error: %v", err)
	}
	if !e.activeMapEnabled {
		t.Fatalf("activeMapEnabled = false after SetActiveMap, want true")
	}
	src := testImage(w, h)
	fillImage(src, 128, 90, 170)
	last := testVP8Frame(t, w, h, 64, 96, 160)
	for row := range h {
		for col := range w {
			last.Img.Y[row*last.Img.YStride+col] = byte((row*53 + col*97 + 7) & 255)
		}
	}
	last.ExtendBorders()
	refs := [...]interAnalysisReference{{
		Frame:      vp8common.LastFrame,
		Img:        &last.Img,
		RefRateSet: true,
		RefRate:    1 << 20,
	}}
	quant := testRegularMacroblockQuant(t, testInterSearchQIndex)

	e.beginInterRDModeDecisionFrame()
	defer e.endInterRDModeDecisionFrame()
	e.beginInterRDModeDecisionMacroblock()
	beforeMBs := e.interMBsTestedSoFar
	beforeHits := e.interModeTestHitCounts
	beforeTouched := e.interRDThreshTouched

	decision, ok := e.selectInterFrameModeDecision(
		sourceImageFromPublic(src), refs[:], len(refs),
		0, 0, mbRows, mbCols,
		testInterSearchQIndex, vp8enc.SegmentationConfig{}, 0,
		nil, nil, nil, nil, nil, &quant,
		false,
	)

	if !ok {
		t.Fatalf("dispatcher returned ok=false for inactive MB")
	}
	if decision.useIntra {
		t.Fatalf("decision.useIntra = true, want false (inactive MB must defer to LAST/ZEROMV)")
	}
	if decision.interMode.RefFrame != vp8common.LastFrame {
		t.Fatalf("interMode.RefFrame = %v, want LAST", decision.interMode.RefFrame)
	}
	if decision.interMode.Mode != vp8common.ZeroMV {
		t.Fatalf("interMode.Mode = %v, want ZEROMV", decision.interMode.Mode)
	}
	if decision.interMode.MV != (vp8enc.MotionVector{}) {
		t.Fatalf("interMode.MV = %+v, want zero", decision.interMode.MV)
	}
	if !decision.interMode.MBSkipCoeff {
		t.Fatalf("interMode.MBSkipCoeff = false, want true")
	}
	if decision.interMode.SegmentID != 0 {
		t.Fatalf("interMode.SegmentID = %d, want 0", decision.interMode.SegmentID)
	}
	if !decision.cyclicRefreshEligible() {
		t.Fatalf("cyclicRefreshEligible = false, want true for ZEROMV/LAST")
	}
	if e.interMBsTestedSoFar != beforeMBs {
		t.Fatalf("interMBsTestedSoFar = %d, want %d", e.interMBsTestedSoFar, beforeMBs)
	}
	for i := range beforeHits {
		want := beforeHits[i]
		if i == libvpxThrZero1 {
			want++
		}
		if got := e.interModeTestHitCounts[i]; got != want {
			t.Fatalf("interModeTestHitCounts[%d] = %d, want %d", i, got, want)
		}
	}
	for i := range beforeTouched {
		want := beforeTouched[i]
		if i == libvpxThrZero1 {
			want = true
		}
		if got := e.interRDThreshTouched[i]; got != want {
			t.Fatalf("interRDThreshTouched[%d] = %v, want %v", i, got, want)
		}
	}
}

func TestFastPickerSourceAltRefGateKeepsModeLoopAccounting(t *testing.T) {
	const w, h = 16, 16
	e := newSizedTestEncoder(t, w, h)
	src := testImage(w, h)
	fillImage(src, 96, 90, 170)
	last := testVP8Frame(t, w, h, 80, 90, 170)
	golden := testVP8Frame(t, w, h, 88, 90, 170)
	alt := testVP8Frame(t, w, h, 96, 90, 170)
	refs := [...]interAnalysisReference{
		{Frame: vp8common.LastFrame, Img: &last.Img, RefRateSet: true, RefRate: 10},
		{Frame: vp8common.GoldenFrame, Img: &golden.Img, RefRateSet: true, RefRate: 20},
		{Frame: vp8common.AltRefFrame, Img: &alt.Img, RefRateSet: true, RefRate: 30},
	}
	quant := testRegularMacroblockQuant(t, testInterSearchQIndex)

	e.beginInterRDModeDecisionFrame()
	defer e.endInterRDModeDecisionFrame()
	e.beginInterRDModeDecisionMacroblock()
	beforeHits := e.interModeTestHitCounts

	decision, ok := e.selectFastInterFrameModeDecision(sourceImageFromPublic(src), refs[:], len(refs), 0, 0, 1, 1, testInterSearchQIndex, 0, nil, nil, nil, &quant, true)
	if !ok {
		t.Fatalf("selectFastInterFrameModeDecision returned ok=false")
	}
	if decision.useIntra || decision.interMode.RefFrame != vp8common.AltRefFrame || decision.interMode.Mode != vp8common.ZeroMV {
		t.Fatalf("decision = %+v, want ALTREF/ZEROMV inter mode", decision)
	}
	for _, modeIndex := range []int{libvpxThrZero1, libvpxThrDC, libvpxThrNearest1, libvpxThrNear1, libvpxThrZero2, libvpxThrNearest2, libvpxThrZero3} {
		if got := e.interModeTestHitCounts[modeIndex]; got != beforeHits[modeIndex]+1 {
			t.Fatalf("interModeTestHitCounts[%d] = %d, want %d", modeIndex, got, beforeHits[modeIndex]+1)
		}
	}
}

// TestImprovedInterFrameSearchStartReferencePolicyAppliesAltRefSignBias verifies
// the high-level reference-switching sign-bias policy: when libvpx walks LAST,
// GOLDEN, and ALTREF as candidate references in vp8_pick_inter_mode /
// vp8_rd_pick_inter_mode, vp8_mv_pred biases each near-MV with mv_bias() based
// on the neighbour's stored ref frame versus the currently tested ref frame.
// In libvpx only ALTREF ever flips its sign bias (driven by source_alt_ref_active
// in onyx_if.c update_alt_ref_frame_stats); LAST and GOLDEN remain at 0. The
// expected behaviour is that re-running the predictor for the same neighbour
// table with target=LAST vs target=ALTREF produces opposite-signed predicted
// MVs when the neighbour ref disagrees with the target on the sign bias map.
func TestImprovedInterFrameSearchStartReferencePolicyAppliesAltRefSignBias(t *testing.T) {
	const mbRows, mbCols = 3, 3
	src := testImage(mbCols*16, mbRows*16)
	fillImage(src, 96, 90, 170)
	analysis := testVP8Frame(t, mbCols*16, mbRows*16, 96, 90, 170)
	last := testVP8Frame(t, mbCols*16, mbRows*16, 96, 90, 170)
	// Populate the previous-frame mode grid with the same LAST-ref MV in every
	// MB so all five lf-frame slots land on LAST sign-bias=false. Two cells
	// (mbRow,mbCol-1 = 1,0 and mbRow-1,mbCol-1 = 0,0) are intra to mirror an
	// arbitrary mix; the remaining cells stamp a positive col MV.
	modes := make([]vp8enc.InterFrameMacroblockMode, mbRows*mbCols)
	bias := make([]bool, len(modes))
	for r := range mbRows {
		for c := range mbCols {
			modes[r*mbCols+c] = vp8enc.InterFrameMacroblockMode{
				RefFrame: vp8common.LastFrame,
				Mode:     vp8common.NewMV,
				MV:       vp8enc.MotionVector{Col: 24},
			}
		}
	}
	e := &VP8Encoder{
		analysis:                 analysis,
		lastRef:                  last,
		lastFrameInterModes:      modes,
		lastFrameInterModeBias:   bias,
		lastFrameInterModesValid: true,
		sourceAltRefActive:       true, // sign_bias[ALTREF] = 1, sign_bias[LAST] = 0
	}
	above := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 24}}
	left := above
	aboveLeft := above
	search := interAnalysisSearchConfig{improvedMVPrediction: true}

	// Target=LAST: target sign-bias matches the LAST-ref neighbours, so the
	// predicted MV is taken verbatim from the first neighbour-ranked LAST slot.
	startLast := e.improvedInterFrameSearchStart(sourceImageFromPublic(src), vp8common.LastFrame, 1, 1, mbRows, mbCols, &above, &left, &aboveLeft, search)
	if !startLast.ok {
		t.Fatalf("LAST predictor returned ok=false")
	}
	if startLast.mv != (vp8enc.MotionVector{Col: 24}) {
		t.Fatalf("LAST predictor MV = %+v, want {Col: 24} (no sign flip when sign_bias[LAST] == sign_bias[LAST])", startLast.mv)
	}

	// Target=ALTREF: every neighbour holds a LAST ref with sign-bias=0, but
	// target ALTREF has sign-bias=1, so libvpx's mv_bias flips every near-MV.
	// No neighbour has refFrame==ALTREF, so improvedInterFrameSearchStart falls
	// through to the median-of-flipped-MVs fallback with sr=0 — the libvpx
	// "sr=0 lets caller pick search range" branch.
	startAltRef := e.improvedInterFrameSearchStart(sourceImageFromPublic(src), vp8common.AltRefFrame, 1, 1, mbRows, mbCols, &above, &left, &aboveLeft, search)
	if !startAltRef.ok {
		t.Fatalf("ALTREF predictor returned ok=false")
	}
	if startAltRef.mv != (vp8enc.MotionVector{Col: -24}) {
		t.Fatalf("ALTREF predictor MV = %+v, want {Col: -24} (sign flipped because sign_bias[ALTREF] != sign_bias[LAST])", startAltRef.mv)
	}
	if startAltRef.sr != 0 {
		t.Fatalf("ALTREF predictor sr = %d, want 0 (median fallback when no neighbour matches target ref)", startAltRef.sr)
	}

	// Symmetry check: a neighbour table populated with ALTREF refs collapses
	// the bias decision the other way — predicting ALTREF returns the raw MV,
	// predicting LAST flips it.
	above.RefFrame = vp8common.AltRefFrame
	left.RefFrame = vp8common.AltRefFrame
	aboveLeft.RefFrame = vp8common.AltRefFrame
	for i := range modes {
		modes[i].RefFrame = vp8common.AltRefFrame
		bias[i] = true
	}
	startAltRef2 := e.improvedInterFrameSearchStart(sourceImageFromPublic(src), vp8common.AltRefFrame, 1, 1, mbRows, mbCols, &above, &left, &aboveLeft, search)
	if !startAltRef2.ok || startAltRef2.mv != (vp8enc.MotionVector{Col: 24}) {
		t.Fatalf("ALTREF predictor with ALTREF neighbours = %+v, want {Col: 24} (matching sign_bias must not flip)", startAltRef2.mv)
	}
	startLast2 := e.improvedInterFrameSearchStart(sourceImageFromPublic(src), vp8common.LastFrame, 1, 1, mbRows, mbCols, &above, &left, &aboveLeft, search)
	if !startLast2.ok || startLast2.mv != (vp8enc.MotionVector{Col: -24}) {
		t.Fatalf("LAST predictor with ALTREF neighbours = %+v, want {Col: -24} (sign flipped because sign_bias[LAST] != sign_bias[ALTREF])", startLast2.mv)
	}
}

// TestSelectRDInterFrameModeDecisionUsesTempTokenContext anchors libvpx
// rdopt.c vp8_rd_pick_inter_mode's tempa/templ contract: candidate-mode
// trials operate on stack-local copies of ENTROPY_CONTEXT and only the
// chosen mode's context is committed to the row state. Pre-populating the
// caller's aboveTok/leftTok with distinctive sentinels and then driving the
// RD picker must leave those structs untouched on return — the deferred
// updateInterAnalysisTokenContext in
// buildReconstructingInterFrameCoefficientsWithSegmentation owns the commit.
func TestSelectRDInterFrameModeDecisionUsesTempTokenContext(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	if !e.interAnalysisUsesRDModeDecision() {
		t.Fatalf("interAnalysisUsesRDModeDecision = false, want true under best-quality deadline")
	}

	src := testImage(16, 16)
	fillImage(src, 96, 96, 96)
	for row := range 16 {
		for col := range 16 {
			src.Y[row*src.YStride+col] = byte((19 + row*41 + col*23 + row*col*7) & 255)
		}
	}
	last := testVP8Frame(t, 16, 16, 96, 96, 96)
	for row := range 16 {
		for col := range 16 {
			last.Img.Y[row*last.Img.YStride+col] = byte((211 - row*13 - col*29) & 255)
		}
	}
	last.ExtendBorders()
	golden := testVP8Frame(t, 16, 16, 96, 96, 96)
	for row := range 16 {
		copy(golden.Img.Y[row*golden.Img.YStride:], src.Y[row*src.YStride:row*src.YStride+16])
	}
	golden.ExtendBorders()
	refs := [...]interAnalysisReference{
		{Frame: vp8common.LastFrame, Img: &last.Img, RefRateSet: true, RefRate: 1 << 20},
		{Frame: vp8common.GoldenFrame, Img: &golden.Img, RefRateSet: true, RefRate: 1 << 20},
	}
	quant := testRegularMacroblockQuant(t, testInterSearchQIndex)

	// Distinctive sentinels: every Y/UV/Y2 plane gets a non-zero pattern that
	// no legitimate post-trial token state could match (libvpx hasCoeffs
	// values are 0 or 1, never ones with high bits set).
	above := vp8enc.TokenContextPlanes{
		Y1: [4]uint8{0xA1, 0xA2, 0xA3, 0xA4},
		U:  [2]uint8{0xA5, 0xA6},
		V:  [2]uint8{0xA7, 0xA8},
		Y2: 0xA9,
	}
	left := vp8enc.TokenContextPlanes{
		Y1: [4]uint8{0xB1, 0xB2, 0xB3, 0xB4},
		U:  [2]uint8{0xB5, 0xB6},
		V:  [2]uint8{0xB7, 0xB8},
		Y2: 0xB9,
	}
	aboveSnapshot := above
	leftSnapshot := left

	e.beginInterRDModeDecisionFrame()
	defer e.endInterRDModeDecisionFrame()
	e.beginInterRDModeDecisionMacroblock()
	decision, ok := e.selectRDInterFrameModeDecision(
		sourceImageFromPublic(src), refs[:], len(refs),
		0, 0, 1, 1, testInterSearchQIndex, 0,
		nil, nil, nil,
		&above, &left,
		&quant,
		false,
	)
	if !ok {
		t.Fatalf("selectRDInterFrameModeDecision returned ok=false")
	}
	// The picker must explore at least one inter or intra candidate, so this
	// is a meaningful exercise of the per-mode token-context paths.
	if !decision.useIntra && decision.interMode.Mode == vp8common.SplitMV {
		// SplitMV exercises a different RD subroutine; either is fine for the
		// invariant we're testing.
		_ = decision
	}

	if above != aboveSnapshot {
		t.Fatalf("aboveTok mutated by RD picker: got %+v, want %+v (caller-owned ENTROPY_CONTEXT must not be touched during candidate trials)", above, aboveSnapshot)
	}
	if left != leftSnapshot {
		t.Fatalf("leftTok mutated by RD picker: got %+v, want %+v (caller-owned ENTROPY_CONTEXT must not be touched during candidate trials)", left, leftSnapshot)
	}
}

// TestRecodeLoopResetsTokenContext anchors libvpx onyx_if.c
// restore_coding_context's effect on the per-row ENTROPY_CONTEXT during the
// inter-frame recode loop: each call to
// buildReconstructingInterFrameCoefficientsWithSegmentation begins with a
// freshly zeroed above/left token-context working set, so a rejected
// attempt's commits never leak into the next attempt. We simulate two
// recode attempts on the same input by corrupting e.tokenAbove between
// calls; the second pass must produce identical coefficients to the first
// because the per-MB working contexts are local to the function.
func TestRecodeLoopResetsTokenContext(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	src := testImage(16, 16)
	fillImage(src, 128, 90, 170)
	for row := range 16 {
		for col := range 16 {
			src.Y[row*src.YStride+col] = byte((33 + row*51 + col*61 + row*col*9) & 255)
		}
	}
	fillBenchmarkVP8Image(&e.lastRef.Img, 200, 90, 170)
	e.lastRef.ExtendBorders()

	modesA := make([]vp8enc.InterFrameMacroblockMode, 1)
	coeffsA := make([]vp8enc.MacroblockCoefficients, 1)
	if _, err := e.buildReconstructingInterFrameCoefficients(sourceImageFromPublic(src), testInterSearchQIndex, modesA, coeffsA, 1, 1, EncodeNoReferenceGolden|EncodeNoReferenceAltRef); err != nil {
		t.Fatalf("first recode attempt returned error: %v", err)
	}

	// Simulate a rejected first attempt that left junk in the encoder's
	// per-frame e.tokenAbove buffer (which the packet writer also expects
	// to overwrite at the start of every call). Set every plane to 0xFF so
	// any leak into the second attempt's RD picker would produce different
	// quantized residuals than the first attempt.
	for i := range e.tokenAbove {
		e.tokenAbove[i] = vp8enc.TokenContextPlanes{
			Y1: [4]uint8{0xFF, 0xFF, 0xFF, 0xFF},
			U:  [2]uint8{0xFF, 0xFF},
			V:  [2]uint8{0xFF, 0xFF},
			Y2: 0xFF,
		}
	}

	modesB := make([]vp8enc.InterFrameMacroblockMode, 1)
	coeffsB := make([]vp8enc.MacroblockCoefficients, 1)
	if _, err := e.buildReconstructingInterFrameCoefficients(sourceImageFromPublic(src), testInterSearchQIndex, modesB, coeffsB, 1, 1, EncodeNoReferenceGolden|EncodeNoReferenceAltRef); err != nil {
		t.Fatalf("second recode attempt returned error: %v", err)
	}

	if modesA[0].Mode != modesB[0].Mode || modesA[0].RefFrame != modesB[0].RefFrame || modesA[0].MV != modesB[0].MV || modesA[0].MBSkipCoeff != modesB[0].MBSkipCoeff {
		t.Fatalf("recode mode drift: first=%+v second=%+v (per-MB token contexts must reset at start of each attempt)", modesA[0], modesB[0])
	}
	if coeffsA[0] != coeffsB[0] {
		t.Fatalf("recode coefficient drift: corrupted e.tokenAbove leaked across attempts")
	}
}

// splitMVDecisionRDFixture builds the deterministic SPLITMV-friendly fixture
// shared by the transform-domain RD assertions below. The shape mirrors
// TestSelectInterFrameSplitMotionDecisionRDAccountsForChromaResidual: the
// luma top half shifts by one column, the bottom half is identity, the
// per-4x4 DC offsets push the residual above the inter zbin so blocks
// quantize to non-zero coefficients, and chroma is matched so UV residual
// only comes from the encoder-derived 8x8 chroma sub-pel filter taps.
