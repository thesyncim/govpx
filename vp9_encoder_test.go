package govpx

import (
	"encoding/binary"
	"errors"
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

func newVP9YCbCrForTest(width, height int, y, u, v byte) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	fillVP9YCbCrForTest(img, y, u, v)
	return img
}

func newVP9CheckerYCbCrForTest(width, height int, lo, hi, u, v byte) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	for y := range height {
		row := img.Y[y*img.YStride:]
		for x := range width {
			if (x+y)&1 == 0 {
				row[x] = lo
			} else {
				row[x] = hi
			}
		}
	}
	for i := range img.Cb {
		img.Cb[i] = u
	}
	for i := range img.Cr {
		img.Cr[i] = v
	}
	return img
}

func newVP9HorizontalBandsForTest(width, height int, u, v byte) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	for y := range height {
		row := img.Y[y*img.YStride:]
		value := byte(32 + (y*5)%192)
		for x := range width {
			row[x] = value
		}
	}
	for i := range img.Cb {
		img.Cb[i] = u
	}
	for i := range img.Cr {
		img.Cr[i] = v
	}
	return img
}

func newVP9ChromaHorizontalBandsForTest(width, height int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	for i := range img.Y {
		img.Y[i] = 128
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for y := range uvHeight {
		cb := img.Cb[y*img.CStride:]
		cr := img.Cr[y*img.CStride:]
		cbValue := byte(32 + (y*7)%192)
		crValue := byte(48 + (y*11)%176)
		for x := range uvWidth {
			cb[x] = cbValue
			cr[x] = crValue
		}
	}
	return img
}

func newVP9MotionYCbCrForTest(width, height int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	for y := range height {
		row := img.Y[y*img.YStride:]
		for x := range width {
			row[x] = byte(16 + (x*7+y*11+(x*y)%37)%224)
		}
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for y := range uvHeight {
		cb := img.Cb[y*img.CStride:]
		cr := img.Cr[y*img.CStride:]
		for x := range uvWidth {
			cb[x] = byte(64 + (x*5+y*3)%128)
			cr[x] = byte(48 + (x*3+y*7)%160)
		}
	}
	return img
}

func shiftedVP9ReferenceYCbCrForTest(ref Image, dx, dy int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, ref.Width, ref.Height), image.YCbCrSubsampleRatio420)
	shiftPlane := func(dst []byte, dstStride int, src []byte, srcStride, width, height, planeDx, planeDy int) {
		for y := range height {
			dstRow := dst[y*dstStride:]
			sy := clampVP9IntForTest(y+planeDy, 0, height-1)
			srcRow := src[sy*srcStride:]
			for x := range width {
				sx := clampVP9IntForTest(x+planeDx, 0, width-1)
				dstRow[x] = srcRow[sx]
			}
		}
	}
	shiftPlane(img.Y, img.YStride, ref.Y, ref.YStride, ref.Width, ref.Height, dx, dy)
	uvWidth := (ref.Width + 1) >> 1
	uvHeight := (ref.Height + 1) >> 1
	shiftPlane(img.Cb, img.CStride, ref.U, ref.UStride, uvWidth, uvHeight, dx>>1, dy>>1)
	shiftPlane(img.Cr, img.CStride, ref.V, ref.VStride, uvWidth, uvHeight, dx>>1, dy>>1)
	return img
}

func splitShiftedVP9ReferenceYCbCrForTest(ref Image, leftDx, rightDx int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, ref.Width, ref.Height), image.YCbCrSubsampleRatio420)
	shiftPlane := func(dst []byte, dstStride int, src []byte, srcStride, width, height, planeLeftDx, planeRightDx int) {
		splitX := width / 2
		for y := range height {
			dstRow := dst[y*dstStride:]
			srcRow := src[y*srcStride:]
			for x := range width {
				dx := planeLeftDx
				if x >= splitX {
					dx = planeRightDx
				}
				sx := clampVP9IntForTest(x+dx, 0, width-1)
				dstRow[x] = srcRow[sx]
			}
		}
	}
	shiftPlane(img.Y, img.YStride, ref.Y, ref.YStride, ref.Width, ref.Height, leftDx, rightDx)
	uvWidth := (ref.Width + 1) >> 1
	uvHeight := (ref.Height + 1) >> 1
	shiftPlane(img.Cb, img.CStride, ref.U, ref.UStride, uvWidth, uvHeight, leftDx>>1, rightDx>>1)
	shiftPlane(img.Cr, img.CStride, ref.V, ref.VStride, uvWidth, uvHeight, leftDx>>1, rightDx>>1)
	return img
}

func predictedVP9ReferenceYCbCrForTest(t *testing.T, ref Image, mv vp9dec.MV) *image.YCbCr {
	t.Helper()
	var d VP9Decoder
	vp9dec.SetupBlockPlanes(&d.planes, 1, 1)
	d.prepareVP9OutputFrame(ref.Width, ref.Height)
	d.refFrames[0].store(ref)
	hdr := vp9dec.UncompressedHeader{
		Width:  uint32(ref.Width),
		Height: uint32(ref.Height),
		InterRef: vp9dec.InterRefBlock{
			RefIndex: [3]uint8{0, 0, 0},
		},
		InterpFilter: vp9dec.InterpEighttap,
	}
	miRows := (ref.Height + 7) >> 3
	miCols := (ref.Width + 7) >> 3
	for miRow := 0; miRow < miRows; miRow += common.MiBlockSize {
		for miCol := 0; miCol < miCols; miCol += common.MiBlockSize {
			bsize := vp9StubBlockSizeForRegion(miRows, miCols,
				miRow, miCol, common.Block64x64)
			mi := vp9dec.NeighborMi{
				SbType:       bsize,
				Mode:         common.NewMv,
				InterpFilter: uint8(vp9dec.InterpEighttap),
				RefFrame: [2]int8{
					vp9dec.LastFrame,
					vp9dec.NoRefFrame,
				},
				Mv: [2]vp9dec.MV{mv},
			}
			if !d.reconstructVP9InterPredictBlock(&hdr, &mi,
				miRow, miCol, vp9ModeInfoDecodeBSize(bsize)) {
				t.Fatalf("reconstruct predictor block at mi %d,%d failed", miRow, miCol)
			}
		}
	}
	img := image.NewYCbCr(image.Rect(0, 0, ref.Width, ref.Height), image.YCbCrSubsampleRatio420)
	copyPlane(img.Y, img.YStride, d.lastFrame.Y, d.lastFrame.YStride, ref.Width, ref.Height)
	uvWidth := (ref.Width + 1) >> 1
	uvHeight := (ref.Height + 1) >> 1
	copyPlane(img.Cb, img.CStride, d.lastFrame.U, d.lastFrame.UStride, uvWidth, uvHeight)
	copyPlane(img.Cr, img.CStride, d.lastFrame.V, d.lastFrame.VStride, uvWidth, uvHeight)
	return img
}

func clampVP9IntForTest(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func fillVP9YCbCrForTest(img *image.YCbCr, y, u, v byte) {
	for i := range img.Y {
		img.Y[i] = y
	}
	for i := range img.Cb {
		img.Cb[i] = u
	}
	for i := range img.Cr {
		img.Cr[i] = v
	}
}

// TestNewVP9EncoderRequiresDimensions: Width and Height must both be
// positive; zero or negative values get rejected with
// ErrInvalidConfig.
func TestNewVP9EncoderRequiresDimensions(t *testing.T) {
	cases := []VP9EncoderOptions{
		{Width: 0, Height: 480},
		{Width: 640, Height: 0},
		{Width: -1, Height: 480},
	}
	for i, opts := range cases {
		_, err := NewVP9Encoder(opts)
		if !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("case %d: err = %v, want ErrInvalidConfig", i, err)
		}
	}
}

