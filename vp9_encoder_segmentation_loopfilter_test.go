package govpx

import (
	"errors"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestVP9EncoderStaticSegmentationSignalsHeaderAndMap(t *testing.T) {
	const width, height = 64, 64
	const segID = 3
	const altQ = int16(-12)
	const altLF = int16(4)

	opts := VP9EncoderOptions{Width: width, Height: height}
	opts.Segmentation.Enabled = true
	opts.Segmentation.UpdateMap = true
	opts.Segmentation.SegmentID = segID
	opts.Segmentation.AbsDelta = true
	opts.Segmentation.AltQEnabled[segID] = true
	opts.Segmentation.AltQ[segID] = altQ
	opts.Segmentation.AltLFEnabled[segID] = true
	opts.Segmentation.AltLF[segID] = altLF

	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	src := newVP9CheckerYCbCrForTest(width, height, 48, 208, 128, 128)
	key, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	keyHeader, _ := vp9test.ParseHeader(t, key)
	assertVP9StaticSegmentationHeaderForTest(t, keyHeader.Seg, segID, altQ, altLF)

	inter, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	var br vp9dec.BitReader
	br.Init(inter)
	interHeader, err := vp9dec.ReadUncompressedHeader(&br, &keyHeader,
		func(uint8) (uint32, uint32) { return width, height })
	if err != nil {
		t.Fatalf("ReadUncompressedHeader inter: %v", err)
	}
	assertVP9StaticSegmentationHeaderForTest(t, interHeader.Seg, segID, altQ, altLF)

	d := decodeVP9KeyInterForTest(t, key, inter)
	assertVP9DecoderSegmentIDForTest(t, d, segID)
}

func TestVP9EncoderStaticSkipSegmentForcesSkippedInterBlocks(t *testing.T) {
	const width, height = 64, 64
	const segID = 2

	opts := VP9EncoderOptions{Width: width, Height: height}
	opts.Segmentation.Enabled = true
	opts.Segmentation.UpdateMap = true
	opts.Segmentation.SegmentID = segID
	opts.Segmentation.SkipEnabled[segID] = true

	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	key, err := e.Encode(newVP9CheckerYCbCrForTest(width, height, 16, 240, 128, 128))
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	keyHeader, _ := vp9test.ParseHeader(t, key)
	assertVP9StaticSkipSegmentationHeaderForTest(t, keyHeader.Seg, segID)

	inter, err := e.Encode(newVP9MotionYCbCrForTest(width, height))
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	d := decodeVP9KeyInterForTest(t, key, inter)
	assertVP9DecoderSegmentIDForTest(t, d, segID)
	for i, mi := range d.miGrid {
		if mi.Skip != 1 {
			t.Fatalf("miGrid[%d].Skip = %d, want forced skip", i, mi.Skip)
		}
		if mi.Mode != common.ZeroMv || mi.Mv != ([2]vp9dec.MV{}) {
			t.Fatalf("miGrid[%d] inter mode/mv = %v/%v, want ZeroMv/zero",
				i, mi.Mode, mi.Mv)
		}
	}
}

func TestVP9EncoderStaticRefFrameSegmentForcesGoldenReference(t *testing.T) {
	const width, height = 64, 64
	const segID = 4

	opts := VP9EncoderOptions{Width: width, Height: height}
	opts.Segmentation.Enabled = true
	opts.Segmentation.UpdateMap = true
	opts.Segmentation.SegmentID = segID
	opts.Segmentation.RefFrameEnabled[segID] = true
	opts.Segmentation.RefFrame[segID] = vp9dec.GoldenFrame

	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	key, err := e.Encode(vp9test.NewYCbCr(width, height, 72, 128, 128))
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	keyHeader, _ := vp9test.ParseHeader(t, key)
	assertVP9StaticRefFrameSegmentationHeaderForTest(t, keyHeader.Seg, segID,
		vp9dec.GoldenFrame)

	inter, err := e.Encode(newVP9MotionYCbCrForTest(width, height))
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	var br vp9dec.BitReader
	br.Init(inter)
	interHeader, err := vp9dec.ReadUncompressedHeader(&br, &keyHeader,
		func(uint8) (uint32, uint32) { return width, height })
	if err != nil {
		t.Fatalf("ReadUncompressedHeader inter: %v", err)
	}
	assertVP9StaticRefFrameSegmentationHeaderForTest(t, interHeader.Seg, segID,
		vp9dec.GoldenFrame)

	d := decodeVP9KeyInterForTest(t, key, inter)
	assertVP9DecoderSegmentIDForTest(t, d, segID)
	for i, mi := range d.miGrid {
		if mi.RefFrame != [2]int8{vp9dec.GoldenFrame, vp9dec.NoRefFrame} {
			t.Fatalf("miGrid[%d].RefFrame = %v, want forced GOLDEN",
				i, mi.RefFrame)
		}
	}
}

func TestVP9EncoderStaticInterRefSegmentKeepsInterSyntax(t *testing.T) {
	const width, height = 64, 64
	const segID = 4

	opts := VP9EncoderOptions{Width: width, Height: height}
	opts.Segmentation.Enabled = true
	opts.Segmentation.UpdateMap = true
	opts.Segmentation.SegmentID = segID
	opts.Segmentation.RefFrameEnabled[segID] = true
	opts.Segmentation.RefFrame[segID] = vp9dec.GoldenFrame

	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if _, err := e.Encode(vp9test.NewYCbCr(width, height, 72, 128, 128)); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	if _, err := e.Encode(newVP9CheckerYCbCrForTest(width, height, 16, 240, 96, 224)); err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	for i, mi := range e.miGrid {
		if mi.SegmentID != segID {
			t.Fatalf("encoder miGrid[%d].SegmentID = %d, want %d", i, mi.SegmentID, segID)
		}
		if mi.RefFrame[0] != vp9dec.GoldenFrame {
			t.Fatalf("encoder miGrid[%d].RefFrame = %v, want forced GOLDEN inter syntax",
				i, mi.RefFrame)
		}
	}
}

func TestVP9EncoderStaticRefFrameSegmentForcesIntraBlock(t *testing.T) {
	const width, height = 64, 64
	const segID = 5

	opts := VP9EncoderOptions{Width: width, Height: height}
	opts.Segmentation.Enabled = true
	opts.Segmentation.UpdateMap = true
	opts.Segmentation.SegmentID = segID
	opts.Segmentation.RefFrameEnabled[segID] = true
	opts.Segmentation.RefFrame[segID] = VP9RefFrameIntra

	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	key, err := e.Encode(vp9test.NewYCbCr(width, height, 72, 128, 128))
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	keyHeader, _ := vp9test.ParseHeader(t, key)
	assertVP9StaticRefFrameSegmentationHeaderForTest(t, keyHeader.Seg, segID,
		VP9RefFrameIntra)

	inter, err := e.Encode(newVP9CheckerYCbCrForTest(width, height, 16, 240, 96, 224))
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	d := decodeVP9KeyInterForTest(t, key, inter)
	assertVP9DecoderSegmentIDForTest(t, d, segID)
	for i, mi := range d.miGrid {
		if mi.RefFrame != [2]int8{vp9dec.IntraFrame, vp9dec.NoRefFrame} {
			t.Fatalf("miGrid[%d].RefFrame = %v, want forced INTRA",
				i, mi.RefFrame)
		}
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after forced-intra inter frame")
	}
	assertVP9VisibleYContrast(t, frame, width, height, 40)
	assertVP9VisibleChromaContrast(t, frame, width, height, 40)
}

func TestVP9EncoderStaticRefFrameSegmentRejectsDisabledReference(t *testing.T) {
	const width, height = 64, 64
	const segID = 1

	opts := VP9EncoderOptions{Width: width, Height: height}
	opts.Segmentation.Enabled = true
	opts.Segmentation.UpdateMap = true
	opts.Segmentation.SegmentID = segID
	opts.Segmentation.RefFrameEnabled[segID] = true
	opts.Segmentation.RefFrame[segID] = vp9dec.GoldenFrame

	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if _, err := e.Encode(vp9test.NewYCbCr(width, height, 72, 128, 128)); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	_, err = e.EncodeWithFlags(newVP9MotionYCbCrForTest(width, height),
		EncodeNoReferenceGolden)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("EncodeWithFlags disabled forced reference error = %v, want ErrInvalidConfig", err)
	}
}

