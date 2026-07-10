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
	if !e.vp9CountCodingPreserved {
		t.Fatal("threaded inter count pass did not preserve coding state")
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
		t.Fatal("replay fast path accepted a denoiser-active leaf without committed count state")
	}
	e.vp9DenoiserCountStateReady = true
	if !e.canReplayVP9CountPassInterLeaf(inter, decision, common.Block16x16, false) {
		t.Fatal("replay fast path rejected a denoiser leaf with committed count state")
	}
	e.denoiser = vp9DenoiserState{}
	e.vp9DenoiserCountStateReady = false
	e.activeMapEnabled = true
	if e.canReplayVP9CountPassInterLeaf(inter, decision, common.Block16x16, false) {
		t.Fatal("replay fast path accepted a dynamic segment-map leaf")
	}
	e.activeMapEnabled = false
	decision.isCompound = true
	decision.secondRefFrame = vp9dec.GoldenFrame
	if e.canReplayVP9CountPassInterLeaf(inter, decision, common.Block16x16, false) {
		t.Fatal("replay fast path accepted a compound leaf without a valid second ref slot")
	}
	decision.secondRefSlot = len(e.refFrames)
	if e.canReplayVP9CountPassInterLeaf(inter, decision, common.Block16x16, false) {
		t.Fatal("replay fast path accepted a compound leaf with an out-of-range second ref slot")
	}
	decision.secondRefSlot = 1
	if e.canReplayVP9CountPassInterLeaf(inter, decision, common.Block16x16, false) {
		t.Fatal("replay fast path accepted a compound leaf with an invalid second ref")
	}
	e.refFrames[1].valid = true
	if !e.canReplayVP9CountPassInterLeaf(inter, decision, common.Block16x16, false) {
		t.Fatal("replay fast path rejected a preserved compound inter leaf")
	}
}

func TestVP9DenoiserCountStateCommitRequiresAllLeafReplay(t *testing.T) {
	e := &VP9Encoder{}
	e.opts.NoiseSensitivity = 2
	e.denoiser.allocated = true
	e.denoiser.sensitivity = 2
	e.denoiser.level = vp9DenoiserMedium
	e.vp9CountCodingPreserved = true
	e.sf.DefaultMinPartitionSize = common.Block8x8
	seg := vp9dec.SegmentationParams{}

	if !e.canCommitVP9DenoiserCountState(true, vp9ModeTreeInterSource, &seg) {
		t.Fatal("eligible denoiser count state was not committable")
	}
	if e.canCommitVP9DenoiserCountState(false, vp9ModeTreeInterSource, &seg) {
		t.Fatal("denoiser count state committed without token replay")
	}
	e.activeMapEnabled = true
	if e.canCommitVP9DenoiserCountState(true, vp9ModeTreeInterSource, &seg) {
		t.Fatal("denoiser count state committed with active segment-map coding")
	}
	e.activeMapEnabled = false
	seg.Enabled = true
	seg.FeatureMask[1] = 1 << uint(vp9dec.SegLvlRefFrame)
	seg.FeatureData[1][vp9dec.SegLvlRefFrame] = int16(vp9dec.GoldenFrame)
	if e.canCommitVP9DenoiserCountState(true, vp9ModeTreeInterSource, &seg) {
		t.Fatal("denoiser count state committed with a forced-reference segment")
	}
}