// TestNewVP9EncoderRejectsBadOptions covers the per-field bounds
// checks beyond the dimension gate.
func TestNewVP9EncoderRejectsBadOptions(t *testing.T) {
	base := VP9EncoderOptions{Width: 320, Height: 240}
	type bad struct {
		mutate func(*VP9EncoderOptions)
		want   error
	}
	cases := []bad{
		{func(o *VP9EncoderOptions) { o.Threads = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.TargetBitrateKbps = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.Quantizer = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.Quantizer = 256 }, ErrInvalidQuantizer},
		{func(o *VP9EncoderOptions) { o.MaxKeyframeInterval = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.FPS = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.TimebaseNum = 1 }, ErrInvalidConfig}, // missing Den
		{func(o *VP9EncoderOptions) { o.TimebaseDen = 1 }, ErrInvalidConfig}, // missing Num
	}
	for i, c := range cases {
		opts := base
		c.mutate(&opts)
		_, err := NewVP9Encoder(opts)
		if !errors.Is(err, c.want) {
			t.Errorf("case %d: err = %v, want %v", i, err, c.want)
		}
	}
}

// TestNewVP9EncoderAcceptsMinimalOptions: a bare {Width,Height}
// works.
func TestNewVP9EncoderAcceptsMinimalOptions(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 320, Height: 240})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if got := e.Codec(); got != CodecVP9 {
		t.Errorf("Codec() = %v, want CodecVP9", got)
	}
}

func TestVP9EncoderRejectsInvalidSourceShape(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	dst := make([]byte, 1024)

	if _, err := e.EncodeInto(nil, dst); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil source err = %v, want ErrInvalidConfig", err)
	}

	wrongSize := image.NewYCbCr(image.Rect(0, 0, 32, 64), image.YCbCrSubsampleRatio420)
	if _, err := e.EncodeInto(wrongSize, dst); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("wrong-size source err = %v, want ErrInvalidConfig", err)
	}

	wrongChroma := image.NewYCbCr(image.Rect(0, 0, 64, 64), image.YCbCrSubsampleRatio444)
	if _, err := e.EncodeInto(wrongChroma, dst); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("wrong-chroma source err = %v, want ErrInvalidConfig", err)
	}

	valid := image.NewYCbCr(image.Rect(0, 0, 64, 64), image.YCbCrSubsampleRatio420)
	if _, err := e.EncodeInto(valid, nil); !errors.Is(err, ErrBufferTooSmall) {
		t.Fatalf("empty dst err = %v, want ErrBufferTooSmall", err)
	}
}

// TestVP9EncoderKeyframeStubProducesParseableBitstream: the
// zero-residue keyframe path emits a Block64x64 PartitionNone + DC-pred +
// skip=1 frame whose every layer parses cleanly through the
// existing decoder primitives.
func TestVP9EncoderKeyframeStubProducesParseableBitstream(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	img := newVP9YCbCrForTest(64, 64, 128, 128, 128)
	got, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("Encode returned empty bytes")
	}

	// Layer 1: uncompressed header.
	var br vp9dec.BitReader
	br.Init(got)
	h, perr := vp9dec.ReadUncompressedHeader(&br, nil, nil)
	if perr != nil {
		t.Fatalf("ReadUncompressedHeader: %v", perr)
	}
	if h.Width != 64 || h.Height != 64 {
		t.Errorf("size = (%d, %d), want (64, 64)", h.Width, h.Height)
	}
	if h.FrameType != common.KeyFrame {
		t.Errorf("FrameType = %d, want KeyFrame", h.FrameType)
	}
	if h.FirstPartitionSize == 0 {
		t.Fatal("FirstPartitionSize = 0 (compressed header empty)")
	}
	uncSize := br.BytesRead()

	// Layer 2: compressed header. The encoder may emit counts-driven
	// probability updates, so the parsed frame context is the one the
	// tile body must use.
	compEnd := uncSize + int(h.FirstPartitionSize)
	if compEnd > len(got) {
		t.Fatalf("compressed header end %d past frame %d", compEnd, len(got))
	}
	var cr bitstream.Reader
	if err := cr.Init(got[uncSize:compEnd]); err != nil {
		t.Fatalf("compressed reader Init: %v", err)
	}
	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	out := vp9dec.ReadCompressedHeader(&cr, &fc, vp9dec.ReadCompressedHeaderArgs{
		Lossless:     false,
		IntraOnly:    true,
		KeyFrame:     true,
		InterpFilter: vp9dec.InterpEighttap,
	})
	if out.TxMode != common.Allow32x32 {
		t.Errorf("TxMode = %d, want Allow32x32", out.TxMode)
	}

	// Layer 3: tile body. The 1-tile case has no size prefix; the
	// SB walk starts immediately after the compressed header.
	var tr bitstream.Reader
	if err := tr.Init(got[compEnd:]); err != nil {
		t.Fatalf("tile reader Init: %v", err)
	}
	aboveCtx := make([]int8, 16)
	leftCtx := make([]int8, common.MiBlockSize)
	maps := vp9dec.IntraSegmentMaps{
		CurrentFrameSegMap: make([]uint8, 64),
		MiCols:             8,
	}
	var seg vp9dec.SegmentationParams

	// Single SB at Block64x64 with PartitionNone — walk the
	// partition + read the per-block intra mode.
	bsl := int(common.BWidthLog2Lookup[common.Block64x64])
	bs := (1 << uint(bsl)) / 4
	ctx := vp9dec.PartitionPlaneContext(aboveCtx, leftCtx, 0, 0, common.Block64x64)
	// Keyframes use vp9_kf_partition_probs, not fc.PartitionProb —
	// see set_partition_probs in libvpx's vp9_onyxc_int.h.
	probs := tables.KfPartitionProbs[ctx][:]
	miRows := int((h.Height + 7) >> 3)
	miCols := int((h.Width + 7) >> 3)
	hasRows := bs < miRows
	hasCols := bs < miCols
	partition := vp9dec.ReadPartition(&tr, probs, hasRows, hasCols)
	if partition != common.PartitionNone {
		t.Errorf("root partition = %d, want PartitionNone", partition)
	}

	leafMi := &vp9dec.NeighborMi{SbType: common.Block64x64}
	mode := vp9dec.ReadIntraFrameModeInfo(vp9dec.IntraFrameDriverArgs{
		Reader:   &tr,
		Fc:       &fc,
		Seg:      &seg,
		Maps:     &maps,
		TxMode:   out.TxMode,
		MiOffset: 0,
		XMis:     8, YMis: 8,
	}, leafMi)
	if leafMi.Mode != common.DcPred {
		t.Errorf("Y mode = %d, want DcPred", leafMi.Mode)
	}
	if leafMi.Skip != 1 {
		t.Errorf("Skip = %d, want 1", leafMi.Skip)
	}
	if leafMi.TxSize != common.Tx32x32 {
		t.Errorf("TxSize = %d, want Tx32x32", leafMi.TxSize)
	}
	if mode.UvMode != common.DcPred {
		t.Errorf("UV mode = %d, want DcPred", mode.UvMode)
	}
	if leafMi.RefFrame[0] != vp9dec.IntraFrame {
		t.Errorf("RefFrame[0] = %d, want IntraFrame", leafMi.RefFrame[0])
	}
}