func TestVP9EncoderLoopFilterLevelFromQuantizer(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64, Quantizer: 128})
	img := newVP9CheckerYCbCrForTest(64, 64, 32, 224, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	h, _ := vp9test.ParseHeader(t, packet)
	want := vp9LoopFilterLevelFromQuantizerForTest(128, true)
	if h.Loopfilter.FilterLevel != want {
		t.Fatalf("FilterLevel = %d, want q-derived %d", h.Loopfilter.FilterLevel, want)
	}
	if h.Loopfilter.FilterLevel == 0 {
		t.Fatal("FilterLevel = 0, want high-quantizer keyframe to enable filtering")
	}
	wantRef := [vp9dec.MaxRefLfDeltas]int8{1, 0, -1, -1}
	wantMode := [vp9dec.MaxModeLfDeltas]int8{0, 0}
	if !h.Loopfilter.ModeRefDeltaEnabled || !h.Loopfilter.ModeRefDeltaUpdate {
		t.Fatalf("loopfilter delta flags = enabled:%v update:%v, want enabled update",
			h.Loopfilter.ModeRefDeltaEnabled, h.Loopfilter.ModeRefDeltaUpdate)
	}
	if h.Loopfilter.RefDeltas != wantRef {
		t.Fatalf("RefDeltas = %v, want %v", h.Loopfilter.RefDeltas, wantRef)
	}
	if h.Loopfilter.ModeDeltas != wantMode {
		t.Fatalf("ModeDeltas = %v, want %v", h.Loopfilter.ModeDeltas, wantMode)
	}
}

