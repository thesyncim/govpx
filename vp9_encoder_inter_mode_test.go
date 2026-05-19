package govpx

import (
	"bytes"
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

func TestVP9EncoderInterDcResidueTracksChangedConstantSource(t *testing.T) {
	const width, height = 96, 80
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := newVP9YCbCrForTest(width, height, 82, 123, 211)
	interSrc := newVP9YCbCrForTest(width, height, 201, 44, 19)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	var keyBR vp9dec.BitReader
	keyBR.Init(key)
	keyHeader, err := vp9dec.ReadUncompressedHeader(&keyBR, nil, nil)
	if err != nil {
		t.Fatalf("ReadUncompressedHeader keyframe: %v", err)
	}
	var interBR vp9dec.BitReader
	interBR.Init(inter)
	interHeader, err := vp9dec.ReadUncompressedHeader(&interBR, &keyHeader,
		func(uint8) (uint32, uint32) { return width, height })
	if err != nil {
		t.Fatalf("ReadUncompressedHeader inter: %v", err)
	}
	// libvpx vp9/encoder/vp9_encoder.c:2141 starts from
	// sf.default_interp_filter=SWITCHABLE, then fix_interp_filter
	// (vp9_bitstream.c:864-885) demotes the frame header when exactly one
	// concrete switchable filter appears in the counts. In this constant
	// 96x80 inter case libvpx v1.16.0 emits EIGHTTAP after the nonrd picker
	// collapses the histogram to that single filter.
	if interHeader.InterpFilter != vp9dec.InterpEighttap {
		t.Fatalf("inter header InterpFilter = %d, want Eighttap",
			interHeader.InterpFilter)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after keyframe")
	}
	assertVP9FilledFrameWithin(t, frame, 96, 80, 82, 123, 211, 1)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter: %v", err)
	}
	frame, ok = d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after visible inter frame")
	}
	assertVP9FilledFrameWithin(t, frame, 96, 80, 201, 44, 19, 64)
}

func TestVP9EncoderInterPicksIntraBlockForSceneCut(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := newVP9YCbCrForTest(width, height, 0, 0, 0)
	interSrc := newVP9YCbCrForTest(width, height, 128, 128, 128)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	d := decodeVP9KeyInterForTest(t, key, inter)
	if got := d.miGrid[0]; got.RefFrame[0] != vp9dec.IntraFrame ||
		got.Mode != common.DcPred || got.Skip != 1 {
		t.Fatalf("top-left inter-frame intra = ref %d mode %d skip %d, want IntraFrame/DcPred skip=1",
			got.RefFrame[0], got.Mode, got.Skip)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after inter-frame intra")
	}
	assertVP9FilledFrame(t, frame, width, height, 128, 128, 128)
}

func TestVP9EncoderInterIntraModeScoresWholeBlock(t *testing.T) {
	const width, height = 128, 128
	const x0, y0 = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	img := newVP9YCbCrForTest(width, height, 128, 128, 128)
	vp9dec.SetupBlockPlanes(&e.planes, 1, 1)
	e.prepareVP9EncoderOutputFrame(width, height)

	aboveRow := (y0 - 1) * e.reconFrame.YStride
	internalAboveRow := (y0 + 31) * e.reconFrame.YStride
	for x := range 64 {
		above := byte(224 - (x%32)*2)
		if x < 32 {
			above = byte(72 + x)
		}
		e.reconY[aboveRow+x0+x] = above
		e.reconY[internalAboveRow+x0+x] = byte(224 - (x%32)*2)
	}
	for y := range 64 {
		left := byte(64 + (y%32)*2)
		e.reconY[(y0+y)*e.reconFrame.YStride+x0-1] = left
		e.reconY[(y0+y)*e.reconFrame.YStride+x0+31] = left
		for x := range 64 {
			pixel := left
			if y < 32 && x < 32 {
				pixel = byte(72 + x)
			}
			img.Y[(y0+y)*img.YStride+x0+x] = pixel
		}
	}

	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	inter := &vp9InterEncodeState{img: img, selectFc: fc}
	tile := vp9dec.TileBounds{MiRowStart: 0, MiRowEnd: 16, MiColStart: 0, MiColEnd: 16}
	got, ok := e.pickVP9InterIntraMode(inter, tile, 16, 16, 8, 8,
		common.Block64x64, common.Tx32x32, 1<<60)
	if !ok {
		t.Fatal("pickVP9InterIntraMode returned !ok")
	}
	if got.mode != common.HPred {
		t.Fatalf("inter intra mode = %d, want HPred from full-block score", got.mode)
	}
}