func TestVP9EncoderKeyframeConstantSourceRoundTrip(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 96, Height: 80})
	img := newVP9YCbCrForTest(96, 80, 91, 143, 37)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
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
		t.Fatal("NextFrame returned !ok after source-backed keyframe")
	}
	assertVP9FilledFrame(t, frame, 96, 80, 91, 143, 37)
}

func TestVP9EncoderKeyframeACResiduePreservesCheckerSource(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 32, Height: 32})
	img := newVP9CheckerYCbCrForTest(32, 32, 48, 208, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
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
		t.Fatal("NextFrame returned !ok after checker keyframe")
	}
	assertVP9VisibleYContrast(t, frame, 32, 32, 40)
}

func TestVP9EncoderACKeyframeUsesCountsDrivenCompressedHeader(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	img := newVP9CheckerYCbCrForTest(64, 64, 48, 208, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	var br vp9dec.BitReader
	br.Init(packet)
	h, err := vp9dec.ReadUncompressedHeader(&br, nil, nil)
	if err != nil {
		t.Fatalf("ReadUncompressedHeader: %v", err)
	}
	if h.FirstPartitionSize <= 2 {
		t.Fatalf("FirstPartitionSize = %d, want counts-driven compressed header larger than no-update", h.FirstPartitionSize)
	}
}

func TestVP9EncoderInterDcResidueTracksChangedConstantSource(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 96, Height: 80})
	keySrc := newVP9YCbCrForTest(96, 80, 82, 123, 211)
	interSrc := newVP9YCbCrForTest(96, 80, 201, 44, 19)
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
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after keyframe")
	}
	assertVP9FilledFrame(t, frame, 96, 80, 82, 123, 211)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter: %v", err)
	}
	frame, ok = d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after visible inter frame")
	}
	assertVP9FilledFrame(t, frame, 96, 80, 201, 44, 19)
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
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
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

func TestVP9EncoderInterPicksNewMvFor16x8Block(t *testing.T) {
	const (
		width  = 32
		height = 8
	)
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
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

func TestVP9EncoderInterSplits64x64ForMixedMotion(t *testing.T) {
	const (
		width  = 64
		height = 64
	)
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
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
	left := d.miGrid[0]
	right := d.miGrid[4]
	if left.SbType != common.Block32x32 || right.SbType != common.Block32x32 {
		t.Fatalf("top blocks = %d/%d, want Block32x32/Block32x32",
			left.SbType, right.SbType)
	}
	if left.Mode != common.NewMv || left.Mv[0] != (vp9dec.MV{Col: 64}) {
		t.Fatalf("left block = mode %d mv %+v, want NewMv {Col:64}",
			left.Mode, left.Mv[0])
	}
	if right.Mode != common.NewMv || right.Mv[0] != (vp9dec.MV{Col: -64}) {
		t.Fatalf("right block = mode %d mv %+v, want NewMv {Col:-64}",
			right.Mode, right.Mv[0])
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after split-motion inter frame")
	}
}

func TestVP9EncoderInterSplits32x32ForMixedMotion(t *testing.T) {
	const (
		width  = 32
		height = 32
	)
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
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
	left := d.miGrid[0]
	right := d.miGrid[2]
	if left.SbType != common.Block16x16 || right.SbType != common.Block16x16 {
		t.Fatalf("top blocks = %d/%d, want Block16x16/Block16x16",
			left.SbType, right.SbType)
	}
	if left.Mode != common.NewMv || left.Mv[0] != (vp9dec.MV{Col: 64}) {
		t.Fatalf("left block = mode %d mv %+v, want NewMv {Col:64}",
			left.Mode, left.Mv[0])
	}
	if right.Mode != common.NewMv || right.Mv[0] != (vp9dec.MV{Col: -64}) {
		t.Fatalf("right block = mode %d mv %+v, want NewMv {Col:-64}",
			right.Mode, right.Mv[0])
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after 32x32 split-motion inter frame")
	}
}

func TestVP9EncoderInterSplits16x16ForMixedMotion(t *testing.T) {
	const (
		width  = 16
		height = 16
	)
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
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
	left := d.miGrid[0]
	right := d.miGrid[1]
	if left.SbType != common.Block8x8 || right.SbType != common.Block8x8 {
		t.Fatalf("top blocks = %d/%d, want Block8x8/Block8x8",
			left.SbType, right.SbType)
	}
	if left.Mode != common.NewMv || left.Mv[0] != (vp9dec.MV{Col: 32}) {
		t.Fatalf("left block = mode %d mv %+v, want NewMv {Col:32}",
			left.Mode, left.Mv[0])
	}
	if right.Mode != common.NewMv || right.Mv[0] != (vp9dec.MV{Col: -32}) {
		t.Fatalf("right block = mode %d mv %+v, want NewMv {Col:-32}",
			right.Mode, right.Mv[0])
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after 16x16 split-motion inter frame")
	}
}

func TestVP9EncoderInterPicksOddIntegerMv(t *testing.T) {
	const (
		width  = 128
		height = 64
	)
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
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
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
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
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after quarter-pel inter frame")
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
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
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
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
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

func TestVP9EncoderForceKeyFrameIsStickyUntilCommitted(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	src := newVP9YCbCrForTest(width, height, 96, 128, 128)
	if _, err := e.Encode(src); err != nil {
		t.Fatalf("Encode initial keyframe: %v", err)
	}
	if e.IsKeyFrameNext() {
		t.Fatal("IsKeyFrameNext = true after initial keyframe, want false")
	}

	e.ForceKeyFrame()
	if !e.IsKeyFrameNext() {
		t.Fatal("IsKeyFrameNext = false after ForceKeyFrame, want true")
	}
	if _, err := e.EncodeInto(src, nil); !errors.Is(err, ErrBufferTooSmall) {
		t.Fatalf("EncodeInto nil err = %v, want ErrBufferTooSmall", err)
	}
	if !e.IsKeyFrameNext() {
		t.Fatal("ForceKeyFrame was consumed by failed EncodeInto")
	}

	forced, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode forced keyframe: %v", err)
	}
	var br vp9dec.BitReader
	br.Init(forced)
	h, perr := vp9dec.ReadUncompressedHeader(&br, nil, nil)
	if perr != nil {
		t.Fatalf("ReadUncompressedHeader forced keyframe: %v", perr)
	}
	if h.FrameType != common.KeyFrame {
		t.Fatalf("forced frame type = %d, want KeyFrame", h.FrameType)
	}
	if e.IsKeyFrameNext() {
		t.Fatal("IsKeyFrameNext still true after forced keyframe commit")
	}
}

func TestVP9EncoderEncodeIntoWithFlagsForceKeyFrameOneShot(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	src := newVP9YCbCrForTest(width, height, 96, 128, 128)
	if _, err := e.Encode(src); err != nil {
		t.Fatalf("Encode initial keyframe: %v", err)
	}
	dst := make([]byte, 65536)
	n, err := e.EncodeIntoWithFlags(src, dst, EncodeForceKeyFrame)
	if err != nil {
		t.Fatalf("EncodeIntoWithFlags force keyframe: %v", err)
	}
	var br vp9dec.BitReader
	br.Init(dst[:n])
	h, perr := vp9dec.ReadUncompressedHeader(&br, nil, nil)
	if perr != nil {
		t.Fatalf("ReadUncompressedHeader forced keyframe: %v", perr)
	}
	if h.FrameType != common.KeyFrame {
		t.Fatalf("forced frame type = %d, want KeyFrame", h.FrameType)
	}
	if e.IsKeyFrameNext() {
		t.Fatal("EncodeForceKeyFrame acted sticky; next frame should be inter")
	}
}

func TestVP9EncoderEncodeIntoWithFlagsNoUpdateLast(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := newVP9YCbCrForTest(width, height, 64, 128, 128)
	if _, err := e.Encode(keySrc); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := newVP9YCbCrForTest(width, height, 160, 128, 128)
	dst := make([]byte, 65536)
	n, err := e.EncodeIntoWithFlags(interSrc, dst, EncodeNoUpdateLast)
	if err != nil {
		t.Fatalf("EncodeIntoWithFlags no-update-LAST: %v", err)
	}

	var br vp9dec.BitReader
	br.Init(dst[:n])
	refDims := func(slot uint8) (uint32, uint32) {
		if slot > vp9AltRefSlot {
			t.Fatalf("inter header requested ref slot %d, want <= %d", slot, vp9AltRefSlot)
		}
		return width, height
	}
	h, perr := vp9dec.ReadUncompressedHeader(&br, nil, refDims)
	if perr != nil {
		t.Fatalf("ReadUncompressedHeader inter: %v", perr)
	}
	if h.FrameType != common.InterFrame {
		t.Fatalf("frame type = %d, want InterFrame", h.FrameType)
	}
	if h.InterRef.RefIndex != [3]uint8{vp9LastRefSlot, vp9GoldenRefSlot, vp9AltRefSlot} {
		t.Fatalf("RefIndex = %v, want LAST/GOLDEN/ALTREF slots 0/1/2", h.InterRef.RefIndex)
	}
	if h.RefreshFrameFlags != 0 {
		t.Fatalf("RefreshFrameFlags = %#x, want 0", h.RefreshFrameFlags)
	}
	if !e.refFrames[0].valid {
		t.Fatal("LAST ref became invalid after no-update-LAST")
	}
	if got := e.refFrames[0].img.Y[0]; got != keySrc.Y[0] {
		t.Fatalf("LAST ref Y[0] = %d, want prior keyframe value %d", got, keySrc.Y[0])
	}
}

func TestVP9EncoderEncodeIntoWithFlagsForceGoldenAltRefRefreshesSlots(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := newVP9YCbCrForTest(width, height, 64, 128, 128)
	if _, err := e.Encode(keySrc); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}

	interSrc := newVP9YCbCrForTest(width, height, 160, 96, 224)
	packet, err := e.EncodeWithFlags(interSrc, EncodeForceGoldenFrame|EncodeForceAltRefFrame)
	if err != nil {
		t.Fatalf("EncodeWithFlags force GF/ARF: %v", err)
	}
	info, err := PeekVP9StreamInfo(packet)
	if err != nil {
		t.Fatalf("PeekVP9StreamInfo: %v", err)
	}
	if info.RefreshFrameFlags != 0x07 {
		t.Fatalf("RefreshFrameFlags = %#x, want LAST|GOLDEN|ALTREF", info.RefreshFrameFlags)
	}
	for _, slot := range []int{vp9LastRefSlot, vp9GoldenRefSlot, vp9AltRefSlot} {
		if !e.refValid[slot] || !e.refFrames[slot].valid {
			t.Fatalf("reference slot %d was not refreshed", slot)
		}
	}
	if got := e.refFrames[vp9GoldenRefSlot].img.Y[0]; got == keySrc.Y[0] {
		t.Fatalf("GOLDEN ref Y[0] still has keyframe value %d", got)
	}
	if got := e.refFrames[vp9AltRefSlot].img.Y[0]; got == keySrc.Y[0] {
		t.Fatalf("ALTREF ref Y[0] still has keyframe value %d", got)
	}
}

