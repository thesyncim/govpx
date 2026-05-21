package govpx

import (
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

func splitMVDecisionRDFixture(t *testing.T) (vp8enc.SourceImage, *vp8common.Image, *vp8common.FrameBuffer, vp8enc.MacroblockQuant, int) {
	t.Helper()
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

	pred := &vp8common.FrameBuffer{}
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

	return sourceImageFromPublic(src), &ref.Img, pred, quant, splitRDQIndex
}

func TestSplitMotionLabelRDEvaluatorUsesTransformTokenRate(t *testing.T) {
	src, ref, _, quant, qIndex := splitMVDecisionRDFixture(t)
	var ev splitMotionLabelRDEvaluator
	if !ev.init(0, 0, nil, nil, false, false) {
		t.Fatalf("splitMotionLabelRDEvaluator.init returned false")
	}
	mode := vp8enc.InterFrameMacroblockMode{Mode: vp8common.SplitMV, Partition: 0}
	mv := vp8enc.MotionVector{Col: 8}
	labelRate := splitSubMotionLabelRate(vp8common.New4x4)
	labelRate += splitMotionVectorCost(mv, &vp8tables.DefaultMVContext)

	rate, yRate, dist, tteob, nextAbove, nextLeft, ok := ev.rateDistortion(src, ref, 0, 0, qIndex, &quant, &vp8tables.DefaultCoefProbs, &mode, 0, mv, labelRate)
	if !ok {
		t.Fatalf("rateDistortion returned ok=false")
	}
	if rate <= labelRate {
		t.Fatalf("rate = %d, want token rate added above label-only rate %d", rate, labelRate)
	}
	if yRate != rate-labelRate {
		t.Fatalf("yRate = %d, want token-only rate %d", yRate, rate-labelRate)
	}
	if dist <= 0 {
		t.Fatalf("distortion = %d, want transform-domain residual distortion", dist)
	}
	if tteob <= 0 {
		t.Fatalf("tteob = %d, want non-zero label EOB count", tteob)
	}
	if nextAbove == ([4]uint8{}) || nextLeft == ([4]uint8{}) {
		t.Fatalf("next contexts = above %v left %v, want cost_coeffs-style token context updates", nextAbove, nextLeft)
	}
}

func TestSplitMotionLabelRDCommitsContextsBeforeNewGate(t *testing.T) {
	src, ref, _, quant, qIndex := splitMVDecisionRDFixture(t)
	var ev splitMotionLabelRDEvaluator
	ev.init(0, 0, nil, nil, false, false)
	mode := vp8enc.InterFrameMacroblockMode{Mode: vp8common.SplitMV, Partition: 0}
	mv, bMode := selectInterFrameSplitSubsetMotionModeWithSearchThresholdAndLabelRD(
		src, ref, 0, 0, &mode, 0, 16, 8,
		vp8enc.MotionVector{}, vp8enc.MotionVector{}, 0, false,
		qIndex, nil, nil, defaultInterAnalysisSearchConfig(), &vp8tables.DefaultMVContext,
		maxInt(), &ev, &quant, &vp8tables.DefaultCoefProbs,
	)
	if bMode == vp8common.New4x4 {
		t.Fatalf("bMode = NEW4X4 with max label threshold; expected pre-NEW gate to return an existing label")
	}
	if mv != (vp8enc.MotionVector{}) {
		t.Fatalf("mv = %+v, want pre-NEW gated existing-label MV", mv)
	}
	if ev.yAbove == ([4]uint8{}) || ev.yLeft == ([4]uint8{}) {
		t.Fatalf("committed contexts = above %v left %v, want rd_check_segment-style context commit before NEW gate", ev.yAbove, ev.yLeft)
	}
}

// TestSplitMVDecisionRDUsesTransformDomainRate pins
// selectInterFrameSplitMotionDecisionRDWithThreshold's rate accounting to
// libvpx's rd_check_segment + vp8_rd_pick_inter_mode SPLITMV path. After
// vp8_rd_pick_best_mbsegmentation commits the per-subblock luma MVs, the
// SPLITMV branch runs forward DCT + vp8_quantize_b + cost_coeffs over all
// 16 luma 4x4 blocks (block_type=3 / Y_WITH_DC) and over all 8 chroma 4x4
// blocks (block_type=2 / UV) — distinct from the per-label SAD + MV-cost
// trial inside rd_check_segment. This test asserts the rate path is the
// transform-domain one (YRate must be strictly positive on a fixture that
// generates non-zero residual), the chroma path runs (UVRate >= 0), and
// the rate2 breakdown
//
//	rate2 = rate_y + rate_uv + other_cost + ref_frame_cost
//
// from update_best_mode is reproduced exactly on the returned decision.
func TestSplitMVDecisionRDUsesTransformDomainRate(t *testing.T) {
	src, ref, pred, quant, qIndex := splitMVDecisionRDFixture(t)
	const otherCost = 73
	const refCost = 41
	decision, ok := selectInterFrameSplitMotionDecisionRDWithThreshold(
		src, ref, vp8common.LastFrame,
		0, 0, vp8enc.MotionVector{}, qIndex, 0,
		&quant, nil, nil, &vp8tables.DefaultCoefProbs, &pred.Img,
		0, false, true,
		0, otherCost, refCost,
	)
	if !ok {
		t.Fatalf("selectInterFrameSplitMotionDecisionRDWithThreshold returned false")
	}
	if decision.Mode.Mode != vp8common.SplitMV {
		t.Fatalf("decision.Mode.Mode = %v, want SplitMV", decision.Mode.Mode)
	}
	// Transform-domain rate must be strictly positive: the fixture's
	// per-4x4 DC offsets push the residual above the inter zbin so each
	// luma 4x4 block emits at least the DC coefficient through cost_coeffs
	// (block_type=3). Prior to wiring the per-block transform/quant path
	// into the SPLITMV decision the rate was a SAD-derived estimate that
	// would not cleanly track this fixture.
	if decision.YRate <= 0 {
		t.Fatalf("YRate = %d, want strictly positive transform-domain rate", decision.YRate)
	}
	if decision.UVRate < 0 {
		t.Fatalf("UVRate = %d, want >= 0", decision.UVRate)
	}
	// rate2 breakdown from update_best_mode in vp8_rd_pick_inter_mode:
	//   rate2 = rate_y (label tree + sub-MV-mode + MV cost + cost_coeffs Y) +
	//           rate_uv (rd_inter4x4_uv cost_coeffs UV) +
	//           other_cost (default no-skip / skip backout) +
	//           x->ref_frame_cost[ref_frame]
	wantRate := decision.YRate + decision.UVRate + decision.OtherCost + decision.RefCost
	if decision.TotalRate != wantRate {
		t.Fatalf("TotalRate = %d, want YRate+UVRate+OtherCost+RefCost = %d (Y=%d UV=%d other=%d ref=%d)", decision.TotalRate, wantRate, decision.YRate, decision.UVRate, decision.OtherCost, decision.RefCost)
	}
	if decision.Rate2 != decision.TotalRate {
		t.Fatalf("Rate2 = %d, want TotalRate %d", decision.Rate2, decision.TotalRate)
	}
	if decision.OtherCost != otherCost || decision.RefCost != refCost {
		t.Fatalf("OtherCost/RefCost = %d/%d, want %d/%d", decision.OtherCost, decision.RefCost, otherCost, refCost)
	}
}

// TestSplitMVDecisionRDDistortionMatchesPerLabelTransformError asserts the
// distortion stored on interSplitMVRDDecision matches a libvpx-faithful
// per-split-label forward-DCT + quantize + (coeff - dqcoeff)^2 sum,
// independently recomputed from the SPLITMV predictor. This pins the
// SPLITMV RD score to rd_check_segment's repeated vp8_encode_inter_mb_segment
// calls:
//
//	distortion += vp8_encode_inter_mb_segment(label) / 4
//
// instead of any pixel-domain SAD/SSE proxy. Chroma follows the same
// transform-domain accounting via the rd_inter4x4_uv path.
func TestSplitMVDecisionRDDistortionMatchesPerLabelTransformError(t *testing.T) {
	src, ref, pred, quant, qIndex := splitMVDecisionRDFixture(t)
	decision, ok := selectInterFrameSplitMotionDecisionRD(
		src, ref, vp8common.LastFrame,
		0, 0, vp8enc.MotionVector{}, qIndex, 0,
		&quant, nil, nil, &vp8tables.DefaultCoefProbs, &pred.Img,
		0, false, true,
	)
	if !ok {
		t.Fatalf("selectInterFrameSplitMotionDecisionRD returned false")
	}
	if decision.Mode.Mode != vp8common.SplitMV {
		t.Fatalf("decision.Mode.Mode = %v, want SplitMV", decision.Mode.Mode)
	}
	// pred now holds the committed SPLITMV predictor. Re-run the same
	// per-4x4-block forward-DCT + quantize + transform-error sum the
	// SPLITMV RD path uses, independently, with the split partition's
	// per-label /4 rounding. We feed the second pass a fresh
	// MacroblockCoefficients so it cannot reuse anything from the first
	// pass.
	var coeffs vp8enc.MacroblockCoefficients
	stats := buildPredictedMacroblockCoefficientsInternal(&predictedMacroblockCoefficientArgs{
		coefProbs:           &vp8tables.DefaultCoefProbs,
		src:                 src,
		mbRow:               0,
		mbCol:               0,
		pred:                &pred.Img,
		quant:               &quant,
		qIndex:              qIndex,
		zbinModeBoost:       vp8enc.SplitInterModeZbinBoost,
		is4x4:               true,
		splitPartitionValid: true,
		splitPartition:      decision.Mode.Partition,
		fastQuant:           false,
		optimize:            true,
		collectStats:        true,
		coeffs:              &coeffs,
	})
	if stats.distortionY != decision.YDist {
		t.Fatalf("YDist = %d, want per-label transform-error sum %d", decision.YDist, stats.distortionY)
	}
	if stats.distortionUV != decision.UVDist {
		t.Fatalf("UVDist = %d, want transform-error sum %d", decision.UVDist, stats.distortionUV)
	}
	if stats.rateY != decision.YRate {
		t.Fatalf("YRate = %d, want per-block cost_coeffs sum %d", decision.YRate, stats.rateY)
	}
	if stats.rateUV != decision.UVRate {
		t.Fatalf("UVRate = %d, want per-block cost_coeffs sum %d", decision.UVRate, stats.rateUV)
	}
	// At least one Y block and the overall distortion must be positive on
	// this fixture: if the picker had collapsed to a zero-residual
	// shortcut, neither the rate nor the distortion would discriminate
	// SPLITMV from ZEROMV.
	if decision.YDist <= 0 {
		t.Fatalf("YDist = %d, want strictly positive on non-zero residual fixture", decision.YDist)
	}
}

func TestSplitMotionPartitionLumaDistortionRoundsPerLabel(t *testing.T) {
	var blockErrors [16]int
	for subset := range int(vp8tables.MBSplitCount[2]) {
		block := int(vp8tables.MBSplitOffset[2][subset])
		blockErrors[block] = 3
	}
	got := splitMotionPartitionLumaDistortionFromBlocks(blockErrors, 2)
	total := 0
	for _, err := range blockErrors {
		total += err
	}
	if singleShift := total >> 2; singleShift != 3 {
		t.Fatalf("test fixture single-shift distortion = %d, want 3", singleShift)
	}
	if got != 0 {
		t.Fatalf("split distortion = %d, want per-label truncation to 0", got)
	}
}

// BenchmarkBuildPredictedMacroblockCoefficientsRD exercises the per-MB
// fused predict+transform+quantize+token-grid pipeline that R11-C
// merged into a single dispatch (gather residuals once, FDCT-batch Y
// and UV, serial token-context update). The benchmark runs whole-block
// inter (is4x4=false), 4x4 SPLITMV (is4x4=true), and the keyframe
// intra16x16 path so a regression in any branch surfaces here.
func BenchmarkBuildPredictedMacroblockCoefficientsRD(b *testing.B) {
	src := testImage(16, 16)
	// Fill src with a deterministic pattern so the residual is
	// non-trivial (otherwise FDCT/quantize/token-cost short-circuit and
	// the bench under-measures).
	for i := range src.Y {
		src.Y[i] = byte(64 + (i*17)%96)
	}
	for i := range src.U {
		src.U[i] = byte(80 + (i*11)%48)
		src.V[i] = byte(144 + (i*7)%48)
	}
	ref := testVP8Frame(b, 16, 16, 96, 90, 170)
	quant := testRegularMacroblockQuant(b, 20)
	source := sourceImageFromPublic(src)
	probs := &vp8tables.DefaultCoefProbs

	cases := []struct {
		name     string
		is4x4    bool
		intra    bool
		fast     bool
		optimize bool
	}{
		{"InterWholeBlockFast", false, false, true, false},
		{"InterWholeBlockRegular", false, false, false, false},
		{"InterSplitMV4x4Fast", true, false, true, false},
		{"IntraKeyFrame16x16", false, true, false, false},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			var coeffs vp8enc.MacroblockCoefficients
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = buildPredictedMacroblockCoefficientsRD(
					probs, source, 0, 0, &ref.Img, nil, nil,
					&quant, 20, 0, 0, tc.is4x4, tc.intra,
					tc.fast, tc.optimize, &coeffs,
				)
			}
		})
	}
}