func TestVP9EncoderInterPicksCompoundZeroMotion(t *testing.T) {
	const width, height = 64, 64
	// libvpx nonrd_pickmode (RT speed >= 5) disables compound prediction
	// unless sf.use_compound_nonrd_pickmode is set (VBR + lag_in_frames
	// only). At default Deadline+CpuUsed (auto-promoted to Realtime+
	// speed8), CBR is implicit and compound is off. Request a slower
	// preset so the GOOD path's compound-prediction loop is exercised.
	// libvpx: vp9/encoder/vp9_speed_features.c:469 / 656 / 665,
	// vp9/encoder/vp9_pickmode.c:1989.
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width: width, Height: height,
		Deadline: DeadlineBestQuality, CpuUsed: 1,
	})
	low := newVP9CompoundAverageYCbCrForTest(width, height, -32)
	mid := newVP9CompoundAverageYCbCrForTest(width, height, 0)
	high := newVP9CompoundAverageYCbCrForTest(width, height, 32)
	key, err := e.Encode(low)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	alt, err := e.EncodeWithFlags(high,
		EncodeForceAltRefFrame|EncodeNoUpdateLast|EncodeNoUpdateGolden|
			EncodeNoReferenceGolden|EncodeNoReferenceAltRef)
	if err != nil {
		t.Fatalf("Encode alt refresh: %v", err)
	}
	inter, err := e.Encode(mid)
	if err != nil {
		t.Fatalf("Encode compound inter: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	for i, packet := range [][]byte{key, alt, inter} {
		if err := d.Decode(packet); err != nil {
			t.Fatalf("Decode packet %d: %v", i, err)
		}
		if _, ok := d.NextFrame(); !ok {
			t.Fatalf("NextFrame returned !ok after packet %d", i)
		}
	}
	if len(d.miGrid) == 0 {
		t.Fatal("decoder MI grid is empty after compound inter frame")
	}
	got := d.miGrid[0]
	if got.RefFrame[1] <= vp9dec.IntraFrame {
		t.Fatalf("top-left ref pair = %v, want compound", got.RefFrame)
	}
	if got.Mode != common.ZeroMv && got.Mode != common.NearestMv && got.Mode != common.NearMv {
		t.Fatalf("top-left compound mode = %d, want zero-motion inter mode", got.Mode)
	}
	if got.Mv != ([2]vp9dec.MV{}) {
		t.Fatalf("top-left compound MV = %+v, want zero MVs", got.Mv)
	}
	if got.RefFrame != [2]int8{vp9dec.LastFrame, vp9dec.AltrefFrame} &&
		got.RefFrame != [2]int8{vp9dec.GoldenFrame, vp9dec.AltrefFrame} {
		t.Fatalf("top-left ref pair = %v, want LAST/ALTREF or GOLDEN/ALTREF", got.RefFrame)
	}
}

func TestVP9EncoderInterPicksCompoundNewMvForTranslatedAverage(t *testing.T) {
	const width, height = 128, 64
	// libvpx nonrd_pickmode disables compound at RT speed >= 5 unless
	// sf.use_compound_nonrd_pickmode is set; request DeadlineBestQuality
	// so the GOOD path's compound walker is exercised. libvpx:
	// vp9/encoder/vp9_pickmode.c:1989.
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width: width, Height: height,
		Deadline: DeadlineBestQuality, CpuUsed: 1,
	})
	low := newVP9CompoundPairYCbCrForTest(width, height, false)
	high := newVP9CompoundPairYCbCrForTest(width, height, true)
	mid := shiftedVP9ReferenceYCbCrForTest(
		vp9ImageFromYCbCrForTest(averageVP9YCbCrForTest(low, high)),
		8, 0)
	key, err := e.Encode(low)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	alt, err := e.EncodeWithFlags(high,
		EncodeForceAltRefFrame|EncodeNoUpdateLast|EncodeNoUpdateGolden|
			EncodeNoReferenceGolden|EncodeNoReferenceAltRef)
	if err != nil {
		t.Fatalf("Encode alt refresh: %v", err)
	}
	inter, err := e.Encode(mid)
	if err != nil {
		t.Fatalf("Encode compound motion inter: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	for i, packet := range [][]byte{key, alt, inter} {
		if err := d.Decode(packet); err != nil {
			t.Fatalf("Decode packet %d: %v", i, err)
		}
		if _, ok := d.NextFrame(); !ok {
			t.Fatalf("NextFrame returned !ok after packet %d", i)
		}
	}
	if len(d.miGrid) == 0 {
		t.Fatal("decoder MI grid is empty after compound motion frame")
	}
	got := d.miGrid[0]
	if got.RefFrame[1] <= vp9dec.IntraFrame {
		t.Fatalf("top-left ref pair = %v, want compound", got.RefFrame)
	}
	if got.Mode != common.NewMv {
		t.Fatalf("top-left compound mode = %d, want NewMv", got.Mode)
	}
	for ref := range got.Mv {
		if got.Mv[ref].Col < 56 || got.Mv[ref].Col > 72 ||
			got.Mv[ref].Row < -8 || got.Mv[ref].Row > 8 {
			t.Fatalf("top-left compound MV = %+v, want both refs near +8px horizontal motion",
				got.Mv)
		}
	}
}

