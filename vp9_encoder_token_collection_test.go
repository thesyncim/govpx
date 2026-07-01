package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestVP9EncoderCountTokenCollectionBuildsEOSBLists(t *testing.T) {
	const width, height = 64, 128
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	dst := make([]byte, 65536)
	keySrc := vp9test.NewCheckerYCbCr(width, height, 32, 224, 128, 128)
	interSrc := vp9test.NewCheckerYCbCr(width, height, 48, 208, 128, 128)

	if _, err := e.EncodeInto(keySrc, dst); err != nil {
		t.Fatalf("key EncodeInto: %v", err)
	}
	assertVP9CountTokenList(t, e, "keyframe row 0", 0, false)
	assertVP9CountTokenList(t, e, "keyframe row 1", 1, false)

	if _, err := e.EncodeInto(interSrc, dst); err != nil {
		t.Fatalf("inter EncodeInto: %v", err)
	}
	assertVP9CountTokenList(t, e, "inter row 0", 0, false)
	assertVP9CountTokenList(t, e, "inter row 1", 1, false)

	if _, err := e.EncodeInto(interSrc, dst); err != nil {
		t.Fatalf("inter skip EncodeInto: %v", err)
	}
	assertVP9CountTokenList(t, e, "inter repeat row 0", 0, false)
	assertVP9CountTokenList(t, e, "inter repeat row 1", 1, false)
}

func TestVP9EncoderCountTokenCollectionTerminatesNoResidueLeaves(t *testing.T) {
	const width, height = 64, 128
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	dst := make([]byte, 65536)
	src := vp9test.NewYCbCr(width, height, 128, 128, 128)

	if _, err := e.EncodeInto(src, dst); err != nil {
		t.Fatalf("flat key EncodeInto: %v", err)
	}
	assertVP9CountTokenList(t, e, "flat key row 0", 0, true)
	assertVP9CountTokenList(t, e, "flat key row 1", 1, true)

	if _, err := e.EncodeInto(src, dst); err != nil {
		t.Fatalf("flat inter EncodeInto: %v", err)
	}
	assertVP9CountTokenList(t, e, "flat inter row 0", 0, true)
	assertVP9CountTokenList(t, e, "flat inter row 1", 1, true)
}

func TestVP9EncoderThreadedCountTokenCollectionBuildsTileLists(t *testing.T) {
	const width, height = 1280, 128
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              width,
		Height:             height,
		Threads:            4,
		Deadline:           DeadlineRealtime,
		CpuUsed:            8,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  800,
		NoiseSensitivity:   0,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	dst := make([]byte, 1<<22)
	keySrc := vp9test.NewCheckerYCbCr(width, height, 32, 224, 128, 128)
	interSrc := vp9test.NewCheckerYCbCr(width, height, 48, 208, 128, 128)

	if _, err := e.EncodeInto(keySrc, dst); err != nil {
		t.Fatalf("key EncodeInto: %v", err)
	}
	for tileCol := range 4 {
		assertVP9CountTokenListAt(t, e, "threaded key", tileCol, 0, false)
	}

	if _, err := e.EncodeInto(interSrc, dst); err != nil {
		t.Fatalf("inter EncodeInto: %v", err)
	}
	for tileCol := range 4 {
		assertVP9CountTokenListAt(t, e, "threaded inter", tileCol, 0, false)
	}
	if e.vp9TilePool == nil {
		t.Fatal("threaded token replay did not initialize tile worker pool")
	}
	for tileCol := range 4 {
		if !e.vp9TilePool.encodeJobs[tileCol].replayTokens {
			t.Fatalf("tile %d encode job did not use staged token replay", tileCol)
		}
	}
}

func TestVP9CountPassInterLeafReplayRequiresPreservedState(t *testing.T) {
	e := &VP9Encoder{}
	e.refFrames[0].valid = true
	e.vp9TokenReplay.active = true
	inter := &vp9InterEncodeState{}
	decision := vp9InterModeDecision{
		refFrame:       vp9dec.LastFrame,
		secondRefFrame: vp9dec.NoRefFrame,
		refSlot:        0,
		mode:           common.ZeroMv,
		interpFilter:   vp9dec.InterpEighttap,
		txSize:         common.Tx8x8,
	}

	if e.canReplayVP9CountPassInterLeaf(inter, decision, common.Block16x16, false) {
		t.Fatal("replay fast path accepted a leaf without preserved coding state")
	}
	e.vp9CountCodingPreserved = true
	if !e.canReplayVP9CountPassInterLeaf(inter, decision, common.Block16x16, false) {
		t.Fatal("replay fast path rejected a preserved single-reference inter leaf")
	}
	e.denoiser.allocated = true
	e.denoiser.sensitivity = 2
	e.denoiser.level = vp9DenoiserMedium
	if e.canReplayVP9CountPassInterLeaf(inter, decision, common.Block16x16, false) {
		t.Fatal("replay fast path accepted a denoiser-active leaf")
	}
	e.denoiser = vp9DenoiserState{}
	e.activeMapEnabled = true
	if e.canReplayVP9CountPassInterLeaf(inter, decision, common.Block16x16, false) {
		t.Fatal("replay fast path accepted a dynamic segment-map leaf")
	}
	e.activeMapEnabled = false
	decision.isCompound = true
	decision.secondRefFrame = vp9dec.GoldenFrame
	if e.canReplayVP9CountPassInterLeaf(inter, decision, common.Block16x16, false) {
		t.Fatal("replay fast path accepted a compound leaf in the single-ref slice")
	}
}

func assertVP9CountTokenList(t *testing.T, e *VP9Encoder, label string,
	tileSBRow int, allowOnlyEOSB bool,
) {
	t.Helper()
	assertVP9CountTokenListAt(t, e, label, 0, tileSBRow, allowOnlyEOSB)
}

func assertVP9CountTokenListAt(t *testing.T, e *VP9Encoder, label string,
	tileCol, tileSBRow int, allowOnlyEOSB bool,
) {
	t.Helper()
	if e.vp9TokenFrame.Used <= 0 {
		t.Fatalf("%s token frame used = %d, want tokens", label, e.vp9TokenFrame.Used)
	}
	list, ok := e.vp9CountTokenListForTileSBRow(0, tileCol, tileSBRow)
	if !ok {
		t.Fatalf("%s token list missing for tile col %d row %d",
			label, tileCol, tileSBRow)
	}
	tokens, ok := e.vp9TokenFrame.TokensForList(list)
	if !ok {
		t.Fatalf("%s token list slice rejected: %+v", label, list)
	}
	if !vp9TokenListHasOnlyEOSBTerminatedLeaves(tokens) {
		t.Fatalf("%s token list is not EOSB terminated", label)
	}
	if got := vp9TokenListEOSBCount(tokens); got == 0 {
		t.Fatalf("%s token list EOSB count = 0, want at least one leaf", label)
	}
	if allowOnlyEOSB && len(tokens) != vp9TokenListEOSBCount(tokens) {
		t.Fatalf("%s token list contains coefficient tokens in skip fixture: len=%d eosb=%d",
			label, len(tokens), vp9TokenListEOSBCount(tokens))
	}
}
