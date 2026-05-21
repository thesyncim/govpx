package govpx

import (
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

// TestCyclicRefreshSegmentationEmitsAggressiveDenoiseAltLF mirrors libvpx
// vp8/encoder/onyx_if.c cyclic_background_refresh: when the aggressive
// denoiser is engaged AND the current Q is below the per-mode qp_thresh AND
// the frame is far enough past the last key frame, the encoder must drop
// the Q delta and instead ship an alt-LF delta of -40 on segment 1
// (MB_LVL_ALT_LF feature index 1, NOT MB_LVL_ALT_Q feature index 0).
func TestCyclicRefreshSegmentationEmitsAggressiveDenoiseAltLF(t *testing.T) {
	t.Parallel()

	e := &VP8Encoder{
		opts: EncoderOptions{
			NoiseSensitivity: 3, // aggressive mode
			ErrorResilient:   true,
		},
	}
	e.cyclicRefreshConfigured = true
	// Drive the aggressive-denoise gate: Q below qp_thresh (80) and
	// frames_since_key > 2*consec_zerolast (2*15 = 30).
	e.rc.mode = RateControlCBR
	e.rc.currentQuantizer = 50
	e.rc.framesSinceKeyframe = 60
	e.denoiser.allocated = true
	e.denoiser.mode, e.denoiser.params = vp8enc.DenoiserSetParameters(vp8enc.DenoiserModeForSensitivity(e.opts.NoiseSensitivity))

	if !e.aggressiveDenoiseSegmentationActive() {
		t.Fatalf("aggressiveDenoiseSegmentationActive=false, want true (Q=%d framesSinceKey=%d)", e.rc.currentQuantizer, e.rc.framesSinceKeyframe)
	}

	cfg := e.cyclicRefreshSegmentationConfig(false)
	if !cfg.Enabled || !cfg.UpdateMap || !cfg.UpdateData {
		t.Fatalf("seg cfg = %+v, want enabled with map+data update", cfg)
	}
	// MB_LVL_ALT_LF feature mode must be active on segment 1 with delta -40.
	if !cfg.FeatureEnabled[vp8common.MBLvlAltLF][vp8enc.StaticSegmentID] {
		t.Fatalf("alt-LF feature enabled[%d]=false, want true", vp8enc.StaticSegmentID)
	}
	if got := cfg.FeatureData[vp8common.MBLvlAltLF][vp8enc.StaticSegmentID]; got != aggressiveDenoiseAltLFDelta {
		t.Fatalf("alt-LF feature data[%d] = %d, want %d", vp8enc.StaticSegmentID, got, aggressiveDenoiseAltLFDelta)
	}
	// MB_LVL_ALT_Q must NOT be enabled in this branch (libvpx writes
	// 0 there but the corresponding "enabled" bit in our config stays
	// off, which produces the same on-wire result for delta-zero).
	if cfg.FeatureEnabled[vp8common.MBLvlAltQ][vp8enc.StaticSegmentID] {
		t.Fatalf("alt-Q feature enabled[%d]=true, want false (aggressive-denoise branch must not ship a Q delta)", vp8enc.StaticSegmentID)
	}
}

// TestCyclicRefreshSegmentationFallsBackToAltQOutsideAggressiveBranch
// confirms the non-aggressive branch keeps shipping the libvpx Q delta
// (cpi->cyclic_refresh_q - Q) on the MB_LVL_ALT_Q feature, never an alt-LF
// delta. This pins the inverse of the aggressive-denoise switch above.
func TestCyclicRefreshSegmentationFallsBackToAltQOutsideAggressiveBranch(t *testing.T) {
	t.Parallel()

	e := &VP8Encoder{opts: EncoderOptions{ErrorResilient: true}}
	e.cyclicRefreshConfigured = true
	e.rc.mode = RateControlCBR
	e.rc.currentQuantizer = 100

	cfg := e.cyclicRefreshSegmentationConfig(false)
	if !cfg.Enabled {
		t.Fatalf("seg cfg = %+v, want enabled", cfg)
	}
	if cfg.FeatureEnabled[vp8common.MBLvlAltLF][vp8enc.StaticSegmentID] {
		t.Fatalf("alt-LF feature enabled[%d]=true, want false (non-aggressive branch must use ALT_Q)", vp8enc.StaticSegmentID)
	}
	if !cfg.FeatureEnabled[vp8common.MBLvlAltQ][vp8enc.StaticSegmentID] {
		t.Fatalf("alt-Q feature enabled[%d]=false, want true", vp8enc.StaticSegmentID)
	}
	wantDelta := int8(e.rc.currentQuantizer/2 - e.rc.currentQuantizer)
	if got := cfg.FeatureData[vp8common.MBLvlAltQ][vp8enc.StaticSegmentID]; got != wantDelta {
		t.Fatalf("alt-Q feature data[%d] = %d, want %d", vp8enc.StaticSegmentID, got, wantDelta)
	}
}

// TestKeyFrameBitstreamCarriesAltLFDelta drives the bitstream writer with a
// constructed SegmentationConfig that uses MB_LVL_ALT_LF and asserts the
// decoded segmentation header carries the same feature mode + delta value.
// This pins that the encoder's ALT_LF segment data round-trips through
// vp8/encoder/state.go writeSegmentationHeader and the decoder's parser.
func TestKeyFrameBitstreamCarriesAltLFDelta(t *testing.T) {
	t.Parallel()

	segmentation := vp8enc.SegmentationConfig{
		Enabled:    true,
		UpdateMap:  true,
		UpdateData: true,
	}
	segmentation.FeatureEnabled[vp8common.MBLvlAltLF][vp8enc.StaticSegmentID] = true
	segmentation.FeatureData[vp8common.MBLvlAltLF][vp8enc.StaticSegmentID] = aggressiveDenoiseAltLFDelta
	for i := range segmentation.TreeProbs {
		segmentation.TreeProbUpdated[i] = true
		segmentation.TreeProbs[i] = 128
	}
	keyModes := []vp8enc.KeyFrameMacroblockMode{{SegmentID: vp8enc.StaticSegmentID, YMode: vp8common.DCPred, UVMode: vp8common.DCPred}}
	keyPacket := make([]byte, 4096)
	keyN, err := vp8enc.WriteZeroKeyFrame(keyPacket, 16, 16, vp8enc.KeyFrameStateConfig{
		TokenPartition: vp8common.OnePartition,
		BaseQIndex:     32,
		Segmentation:   segmentation,
	}, keyModes)
	if err != nil {
		t.Fatalf("WriteZeroKeyFrame returned error: %v", err)
	}
	state := packetState(t, keyPacket[:keyN])
	if !state.Segmentation.Enabled || !state.Segmentation.UpdateMap || !state.Segmentation.UpdateData {
		t.Fatalf("segmentation header = %+v, want enabled with map+data update", state.Segmentation)
	}
	// Decoded ALT_LF feature data on segment 1 must equal the delta the
	// encoder shipped; the matching ALT_Q slot must remain zero (the
	// aggressive-denoise branch suppresses the Q delta).
	if got := state.Segmentation.FeatureData[vp8common.MBLvlAltLF][vp8enc.StaticSegmentID]; got != aggressiveDenoiseAltLFDelta {
		t.Fatalf("decoded alt-LF segment %d = %d, want %d", vp8enc.StaticSegmentID, got, aggressiveDenoiseAltLFDelta)
	}
	if got := state.Segmentation.FeatureData[vp8common.MBLvlAltQ][vp8enc.StaticSegmentID]; got != 0 {
		t.Fatalf("decoded alt-Q segment %d = %d, want 0 (no Q delta in alt-LF branch)", vp8enc.StaticSegmentID, got)
	}
}

// TestLoopFilterSegmentationHeaderTranslatesAltLFFeatureData pins the helper
// that converts the encoder's writer-shaped SegmentationConfig into the
// decoder's reader-shaped SegmentationHeader for the in-encoder
// reconstruction loop filter. ALT_LF deltas must propagate so the encoder's
// post-LF reconstruction matches what the decoder will compute from the
// bitstream; disabled feature slots must round-trip as zero.
func TestLoopFilterSegmentationHeaderTranslatesAltLFFeatureData(t *testing.T) {
	t.Parallel()

	cfg := vp8enc.SegmentationConfig{Enabled: true, AbsDelta: false, UpdateData: true, UpdateMap: true}
	cfg.FeatureEnabled[vp8common.MBLvlAltLF][vp8enc.StaticSegmentID] = true
	cfg.FeatureData[vp8common.MBLvlAltLF][vp8enc.StaticSegmentID] = aggressiveDenoiseAltLFDelta
	cfg.FeatureEnabled[vp8common.MBLvlAltQ][2] = true
	cfg.FeatureData[vp8common.MBLvlAltQ][2] = -7
	for i := range cfg.TreeProbs {
		cfg.TreeProbs[i] = uint8(100 + i)
	}

	header := loopFilterSegmentationHeader(cfg)
	if !header.Enabled || header.AbsDelta || !header.UpdateData || !header.UpdateMap {
		t.Fatalf("translated header flags = %+v, want enabled delta-mode with map+data update", header)
	}
	if got := header.FeatureData[vp8common.MBLvlAltLF][vp8enc.StaticSegmentID]; got != aggressiveDenoiseAltLFDelta {
		t.Fatalf("translated alt-LF[%d] = %d, want %d", vp8enc.StaticSegmentID, got, aggressiveDenoiseAltLFDelta)
	}
	if got := header.FeatureData[vp8common.MBLvlAltQ][2]; got != -7 {
		t.Fatalf("translated alt-Q[2] = %d, want -7", got)
	}
	// Disabled slots must stay zero (the writer encodes "disabled" as a
	// single 0 bit, the parser leaves the matching FeatureData entry
	// untouched at zero, so the encoder-side mirror must do the same).
	if got := header.FeatureData[vp8common.MBLvlAltLF][2]; got != 0 {
		t.Fatalf("translated alt-LF[2] = %d, want 0", got)
	}
	for i := range header.TreeProbs {
		if header.TreeProbs[i] != cfg.TreeProbs[i] {
			t.Fatalf("translated tree prob[%d] = %d, want %d", i, header.TreeProbs[i], cfg.TreeProbs[i])
		}
	}

	// A disabled segmentation config must translate to a disabled header
	// regardless of stale feature_data — the decoder's
	// loopFilterFrameConfig short-circuits on segmentation.Enabled = false.
	disabled := loopFilterSegmentationHeader(vp8enc.SegmentationConfig{})
	if disabled.Enabled || disabled != (vp8dec.SegmentationHeader{}) {
		t.Fatalf("disabled translation = %+v, want zero header", disabled)
	}
}

func TestSegmentationConfigForLoopFilterLevelUsesInstalledPacketAltLFData(t *testing.T) {
	t.Parallel()

	e := &VP8Encoder{}
	cfg := vp8enc.SegmentationConfig{Enabled: true, UpdateData: true}
	cfg.FeatureEnabled[vp8common.MBLvlAltLF][1] = true
	cfg.FeatureData[vp8common.MBLvlAltLF][1] = -3
	cfg.FeatureEnabled[vp8common.MBLvlAltLF][2] = true
	cfg.FeatureData[vp8common.MBLvlAltLF][2] = 4
	cfg.FeatureEnabled[vp8common.MBLvlAltLF][3] = true
	cfg.FeatureData[vp8common.MBLvlAltLF][3] = -2

	got := e.segmentationConfigForLoopFilterLevel(cfg, 0)
	if got.FeatureEnabled[vp8common.MBLvlAltLF][1] || got.FeatureData[vp8common.MBLvlAltLF][1] != 0 {
		t.Fatalf("uninstalled ALT_LF = enabled:%t data:%d, want zero packet state", got.FeatureEnabled[vp8common.MBLvlAltLF][1], got.FeatureData[vp8common.MBLvlAltLF][1])
	}
	if got.FeatureEnabled[vp8common.MBLvlAltLF][2] || got.FeatureData[vp8common.MBLvlAltLF][2] != 0 {
		t.Fatalf("uninstalled positive ALT_LF = enabled:%t data:%d, want zero packet state", got.FeatureEnabled[vp8common.MBLvlAltLF][2], got.FeatureData[vp8common.MBLvlAltLF][2])
	}

	e.loopFilterSegmentLF[1] = -5
	e.loopFilterSegmentLF[3] = 2
	got = e.segmentationConfigForLoopFilterLevel(cfg, 3)
	if !got.FeatureEnabled[vp8common.MBLvlAltLF][1] || got.FeatureData[vp8common.MBLvlAltLF][1] != -5 {
		t.Fatalf("current cfg leaked into installed ALT_LF[1] = enabled:%t data:%d, want installed -5", got.FeatureEnabled[vp8common.MBLvlAltLF][1], got.FeatureData[vp8common.MBLvlAltLF][1])
	}
	if got.FeatureEnabled[vp8common.MBLvlAltLF][2] || got.FeatureData[vp8common.MBLvlAltLF][2] != 0 {
		t.Fatalf("cleared installed ALT_LF[2] = enabled:%t data:%d, want zero", got.FeatureEnabled[vp8common.MBLvlAltLF][2], got.FeatureData[vp8common.MBLvlAltLF][2])
	}
	if !got.FeatureEnabled[vp8common.MBLvlAltLF][3] || got.FeatureData[vp8common.MBLvlAltLF][3] != 2 {
		t.Fatalf("installed ALT_LF[3] = enabled:%t data:%d, want 2", got.FeatureEnabled[vp8common.MBLvlAltLF][3], got.FeatureData[vp8common.MBLvlAltLF][3])
	}

	cfg.AbsDelta = true
	got = e.segmentationConfigForLoopFilterLevel(cfg, 0)
	if !got.FeatureEnabled[vp8common.MBLvlAltLF][1] || got.FeatureData[vp8common.MBLvlAltLF][1] != -5 {
		t.Fatalf("absolute installed ALT_LF[1] = enabled:%t data:%d, want -5", got.FeatureEnabled[vp8common.MBLvlAltLF][1], got.FeatureData[vp8common.MBLvlAltLF][1])
	}
}

func TestLoopFilterFastPickerUsesInstalledAltLF(t *testing.T) {
	t.Parallel()

	e := newSizedTestEncoder(t, 16, 16)
	e.loopFilterSegmentLF[vp8enc.StaticSegmentID] = -7
	cfg := vp8enc.SegmentationConfig{Enabled: true, AbsDelta: false, UpdateData: true, UpdateMap: true}
	cfg.FeatureEnabled[vp8common.MBLvlAltLF][vp8enc.StaticSegmentID] = true
	cfg.FeatureData[vp8common.MBLvlAltLF][vp8enc.StaticSegmentID] = aggressiveDenoiseAltLFDelta

	ctx := e.newLoopFilterPickContext(sourceImageFromPublic(testImage(16, 16)), vp8common.InterFrame, 0, 1, 1, 1, cfg)
	if got := ctx.fastFrameConfig.SegmentLF[vp8enc.StaticSegmentID]; got != -7 {
		t.Fatalf("fast picker alt-LF = %d, want installed previous value -7", got)
	}
	if got := ctx.fullFrameConfig.SegmentLF[vp8enc.StaticSegmentID]; got != aggressiveDenoiseAltLFDelta {
		t.Fatalf("full picker alt-LF = %d, want current value %d", got, aggressiveDenoiseAltLFDelta)
	}
}