func TestVP9EncoderInterPicksCompoundNewMvWithStationaryHalf(t *testing.T) {
	const width, height = 128, 64
	// libvpx nonrd_pickmode disables compound at RT speed >= 5 unless
	// sf.use_compound_nonrd_pickmode is set; request DeadlineBestQuality
	// so the GOOD path's compound walker is exercised. libvpx:
	// vp9/encoder/vp9_pickmode.c:1989.
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width: width, Height: height,
		Deadline: DeadlineBestQuality, CpuUsed: 1,
	})
	low := newVP9CompoundPairYCbCrForTest(width, height, false)
	high := newVP9CompoundPairYCbCrForTest(width, height, true)
	shiftedHigh := shiftedVP9ReferenceYCbCrForTest(vp9ImageFromYCbCrForTest(high), 8, 0)
	mid := averageVP9YCbCrForTest(low, shiftedHigh)
	key, err := e.Encode(low)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	alt, err := e.EncodeWithFlags(high,
		EncodeForceAltRefFrame|EncodeNoUpdateLast|EncodeNoUpdateGolden|
			EncodeNoReferenceGolden|EncodeNoReferenceAltRef)
	if err != nil {
		t.Fatalf("Encode alt refresh: %v", err)
	}
	inter, err := e.Encode(mid)
	if err != nil {
		t.Fatalf("Encode asymmetric compound motion inter: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	for i, packet := range [][]byte{key, alt, inter} {
		if err := d.Decode(packet); err != nil {
			t.Fatalf("Decode packet %d: %v", i, err)
		}
		if _, ok := d.NextFrame(); !ok {
			t.Fatalf("NextFrame returned !ok after packet %d", i)
		}
	}
	got := d.miGrid[0]
	if got.RefFrame != [2]int8{vp9dec.LastFrame, vp9dec.AltrefFrame} &&
		got.RefFrame != [2]int8{vp9dec.GoldenFrame, vp9dec.AltrefFrame} {
		t.Fatalf("top-left ref pair = %v, want LAST/ALTREF or GOLDEN/ALTREF", got.RefFrame)
	}
	if got.Mode != common.NewMv {
		t.Fatalf("top-left compound mode = %d, want NewMv", got.Mode)
	}
	if got.Mv[0].Col < -4 || got.Mv[0].Col > 4 ||
		got.Mv[0].Row < -4 || got.Mv[0].Row > 4 {
		t.Fatalf("stationary compound MV half = %+v, want near zero", got.Mv[0])
	}
	if got.Mv[1].Col < 56 || got.Mv[1].Col > 72 ||
		got.Mv[1].Row < -8 || got.Mv[1].Row > 8 {
		t.Fatalf("moving compound MV half = %+v, want near +8px horizontal motion", got.Mv[1])
	}
}

func TestVP9EncoderInterACResiduePreservesCheckerSource(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 32, Height: 32})
	keySrc := newVP9YCbCrForTest(32, 32, 128, 128, 128)
	interSrc := newVP9CheckerYCbCrForTest(32, 32, 48, 208, 128, 128)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after keyframe")
	}
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after checker inter frame")
	}
	assertVP9VisibleYContrast(t, frame, 32, 32, 40)
}

func TestVP9EncoderInterPicksNewMvForTranslatedBlock(t *testing.T) {
	const (
		width  = 128
		height = 64
	)
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height, CpuUsed: -3})
	keySrc := newVP9MotionYCbCrForTest(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	if !e.refFrames[0].valid {
		t.Fatal("LAST reference was not refreshed by keyframe")
	}
	interSrc := shiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 8, 0)
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after keyframe")
	}
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter: %v", err)
	}
	if len(d.miGrid) == 0 {
		t.Fatal("decoder MI grid is empty after inter frame")
	}
	got := d.miGrid[0]
	if got.Mode != common.NewMv {
		t.Fatalf("top-left inter mode = %d, want NewMv", got.Mode)
	}
	want := vp9dec.MV{Col: 64}
	if got.Mv[0] != want {
		t.Fatalf("top-left MV = %+v, want %+v", got.Mv[0], want)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after NEWMV inter frame")
	}
}