func TestVP9EncoderEncodeIntoWithFlagsForceGoldenCanSkipLastUpdate(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := newVP9YCbCrForTest(width, height, 72, 128, 128)
	if _, err := e.Encode(keySrc); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}

	interSrc := newVP9YCbCrForTest(width, height, 196, 96, 224)
	packet, err := e.EncodeWithFlags(interSrc, EncodeForceGoldenFrame|EncodeNoUpdateLast)
	if err != nil {
		t.Fatalf("EncodeWithFlags force GF/no-update-LAST: %v", err)
	}
	info, err := PeekVP9StreamInfo(packet)
	if err != nil {
		t.Fatalf("PeekVP9StreamInfo: %v", err)
	}
	if info.RefreshFrameFlags != 0x02 {
		t.Fatalf("RefreshFrameFlags = %#x, want GOLDEN only", info.RefreshFrameFlags)
	}
	if got := e.refFrames[vp9LastRefSlot].img.Y[0]; got != keySrc.Y[0] {
		t.Fatalf("LAST ref Y[0] = %d, want prior keyframe value %d", got, keySrc.Y[0])
	}
	if got := e.refFrames[vp9GoldenRefSlot].img.Y[0]; got == keySrc.Y[0] {
		t.Fatalf("GOLDEN ref Y[0] still has keyframe value %d", got)
	}
}

