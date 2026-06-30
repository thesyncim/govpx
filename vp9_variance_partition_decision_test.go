package govpx

import (
	"bytes"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// TestVP9VarPartDecisionForClaimAtBSize: when the picker stamped bsize
// at (miRow, miCol), partition_lookup[bsl][bsize] == PartitionNone, so
// the read-back commits to the leaf at bsize via (bsize, true). The
// caller forwards `bsize` and writeVP9ModesSb emits PartitionNone.
func TestVP9VarPartDecisionForClaimAtBSize(t *testing.T) {
	const miRows, miCols = 8, 8
	e := &VP9Encoder{}
	e.varPartGrid = make([]vp9dec.NeighborMi, miRows*miCols)
	e.varPartFrameValid = true
	// Picker claimed Block32x32 at (0, 0).
	e.varPartGrid[0].SbType = common.Block32x32
	got, ok := e.vp9VarPartDecisionFor(miCols, 0, 0, common.Block32x32)
	if !ok || got != common.Block32x32 {
		t.Errorf("decision = (%v, %v), want (Block32x32, true)", got, ok)
	}
}

// TestVP9VarPartDecisionForSplitToSmaller: when the picker stamped a
// smaller bsize than the call's bsize, the decision returns splitSize.
func TestVP9VarPartDecisionForSplitToSmaller(t *testing.T) {
	const miRows, miCols = 8, 8
	e := &VP9Encoder{}
	e.varPartGrid = make([]vp9dec.NeighborMi, miRows*miCols)
	e.varPartFrameValid = true
	// Picker stamped Block16x16 at (0, 0) under a Block32x32 call.
	e.varPartGrid[0].SbType = common.Block16x16
	got, ok := e.vp9VarPartDecisionFor(miCols, 0, 0, common.Block32x32)
	if !ok || got != common.Block16x16 {
		t.Errorf("decision = (%v, %v), want (Block16x16, true)", got, ok)
	}
}

// TestVP9VarPartDecisionForHorzSplit checks horizontal-split detection.
func TestVP9VarPartDecisionForHorzSplit(t *testing.T) {
	const miRows, miCols = 8, 8
	e := &VP9Encoder{}
	e.varPartGrid = make([]vp9dec.NeighborMi, miRows*miCols)
	e.varPartFrameValid = true
	// Block32x32 with PartitionHorz => Block32x16.
	horz := common.SubsizeLookup[common.PartitionHorz][common.Block32x32]
	e.varPartGrid[0].SbType = horz
	got, ok := e.vp9VarPartDecisionFor(miCols, 0, 0, common.Block32x32)
	if !ok || got != horz {
		t.Errorf("decision = (%v, %v), want (%v, true)", got, ok, horz)
	}
}

// TestVP9VarPartDecisionForVertSplit checks vertical-split detection.
func TestVP9VarPartDecisionForVertSplit(t *testing.T) {
	const miRows, miCols = 8, 8
	e := &VP9Encoder{}
	e.varPartGrid = make([]vp9dec.NeighborMi, miRows*miCols)
	e.varPartFrameValid = true
	vert := common.SubsizeLookup[common.PartitionVert][common.Block32x32]
	e.varPartGrid[0].SbType = vert
	got, ok := e.vp9VarPartDecisionFor(miCols, 0, 0, common.Block32x32)
	if !ok || got != vert {
		t.Errorf("decision = (%v, %v), want (%v, true)", got, ok, vert)
	}
}

// TestVP9VarPartDecisionForInvalidWhenNotValid: when varPartFrameValid
// is false (picker hasn't run), the read-back returns
// (BlockInvalid, false) so the existing pickers can take over.
func TestVP9VarPartDecisionForInvalidWhenNotValid(t *testing.T) {
	const miRows, miCols = 8, 8
	e := &VP9Encoder{}
	e.varPartGrid = make([]vp9dec.NeighborMi, miRows*miCols)
	e.varPartFrameValid = false
	e.varPartGrid[0].SbType = common.Block16x16
	got, ok := e.vp9VarPartDecisionFor(miCols, 0, 0, common.Block32x32)
	if ok || got != common.BlockInvalid {
		t.Errorf("decision = (%v, %v), want (BlockInvalid, false) when frame invalid",
			got, ok)
	}
}

// TestVP9ChoosePartitioningSBIndex checks the SB-index computation
// (mi_stride >> 3) * (mi_row >> 3) + (mi_col >> 3) — libvpx
// vp9_encodeframe.c:1314.
func TestVP9ChoosePartitioningSBIndex(t *testing.T) {
	e := &VP9Encoder{}
	// 64x64 frame: 8 mi cols, 8 mi rows, 1 SB.
	if got := e.vp9ChoosePartitioningSBIndex(8, 0, 0); got != 0 {
		t.Errorf("sbIdx(8, 0, 0) = %d, want 0", got)
	}
	// 128x64 frame: 16 mi cols, 8 mi rows, 2 SBs.
	if got := e.vp9ChoosePartitioningSBIndex(16, 0, 0); got != 0 {
		t.Errorf("sbIdx(16, 0, 0) = %d, want 0", got)
	}
	if got := e.vp9ChoosePartitioningSBIndex(16, 0, 8); got != 1 {
		t.Errorf("sbIdx(16, 0, 8) = %d, want 1", got)
	}
	// 128x128 frame: 16 mi cols, 16 mi rows, 4 SBs.
	if got := e.vp9ChoosePartitioningSBIndex(16, 8, 0); got != 2 {
		t.Errorf("sbIdx(16, 8, 0) = %d, want 2", got)
	}
	if got := e.vp9ChoosePartitioningSBIndex(16, 8, 8); got != 3 {
		t.Errorf("sbIdx(16, 8, 8) = %d, want 3", got)
	}
}

func TestVP9EnsureSBPartitionChosenThreadsNoiseEstimate(t *testing.T) {
	const width, height = 640, 480
	const miRows, miCols = 60, 80
	pick := func(enabled bool, value int) common.BlockSize {
		t.Helper()
		e, err := NewVP9Encoder(VP9EncoderOptions{
			Width:   width,
			Height:  height,
			CpuUsed: 8,
		})
		if err != nil {
			t.Fatalf("NewVP9Encoder: %v", err)
		}
		defer e.Close()
		e.sf.VariancePartThreshMult = 2
		e.noiseEstimate.Enabled = enabled
		e.noiseEstimate.Value = value

		ref := vp9test.NewYCbCr(width, height, 128, 128, 128)
		src := vp9test.NewYCbCr(width, height, 128, 128, 128)
		fillVP9Partition8x8AlternatingForTest(src, 130)
		e.refFrames[vp9LastRefSlot] = vp9ReferenceFrameFromYCbCr(ref)

		var dq vp9dec.DequantTables
		inter := &vp9InterEncodeState{
			img:        src,
			dq:         &dq,
			refMask:    1 << uint(vp9dec.LastFrame),
			baseQindex: 37,
		}
		if !e.vp9EnsureSBPartitionChosen(miRows, miCols, 0, 0, nil, inter) {
			t.Fatal("vp9EnsureSBPartitionChosen returned false")
		}
		return e.varPartGrid[0].SbType
	}

	low := pick(false, 0)
	high := pick(true, 300)
	if low == common.Block64x64 {
		t.Fatalf("noise-disabled partition = Block64x64, want split fixture")
	}
	if high != common.Block64x64 {
		t.Fatalf("high-noise partition = %v, want Block64x64 from raised VBP thresholds",
			high)
	}
}

func TestVP9PartitionReferenceSlotIgnoresCodingRefMask(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:   width,
		Height:  height,
		CpuUsed: 8,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	src := vp9test.NewMotionYCbCr(width, height)
	e.refFrames[vp9LastRefSlot] = vp9ReferenceFrameFromYCbCr(src)

	inter := &vp9InterEncodeState{
		refMask: 1 << uint(vp9dec.GoldenFrame),
	}
	if _, ok := e.vp9InterReferenceSlot(inter, vp9dec.LastFrame); ok {
		t.Fatal("vp9InterReferenceSlot accepted LAST despite the coding ref mask")
	}
	if slot, ok := e.vp9PartitionReferenceSlot(vp9dec.LastFrame); !ok || slot != vp9LastRefSlot {
		t.Fatalf("vp9PartitionReferenceSlot = (%d, %t), want (%d, true)",
			slot, ok, vp9LastRefSlot)
	}
}

func TestVP9EnsureSBPartitionChosenUsesCyclicRefreshSegmentQ(t *testing.T) {
	const width, height = 640, 480
	const miRows, miCols = 60, 80
	pick := func(segmentID uint8) common.BlockSize {
		t.Helper()
		e, err := NewVP9Encoder(VP9EncoderOptions{
			Width:              width,
			Height:             height,
			CpuUsed:            8,
			Deadline:           DeadlineRealtime,
			RateControlMode:    RateControlCBR,
			RateControlModeSet: true,
			TargetBitrateKbps:  1000,
			AQMode:             VP9AQCyclicRefresh,
		})
		if err != nil {
			t.Fatalf("NewVP9Encoder: %v", err)
		}
		defer e.Close()
		e.sf.VariancePartThreshMult = 2
		e.cyclicAQ.Alloc(miRows, miCols)
		e.cyclicAQ.Enabled = true
		e.cyclicAQ.Apply = true
		e.cyclicAQ.SegMap[0] = segmentID
		e.vp9HeaderScratch.Seg.Enabled = true
		e.vp9HeaderScratch.Seg.FeatureMask[encoder.CyclicRefreshSegmentBoost1] =
			1 << uint(vp9dec.SegLvlAltQ)
		e.vp9HeaderScratch.Seg.FeatureData[encoder.CyclicRefreshSegmentBoost1][vp9dec.SegLvlAltQ] = -37

		ref := vp9test.NewYCbCr(width, height, 128, 128, 128)
		src := vp9test.NewYCbCr(width, height, 128, 128, 128)
		fillVP9Partition8x8AlternatingForTest(src, 129)
		e.refFrames[vp9LastRefSlot] = vp9ReferenceFrameFromYCbCr(ref)

		var dq vp9dec.DequantTables
		inter := &vp9InterEncodeState{
			img:        src,
			dq:         &dq,
			refMask:    1 << uint(vp9dec.LastFrame),
			baseQindex: 37,
		}
		if !e.vp9EnsureSBPartitionChosen(miRows, miCols, 0, 0, nil, inter) {
			t.Fatal("vp9EnsureSBPartitionChosen returned false")
		}
		return e.varPartGrid[0].SbType
	}

	base := pick(encoder.CyclicRefreshSegmentBase)
	boosted := pick(encoder.CyclicRefreshSegmentBoost1)
	if base != common.Block64x64 {
		t.Fatalf("base CR segment partition = %v, want Block64x64 fixture", base)
	}
	if boosted == common.Block64x64 {
		t.Fatalf("boosted CR segment partition = Block64x64, want split from segment qindex thresholds")
	}
}

func TestVP9EnsureSBPartitionChosenThreadsHighSourceSAD(t *testing.T) {
	const width, height = 64, 64
	const miRows, miCols = 8, 8
	pick := func(highSourceSAD bool) common.BlockSize {
		t.Helper()
		e, err := NewVP9Encoder(VP9EncoderOptions{
			Width:   width,
			Height:  height,
			CpuUsed: 8,
		})
		if err != nil {
			t.Fatalf("NewVP9Encoder: %v", err)
		}
		defer e.Close()
		e.sf.VariancePartThreshMult = 1
		e.rc.highSourceSAD = highSourceSAD

		ref := vp9test.NewYCbCr(width, height, 128, 128, 128)
		src := vp9test.NewYCbCr(width, height, 128, 128, 128)
		e.refFrames[vp9LastRefSlot] = vp9ReferenceFrameFromYCbCr(ref)

		var dq vp9dec.DequantTables
		inter := &vp9InterEncodeState{
			img:        src,
			dq:         &dq,
			refMask:    1 << uint(vp9dec.LastFrame),
			baseQindex: 37,
		}
		if !e.vp9EnsureSBPartitionChosen(miRows, miCols, 0, 0, nil, inter) {
			t.Fatal("vp9EnsureSBPartitionChosen returned false")
		}
		return e.varPartGrid[0].SbType
	}

	if got := pick(false); got != common.Block64x64 {
		t.Fatalf("plain partition = %v, want Block64x64", got)
	}
	if got := pick(true); got == common.Block64x64 {
		t.Fatalf("high-source-SAD partition = Block64x64, want force split")
	}
}

func TestVP9KeyframeNonRDUsesChoosePartitioning(t *testing.T) {
	const width, height = 64, 64
	const miRows, miCols = 8, 8

	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:    width,
		Height:   height,
		Deadline: DeadlineRealtime,
		CpuUsed:  8,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	const baseQ = 37
	e.vp9ApplySpeedFeatures(e.vp9PerFrameSpeedContext(vp9PerFrameSpeedContextArgs{
		IsKey:      true,
		IntraOnly:  true,
		ShowFrame:  true,
		BaseQIndex: baseQ,
	}))
	key := &vp9KeyframeEncodeState{
		img: vp9test.NewPanningYCbCr(width, height, 0),
		hdr: &vp9dec.UncompressedHeader{
			FrameType: common.KeyFrame,
			Quant:     vp9dec.QuantizationParams{BaseQindex: baseQ},
		},
		dq: &vp9dec.DequantTables{},
	}
	if !e.vp9KeyframeChoosePartitioningEnabled(key) {
		t.Fatalf("keyframe choose_partitioning disabled for realtime non-RD row")
	}
	got, ok := e.pickVP9KeyframeVariancePartitionBlockSize(key,
		miRows, miCols, 0, 0, common.Block64x64)
	if !ok || got != common.Block8x8 {
		t.Fatalf("keyframe partition = (%v, %v), want (Block8x8, true)", got, ok)
	}
}

func TestVP9EnsureSBPartitionChosenCachesColorSensitivity(t *testing.T) {
	const width, height = 64, 64
	const miRows, miCols = 8, 8
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:   width,
		Height:  height,
		CpuUsed: 8,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	e.sf.VariancePartThreshMult = 1
	vp9dec.SetupBlockPlanes(&e.planes, 1, 1)
	e.prepareVP9EncoderOutputFrame(width, height)

	ref := vp9test.NewYCbCr(width, height, 128, 128, 128)
	src := vp9test.NewYCbCr(width, height, 128, 255, 128)
	e.refFrames[vp9LastRefSlot] = vp9ReferenceFrameFromYCbCr(ref)
	for i := range e.reconY {
		e.reconY[i] = 73
	}
	lumaBefore := append([]byte(nil), e.reconY...)

	var dq vp9dec.DequantTables
	inter := &vp9InterEncodeState{
		img:        src,
		dq:         &dq,
		ref:        &e.refFrames[0],
		refMask:    1 << uint(vp9dec.LastFrame),
		baseQindex: 37,
	}
	if !e.vp9EnsureSBPartitionChosen(miRows, miCols, 0, 0, nil, inter) {
		t.Fatal("vp9EnsureSBPartitionChosen returned false")
	}
	got, ok := e.vp9VarPartSBColorSensitivity(miCols, 0, 0)
	if !ok {
		t.Fatal("color sensitivity cache was not marked valid")
	}
	if !got[0] || got[1] {
		t.Fatalf("color sensitivity = %v, want U-only sensitivity", got)
	}
	if !bytes.Equal(e.reconY, lumaBefore) {
		t.Fatalf("color-sensitivity chroma SAD mutated luma prediction plane")
	}
}

func TestVP9EnsureSBPartitionChosenLowResEdgeUsesSubBsize(t *testing.T) {
	const width, height = 160, 96
	const miRows, miCols = 12, 20
	const sbMiRow, sbMiCol = 8, 16

	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width:   width,
		Height:  height,
		CpuUsed: 8,
	})
	e.sf.VariancePartThreshMult = 1

	ref := vp9test.NewMotionYCbCr(width, height)
	src := shiftedVP9ReferenceYCbCrForTest(vp9ImageFromYCbCrForTest(ref), 8, 0)
	e.refFrames[vp9LastRefSlot] = vp9ReferenceFrameFromYCbCr(ref)
	e.ensureLastBordered()

	var dq vp9dec.DequantTables
	inter := &vp9InterEncodeState{
		img:        src,
		dq:         &dq,
		refMask:    1 << uint(vp9dec.LastFrame),
		baseQindex: e.vp9EncoderModeDecisionQIndex(),
	}
	if !e.vp9EnsureSBPartitionChosen(miRows, miCols, sbMiRow, sbMiCol, nil, inter) {
		t.Fatal("vp9EnsureSBPartitionChosen returned false")
	}

	subBsize := encoder.GetEstimatedPredSubBsize(sbMiRow, sbMiCol, miRows, miCols)
	if subBsize != common.Block32x32 {
		t.Fatalf("edge sub-bsize = %v, want Block32x32", subBsize)
	}

	x0 := sbMiCol * common.MiSize
	y0 := sbMiRow * common.MiSize
	srcOriginX := e.intProSrcBordered.OriginX()
	srcOriginY := e.intProSrcBordered.OriginY()
	refOriginX := e.lastBordered.OriginX()
	refOriginY := e.lastBordered.OriginY()
	srcStrideB := e.intProSrcBordered.Stride
	refStrideB := e.lastBordered.Stride
	estIn := &encoder.GetEstimatedPredInterInput{
		Bsize:         subBsize,
		Src:           e.intProSrcBordered.Pixels,
		SrcOff:        (srcOriginY+y0)*srcStrideB + (srcOriginX + x0),
		SrcStride:     srcStrideB,
		LastRef:       e.lastBordered.Pixels,
		LastRefOff:    (refOriginY+y0)*refStrideB + (refOriginX + x0),
		LastRefStride: refStrideB,
		Speed:         int(e.opts.CpuUsed),
		MvLimits: encoder.MvLimits{
			ColMin: -(x0 + common.VP9EncBorderInPixels),
			ColMax: width - x0 + common.VP9EncBorderInPixels,
			RowMin: -(y0 + common.VP9EncBorderInPixels),
			RowMax: height - y0 + common.VP9EncBorderInPixels,
		},
	}
	expected := make([]uint8, 64*64)
	encoder.GetEstimatedPred(false, estIn, expected)
	if !bytes.Equal(e.intProEstPred[:], expected) {
		t.Fatal("low-res edge predictor did not use edge-aware sub-bsize")
	}

	oldShape := make([]uint8, 64*64)
	oldIn := *estIn
	oldIn.Bsize = common.Block64x64
	encoder.GetEstimatedPred(false, &oldIn, oldShape)
	if bytes.Equal(expected, oldShape) {
		t.Fatal("test fixture is not sensitive to Block32x32 vs Block64x64 int-pro search")
	}
}

func fillVP9Partition8x8AlternatingForTest(img *image.YCbCr, high byte) {
	for by := range 8 {
		for bx := range 8 {
			value := byte(128)
			if (by+bx)&1 != 0 {
				value = high
			}
			for y := by * 8; y < by*8+8; y++ {
				row := img.Y[y*img.YStride:]
				for x := bx * 8; x < bx*8+8; x++ {
					row[x] = value
				}
			}
		}
	}
}
