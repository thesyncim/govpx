package govpx

import (
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

// TestPredictBestBPredLumaModeRDIterationOrderMatchesLibvpx pins the candidate
// list to libvpx's blockd.h B_PREDICTION_MODE enum order
// (B_DC_PRED, B_TM_PRED, B_VE_PRED, B_HE_PRED, B_LD_PRED, B_RD_PRED,
// B_VR_PRED, B_VL_PRED, B_HD_PRED, B_HU_PRED) so that
// rd_pick_intra4x4block's `for (mode = B_DC_PRED; mode <= B_HU_PRED; ++mode)`
// stays bit-aligned with govpx.
func TestPredictBestBPredLumaModeRDIterationOrderMatchesLibvpx(t *testing.T) {
	want := [...]vp8common.BPredictionMode{
		vp8common.BDCPred,
		vp8common.BTMPred,
		vp8common.BVEPred,
		vp8common.BHEPred,
		vp8common.BLDPred,
		vp8common.BRDPred,
		vp8common.BVRPred,
		vp8common.BVLPred,
		vp8common.BHDPred,
		vp8common.BHUPred,
	}
	if len(bPredIntraModeCandidates) != len(want) {
		t.Fatalf("bPredIntraModeCandidates length = %d, want %d", len(bPredIntraModeCandidates), len(want))
	}
	for i := range want {
		if bPredIntraModeCandidates[i] != want[i] {
			t.Fatalf("bPredIntraModeCandidates[%d] = %v, want %v", i, bPredIntraModeCandidates[i], want[i])
		}
		if int(bPredIntraModeCandidates[i]) != i {
			t.Fatalf("bPredIntraModeCandidates[%d] = %d, want %d (libvpx B_PREDICTION_MODE order)", i, bPredIntraModeCandidates[i], i)
		}
	}
}

// fillIntra4x4PickerGradient writes a smooth gradient that lets B_VE_PRED /
// B_HE_PRED produce small residuals in many sub-blocks, exercising the inner
// candidate loop of predictBestBPredLumaModeRD without saturating to a single
// trivial mode.
func fillIntra4x4PickerGradient(img Image) {
	for row := 0; row < img.Height; row++ {
		for col := 0; col < img.Width; col++ {
			img.Y[row*img.YStride+col] = byte(64 + row*4 + col*4)
		}
	}
}

func newIntra4x4PickerSource() Image {
	src := testImage(16, 16)
	fillIntra4x4PickerGradient(src)
	return src
}

func newIntra4x4PickerPred(t *testing.T) vp8common.FrameBuffer {
	t.Helper()
	pred := testVP8Frame(t, 16, 16, 128, 128, 128)
	for row := 0; row < pred.Img.Height; row++ {
		for col := 0; col < pred.Img.Width; col++ {
			pred.Img.Y[row*pred.Img.YStride+col] = byte(60 + row*4 + col*4)
		}
	}
	pred.ExtendBorders()
	return pred
}

// TestPredictBestBPredLumaModeRDKeyFrameRateIsModePlusTokens verifies the
// returned rate equals the sum of per-block bmode_costs (tree cost over
// KeyFrameBModeProbs[A][L]) plus the per-block coefficient-token rate, with
// the entropy context propagating across blocks via the hasCoeffs bit. This
// pins parity with libvpx's `cost = bmode_costs[A][L][mode] + ratey` summed
// across all 16 blocks in rd_pick_intra4x4mby_modes.
func TestPredictBestBPredLumaModeRDKeyFrameRateIsModePlusTokens(t *testing.T) {
	src := newIntra4x4PickerSource()
	predForRun := newIntra4x4PickerPred(t)
	qIndex := 20
	probs := vp8tables.DefaultCoefProbs
	quant := testMacroblockQuant(qIndex)
	var scratch vp8dec.IntraReconstructionScratch

	modes, rate, dist, ok := predictBestBPredLumaModeRD(sourceImageFromPublic(src), qIndex, 0, true, 0, 0, nil, nil, nil, nil, &quant, &predForRun.Img, &scratch, 0, &probs, false)
	if !ok {
		t.Fatalf("predictBestBPredLumaModeRD returned ok=false")
	}

	// Oracle: replay the per-block loop on a fresh predictor with the
	// picker-chosen modes, decompose the rate as
	//   sum(bmode_cost) + sum(token_rate),
	// and confirm it matches what the picker returned.
	oraclePred := newIntra4x4PickerPred(t)
	var oracleScratch vp8dec.IntraReconstructionScratch
	refs := vp8dec.BuildIntraPredictorRefs(&oraclePred.Img, 0, 0, &oracleScratch.Refs)
	y := oraclePred.Img.Y[:]
	var tokAbove [4]uint8
	var tokLeft [4]uint8
	var trackModes [16]vp8common.BPredictionMode
	oracleModeRate := 0
	oracleTokenRate := 0
	oracleDist := 0
	for block := range 16 {
		mode := modes[block]
		var blockPred [16]byte
		if !predictAnalysisBPredBlock(mode, blockPred[:], 4, y, oraclePred.Img.YStride, refs.YAbove, refs.YLeft, refs.YTopLeft, block) {
			t.Fatalf("oracle predictAnalysisBPredBlock returned false")
		}
		var input [16]int16
		var dct [16]int16
		var qcoeff [16]int16
		var dqcoeff [16]int16
		vp8enc.FillBPredResidual4x4(sourceImageFromPublic(src), 0, 0, block, blockPred[:], &input)
		vp8enc.ForwardDCT4x4(input[:], 4, &dct)
		ctx := int(tokAbove[block&3] + tokLeft[(block&0x0c)>>2])
		eob := vp8enc.QuantizeDecisionBlock(false, &dct, &quant.Y1, 0, &qcoeff, &dqcoeff)
		tokenRate := vp8enc.CoefficientBlockTokenRate(&probs, 3, ctx, 0, &qcoeff, eob)
		modeRate := bPredModeRate(true, mode,
			bPredAnalysisAboveMode(true, nil, trackModes, block),
			bPredAnalysisLeftMode(true, nil, trackModes, block))
		oracleModeRate += modeRate
		oracleTokenRate += tokenRate
		oracleDist += vp8enc.TransformBlockError(&dct, &dqcoeff) >> 2

		// Reconstruct and update neighbor pixels + entropy ctx like the
		// production picker does so subsequent blocks see the same
		// predictor inputs and ctx state.
		var recon [16]byte
		dsp.IDCT4x4Add(&dqcoeff, blockPred[:], 4, recon[:], 4)
		vp8enc.CopyBPredBlock(recon[:], y, oraclePred.Img.YStride, block)
		hasCoeffs := uint8(0)
		if eob > 0 {
			hasCoeffs = 1
		}
		tokAbove[block&3] = hasCoeffs
		tokLeft[(block&0x0c)>>2] = hasCoeffs
		trackModes[block] = mode
	}
	if oracleModeRate+oracleTokenRate != rate {
		t.Fatalf("rate decomposition mismatch: picker rate=%d, oracle modes=%d + tokens=%d = %d",
			rate, oracleModeRate, oracleTokenRate, oracleModeRate+oracleTokenRate)
	}
	if oracleDist != dist {
		t.Fatalf("dist decomposition mismatch: picker dist=%d, oracle=%d", dist, oracleDist)
	}
	if oracleModeRate <= 0 {
		t.Fatalf("oracle mode rate=%d, want positive", oracleModeRate)
	}
	if oracleTokenRate < 0 {
		t.Fatalf("oracle token rate=%d, want non-negative", oracleTokenRate)
	}
}

// TestPredictBestBPredLumaModeRDInterUsesDefaultBModeProbs confirms that
// non-key-frame runs ignore neighbor B-modes (libvpx's `inter_bmode_costs`
// path) and that bPredModeRate uses DefaultBModeProbs in that branch. The
// RD picker intentionally does NOT mirror libvpx's sub_mv_ref overwrite of
// inter_bmode_costs[0..3] — see predictBestBPredLumaModeRD comment for the
// good-cpu3-vbr SPLITMV regression that pinned this choice.
func TestPredictBestBPredLumaModeRDInterUsesDefaultBModeProbs(t *testing.T) {
	src := newIntra4x4PickerSource()
	pred := newIntra4x4PickerPred(t)
	qIndex := 20
	probs := vp8tables.DefaultCoefProbs
	quant := testMacroblockQuant(qIndex)
	var scratch vp8dec.IntraReconstructionScratch

	modes, rate, dist, ok := predictBestBPredLumaModeRD(sourceImageFromPublic(src), qIndex, 0, false, 0, 0, nil, nil, nil, nil, &quant, &pred.Img, &scratch, 0, &probs, false)
	if !ok {
		t.Fatalf("predictBestBPredLumaModeRD inter returned ok=false")
	}
	if rate <= 0 || dist < 0 {
		t.Fatalf("rate=%d dist=%d, want positive rate and non-negative distortion", rate, dist)
	}
	for i, m := range modes {
		if m < vp8common.BDCPred || m > vp8common.BHUPred {
			t.Fatalf("modes[%d]=%v outside B_DC_PRED..B_HU_PRED", i, m)
		}
	}

	// In the inter branch, bPredModeRate must use DefaultBModeProbs and
	// be insensitive to neighbor B-modes. Spot-check by comparing
	// bPredModeRate(false, mode, A, L) for two unrelated A/L values.
	for _, m := range modes {
		ra := bPredModeRate(false, m, vp8common.BDCPred, vp8common.BDCPred)
		rb := bPredModeRate(false, m, vp8common.BTMPred, vp8common.BHUPred)
		if ra != rb {
			t.Fatalf("inter bPredModeRate(%v) leaked neighbor context: A=DC,L=DC -> %d, A=TM,L=HU -> %d", m, ra, rb)
		}
	}
}

// TestPredictBestBPredLumaModeRDKeyFrameUsesNeighborProbs confirms that
// keyFrame=true picks bmode_costs from KeyFrameBModeProbs[A][L]: changing the
// first block's A/L neighbors (via the `above`/`left` MB pointers) must
// change bPredModeRate for that block, while keeping the inter branch flat.
// This pins libvpx's `bmode_costs = mb->bmode_costs[A][L]` indexing.
func TestPredictBestBPredLumaModeRDKeyFrameUsesNeighborProbs(t *testing.T) {
	// Build two MB neighbors with different YMode -> B-mode mappings:
	//  - DC neighbor maps to B_DC_PRED
	//  - V_PRED neighbor maps to B_VE_PRED
	above1 := vp8enc.KeyFrameMacroblockMode{YMode: vp8common.DCPred, UVMode: vp8common.DCPred}
	left1 := vp8enc.KeyFrameMacroblockMode{YMode: vp8common.DCPred, UVMode: vp8common.DCPred}
	above2 := vp8enc.KeyFrameMacroblockMode{YMode: vp8common.VPred, UVMode: vp8common.DCPred}
	left2 := vp8enc.KeyFrameMacroblockMode{YMode: vp8common.HPred, UVMode: vp8common.DCPred}

	// Sanity: bPredModeRate for the same candidate with two neighbor configs
	// should differ across at least one B-mode.
	differs := false
	for _, candidate := range bPredIntraModeCandidates {
		r1 := bPredModeRate(true, candidate,
			blockModeFromKeyFrameMacroblockMode(above1.YMode),
			blockModeFromKeyFrameMacroblockMode(left1.YMode))
		r2 := bPredModeRate(true, candidate,
			blockModeFromKeyFrameMacroblockMode(above2.YMode),
			blockModeFromKeyFrameMacroblockMode(left2.YMode))
		if r1 != r2 {
			differs = true
			break
		}
	}
	if !differs {
		t.Fatalf("bPredModeRate keyFrame did not depend on neighbor modes; KeyFrameBModeProbs[A][L] indexing may be lost")
	}

	src := newIntra4x4PickerSource()
	qIndex := 20
	probs := vp8tables.DefaultCoefProbs
	quant := testMacroblockQuant(qIndex)

	pred1 := newIntra4x4PickerPred(t)
	var scratch1 vp8dec.IntraReconstructionScratch
	_, rate1, _, ok := predictBestBPredLumaModeRD(sourceImageFromPublic(src), qIndex, 0, true, 0, 0, &above1, &left1, nil, nil, &quant, &pred1.Img, &scratch1, 0, &probs, false)
	if !ok {
		t.Fatalf("predictBestBPredLumaModeRD with DC neighbors returned ok=false")
	}

	pred2 := newIntra4x4PickerPred(t)
	var scratch2 vp8dec.IntraReconstructionScratch
	_, rate2, _, ok := predictBestBPredLumaModeRD(sourceImageFromPublic(src), qIndex, 0, true, 0, 0, &above2, &left2, nil, nil, &quant, &pred2.Img, &scratch2, 0, &probs, false)
	if !ok {
		t.Fatalf("predictBestBPredLumaModeRD with V/H neighbors returned ok=false")
	}

	if rate1 == rate2 {
		t.Fatalf("keyFrame rate did not respond to neighbor changes: rate1=%d rate2=%d (expected libvpx bmode_costs[A][L] sensitivity)",
			rate1, rate2)
	}
}

// TestPredictBestBPredLumaModeRDPerBlockBailoutFires forces the per-block
// short-circuit (`total_rd >= bestRD`) by passing bestRD = 1, mirroring
// rd_pick_intra4x4mby_modes' `if (total_rd >= (int64_t)best_rd) break;
// return INT_MAX;` early-exit.
func TestPredictBestBPredLumaModeRDPerBlockBailoutFires(t *testing.T) {
	src := newIntra4x4PickerSource()
	pred := newIntra4x4PickerPred(t)
	qIndex := 20
	probs := vp8tables.DefaultCoefProbs
	quant := testMacroblockQuant(qIndex)
	var scratch vp8dec.IntraReconstructionScratch

	_, rate, dist, ok := predictBestBPredLumaModeRD(sourceImageFromPublic(src), qIndex, 0, true, 0, 0, nil, nil, nil, nil, &quant, &pred.Img, &scratch, 1, &probs, false)
	if ok {
		t.Fatalf("expected per-block bailout to fire (rate=%d dist=%d), got ok=true", rate, dist)
	}
	if rate != 0 || dist != 0 {
		t.Fatalf("bailout returned rate=%d dist=%d, want zeros", rate, dist)
	}
}

// TestPredictBestBPredLumaModeRDPerBlockBailoutNoFireWithLargeBestRD verifies
// the picker completes when bestRD is large enough that the running RDCOST
// never reaches it. This is the dual of the bailout test and pins
// govpx's `bestRD > 0 && totalRD >= bestRD` guard alongside libvpx's
// per-block break.
func TestPredictBestBPredLumaModeRDPerBlockBailoutNoFireWithLargeBestRD(t *testing.T) {
	src := newIntra4x4PickerSource()
	pred := newIntra4x4PickerPred(t)
	qIndex := 20
	probs := vp8tables.DefaultCoefProbs
	quant := testMacroblockQuant(qIndex)
	var scratch vp8dec.IntraReconstructionScratch

	const huge = 1 << 30
	_, rate, _, ok := predictBestBPredLumaModeRD(sourceImageFromPublic(src), qIndex, 0, true, 0, 0, nil, nil, nil, nil, &quant, &pred.Img, &scratch, huge, &probs, false)
	if !ok {
		t.Fatalf("expected picker to complete with large bestRD; got ok=false rate=%d", rate)
	}
	if rate <= 0 {
		t.Fatalf("rate=%d, want positive", rate)
	}
}