func TestVP9EncoderEncodeIntoWithFlagsNoReferenceLastCanUseGolden(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := newVP9YCbCrForTest(width, height, 72, 128, 128)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	goldenSrc := newVP9YCbCrForTest(width, height, 188, 96, 224)
	goldenRefresh, err := e.EncodeWithFlags(goldenSrc,
		EncodeForceGoldenFrame|EncodeNoUpdateLast)
	if err != nil {
		t.Fatalf("Encode force-GOLDEN: %v", err)
	}
	inter, err := e.EncodeWithFlags(goldenSrc,
		EncodeNoReferenceLast|EncodeNoReferenceAltRef|EncodeNoUpdateLast)
	if err != nil {
		t.Fatalf("Encode GOLDEN-only inter: %v", err)
	}
	info, err := PeekVP9StreamInfo(inter)
	if err != nil {
		t.Fatalf("PeekVP9StreamInfo: %v", err)
	}
	if info.KeyFrame {
		t.Fatal("NoReferenceLast forced a keyframe despite usable GOLDEN")
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	for _, packet := range [][]byte{key, goldenRefresh, inter} {
		if err := d.Decode(packet); err != nil {
			t.Fatalf("Decode packet: %v", err)
		}
	}
	if len(d.miGrid) == 0 {
		t.Fatal("decoder MI grid is empty after GOLDEN-only inter")
	}
	if got := d.miGrid[0]; got.RefFrame[0] != vp9dec.GoldenFrame || got.Mv[0] != (vp9dec.MV{}) {
		t.Fatalf("top-left inter = ref %d mode %d mv %+v, want GOLDEN with zero MV",
			got.RefFrame[0], got.Mode, got.Mv[0])
	}
}

func TestVP9EncoderEncodeIntoWithFlagsNoReferenceLastGoldenCanUseAltRef(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := newVP9YCbCrForTest(width, height, 64, 128, 128)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	altSrc := newVP9YCbCrForTest(width, height, 44, 208, 96)
	altRefresh, err := e.EncodeWithFlags(altSrc,
		EncodeForceAltRefFrame|EncodeNoUpdateLast|EncodeNoUpdateGolden)
	if err != nil {
		t.Fatalf("Encode force-ALTREF: %v", err)
	}
	inter, err := e.EncodeWithFlags(altSrc,
		EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoUpdateLast)
	if err != nil {
		t.Fatalf("Encode ALTREF-only inter: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	for _, packet := range [][]byte{key, altRefresh, inter} {
		if err := d.Decode(packet); err != nil {
			t.Fatalf("Decode packet: %v", err)
		}
	}
	if len(d.miGrid) == 0 {
		t.Fatal("decoder MI grid is empty after ALTREF-only inter")
	}
	if got := d.miGrid[0]; got.RefFrame[0] != vp9dec.AltrefFrame || got.Mv[0] != (vp9dec.MV{}) {
		t.Fatalf("top-left inter = ref %d mode %d mv %+v, want ALTREF with zero MV",
			got.RefFrame[0], got.Mode, got.Mv[0])
	}
}

func TestVP9EncoderEncodeIntoWithFlagsNoUpdateEntropyRestoresFrameContext(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := newVP9CheckerYCbCrForTest(width, height, 0, 255, 128, 128)
	if _, err := e.Encode(keySrc); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	before := e.fc
	interSrc := newVP9CheckerYCbCrForTest(width, height, 255, 0, 128, 128)
	dst := make([]byte, 65536)
	if _, err := e.EncodeIntoWithFlags(interSrc, dst, EncodeNoUpdateEntropy); err != nil {
		t.Fatalf("EncodeIntoWithFlags no-update-entropy: %v", err)
	}
	if e.fc != before {
		t.Fatal("frame context changed after EncodeNoUpdateEntropy")
	}
}

func TestVP9EncoderErrorResilientRestoresDefaultFrameContext(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width: width, Height: height, ErrorResilient: true,
	})
	src := newVP9CheckerYCbCrForTest(width, height, 0, 255, 128, 128)
	if _, err := e.Encode(src); err != nil {
		t.Fatalf("Encode error-resilient keyframe: %v", err)
	}
	var want vp9dec.FrameContext
	vp9dec.ResetFrameContext(&want)
	if e.fc != want {
		t.Fatal("frame context changed after error-resilient keyframe")
	}
	if _, err := e.Encode(newVP9CheckerYCbCrForTest(width, height, 255, 0, 128, 128)); err != nil {
		t.Fatalf("Encode error-resilient inter: %v", err)
	}
	if e.fc != want {
		t.Fatal("frame context changed after error-resilient inter frame")
	}
}

func TestVP9EncoderEncodeIntoWithFlagsRejectsUnsupportedFlags(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	src := newVP9YCbCrForTest(width, height, 96, 128, 128)
	dst := make([]byte, 65536)
	for _, flags := range []EncodeFlags{
		EncodeInvisibleFrame,
		EncodeNoUpdateLast,
		EncodeForceGoldenFrame | EncodeNoUpdateGolden,
		EncodeForceAltRefFrame | EncodeNoUpdateAltRef,
	} {
		if _, err := e.EncodeIntoWithFlags(src, dst, flags); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("flags %#x err = %v, want ErrInvalidConfig", flags, err)
		}
	}
}

func TestVP9InterModeScoreIncludesNewMvRate(t *testing.T) {
	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)

	zeroRate := vp9InterModeRateCost(&fc, 0, common.ZeroMv,
		vp9dec.MV{}, vp9dec.MV{})
	newRate := vp9InterModeRateCost(&fc, 0, common.NewMv,
		vp9dec.MV{Col: 64}, vp9dec.MV{})
	if newRate <= zeroRate {
		t.Fatalf("NEWMV rate = %d, want greater than ZEROMV rate %d",
			newRate, zeroRate)
	}
	if got, wantGreater := vp9InterModeScore(0, newRate, 1),
		vp9InterModeScore(0, zeroRate, 1); got <= wantGreater {
		t.Fatalf("equal-SAD NEWMV score = %d, want greater than ZEROMV score %d",
			got, wantGreater)
	}
	if got, wantLess := vp9InterModeScore(0, newRate, 1),
		vp9InterModeScore(4096, zeroRate, 1); got >= wantLess {
		t.Fatalf("large-gain NEWMV score = %d, want less than ZEROMV score %d",
			got, wantLess)
	}
}

func TestVP9BlockSADNoLimitMatchesScalar(t *testing.T) {
	const stride = 80
	src := make([]byte, stride*80)
	ref := make([]byte, stride*80)
	for i := range src {
		src[i] = byte((i*17 + i/7) & 0xff)
		ref[i] = byte((i*29 + 11) & 0xff)
	}
	cases := []struct {
		w, h int
	}{
		{64, 64}, {64, 32}, {32, 64}, {32, 32}, {32, 16},
		{16, 32}, {16, 16}, {16, 8}, {8, 16}, {8, 8},
		{8, 4}, {4, 8}, {4, 4},
	}
	for _, tc := range cases {
		got := vp9BlockSAD(src, stride, ref, stride,
			3, 5, 7, 11, tc.w, tc.h, ^uint64(0))
		want := vp9BlockSAD(src, stride, ref, stride,
			3, 5, 7, 11, tc.w, tc.h, 1<<63)
		if got != want {
			t.Fatalf("%dx%d SAD = %d, want scalar %d", tc.w, tc.h, got, want)
		}
	}
}