func TestVP9EncoderInterMvSearchUsesMvPredSeedAsCenter(t *testing.T) {
	const (
		width  = 128
		height = 64
	)
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := newVP9MotionYCbCrForTest(width, height)
	if _, err := e.Encode(keySrc); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	if !e.refFrames[0].valid {
		t.Fatal("LAST reference was not refreshed by keyframe")
	}

	interSrc := shiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 24, 0)
	inter := &vp9InterEncodeState{
		img:     interSrc,
		ref:     &e.refFrames[0],
		allowHP: true,
	}
	e.sf.Mv.SearchMethod = SearchMethodFastDiamond
	got, _, ok := e.pickVP9InterMvWithOptions(inter, 8, 16,
		0, 0, common.Block64x64, vp9dec.LastFrame,
		vp9InterMvSearchOptions{
			seed:      vp9dec.MV{Col: 24 * 8},
			seedValid: true,
		})
	if !ok {
		t.Fatal("seeded NEWMV search returned !ok")
	}
	want := vp9dec.MV{Col: 24 * 8}
	if got != want {
		t.Fatalf("seeded NEWMV = %+v, want %+v", got, want)
	}
}

func TestVP9EncoderInterPicksNewMvFor16x8Block(t *testing.T) {
	const (
		width  = 32
		height = 8
	)
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height, CpuUsed: -3})
	keySrc := newVP9MotionYCbCrForTest(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := shiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 8, 0)
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after keyframe")
	}
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter: %v", err)
	}
	if len(d.miGrid) == 0 {
		t.Fatal("decoder MI grid is empty after 16x8 inter frame")
	}
	got := d.miGrid[0]
	if got.SbType != common.Block16x8 {
		t.Fatalf("top-left block size = %d, want Block16x8", got.SbType)
	}
	want := vp9dec.MV{Col: 64}
	if got.Mode != common.NewMv || got.Mv[0] != want {
		t.Fatalf("top-left inter = mode %d mv %+v, want NewMv %+v",
			got.Mode, got.Mv[0], want)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after 16x8 NEWMV inter frame")
	}
}

func TestVP9EncoderInterPicksVert64x64ForHorizontalMixedMotion(t *testing.T) {
	const (
		width  = 64
		height = 64
	)
	// CpuUsed: -3 retains the recursive RD partition picker (speed=3,
	// PartitionSearchType=SearchPartition). The default speed=8 path uses
	// VAR_BASED_PARTITION which intentionally commits the root SB size.
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height, CpuUsed: -3})
	keySrc := newVP9MotionYCbCrForTest(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := splitShiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 8, -8)
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	d := decodeVP9KeyInterForTest(t, key, inter)
	left := d.miGrid[0]
	right := d.miGrid[4]
	if left.SbType != common.Block32x64 || right.SbType != common.Block32x64 {
		t.Fatalf("top blocks = %d/%d, want Block32x64/Block32x64",
			left.SbType, right.SbType)
	}
	assertVP9InterMotionBlockForTest(t, "left", left, vp9dec.MV{Col: 64})
	assertVP9InterMotionBlockForTest(t, "right", right, vp9dec.MV{Col: -64})
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after vertical-partition inter frame")
	}
}

func TestVP9EncoderInterPartitionScoringRestoresFrameState(t *testing.T) {
	const width, height = 64, 64
	// CpuUsed: -3 forces the SPEED_FEATURES dispatcher to speed=3
	// (PartitionSearchType=SearchPartition). The default normalisation routes
	// CpuUsed=0 to realtime+speed=8 (VAR_BASED_PARTITION + NonrdPickmode),
	// where the recursive RD partition picker is intentionally bypassed.
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height, CpuUsed: -3})
	keySrc := newVP9MotionYCbCrForTest(width, height)
	if _, err := e.Encode(keySrc); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := splitShiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 8, -8)

	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	e.resetVP9EncoderCodingState(width, height)
	origMi := append([]vp9dec.NeighborMi(nil), e.miGrid...)
	origY := append([]byte(nil), e.reconY[:e.reconFrame.YStride*height]...)
	origU := append([]byte(nil), e.reconU[:e.reconFrame.UStride*(height/2)]...)
	origV := append([]byte(nil), e.reconV[:e.reconFrame.VStride*(height/2)]...)

	inter := &vp9InterEncodeState{
		img:           interSrc,
		refMask:       1 << uint(vp9dec.LastFrame),
		allowHP:       true,
		selectFc:      fc,
		referenceMode: vp9dec.SingleReference,
	}
	tile := vp9dec.TileBounds{MiRowStart: 0, MiRowEnd: 8, MiColStart: 0, MiColEnd: 8}
	got := e.pickVP9InterPartitionBlockSize(inter, tile, &fc.PartitionProb,
		8, 8, 0, 0, common.Block64x64)
	if got != common.Block32x64 {
		t.Fatalf("partition size = %d, want Block32x64", got)
	}
	for i := range origMi {
		if e.miGrid[i] != origMi[i] {
			t.Fatalf("miGrid[%d] = %+v, want restored %+v", i, e.miGrid[i], origMi[i])
		}
	}
	if !bytes.Equal(e.reconY[:len(origY)], origY) ||
		!bytes.Equal(e.reconU[:len(origU)], origU) ||
		!bytes.Equal(e.reconV[:len(origV)], origV) {
		t.Fatal("partition scoring leaked temporary reconstruction into frame state")
	}
}