func vp9LoopFilterLevelFromQuantizerForTest(qindex int, isKey bool) uint8 {
	q := int(vp9dec.VpxAcQuant(qindex, 0, vp9dec.BitDepth8))
	level := (q*20723 + 1015158 + (1 << 17)) >> 18
	if isKey {
		level -= 4
	}
	if level < 0 {
		return 0
	}
	if level > vp9dec.MaxLoopFilter {
		return vp9dec.MaxLoopFilter
	}
	return uint8(level)
}

func TestVP9EncoderLastLoopFilterLevel(t *testing.T) {
	var nilEnc *VP9Encoder
	if _, ok := nilEnc.LastLoopFilterLevel(); ok {
		t.Fatal("nil LastLoopFilterLevel ok = true, want false")
	}

	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64, Quantizer: 128})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if _, ok := e.LastLoopFilterLevel(); ok {
		t.Fatal("pre-encode LastLoopFilterLevel ok = true, want false")
	}

	packet, err := e.Encode(newVP9CheckerYCbCrForTest(64, 64, 32, 224, 128, 128))
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	h, _ := vp9test.ParseHeader(t, packet)
	level, ok := e.LastLoopFilterLevel()
	if !ok || level != h.Loopfilter.FilterLevel {
		t.Fatalf("LastLoopFilterLevel = (%d, %t), want (%d, true)",
			level, ok, h.Loopfilter.FilterLevel)
	}

	if err := e.SetDisableLoopfilter(VP9LoopfilterDisableAll); err != nil {
		t.Fatalf("SetDisableLoopfilter: %v", err)
	}
	packet, err = e.Encode(newVP9MotionYCbCrForTest(64, 64))
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	h, _ = vp9test.ParseHeader(t, packet)
	level, ok = e.LastLoopFilterLevel()
	if !ok || level != 0 || h.Loopfilter.FilterLevel != 0 {
		t.Fatalf("disabled LastLoopFilterLevel/header = (%d,%t)/%d, want 0/true/0",
			level, ok, h.Loopfilter.FilterLevel)
	}

	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, ok := e.LastLoopFilterLevel(); ok {
		t.Fatal("closed LastLoopFilterLevel ok = true, want false")
	}
}