// TestVP9EncoderInterSkipProducesParseableBitstream covers the public
// second-frame path: a visible LAST/ZeroMv skipped inter frame whose
// reference dimensions come from the preceding keyframe.
func TestVP9EncoderInterSkipProducesParseableBitstream(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	img := newVP9YCbCrForTest(64, 64, 128, 128, 128)
	key, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	inter, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	if len(inter) == 0 {
		t.Fatal("Encode returned empty inter frame")
	}

	var keyBR vp9dec.BitReader
	keyBR.Init(key)
	keyHeader, perr := vp9dec.ReadUncompressedHeader(&keyBR, nil, nil)
	if perr != nil {
		t.Fatalf("ReadUncompressedHeader keyframe: %v", perr)
	}

	var interBR vp9dec.BitReader
	interBR.Init(inter)
	refDims := func(slot uint8) (uint32, uint32) {
		if slot > vp9AltRefSlot {
			t.Fatalf("inter header requested ref slot %d, want <= %d", slot, vp9AltRefSlot)
		}
		return 64, 64
	}
	interHeader, perr := vp9dec.ReadUncompressedHeader(&interBR, &keyHeader, refDims)
	if perr != nil {
		t.Fatalf("ReadUncompressedHeader inter: %v", perr)
	}
	if interHeader.FrameType != common.InterFrame {
		t.Errorf("FrameType = %d, want InterFrame", interHeader.FrameType)
	}
	if !interHeader.ShowFrame {
		t.Error("ShowFrame = false, want true")
	}
	if interHeader.IntraOnly {
		t.Error("IntraOnly = true, want false")
	}
	if interHeader.RefreshFrameFlags != 1 {
		t.Errorf("RefreshFrameFlags = %#x, want 0x1", interHeader.RefreshFrameFlags)
	}
	if interHeader.Width != 64 || interHeader.Height != 64 {
		t.Errorf("size = (%d, %d), want (64, 64)", interHeader.Width, interHeader.Height)
	}
	if interHeader.InterRef.RefIndex != [3]uint8{vp9LastRefSlot, vp9GoldenRefSlot, vp9AltRefSlot} {
		t.Errorf("RefIndex = %v, want LAST/GOLDEN/ALTREF slots 0/1/2", interHeader.InterRef.RefIndex)
	}
	if interHeader.InterRef.SignBias != [3]uint8{0, 0, 0} {
		t.Errorf("SignBias = %v, want [0 0 0]", interHeader.InterRef.SignBias)
	}
	if interHeader.AllowHighPrecisionMv {
		t.Error("AllowHighPrecisionMv = true, want false")
	}
	if interHeader.InterpFilter != vp9dec.InterpEighttap {
		t.Errorf("InterpFilter = %d, want Eighttap", interHeader.InterpFilter)
	}
	if interHeader.FirstPartitionSize == 0 {
		t.Fatal("FirstPartitionSize = 0 (compressed header empty)")
	}

	uncSize := interBR.BytesRead()
	compEnd := uncSize + int(interHeader.FirstPartitionSize)
	if compEnd > len(inter) {
		t.Fatalf("compressed header end %d past frame %d", compEnd, len(inter))
	}
	var cr bitstream.Reader
	if err := cr.Init(inter[uncSize:compEnd]); err != nil {
		t.Fatalf("compressed reader Init: %v", err)
	}
	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	out := vp9dec.ReadCompressedHeader(&cr, &fc, vp9dec.ReadCompressedHeaderArgs{
		Lossless:             false,
		IntraOnly:            false,
		KeyFrame:             false,
		InterpFilter:         vp9dec.InterpEighttap,
		AllowHighPrecisionMv: false,
		CompoundRefAllowed:   false,
	})
	if cr.HasError() {
		t.Fatal("compressed header reader reported over-read")
	}
	if out.TxMode != common.Allow32x32 {
		t.Errorf("TxMode = %d, want Allow32x32", out.TxMode)
	}
	if out.ReferenceMode != vp9dec.SingleReference {
		t.Errorf("ReferenceMode = %d, want SingleReference", out.ReferenceMode)
	}
	if compEnd >= len(inter) {
		t.Fatal("inter frame has no tile payload")
	}
}

// TestVP9EncoderKeyframeMultiSb: 128x64 frame → 2 SBs side-by-side.
// Confirms the SB walker emits 2 PartitionNone leaves in row-major
// order and both decode through the per-block keyframe driver.
func TestVP9EncoderKeyframeMultiSb(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 128, Height: 64})
	img := newVP9YCbCrForTest(128, 64, 128, 128, 128)
	got, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	var br vp9dec.BitReader
	br.Init(got)
	h, _ := vp9dec.ReadUncompressedHeader(&br, nil, nil)
	uncSize := br.BytesRead()
	compEnd := uncSize + int(h.FirstPartitionSize)

	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	var cr bitstream.Reader
	cr.Init(got[uncSize:compEnd])
	out := vp9dec.ReadCompressedHeader(&cr, &fc, vp9dec.ReadCompressedHeaderArgs{
		Lossless: false, IntraOnly: true, KeyFrame: true,
		InterpFilter: vp9dec.InterpEighttap,
	})

	var tr bitstream.Reader
	tr.Init(got[compEnd:])
	miRows := int((h.Height + 7) >> 3)
	miCols := int((h.Width + 7) >> 3)
	aboveCtx := make([]int8, miCols)
	leftCtx := make([]int8, common.MiBlockSize)
	maps := vp9dec.IntraSegmentMaps{
		CurrentFrameSegMap: make([]uint8, miRows*miCols),
		MiCols:             miCols,
	}
	var seg vp9dec.SegmentationParams
	miGrid := make([]vp9dec.NeighborMi, miRows*miCols)
	miAt := func(r, c int) *vp9dec.NeighborMi {
		if r < 0 || c < 0 || r >= miRows || c >= miCols {
			return nil
		}
		return &miGrid[r*miCols+c]
	}
	fillMi := func(r, c int, bsize common.BlockSize, mi vp9dec.NeighborMi) {
		rows := int(common.Num8x8BlocksHighLookup[bsize])
		cols := int(common.Num8x8BlocksWideLookup[bsize])
		for rr := 0; rr < rows && r+rr < miRows; rr++ {
			row := miGrid[(r+rr)*miCols:]
			for cc := 0; cc < cols && c+cc < miCols; cc++ {
				row[c+cc] = mi
			}
		}
	}

	// Half-step (hbs) for Block64x64 in mi units: (1 << bsl) / 4 = 4.
	const hbs = 4
	walked := 0
	for miCol := 0; miCol < miCols; miCol += common.MiBlockSize {
		ctx := vp9dec.PartitionPlaneContext(aboveCtx, leftCtx, 0, miCol, common.Block64x64)
		probs := tables.KfPartitionProbs[ctx][:]
		hasRows := (0 + hbs) < miRows
		hasCols := (miCol + hbs) < miCols
		p := vp9dec.ReadPartition(&tr, probs, hasRows, hasCols)
		if p != common.PartitionNone {
			t.Errorf("SB at miCol=%d: partition = %d, want PartitionNone", miCol, p)
		}
		leafMi := &vp9dec.NeighborMi{SbType: common.Block64x64}
		mode := vp9dec.ReadIntraFrameModeInfo(vp9dec.IntraFrameDriverArgs{
			Reader: &tr, Fc: &fc, Seg: &seg, Maps: &maps,
			TxMode:   out.TxMode,
			MiOffset: miCol, XMis: common.MiBlockSize, YMis: common.MiBlockSize,
			Above: miAt(-1, miCol),
			Left:  miAt(0, miCol-1),
		}, leafMi)
		if leafMi.Mode != common.DcPred || mode.UvMode != common.DcPred {
			t.Errorf("SB at miCol=%d: Y=%d UV=%d, want DcPred/DcPred",
				miCol, leafMi.Mode, mode.UvMode)
		}
		if leafMi.TxSize != common.Tx32x32 {
			t.Errorf("SB at miCol=%d: TxSize = %d, want Tx32x32", miCol, leafMi.TxSize)
		}
		fillMi(0, miCol, common.Block64x64, *leafMi)
		// Update partition context (decoder side mirror of encoder stamp).
		vp9dec.UpdatePartitionContext(aboveCtx, leftCtx, 0, miCol,
			common.Block64x64, common.MiBlockSize)
		walked++
	}
	if walked != 2 {
		t.Errorf("walked %d SBs, want 2", walked)
	}
}

func TestVP9EncoderKeyframePicksHorizontalModeFromLeftContext(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 128, Height: 64})
	img := newVP9HorizontalBandsForTest(128, 64, 128, 128)
	vp9dec.SetupBlockPlanes(&e.planes, 1, 1)
	e.prepareVP9EncoderOutputFrame(128, 64)
	for y := range 64 {
		copy(e.reconY[y*e.reconFrame.YStride:y*e.reconFrame.YStride+64],
			img.Y[y*img.YStride:y*img.YStride+64])
	}

	hdr := vp9dec.UncompressedHeader{Width: 128, Height: 64}
	key := &vp9KeyframeEncodeState{img: img, hdr: &hdr}
	mi := vp9dec.NeighborMi{SbType: common.Block64x64, TxSize: common.Tx32x32}
	tile := vp9dec.TileBounds{MiRowStart: 0, MiRowEnd: 8, MiColStart: 0, MiColEnd: 16}
	got := e.pickVP9KeyframeMode(key, tile, 8, 16, 0, 8, common.Block64x64, &mi)
	if got != common.HPred {
		t.Errorf("mode = %d, want HPred", got)
	}
}