func TestVP9PartitionReconSnapshotStacksNestedSaves(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	vp9dec.SetupBlockPlanes(&e.planes, 1, 1)
	e.ensureVP9EncoderModeBuffers(8, 8)
	e.prepareVP9EncoderOutputFrame(width, height)

	visibleRecon := func() []byte {
		out := make([]byte, 0, width*height+(width/2)*(height/2)*2)
		for y := range height {
			row := e.reconY[y*e.reconFrame.YStride:]
			out = append(out, row[:width]...)
		}
		chromaW := (width + 1) >> 1
		chromaH := (height + 1) >> 1
		for y := range chromaH {
			row := e.reconU[y*e.reconFrame.UStride:]
			out = append(out, row[:chromaW]...)
		}
		for y := range chromaH {
			row := e.reconV[y*e.reconFrame.VStride:]
			out = append(out, row[:chromaW]...)
		}
		return out
	}
	fillVisibleRecon := func(seed byte) {
		for y := range height {
			row := e.reconY[y*e.reconFrame.YStride:]
			for x := range width {
				row[x] = byte(int(seed) + (x*3+y*5)&0xff)
			}
		}
		chromaW := (width + 1) >> 1
		chromaH := (height + 1) >> 1
		for y := range chromaH {
			uRow := e.reconU[y*e.reconFrame.UStride:]
			vRow := e.reconV[y*e.reconFrame.VStride:]
			for x := range chromaW {
				uRow[x] = byte(int(seed) + 17 + (x*7+y*11)&0xff)
				vRow[x] = byte(int(seed) + 29 + (x*13+y*3)&0xff)
			}
		}
	}
	fillReconBlock := func(miRow, miCol int, bsize common.BlockSize, seed byte) {
		for plane := range vp9dec.MaxMbPlane {
			pd := &e.planes[plane]
			planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
			if planeBsize >= common.BlockSizes {
				continue
			}
			data, stride := e.vp9EncoderReconPlane(plane)
			x0 := (miCol * common.MiSize) >> pd.SubsamplingX
			y0 := (miRow * common.MiSize) >> pd.SubsamplingY
			w := int(common.Num4x4BlocksWideLookup[planeBsize]) * 4
			h := int(common.Num4x4BlocksHighLookup[planeBsize]) * 4
			for y := range h {
				row := data[(y0+y)*stride+x0:]
				for x := range w {
					row[x] = byte(int(seed) + plane*31 + (x*5+y*9)&0xff)
				}
			}
		}
	}

	fillVisibleRecon(11)
	original := visibleRecon()
	outer, ok := e.saveVP9PartitionReconSnapshot(0, 0, common.Block64x64)
	if !ok {
		t.Fatal("save outer snapshot failed")
	}
	fillReconBlock(0, 0, common.Block16x16, 37)
	afterOuterMutation := visibleRecon()
	inner, ok := e.saveVP9PartitionReconSnapshot(0, 0, common.Block16x16)
	if !ok {
		t.Fatal("save inner snapshot failed")
	}
	if inner.top != outer.end {
		t.Fatalf("inner snapshot top = %d, want outer end %d", inner.top, outer.end)
	}
	fillReconBlock(0, 0, common.Block16x16, 93)

	e.restoreVP9PartitionReconSnapshot(inner)
	if !bytes.Equal(visibleRecon(), afterOuterMutation) {
		t.Fatal("inner restore did not preserve the outer mutation")
	}
	e.releaseVP9PartitionReconSnapshot(inner)
	if e.partitionReconScratchTop != outer.end {
		t.Fatalf("scratch top after inner release = %d, want %d",
			e.partitionReconScratchTop, outer.end)
	}
	e.restoreVP9PartitionReconSnapshot(outer)
	if !bytes.Equal(visibleRecon(), original) {
		t.Fatal("outer restore was overwritten by nested snapshot data")
	}
	e.releaseVP9PartitionReconSnapshot(outer)
	if e.partitionReconScratchTop != 0 {
		t.Fatalf("scratch top after outer release = %d, want 0",
			e.partitionReconScratchTop)
	}
}

