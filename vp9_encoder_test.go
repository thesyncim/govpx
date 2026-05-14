package govpx

import (
	"bytes"
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

func newVP9CompoundAverageYCbCrForTest(width, height, delta int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	for y := range height {
		row := img.Y[y*img.YStride:]
		for x := range width {
			base := 96 + (x*5+y*7+(x*y)%19)%64
			row[x] = byte(base + delta)
		}
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for y := range uvHeight {
		cb := img.Cb[y*img.CStride:]
		cr := img.Cr[y*img.CStride:]
		for x := range uvWidth {
			baseCb := 104 + (x*3+y*5)%32
			baseCr := 112 + (x*7+y*2)%32
			cb[x] = byte(baseCb + delta/2)
			cr[x] = byte(baseCr + delta/2)
		}
	}
	return img
}

func newVP9CompoundPairYCbCrForTest(width, height int, variant bool) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	for y := range height {
		row := img.Y[y*img.YStride:]
		for x := range width {
			if variant {
				row[x] = byte(88 + (x*29+y*7+((x+3)*(y+5))%83)%104)
			} else {
				row[x] = byte(48 + (x*17+y*31+(x*y)%67)%120)
			}
		}
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for y := range uvHeight {
		cb := img.Cb[y*img.CStride:]
		cr := img.Cr[y*img.CStride:]
		for x := range uvWidth {
			if variant {
				cb[x] = byte(96 + (x*13+y*5+(x*y)%19)%64)
				cr[x] = byte(88 + (x*7+y*17+(x*y)%23)%72)
			} else {
				cb[x] = byte(72 + (x*11+y*9+(x*y)%17)%72)
				cr[x] = byte(80 + (x*5+y*15+(x*y)%29)%64)
			}
		}
	}
	return img
}

func averageVP9YCbCrForTest(a, b *image.YCbCr) *image.YCbCr {
	width, height := a.Rect.Dx(), a.Rect.Dy()
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	avgPlane := func(dst []byte, dstStride int, ap []byte, aStride int, bp []byte, bStride int, w, h int) {
		for y := range h {
			dstRow := dst[y*dstStride:]
			aRow := ap[y*aStride:]
			bRow := bp[y*bStride:]
			for x := range w {
				dstRow[x] = byte((int(aRow[x]) + int(bRow[x]) + 1) >> 1)
			}
		}
	}
	avgPlane(img.Y, img.YStride, a.Y, a.YStride, b.Y, b.YStride, width, height)
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	avgPlane(img.Cb, img.CStride, a.Cb, a.CStride, b.Cb, b.CStride, uvWidth, uvHeight)
	avgPlane(img.Cr, img.CStride, a.Cr, a.CStride, b.Cr, b.CStride, uvWidth, uvHeight)
	return img
}

func assertVP9InterMotionBlockForTest(t *testing.T, name string,
	mi vp9dec.NeighborMi, want vp9dec.MV,
) {
	t.Helper()
	if mi.Mode != common.NearestMv && mi.Mode != common.NearMv && mi.Mode != common.NewMv {
		t.Fatalf("%s block mode = %d, want an inter motion mode", name, mi.Mode)
	}
	if mi.Mv[0] != want {
		t.Fatalf("%s block MV = %+v, want %+v", name, mi.Mv[0], want)
	}
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

func vp9ImageFromYCbCrForTest(img *image.YCbCr) Image {
	return Image{
		Width:   img.Rect.Dx(),
		Height:  img.Rect.Dy(),
		Y:       img.Y,
		U:       img.Cb,
		V:       img.Cr,
		YStride: img.YStride,
		UStride: img.CStride,
		VStride: img.CStride,
	}
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

func splitYShiftedVP9ReferenceYCbCrForTest(ref Image, topDy, bottomDy int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, ref.Width, ref.Height), image.YCbCrSubsampleRatio420)
	shiftPlane := func(dst []byte, dstStride int, src []byte, srcStride, width, height, planeTopDy, planeBottomDy int) {
		splitY := height / 2
		for y := range height {
			dy := planeTopDy
			if y >= splitY {
				dy = planeBottomDy
			}
			sy := clampVP9IntForTest(y+dy, 0, height-1)
			dstRow := dst[y*dstStride:]
			srcRow := src[sy*srcStride:]
			for x := range width {
				dstRow[x] = srcRow[x]
			}
		}
	}
	shiftPlane(img.Y, img.YStride, ref.Y, ref.YStride, ref.Width, ref.Height, topDy, bottomDy)
	uvWidth := (ref.Width + 1) >> 1
	uvHeight := (ref.Height + 1) >> 1
	shiftPlane(img.Cb, img.CStride, ref.U, ref.UStride, uvWidth, uvHeight, topDy>>1, bottomDy>>1)
	shiftPlane(img.Cr, img.CStride, ref.V, ref.VStride, uvWidth, uvHeight, topDy>>1, bottomDy>>1)
	return img
}

func quadrantShiftedVP9ReferenceYCbCrForTest(ref Image,
	topLeft, topRight, bottomLeft, bottomRight image.Point,
) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, ref.Width, ref.Height), image.YCbCrSubsampleRatio420)
	shiftPlane := func(dst []byte, dstStride int, src []byte, srcStride, width, height int,
		tl, tr, bl, br image.Point,
	) {
		splitX := width / 2
		splitY := height / 2
		for y := range height {
			dstRow := dst[y*dstStride:]
			for x := range width {
				shift := tl
				if y >= splitY {
					shift = bl
					if x >= splitX {
						shift = br
					}
				} else if x >= splitX {
					shift = tr
				}
				srcX := clampVP9IntForTest(x+shift.X, 0, width-1)
				srcY := clampVP9IntForTest(y+shift.Y, 0, height-1)
				dstRow[x] = src[srcY*srcStride+srcX]
			}
		}
	}
	shiftPlane(img.Y, img.YStride, ref.Y, ref.YStride, ref.Width, ref.Height,
		topLeft, topRight, bottomLeft, bottomRight)
	uvWidth := (ref.Width + 1) >> 1
	uvHeight := (ref.Height + 1) >> 1
	uvTopLeft := image.Point{X: topLeft.X >> 1, Y: topLeft.Y >> 1}
	uvTopRight := image.Point{X: topRight.X >> 1, Y: topRight.Y >> 1}
	uvBottomLeft := image.Point{X: bottomLeft.X >> 1, Y: bottomLeft.Y >> 1}
	uvBottomRight := image.Point{X: bottomRight.X >> 1, Y: bottomRight.Y >> 1}
	shiftPlane(img.Cb, img.CStride, ref.U, ref.UStride, uvWidth, uvHeight,
		uvTopLeft, uvTopRight, uvBottomLeft, uvBottomRight)
	shiftPlane(img.Cr, img.CStride, ref.V, ref.VStride, uvWidth, uvHeight,
		uvTopLeft, uvTopRight, uvBottomLeft, uvBottomRight)
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

