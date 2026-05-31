package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"testing"
)

func TestVP9EncoderInterDcResidueTracksChangedConstantSource(t *testing.T) {
	const width, height = 96, 80
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewYCbCr(width, height, 82, 123, 211)
	interSrc := vp9test.NewYCbCr(width, height, 201, 44, 19)
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
	keySrc := vp9test.NewYCbCr(width, height, 0, 0, 0)
	interSrc := vp9test.NewYCbCr(width, height, 128, 128, 128)
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
	img := vp9test.NewYCbCr(width, height, 128, 128, 128)
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

func TestVP9NonrdIntraScratchUsesLiveInteriorAndReconBorder(t *testing.T) {
	const width, height = 128, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	vp9dec.SetupBlockPlanes(&e.planes, 1, 1)
	e.prepareVP9EncoderOutputFrame(width, height)

	img := vp9test.NewYCbCr(width, height, 0, 128, 128)
	for y := 0; y < 8; y++ {
		e.reconY[y*e.reconFrame.YStride+63] = 152
	}
	var scratch [64 * 64]uint8
	for i := range scratch {
		scratch[i] = 128
	}

	key := &vp9KeyframeEncodeState{
		img: img,
		hdr: &vp9dec.UncompressedHeader{Width: width, Height: height},
	}
	tile := vp9dec.TileBounds{MiRowStart: 0, MiRowEnd: 8, MiColStart: 0, MiColEnd: 16}
	if _, _, ok := e.vp9NoReferenceIntraResidualStatsScratchNoRestore(
		key, common.HPred, common.Tx8x8, tile, 8, 16, 0, 8,
		common.Block8x8, scratch[:], 64, 0, 8); !ok {
		t.Fatal("HPred scratch stats returned !ok")
	}
	for x := 0; x < 8; x++ {
		if got := scratch[7*64+x]; got != 152 {
			t.Fatalf("HPred scratch bottom row[%d] = %d, want recon-border left sample 152",
				x, got)
		}
	}

	if _, _, ok := e.vp9NoReferenceIntraResidualStatsScratchNoRestore(
		key, common.VPred, common.Tx8x8, tile, 8, 16, 1, 8,
		common.Block8x8, scratch[:], 64, 0, 8); !ok {
		t.Fatal("VPred scratch stats returned !ok")
	}
	for x := 0; x < 8; x++ {
		if got := scratch[8*64+x]; got != 152 {
			t.Fatalf("VPred scratch top row[%d] = %d, want live scratch above sample 152",
				x, got)
		}
	}
}

func TestVP9EncoderInterACResiduePreservesCheckerSource(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 32, Height: 32})
	keySrc := vp9test.NewYCbCr(32, 32, 128, 128, 128)
	interSrc := vp9test.NewCheckerYCbCr(32, 32, 48, 208, 128, 128)
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
