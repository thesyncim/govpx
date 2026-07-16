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

func TestVP9FinalInterLeafDecisionOmissionRequiresPurePackEnvelope(t *testing.T) {
	e := &VP9Encoder{}
	e.vp9TokenCollect.active = true
	e.sf.FrameParameterUpdate = 0
	inter := &vp9InterEncodeState{
		counts:              &e.frameCounts,
		preserveCodingState: true,
	}

	if !e.canOmitVP9FinalInterLeafDecision(inter, common.TxModeSelect) {
		t.Fatal("pure-pack count leaf retained the finalized decision cache")
	}

	tests := []struct {
		name string
		set  func(*VP9Encoder, *vp9InterEncodeState)
	}{
		{name: "no preserved coding state", set: func(_ *VP9Encoder, inter *vp9InterEncodeState) {
			inter.preserveCodingState = false
		}},
		{name: "inactive token collection", set: func(e *VP9Encoder, _ *vp9InterEncodeState) {
			e.vp9TokenCollect.active = false
		}},
		{name: "svc", set: func(e *VP9Encoder, _ *vp9InterEncodeState) { e.svc.UseSvc = true }},
		{name: "denoiser", set: func(e *VP9Encoder, _ *vp9InterEncodeState) {
			e.denoiser.allocated = true
			e.denoiser.sensitivity = 2
			e.denoiser.level = vp9DenoiserMedium
		}},
		{name: "active segment map", set: func(e *VP9Encoder, _ *vp9InterEncodeState) {
			e.activeMapEnabled = true
		}},
		{name: "tx mode demotion", set: func(e *VP9Encoder, _ *vp9InterEncodeState) {
			e.sf.FrameParameterUpdate = 1
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			copyEncoder := *e
			copyInter := *inter
			tc.set(&copyEncoder, &copyInter)
			if copyEncoder.canOmitVP9FinalInterLeafDecision(&copyInter, common.TxModeSelect) {
				t.Fatal("finalized decision cache omitted outside pure-pack envelope")
			}
		})
	}

	e.sf.FrameParameterUpdate = 1
	if !e.canOmitVP9FinalInterLeafDecision(inter, common.Allow32x32) {
		t.Fatal("fixed tx mode retained cache despite disabled demotion path")
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
	e.vp9CountCodingPreserved = false
	inter := &vp9InterEncodeState{preserveCodingState: true}
	if !e.canDispatchVP9DenoiserCountRows(inter, vp9ModeTreeInterSource, &seg) {
		t.Fatal("eligible denoiser count rows were rejected before preservation finalized")
	}
	inter.preserveCodingState = false
	if e.canDispatchVP9DenoiserCountRows(inter, vp9ModeTreeInterSource, &seg) {
		t.Fatal("denoiser count rows accepted without prospective preservation")
	}
	e.vp9CountCodingPreserved = true
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

func TestVP9DenoiserCountRollbackRestoresCallerInput(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:            width,
		Height:           height,
		NoiseSensitivity: 3,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	input := vp9test.NewCheckerYCbCr(width, height, 32, 224, 96, 160)
	if prepared := e.prepareVP9DenoiserSource(input); prepared == input {
		t.Fatal("active denoiser did not prepare a private source")
	}
	if !e.saveVP9DenoiserForCounts(&vp9InterEncodeState{}) {
		t.Fatal("active denoiser did not arm count rollback")
	}
	clear(e.denoiser.source.Y)
	clear(e.denoiser.source.Cb)
	clear(e.denoiser.source.Cr)
	clear(e.denoiser.runningAvg[vp9DenoiserAvgIntra].Y)
	clear(e.denoiser.runningAvg[vp9DenoiserAvgIntra].Cb)
	clear(e.denoiser.runningAvg[vp9DenoiserAvgIntra].Cr)

	e.restoreVP9DenoiserAfterCounts(true, input)
	if !vp9test.EqualYCbCr(&e.denoiser.source, input, width, height) {
		t.Fatal("count rollback did not restore the denoiser source")
	}
	if !vp9test.EqualYCbCr(&e.denoiser.runningAvg[vp9DenoiserAvgIntra],
		input, width, height) {
		t.Fatal("count rollback did not restore the intra running average")
	}
}

func TestVP9CountWorkerDecisionCachesPingPongOwnership(t *testing.T) {
	e := &VP9Encoder{
		vp9LeafInterDecisions:        make([]vp9LeafInterDecisionEntry, 2),
		vp9LeafInterDecisionsRows:    1,
		vp9LeafInterDecisionsCols:    2,
		vp9LeafInterDecisionsVer:     3,
		vp9LeafKeyframeDecisions:     make([]vp9LeafKeyframeDecisionEntry, 2),
		vp9LeafKeyframeDecisionsRows: 1,
		vp9LeafKeyframeDecisionsCols: 2,
		vp9LeafKeyframeDecisionsVer:  5,
	}
	w := &VP9Encoder{
		vp9LeafInterDecisions:        make([]vp9LeafInterDecisionEntry, 3),
		vp9LeafInterDecisionsRows:    3,
		vp9LeafInterDecisionsCols:    1,
		vp9LeafInterDecisionsVer:     13,
		vp9LeafKeyframeDecisions:     make([]vp9LeafKeyframeDecisionEntry, 3),
		vp9LeafKeyframeDecisionsRows: 3,
		vp9LeafKeyframeDecisionsCols: 1,
		vp9LeafKeyframeDecisionsVer:  15,
	}
	eInter := &e.vp9LeafInterDecisions[0]
	eKey := &e.vp9LeafKeyframeDecisions[0]
	wInter := &w.vp9LeafInterDecisions[0]
	wKey := &w.vp9LeafKeyframeDecisions[0]

	e.adoptVP9CountWorkerLeafDecisionCaches(w)

	if &e.vp9LeafInterDecisions[0] != wInter ||
		&e.vp9LeafKeyframeDecisions[0] != wKey {
		t.Fatal("dispatcher did not adopt worker cache ownership")
	}
	if &w.vp9LeafInterDecisions[0] != eInter ||
		&w.vp9LeafKeyframeDecisions[0] != eKey {
		t.Fatal("worker did not receive the prior dispatcher cache ownership")
	}
	if e.vp9LeafInterDecisionsRows != 3 || e.vp9LeafInterDecisionsCols != 1 ||
		e.vp9LeafInterDecisionsVer != 13 ||
		e.vp9LeafKeyframeDecisionsRows != 3 ||
		e.vp9LeafKeyframeDecisionsCols != 1 ||
		e.vp9LeafKeyframeDecisionsVer != 15 {
		t.Fatal("dispatcher did not adopt worker cache metadata")
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
	leafModes, ok := frame.LeafModesForList(frame.LeafLists[idx])
	if !ok {
		t.Fatalf("%s leaf-mode list slice rejected: %+v", label, frame.LeafLists[idx])
	}
	if got, want := len(leafModes), vp9TokenListEOSBCount(tokens); got != want {
		t.Fatalf("%s leaf-mode count = %d, want EOSB count %d", label, got, want)
	}
	for i, mode := range leafModes {
		if int(mode) >= common.IntraModes {
			t.Fatalf("%s leaf mode[%d] = %d, want UV mode", label, i, mode)
		}
	}
	partitions, ok := frame.PartitionsForList(frame.PartitionLists[idx])
	if !ok || len(partitions) == 0 {
		t.Fatalf("%s partition list missing or empty", label)
	}
	for i, partition := range partitions {
		if common.PartitionType(partition) >= common.PartitionTypes {
			t.Fatalf("%s partition[%d] = %d, want valid partition", label, i, partition)
		}
	}
	if allowOnlyEOSB && len(tokens) != vp9TokenListEOSBCount(tokens) {
		t.Fatalf("%s token list contains coefficient tokens in skip fixture: len=%d eosb=%d",
			label, len(tokens), vp9TokenListEOSBCount(tokens))
	}
}