func TestVP9CountWorkerDecisionCachesPingPongOwnership(t *testing.T) {
	e := &VP9Encoder{
		vp9LeafInterDecisions:          make([]vp9LeafInterDecisionEntry, 2),
		vp9LeafInterDecisionsRows:      1,
		vp9LeafInterDecisionsCols:      2,
		vp9LeafInterDecisionsVer:       3,
		vp9InterPartitionDecisions:     make([]vp9InterPartitionDecisionEntry, 2),
		vp9InterPartitionDecisionsRows: 1,
		vp9InterPartitionDecisionsCols: 2,
		vp9InterPartitionDecisionsVer:  4,
		vp9LeafKeyframeDecisions:       make([]vp9LeafKeyframeDecisionEntry, 2),
		vp9LeafKeyframeDecisionsRows:   1,
		vp9LeafKeyframeDecisionsCols:   2,
		vp9LeafKeyframeDecisionsVer:    5,
	}
	w := &VP9Encoder{
		vp9LeafInterDecisions:          make([]vp9LeafInterDecisionEntry, 3),
		vp9LeafInterDecisionsRows:      3,
		vp9LeafInterDecisionsCols:      1,
		vp9LeafInterDecisionsVer:       13,
		vp9InterPartitionDecisions:     make([]vp9InterPartitionDecisionEntry, 3),
		vp9InterPartitionDecisionsRows: 3,
		vp9InterPartitionDecisionsCols: 1,
		vp9InterPartitionDecisionsVer:  14,
		vp9LeafKeyframeDecisions:       make([]vp9LeafKeyframeDecisionEntry, 3),
		vp9LeafKeyframeDecisionsRows:   3,
		vp9LeafKeyframeDecisionsCols:   1,
		vp9LeafKeyframeDecisionsVer:    15,
	}
	eInter := &e.vp9LeafInterDecisions[0]
	ePart := &e.vp9InterPartitionDecisions[0]
	eKey := &e.vp9LeafKeyframeDecisions[0]
	wInter := &w.vp9LeafInterDecisions[0]
	wPart := &w.vp9InterPartitionDecisions[0]
	wKey := &w.vp9LeafKeyframeDecisions[0]

	e.adoptVP9CountWorkerLeafDecisionCaches(w)

	if &e.vp9LeafInterDecisions[0] != wInter ||
		&e.vp9InterPartitionDecisions[0] != wPart ||
		&e.vp9LeafKeyframeDecisions[0] != wKey {
		t.Fatal("dispatcher did not adopt worker cache ownership")
	}
	if &w.vp9LeafInterDecisions[0] != eInter ||
		&w.vp9InterPartitionDecisions[0] != ePart ||
		&w.vp9LeafKeyframeDecisions[0] != eKey {
		t.Fatal("worker did not receive the prior dispatcher cache ownership")
	}
	if e.vp9LeafInterDecisionsRows != 3 || e.vp9LeafInterDecisionsCols != 1 ||
		e.vp9LeafInterDecisionsVer != 13 ||
		e.vp9InterPartitionDecisionsRows != 3 ||
		e.vp9InterPartitionDecisionsCols != 1 ||
		e.vp9InterPartitionDecisionsVer != 14 ||
		e.vp9LeafKeyframeDecisionsRows != 3 ||
		e.vp9LeafKeyframeDecisionsCols != 1 ||
		e.vp9LeafKeyframeDecisionsVer != 15 {
		t.Fatal("dispatcher did not adopt worker cache metadata")
	}
}

func TestVP9CountPassIntraLeafReplayRequiresPreservedState(t *testing.T) {
	e := &VP9Encoder{}
	e.vp9TokenReplay.active = true
	inter := &vp9InterEncodeState{}
	decision := vp9InterModeDecision{
		intra:          true,
		refFrame:       vp9dec.IntraFrame,
		secondRefFrame: vp9dec.NoRefFrame,
		mode:           common.DcPred,
		txSize:         common.Tx8x8,
		uvMode:         common.TmPred,
	}

	if e.canReplayVP9CountPassIntraLeaf(inter, decision, common.Block16x16) {
		t.Fatal("intra replay accepted a leaf without preserved coding state")
	}
	e.vp9CountCodingPreserved = true
	if !e.canReplayVP9CountPassIntraLeaf(inter, decision, common.Block16x16) {
		t.Fatal("intra replay rejected a preserved intra leaf")
	}
	e.activeMapEnabled = true
	if e.canReplayVP9CountPassIntraLeaf(inter, decision, common.Block16x16) {
		t.Fatal("intra replay accepted a dynamic segment-map leaf")
	}
	e.activeMapEnabled = false
	if e.canReplayVP9CountPassIntraLeaf(inter, decision, common.Block4x4) {
		t.Fatal("intra replay accepted a sub-8x8 leaf")
	}
	decision.intra = false
	decision.refFrame = vp9dec.LastFrame
	if e.canReplayVP9CountPassIntraLeaf(inter, decision, common.Block16x16) {
		t.Fatal("intra replay accepted an inter leaf")
	}
}

