package govpx

import (
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

func TestProjectedFrameSizeBitsSubtractsLibvpxEntropySavings(t *testing.T) {
	const macroblocks = 16
	modes := make([]vp8enc.InterFrameMacroblockMode, macroblocks)
	for i := range modes {
		modes[i].Mode = vp8common.ZeroMV
		modes[i].RefFrame = vp8common.LastFrame
	}
	e := &VP8Encoder{
		interFrameModes: modes,
		refProbIntra:    200,
		refProbLast:     64,
		refProbGolden:   128,
	}
	e.rc = rateControlState{
		mode:              RateControlCBR,
		minQuantizer:      4,
		maxQuantizer:      56,
		currentQuantizer:  20,
		bitsPerFrame:      1000,
		frameTargetBits:   1000,
		bufferOptimalBits: 1000,
		bufferLevelBits:   800,
		maximumBufferBits: 2000,
	}
	// Exercise the GF/AR-refresh path so the libvpx vp8_convert_rfct_to_prob
	// gate (refFrameEntropySavingsBitsForFrame) is SKIPPED and the
	// heuristic-biased ref-frame branch contributes savings to the
	// projection. The default single-layer non-refresh path zeros out the
	// inter-frame ref-frame branch to mirror libvpx's encode_frame trailing
	// convert hook (covered by TestRefFrameEntropySavingsZeroForConvertHookFrame
	// below).
	refSavings := e.refFrameEntropySavingsBitsForFrame(false, macroblocks, true /*refreshGolden*/, false)
	if refSavings <= 0 {
		t.Fatalf("ref-frame entropy savings (refreshGolden=true) = %d, want positive test fixture", refSavings)
	}
	projectedBitsBeforeSavings := refSavings + 123
	got, _, gotRef := e.projectedFrameSizeBitsFromRateWithSavings(false, macroblocks, projectedBitsBeforeSavings<<8, true, false)
	if gotRef != refSavings {
		t.Fatalf("projection refFrameSavings = %d, want %d", gotRef, refSavings)
	}
	if got != 123 {
		t.Fatalf("projected frame size = %d, want prepack bits %d - savings %d = 123", got, projectedBitsBeforeSavings, refSavings)
	}
}

// TestRefFrameEntropySavingsZeroForConvertHookFrame pins the parity rule:
// for inter frames where libvpx's vp8/encoder/encodeframe.c trailing
// vp8_convert_rfct_to_prob hook fires (single-layer non-GF/AR-refresh,
// or any multi-layer case), govpx must NOT subtract the heuristic-biased
// ref-frame entropy savings because libvpx's
// vp8_estimate_entropy_savings already sees old==new (the convert hook
// pre-overwrote cpi->prob_*_coded with the rfct-derived probabilities).
// See refFrameEntropySavingsBitsForFrame for the gate.
func TestRefFrameEntropySavingsZeroForConvertHookFrame(t *testing.T) {
	const macroblocks = 16
	modes := make([]vp8enc.InterFrameMacroblockMode, macroblocks)
	for i := range modes {
		modes[i].Mode = vp8common.ZeroMV
		modes[i].RefFrame = vp8common.LastFrame
	}
	e := &VP8Encoder{
		interFrameModes: modes,
		refProbIntra:    200,
		refProbLast:     64,
		refProbGolden:   128,
	}
	got := e.refFrameEntropySavingsBitsForFrame(false, macroblocks, false, false)
	if got != 0 {
		t.Fatalf("refFrameEntropySavingsBitsForFrame(refreshGolden=false, refreshAltRef=false) = %d, want 0 (convert hook fires)", got)
	}
}

func TestCoefficientEntropySavingsBitsIncludesCoefficientSavings(t *testing.T) {
	const rows, cols = 16, 16
	modes := make([]vp8enc.KeyFrameMacroblockMode, rows*cols)
	coeffs := make([]vp8enc.MacroblockCoefficients, rows*cols)
	for i := range modes {
		modes[i] = vp8enc.KeyFrameMacroblockMode{YMode: vp8common.DCPred, UVMode: vp8common.DCPred}
	}
	e := &VP8Encoder{
		opts: EncoderOptions{
			Width:  cols * 16,
			Height: rows * 16,
		},
		keyFrameModes:  modes,
		keyFrameCoeffs: coeffs,
		tokenAbove:     make([]vp8enc.TokenContextPlanes, cols),
	}
	if savings := e.coefficientEntropySavingsBits(true, rows*cols); savings <= 0 {
		t.Fatalf("coefficient entropy savings = %d, want positive", savings)
	}
}