func TestVP9EncoderInterPartitionScoringUsesPriorChildContext(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height, CpuUsed: -3})
	keySrc := newVP9MotionYCbCrForTest(width, height)
	if _, err := e.Encode(keySrc); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := shiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 8, 0)

	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	e.resetVP9EncoderCodingState(width, height)
	inter := &vp9InterEncodeState{
		img:           interSrc,
		refMask:       1 << uint(vp9dec.LastFrame),
		allowHP:       true,
		selectFc:      fc,
		referenceMode: vp9dec.SingleReference,
	}
	tile := vp9dec.TileBounds{MiRowStart: 0, MiRowEnd: 8, MiColStart: 0, MiColEnd: 8}
	first, ok := e.pickVP9InterReferenceMode(inter, tile, 8, 8,
		0, 0, common.Block32x64)
	if !ok {
		t.Fatal("first child inter mode returned !ok")
	}
	withoutContext, ok := e.pickVP9InterReferenceMode(inter, tile, 8, 8,
		0, 4, common.Block32x64)
	if !ok {
		t.Fatal("second child without context returned !ok")
	}
	e.fillVP9MiGrid(8, 8, 0, 0, common.Block32x64,
		vp9InterModeDecisionMi(common.Block32x64, first))
	withContext, ok := e.pickVP9InterReferenceMode(inter, tile, 8, 8,
		0, 4, common.Block32x64)
	if !ok {
		t.Fatal("second child with context returned !ok")
	}
	if withoutContext.mode == common.NearestMv {
		t.Fatalf("second child without context unexpectedly chose NearestMv")
	}
	if withContext.mode != common.NearestMv {
		t.Fatalf("second child with context mode = %d, want NearestMv", withContext.mode)
	}
	if withContext.score >= withoutContext.score {
		t.Fatalf("contextual score = %d, want lower than uncached score %d",
			withContext.score, withoutContext.score)
	}
}

func TestVP9EncoderInterPicksVert32x32ForHorizontalMixedMotion(t *testing.T) {
	const (
		width  = 32
		height = 32
	)
	// CpuUsed: -3 retains the recursive RD partition picker; see the speed-8
	// rationale on the 64x64 sibling test.
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height, CpuUsed: -3})
	keySrc := newVP9MotionYCbCrForTest(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := splitShiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 8, -8)
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	d := decodeVP9KeyInterForTest(t, key, inter)
	left := d.miGrid[0]
	right := d.miGrid[2]
	if left.SbType != common.Block16x32 || right.SbType != common.Block16x32 {
		t.Fatalf("top blocks = %d/%d, want Block16x32/Block16x32",
			left.SbType, right.SbType)
	}
	assertVP9InterMotionBlockForTest(t, "left", left, vp9dec.MV{Col: 64})
	assertVP9InterMotionBlockForTest(t, "right", right, vp9dec.MV{Col: -64})
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after 32x32 vertical-partition inter frame")
	}
}

func TestVP9EncoderInterPicksVert16x16ForHorizontalMixedMotion(t *testing.T) {
	const (
		width  = 16
		height = 16
	)
	// CpuUsed: -3 retains the recursive RD partition picker; see the speed-8
	// rationale on the 64x64 sibling test.
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height, CpuUsed: -3})
	keySrc := newVP9MotionYCbCrForTest(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := splitShiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 4, -4)
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	d := decodeVP9KeyInterForTest(t, key, inter)
	left := d.miGrid[0]
	right := d.miGrid[1]
	if left.SbType != common.Block8x16 || right.SbType != common.Block8x16 {
		t.Fatalf("top blocks = %d/%d, want Block8x16/Block8x16",
			left.SbType, right.SbType)
	}
	assertVP9InterMotionBlockForTest(t, "left", left, vp9dec.MV{Col: 32})
	assertVP9InterMotionBlockForTest(t, "right", right, vp9dec.MV{Col: -32})
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after 16x16 vertical-partition inter frame")
	}
}

func TestVP9EncoderInterPicksHorz64x64ForVerticalMixedMotion(t *testing.T) {
	const (
		width  = 64
		height = 64
	)
	// CpuUsed: -3 retains the recursive RD partition picker; see the speed-8
	// rationale on the 64x64 sibling test.
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height, CpuUsed: -3})
	keySrc := newVP9MotionYCbCrForTest(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := splitYShiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 8, -8)
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	d := decodeVP9KeyInterForTest(t, key, inter)
	top := d.miGrid[0]
	bottom := d.miGrid[4*8]
	if top.SbType != common.Block64x32 || bottom.SbType != common.Block64x32 {
		t.Fatalf("left blocks = %d/%d, want Block64x32/Block64x32",
			top.SbType, bottom.SbType)
	}
	assertVP9InterMotionBlockForTest(t, "top", top, vp9dec.MV{Row: 64})
	assertVP9InterMotionBlockForTest(t, "bottom", bottom, vp9dec.MV{Row: -64})
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after horizontal-partition inter frame")
	}
}