func TestVP9CountPassInterLeafReplayRestoresCompoundModeInfo(t *testing.T) {
	e := &VP9Encoder{}
	e.refFrames[0].valid = true
	e.refFrames[1].valid = true
	inter := &vp9InterEncodeState{}
	decision := vp9InterModeDecision{
		refFrame:       vp9dec.LastFrame,
		secondRefFrame: vp9dec.GoldenFrame,
		refSlot:        0,
		secondRefSlot:  1,
		isCompound:     true,
		mode:           common.NewMv,
		mv: [2]vp9dec.MV{
			{Row: 8, Col: -16},
			{Row: -4, Col: 12},
		},
		bmi: [4]vp9dec.Bmi{
			{AsMode: common.NewMv, AsMv: [2]vp9dec.MV{{Row: 1, Col: 2}, {Row: 3, Col: 4}}},
			{AsMode: common.NearMv, AsMv: [2]vp9dec.MV{{Row: 5, Col: 6}, {Row: 7, Col: 8}}},
			{AsMode: common.NearestMv, AsMv: [2]vp9dec.MV{{Row: 9, Col: 10}, {Row: 11, Col: 12}}},
			{AsMode: common.ZeroMv, AsMv: [2]vp9dec.MV{{Row: 13, Col: 14}, {Row: 15, Col: 16}}},
		},
		interpFilter: vp9dec.InterpEighttapSmooth,
		txSize:       common.Tx8x8,
	}

	var mi vp9dec.NeighborMi
	e.applyVP9CountPassInterLeaf(inter, &mi, decision, common.Block16x16)

	if mi.Mode != decision.mode {
		t.Fatalf("mode = %v, want %v", mi.Mode, decision.mode)
	}
	if mi.RefFrame != [2]int8{vp9dec.LastFrame, vp9dec.GoldenFrame} {
		t.Fatalf("ref frames = %v, want LAST/GOLDEN", mi.RefFrame)
	}
	if mi.Mv != decision.mv {
		t.Fatalf("mv = %v, want %v", mi.Mv, decision.mv)
	}
	if mi.Bmi != decision.bmi {
		t.Fatalf("bmi = %v, want %v", mi.Bmi, decision.bmi)
	}
	if got := vp9dec.InterpFilter(mi.InterpFilter); got != decision.interpFilter {
		t.Fatalf("interp filter = %v, want %v", got, decision.interpFilter)
	}
	if mi.TxSize != decision.txSize {
		t.Fatalf("tx size = %v, want %v", mi.TxSize, decision.txSize)
	}
	if inter.ref != &e.refFrames[0] {
		t.Fatalf("primary ref pointer was not restored")
	}

	decision.isCompound = false
	decision.secondRefFrame = vp9dec.GoldenFrame
	e.applyVP9CountPassInterLeaf(inter, &mi, decision, common.Block16x16)
	if mi.RefFrame != [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame} {
		t.Fatalf("single-ref replay ref frames = %v, want LAST/NO_REF", mi.RefFrame)
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
	frame := &e.vp9TokenFrame
	if e.vp9ThreadedTokenReplayReady && e.vp9TilePool != nil &&
		tileCol >= 0 && tileCol < len(e.vp9TilePool.countTokens) {
		frame = &e.vp9TilePool.countTokens[tileCol]
	}
	if frame.Used <= 0 {
		t.Fatalf("%s token frame used = %d, want tokens", label, frame.Used)
	}
	idx, ok := frame.TokenListIndex(0, tileCol, tileSBRow)
	if !ok {
		t.Fatalf("%s token list missing for tile col %d row %d",
			label, tileCol, tileSBRow)
	}
	list := frame.Lists[idx]
	if list.Count == 0 {
		t.Fatalf("%s token list empty for tile col %d row %d",
			label, tileCol, tileSBRow)
	}
	tokens, ok := frame.TokensForList(list)
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