func TestVP9EncoderLastLoopFilterLevelIgnoresDroppedFrames(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              width,
		Height:             height,
		FPS:                30,
		TargetBitrateKbps:  1,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		DropFrameAllowed:   true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	src := vp9test.NewYCbCr(width, height, 128, 128, 128)
	dst := make([]byte, 65536)
	if _, err := e.EncodeIntoWithResult(src, dst); err != nil {
		t.Fatalf("key EncodeIntoWithResult: %v", err)
	}
	want, ok := e.LastLoopFilterLevel()
	if !ok {
		t.Fatal("LastLoopFilterLevel after key ok = false, want true")
	}

	e.rc.bufferLevelBits = -e.rc.bitsPerFrame - 1
	result, err := e.EncodeIntoWithResult(src, dst)
	if err != nil {
		t.Fatalf("dropped EncodeIntoWithResult: %v", err)
	}
	if !result.Dropped {
		t.Fatal("second CBR frame was not dropped")
	}
	got, ok := e.LastLoopFilterLevel()
	if !ok || got != want {
		t.Fatalf("LastLoopFilterLevel after drop = (%d,%t), want (%d,true)",
			got, ok, want)
	}
}

func TestVP9EncoderSharpnessOptionAndRuntimeControl(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:        width,
		Height:       height,
		MinQuantizer: 32,
		MaxQuantizer: 32,
		Sharpness:    4,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dst := make([]byte, 65536)
	for i, tc := range []struct {
		name      string
		sharpness uint8
	}{
		{name: "option", sharpness: 4},
		{name: "runtime high", sharpness: 7},
		{name: "runtime disabled", sharpness: 0},
	} {
		if i > 0 {
			if err := e.SetSharpness(tc.sharpness); err != nil {
				t.Fatalf("SetSharpness(%d): %v", tc.sharpness, err)
			}
		}
		src := newVP9CheckerYCbCrForTest(width, height,
			byte(32+i*17), byte(224-i*19), 128, 128)
		n, err := e.EncodeInto(src, dst)
		if err != nil {
			t.Fatalf("%s EncodeInto: %v", tc.name, err)
		}
		h, _ := vp9test.ParseHeader(t, dst[:n])
		if h.Loopfilter.SharpnessLevel != tc.sharpness {
			t.Fatalf("%s sharpness = %d, want %d",
				tc.name, h.Loopfilter.SharpnessLevel, tc.sharpness)
		}
	}
	before := e.opts.Sharpness
	if err := e.SetSharpness(8); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetSharpness invalid err = %v, want ErrInvalidConfig", err)
	}
	if e.opts.Sharpness != before {
		t.Fatalf("invalid SetSharpness mutated encoder to %d, want %d",
			e.opts.Sharpness, before)
	}
}