func decodeVP9KeyInterForTest(t *testing.T, key, inter []byte) *VP9Decoder {
	t.Helper()
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
	return d
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
		{func(o *VP9EncoderOptions) {
			o.Segmentation.Enabled = true
			o.Segmentation.SegmentID = VP9MaxSegments
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.Segmentation.Enabled = true
			o.Segmentation.SegmentID = 1
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.Segmentation.Enabled = true
			o.Segmentation.AltQEnabled[0] = true
			o.Segmentation.AltQ[0] = -256
		}, ErrInvalidQuantizer},
		{func(o *VP9EncoderOptions) {
			o.Segmentation.Enabled = true
			o.Segmentation.AltLFEnabled[0] = true
			o.Segmentation.AltLF[0] = 64
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.Segmentation.Enabled = true
			o.Segmentation.RefFrameEnabled[0] = true
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.Segmentation.Enabled = true
			o.Segmentation.RefFrameEnabled[0] = true
			o.Segmentation.RefFrame[0] = vp9dec.AltrefFrame + 1
		}, ErrInvalidConfig},
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

func TestVP9EncoderDefaultQuantizerUsesPinnedCQBaseQIndex(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	img := newVP9CheckerYCbCrForTest(64, 64, 32, 224, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	h, _ := parseVP9EncoderHeaderForTest(t, packet)
	if got := int(h.Quant.BaseQindex); got != vp9DefaultBaseQIndex {
		t.Fatalf("BaseQindex = %d, want pinned default %d",
			got, vp9DefaultBaseQIndex)
	}
}

func TestVP9EncoderExplicitQuantizerOverridesDefault(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width:     64,
		Height:    64,
		Quantizer: 1,
	})
	img := newVP9CheckerYCbCrForTest(64, 64, 32, 224, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	h, _ := parseVP9EncoderHeaderForTest(t, packet)
	if h.Quant.BaseQindex != 1 {
		t.Fatalf("BaseQindex = %d, want explicit qindex 1", h.Quant.BaseQindex)
	}
}

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
	key, err := e.Encode(newVP9CheckerYCbCrForTest(width, height, 48, 208, 128, 128))
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	keyHeader, _ := parseVP9EncoderHeaderForTest(t, key)
	assertVP9StaticSegmentationHeaderForTest(t, keyHeader.Seg, segID, altQ, altLF)

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
	keyHeader, _ := parseVP9EncoderHeaderForTest(t, key)
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
	key, err := e.Encode(newVP9YCbCrForTest(width, height, 72, 128, 128))
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	keyHeader, _ := parseVP9EncoderHeaderForTest(t, key)
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
	if _, err := e.Encode(newVP9YCbCrForTest(width, height, 72, 128, 128)); err != nil {
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
	h, _ := parseVP9EncoderHeaderForTest(t, packet)
	want := vp9EncoderLoopFilterLevel(128, true)
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
	keyHeader, _ := parseVP9EncoderHeaderForTest(t, keyPacket)

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
	h, _ := parseVP9EncoderHeaderForTest(t, packet)
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
	if interHeader.InterpFilter != vp9dec.InterpSwitchable {
		t.Fatalf("inter header InterpFilter = %d, want Switchable",
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
	for x := 0; x < 64; x++ {
		above := byte(224 - (x%32)*2)
		if x < 32 {
			above = byte(72 + x)
		}
		e.reconY[aboveRow+x0+x] = above
		e.reconY[internalAboveRow+x0+x] = byte(224 - (x%32)*2)
	}
	for y := 0; y < 64; y++ {
		left := byte(64 + (y%32)*2)
		e.reconY[(y0+y)*e.reconFrame.YStride+x0-1] = left
		e.reconY[(y0+y)*e.reconFrame.YStride+x0+31] = left
		for x := 0; x < 64; x++ {
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
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
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
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
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
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
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

func TestVP9EncoderInterPicksVert64x64ForHorizontalMixedMotion(t *testing.T) {
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
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
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

func TestVP9EncoderInterPartitionScoringUsesPriorChildContext(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
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
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
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
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
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
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
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

func TestVP9EncoderEncodeIntoWithFlagsInvisibleKeyFrameUpdatesReferences(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	src := newVP9YCbCrForTest(width, height, 91, 143, 37)
	hidden, err := e.EncodeWithFlags(src, EncodeInvisibleFrame)
	if err != nil {
		t.Fatalf("Encode hidden keyframe: %v", err)
	}
	h, _ := parseVP9EncoderHeaderForTest(t, hidden)
	if h.FrameType != common.KeyFrame || h.ShowFrame {
		t.Fatalf("hidden key header frame_type=%d show=%t, want key/show=false",
			h.FrameType, h.ShowFrame)
	}

	visible, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode visible inter after hidden keyframe: %v", err)
	}
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(hidden); err != nil {
		t.Fatalf("Decode hidden keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatal("NextFrame returned visible output after hidden keyframe")
	}
	if info, ok := d.LastFrameInfo(); !ok || !info.KeyFrame || info.ShowFrame {
		t.Fatalf("LastFrameInfo after hidden keyframe = %+v ok=%t, want hidden keyframe",
			info, ok)
	}
	if err := d.Decode(visible); err != nil {
		t.Fatalf("Decode visible inter: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after visible inter")
	}
	assertVP9FilledFrame(t, frame, width, height, 91, 143, 37)
}

func TestVP9EncoderEncodeIntoWithFlagsInvisibleAltRefRefresh(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := newVP9YCbCrForTest(width, height, 64, 128, 128)
	altSrc := newVP9YCbCrForTest(width, height, 188, 96, 224)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	hidden, err := e.EncodeWithFlags(altSrc,
		EncodeInvisibleFrame|EncodeForceAltRefFrame|EncodeNoUpdateLast|
			EncodeNoUpdateGolden|EncodeNoReferenceGolden|EncodeNoReferenceAltRef)
	if err != nil {
		t.Fatalf("Encode hidden altref refresh: %v", err)
	}
	visible, err := e.EncodeWithFlags(altSrc,
		EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoUpdateLast)
	if err != nil {
		t.Fatalf("Encode visible altref-only inter: %v", err)
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
	if err := d.Decode(hidden); err != nil {
		t.Fatalf("Decode hidden altref refresh: %v", err)
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatal("NextFrame returned visible output after hidden altref refresh")
	}
	if info, ok := d.LastFrameInfo(); !ok || info.ShowFrame ||
		info.RefreshFrameFlags != 1<<vp9AltRefSlot {
		t.Fatalf("LastFrameInfo after hidden altref = %+v ok=%t, want hidden ALTREF refresh",
			info, ok)
	}
	if err := d.Decode(visible); err != nil {
		t.Fatalf("Decode visible altref-only inter: %v", err)
	}
	if got := d.miGrid[0]; got.RefFrame[0] != vp9dec.AltrefFrame {
		t.Fatalf("visible inter ref = %v, want ALTREF", got.RefFrame)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after visible altref-only inter")
	}
	assertVP9FilledFrame(t, frame, width, height, 188, 96, 224)
}

func TestVP9EncoderEncodeShowExistingFrameInto(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	src := newVP9YCbCrForTest(width, height, 91, 143, 37)
	key, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}

	dst := make([]byte, 1)
	n, err := e.EncodeShowExistingFrameInto(dst, 5)
	if err != nil {
		t.Fatalf("EncodeShowExistingFrameInto: %v", err)
	}
	if n != 1 {
		t.Fatalf("EncodeShowExistingFrameInto wrote %d bytes, want 1", n)
	}
	packet := dst[:n]

	info, err := PeekVP9StreamInfo(packet)
	if err != nil {
		t.Fatalf("PeekVP9StreamInfo: %v", err)
	}
	if !info.ShowExistingFrame || info.ExistingFrameSlot != 5 ||
		!info.ShowFrame || info.KeyFrame || info.FirstPartitionSize != 0 {
		t.Fatalf("show-existing stream info = %+v, want visible slot 5 packet", info)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	if err := d.DecodeWithPTS(packet, 77); err != nil {
		t.Fatalf("Decode show-existing: %v", err)
	}
	last, ok := d.LastFrameInfo()
	if !ok || !last.ShowExistingFrame || last.ExistingFrameSlot != 5 ||
		!last.ShowFrame || last.PTS != 77 {
		t.Fatalf("LastFrameInfo after show-existing = %+v ok=%t, want slot 5 PTS 77",
			last, ok)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after show-existing")
	}
	assertVP9FilledFrame(t, frame, width, height, 91, 143, 37)
}

func TestVP9EncoderEncodeShowExistingFrameRejectsInvalidState(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	dst := make([]byte, 1)
	if _, err := e.EncodeShowExistingFrameInto(dst, 0); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("EncodeShowExistingFrameInto before refs error = %v, want ErrInvalidConfig", err)
	}
	if _, err := e.Encode(newVP9YCbCrForTest(64, 64, 128, 128, 128)); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	if _, err := e.EncodeShowExistingFrameInto(nil, 0); !errors.Is(err, ErrBufferTooSmall) {
		t.Fatalf("EncodeShowExistingFrameInto nil dst error = %v, want ErrBufferTooSmall", err)
	}
	if _, err := e.EncodeShowExistingFrameInto(dst, uint8(common.RefFrames)); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("EncodeShowExistingFrameInto bad slot error = %v, want ErrInvalidConfig", err)
	}
}

func TestVP9EncoderEncodeShowExistingFrameIntoSteadyStateAlloc(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	if _, err := e.Encode(newVP9YCbCrForTest(64, 64, 128, 128, 128)); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	dst := make([]byte, 1)

	var n int
	var err error
	allocs := testing.AllocsPerRun(100, func() {
		n, err = e.EncodeShowExistingFrameInto(dst, 5)
	})
	if err != nil {
		t.Fatalf("EncodeShowExistingFrameInto: %v", err)
	}
	if n != 1 {
		t.Fatalf("EncodeShowExistingFrameInto wrote %d bytes, want 1", n)
	}
	if allocs != 0 {
		t.Fatalf("EncodeShowExistingFrameInto steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9EncoderEncodeIntraOnlyFrameRefreshesLastAndShowExisting(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := newVP9YCbCrForTest(width, height, 16, 128, 128)
	src := newVP9YCbCrForTest(width, height, 83, 141, 209)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	hidden, err := e.EncodeIntraOnlyFrame(src, 0)
	if err != nil {
		t.Fatalf("EncodeIntraOnlyFrame: %v", err)
	}
	info, err := PeekVP9StreamInfo(hidden)
	if err != nil {
		t.Fatalf("PeekVP9StreamInfo hidden intra-only: %v", err)
	}
	if info.KeyFrame || !info.IntraOnly || info.ShowFrame ||
		info.RefreshFrameFlags != 1<<vp9LastRefSlot ||
		info.Width != width || info.Height != height {
		t.Fatalf("hidden intra-only info = %+v, want hidden LAST intra-only", info)
	}
	var br vp9dec.BitReader
	br.Init(hidden)
	hdr, err := vp9dec.ReadUncompressedHeader(&br, nil, nil)
	if err != nil {
		t.Fatalf("ReadUncompressedHeader hidden intra-only: %v", err)
	}
	if hdr.ResetFrameContext != 2 || !hdr.FrameParallelDecoding {
		t.Fatalf("hidden intra-only context flags = reset:%d parallel:%t, want reset 2 and frame-parallel",
			hdr.ResetFrameContext, hdr.FrameParallelDecoding)
	}
	show, err := e.EncodeShowExistingFrame(vp9LastRefSlot)
	if err != nil {
		t.Fatalf("EncodeShowExistingFrame LAST: %v", err)
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
	if err := d.DecodeWithPTS(hidden, 10); err != nil {
		t.Fatalf("Decode hidden intra-only: %v", err)
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatal("NextFrame returned visible output after hidden intra-only frame")
	}
	if last, ok := d.LastFrameInfo(); !ok || last.KeyFrame || last.ShowFrame ||
		last.RefreshFrameFlags != 1<<vp9LastRefSlot || last.PTS != 10 {
		t.Fatalf("LastFrameInfo hidden intra-only = %+v ok=%t, want hidden LAST refresh",
			last, ok)
	}
	if err := d.DecodeWithPTS(show, 11); err != nil {
		t.Fatalf("Decode show-existing LAST: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after show-existing LAST")
	}
	assertVP9FilledFrame(t, frame, width, height, 83, 141, 209)
}

func TestVP9EncoderEncodeIntraOnlyFrameRejectsConflictingFlags(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	src := newVP9YCbCrForTest(64, 64, 128, 128, 128)
	dst := make([]byte, 65536)
	if _, err := e.EncodeIntraOnlyFrameInto(src, dst, 0); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("EncodeIntraOnlyFrameInto before stream init error = %v, want ErrInvalidConfig", err)
	}
	if _, err := e.Encode(src); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	if _, err := e.EncodeIntraOnlyFrameInto(src, dst, EncodeForceKeyFrame); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("EncodeIntraOnlyFrameInto force-key error = %v, want ErrInvalidConfig", err)
	}
	if _, err := e.EncodeIntraOnlyFrameInto(src, dst,
		EncodeNoUpdateLast|EncodeNoUpdateGolden|EncodeNoUpdateAltRef); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("EncodeIntraOnlyFrameInto no-refresh error = %v, want ErrInvalidConfig", err)
	}
	if _, err := e.EncodeIntraOnlyFrameInto(src, nil, 0); !errors.Is(err, ErrBufferTooSmall) {
		t.Fatalf("EncodeIntraOnlyFrameInto nil dst error = %v, want ErrBufferTooSmall", err)
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
		vp9dec.MV{}, vp9dec.MV{}, false)
	newRate := vp9InterModeRateCost(&fc, 0, common.NewMv,
		vp9dec.MV{Col: 64}, vp9dec.MV{}, false)
	compoundNewRate := vp9InterModeRateCostN(&fc, 0, common.NewMv,
		[2]vp9dec.MV{{Col: 64}, {Col: -64}}, [2]vp9dec.MV{}, 2, false)
	if newRate <= zeroRate {
		t.Fatalf("NEWMV rate = %d, want greater than ZEROMV rate %d",
			newRate, zeroRate)
	}
	if compoundNewRate <= newRate {
		t.Fatalf("compound NEWMV rate = %d, want greater than single NEWMV rate %d",
			compoundNewRate, newRate)
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

func TestVP9BlockSADSSEMatchesScalar(t *testing.T) {
	const stride = 80
	src := make([]byte, stride*80)
	ref := make([]byte, stride*80)
	for i := range src {
		src[i] = byte((i*13 + i/5) & 0xff)
		ref[i] = byte((i*23 + 19) & 0xff)
	}

	gotSAD, gotSSE := vp9BlockSADSSE(src, stride, ref, stride,
		3, 5, 7, 11, 32, 16, ^uint64(0))
	var wantSAD, wantSSE uint64
	for y := 0; y < 16; y++ {
		srcRow := src[(5+y)*stride+3:]
		refRow := ref[(11+y)*stride+7:]
		for x := 0; x < 32; x++ {
			diff := int(srcRow[x]) - int(refRow[x])
			if diff < 0 {
				wantSAD += uint64(-diff)
			} else {
				wantSAD += uint64(diff)
			}
			wantSSE += uint64(diff * diff)
		}
	}
	if gotSAD != wantSAD || gotSSE != wantSSE {
		t.Fatalf("SAD/SSE = %d/%d, want %d/%d",
			gotSAD, gotSSE, wantSAD, wantSSE)
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
	if interHeader.InterRef.SignBias != [3]uint8{0, 0, 1} {
		t.Errorf("SignBias = %v, want [0 0 1]", interHeader.InterRef.SignBias)
	}
	if !interHeader.AllowHighPrecisionMv {
		t.Error("AllowHighPrecisionMv = false, want true")
	}
	if interHeader.InterpFilter != vp9dec.InterpSwitchable {
		t.Errorf("InterpFilter = %d, want Switchable", interHeader.InterpFilter)
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
		InterpFilter:         interHeader.InterpFilter,
		AllowHighPrecisionMv: interHeader.AllowHighPrecisionMv,
		CompoundRefAllowed:   true,
	})
	if cr.HasError() {
		t.Fatal("compressed header reader reported over-read")
	}
	if out.TxMode != common.TxModeSelect {
		t.Errorf("TxMode = %d, want TxModeSelect", out.TxMode)
	}
	if out.ReferenceMode != vp9dec.ReferenceModeSelect {
		t.Errorf("ReferenceMode = %d, want ReferenceModeSelect", out.ReferenceMode)
	}
	if compEnd >= len(inter) {
		t.Fatal("inter frame has no tile payload")
	}
}

func TestVP9EncoderInterTxScoringKeepsActiveResidual(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := newVP9YCbCrForTest(width, height, 96, 128, 128)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := newVP9CheckerYCbCrForTest(width, height, 48, 208, 128, 128)
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
	uncSize := interBR.BytesRead()
	compEnd := uncSize + int(interHeader.FirstPartitionSize)
	if compEnd > len(inter) {
		t.Fatalf("compressed header end %d past frame %d", compEnd, len(inter))
	}
	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	var cr bitstream.Reader
	if err := cr.Init(inter[uncSize:compEnd]); err != nil {
		t.Fatalf("compressed reader Init: %v", err)
	}
	out := vp9dec.ReadCompressedHeader(&cr, &fc, vp9dec.ReadCompressedHeaderArgs{
		Lossless:             false,
		IntraOnly:            false,
		KeyFrame:             false,
		InterpFilter:         interHeader.InterpFilter,
		AllowHighPrecisionMv: interHeader.AllowHighPrecisionMv,
		CompoundRefAllowed:   false,
	})
	if out.TxMode != common.TxModeSelect {
		t.Fatalf("TxMode = %d, want TxModeSelect", out.TxMode)
	}

	d := decodeVP9KeyInterForTest(t, key, inter)
	got := d.miGrid[0]
	if got.Skip != 0 {
		t.Fatal("top-left block skip=1, want active residual")
	}
	if got.TxSize != common.Tx32x32 {
		t.Fatalf("top-left TxSize = %d, want Tx32x32 from RD scoring", got.TxSize)
	}
}

func TestVP9EncoderInterTxScoringSelectsTx16ForLocalizedResidual(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	vp9dec.SetupBlockPlanes(&e.planes, 1, 1)
	e.ensureVP9EncoderModeBuffers(8, 8)
	e.prepareVP9EncoderOutputFrame(width, height)
	vp9dec.ResetFrameContext(&e.fc)

	img := newVP9YCbCrForTest(width, height, 128, 128, 128)
	for y := 0; y < 16; y++ {
		row := img.Y[y*img.YStride:]
		for x := 0; x < 16; x++ {
			if (x+y)&1 == 0 {
				row[x] = 16
			} else {
				row[x] = 240
			}
		}
	}
	var seg vp9dec.SegmentationParams
	var dq vp9dec.DequantTables
	vp9dec.SetupSegmentationDequant(&seg, vp9dec.SetupSegmentationDequantArgs{
		BaseQindex: e.vp9EncoderModeDecisionQIndex(),
		BitDepth:   vp9dec.Bits8,
	}, &dq)
	inter := &vp9InterEncodeState{img: img, dq: &dq}
	beforeY := append([]byte(nil), e.reconY[:e.reconFrame.YStride*height]...)
	beforeU := append([]byte(nil), e.reconU[:e.reconFrame.UStride*(height/2)]...)
	beforeV := append([]byte(nil), e.reconV[:e.reconFrame.VStride*(height/2)]...)
	got := e.pickVP9InterTxSize(inter, vp9dec.TileBounds{
		MiRowStart: 0, MiRowEnd: 8, MiColStart: 0, MiColEnd: 8,
	}, 8, 8, 0, 0, common.Block64x64, common.Tx32x32)
	if got != common.Tx16x16 {
		t.Fatalf("TxSize = %d, want Tx16x16 for localized residual", got)
	}
	if !bytes.Equal(e.reconY[:len(beforeY)], beforeY) ||
		!bytes.Equal(e.reconU[:len(beforeU)], beforeU) ||
		!bytes.Equal(e.reconV[:len(beforeV)], beforeV) {
		t.Fatal("tx-size scoring leaked candidate reconstruction into frame state")
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

func TestVP9EncoderKeyframeModeScoresWholeBlock(t *testing.T) {
	const width, height = 128, 128
	const x0, y0 = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	img := newVP9YCbCrForTest(width, height, 128, 128, 128)
	vp9dec.SetupBlockPlanes(&e.planes, 1, 1)
	e.prepareVP9EncoderOutputFrame(width, height)

	for x := 0; x < 64; x++ {
		e.reconY[(y0-1)*e.reconFrame.YStride+x0+x] = byte(48 + x*2)
	}
	for y := 0; y < 64; y++ {
		e.reconY[(y0+y)*e.reconFrame.YStride+x0-1] = byte(32 + y*3)
	}
	for y := 0; y < 64; y++ {
		row := img.Y[(y0+y)*img.YStride:]
		for x := 0; x < 64; x++ {
			if y < 32 && x < 32 {
				row[x0+x] = e.reconY[(y0-1)*e.reconFrame.YStride+x0+x]
			} else {
				row[x0+x] = e.reconY[(y0+y)*e.reconFrame.YStride+x0-1]
			}
		}
	}

	hdr := vp9dec.UncompressedHeader{Width: width, Height: height}
	key := &vp9KeyframeEncodeState{img: img, hdr: &hdr}
	mi := vp9dec.NeighborMi{SbType: common.Block64x64, TxSize: common.Tx32x32}
	tile := vp9dec.TileBounds{MiRowStart: 0, MiRowEnd: 16, MiColStart: 0, MiColEnd: 16}
	got := e.pickVP9KeyframeMode(key, tile, 16, 16, 8, 8, common.Block64x64, &mi)
	if got != common.HPred {
		t.Fatalf("mode = %d, want HPred from full-block score", got)
	}
}

func TestVP9EncoderKeyframePicksHorizontalModeForTx16(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 32, Height: 16})
	img := newVP9HorizontalBandsForTest(32, 16, 128, 128)
	vp9dec.SetupBlockPlanes(&e.planes, 1, 1)
	e.prepareVP9EncoderOutputFrame(32, 16)
	for y := range 16 {
		copy(e.reconY[y*e.reconFrame.YStride:y*e.reconFrame.YStride+16],
			img.Y[y*img.YStride:y*img.YStride+16])
	}

	hdr := vp9dec.UncompressedHeader{Width: 32, Height: 16}
	key := &vp9KeyframeEncodeState{img: img, hdr: &hdr}
	mi := vp9dec.NeighborMi{SbType: common.Block16x16, TxSize: common.Tx16x16}
	tile := vp9dec.TileBounds{MiRowStart: 0, MiRowEnd: 2, MiColStart: 0, MiColEnd: 4}
	got := e.pickVP9KeyframeMode(key, tile, 2, 4, 0, 2, common.Block16x16, &mi)
	if got != common.HPred {
		t.Errorf("mode = %d, want HPred", got)
	}
}

func TestVP9EncoderKeyframeTx16HybridResidue(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 32, Height: 16})
	img := image.NewYCbCr(image.Rect(0, 0, 32, 16), image.YCbCrSubsampleRatio420)
	for y := range 16 {
		row := img.Y[y*img.YStride:]
		base := byte(24 + y*9)
		for x := range 32 {
			v := int(base)
			if x >= 16 {
				v += (x - 15) * ((y % 3) + 1)
			}
			row[x] = byte(min(v, 255))
		}
	}
	for i := range img.Cb {
		img.Cb[i] = 128
	}
	for i := range img.Cr {
		img.Cr[i] = 128
	}

	vp9dec.SetupBlockPlanes(&e.planes, 1, 1)
	e.prepareVP9EncoderOutputFrame(32, 16)
	for y := range 16 {
		copy(e.reconY[y*e.reconFrame.YStride:y*e.reconFrame.YStride+16],
			img.Y[y*img.YStride:y*img.YStride+16])
	}

	hdr := vp9dec.UncompressedHeader{Width: 32, Height: 16}
	key := &vp9KeyframeEncodeState{img: img, hdr: &hdr}
	tile := vp9dec.TileBounds{MiRowStart: 0, MiRowEnd: 2, MiColStart: 0, MiColEnd: 4}
	var coeffs [vp9EncoderTxCoeffSlots]int16
	if !e.prepareVP9KeyframeTxResidue(key, &e.planes[0], 0, common.HPred,
		common.Tx16x16, tile, 2, 4, 0, 2, common.Block16x16, 0, 0,
		[2]int16{4, 4}, coeffs[:]) {
		t.Fatal("Tx16 HPred residue returned false, want nonzero hybrid-transform coefficients")
	}
	nonzeroAC := false
	for i, c := range coeffs[:vp9dec.MaxEobForTxSize(common.Tx16x16)] {
		if i != 0 && c != 0 {
			nonzeroAC = true
			break
		}
	}
	if !nonzeroAC {
		t.Fatal("Tx16 HPred residue produced no AC coefficients")
	}
}

func TestVP9EncoderKeyframeSignalsTx16HorizontalMode(t *testing.T) {
	const width, height = 128, 16
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	img := newVP9HorizontalBandsForTest(width, height, 128, 128)
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
	if len(d.miGrid) <= 8 {
		t.Fatalf("decoder MI grid len = %d, want second SB-row block", len(d.miGrid))
	}
	got := d.miGrid[8]
	if got.SbType != common.Block32x16 {
		t.Fatalf("second block size = %d, want Block32x16", got.SbType)
	}
	if got.TxSize != common.Tx16x16 {
		t.Fatalf("second block tx size = %d, want Tx16x16", got.TxSize)
	}
	if got.Mode != common.HPred {
		t.Fatalf("second block mode = %d, want HPred", got.Mode)
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

func TestVP9EncoderKeyframeUvModeScoresWholeBlock(t *testing.T) {
	const width, height = 128, 128
	const uvX, uvY = 32, 32
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	img := newVP9YCbCrForTest(width, height, 128, 128, 128)
	vp9dec.SetupBlockPlanes(&e.planes, 1, 1)
	e.prepareVP9EncoderOutputFrame(width, height)

	writePlane := func(src []byte, srcStride int, recon []byte, reconStride int,
		nearBase, leftBase, farBase int,
	) {
		aboveRow := (uvY - 1) * reconStride
		internalAboveRow := (uvY + 15) * reconStride
		for x := 0; x < 32; x++ {
			above := byte(farBase - (x%16)*2)
			if x < 16 {
				above = byte(nearBase + x)
			}
			recon[aboveRow+uvX+x] = above
			recon[internalAboveRow+uvX+x] = byte(farBase - (x%16)*2)
		}
		for y := 0; y < 32; y++ {
			left := byte(leftBase + (y%16)*2)
			recon[(uvY+y)*reconStride+uvX-1] = left
			recon[(uvY+y)*reconStride+uvX+15] = left
			for x := 0; x < 32; x++ {
				pixel := left
				if y < 16 && x < 16 {
					pixel = byte(nearBase + x)
				}
				src[(uvY+y)*srcStride+uvX+x] = pixel
			}
		}
	}
	writePlane(img.Cb, img.CStride, e.reconU, e.reconFrame.UStride, 72, 64, 224)
	writePlane(img.Cr, img.CStride, e.reconV, e.reconFrame.VStride, 120, 112, 32)

	hdr := vp9dec.UncompressedHeader{Width: width, Height: height}
	key := &vp9KeyframeEncodeState{img: img, hdr: &hdr}
	mi := vp9dec.NeighborMi{
		SbType: common.Block64x64,
		Mode:   common.DcPred,
		TxSize: common.Tx32x32,
	}
	tile := vp9dec.TileBounds{MiRowStart: 0, MiRowEnd: 16, MiColStart: 0, MiColEnd: 16}
	got := e.pickVP9KeyframeUvMode(key, tile, 16, 16, 8, 8, common.Block64x64, &mi)
	if got != common.HPred {
		t.Fatalf("UV mode = %d, want HPred from full-block score", got)
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

func TestVP9EncoderAllocatingWrapperGrowsForLargePacket(t *testing.T) {
	const width, height = 512, 512
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width:     width,
		Height:    height,
		Quantizer: 1,
	})
	img := newVP9CheckerYCbCrForTest(width, height, 16, 240, 96, 224)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode large keyframe: %v", err)
	}
	if len(packet) <= 65536 {
		t.Fatalf("large keyframe packet size = %d, want > 65536 to cover allocating growth", len(packet))
	}
	info, err := PeekVP9StreamInfo(packet)
	if err != nil {
		t.Fatalf("PeekVP9StreamInfo large keyframe: %v", err)
	}
	if !info.KeyFrame || info.Width != width || info.Height != height {
		t.Fatalf("large keyframe info = %+v, want %dx%d keyframe", info, width, height)
	}
}

func TestVP9EncoderBufferFullInterRetryPreservesFrameContext(t *testing.T) {
	const width, height = 64, 64
	keySrc := newVP9CheckerYCbCrForTest(width, height, 48, 208, 128, 128)
	interSrc := newVP9MotionYCbCrForTest(width, height)

	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if _, err := e.Encode(keySrc); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	if _, err := e.EncodeInto(interSrc, make([]byte, 512)); !errors.Is(err, vp9enc.ErrPackBufferFull) &&
		!errors.Is(err, vp9enc.ErrTileBufferFull) {
		t.Fatalf("short inter EncodeInto error = %v, want VP9 buffer-full error", err)
	}
	got, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("retry Encode inter: %v", err)
	}

	fresh, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if _, err := fresh.Encode(keySrc); err != nil {
		t.Fatalf("fresh Encode keyframe: %v", err)
	}
	want, err := fresh.Encode(interSrc)
	if err != nil {
		t.Fatalf("fresh Encode inter: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("inter retry changed packet after buffer-full failure: got %x want %x", got, want)
	}
}

func TestVP9EncoderEncodeIntoRejectsTinyBuffer(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	img := newVP9YCbCrForTest(64, 64, 128, 128, 128)
	if _, err := e.EncodeInto(img, make([]byte, vp9MinEncodeIntoBuffer-1)); !errors.Is(err, ErrBufferTooSmall) {
		t.Fatalf("tiny EncodeInto error = %v, want ErrBufferTooSmall", err)
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

func vp9VisibleImageEqual(a, b Image) bool {
	if a.Width != b.Width || a.Height != b.Height {
		return false
	}
	uvWidth := (a.Width + 1) >> 1
	uvHeight := (a.Height + 1) >> 1
	return planeEqual(a.Y, a.YStride, b.Y, b.YStride, a.Width, a.Height) &&
		planeEqual(a.U, a.UStride, b.U, b.UStride, uvWidth, uvHeight) &&
		planeEqual(a.V, a.VStride, b.V, b.VStride, uvWidth, uvHeight)
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

func assertVP9StaticSegmentationHeaderForTest(t *testing.T,
	seg vp9dec.SegmentationParams, segID int, altQ, altLF int16,
) {
	t.Helper()
	if !seg.Enabled || !seg.UpdateMap || !seg.UpdateData || !seg.AbsDelta {
		t.Fatalf("segmentation flags = enabled:%v updateMap:%v updateData:%v absDelta:%v, want all true",
			seg.Enabled, seg.UpdateMap, seg.UpdateData, seg.AbsDelta)
	}
	for i := range vp9dec.SegTreeProbs {
		if seg.TreeProbs[i] != vp9dec.MaxProb {
			t.Fatalf("TreeProbs[%d] = %d, want MaxProb", i, seg.TreeProbs[i])
		}
	}
	wantMask := uint32((1 << uint(vp9dec.SegLvlAltQ)) |
		(1 << uint(vp9dec.SegLvlAltLf)))
	if got := seg.FeatureMask[segID]; got != wantMask {
		t.Fatalf("FeatureMask[%d] = %#x, want AltQ|AltLF", segID, got)
	}
	if got := seg.FeatureData[segID][vp9dec.SegLvlAltQ]; got != altQ {
		t.Fatalf("AltQ[%d] = %d, want %d", segID, got, altQ)
	}
	if got := seg.FeatureData[segID][vp9dec.SegLvlAltLf]; got != altLF {
		t.Fatalf("AltLF[%d] = %d, want %d", segID, got, altLF)
	}
	for i := range vp9dec.MaxSegments {
		if i == segID {
			continue
		}
		if seg.FeatureMask[i] != 0 {
			t.Fatalf("FeatureMask[%d] = %#x, want 0", i, seg.FeatureMask[i])
		}
	}
}

func assertVP9StaticSkipSegmentationHeaderForTest(t *testing.T,
	seg vp9dec.SegmentationParams, segID int,
) {
	t.Helper()
	if !seg.Enabled || !seg.UpdateMap || !seg.UpdateData {
		t.Fatalf("segmentation flags = enabled:%v updateMap:%v updateData:%v, want all true",
			seg.Enabled, seg.UpdateMap, seg.UpdateData)
	}
	wantMask := uint32(1 << uint(vp9dec.SegLvlSkip))
	if got := seg.FeatureMask[segID]; got != wantMask {
		t.Fatalf("FeatureMask[%d] = %#x, want Skip", segID, got)
	}
	for i := range vp9dec.MaxSegments {
		if i == segID {
			continue
		}
		if seg.FeatureMask[i] != 0 {
			t.Fatalf("FeatureMask[%d] = %#x, want 0", i, seg.FeatureMask[i])
		}
	}
}

func assertVP9StaticRefFrameSegmentationHeaderForTest(t *testing.T,
	seg vp9dec.SegmentationParams, segID int, refFrame int8,
) {
	t.Helper()
	if !seg.Enabled || !seg.UpdateMap || !seg.UpdateData {
		t.Fatalf("segmentation flags = enabled:%v updateMap:%v updateData:%v, want all true",
			seg.Enabled, seg.UpdateMap, seg.UpdateData)
	}
	wantMask := uint32(1 << uint(vp9dec.SegLvlRefFrame))
	if got := seg.FeatureMask[segID]; got != wantMask {
		t.Fatalf("FeatureMask[%d] = %#x, want RefFrame", segID, got)
	}
	if got := int8(seg.FeatureData[segID][vp9dec.SegLvlRefFrame]); got != refFrame {
		t.Fatalf("RefFrame[%d] = %d, want %d", segID, got, refFrame)
	}
	for i := range vp9dec.MaxSegments {
		if i == segID {
			continue
		}
		if seg.FeatureMask[i] != 0 {
			t.Fatalf("FeatureMask[%d] = %#x, want 0", i, seg.FeatureMask[i])
		}
	}
}

func assertVP9DecoderSegmentIDForTest(t *testing.T, d *VP9Decoder, segID uint8) {
	t.Helper()
	if len(d.miGrid) == 0 {
		t.Fatal("decoder MI grid is empty")
	}
	for i, mi := range d.miGrid {
		if mi.SegmentID != segID || mi.SegIDPredicted != segID {
			t.Fatalf("miGrid[%d] segment = (%d,%d), want (%d,%d)",
				i, mi.SegmentID, mi.SegIDPredicted, segID, segID)
		}
	}
	if len(d.lastSegMap) == 0 {
		t.Fatal("decoder last segment map is empty")
	}
	for i, got := range d.lastSegMap {
		if got != segID {
			t.Fatalf("lastSegMap[%d] = %d, want %d", i, got, segID)
		}
	}
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