func TestVP9EncoderKeyframePicksHorizontalUvModeFromLeftContext(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 128, Height: 64})
	img := newVP9ChromaHorizontalBandsForTest(128, 64)
	vp9dec.SetupBlockPlanes(&e.planes, 1, 1)
	e.prepareVP9EncoderOutputFrame(128, 64)
	for y := range 32 {
		copy(e.reconU[y*e.reconFrame.UStride:y*e.reconFrame.UStride+32],
			img.Cb[y*img.CStride:y*img.CStride+32])
		copy(e.reconV[y*e.reconFrame.VStride:y*e.reconFrame.VStride+32],
			img.Cr[y*img.CStride:y*img.CStride+32])
	}

	hdr := vp9dec.UncompressedHeader{Width: 128, Height: 64}
	key := &vp9KeyframeEncodeState{img: img, hdr: &hdr}
	mi := vp9dec.NeighborMi{
		SbType: common.Block64x64,
		Mode:   common.DcPred,
		TxSize: common.Tx32x32,
	}
	tile := vp9dec.TileBounds{MiRowStart: 0, MiRowEnd: 8, MiColStart: 0, MiColEnd: 16}
	got := e.pickVP9KeyframeUvMode(key, tile, 8, 16, 0, 8, common.Block64x64, &mi)
	if got != common.HPred {
		t.Errorf("UV mode = %d, want HPred", got)
	}
}

func TestVP9EncoderKeyframeChromaBandsRoundTrip(t *testing.T) {
	const width, height = 128, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	img := newVP9ChromaHorizontalBandsForTest(width, height)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
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
		t.Fatal("NextFrame returned !ok after chroma keyframe")
	}
	assertVP9VisibleChromaContrast(t, frame, width, height, 48)
}

func TestVP9EncoderWideFrameUsesMinimumLegalTileColumns(t *testing.T) {
	const width, height = 4160, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	img := newVP9YCbCrForTest(width, height, 91, 143, 37)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	h, tileStart := parseVP9EncoderHeaderForTest(t, packet)
	minLog2, _ := vp9dec.TileNBits(int((uint32(width) + 7) >> 3))
	if minLog2 < 1 {
		t.Fatalf("test frame min tile columns = %d, want >= 1", minLog2)
	}
	if h.Tile.Log2TileCols != minLog2 {
		t.Fatalf("Log2TileCols = %d, want minimum legal %d",
			h.Tile.Log2TileCols, minLog2)
	}
	assertVP9EncoderTilePrefixForTest(t, packet, tileStart)

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after multi-tile keyframe")
	}
	assertVP9FilledFrame(t, frame, width, height, 91, 143, 37)
}

func TestVP9EncoderThreadsHintIncreasesTileColumns(t *testing.T) {
	const width, height = 1280, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width:   width,
		Height:  height,
		Threads: 4,
	})
	img := newVP9YCbCrForTest(width, height, 82, 123, 211)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	h, tileStart := parseVP9EncoderHeaderForTest(t, packet)
	if h.Tile.Log2TileCols != 2 {
		t.Fatalf("Log2TileCols = %d, want 2 for Threads=4",
			h.Tile.Log2TileCols)
	}
	assertVP9EncoderTilePrefixForTest(t, packet, tileStart)

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after threaded-tile keyframe")
	}
	assertVP9FilledFrame(t, frame, width, height, 82, 123, 211)
}

// TestVP9EncoderIVFRoundTrip wraps the encoded keyframe in an IVF
// container and round-trips it through the existing IVF parser.
// Confirms the encoder's output is a valid VP9-IVF stream — the
// shape vpxdec --codec=vp9 expects on disk.
func TestVP9EncoderIVFRoundTrip(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	img := newVP9YCbCrForTest(64, 64, 128, 128, 128)
	payload, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	header := testutil.IVFHeader{
		FourCC:              [4]byte{'V', 'P', '9', '0'},
		Width:               64,
		Height:              64,
		TimebaseDenominator: 30,
		TimebaseNumerator:   1,
		FrameCount:          1,
	}
	stream := append(testutil.WriteIVFHeader(header), testutil.WriteIVFFrame(payload, 0)...)

	gotHdr, err := testutil.ParseIVFHeader(stream)
	if err != nil {
		t.Fatalf("ParseIVFHeader: %v", err)
	}
	if gotHdr.FourCC != header.FourCC {
		t.Errorf("FourCC = %v, want VP90", gotHdr.FourCC)
	}
	if gotHdr.Width != 64 || gotHdr.Height != 64 {
		t.Errorf("ivf size = (%d, %d), want (64, 64)", gotHdr.Width, gotHdr.Height)
	}

	offset, err := testutil.FirstIVFFrameOffset(stream)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset: %v", err)
	}
	frame, _, err := testutil.NextIVFFrame(stream, offset, 0)
	if err != nil {
		t.Fatalf("NextIVFFrame: %v", err)
	}
	if len(frame.Data) != len(payload) {
		t.Errorf("frame size = %d, want %d", len(frame.Data), len(payload))
	}
	for i := range payload {
		if frame.Data[i] != payload[i] {
			t.Errorf("byte %d differs: %#x != %#x", i, frame.Data[i], payload[i])
			break
		}
	}

	// And the recovered payload still parses as a VP9 keyframe.
	var br vp9dec.BitReader
	br.Init(frame.Data)
	h, perr := vp9dec.ReadUncompressedHeader(&br, nil, nil)
	if perr != nil {
		t.Fatalf("ReadUncompressedHeader on IVF payload: %v", perr)
	}
	if h.FrameType != common.KeyFrame {
		t.Errorf("recovered FrameType = %d, want KeyFrame", h.FrameType)
	}
}