func TestVP9EncoderStaticThresholdBreakout(t *testing.T) {
	const width, height = 64, 64
	opts := VP9EncoderOptions{
		Width:        width,
		Height:       height,
		MinQuantizer: 4,
		MaxQuantizer: 4,
	}
	baseSrc := vp9test.NewYCbCr(width, height, 64, 128, 128)
	changedSrc := newVP9CheckerYCbCrForTest(width, height, 62, 66, 128, 128)

	noStatic, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder(noStatic): %v", err)
	}
	if _, err := noStatic.Encode(baseSrc); err != nil {
		t.Fatalf("noStatic key Encode: %v", err)
	}
	if _, err := noStatic.Encode(changedSrc); err != nil {
		t.Fatalf("noStatic inter Encode: %v", err)
	}
	if mi := noStatic.vp9MiAt(encoderMacroblockRows(height), encoderMacroblockCols(width), 0, 0); mi == nil || mi.Skip != 0 {
		t.Fatalf("non-static first block skip = %v, want residue", mi)
	}

	breakout, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder(breakout): %v", err)
	}
	if _, err := breakout.Encode(baseSrc); err != nil {
		t.Fatalf("breakout key Encode: %v", err)
	}
	if err := breakout.SetStaticThreshold(1 << 30); err != nil {
		t.Fatalf("SetStaticThreshold: %v", err)
	}
	if _, err := breakout.Encode(changedSrc); err != nil {
		t.Fatalf("breakout inter Encode: %v", err)
	}
	mi := breakout.vp9MiAt(encoderMacroblockRows(height), encoderMacroblockCols(width), 0, 0)
	if mi == nil || mi.Skip != 1 || mi.Mode < common.NearestMv ||
		mi.RefFrame[0] != vp9dec.LastFrame || mi.Mv[0] != (vp9dec.MV{}) {
		t.Fatalf("static breakout mi = %+v, want skipped low-motion LAST", mi)
	}

	before := breakout.opts.StaticThreshold
	if err := breakout.SetStaticThreshold(-1); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetStaticThreshold invalid err = %v, want ErrInvalidConfig", err)
	}
	if breakout.opts.StaticThreshold != before {
		t.Fatalf("invalid SetStaticThreshold mutated threshold to %d, want %d",
			breakout.opts.StaticThreshold, before)
	}
}

func TestVP9EncoderLoopFilterDeltasCarryAcrossInterFrame(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width:     width,
		Height:    height,
		Quantizer: 128,
	})
	keySrc := newVP9CheckerYCbCrForTest(width, height, 32, 224, 128, 128)
	keyPacket, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	keyHeader, _ := vp9test.ParseHeader(t, keyPacket)

	interSrc := newVP9CheckerYCbCrForTest(width, height, 224, 32, 128, 128)
	interPacket, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	var br vp9dec.BitReader
	br.Init(interPacket)
	refDims := func(slot uint8) (uint32, uint32) {
		return width, height
	}
	interHeader, err := vp9dec.ReadUncompressedHeader(&br, &keyHeader, refDims)
	if err != nil {
		t.Fatalf("ReadUncompressedHeader inter: %v", err)
	}

	wantRef := [vp9dec.MaxRefLfDeltas]int8{1, 0, -1, -1}
	wantMode := [vp9dec.MaxModeLfDeltas]int8{0, 0}
	if !interHeader.Loopfilter.ModeRefDeltaEnabled {
		t.Fatal("ModeRefDeltaEnabled = false, want default deltas enabled")
	}
	if interHeader.Loopfilter.ModeRefDeltaUpdate {
		t.Fatal("ModeRefDeltaUpdate = true, want normal inter frame to preserve deltas")
	}
	if interHeader.Loopfilter.RefDeltas != wantRef {
		t.Fatalf("RefDeltas = %v, want %v", interHeader.Loopfilter.RefDeltas, wantRef)
	}
	if interHeader.Loopfilter.ModeDeltas != wantMode {
		t.Fatalf("ModeDeltas = %v, want %v", interHeader.Loopfilter.ModeDeltas, wantMode)
	}
}

func TestVP9EncoderLoopFilteredReferenceMatchesDecodedFrame(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width:     width,
		Height:    height,
		Quantizer: 128,
	})
	img := newVP9CheckerYCbCrForTest(width, height, 32, 224, 96, 224)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !e.refFrames[0].valid {
		t.Fatal("LAST reference was not refreshed")
	}
	h, _ := vp9test.ParseHeader(t, packet)
	if h.Loopfilter.FilterLevel == 0 {
		t.Fatal("FilterLevel = 0, want loopfiltered reference test to exercise filter path")
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after keyframe")
	}
	if !vp9VisibleImageEqual(e.refFrames[0].img, frame) {
		t.Fatal("encoder refreshed reference does not match decoded loopfiltered frame")
	}
}