func TestVP9EncoderInterSplits64x64ForQuadrantMotion(t *testing.T) {
	const (
		width  = 64
		height = 64
	)
	// CpuUsed: -3 retains the recursive RD partition picker; see the speed-8
	// rationale on the 64x64 sibling test.
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height, CpuUsed: -3})
	keySrc := newVP9MotionYCbCrForTest(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := quadrantShiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img,
		image.Point{X: 8}, image.Point{X: -8},
		image.Point{Y: 8}, image.Point{Y: -8})
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	d := decodeVP9KeyInterForTest(t, key, inter)
	topLeft := d.miGrid[0]
	topRight := d.miGrid[4]
	bottomLeft := d.miGrid[4*8]
	bottomRight := d.miGrid[4*8+4]
	for _, block := range []struct {
		name string
		mi   vp9dec.NeighborMi
	}{
		{"top-left", topLeft},
		{"top-right", topRight},
		{"bottom-left", bottomLeft},
		{"bottom-right", bottomRight},
	} {
		if common.Num8x8BlocksWideLookup[block.mi.SbType] > 4 ||
			common.Num8x8BlocksHighLookup[block.mi.SbType] > 4 {
			t.Fatalf("%s block size = %d, want no larger than Block32x32",
				block.name, block.mi.SbType)
		}
	}
	assertVP9InterMotionBlockForTest(t, "top-left", topLeft, vp9dec.MV{Col: 64})
	assertVP9InterMotionBlockForTest(t, "top-right", topRight, vp9dec.MV{Col: -64})
	assertVP9InterMotionBlockForTest(t, "bottom-left", bottomLeft, vp9dec.MV{Row: 64})
	assertVP9InterMotionBlockForTest(t, "bottom-right", bottomRight, vp9dec.MV{Row: -64})
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after split-partition inter frame")
	}
}

func TestVP9EncoderInterPicksOddIntegerMv(t *testing.T) {
	const (
		width  = 128
		height = 64
	)
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height, CpuUsed: -3})
	keySrc := newVP9MotionYCbCrForTest(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := shiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 7, 0)
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after keyframe")
	}
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter: %v", err)
	}
	want := vp9dec.MV{Col: 56}
	if got := d.miGrid[0]; got.Mode != common.NewMv || got.Mv[0] != want {
		t.Fatalf("top-left inter = mode %d mv %+v, want NewMv %+v",
			got.Mode, got.Mv[0], want)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after odd-MV inter frame")
	}
}

func TestVP9EncoderInterPicksQuarterPelMv(t *testing.T) {
	const (
		width  = 128
		height = 64
	)
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height, CpuUsed: -3})
	keySrc := newVP9MotionYCbCrForTest(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	want := vp9dec.MV{Col: 58}
	interSrc := predictedVP9ReferenceYCbCrForTest(t, e.refFrames[0].img, want)
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after keyframe")
	}
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter: %v", err)
	}
	if got := d.miGrid[0]; got.Mode != common.NewMv || got.Mv[0] != want {
		t.Fatalf("top-left inter = mode %d mv %+v, want NewMv %+v",
			got.Mode, got.Mv[0], want)
	} else if got.InterpFilter != uint8(vp9dec.InterpEighttap) {
		t.Fatalf("top-left interp filter = %d, want Eighttap", got.InterpFilter)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after quarter-pel inter frame")
	}
}