// TestVP9EncoderEncodeIntoSteadyStateAlloc verifies that the
// caller-owned output path allocates only during setup / growth. The
// hot path reuses the compressed-header scratch, partition contexts,
// and MI grid across frames.
func TestVP9EncoderEncodeIntoSteadyStateAlloc(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 256, Height: 192})
	img := newVP9YCbCrForTest(256, 192, 128, 128, 128)
	dst := make([]byte, 65536)

	if _, err := e.EncodeInto(img, dst); err != nil {
		t.Fatalf("warm EncodeInto: %v", err)
	}

	var n int
	var err error
	allocs := testing.AllocsPerRun(100, func() {
		e.frameIndex = 0
		n, err = e.EncodeInto(img, dst)
	})
	if err != nil {
		t.Fatalf("EncodeInto: %v", err)
	}
	if n == 0 {
		t.Fatal("EncodeInto wrote no bytes")
	}
	if allocs != 0 {
		t.Fatalf("EncodeInto steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9EncoderEncodeIntoSourceKeyframeSteadyStateAlloc(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 256, Height: 192})
	img := newVP9YCbCrForTest(256, 192, 87, 144, 39)
	dst := make([]byte, 65536)

	if _, err := e.EncodeInto(img, dst); err != nil {
		t.Fatalf("warm EncodeInto: %v", err)
	}

	var n int
	var err error
	allocs := testing.AllocsPerRun(100, func() {
		e.frameIndex = 0
		n, err = e.EncodeInto(img, dst)
	})
	if err != nil {
		t.Fatalf("EncodeInto source keyframe: %v", err)
	}
	if n == 0 {
		t.Fatal("EncodeInto source keyframe wrote no bytes")
	}
	if allocs != 0 {
		t.Fatalf("EncodeInto source keyframe steady state: got %v allocs/op, want 0", allocs)
	}
}

// TestVP9EncoderEncodeIntoInterSteadyStateAlloc verifies that visible
// inter-frame header/mode emission reuses the keyframe-allocated scratch,
// partition contexts, and MI grid.
func TestVP9EncoderEncodeIntoInterSteadyStateAlloc(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 256, Height: 192})
	img := newVP9YCbCrForTest(256, 192, 128, 128, 128)
	dst := make([]byte, 65536)

	if _, err := e.EncodeInto(img, dst); err != nil {
		t.Fatalf("warm keyframe EncodeInto: %v", err)
	}
	if _, err := e.EncodeInto(img, dst); err != nil {
		t.Fatalf("warm inter EncodeInto: %v", err)
	}

	var n int
	var err error
	allocs := testing.AllocsPerRun(100, func() {
		e.frameIndex = 1
		n, err = e.EncodeInto(img, dst)
	})
	if err != nil {
		t.Fatalf("EncodeInto inter: %v", err)
	}
	if n == 0 {
		t.Fatal("EncodeInto inter wrote no bytes")
	}
	if allocs != 0 {
		t.Fatalf("EncodeInto inter steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9EncoderEncodeIntoInterResidueSteadyStateAlloc(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 256, Height: 192})
	keySrc := newVP9YCbCrForTest(256, 192, 81, 123, 210)
	interSrc := newVP9YCbCrForTest(256, 192, 204, 47, 18)
	dst := make([]byte, 65536)

	if _, err := e.EncodeInto(keySrc, dst); err != nil {
		t.Fatalf("warm keyframe EncodeInto: %v", err)
	}
	var keyRef vp9ReferenceFrame
	keyRef.store(e.reconFrame)
	if _, err := e.EncodeInto(interSrc, dst); err != nil {
		t.Fatalf("warm inter EncodeInto: %v", err)
	}

	var n int
	var err error
	allocs := testing.AllocsPerRun(100, func() {
		e.frameIndex = 1
		e.refFrames[0].store(keyRef.img)
		n, err = e.EncodeInto(interSrc, dst)
	})
	if err != nil {
		t.Fatalf("EncodeInto inter residue: %v", err)
	}
	if n == 0 {
		t.Fatal("EncodeInto inter residue wrote no bytes")
	}
	if allocs != 0 {
		t.Fatalf("EncodeInto inter residue steady state: got %v allocs/op, want 0", allocs)
	}
}

func assertVP9VisibleYContrast(t *testing.T, got Image, width, height int, minDelta byte) {
	t.Helper()
	if got.Width != width || got.Height != height {
		t.Fatalf("frame dimensions = %dx%d, want %dx%d",
			got.Width, got.Height, width, height)
	}
	if got.YStride < width || len(got.Y) < planeLen(got.YStride, height, width) {
		t.Fatalf("Y plane shape = len %d stride %d, want %dx%d",
			len(got.Y), got.YStride, width, height)
	}
	lo, hi := byte(255), byte(0)
	for y := range height {
		row := got.Y[y*got.YStride:]
		for x := range width {
			v := row[x]
			if v < lo {
				lo = v
			}
			if v > hi {
				hi = v
			}
		}
	}
	if hi-lo < minDelta {
		t.Fatalf("visible Y contrast = %d..%d, want delta >= %d", lo, hi, minDelta)
	}
}

func assertVP9VisibleChromaContrast(t *testing.T, got Image, width, height int, minDelta byte) {
	t.Helper()
	if got.Width != width || got.Height != height {
		t.Fatalf("frame dimensions = %dx%d, want %dx%d",
			got.Width, got.Height, width, height)
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	if got.UStride < uvWidth || got.VStride < uvWidth ||
		len(got.U) < planeLen(got.UStride, uvHeight, uvWidth) ||
		len(got.V) < planeLen(got.VStride, uvHeight, uvWidth) {
		t.Fatalf("UV plane shape = U len %d stride %d, V len %d stride %d, want %dx%d",
			len(got.U), got.UStride, len(got.V), got.VStride, uvWidth, uvHeight)
	}
	lo, hi := byte(255), byte(0)
	for y := range uvHeight {
		uRow := got.U[y*got.UStride:]
		vRow := got.V[y*got.VStride:]
		for x := range uvWidth {
			for _, v := range [...]byte{uRow[x], vRow[x]} {
				if v < lo {
					lo = v
				}
				if v > hi {
					hi = v
				}
			}
		}
	}
	if hi-lo < minDelta {
		t.Fatalf("visible UV contrast = %d..%d, want delta >= %d", lo, hi, minDelta)
	}
}

func parseVP9EncoderHeaderForTest(t *testing.T, packet []byte) (vp9dec.UncompressedHeader, int) {
	t.Helper()
	var br vp9dec.BitReader
	br.Init(packet)
	h, err := vp9dec.ReadUncompressedHeader(&br, nil, nil)
	if err != nil {
		t.Fatalf("ReadUncompressedHeader: %v", err)
	}
	tileStart := br.BytesRead() + int(h.FirstPartitionSize)
	if tileStart > len(packet) {
		t.Fatalf("tile start %d past packet len %d", tileStart, len(packet))
	}
	return h, tileStart
}

func assertVP9EncoderTilePrefixForTest(t *testing.T, packet []byte, tileStart int) {
	t.Helper()
	if len(packet)-tileStart < 5 {
		t.Fatalf("multi-tile payload too small: tileStart=%d packet=%d",
			tileStart, len(packet))
	}
	firstTileSize := int(binary.BigEndian.Uint32(packet[tileStart : tileStart+4]))
	if firstTileSize <= 0 {
		t.Fatalf("first tile size prefix = %d, want > 0", firstTileSize)
	}
	if tileStart+4+firstTileSize >= len(packet) {
		t.Fatalf("first tile consumes packet: start=%d size=%d len=%d",
			tileStart, firstTileSize, len(packet))
	}
}

// TestVP9EncoderClose: after Close, Encode/EncodeInto return
// ErrClosed.
func TestVP9EncoderClose(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 320, Height: 240})
	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	img := image.NewYCbCr(image.Rect(0, 0, 320, 240), image.YCbCrSubsampleRatio420)
	if _, err := e.Encode(img); !errors.Is(err, ErrClosed) {
		t.Errorf("Encode after Close err = %v, want ErrClosed", err)
	}
}

// TestVP9EncoderIsKeyFrameNextCadence: first frame is always a key;
// later frames key on MaxKeyframeInterval boundaries (default 128).
func TestVP9EncoderIsKeyFrameNextCadence(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width: 320, Height: 240, MaxKeyframeInterval: 4,
	})
	if !e.IsKeyFrameNext() {
		t.Error("first frame should be key")
	}
	// Pretend we encoded one frame.
	e.frameIndex = 1
	if e.IsKeyFrameNext() {
		t.Error("frame 1 should NOT be key when cadence=4")
	}
	e.frameIndex = 4
	if !e.IsKeyFrameNext() {
		t.Error("frame 4 should be key (cadence boundary)")
	}
	// After Close → never key.
	e.Close()
	if e.IsKeyFrameNext() {
		t.Error("closed encoder should never report key")
	}
}