func TestVP9EncoderInterPicksEighthPelMv(t *testing.T) {
	const (
		width  = 128
		height = 64
	)
	// CpuUsed: -3 forces the SPEED_FEATURES dispatcher to speed=3, which
	// retains SubpelForceStop=EighthPel. The default normalisation routes
	// CpuUsed=0 to realtime+speed=8 (SubpelForceStop=QuarterPel), where
	// 1/8-pel granularity is intentionally suppressed.
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height, CpuUsed: -3})
	keySrc := newVP9MotionYCbCrForTest(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	want := vp9dec.MV{Col: 57}
	interSrc := predictedVP9ReferenceYCbCrForTest(t, e.refFrames[0].img, want)
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	var keyBR vp9dec.BitReader
	keyBR.Init(key)
	keyHeader, err := vp9dec.ReadUncompressedHeader(&keyBR, nil, nil)
	if err != nil {
		t.Fatalf("ReadUncompressedHeader keyframe: %v", err)
	}
	var interBR vp9dec.BitReader
	interBR.Init(inter)
	interHeader, err := vp9dec.ReadUncompressedHeader(&interBR, &keyHeader,
		func(uint8) (uint32, uint32) { return width, height })
	if err != nil {
		t.Fatalf("ReadUncompressedHeader inter: %v", err)
	}
	if !interHeader.AllowHighPrecisionMv {
		t.Fatal("AllowHighPrecisionMv = false, want true")
	}

	d := decodeVP9KeyInterForTest(t, key, inter)
	if got := d.miGrid[0]; got.Mode != common.NewMv || got.Mv[0] != want {
		t.Fatalf("top-left inter = mode %d mv %+v, want NewMv %+v",
			got.Mode, got.Mv[0], want)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after eighth-pel inter frame")
	}
}

func TestVP9EncoderCountsNewMvSymbols(t *testing.T) {
	var counts vp9enc.FrameCounts
	countVP9NewMv(&counts, vp9dec.MV{Col: 58}, vp9dec.MV{Col: 2})

	if counts.Mv.Joints[tables.MvJointHnzVz] != 1 {
		t.Fatalf("horizontal joint count = %d, want 1",
			counts.Mv.Joints[tables.MvJointHnzVz])
	}
	for joint, got := range counts.Mv.Joints {
		if joint != tables.MvJointHnzVz && got != 0 {
			t.Fatalf("Joints[%d] = %d, want 0", joint, got)
		}
	}
	if counts.Mv.Comps[0].Sign != [2]uint32{} {
		t.Fatalf("row component counts = %v, want zero", counts.Mv.Comps[0].Sign)
	}
	col := counts.Mv.Comps[1]
	if col.Sign != [2]uint32{1, 0} {
		t.Fatalf("col sign counts = %v, want [1 0]", col.Sign)
	}
	classTotal := uint32(0)
	for _, got := range col.Classes {
		classTotal += got
	}
	if classTotal != 1 {
		t.Fatalf("col class total = %d, want 1", classTotal)
	}
}

func TestVP9EncoderInterReusesNearestMvCandidate(t *testing.T) {
	const (
		width  = 192
		height = 64
	)
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height, CpuUsed: -3})
	keySrc := newVP9MotionYCbCrForTest(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := shiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 8, 0)
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after keyframe")
	}
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter: %v", err)
	}
	if len(d.miGrid) < 9 {
		t.Fatalf("decoder MI grid len = %d, want at least 9", len(d.miGrid))
	}
	want := vp9dec.MV{Col: 64}
	if got := d.miGrid[0]; got.Mode != common.NewMv || got.Mv[0] != want {
		t.Fatalf("first block = mode %d mv %+v, want NewMv %+v",
			got.Mode, got.Mv[0], want)
	}
	if got := d.miGrid[8]; got.Mode != common.NearestMv || got.Mv[0] != want {
		t.Fatalf("second block = mode %d mv %+v, want NearestMv %+v",
			got.Mode, got.Mv[0], want)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after NearestMv inter frame")
	}
}

func TestVP9EncoderInterUsesPreviousFrameMvRefs(t *testing.T) {
	const (
		width  = 128
		height = 64
	)
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height, CpuUsed: -3})
	keySrc := newVP9MotionYCbCrForTest(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	inter1Src := shiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 8, 0)
	inter1, err := e.Encode(inter1Src)
	if err != nil {
		t.Fatalf("Encode first inter: %v", err)
	}
	inter2Src := shiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 8, 0)
	inter2, err := e.Encode(inter2Src)
	if err != nil {
		t.Fatalf("Encode second inter: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	frames := []struct {
		name   string
		packet []byte
	}{
		{"key", key},
		{"inter1", inter1},
		{"inter2", inter2},
	}
	for _, frame := range frames {
		name, packet := frame.name, frame.packet
		if err := d.Decode(packet); err != nil {
			t.Fatalf("Decode %s: %v", name, err)
		}
		if _, ok := d.NextFrame(); !ok {
			t.Fatalf("NextFrame returned !ok after %s", name)
		}
	}
	if len(d.miGrid) == 0 {
		t.Fatal("decoder MI grid is empty after second inter frame")
	}
	want := vp9dec.MV{Col: 64}
	if got := d.miGrid[0]; got.Mode != common.NearestMv || got.Mv[0] != want {
		t.Fatalf("second inter top-left = mode %d mv %+v, want NearestMv %+v",
			got.Mode, got.Mv[0], want)
	}
}
