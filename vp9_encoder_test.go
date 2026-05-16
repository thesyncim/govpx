package govpx

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"reflect"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

const (
	vp9EncoderKeyframeAllocRuns = 10
	vp9EncoderInterAllocRuns    = 3
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
		{func(o *VP9EncoderOptions) { o.Width = maxVP9Dimension + 1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.Height = maxVP9Dimension + 1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.Threads = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.Log2TileRows = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.Log2TileRows = 3 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.Height = 64
			o.Log2TileRows = 1
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.Deadline = Deadline(-1) }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.CpuUsed = -10 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.CpuUsed = 10 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.TargetBitrateKbps = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.Quantizer = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.Quantizer = 256 }, ErrInvalidQuantizer},
		{func(o *VP9EncoderOptions) { o.MinQuantizer = -1 }, ErrInvalidQuantizer},
		{func(o *VP9EncoderOptions) { o.MaxQuantizer = 64 }, ErrInvalidQuantizer},
		{func(o *VP9EncoderOptions) { o.CQLevel = 64 }, ErrInvalidQuantizer},
		{func(o *VP9EncoderOptions) {
			o.MinQuantizer = 40
			o.MaxQuantizer = 20
		}, ErrInvalidQuantizer},
		{func(o *VP9EncoderOptions) {
			o.MinQuantizer = 20
			o.MaxQuantizer = 40
			o.CQLevel = 10
		}, ErrInvalidQuantizer},
		{func(o *VP9EncoderOptions) {
			o.Quantizer = 64
			o.CQLevel = 32
		}, ErrInvalidQuantizer},
		{func(o *VP9EncoderOptions) {
			o.TemporalScalability = TemporalScalabilityConfig{
				Enabled: true,
				Mode:    TemporalLayeringTwoLayers,
			}
		}, ErrInvalidBitrate},
		{func(o *VP9EncoderOptions) {
			o.TargetBitrateKbps = 300
			o.TemporalScalability = TemporalScalabilityConfig{
				Enabled: true,
				Mode:    TemporalLayeringMode(99),
			}
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.SpatialScalability = VP9SpatialScalabilityConfig{
				Enabled: true,
			}
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.SpatialScalability = VP9SpatialScalabilityConfig{
				Enabled:    true,
				LayerCount: VP9MaxSpatialLayers + 1,
			}
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.SpatialScalability = VP9SpatialScalabilityConfig{
				Enabled:    true,
				LayerCount: 2,
				LayerID:    2,
			}
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.SpatialScalability = VP9SpatialScalabilityConfig{
				Enabled:              true,
				LayerCount:           2,
				InterLayerDependency: true,
			}
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.SpatialScalability = VP9SpatialScalabilityConfig{
				Enabled:    true,
				LayerCount: 2,
				LayerID:    1,
				Width:      [VP9RTPMaxSpatialLayers]uint16{32, 64},
				Height:     [VP9RTPMaxSpatialLayers]uint16{32, 64},
			}
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.SpatialScalability = VP9SpatialScalabilityConfig{
				Enabled:           true,
				LayerCount:        2,
				LayerID:           1,
				ResolutionPresent: true,
				Width:             [VP9RTPMaxSpatialLayers]uint16{32, 32},
				Height:            [VP9RTPMaxSpatialLayers]uint16{32, 32},
			}
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.Lossless = true
			o.Quantizer = 1
		}, ErrInvalidQuantizer},
		{func(o *VP9EncoderOptions) {
			o.Lossless = true
			o.Segmentation.Enabled = true
			o.Segmentation.AltQEnabled[0] = true
			o.Segmentation.AltQ[0] = 1
		}, ErrInvalidQuantizer},
		{func(o *VP9EncoderOptions) {
			o.Lossless = true
			o.Segmentation.Enabled = true
			o.Segmentation.AltLFEnabled[0] = true
			o.Segmentation.AltLF[0] = 1
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.MaxKeyframeInterval = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.LookaheadFrames = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.LookaheadFrames = vp9MaxLookaheadFrames + 1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.ARNRMaxFrames = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.ARNRMaxFrames = maxARNRFrames + 1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.ARNRStrength = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.ARNRStrength = 7 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.ARNRType = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.ARNRType = 4 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.ScreenContentMode = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.ScreenContentMode = 3 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.NoiseSensitivity = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.NoiseSensitivity = 7 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.Sharpness = 8 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.AutoAltRef = true }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.AutoAltRef = true
			o.LookaheadFrames = 1
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.AutoAltRef = true
			o.LookaheadFrames = 2
			o.ErrorResilient = true
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.AQMode = VP9AQMode(99) }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.AQMode = VP9AQCyclicRefresh }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.AQMode = VP9AQCyclicRefresh
			o.RateControlModeSet = true
			o.RateControlMode = RateControlVBR
			o.TargetBitrateKbps = 300
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.AQMode = VP9AQCyclicRefresh
			o.RateControlModeSet = true
			o.RateControlMode = RateControlCBR
			o.TargetBitrateKbps = 300
			o.Lossless = true
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.AQMode = VP9AQCyclicRefresh
			o.RateControlModeSet = true
			o.RateControlMode = RateControlCBR
			o.TargetBitrateKbps = 300
			o.Segmentation.Enabled = true
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.RateControlModeSet = true
			o.RateControlMode = RateControlMode(99)
			o.TargetBitrateKbps = 300
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.RateControlModeSet = true
			o.RateControlMode = RateControlVBR
		}, ErrInvalidBitrate},
		{func(o *VP9EncoderOptions) {
			o.RateControlModeSet = true
			o.RateControlMode = RateControlVBR
			o.TargetBitrateKbps = 300
			o.DropFrameAllowed = true
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.TwoPassStats = finalizedVP9TwoPassTestStats(100, 200)
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.RateControlModeSet = true
			o.RateControlMode = RateControlCBR
			o.TargetBitrateKbps = 300
			o.TwoPassStats = finalizedVP9TwoPassTestStats(100, 200)
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.RateControlModeSet = true
			o.RateControlMode = RateControlQ
			o.TargetBitrateKbps = 300
			o.TwoPassStats = finalizedVP9TwoPassTestStats(100, 200)
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.TwoPassVBRBiasPct = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.TwoPassMinPct = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.TwoPassMaxPct = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.LookaheadFrames = 2
			o.RateControlModeSet = true
			o.RateControlMode = RateControlCBR
			o.TargetBitrateKbps = 300
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.AutoAltRef = true
			o.LookaheadFrames = 2
			o.RateControlModeSet = true
			o.RateControlMode = RateControlCBR
			o.TargetBitrateKbps = 300
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.LookaheadFrames = 2
			o.TargetBitrateKbps = 300
			o.TemporalScalability = TemporalScalabilityConfig{
				Enabled: true,
				Mode:    TemporalLayeringTwoLayers,
			}
		}, ErrInvalidConfig},
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
			o.Segmentation.RefFrame[0] = VP9RefFrameIntra - 1
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
	if e.opts.Deadline != DeadlineRealtime || e.opts.CpuUsed != vp9DefaultCPUUsed {
		t.Fatalf("default VP9 speed = deadline:%d cpu:%d, want realtime/%d",
			e.opts.Deadline, e.opts.CpuUsed, vp9DefaultCPUUsed)
	}
}

func TestVP9EncoderSpeedControlsUpdateSpeedFeatures(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if got := e.vp9CoeffProbAppxStep(); got != 4 {
		t.Fatalf("default coeff step = %d, want 4", got)
	}
	if !e.vp9SkipTx16PlusCoefUpdates(false) {
		t.Fatal("default speed should skip tx16+ coef updates")
	}
	if !e.vp9RealtimeVariancePartitionEnabled() {
		t.Fatal("default speed should enable realtime variance partition")
	}

	if err := e.SetDeadline(DeadlineGoodQuality); err != nil {
		t.Fatalf("SetDeadline(good): %v", err)
	}
	if got := e.vp9CoeffProbAppxStep(); got != 1 {
		t.Fatalf("good coeff step = %d, want 1", got)
	}
	if e.vp9SkipTx16PlusCoefUpdates(false) {
		t.Fatal("good deadline should not use realtime tx16+ skip")
	}
	if e.vp9RealtimeVariancePartitionEnabled() {
		t.Fatal("good deadline should not use realtime variance partition")
	}

	if err := e.SetDeadline(DeadlineRealtime); err != nil {
		t.Fatalf("SetDeadline(rt): %v", err)
	}
	if err := e.SetCPUUsed(5); err != nil {
		t.Fatalf("SetCPUUsed(5): %v", err)
	}
	if got := e.vp9CoeffProbAppxStep(); got != 4 {
		t.Fatalf("rt cpu5 coeff step = %d, want 4", got)
	}
	if !e.vp9SkipTx16PlusCoefUpdates(false) {
		t.Fatal("rt cpu5 should skip tx16+ coef updates")
	}
	if e.vp9RealtimeVariancePartitionEnabled() {
		t.Fatal("rt cpu5 should not enable speed8 variance partition")
	}

	if err := e.SetCPUUsed(4); err != nil {
		t.Fatalf("SetCPUUsed(4): %v", err)
	}
	if got := e.vp9CoeffProbAppxStep(); got != 1 {
		t.Fatalf("rt cpu4 coeff step = %d, want 1", got)
	}
	if !e.vp9SkipTx16PlusCoefUpdates(false) {
		t.Fatal("rt cpu4 should still skip tx16+ coef updates")
	}

	if err := e.SetCPUUsed(0); err != nil {
		t.Fatalf("SetCPUUsed(0): %v", err)
	}
	if e.vp9SkipTx16PlusCoefUpdates(false) {
		t.Fatal("rt cpu0 should not skip tx16+ coef updates")
	}
	if err := e.SetCPUUsed(-9); err != nil {
		t.Fatalf("SetCPUUsed(-9): %v", err)
	}
	if !e.vp9RealtimeVariancePartitionEnabled() {
		t.Fatal("rt cpu-9 should use abs(cpu-used) speed")
	}

	beforeDeadline, beforeCPU := e.opts.Deadline, e.opts.CpuUsed
	if err := e.SetDeadline(Deadline(-1)); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetDeadline invalid err = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetCPUUsed(10); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetCPUUsed invalid err = %v, want ErrInvalidConfig", err)
	}
	if e.opts.Deadline != beforeDeadline || e.opts.CpuUsed != beforeCPU {
		t.Fatal("invalid VP9 speed controls mutated encoder")
	}
}

func TestVP9EncoderSetNoiseSensitivity(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	for _, tc := range []struct {
		sensitivity int
		level       int8
	}{
		{sensitivity: 1, level: vp9DenoiserLow},
		{sensitivity: 2, level: vp9DenoiserMedium},
		{sensitivity: 3, level: vp9DenoiserHigh},
		{sensitivity: 4, level: vp9DenoiserHigh},
		{sensitivity: 5, level: vp9DenoiserHigh},
		{sensitivity: 6, level: vp9DenoiserHigh},
	} {
		if err := e.SetNoiseSensitivity(tc.sensitivity); err != nil {
			t.Fatalf("SetNoiseSensitivity(%d): %v", tc.sensitivity, err)
		}
		if e.opts.NoiseSensitivity != int8(tc.sensitivity) {
			t.Fatalf("NoiseSensitivity = %d, want %d",
				e.opts.NoiseSensitivity, tc.sensitivity)
		}
		if e.denoiser.sensitivity != int8(tc.sensitivity) ||
			e.denoiser.level != tc.level {
			t.Fatalf("denoiser sensitivity/level = %d/%d, want %d/%d",
				e.denoiser.sensitivity, e.denoiser.level,
				tc.sensitivity, tc.level)
		}
	}
	if err := e.SetNoiseSensitivity(7); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetNoiseSensitivity invalid err = %v, want ErrInvalidConfig", err)
	}
	if e.opts.NoiseSensitivity != 6 {
		t.Fatal("invalid SetNoiseSensitivity mutated encoder")
	}
	if err := e.SetNoiseSensitivity(0); err != nil {
		t.Fatalf("SetNoiseSensitivity(0): %v", err)
	}
	if e.opts.NoiseSensitivity != 0 || e.denoiser.sensitivity != 0 {
		t.Fatalf("disabled noise sensitivity = opts:%d state:%d, want 0/0",
			e.opts.NoiseSensitivity, e.denoiser.sensitivity)
	}
}

func TestVP9EncoderSetScreenContentMode(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	for _, mode := range []int{0, 1, 2} {
		if err := e.SetScreenContentMode(mode); err != nil {
			t.Fatalf("SetScreenContentMode(%d): %v", mode, err)
		}
		if e.opts.ScreenContentMode != int8(mode) {
			t.Fatalf("ScreenContentMode = %d, want %d",
				e.opts.ScreenContentMode, mode)
		}
	}
	if err := e.SetScreenContentMode(3); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetScreenContentMode invalid err = %v, want ErrInvalidConfig", err)
	}
	if e.opts.ScreenContentMode != 2 {
		t.Fatal("invalid SetScreenContentMode mutated encoder")
	}

	if got := vp9NoReferenceIntraModeCount(common.Block32x32, 0); got != 1 {
		t.Fatalf("default 32x32 no-ref intra mode count = %d, want 1", got)
	}
	if got := vp9NoReferenceIntraModeCount(common.Block32x32, 1); got != 3 {
		t.Fatalf("screen 32x32 no-ref intra mode count = %d, want 3", got)
	}
	if got := vp9NoReferenceIntraModeCount(common.Block32x32, 2); got != 1 {
		t.Fatalf("film 32x32 no-ref intra mode count = %d, want 1", got)
	}
	if got := vp9NoReferenceIntraModeCount(common.Block16x16, 0); got != 3 {
		t.Fatalf("default 16x16 no-ref intra mode count = %d, want 3", got)
	}
}

func TestVP9EncoderSetKeyFrameInterval(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetKeyFrameInterval(2); err != nil {
		t.Fatalf("SetKeyFrameInterval(2): %v", err)
	}
	if e.opts.MaxKeyframeInterval != 2 {
		t.Fatalf("MaxKeyframeInterval = %d, want 2", e.opts.MaxKeyframeInterval)
	}
	dst := make([]byte, 65536)
	results := make([]VP9EncodeResult, 3)
	for frame := range results {
		src := newVP9YCbCrForTest(width, height, uint8(96+frame), 128, 128)
		results[frame], err = e.EncodeIntoWithResult(src, dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult[%d]: %v", frame, err)
		}
	}
	if !results[0].KeyFrame || results[1].KeyFrame || !results[2].KeyFrame {
		t.Fatalf("keyframe cadence = [%t %t %t], want [true false true]",
			results[0].KeyFrame, results[1].KeyFrame, results[2].KeyFrame)
	}
	before := e.opts.MaxKeyframeInterval
	if err := e.SetKeyFrameInterval(-1); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetKeyFrameInterval(-1) err = %v, want ErrInvalidConfig", err)
	}
	if e.opts.MaxKeyframeInterval != before {
		t.Fatal("invalid SetKeyFrameInterval mutated encoder")
	}
	if err := e.SetKeyFrameInterval(0); err != nil {
		t.Fatalf("SetKeyFrameInterval(0): %v", err)
	}
	if e.opts.MaxKeyframeInterval != 0 {
		t.Fatalf("MaxKeyframeInterval reset = %d, want 0", e.opts.MaxKeyframeInterval)
	}
}

func TestVP9EncoderSetARNR(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:           width,
		Height:          height,
		LookaheadFrames: 4,
		AutoAltRef:      true,
		ARNRMaxFrames:   1,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if len(e.vp9ARNRScratch.Y) != 0 {
		t.Fatal("ARNR scratch allocated before ARNR is enabled")
	}
	if err := e.SetARNR(5, 6, 1); err != nil {
		t.Fatalf("SetARNR: %v", err)
	}
	if e.opts.ARNRMaxFrames != 5 || e.opts.ARNRStrength != 6 ||
		e.opts.ARNRType != 1 {
		t.Fatalf("ARNR opts = max:%d strength:%d type:%d, want 5/6/1",
			e.opts.ARNRMaxFrames, e.opts.ARNRStrength, e.opts.ARNRType)
	}
	if len(e.vp9ARNRScratch.Y) == 0 {
		t.Fatal("SetARNR did not allocate ARNR scratch for active auto-alt-ref")
	}
	for frame := 0; frame < 4; frame++ {
		src := newVP9YCbCrForTest(width, height, uint8(96+frame*12), 128, 128)
		if err := e.pushVP9Lookahead(src, 0); err != nil {
			t.Fatalf("pushVP9Lookahead[%d]: %v", frame, err)
		}
	}
	future, ok := e.newestVP9LookaheadEntry()
	if !ok {
		t.Fatal("newestVP9LookaheadEntry returned !ok")
	}
	if !e.applyVP9ARNRFilter(future) {
		t.Fatal("runtime SetARNR filter returned false")
	}
	if bytes.Equal(e.vp9ARNRScratch.Y, future.img.Y) {
		t.Fatal("runtime SetARNR left ARNR scratch equal to source")
	}
	before := e.opts
	for _, tc := range []struct {
		name string
		max  int
		str  int
		typ  int
	}{
		{name: "max low", max: -1, str: 3, typ: 3},
		{name: "max high", max: maxARNRFrames + 1, str: 3, typ: 3},
		{name: "strength low", max: 5, str: -1, typ: 3},
		{name: "strength high", max: 5, str: 7, typ: 3},
		{name: "type low", max: 5, str: 3, typ: -1},
		{name: "type high", max: 5, str: 3, typ: 4},
	} {
		if err := e.SetARNR(tc.max, tc.str, tc.typ); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("%s SetARNR err = %v, want ErrInvalidConfig", tc.name, err)
		}
		if e.opts.ARNRMaxFrames != before.ARNRMaxFrames ||
			e.opts.ARNRStrength != before.ARNRStrength ||
			e.opts.ARNRType != before.ARNRType {
			t.Fatalf("%s invalid SetARNR mutated opts", tc.name)
		}
	}
}

func TestVP9EncoderNoiseSensitivityDenoisesInterLuma(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:            width,
		Height:           height,
		NoiseSensitivity: 3,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	keySrc := newVP9YCbCrForTest(width, height, 100, 96, 160)
	interSrc := newVP9YCbCrForTest(width, height, 102, 98, 158)
	dst := make([]byte, 65536)

	if _, err := e.EncodeInto(keySrc, dst); err != nil {
		t.Fatalf("key EncodeInto: %v", err)
	}
	if _, err := e.EncodeInto(interSrc, dst); err != nil {
		t.Fatalf("inter EncodeInto: %v", err)
	}
	if !e.denoiser.active() {
		t.Fatal("denoiser inactive after noise-sensitive encode")
	}
	if got := interSrc.Y[0]; got != 102 {
		t.Fatalf("caller source was mutated: Y[0]=%d, want 102", got)
	}
	if got := e.denoiser.runningAvg[vp9DenoiserAvgLast].Y[0]; got != 100 {
		t.Fatalf("denoised LAST running average Y[0] = %d, want 100", got)
	}
	if got := e.denoiser.source.Y[0]; got != 100 {
		t.Fatalf("denoised encoder source Y[0] = %d, want 100", got)
	}
}

func TestVP9EncoderNoiseSensitivityDenoisesInterChroma(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:            width,
		Height:           height,
		NoiseSensitivity: 3,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	keySrc := newVP9YCbCrForTest(width, height, 100, 96, 160)
	interSrc := newVP9YCbCrForTest(width, height, 102, 98, 158)
	dst := make([]byte, 65536)

	if _, err := e.EncodeInto(keySrc, dst); err != nil {
		t.Fatalf("key EncodeInto: %v", err)
	}
	if _, err := e.EncodeInto(interSrc, dst); err != nil {
		t.Fatalf("inter EncodeInto: %v", err)
	}
	if !e.denoiser.active() {
		t.Fatal("denoiser inactive after noise-sensitive encode")
	}
	if got := interSrc.Cb[0]; got != 98 {
		t.Fatalf("caller source was mutated: Cb[0]=%d, want 98", got)
	}
	if got := interSrc.Cr[0]; got != 158 {
		t.Fatalf("caller source was mutated: Cr[0]=%d, want 158", got)
	}
	if got := e.denoiser.runningAvg[vp9DenoiserAvgLast].Cb[0]; got != 96 {
		t.Fatalf("denoised LAST running average Cb[0] = %d, want 96", got)
	}
	if got := e.denoiser.runningAvg[vp9DenoiserAvgLast].Cr[0]; got != 160 {
		t.Fatalf("denoised LAST running average Cr[0] = %d, want 160", got)
	}
	if got := e.denoiser.source.Cb[0]; got != 96 {
		t.Fatalf("denoised encoder source Cb[0] = %d, want 96", got)
	}
	if got := e.denoiser.source.Cr[0]; got != 160 {
		t.Fatalf("denoised encoder source Cr[0] = %d, want 160", got)
	}
}

func TestVP9EncoderExplicitRateControlModesEncode(t *testing.T) {
	const width, height = 64, 64
	const targetKbps = 300
	const wantBitsPerFrame = targetKbps * 1000 / 30
	cases := []struct {
		name    string
		mode    RateControlMode
		cqLevel int
	}{
		{name: "vbr", mode: RateControlVBR},
		{name: "cq", mode: RateControlCQ, cqLevel: 20},
		{name: "q", mode: RateControlQ, cqLevel: 20},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, err := NewVP9Encoder(VP9EncoderOptions{
				Width:              width,
				Height:             height,
				FPS:                30,
				TargetBitrateKbps:  targetKbps,
				RateControlModeSet: true,
				RateControlMode:    tc.mode,
				CQLevel:            tc.cqLevel,
			})
			if err != nil {
				t.Fatalf("NewVP9Encoder: %v", err)
			}
			if !e.rc.enabled || e.rc.mode != tc.mode {
				t.Fatalf("rate control state = enabled:%t mode:%d, want true/%d",
					e.rc.enabled, e.rc.mode, tc.mode)
			}
			if e.rc.dropFrameAllowed || e.rc.dropFramesWaterMark != 0 {
				t.Fatalf("non-CBR drop state = allowed:%t watermark:%d, want disabled",
					e.rc.dropFrameAllowed, e.rc.dropFramesWaterMark)
			}

			dst := make([]byte, 65536)
			dec, err := NewVP9Decoder(VP9DecoderOptions{})
			if err != nil {
				t.Fatalf("NewVP9Decoder: %v", err)
			}
			for frame := 0; frame < 2; frame++ {
				src := newVP9YCbCrForTest(width, height,
					uint8(96+frame*20), 128, 128)
				result, err := e.EncodeIntoWithResult(src, dst)
				if err != nil {
					t.Fatalf("EncodeIntoWithResult frame %d: %v", frame, err)
				}
				if result.Dropped || len(result.Data) == 0 {
					t.Fatalf("frame %d result = dropped:%t bytes:%d, want packet",
						frame, result.Dropped, len(result.Data))
				}
				wantFrameTargetBits := wantBitsPerFrame
				if frame == 0 {
					wantFrameTargetBits = e.rc.onePassVBRKeyFrameTargetBits()
				} else {
					wantFrameTargetBits = e.rc.onePassVBRInterFrameTargetBits(
						1 << vp9LastRefSlot)
				}
				if result.TargetBitrateKbps != targetKbps ||
					result.FrameTargetBits != wantFrameTargetBits {
					t.Fatalf("frame %d rate = kbps:%d target:%d, want %d/%d",
						frame, result.TargetBitrateKbps, result.FrameTargetBits,
						targetKbps, wantFrameTargetBits)
				}
				if frame == 0 && !result.KeyFrame {
					t.Fatal("first explicit rate-control packet is not a keyframe")
				}
				if frame == 1 && result.KeyFrame {
					t.Fatal("second explicit rate-control packet unexpectedly keyframed")
				}
				if err := dec.Decode(result.Data); err != nil {
					t.Fatalf("Decode frame %d: %v", frame, err)
				}
				if _, ok := dec.NextFrame(); !ok {
					t.Fatalf("NextFrame frame %d returned !ok", frame)
				}
			}
		})
	}
}

func TestVP9EncoderExplicitVBRUsesOnePassRateQuantizer(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		TargetBitrateKbps:   700,
		RateControlModeSet:  true,
		RateControlMode:     RateControlVBR,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		MaxKeyframeInterval: 128,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if e.rc.avgFrameQIndexKey != 120 || e.rc.avgFrameQIndexInter != 120 {
		t.Fatalf("initial VBR average q = key:%d inter:%d, want midpoint 120/120",
			e.rc.avgFrameQIndexKey, e.rc.avgFrameQIndexInter)
	}
	dst := make([]byte, 65536)
	key, err := e.EncodeIntoWithResult(newVP9YCbCrForTest(width, height,
		96, 128, 128), dst)
	if err != nil {
		t.Fatalf("Encode key: %v", err)
	}
	if key.InternalQuantizer >= vp9DefaultBaseQIndex {
		t.Fatalf("VBR key qindex = %d, want below public-Q key qindex %d",
			key.InternalQuantizer, vp9DefaultBaseQIndex)
	}
	if key.FrameTargetBits != e.rc.onePassVBRKeyFrameTargetBits() {
		t.Fatalf("VBR key target = %d, want one-pass target %d",
			key.FrameTargetBits, e.rc.onePassVBRKeyFrameTargetBits())
	}
	inter, err := e.EncodeIntoWithResult(newVP9YCbCrForTest(width, height,
		116, 128, 128), dst)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	if inter.InternalQuantizer == vp9DefaultInterBaseQIndex {
		t.Fatalf("VBR inter qindex = %d, still public-Q inter default",
			inter.InternalQuantizer)
	}
}

func TestVP9RateControlVBRGoldenUsesGFARFCorrectionFactor(t *testing.T) {
	var rc vp9RateControlState
	if err := rc.applyOptions(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		TargetBitrateKbps:  700,
		RateControlModeSet: true,
		RateControlMode:    RateControlVBR,
		MinQuantizer:       4,
		MaxQuantizer:       56,
	}, vp9TimingStateFromOptions(VP9EncoderOptions{FPS: 30})); err != nil {
		t.Fatalf("applyOptions: %v", err)
	}
	const qindex = 96
	macroblocks := vp9MacroblockCount((64+7)>>3, (64+7)>>3)
	actualBits := vp9EstimatedBitsAtQ(false, qindex, macroblocks, 1) * 2
	rc.updateRateCorrectionFactor(actualBits, qindex, false,
		1<<vp9GoldenRefSlot, macroblocks)
	if rc.rateCorrectionFactors[vp9RateFactorGFARFStd] <= 1 {
		t.Fatalf("GF/ARF correction factor = %.3f, want updated above 1",
			rc.rateCorrectionFactors[vp9RateFactorGFARFStd])
	}
	if rc.rateCorrectionFactors[vp9RateFactorInterNormal] != 1 {
		t.Fatalf("INTER_NORMAL correction factor = %.3f, want unchanged",
			rc.rateCorrectionFactors[vp9RateFactorInterNormal])
	}
}

func TestVP9RateControlBoostedRefreshUpdatesLastBoostedQIndex(t *testing.T) {
	rc := vp9RateControlState{lastBoostedQIndex: 40}
	rc.updateQHistory(80, false, 1<<vp9GoldenRefSlot, true)
	if got := rc.lastBoostedQIndex; got != 80 {
		t.Fatalf("last boosted q after golden refresh = %d, want 80", got)
	}
}

func TestVP9EncoderOnePassVBRGoldenRefreshCadence(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		TargetBitrateKbps:   700,
		RateControlModeSet:  true,
		RateControlMode:     RateControlVBR,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		MaxKeyframeInterval: 128,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dst := make([]byte, 65536)
	for frame := 0; frame <= 10; frame++ {
		result, err := e.EncodeIntoWithResult(newVP9YCbCrForTest(width,
			height, uint8(96+frame*3), 128, 128), dst)
		if err != nil {
			t.Fatalf("Encode frame %d: %v", frame, err)
		}
		switch frame {
		case 0:
			if result.RefreshFrameFlags != 0xff {
				t.Fatalf("key refresh flags = %#x, want all refs",
					result.RefreshFrameFlags)
			}
		case 10:
			want := uint8(1<<vp9LastRefSlot | 1<<vp9GoldenRefSlot)
			if result.RefreshFrameFlags != want {
				t.Fatalf("frame 10 refresh flags = %#x, want one-pass GF %#x",
					result.RefreshFrameFlags, want)
			}
			if result.FrameTargetBits <= e.rc.bitsPerFrame {
				t.Fatalf("frame 10 target = %d, want boosted above %d",
					result.FrameTargetBits, e.rc.bitsPerFrame)
			}
		default:
			if result.RefreshFrameFlags != 1<<vp9LastRefSlot {
				t.Fatalf("frame %d refresh flags = %#x, want LAST only",
					frame, result.RefreshFrameFlags)
			}
		}
	}
}

func TestVP9SetRateControlCBRToVBRSeedsGoldenCadence(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   700,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dst := make([]byte, 65536)
	for frame := 0; frame < 3; frame++ {
		if _, err := e.EncodeIntoWithResult(newVP9YCbCrForTest(width,
			height, uint8(96+frame*3), 128, 128), dst); err != nil {
			t.Fatalf("Encode CBR frame %d: %v", frame, err)
		}
	}
	if err := e.SetRateControl(RateControlConfig{
		Mode:                RateControlVBR,
		TargetBitrateKbps:   700,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        6000,
		BufferInitialSizeMs: 4000,
		BufferOptimalSizeMs: 5000,
	}); err != nil {
		t.Fatalf("SetRateControl(VBR): %v", err)
	}
	if e.rc.framesTillGF == 0 {
		t.Fatal("CBR->VBR runtime transition left golden cadence immediately due")
	}
	result, err := e.EncodeIntoWithResult(newVP9YCbCrForTest(width,
		height, 112, 128, 128), dst)
	if err != nil {
		t.Fatalf("Encode post-transition frame: %v", err)
	}
	if result.RefreshFrameFlags != 1<<vp9LastRefSlot {
		t.Fatalf("post-transition refresh flags = %#x, want LAST only",
			result.RefreshFrameFlags)
	}
}

func TestVP9SetRateControlPreservesOnePassGoldenCadence(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     RateControlCQ,
		TargetBitrateKbps:   700,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             20,
		MaxKeyframeInterval: 128,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dst := make([]byte, 65536)
	if _, err := e.EncodeIntoWithResult(newVP9YCbCrForTest(width,
		height, 96, 128, 128), dst); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	wantFramesTillGF := e.rc.framesTillGF
	if wantFramesTillGF == 0 {
		t.Fatal("initial CQ keyframe left golden cadence immediately due")
	}
	if err := e.SetRateControl(RateControlConfig{
		Mode:                RateControlQ,
		TargetBitrateKbps:   700,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             20,
		BufferSizeMs:        6000,
		BufferInitialSizeMs: 4000,
		BufferOptimalSizeMs: 5000,
	}); err != nil {
		t.Fatalf("SetRateControl(Q): %v", err)
	}
	if e.rc.framesTillGF != wantFramesTillGF {
		t.Fatalf("framesTillGF after CQ->Q = %d, want preserved %d",
			e.rc.framesTillGF, wantFramesTillGF)
	}
	result, err := e.EncodeIntoWithResult(newVP9YCbCrForTest(width,
		height, 104, 128, 128), dst)
	if err != nil {
		t.Fatalf("Encode post-transition frame: %v", err)
	}
	if result.RefreshFrameFlags != 1<<vp9LastRefSlot {
		t.Fatalf("post-transition refresh flags = %#x, want LAST only",
			result.RefreshFrameFlags)
	}
}

func TestVP9SetRateControlPreservesLibvpxAdaptiveState(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   700,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		DropFrameAllowed:    true,
		DropFrameWaterMark:  60,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	e.rc.bufferLevelBits = 0
	e.rc.decimationFactor = 2
	e.rc.decimationCount = 1
	e.rc.avgFrameQIndexKey = 41
	e.rc.avgFrameQIndexInter = 47
	e.rc.lastQKey = 33
	e.rc.lastQInter = 55
	e.rc.lastBoostedQIndex = 43
	e.rc.totalActualBits = 123456
	e.rc.totalTargetBits = 654321
	for i := range e.rc.rateCorrectionFactors {
		e.rc.rateCorrectionFactors[i] = 1.25 + float64(i)/4
		e.rc.dampedAdjustment[i] = i&1 == 1
	}
	e.rc.q1Frame = 31
	e.rc.q2Frame = 39
	e.rc.rc1Frame = -7
	e.rc.rc2Frame = 9
	e.rc.framesSinceKey = 77
	e.rc.framesTillGF = 3
	wantFactors := e.rc.rateCorrectionFactors
	wantDamped := e.rc.dampedAdjustment

	if err := e.SetRateControl(RateControlConfig{
		Mode:                RateControlCBR,
		TargetBitrateKbps:   900,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		DropFrameAllowed:    true,
		DropFrameWaterMark:  60,
	}); err != nil {
		t.Fatalf("SetRateControl(CBR): %v", err)
	}
	if e.rc.bufferLevelBits != 0 {
		t.Fatalf("buffer after rate-control change = %d, want preserved zero",
			e.rc.bufferLevelBits)
	}
	if e.rc.decimationFactor != 2 || e.rc.decimationCount != 1 {
		t.Fatalf("decimation state = factor:%d count:%d, want 2/1",
			e.rc.decimationFactor, e.rc.decimationCount)
	}
	if e.rc.avgFrameQIndexKey != 41 || e.rc.avgFrameQIndexInter != 47 ||
		e.rc.lastQKey != 33 || e.rc.lastQInter != 55 ||
		e.rc.lastBoostedQIndex != 43 {
		t.Fatalf("quantizer history = key:%d inter:%d last:%d/%d boosted:%d, want 41/47/33/55/43",
			e.rc.avgFrameQIndexKey, e.rc.avgFrameQIndexInter,
			e.rc.lastQKey, e.rc.lastQInter, e.rc.lastBoostedQIndex)
	}
	if e.rc.totalActualBits != 123456 || e.rc.totalTargetBits != 654321 {
		t.Fatalf("totals = actual:%d target:%d, want 123456/654321",
			e.rc.totalActualBits, e.rc.totalTargetBits)
	}
	if e.rc.rateCorrectionFactors != wantFactors ||
		e.rc.dampedAdjustment != wantDamped {
		t.Fatalf("rate correction state was not preserved")
	}
	if e.rc.q1Frame != 31 || e.rc.q2Frame != 39 ||
		e.rc.rc1Frame != -7 || e.rc.rc2Frame != 9 {
		t.Fatalf("recode history = q:%d/%d rc:%d/%d, want 31/39 -7/9",
			e.rc.q1Frame, e.rc.q2Frame, e.rc.rc1Frame, e.rc.rc2Frame)
	}
	if e.rc.framesSinceKey != 77 || e.rc.framesTillGF != 3 {
		t.Fatalf("frame cadence = since-key:%d till-gf:%d, want 77/3",
			e.rc.framesSinceKey, e.rc.framesTillGF)
	}
}

func TestVP9EncoderCyclicRefreshAQEmitsSegmentation(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              width,
		Height:             height,
		FPS:                30,
		TargetBitrateKbps:  300,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		AQMode:             VP9AQCyclicRefresh,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if !e.cyclicAQ.enabled || len(e.cyclicAQ.segMap) != 64 {
		t.Fatalf("cyclic AQ state = enabled:%t map:%d, want true/64",
			e.cyclicAQ.enabled, len(e.cyclicAQ.segMap))
	}

	dst := make([]byte, 65536)
	key, err := e.EncodeInto(newVP9YCbCrForTest(width, height, 96, 128, 128), dst)
	if err != nil {
		t.Fatalf("Encode key: %v", err)
	}
	keyPacket := append([]byte(nil), dst[:key]...)
	inter, err := e.EncodeInto(newVP9YCbCrForTest(width, height, 116, 128, 128), dst)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	interPacket := append([]byte(nil), dst[:inter]...)

	var keyBR vp9dec.BitReader
	keyBR.Init(keyPacket)
	keyHeader, err := vp9dec.ReadUncompressedHeader(&keyBR, nil, nil)
	if err != nil {
		t.Fatalf("ReadUncompressedHeader key: %v", err)
	}
	if keyHeader.Seg.Enabled {
		t.Fatal("keyframe segmentation enabled, want cyclic refresh to start on inter frames")
	}

	var interBR vp9dec.BitReader
	interBR.Init(interPacket)
	interHeader, err := vp9dec.ReadUncompressedHeader(&interBR, &keyHeader,
		func(uint8) (uint32, uint32) { return width, height })
	if err != nil {
		t.Fatalf("ReadUncompressedHeader inter: %v", err)
	}
	seg := interHeader.Seg
	if !seg.Enabled || !seg.UpdateMap || !seg.UpdateData || seg.AbsDelta {
		t.Fatalf("inter segmentation flags = enabled:%t updateMap:%t updateData:%t absDelta:%t, want true/true/true/false",
			seg.Enabled, seg.UpdateMap, seg.UpdateData, seg.AbsDelta)
	}
	if !vp9dec.SegFeatureActive(&seg, vp9CyclicRefreshSegmentBoost1, vp9dec.SegLvlAltQ) {
		t.Fatalf("cyclic segment %d missing AltQ feature", vp9CyclicRefreshSegmentBoost1)
	}
	delta1 := vp9dec.GetSegData(&seg, vp9CyclicRefreshSegmentBoost1, vp9dec.SegLvlAltQ)
	if delta1 >= 0 {
		t.Fatalf("cyclic segment AltQ delta = %d, want negative boost", delta1)
	}
	if !vp9dec.SegFeatureActive(&seg, vp9CyclicRefreshSegmentBoost2, vp9dec.SegLvlAltQ) {
		t.Fatalf("cyclic segment %d missing AltQ feature", vp9CyclicRefreshSegmentBoost2)
	}
	delta2 := vp9dec.GetSegData(&seg, vp9CyclicRefreshSegmentBoost2, vp9dec.SegLvlAltQ)
	if delta2 >= delta1 {
		t.Fatalf("cyclic segment deltas = %d/%d, want segment 2 stronger",
			delta1, delta2)
	}
	if seg.TreeProbs[3] != 1 {
		t.Fatalf("cyclic segment tree prob[3] = %d, want 1 for all segment-1 map",
			seg.TreeProbs[3])
	}
	boosted := 0
	for _, mi := range e.miGrid {
		if mi.SegmentID == vp9CyclicRefreshSegmentBoost1 {
			boosted++
		}
	}
	if boosted == 0 {
		t.Fatal("cyclic refresh AQ encoded no boosted segment blocks")
	}

	dec, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := dec.Decode(keyPacket); err != nil {
		t.Fatalf("Decode key: %v", err)
	}
	if _, ok := dec.NextFrame(); !ok {
		t.Fatal("NextFrame key returned !ok")
	}
	if err := dec.Decode(interPacket); err != nil {
		t.Fatalf("Decode inter: %v", err)
	}
	if _, ok := dec.NextFrame(); !ok {
		t.Fatal("NextFrame inter returned !ok")
	}
}

func TestVP9EncoderSetRealtimeTargetUpdatesExplicitVBR(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		TargetBitrateKbps:  300,
		RateControlModeSet: true,
		RateControlMode:    RateControlVBR,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{BitrateKbps: 600, FPS: 60}); err != nil {
		t.Fatalf("SetRealtimeTarget: %v", err)
	}
	if e.opts.TargetBitrateKbps != 600 || e.rc.targetBitrateKbps != 600 ||
		e.rc.bitsPerFrame != 10000 {
		t.Fatalf("rate state after target = opts:%d rc:%d bits:%d, want 600/600/10000",
			e.opts.TargetBitrateKbps, e.rc.targetBitrateKbps, e.rc.bitsPerFrame)
	}
}

func TestVP9EncoderLookaheadDelaysAndFlushes(t *testing.T) {
	const width, height = 64, 64
	firstSrc := newVP9YCbCrForTest(width, height, 96, 128, 128)
	secondSrc := newVP9YCbCrForTest(width, height, 160, 128, 128)

	delayed, err := NewVP9Encoder(VP9EncoderOptions{
		Width:           width,
		Height:          height,
		LookaheadFrames: 2,
	})
	if err != nil {
		t.Fatalf("delayed NewVP9Encoder: %v", err)
	}
	dst := make([]byte, 65536)
	if _, err := delayed.EncodeIntoWithResult(firstSrc, dst); !errors.Is(err, ErrFrameNotReady) {
		t.Fatalf("first lookahead encode err = %v, want ErrFrameNotReady", err)
	}
	if !equalVP9YCbCrForTest(&delayed.lookahead[0].img, firstSrc, width, height) {
		t.Fatal("lookahead copied first source incorrectly")
	}
	gotFirst, err := delayed.EncodeIntoWithFlagsResult(secondSrc, dst,
		EncodeNoUpdateLast|EncodeForceAltRefFrame)
	if err != nil {
		t.Fatalf("second lookahead encode: %v", err)
	}
	if !gotFirst.KeyFrame || len(gotFirst.Data) == 0 {
		t.Fatalf("second call result = key:%t bytes:%d, want delayed first key packet",
			gotFirst.KeyFrame, len(gotFirst.Data))
	}
	gotSecond, err := delayed.FlushIntoWithResult(dst)
	if err != nil {
		t.Fatalf("FlushIntoWithResult: %v", err)
	}
	if gotSecond.KeyFrame || len(gotSecond.Data) == 0 ||
		gotSecond.RefreshFrameFlags&(1<<vp9AltRefSlot) == 0 ||
		gotSecond.RefreshFrameFlags&(1<<vp9LastRefSlot) != 0 {
		t.Fatalf("flushed packet = key:%t bytes:%d refresh:%#x, want queued alt-ref inter",
			gotSecond.KeyFrame, len(gotSecond.Data), gotSecond.RefreshFrameFlags)
	}
	if n, err := delayed.FlushInto(dst); !errors.Is(err, ErrFrameNotReady) || n != 0 {
		t.Fatalf("empty FlushInto = n:%d err:%v, want 0/ErrFrameNotReady", n, err)
	}
}

func TestVP9EncoderAutoAltRefLookaheadEmitsHiddenAltRef(t *testing.T) {
	const width, height = 64, 64
	const frames = 6
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:           width,
		Height:          height,
		LookaheadFrames: 4,
		AutoAltRef:      true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}

	dst := make([]byte, 65536)
	results := make([]VP9EncodeResult, 0, frames+1)
	packets := make([][]byte, 0, frames+1)
	for frame := range frames {
		src := newVP9YCbCrForTest(width, height, uint8(80+frame*17), 128, 128)
		result, err := e.EncodeIntoWithResult(src, dst)
		if errors.Is(err, ErrFrameNotReady) {
			continue
		}
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", frame, err)
		}
		results = append(results, result)
		packets = append(packets, append([]byte(nil), result.Data...))
	}
	for {
		result, err := e.FlushIntoWithResult(dst)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("FlushIntoWithResult: %v", err)
		}
		results = append(results, result)
		packets = append(packets, append([]byte(nil), result.Data...))
	}
	if got, want := len(results), frames+1; got != want {
		t.Fatalf("auto-alt-ref packets = %d, want %d", got, want)
	}

	hiddenIndex := -1
	for i := range results {
		if !results[i].ShowFrame {
			if hiddenIndex >= 0 {
				t.Fatalf("multiple hidden packets: first=%d second=%d", hiddenIndex, i)
			}
			hiddenIndex = i
		}
	}
	if hiddenIndex < 0 {
		t.Fatal("auto-alt-ref emitted no hidden packet")
	}
	hidden := results[hiddenIndex]
	if hidden.KeyFrame || hidden.Dropped || len(hidden.Data) == 0 ||
		hidden.RefreshFrameFlags != 1<<vp9AltRefSlot {
		t.Fatalf("hidden packet = key:%t dropped:%t bytes:%d refresh:%#x, want inter ALTREF refresh",
			hidden.KeyFrame, hidden.Dropped, len(hidden.Data), hidden.RefreshFrameFlags)
	}
	if hiddenIndex == 0 || !results[0].KeyFrame {
		t.Fatalf("hidden index/key ordering = index:%d firstKey:%t, want hidden after first key",
			hiddenIndex, results[0].KeyFrame)
	}
	for i := range results {
		if i == hiddenIndex {
			continue
		}
		if !results[i].ShowFrame || results[i].Dropped || len(results[i].Data) == 0 {
			t.Fatalf("visible packet %d = show:%t dropped:%t bytes:%d",
				i, results[i].ShowFrame, results[i].Dropped, len(results[i].Data))
		}
	}

	dec, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	visible := 0
	for i, packet := range packets {
		if err := dec.Decode(packet); err != nil {
			t.Fatalf("Decode packet %d: %v", i, err)
		}
		if i == hiddenIndex {
			if _, ok := dec.NextFrame(); ok {
				t.Fatal("NextFrame returned visible output after hidden ALTREF")
			}
			if info, ok := dec.LastFrameInfo(); !ok || info.ShowFrame ||
				info.RefreshFrameFlags != 1<<vp9AltRefSlot {
				t.Fatalf("LastFrameInfo after hidden = %+v ok=%t, want hidden ALTREF refresh",
					info, ok)
			}
			continue
		}
		if _, ok := dec.NextFrame(); !ok {
			t.Fatalf("NextFrame packet %d returned !ok", i)
		}
		visible++
	}
	if visible != frames {
		t.Fatalf("visible decoded frames = %d, want %d", visible, frames)
	}
}

func TestVP9EncoderAutoAltRefARNRFiltersHiddenSource(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:           width,
		Height:          height,
		LookaheadFrames: 4,
		AutoAltRef:      true,
		ARNRMaxFrames:   5,
		ARNRStrength:    6,
		ARNRType:        1,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	for i := 0; i < 4; i++ {
		src := newVP9YCbCrForTest(width, height, uint8(100+i*4), 128, 128)
		if err := e.pushVP9Lookahead(src, 0); err != nil {
			t.Fatalf("pushVP9Lookahead %d: %v", i, err)
		}
	}
	future, ok := e.newestVP9LookaheadEntry()
	if !ok {
		t.Fatal("newestVP9LookaheadEntry returned !ok")
	}
	before := append([]byte(nil), future.img.Y...)
	if !e.applyVP9ARNRFilter(future) {
		t.Fatal("applyVP9ARNRFilter returned false")
	}
	if bytes.Equal(e.vp9ARNRScratch.Y, future.img.Y) {
		t.Fatal("ARNR scratch luma matches unfiltered future source")
	}
	if !bytes.Equal(before, future.img.Y) {
		t.Fatal("ARNR mutated queued future source")
	}
}

func TestVP9EncoderAutoAltRefARNRSteadyStateAlloc(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:           width,
		Height:          height,
		LookaheadFrames: 4,
		AutoAltRef:      true,
		ARNRMaxFrames:   5,
		ARNRStrength:    6,
		ARNRType:        1,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	for i := 0; i < 4; i++ {
		src := newVP9YCbCrForTest(width, height, uint8(100+i*4), 128, 128)
		if err := e.pushVP9Lookahead(src, 0); err != nil {
			t.Fatalf("pushVP9Lookahead %d: %v", i, err)
		}
	}
	future, ok := e.newestVP9LookaheadEntry()
	if !ok {
		t.Fatal("newestVP9LookaheadEntry returned !ok")
	}
	if !e.applyVP9ARNRFilter(future) {
		t.Fatal("warm applyVP9ARNRFilter returned false")
	}
	allocs := testing.AllocsPerRun(10, func() {
		if !e.applyVP9ARNRFilter(future) {
			t.Fatal("applyVP9ARNRFilter returned false")
		}
	})
	if allocs != 0 {
		t.Fatalf("VP9 ARNR steady state: got %v allocs/op, want 0", allocs)
	}
}

func equalVP9YCbCrForTest(a *image.YCbCr, b *image.YCbCr, width int, height int) bool {
	for y := 0; y < height; y++ {
		if !bytes.Equal(a.Y[y*a.YStride:][:width], b.Y[y*b.YStride:][:width]) {
			return false
		}
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for y := 0; y < uvHeight; y++ {
		if !bytes.Equal(a.Cb[y*a.CStride:][:uvWidth], b.Cb[y*b.CStride:][:uvWidth]) ||
			!bytes.Equal(a.Cr[y*a.CStride:][:uvWidth], b.Cr[y*b.CStride:][:uvWidth]) {
			return false
		}
	}
	return true
}

func TestVP9EncoderLookaheadQueuedFrameBlocksResize(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:           width,
		Height:          height,
		LookaheadFrames: 2,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dst := make([]byte, 65536)
	if _, err := e.EncodeIntoWithResult(
		newVP9YCbCrForTest(width, height, 96, 128, 128), dst); !errors.Is(err, ErrFrameNotReady) {
		t.Fatalf("lookahead fill err = %v, want ErrFrameNotReady", err)
	}
	err = e.SetRealtimeTarget(RealtimeTarget{Width: 96, Height: 64})
	if !errors.Is(err, ErrFrameNotReady) {
		t.Fatalf("queued resize err = %v, want ErrFrameNotReady", err)
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

func TestVP9EncoderFrameTxModeFromCountsReducesFixedMode(t *testing.T) {
	counts := &vp9enc.FrameCounts{}
	counts.TxTotals[common.Tx16x16] = 1
	counts.TxTotals[common.Tx8x8] = 1
	if got := vp9EncoderFrameTxModeFromCounts(common.Allow32x32, false, counts); got != common.Allow16x16 {
		t.Fatalf("tx mode = %d, want Allow16x16", got)
	}

	counts = &vp9enc.FrameCounts{}
	counts.TxTotals[common.Tx4x4] = 1
	if got := vp9EncoderFrameTxModeFromCounts(common.Allow32x32, false, counts); got != common.Only4x4 {
		t.Fatalf("tx mode = %d, want Only4x4", got)
	}

	counts = &vp9enc.FrameCounts{}
	counts.TxTotals[common.Tx32x32] = 1
	if got := vp9EncoderFrameTxModeFromCounts(common.Allow32x32, false, counts); got != common.Allow32x32 {
		t.Fatalf("tx mode = %d, want Allow32x32", got)
	}
	if got := vp9EncoderFrameTxModeFromCounts(common.TxModeSelect, false, counts); got != common.TxModeSelect {
		t.Fatalf("select tx mode = %d, want TxModeSelect", got)
	}
}

// TestVP9EncoderKeyframeStubProducesParseableBitstream: the constant
// source-backed keyframe path emits oracle-shaped Block32x32 / Tx16 DC
// skip leaves whose every layer parses cleanly through the decoder.
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
	if out.TxMode != common.Allow16x16 {
		t.Errorf("TxMode = %d, want Allow16x16", out.TxMode)
	}

	// Layer 3: tile body via full packet decode.
	grid := decodeVP9PacketMiGridForOracleTest(t, got)
	if len(grid) != 64 {
		t.Fatalf("decoded mi grid len = %d, want 64", len(grid))
	}
	for i, mi := range grid {
		if mi.SbType != common.Block32x32 || mi.TxSize != common.Tx16x16 ||
			mi.Mode != common.DcPred || mi.Skip != 1 ||
			mi.RefFrame[0] != vp9dec.IntraFrame {
			t.Fatalf("mi[%d] = %+v, want Block32x32/Tx16/DcPred/skip intra", i, mi)
		}
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
	assertVP9FilledFrameWithin(t, frame, 96, 80, 91, 143, 37, 1)
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

func TestVP9EncoderACKeyframeUsesOracleNoUpdateCompressedHeader(t *testing.T) {
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
	if h.FirstPartitionSize != 2 {
		t.Fatalf("FirstPartitionSize = %d, want oracle no-update compressed header size 2", h.FirstPartitionSize)
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

func TestVP9EncoderDefaultInterQuantizerUsesPinnedCQBaseQIndex(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	src := newVP9YCbCrForTest(64, 64, 128, 128, 128)
	key, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	inter, err := e.Encode(src)
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
		func(uint8) (uint32, uint32) { return 64, 64 })
	if err != nil {
		t.Fatalf("ReadUncompressedHeader inter: %v", err)
	}
	if got := int(interHeader.Quant.BaseQindex); got != vp9DefaultInterBaseQIndex {
		t.Fatalf("inter BaseQindex = %d, want pinned default %d",
			got, vp9DefaultInterBaseQIndex)
	}
}

func TestVP9PublicQuantizerMappingMatchesLibvpxTable(t *testing.T) {
	want := []int{
		0, 4, 8, 12, 16, 20, 24, 28,
		32, 36, 40, 44, 48, 52, 56, 60,
		64, 68, 72, 76, 80, 84, 88, 92,
		96, 100, 104, 108, 112, 116, 120, 124,
		128, 132, 136, 140, 144, 148, 152, 156,
		160, 164, 168, 172, 176, 180, 184, 188,
		192, 196, 200, 204, 208, 212, 216, 220,
		224, 228, 232, 236, 240, 244, 249, 255,
	}
	for q, qindex := range want {
		if got := vp9PublicQuantizerToQIndex(q); got != qindex {
			t.Fatalf("vp9PublicQuantizerToQIndex(%d) = %d, want %d",
				q, got, qindex)
		}
		if got := vp9QIndexToPublicQuantizer(qindex); got != q {
			t.Fatalf("vp9QIndexToPublicQuantizer(%d) = %d, want %d",
				qindex, got, q)
		}
	}
}

func TestVP9EncoderPublicFixedQuantizerControlsQIndex(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:        width,
		Height:       height,
		MinQuantizer: 20,
		MaxQuantizer: 20,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	src := newVP9YCbCrForTest(width, height, 128, 128, 128)
	key, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	inter, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	wantQIndex := vp9PublicQuantizerToQIndex(20)
	keyHeader, _ := parseVP9EncoderHeaderForTest(t, key)
	if got := int(keyHeader.Quant.BaseQindex); got != wantQIndex {
		t.Fatalf("key BaseQindex = %d, want %d", got, wantQIndex)
	}
	interHeader, _ := parseVP9EncoderHeaderForTest(t, inter)
	if got := int(interHeader.Quant.BaseQindex); got != wantQIndex {
		t.Fatalf("inter BaseQindex = %d, want %d", got, wantQIndex)
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

func TestVP9EncoderLosslessKeyframeRoundTripExact(t *testing.T) {
	const width, height = 32, 32
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:    width,
		Height:   height,
		Lossless: true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	img := newVP9CheckerYCbCrForTest(width, height, 16, 240, 32, 224)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode lossless keyframe: %v", err)
	}

	var br vp9dec.BitReader
	br.Init(packet)
	h, err := vp9dec.ReadUncompressedHeader(&br, nil, nil)
	if err != nil {
		t.Fatalf("ReadUncompressedHeader: %v", err)
	}
	if h.Quant.BaseQindex != 0 || !h.Quant.Lossless {
		t.Fatalf("quantization = %+v, want lossless qindex 0", h.Quant)
	}
	if h.Loopfilter.FilterLevel != 0 {
		t.Fatalf("loop filter level = %d, want 0 for lossless", h.Loopfilter.FilterLevel)
	}
	uncSize := br.BytesRead()
	compEnd := uncSize + int(h.FirstPartitionSize)
	if compEnd > len(packet) {
		t.Fatalf("compressed header end %d past packet len %d", compEnd, len(packet))
	}
	var cr bitstream.Reader
	if err := cr.Init(packet[uncSize:compEnd]); err != nil {
		t.Fatalf("compressed reader Init: %v", err)
	}
	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	out := vp9dec.ReadCompressedHeader(&cr, &fc, vp9dec.ReadCompressedHeaderArgs{
		Lossless:     h.Quant.Lossless,
		IntraOnly:    true,
		KeyFrame:     true,
		InterpFilter: vp9dec.InterpEighttap,
	})
	if out.TxMode != common.Only4x4 {
		t.Fatalf("TxMode = %d, want Only4x4 for lossless", out.TxMode)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode lossless keyframe: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after lossless keyframe")
	}
	assertVP9ImageMatchesYCbCr(t, "lossless keyframe", frame, img)
}

func TestVP9EncoderLosslessInterRoundTripExact(t *testing.T) {
	const width, height = 32, 32
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:    width,
		Height:   height,
		Lossless: true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	keySrc := newVP9MotionYCbCrForTest(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode lossless keyframe: %v", err)
	}
	interSrc := newVP9CheckerYCbCrForTest(width, height, 16, 240, 32, 224)
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode lossless inter: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode lossless keyframe: %v", err)
	}
	keyFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after lossless keyframe")
	}
	assertVP9ImageMatchesYCbCr(t, "lossless keyframe", keyFrame, keySrc)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode lossless inter: %v", err)
	}
	interFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after lossless inter frame")
	}
	assertVP9ImageMatchesYCbCr(t, "lossless inter frame", interFrame, interSrc)
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
	src := newVP9CheckerYCbCrForTest(width, height, 48, 208, 128, 128)
	key, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	keyHeader, _ := parseVP9EncoderHeaderForTest(t, key)
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
	if _, err := e.Encode(newVP9YCbCrForTest(width, height, 72, 128, 128)); err != nil {
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
	key, err := e.Encode(newVP9YCbCrForTest(width, height, 72, 128, 128))
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	keyHeader, _ := parseVP9EncoderHeaderForTest(t, key)
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
		h, _ := parseVP9EncoderHeaderForTest(t, dst[:n])
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

func TestVP9EncoderSetRealtimeTargetUpdatesHints(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width:             64,
		Height:            64,
		FPS:               30,
		TargetBitrateKbps: 900,
	})

	if err := e.SetRealtimeTarget(RealtimeTarget{
		BitrateKbps: 1500,
		FPS:         60,
	}); err != nil {
		t.Fatalf("SetRealtimeTarget: %v", err)
	}
	if e.opts.TargetBitrateKbps != 1500 ||
		e.opts.FPS != 60 ||
		e.opts.TimebaseNum != 1 ||
		e.opts.TimebaseDen != 60 {
		t.Fatalf("opts after target = %+v, want bitrate 1500 and 1/60 timebase",
			e.opts)
	}
}

func TestVP9EncoderSetRealtimeTargetResizeForcesKeyFrame(t *testing.T) {
	const (
		w1 = 64
		h1 = 64
		w2 = 96
		h2 = 80
	)
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: w1, Height: h1})
	if _, err := e.Encode(newVP9YCbCrForTest(w1, h1, 72, 128, 128)); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	inter, err := e.Encode(newVP9YCbCrForTest(w1, h1, 92, 128, 128))
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	if h, _ := parseVP9EncoderHeaderForTest(t, inter); h.FrameType != common.InterFrame {
		t.Fatalf("pre-resize frame type = %d, want inter", h.FrameType)
	}
	if !e.refValid[vp9LastRefSlot] || !e.refFrames[vp9LastRefSlot].valid {
		t.Fatal("LAST reference not valid before resize")
	}

	if err := e.SetRealtimeTarget(RealtimeTarget{Width: w2, Height: h2}); err != nil {
		t.Fatalf("SetRealtimeTarget resize: %v", err)
	}
	if e.opts.Width != w2 || e.opts.Height != h2 {
		t.Fatalf("dims after resize = %dx%d, want %dx%d",
			e.opts.Width, e.opts.Height, w2, h2)
	}
	if !e.IsKeyFrameNext() || !e.forceKeyFrame {
		t.Fatal("resize did not force the next VP9 frame to keyframe")
	}
	for slot := range e.refValid {
		if e.refValid[slot] || e.refFrames[slot].valid {
			t.Fatalf("reference slot %d still valid after resize", slot)
		}
	}
	if _, err := e.Encode(newVP9YCbCrForTest(w1, h1, 100, 128, 128)); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("old-size Encode after resize err = %v, want ErrInvalidConfig", err)
	}

	resized, err := e.Encode(newVP9YCbCrForTest(w2, h2, 111, 123, 211))
	if err != nil {
		t.Fatalf("Encode resized keyframe: %v", err)
	}
	h, _ := parseVP9EncoderHeaderForTest(t, resized)
	if h.FrameType != common.KeyFrame || h.Width != w2 || h.Height != h2 {
		t.Fatalf("resized header = type:%d %dx%d, want key %dx%d",
			h.FrameType, h.Width, h.Height, w2, h2)
	}
	if e.forceKeyFrame {
		t.Fatal("forceKeyFrame still set after resized keyframe")
	}
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(resized); err != nil {
		t.Fatalf("Decode resized keyframe: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after resized keyframe")
	}
	assertVP9FilledFrameWithin(t, frame, w2, h2, 111, 123, 211, 1)
}

func TestVP9EncoderSetRealtimeTargetValidationNoMutation(t *testing.T) {
	const width, height = 64, 64
	cases := []struct {
		name   string
		target RealtimeTarget
		want   error
	}{
		{"negative bitrate", RealtimeTarget{BitrateKbps: -1}, ErrInvalidConfig},
		{"one dimension", RealtimeTarget{Width: 96}, ErrInvalidConfig},
		{"too wide", RealtimeTarget{Width: maxVP9Dimension + 1, Height: 64}, ErrInvalidConfig},
		{"explicit frame drop", RealtimeTarget{FrameDrop: RealtimeFrameDropEnabled}, ErrInvalidConfig},
		{"bad quantizer range", RealtimeTarget{MinQuantizer: 50, MaxQuantizer: 20}, ErrInvalidQuantizer},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
			if err := e.SetRealtimeTarget(tc.target); !errors.Is(err, tc.want) {
				t.Fatalf("SetRealtimeTarget error = %v, want %v", err, tc.want)
			}
			if e.opts.Width != width || e.opts.Height != height ||
				e.forceKeyFrame || e.opts.TargetBitrateKbps != 0 {
				t.Fatalf("encoder mutated after reject: opts=%+v forceKeyFrame=%t",
					e.opts, e.forceKeyFrame)
			}
			packet, err := e.Encode(newVP9YCbCrForTest(width, height, 80, 128, 128))
			if err != nil {
				t.Fatalf("Encode after rejected target: %v", err)
			}
			info, err := PeekVP9StreamInfo(packet)
			if err != nil {
				t.Fatalf("PeekVP9StreamInfo: %v", err)
			}
			if !info.KeyFrame || info.Width != width || info.Height != height {
				t.Fatalf("info after rejected target = %+v, want original keyframe", info)
			}
		})
	}
}

func TestVP9EncoderSetRealtimeTargetUpdatesQuantizerBounds(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{MinQuantizer: 20, MaxQuantizer: 20}); err != nil {
		t.Fatalf("SetRealtimeTarget: %v", err)
	}
	if e.opts.MinQuantizer != 20 || e.opts.MaxQuantizer != 20 {
		t.Fatalf("quantizer bounds = %d/%d, want 20/20",
			e.opts.MinQuantizer, e.opts.MaxQuantizer)
	}
	packet, err := e.Encode(newVP9YCbCrForTest(width, height, 80, 128, 128))
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	h, _ := parseVP9EncoderHeaderForTest(t, packet)
	if got, want := int(h.Quant.BaseQindex), vp9PublicQuantizerToQIndex(20); got != want {
		t.Fatalf("BaseQindex = %d, want %d", got, want)
	}
}

func TestVP9EncoderCBRDropBufferUnderrunReturnsDropped(t *testing.T) {
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
	src := newVP9YCbCrForTest(width, height, 128, 128, 128)
	dst := make([]byte, 65536)
	key, err := e.EncodeIntoWithResult(src, dst)
	if err != nil {
		t.Fatalf("key EncodeIntoWithResult: %v", err)
	}
	if !key.KeyFrame || key.Dropped || len(key.Data) == 0 {
		t.Fatalf("key result = key:%t dropped:%t data:%d, want encoded keyframe",
			key.KeyFrame, key.Dropped, len(key.Data))
	}

	e.rc.bufferLevelBits = -e.rc.bitsPerFrame - 1
	drainedBuffer := e.rc.bufferLevelBits
	wantBufferAfterRefill := drainedBuffer + e.rc.bitsPerFrame
	inter, err := e.EncodeIntoWithResult(src, dst)
	if err != nil {
		t.Fatalf("inter EncodeIntoWithResult: %v", err)
	}
	if !inter.Dropped || inter.KeyFrame || len(inter.Data) != 0 || inter.SizeBytes != 0 {
		t.Fatalf("inter result = key:%t dropped:%t size:%d data:%d, want dropped inter",
			inter.KeyFrame, inter.Dropped, inter.SizeBytes, len(inter.Data))
	}
	if inter.TargetBitrateKbps != 1 || inter.FrameTargetBits != vp9FrameOverhead {
		t.Fatalf("inter rate = kbps:%d target:%d, want 1/%d",
			inter.TargetBitrateKbps, inter.FrameTargetBits, vp9FrameOverhead)
	}
	if inter.BufferLevelBits != wantBufferAfterRefill {
		t.Fatalf("buffer after drop = %d, want %d",
			inter.BufferLevelBits, wantBufferAfterRefill)
	}
}

func TestVP9EncoderCBRSelectsLibvpxQuantizers(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		TargetBitrateKbps:   700,
		RateControlModeSet:  true,
		RateControlMode:     RateControlCBR,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		MaxKeyframeInterval: 128,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dst := make([]byte, 65536)
	wantQ := [...]int{16, 145, 145, 162}
	for i, want := range wantQ {
		src := newVP9YCbCrForTest(width, height, uint8(96+i*11), 128, 128)
		result, err := e.EncodeIntoWithResult(src, dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", i, err)
		}
		if result.InternalQuantizer != want {
			t.Fatalf("frame %d internal quantizer = %d, want %d",
				i, result.InternalQuantizer, want)
		}
	}
}

func TestVP9EncoderCBRFrameTargetUsesPerFrameBandwidth(t *testing.T) {
	const width, height = 64, 64
	for _, targetKbps := range [...]int{700, 140} {
		e, err := NewVP9Encoder(VP9EncoderOptions{
			Width:               width,
			Height:              height,
			FPS:                 30,
			TargetBitrateKbps:   targetKbps,
			RateControlModeSet:  true,
			RateControlMode:     RateControlCBR,
			BufferSizeMs:        600,
			BufferInitialSizeMs: 400,
			BufferOptimalSizeMs: 500,
			MinQuantizer:        4,
			MaxQuantizer:        56,
			MaxKeyframeInterval: 128,
		})
		if err != nil {
			t.Fatalf("NewVP9Encoder target %d: %v", targetKbps, err)
		}
		dst := make([]byte, 65536)
		wantTarget := targetKbps * 1000 / 30
		for i := 0; i < 3; i++ {
			src := newVP9YCbCrForTest(width, height, uint8(96+i*11), 128, 128)
			result, err := e.EncodeIntoWithResult(src, dst)
			if err != nil {
				t.Fatalf("EncodeIntoWithResult target %d frame %d: %v",
					targetKbps, i, err)
			}
			if result.FrameTargetBits != wantTarget {
				t.Fatalf("target %d frame %d target bits = %d, want %d",
					targetKbps, i, result.FrameTargetBits, wantTarget)
			}
		}
	}
}

func TestVP9EncoderCBRDropDoesNotDropKeyOrInvisibleFrame(t *testing.T) {
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
	src := newVP9YCbCrForTest(width, height, 128, 128, 128)
	dst := make([]byte, 65536)

	e.rc.bufferLevelBits = -1
	key, err := e.EncodeIntoWithResult(src, dst)
	if err != nil {
		t.Fatalf("key EncodeIntoWithResult: %v", err)
	}
	if !key.KeyFrame || key.Dropped || len(key.Data) == 0 {
		t.Fatalf("key result = key:%t dropped:%t data:%d, want encoded keyframe",
			key.KeyFrame, key.Dropped, len(key.Data))
	}

	e.rc.bufferLevelBits = -1
	hidden, err := e.EncodeIntoWithFlagsResult(src, dst, EncodeInvisibleFrame)
	if err != nil {
		t.Fatalf("hidden EncodeIntoWithFlagsResult: %v", err)
	}
	if hidden.Dropped || hidden.KeyFrame || hidden.ShowFrame || len(hidden.Data) == 0 {
		t.Fatalf("hidden result = key:%t show:%t dropped:%t data:%d, want encoded hidden inter",
			hidden.KeyFrame, hidden.ShowFrame, hidden.Dropped, len(hidden.Data))
	}
}

func TestVP9RateControlDropWatermarkDecimation(t *testing.T) {
	rc := vp9RateControlState{
		enabled:             true,
		mode:                RateControlCBR,
		dropFrameAllowed:    true,
		dropFramesWaterMark: 60,
		bufferOptimalBits:   10000,
		bufferSizeBits:      12000,
		bitsPerFrame:        1000,
	}

	rc.bufferLevelBits = 6000
	reason, drop := rc.testDropInterFrame()
	if drop || reason != vp9DropNone || rc.decimationFactor != 1 || rc.decimationCount != 1 {
		t.Fatalf("first watermark check = reason:%d drop:%t factor:%d count:%d, want arm only",
			reason, drop, rc.decimationFactor, rc.decimationCount)
	}
	reason, drop = rc.testDropInterFrame()
	if !drop || reason != vp9DropWatermarkDecimation || rc.decimationFactor != 1 || rc.decimationCount != 0 {
		t.Fatalf("second watermark check = reason:%d drop:%t factor:%d count:%d, want decimation drop",
			reason, drop, rc.decimationFactor, rc.decimationCount)
	}
	reason, drop = rc.testDropInterFrame()
	if drop || reason != vp9DropNone || rc.decimationFactor != 1 || rc.decimationCount != 1 {
		t.Fatalf("third watermark check = reason:%d drop:%t factor:%d count:%d, want re-arm",
			reason, drop, rc.decimationFactor, rc.decimationCount)
	}

	rc.bufferLevelBits = 7000
	reason, drop = rc.testDropInterFrame()
	if drop || reason != vp9DropNone || rc.decimationFactor != 0 || rc.decimationCount != 0 {
		t.Fatalf("recovered watermark check = reason:%d drop:%t factor:%d count:%d, want reset",
			reason, drop, rc.decimationFactor, rc.decimationCount)
	}
}

func TestVP9RateControlDropNegativeBufferBypassesWatermark(t *testing.T) {
	rc := vp9RateControlState{
		enabled:             true,
		mode:                RateControlCBR,
		dropFrameAllowed:    true,
		dropFramesWaterMark: 60,
		bufferOptimalBits:   10000,
		decimationFactor:    1,
		decimationCount:     1,
		bufferLevelBits:     -1,
	}

	reason, drop := rc.testDropInterFrame()
	if !drop || reason != vp9DropNegativeBuffer {
		t.Fatalf("negative buffer drop = reason:%d drop:%t, want negative-buffer drop",
			reason, drop)
	}
	if rc.decimationFactor != 1 || rc.decimationCount != 1 {
		t.Fatalf("negative buffer changed decimation = factor:%d count:%d, want unchanged 1/1",
			rc.decimationFactor, rc.decimationCount)
	}
}

func TestVP9RateControlPreEncodeRefillPrecedesDropGate(t *testing.T) {
	rc := vp9RateControlState{
		enabled:             true,
		mode:                RateControlCBR,
		dropFrameAllowed:    true,
		dropFramesWaterMark: 60,
		bufferOptimalBits:   10000,
		bufferSizeBits:      12000,
		bitsPerFrame:        1000,
		bufferLevelBits:     -1,
	}

	rc.preEncodeFrame(true)
	reason, drop := rc.testDropInterFrame()
	if drop || reason != vp9DropNone || rc.bufferLevelBits != 999 ||
		rc.decimationFactor != 1 || rc.decimationCount != 1 {
		t.Fatalf("first drop gate = reason:%d drop:%t buffer:%d factor:%d count:%d, want pre-refill arm only",
			reason, drop, rc.bufferLevelBits, rc.decimationFactor,
			rc.decimationCount)
	}
	rc.preEncodeFrame(true)
	reason, drop = rc.testDropInterFrame()
	if !drop || reason != vp9DropWatermarkDecimation ||
		rc.bufferLevelBits != 1999 || rc.decimationFactor != 1 ||
		rc.decimationCount != 0 {
		t.Fatalf("second drop gate = reason:%d drop:%t buffer:%d factor:%d count:%d, want watermark decimation",
			reason, drop, rc.bufferLevelBits, rc.decimationFactor,
			rc.decimationCount)
	}
}

func TestVP9EncoderSetRealtimeTargetFrameDropMode(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		TargetBitrateKbps:  300,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		DropFrameAllowed:   true,
		DropFrameWaterMark: 75,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{BitrateKbps: 600}); err != nil {
		t.Fatalf("bitrate SetRealtimeTarget: %v", err)
	}
	if !e.rc.dropFrameAllowed || !e.opts.DropFrameAllowed {
		t.Fatal("bitrate-only SetRealtimeTarget disabled frame dropping")
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{FrameDrop: RealtimeFrameDropDisabled}); err != nil {
		t.Fatalf("disable FrameDrop: %v", err)
	}
	if e.rc.dropFrameAllowed || e.opts.DropFrameAllowed {
		t.Fatal("FrameDrop disabled did not clear VP9 drop toggle")
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{FrameDrop: RealtimeFrameDropEnabled}); err != nil {
		t.Fatalf("enable FrameDrop: %v", err)
	}
	if !e.rc.dropFrameAllowed || !e.opts.DropFrameAllowed ||
		e.rc.dropFramesWaterMark != 75 {
		t.Fatalf("drop state = allowed:%t opts:%t mark:%d, want true/true/75",
			e.rc.dropFrameAllowed, e.opts.DropFrameAllowed, e.rc.dropFramesWaterMark)
	}
}

func TestVP9EncoderSetBitrateKbpsUpdatesRateControl(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:               64,
		Height:              64,
		FPS:                 30,
		TargetBitrateKbps:   300,
		RateControlModeSet:  true,
		RateControlMode:     RateControlCBR,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		TemporalScalability: TemporalScalabilityConfig{
			Enabled: true,
			Mode:    TemporalLayeringTwoLayers,
		},
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	e.rc.bufferLevelBits = maxInt()
	if err := e.SetBitrateKbps(900); err != nil {
		t.Fatalf("SetBitrateKbps: %v", err)
	}
	if e.opts.TargetBitrateKbps != 900 || e.rc.targetBitrateKbps != 900 ||
		e.rc.targetBandwidthBits != 900000 || e.rc.bitsPerFrame != 30000 {
		t.Fatalf("bitrate state = opts:%d rc:%d bandwidth:%d bpf:%d, want 900/900/900000/30000",
			e.opts.TargetBitrateKbps, e.rc.targetBitrateKbps,
			e.rc.targetBandwidthBits, e.rc.bitsPerFrame)
	}
	if e.rc.bufferSizeBits != 540000 || e.rc.bufferInitialBits != 360000 ||
		e.rc.bufferOptimalBits != 450000 || e.rc.bufferLevelBits != 540000 {
		t.Fatalf("buffer bits = size:%d initial:%d optimal:%d level:%d, want 540000/360000/450000/540000",
			e.rc.bufferSizeBits, e.rc.bufferInitialBits,
			e.rc.bufferOptimalBits, e.rc.bufferLevelBits)
	}
	if got := e.opts.TemporalScalability.LayerTargetBitrateKbps; got[0] != 540 || got[1] != 900 {
		t.Fatalf("temporal bitrates after SetBitrateKbps = %v, want [540 900 ...]", got)
	}

	oldRC := e.rc
	oldOpts := e.opts
	oldTemporal := e.temporal
	oldTwoPass := e.twoPass
	if err := e.SetBitrateKbps(0); !errors.Is(err, ErrInvalidBitrate) {
		t.Fatalf("invalid SetBitrateKbps err = %v, want ErrInvalidBitrate", err)
	}
	if e.rc != oldRC || !reflect.DeepEqual(e.opts, oldOpts) ||
		e.temporal != oldTemporal || !reflect.DeepEqual(e.twoPass, oldTwoPass) {
		t.Fatal("invalid SetBitrateKbps mutated encoder state")
	}

	publicQ, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	if err != nil {
		t.Fatalf("NewVP9Encoder public-Q: %v", err)
	}
	if err := publicQ.SetBitrateKbps(900); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("public-Q SetBitrateKbps err = %v, want ErrInvalidConfig", err)
	}
}

func TestVP9EncoderSetCQLevelUpdatesPublicQAndRateControl(t *testing.T) {
	publicQ, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	if err != nil {
		t.Fatalf("NewVP9Encoder public-Q: %v", err)
	}
	beforeQ := publicQ.vp9EncoderPublicQModeQIndex(false, false, 0)
	if err := publicQ.SetCQLevel(20); err != nil {
		t.Fatalf("public-Q SetCQLevel: %v", err)
	}
	afterQ := publicQ.vp9EncoderPublicQModeQIndex(false, false, 0)
	if publicQ.opts.CQLevel != 20 || afterQ == beforeQ {
		t.Fatalf("public-Q CQ update = opts:%d q:%d before:%d, want changed level 20",
			publicQ.opts.CQLevel, afterQ, beforeQ)
	}

	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		TargetBitrateKbps:  700,
		RateControlModeSet: true,
		RateControlMode:    RateControlQ,
		MinQuantizer:       4,
		MaxQuantizer:       56,
		CQLevel:            20,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder RC-Q: %v", err)
	}
	if err := e.SetCQLevel(24); err != nil {
		t.Fatalf("RC-Q SetCQLevel: %v", err)
	}
	if e.opts.CQLevel != 24 ||
		e.rc.cqLevel != uint8(vp9PublicQuantizerToQIndex(24)) {
		t.Fatalf("RC-Q CQ update = opts:%d rc:%d, want 24/%d",
			e.opts.CQLevel, e.rc.cqLevel, vp9PublicQuantizerToQIndex(24))
	}

	oldRC := e.rc
	oldOpts := e.opts
	if err := e.SetCQLevel(3); !errors.Is(err, ErrInvalidQuantizer) {
		t.Fatalf("below-min SetCQLevel err = %v, want ErrInvalidQuantizer", err)
	}
	if e.rc != oldRC || !reflect.DeepEqual(e.opts, oldOpts) {
		t.Fatal("invalid SetCQLevel mutated encoder state")
	}
	if err := e.SetCQLevel(0); err != nil {
		t.Fatalf("reset SetCQLevel: %v", err)
	}
	if e.opts.CQLevel != 0 ||
		e.rc.cqLevel != uint8(vp9PublicQuantizerToQIndex(vp9DefaultCQLevel)) {
		t.Fatalf("reset CQ state = opts:%d rc:%d, want 0/%d",
			e.opts.CQLevel, e.rc.cqLevel,
			vp9PublicQuantizerToQIndex(vp9DefaultCQLevel))
	}
}

func TestVP9EncoderSetRateControlSwitchesModeAtomically(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetRateControl(RateControlConfig{
		Mode:                RateControlCQ,
		TargetBitrateKbps:   700,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             20,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	}); err != nil {
		t.Fatalf("SetRateControl(CQ): %v", err)
	}
	if !e.opts.RateControlModeSet || e.opts.RateControlMode != RateControlCQ ||
		!e.rc.enabled || e.rc.mode != RateControlCQ ||
		e.opts.TargetBitrateKbps != 700 || e.rc.bitsPerFrame != 23333 ||
		e.rc.cqLevel != uint8(vp9PublicQuantizerToQIndex(20)) {
		t.Fatalf("CQ rate control state = opts:%+v rc:%+v, want enabled CQ 700kbps cq20",
			e.opts, e.rc)
	}
	dst := make([]byte, 65536)
	result, err := e.EncodeIntoWithResult(
		newVP9YCbCrForTest(64, 64, 96, 128, 128), dst)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult after SetRateControl: %v", err)
	}
	if result.TargetBitrateKbps != 700 || result.Dropped || len(result.Data) == 0 {
		t.Fatalf("post-SetRateControl result = kbps:%d dropped:%t bytes:%d, want 700 encoded",
			result.TargetBitrateKbps, result.Dropped, len(result.Data))
	}

	oldRC := e.rc
	oldOpts := e.opts
	oldTwoPass := e.twoPass
	if err := e.SetRateControl(RateControlConfig{
		Mode:              RateControlVBR,
		TargetBitrateKbps: 700,
		DropFrameAllowed:  true,
	}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("invalid drop SetRateControl err = %v, want ErrInvalidConfig", err)
	}
	if e.rc != oldRC || !reflect.DeepEqual(e.opts, oldOpts) ||
		!reflect.DeepEqual(e.twoPass, oldTwoPass) {
		t.Fatal("invalid SetRateControl mutated encoder state")
	}
	if err := e.SetRateControl(RateControlConfig{
		Mode:              RateControlVBR,
		TargetBitrateKbps: 700,
		MinBitrateKbps:    100,
	}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("unsupported field SetRateControl err = %v, want ErrInvalidConfig", err)
	}
	if e.rc != oldRC || !reflect.DeepEqual(e.opts, oldOpts) ||
		!reflect.DeepEqual(e.twoPass, oldTwoPass) {
		t.Fatal("unsupported-field SetRateControl mutated encoder state")
	}
}

func TestVP9EncoderSetRateControlRebuildsTwoPassPlan(t *testing.T) {
	stats := finalizedVP9TwoPassTestStats(100, 200)
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		TargetBitrateKbps:  300,
		RateControlModeSet: true,
		RateControlMode:    RateControlVBR,
		TwoPassStats:       stats,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetRateControl(RateControlConfig{
		Mode:              RateControlVBR,
		TargetBitrateKbps: 600,
	}); err != nil {
		t.Fatalf("SetRateControl two-pass VBR: %v", err)
	}
	if !e.twoPass.enabled() || e.twoPass.bitsLeft != 40000 ||
		e.twoPass.frameIndex != 0 || e.rc.bitsPerFrame != 20000 {
		t.Fatalf("two-pass state after SetRateControl = enabled:%t bitsLeft:%d frame:%d bpf:%d, want true/40000/0/20000",
			e.twoPass.enabled(), e.twoPass.bitsLeft, e.twoPass.frameIndex,
			e.rc.bitsPerFrame)
	}
	if err := e.SetRateControl(RateControlConfig{
		Mode:              RateControlCBR,
		TargetBitrateKbps: 600,
	}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetRateControl CBR with existing two-pass stats err = %v, want ErrInvalidConfig", err)
	}
}

func TestVP9EncoderSetActiveMapValidationAndCopy(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	rows := encoderMacroblockRows(height)
	cols := encoderMacroblockCols(width)
	activeMap := make([]uint8, rows*cols)
	for i := range activeMap {
		activeMap[i] = 1
	}
	activeMap[0] = 0
	if err := e.SetActiveMap(activeMap, rows, cols); err != nil {
		t.Fatalf("SetActiveMap: %v", err)
	}
	if !e.activeMapEnabled || e.activeMapMiRows != 8 || e.activeMapMiCols != 8 {
		t.Fatalf("active-map state = enabled:%t mi:%dx%d, want true 8x8",
			e.activeMapEnabled, e.activeMapMiRows, e.activeMapMiCols)
	}
	for _, idx := range []int{0, 1, 8, 9} {
		if e.activeMap[idx] != vp9ActiveMapSegmentInactive {
			t.Fatalf("expanded inactive map[%d] = %d, want %d",
				idx, e.activeMap[idx], vp9ActiveMapSegmentInactive)
		}
	}
	if e.activeMap[2] != vp9ActiveMapSegmentActive {
		t.Fatalf("expanded active map[2] = %d, want %d",
			e.activeMap[2], vp9ActiveMapSegmentActive)
	}
	activeMap[0] = 1
	if e.activeMap[0] != vp9ActiveMapSegmentInactive {
		t.Fatal("SetActiveMap kept caller slice instead of copying")
	}

	oldMap := append([]uint8(nil), e.activeMap...)
	oldRows, oldCols := e.activeMapMiRows, e.activeMapMiCols
	if err := e.SetActiveMap(activeMap, rows+1, cols); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("bad rows SetActiveMap err = %v, want ErrInvalidConfig", err)
	}
	if !e.activeMapEnabled || e.activeMapMiRows != oldRows ||
		e.activeMapMiCols != oldCols || !bytes.Equal(e.activeMap, oldMap) {
		t.Fatal("invalid SetActiveMap mutated encoder state")
	}
	if err := e.SetActiveMap(activeMap[:len(activeMap)-1], rows, cols); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("short map SetActiveMap err = %v, want ErrInvalidConfig", err)
	}
	if !e.activeMapEnabled || !bytes.Equal(e.activeMap, oldMap) {
		t.Fatal("short SetActiveMap mutated encoder state")
	}
	if err := e.SetActiveMap(nil, 0, 0); err != nil {
		t.Fatalf("disable SetActiveMap: %v", err)
	}
	if e.activeMapEnabled {
		t.Fatal("SetActiveMap(nil) did not disable active map")
	}
}

func TestVP9EncoderActiveMapInterBlocksUseSkipSegment(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	keyPacket, err := e.Encode(newVP9YCbCrForTest(width, height, 64, 128, 128))
	if err != nil {
		t.Fatalf("Encode key: %v", err)
	}
	keyHeader, _ := parseVP9EncoderHeaderForTest(t, keyPacket)
	rows := encoderMacroblockRows(height)
	cols := encoderMacroblockCols(width)
	activeMap := make([]uint8, rows*cols)
	for i := range activeMap {
		activeMap[i] = 1
	}
	activeMap[0] = 0
	if err := e.SetActiveMap(activeMap, rows, cols); err != nil {
		t.Fatalf("SetActiveMap: %v", err)
	}
	interPacket, err := e.Encode(newVP9YCbCrForTest(width, height, 180, 128, 128))
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	var br vp9dec.BitReader
	br.Init(interPacket)
	header, err := vp9dec.ReadUncompressedHeader(&br, &keyHeader,
		func(uint8) (uint32, uint32) { return width, height })
	if err != nil {
		t.Fatalf("ReadUncompressedHeader inter: %v", err)
	}
	if !header.Seg.Enabled || !header.Seg.UpdateMap || !header.Seg.UpdateData {
		t.Fatalf("active-map segmentation header = enabled:%t updateMap:%t updateData:%t, want all true",
			header.Seg.Enabled, header.Seg.UpdateMap, header.Seg.UpdateData)
	}
	if !vp9dec.SegFeatureActive(&header.Seg, int(vp9ActiveMapSegmentInactive), vp9dec.SegLvlSkip) {
		t.Fatalf("inactive segment %d missing SEG_LVL_SKIP", vp9ActiveMapSegmentInactive)
	}
	if got := header.Seg.FeatureData[vp9ActiveMapSegmentInactive][vp9dec.SegLvlAltLf]; got != -vp9dec.MaxLoopFilter {
		t.Fatalf("inactive segment alt-lf = %d, want %d",
			got, -vp9dec.MaxLoopFilter)
	}

	miCols := (width + 7) >> 3
	for _, rc := range [][2]int{{0, 0}, {0, 1}, {1, 0}, {1, 1}} {
		mi := e.miGrid[rc[0]*miCols+rc[1]]
		if mi.SegmentID != vp9ActiveMapSegmentInactive || mi.Skip != 1 ||
			mi.Mode != common.ZeroMv ||
			mi.RefFrame != [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame} {
			t.Fatalf("inactive mi[%d,%d] = seg:%d skip:%d mode:%d refs:%v, want inactive skip LAST/ZEROMV",
				rc[0], rc[1], mi.SegmentID, mi.Skip, mi.Mode, mi.RefFrame)
		}
	}
	if got := e.miGrid[2].SegmentID; got != vp9ActiveMapSegmentActive {
		t.Fatalf("active mi[0,2] segment = %d, want %d",
			got, vp9ActiveMapSegmentActive)
	}
}

func TestVP9EncoderActiveMapConstant320ChoosesTemporalPredProbs(t *testing.T) {
	const width, height = 320, 180
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:        width,
		Height:       height,
		MinQuantizer: 20,
		MaxQuantizer: 20,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	keyPacket, err := e.Encode(newVP9YCbCrForTest(width, height, 128, 128, 128))
	if err != nil {
		t.Fatalf("Encode key: %v", err)
	}
	keyHeader, _ := parseVP9EncoderHeaderForTest(t, keyPacket)
	rows := encoderMacroblockRows(height)
	cols := encoderMacroblockCols(width)
	activeMap := make([]uint8, rows*cols)
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			if (row+col)&1 == 0 {
				activeMap[row*cols+col] = 1
			}
		}
	}
	if err := e.SetActiveMap(activeMap, rows, cols); err != nil {
		t.Fatalf("SetActiveMap: %v", err)
	}
	interPacket, err := e.Encode(newVP9YCbCrForTest(width, height, 128, 128, 128))
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	var br vp9dec.BitReader
	br.Init(interPacket)
	header, err := vp9dec.ReadUncompressedHeader(&br, &keyHeader,
		func(uint8) (uint32, uint32) { return width, height })
	if err != nil {
		t.Fatalf("ReadUncompressedHeader inter: %v", err)
	}
	if !header.Seg.TemporalUpdate {
		t.Fatal("active-map constant inter did not use temporal segment prediction")
	}
	for i, prob := range header.Seg.PredProbs {
		if prob != 1 {
			t.Fatalf("active-map constant pred prob[%d] = %d, want 1", i, prob)
		}
	}
}

func TestVP9EncoderActiveMapUnchangedInactiveBlocksStayBaseSegment(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	keyPacket, err := e.Encode(newVP9YCbCrForTest(width, height, 128, 128, 128))
	if err != nil {
		t.Fatalf("Encode key: %v", err)
	}
	keyHeader, _ := parseVP9EncoderHeaderForTest(t, keyPacket)
	rows := encoderMacroblockRows(height)
	cols := encoderMacroblockCols(width)
	activeMap := make([]uint8, rows*cols)
	for i := range activeMap {
		activeMap[i] = 1
	}
	activeMap[0] = 0
	if err := e.SetActiveMap(activeMap, rows, cols); err != nil {
		t.Fatalf("SetActiveMap: %v", err)
	}
	interPacket, err := e.Encode(newVP9YCbCrForTest(width, height, 128, 128, 128))
	if err != nil {
		t.Fatalf("Encode unchanged inter: %v", err)
	}
	var br vp9dec.BitReader
	br.Init(interPacket)
	header, err := vp9dec.ReadUncompressedHeader(&br, &keyHeader,
		func(uint8) (uint32, uint32) { return width, height })
	if err != nil {
		t.Fatalf("ReadUncompressedHeader inter: %v", err)
	}
	if !header.Seg.Enabled || !header.Seg.UpdateMap || !header.Seg.UpdateData ||
		!header.Seg.TemporalUpdate {
		t.Fatalf("active-map header = enabled:%t updateMap:%t updateData:%t temporal:%t, want all true",
			header.Seg.Enabled, header.Seg.UpdateMap, header.Seg.UpdateData,
			header.Seg.TemporalUpdate)
	}

	miCols := (width + 7) >> 3
	for _, rc := range [][2]int{{0, 0}, {0, 1}, {1, 0}, {1, 1}} {
		mi := e.miGrid[rc[0]*miCols+rc[1]]
		if mi.SegmentID != vp9ActiveMapSegmentActive || mi.SegIDPredicted != 1 ||
			mi.Skip != 1 {
			t.Fatalf("unchanged inactive mi[%d,%d] = seg:%d pred:%d skip:%d, want base predicted skip",
				rc[0], rc[1], mi.SegmentID, mi.SegIDPredicted, mi.Skip)
		}
	}

	steadyPacket, err := e.Encode(newVP9YCbCrForTest(width, height, 128, 128, 128))
	if err != nil {
		t.Fatalf("Encode steady inter: %v", err)
	}
	br = vp9dec.BitReader{}
	br.Init(steadyPacket)
	steadyHeader, err := vp9dec.ReadUncompressedHeader(&br, &header,
		func(uint8) (uint32, uint32) { return width, height })
	if err != nil {
		t.Fatalf("ReadUncompressedHeader steady inter: %v", err)
	}
	if !steadyHeader.Seg.Enabled || steadyHeader.Seg.UpdateMap ||
		steadyHeader.Seg.UpdateData {
		t.Fatalf("steady active-map header = enabled:%t updateMap:%t updateData:%t, want enabled with no updates",
			steadyHeader.Seg.Enabled, steadyHeader.Seg.UpdateMap,
			steadyHeader.Seg.UpdateData)
	}
}

func TestVP9EncoderSetActiveMapDisabledByRuntimeResize(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	rows := encoderMacroblockRows(64)
	cols := encoderMacroblockCols(64)
	activeMap := make([]uint8, rows*cols)
	for i := range activeMap {
		activeMap[i] = 1
	}
	if err := e.SetActiveMap(activeMap, rows, cols); err != nil {
		t.Fatalf("SetActiveMap: %v", err)
	}
	roi := ROIMap{
		Enabled:   true,
		Rows:      (64 + 7) >> 3,
		Cols:      (64 + 7) >> 3,
		SegmentID: make([]uint8, ((64+7)>>3)*((64+7)>>3)),
	}
	roi.SegmentID[0] = 1
	roi.DeltaQuantizer[1] = -10
	if err := e.SetROIMap(&roi); err != nil {
		t.Fatalf("SetROIMap: %v", err)
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{Width: 96, Height: 80}); err != nil {
		t.Fatalf("SetRealtimeTarget resize: %v", err)
	}
	if e.activeMapEnabled || e.activeMapMiRows != 0 || e.activeMapMiCols != 0 {
		t.Fatalf("active map after resize = enabled:%t mi:%dx%d, want disabled",
			e.activeMapEnabled, e.activeMapMiRows, e.activeMapMiCols)
	}
	if e.roi.enabled || e.roi.rows != 0 || e.roi.cols != 0 {
		t.Fatalf("ROI map after resize = enabled:%t dims:%dx%d, want disabled",
			e.roi.enabled, e.roi.rows, e.roi.cols)
	}
}

func TestVP9EncoderSetRateControlBufferUpdatesBufferModel(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:               64,
		Height:              64,
		FPS:                 30,
		TargetBitrateKbps:   300,
		RateControlModeSet:  true,
		RateControlMode:     RateControlCBR,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	e.rc.bufferLevelBits = 100000
	if err := e.SetRateControlBuffer(200, 100, 150); err != nil {
		t.Fatalf("SetRateControlBuffer: %v", err)
	}
	if e.opts.BufferSizeMs != 200 || e.opts.BufferInitialSizeMs != 100 ||
		e.opts.BufferOptimalSizeMs != 150 {
		t.Fatalf("buffer opts = %d/%d/%d, want 200/100/150",
			e.opts.BufferSizeMs, e.opts.BufferInitialSizeMs,
			e.opts.BufferOptimalSizeMs)
	}
	if e.rc.bufferSizeBits != 60000 || e.rc.bufferInitialBits != 30000 ||
		e.rc.bufferOptimalBits != 45000 || e.rc.bufferLevelBits != 60000 {
		t.Fatalf("buffer bits = size:%d initial:%d optimal:%d level:%d, want 60000/30000/45000/60000",
			e.rc.bufferSizeBits, e.rc.bufferInitialBits,
			e.rc.bufferOptimalBits, e.rc.bufferLevelBits)
	}

	oldRC := e.rc
	oldOpts := e.opts
	if err := e.SetRateControlBuffer(0, 100, 150); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("invalid SetRateControlBuffer err = %v, want ErrInvalidConfig", err)
	}
	if e.rc != oldRC || !reflect.DeepEqual(e.opts, oldOpts) {
		t.Fatal("invalid SetRateControlBuffer mutated encoder state")
	}
}

func TestVP9EncoderSetRateControlBufferRequiresCBR(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetRateControlBuffer(200, 100, 150); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetRateControlBuffer without CBR err = %v, want ErrInvalidConfig", err)
	}
}

func TestVP9EncoderTemporalTwoLayerResultSequence(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:             width,
		Height:            height,
		TargetBitrateKbps: 300,
		TemporalScalability: TemporalScalabilityConfig{
			Enabled: true,
			Mode:    TemporalLayeringTwoLayers,
		},
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dst := make([]byte, 65536)
	decoder, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	wantLayer := []int{0, 1, 0, 1}
	wantTL0 := []uint8{0, 0, 1, 1}
	wantRefresh := []uint8{0xff, 0x02, 0x01, 0x02}
	wantSync := []bool{false, true, false, false}
	var prevHeader *vp9dec.UncompressedHeader
	for i := range wantLayer {
		src := newVP9YCbCrForTest(width, height, byte(80+i*20), 128, 128)
		result, err := e.EncodeIntoWithResult(src, dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult[%d]: %v", i, err)
		}
		packet := append([]byte(nil), result.Data...)
		if len(packet) == 0 || result.SizeBytes != len(packet) {
			t.Fatalf("result[%d] size = %d data=%d", i, result.SizeBytes, len(packet))
		}
		if got := result.TemporalLayerID; got != wantLayer[i] {
			t.Fatalf("frame %d temporal layer = %d, want %d", i, got, wantLayer[i])
		}
		if got := result.TemporalLayerCount; got != 2 {
			t.Fatalf("frame %d temporal layer count = %d, want 2", i, got)
		}
		if got := result.TL0PICIDX; got != wantTL0[i] {
			t.Fatalf("frame %d TL0PICIDX = %d, want %d", i, got, wantTL0[i])
		}
		if got, want := result.TemporalLayerSync, wantSync[i]; got != want {
			t.Fatalf("frame %d temporal sync = %t, want %t", i, got, want)
		}
		var br vp9dec.BitReader
		br.Init(packet)
		header, err := vp9dec.ReadUncompressedHeader(&br, prevHeader,
			func(uint8) (uint32, uint32) { return width, height })
		if err != nil {
			t.Fatalf("ReadUncompressedHeader[%d]: %v", i, err)
		}
		prevHeader = &header
		if got := result.RefreshFrameFlags; got != wantRefresh[i] {
			t.Fatalf("frame %d result refresh flags = %#x, want %#x", i, got, wantRefresh[i])
		}
		if got := header.RefreshFrameFlags; got != wantRefresh[i] {
			t.Fatalf("frame %d parsed header = %+v refresh flags = %#x, want %#x",
				i, header, got, wantRefresh[i])
		}
		if got, want := result.KeyFrame, i == 0; got != want {
			t.Fatalf("frame %d keyframe = %t, want %t", i, got, want)
		}
		if !result.ShowFrame || !header.ShowFrame {
			t.Fatalf("frame %d ShowFrame result=%t header=%t, want visible",
				i, result.ShowFrame, header.ShowFrame)
		}
		if err := decoder.Decode(packet); err != nil {
			t.Fatalf("Decode[%d]: %v", i, err)
		}
		if _, ok := decoder.NextFrame(); !ok {
			t.Fatalf("NextFrame[%d] returned !ok", i)
		}
		if i == 1 {
			desc := result.RTPPayloadDescriptor()
			payload, err := PackVP9RTPPayload(desc, packet)
			if err != nil {
				t.Fatalf("PackVP9RTPPayload: %v", err)
			}
			gotDesc, gotPacket, err := ParseVP9RTPPayloadDescriptor(payload)
			if err != nil {
				t.Fatalf("ParseVP9RTPPayloadDescriptor: %v", err)
			}
			if !bytes.Equal(gotPacket, packet) {
				t.Fatalf("RTP payload packet changed")
			}
			if !gotDesc.LayerIndicesPresent || gotDesc.TemporalID != 1 ||
				gotDesc.TL0PICIDX != 0 || !gotDesc.SwitchingUpPoint ||
				!gotDesc.InterPicturePredicted {
				t.Fatalf("RTP descriptor = %+v, want temporal layer 1 sync", gotDesc)
			}
		}
	}
}

func TestVP9EncoderSetTemporalScalabilityUpdatesResultSequence(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:             width,
		Height:            height,
		TargetBitrateKbps: 300,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetTemporalScalability(TemporalScalabilityConfig{
		Enabled: true,
		Mode:    TemporalLayeringTwoLayers,
	}); err != nil {
		t.Fatalf("SetTemporalScalability: %v", err)
	}
	if got := e.opts.TemporalScalability.LayerTargetBitrateKbps; got[0] != 180 || got[1] != 300 {
		t.Fatalf("derived VP9 temporal bitrates = %v, want [180 300 ...]", got)
	}

	dst := make([]byte, 65536)
	for i, wantLayer := range []int{0, 1} {
		result, err := e.EncodeIntoWithResult(
			newVP9YCbCrForTest(width, height, byte(90+i*20), 128, 128), dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult[%d]: %v", i, err)
		}
		if result.TemporalLayerID != wantLayer || result.TemporalLayerCount != 2 {
			t.Fatalf("frame %d temporal = id:%d count:%d, want %d/2",
				i, result.TemporalLayerID, result.TemporalLayerCount, wantLayer)
		}
	}

	if err := e.SetTemporalLayerID(1); err != nil {
		t.Fatalf("SetTemporalLayerID: %v", err)
	}
	result, err := e.EncodeIntoWithResult(
		newVP9YCbCrForTest(width, height, 140, 128, 128), dst)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult override: %v", err)
	}
	if result.TemporalLayerID != 1 {
		t.Fatalf("override temporal layer = %d, want 1", result.TemporalLayerID)
	}
	if err := e.SetTemporalScalability(TemporalScalabilityConfig{}); err != nil {
		t.Fatalf("disable SetTemporalScalability: %v", err)
	}
	result, err = e.EncodeIntoWithResult(
		newVP9YCbCrForTest(width, height, 160, 128, 128), dst)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult disabled: %v", err)
	}
	if result.TemporalLayerID != 0 || result.TemporalLayerCount != 1 {
		t.Fatalf("disabled temporal = id:%d count:%d, want 0/1",
			result.TemporalLayerID, result.TemporalLayerCount)
	}
}

func TestVP9EncoderSpatialScalabilityResultAndRTPDescriptor(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:  width,
		Height: height,
		SpatialScalability: VP9SpatialScalabilityConfig{
			Enabled:                    true,
			LayerCount:                 2,
			LayerID:                    1,
			InterLayerDependency:       true,
			NotRefForUpperSpatialLayer: true,
			ResolutionPresent:          true,
			Width:                      [VP9RTPMaxSpatialLayers]uint16{32, width},
			Height:                     [VP9RTPMaxSpatialLayers]uint16{32, height},
		},
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dst := make([]byte, 65536)
	result, err := e.EncodeIntoWithResult(newVP9YCbCrForTest(width, height,
		100, 128, 128), dst)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult: %v", err)
	}
	if result.SpatialLayerID != 1 || result.SpatialLayerCount != 2 ||
		!result.InterLayerDependency || !result.NotRefForUpperSpatialLayer ||
		!result.ScalabilityStructurePresent {
		t.Fatalf("spatial result = %+v, want layer 1/2 dependency with SS", result)
	}
	ss := result.SpatialScalabilityStructure
	if ss.SpatialLayerCount != 2 || !ss.ResolutionPresent ||
		ss.Width[0] != 32 || ss.Height[0] != 32 ||
		ss.Width[1] != width || ss.Height[1] != height {
		t.Fatalf("spatial scalability structure = %+v", ss)
	}

	desc := result.RTPPayloadDescriptor()
	if !desc.LayerIndicesPresent || desc.TemporalID != 0 ||
		desc.SpatialID != 1 || !desc.InterLayerDependency ||
		!desc.NotRefForUpperSpatialLayer ||
		!desc.ScalabilityStructurePresent {
		t.Fatalf("RTP descriptor = %+v, want spatial layer descriptor", desc)
	}
	if desc.ScalabilityStructure.SpatialLayerCount != 2 ||
		desc.ScalabilityStructure.Width[1] != width ||
		desc.ScalabilityStructure.Height[1] != height {
		t.Fatalf("RTP scalability structure = %+v", desc.ScalabilityStructure)
	}
	payload, err := PackVP9RTPPayload(desc, result.Data)
	if err != nil {
		t.Fatalf("PackVP9RTPPayload: %v", err)
	}
	gotDesc, gotPacket, err := ParseVP9RTPPayloadDescriptor(payload)
	if err != nil {
		t.Fatalf("ParseVP9RTPPayloadDescriptor: %v", err)
	}
	if !bytes.Equal(gotPacket, result.Data) {
		t.Fatal("RTP payload packet changed")
	}
	if gotDesc.SpatialID != 1 || !gotDesc.InterLayerDependency ||
		!gotDesc.NotRefForUpperSpatialLayer ||
		gotDesc.ScalabilityStructure.SpatialLayerCount != 2 {
		t.Fatalf("parsed RTP descriptor = %+v", gotDesc)
	}
}

func TestVP9EncoderSetSpatialScalabilityUpdatesResultMetadata(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetSpatialScalability(VP9SpatialScalabilityConfig{
		Enabled:    true,
		LayerCount: 3,
		LayerID:    2,
	}); err != nil {
		t.Fatalf("SetSpatialScalability: %v", err)
	}
	dst := make([]byte, 65536)
	result, err := e.EncodeIntoWithResult(newVP9YCbCrForTest(width, height,
		120, 128, 128), dst)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult layer 2: %v", err)
	}
	if result.SpatialLayerID != 2 || result.SpatialLayerCount != 3 ||
		result.ScalabilityStructurePresent {
		t.Fatalf("spatial result layer 2 = %+v, want 2/3 without SS", result)
	}
	if desc := result.RTPPayloadDescriptor(); !desc.LayerIndicesPresent ||
		desc.SpatialID != 2 || desc.ScalabilityStructurePresent {
		t.Fatalf("RTP descriptor layer 2 = %+v", desc)
	}

	if err := e.SetSpatialLayerID(1); err != nil {
		t.Fatalf("SetSpatialLayerID(1): %v", err)
	}
	result, err = e.EncodeIntoWithResult(newVP9YCbCrForTest(width, height,
		140, 128, 128), dst)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult layer 1: %v", err)
	}
	if result.SpatialLayerID != 1 || result.SpatialLayerCount != 3 {
		t.Fatalf("spatial result layer 1 = %+v, want 1/3", result)
	}
	if err := e.SetSpatialScalability(VP9SpatialScalabilityConfig{}); err != nil {
		t.Fatalf("disable SetSpatialScalability: %v", err)
	}
	result, err = e.EncodeIntoWithResult(newVP9YCbCrForTest(width, height,
		160, 128, 128), dst)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult disabled: %v", err)
	}
	if result.SpatialLayerID != 0 || result.SpatialLayerCount != 1 ||
		result.RTPPayloadDescriptor().LayerIndicesPresent {
		t.Fatalf("disabled spatial result = %+v", result)
	}
}

func TestVP9EncoderSetSpatialScalabilityValidation(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetSpatialLayerID(1); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetSpatialLayerID disabled err = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetSpatialLayerID(0); err != nil {
		t.Fatalf("SetSpatialLayerID disabled base: %v", err)
	}
	if err := e.SetSpatialScalability(VP9SpatialScalabilityConfig{
		Enabled:    true,
		LayerCount: 2,
		LayerID:    2,
	}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetSpatialScalability invalid err = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetSpatialScalability(VP9SpatialScalabilityConfig{
		Enabled:           true,
		LayerCount:        2,
		LayerID:           1,
		ResolutionPresent: true,
		Width:             [VP9RTPMaxSpatialLayers]uint16{32, 32},
		Height:            [VP9RTPMaxSpatialLayers]uint16{32, 32},
	}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetSpatialScalability mismatched dimensions err = %v, want ErrInvalidConfig", err)
	}
}

func TestVP9EncoderSetRealtimeTargetClosed(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{BitrateKbps: 1200}); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetRealtimeTarget after Close err = %v, want ErrClosed", err)
	}
	if err := e.SetDeadline(DeadlineRealtime); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetDeadline after Close err = %v, want ErrClosed", err)
	}
	if err := e.SetCPUUsed(8); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetCPUUsed after Close err = %v, want ErrClosed", err)
	}
	if err := e.SetBitrateKbps(900); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetBitrateKbps after Close err = %v, want ErrClosed", err)
	}
	if err := e.SetCQLevel(20); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetCQLevel after Close err = %v, want ErrClosed", err)
	}
	if err := e.SetRateControl(RateControlConfig{Mode: RateControlVBR, TargetBitrateKbps: 900}); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetRateControl after Close err = %v, want ErrClosed", err)
	}
	if err := e.SetActiveMap([]uint8{1}, 1, 1); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetActiveMap after Close err = %v, want ErrClosed", err)
	}
	if err := e.SetROIMap(&ROIMap{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetROIMap after Close err = %v, want ErrClosed", err)
	}
	if err := e.SetRateControlBuffer(200, 100, 150); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetRateControlBuffer after Close err = %v, want ErrClosed", err)
	}
	if err := e.SetKeyFrameInterval(2); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetKeyFrameInterval after Close err = %v, want ErrClosed", err)
	}
	if err := e.SetARNR(5, 6, 3); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetARNR after Close err = %v, want ErrClosed", err)
	}
	if err := e.SetScreenContentMode(1); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetScreenContentMode after Close err = %v, want ErrClosed", err)
	}
	if err := e.SetSharpness(3); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetSharpness after Close err = %v, want ErrClosed", err)
	}
	if err := e.SetTemporalScalability(TemporalScalabilityConfig{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetTemporalScalability after Close err = %v, want ErrClosed", err)
	}
	if err := e.SetTemporalLayerID(0); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetTemporalLayerID after Close err = %v, want ErrClosed", err)
	}
	if err := e.SetSpatialScalability(VP9SpatialScalabilityConfig{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetSpatialScalability after Close err = %v, want ErrClosed", err)
	}
	if err := e.SetSpatialLayerID(0); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetSpatialLayerID after Close err = %v, want ErrClosed", err)
	}
	var nilEnc *VP9Encoder
	if err := nilEnc.SetRealtimeTarget(RealtimeTarget{BitrateKbps: 1200}); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetRealtimeTarget on nil encoder err = %v, want ErrClosed", err)
	}
	if err := nilEnc.SetDeadline(DeadlineRealtime); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetDeadline on nil encoder err = %v, want ErrClosed", err)
	}
	if err := nilEnc.SetCPUUsed(8); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetCPUUsed on nil encoder err = %v, want ErrClosed", err)
	}
	if err := nilEnc.SetBitrateKbps(900); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetBitrateKbps on nil encoder err = %v, want ErrClosed", err)
	}
	if err := nilEnc.SetCQLevel(20); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetCQLevel on nil encoder err = %v, want ErrClosed", err)
	}
	if err := nilEnc.SetRateControl(RateControlConfig{Mode: RateControlVBR, TargetBitrateKbps: 900}); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetRateControl on nil encoder err = %v, want ErrClosed", err)
	}
	if err := nilEnc.SetActiveMap([]uint8{1}, 1, 1); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetActiveMap on nil encoder err = %v, want ErrClosed", err)
	}
	if err := nilEnc.SetROIMap(&ROIMap{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetROIMap on nil encoder err = %v, want ErrClosed", err)
	}
	if err := nilEnc.SetRateControlBuffer(200, 100, 150); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetRateControlBuffer on nil encoder err = %v, want ErrClosed", err)
	}
	if err := nilEnc.SetKeyFrameInterval(2); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetKeyFrameInterval on nil encoder err = %v, want ErrClosed", err)
	}
	if err := nilEnc.SetARNR(5, 6, 3); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetARNR on nil encoder err = %v, want ErrClosed", err)
	}
	if err := nilEnc.SetScreenContentMode(1); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetScreenContentMode on nil encoder err = %v, want ErrClosed", err)
	}
	if err := nilEnc.SetSharpness(3); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetSharpness on nil encoder err = %v, want ErrClosed", err)
	}
	if err := nilEnc.SetTemporalScalability(TemporalScalabilityConfig{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetTemporalScalability on nil encoder err = %v, want ErrClosed", err)
	}
	if err := nilEnc.SetTemporalLayerID(0); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetTemporalLayerID on nil encoder err = %v, want ErrClosed", err)
	}
	if err := nilEnc.SetSpatialScalability(VP9SpatialScalabilityConfig{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetSpatialScalability on nil encoder err = %v, want ErrClosed", err)
	}
	if err := nilEnc.SetSpatialLayerID(0); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetSpatialLayerID on nil encoder err = %v, want ErrClosed", err)
	}
}

func TestVP9EncoderEncodeIntoWithFlagsNoUpdateLast(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := newVP9YCbCrForTest(width, height, 64, 128, 128)
	if _, err := e.Encode(keySrc); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	keyRefY := e.refFrames[vp9LastRefSlot].img.Y[0]
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
	if h.RefreshFrameFlags != 0x06 {
		t.Fatalf("RefreshFrameFlags = %#x, want GOLDEN|ALTREF", h.RefreshFrameFlags)
	}
	if !e.refFrames[0].valid {
		t.Fatal("LAST ref became invalid after no-update-LAST")
	}
	if got := e.refFrames[0].img.Y[0]; got != keyRefY {
		t.Fatalf("LAST ref Y[0] = %d, want prior keyframe value %d", got, keyRefY)
	}
	for _, slot := range []int{vp9GoldenRefSlot, vp9AltRefSlot} {
		if got := e.refFrames[slot].img.Y[0]; got == keyRefY {
			t.Fatalf("ref slot %d Y[0] still has keyframe value %d", slot, got)
		}
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
	keyRefY := e.refFrames[vp9LastRefSlot].img.Y[0]

	interSrc := newVP9YCbCrForTest(width, height, 196, 96, 224)
	packet, err := e.EncodeWithFlags(interSrc, EncodeForceGoldenFrame|EncodeNoUpdateLast)
	if err != nil {
		t.Fatalf("EncodeWithFlags force GF/no-update-LAST: %v", err)
	}
	info, err := PeekVP9StreamInfo(packet)
	if err != nil {
		t.Fatalf("PeekVP9StreamInfo: %v", err)
	}
	if info.RefreshFrameFlags != 0x06 {
		t.Fatalf("RefreshFrameFlags = %#x, want GOLDEN|ALTREF", info.RefreshFrameFlags)
	}
	if got := e.refFrames[vp9LastRefSlot].img.Y[0]; got != keyRefY {
		t.Fatalf("LAST ref Y[0] = %d, want prior keyframe value %d", got, keyRefY)
	}
	if got := e.refFrames[vp9GoldenRefSlot].img.Y[0]; got == keyRefY {
		t.Fatalf("GOLDEN ref Y[0] still has keyframe value %d", got)
	}
	if got := e.refFrames[vp9AltRefSlot].img.Y[0]; got == keyRefY {
		t.Fatalf("ALTREF ref Y[0] still has keyframe value %d", got)
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
	if got := d.miGrid[0]; got.RefFrame[0] != vp9dec.GoldenFrame {
		t.Fatalf("top-left inter = ref %d mode %d mv %+v, want GOLDEN",
			got.RefFrame[0], got.Mode, got.Mv[0])
	}
}

func TestVP9EncoderEncodeIntoWithFlagsNoReferenceAllStaysInterIntra(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := newVP9YCbCrForTest(width, height, 72, 128, 128)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := newVP9YCbCrForTest(width, height, 144, 96, 224)
	inter, err := e.EncodeWithFlags(interSrc,
		EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef)
	if err != nil {
		t.Fatalf("Encode no-reference-all inter: %v", err)
	}
	header, _ := parseVP9EncoderHeaderForTest(t, inter)
	if header.FrameType != common.InterFrame || header.IntraOnly {
		t.Fatalf("no-reference-all header frame_type=%d intra_only=%t, want inter/intra-coded blocks",
			header.FrameType, header.IntraOnly)
	}
	if header.RefreshFrameFlags != 1<<vp9LastRefSlot {
		t.Fatalf("no-reference-all refresh = %#x, want LAST refresh",
			header.RefreshFrameFlags)
	}
	if header.InterpFilter != vp9dec.InterpSwitchable {
		t.Fatalf("no-reference-all interp filter = %d, want switchable",
			header.InterpFilter)
	}

	d := decodeVP9KeyInterForTest(t, key, inter)
	if len(d.miGrid) == 0 {
		t.Fatal("decoder MI grid is empty after no-reference-all inter")
	}
	if got := d.miGrid[0]; got.RefFrame != [2]int8{vp9dec.IntraFrame, vp9dec.NoRefFrame} {
		t.Fatalf("top-left block ref = %v mode=%d, want intra block inside inter frame",
			got.RefFrame, got.Mode)
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
	if got := d.miGrid[0]; got.RefFrame[0] != vp9dec.AltrefFrame {
		t.Fatalf("top-left inter = ref %d mode %d mv %+v, want ALTREF reference",
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
	assertVP9FilledFrameWithin(t, frame, width, height, 91, 143, 37, 1)
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
	assertVP9FilledFrameWithin(t, frame, width, height, 188, 96, 224, 4)
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
	assertVP9FilledFrameWithin(t, frame, width, height, 91, 143, 37, 1)
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
	allocs := testing.AllocsPerRun(vp9EncoderKeyframeAllocRuns, func() {
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
	assertVP9FilledFrameWithin(t, frame, width, height, 83, 141, 209, 1)
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

func TestVP9BlockSSEMatchesScalar(t *testing.T) {
	const stride = 80
	src := make([]byte, stride*80)
	ref := make([]byte, stride*80)
	for i := range src {
		src[i] = byte((i*13 + i/5) & 0xff)
		ref[i] = byte((i*23 + 19) & 0xff)
	}

	got := vp9BlockSSE(src, stride, ref, stride, 3, 5, 7, 11, 32, 16)
	var want uint64
	for y := 0; y < 16; y++ {
		srcRow := src[(5+y)*stride+3:]
		refRow := ref[(11+y)*stride+7:]
		for x := 0; x < 32; x++ {
			diff := int(srcRow[x]) - int(refRow[x])
			want += uint64(diff * diff)
		}
	}
	if got != want {
		t.Fatalf("SSE = %d, want %d", got, want)
	}
}

var vp9BlockSSEBenchmarkSink uint64

func BenchmarkVP9BlockSSE64x64(b *testing.B) {
	const stride = 96
	src := make([]byte, stride*96)
	ref := make([]byte, stride*96)
	for i := range src {
		src[i] = byte((i*13 + i/5) & 0xff)
		ref[i] = byte((i*23 + 19) & 0xff)
	}

	b.ReportAllocs()
	var sum uint64
	for b.Loop() {
		sum += vp9BlockSSE(src, stride, ref, stride, 3, 5, 7, 11, 64, 64)
	}
	vp9BlockSSEBenchmarkSink = sum
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
	if interHeader.InterRef.SignBias != [3]uint8{} {
		t.Errorf("SignBias = %v, want [0 0 0]", interHeader.InterRef.SignBias)
	}
	if !interHeader.AllowHighPrecisionMv {
		t.Error("AllowHighPrecisionMv = false, want true")
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
		InterpFilter:         interHeader.InterpFilter,
		AllowHighPrecisionMv: interHeader.AllowHighPrecisionMv,
		CompoundRefAllowed:   false,
	})
	if cr.HasError() {
		t.Fatal("compressed header reader reported over-read")
	}
	if out.TxMode != common.TxModeSelect {
		t.Errorf("TxMode = %d, want TxModeSelect", out.TxMode)
	}
	if out.ReferenceMode != vp9dec.SingleReference {
		t.Errorf("ReferenceMode = %d, want SingleReference", out.ReferenceMode)
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
	if got.SbType != common.Block8x8 {
		t.Fatalf("top-left SbType = %d, want oracle-style Block8x8", got.SbType)
	}
	if got.TxSize != common.Tx8x8 {
		t.Fatalf("top-left TxSize = %d, want oracle-style Tx8x8", got.TxSize)
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
// Confirms the SB walker emits oracle-shaped 32x32 keyframe leaves
// across both superblocks.
func TestVP9EncoderKeyframeMultiSb(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 128, Height: 64})
	img := newVP9YCbCrForTest(128, 64, 128, 128, 128)
	got, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	const miRows, miCols = 8, 16
	grid := decodeVP9PacketMiGridForOracleTest(t, got)
	if len(grid) != miRows*miCols {
		t.Fatalf("decoded mi grid len = %d, want %d", len(grid), miRows*miCols)
	}
	leaves := 0
	for miRow := 0; miRow < miRows; miRow += 4 {
		for miCol := 0; miCol < miCols; miCol += 4 {
			mi := grid[miRow*miCols+miCol]
			if mi.SbType != common.Block32x32 || mi.TxSize != common.Tx16x16 ||
				mi.Mode != common.DcPred || mi.Skip != 1 ||
				mi.RefFrame[0] != vp9dec.IntraFrame {
				t.Fatalf("leaf at (%d,%d) = %+v, want Block32x32/Tx16/DcPred/skip intra",
					miRow, miCol, mi)
			}
			leaves++
		}
	}
	if leaves != 8 {
		t.Errorf("decoded %d Block32x32 leaves, want 8", leaves)
	}
}

func TestVP9EncoderKeyframeThreeMiEdgeUsesBlock32x32(t *testing.T) {
	const width, height = 320, 180
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width:        width,
		Height:       height,
		MinQuantizer: 20,
		MaxQuantizer: 20,
	})
	img := newVP9YCbCrForTest(width, height, 128, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	grid := decodeVP9PacketMiGridForOracleTest(t, packet)
	miRows := (height + 7) >> 3
	miCols := (width + 7) >> 3
	if got, want := len(grid), miRows*miCols; got != want {
		t.Fatalf("decoded mi grid len = %d, want %d", got, want)
	}
	for miCol := 0; miCol < miCols; miCol += 4 {
		mi := grid[20*miCols+miCol]
		if mi.SbType != common.Block32x32 || mi.TxSize != common.Tx16x16 ||
			mi.Mode != common.DcPred || mi.Skip != 1 {
			t.Fatalf("bottom 3-mi edge leaf at col %d = %+v, want Block32x32/Tx16/DcPred/skip",
				miCol, mi)
		}
	}
}

func TestVP9EncoderFixedQNonNeutralKeyframeThreeMiEdgeUsesSquareBlocks(t *testing.T) {
	const width, height = 320, 180
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width:        width,
		Height:       height,
		MinQuantizer: 20,
		MaxQuantizer: 20,
	})
	img := newVP9YCbCrForTest(width, height, 96, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	grid := decodeVP9PacketMiGridForOracleTest(t, packet)
	miRows := (height + 7) >> 3
	miCols := (width + 7) >> 3
	if got, want := len(grid), miRows*miCols; got != want {
		t.Fatalf("decoded mi grid len = %d, want %d", got, want)
	}
	for miCol := 0; miCol < miCols; miCol += 2 {
		mi := grid[20*miCols+miCol]
		if mi.SbType != common.Block16x16 || mi.TxSize != common.Tx16x16 ||
			mi.Skip != 1 {
			t.Fatalf("bottom 3-mi edge 16x16 leaf at col %d = %+v, want Block16x16/Tx16/skip",
				miCol, mi)
		}
	}
	for miCol := 0; miCol < miCols; miCol++ {
		mi := grid[22*miCols+miCol]
		if mi.SbType != common.Block8x8 || mi.TxSize != common.Tx8x8 ||
			mi.Skip != 1 {
			t.Fatalf("bottom 1-mi edge leaf at col %d = %+v, want Block8x8/Tx8/skip",
				miCol, mi)
		}
	}
}

func TestVP9EncoderInterOneMiEdgeKeepsBlock64x64(t *testing.T) {
	const width, height = 320, 180
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width:        width,
		Height:       height,
		MinQuantizer: 20,
		MaxQuantizer: 20,
	})
	img := newVP9YCbCrForTest(width, height, 128, 128, 128)
	keyPacket, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode inter frame: %v", err)
	}
	grid := decodeVP9TwoFrameInterMiGridForOracleTest(t, keyPacket, packet)
	miRows := (height + 7) >> 3
	miCols := (width + 7) >> 3
	if got, want := len(grid), miRows*miCols; got != want {
		t.Fatalf("decoded mi grid len = %d, want %d", got, want)
	}
	for miCol := 0; miCol < miCols; miCol += 8 {
		mi := grid[16*miCols+miCol]
		if mi.SbType != common.Block64x64 || mi.Mode != common.NearestMv ||
			mi.Skip != 1 || mi.RefFrame[0] != vp9dec.LastFrame {
			t.Fatalf("bottom one-mi-clipped inter root at col %d = %+v, want Block64x64/NearestMv/LAST/skip",
				miCol, mi)
		}
	}
}

func TestVP9EncoderInterFourMiEdgeUsesBlock32x32(t *testing.T) {
	const width, height = 640, 480
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width:        width,
		Height:       height,
		MinQuantizer: 20,
		MaxQuantizer: 20,
	})
	img := newVP9YCbCrForTest(width, height, 128, 128, 128)
	keyPacket, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode inter frame: %v", err)
	}
	grid := decodeVP9TwoFrameInterMiGridForOracleTest(t, keyPacket, packet)
	miRows := (height + 7) >> 3
	miCols := (width + 7) >> 3
	if got, want := len(grid), miRows*miCols; got != want {
		t.Fatalf("decoded mi grid len = %d, want %d", got, want)
	}
	for miCol := 0; miCol < miCols; miCol += 4 {
		mi := grid[56*miCols+miCol]
		if mi.SbType != common.Block32x32 || mi.Mode != common.NearestMv ||
			mi.Skip != 1 || mi.RefFrame[0] != vp9dec.LastFrame {
			t.Fatalf("bottom four-mi inter edge at col %d = %+v, want Block32x32/NearestMv/LAST/skip",
				miCol, mi)
		}
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

	key := newVP9KeyframeModeTestState(e, img, 128, 64)
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

	key := newVP9KeyframeModeTestState(e, img, width, height)
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

	key := newVP9KeyframeModeTestState(e, img, 32, 16)
	mi := vp9dec.NeighborMi{SbType: common.Block16x16, TxSize: common.Tx16x16}
	tile := vp9dec.TileBounds{MiRowStart: 0, MiRowEnd: 2, MiColStart: 0, MiColEnd: 4}
	got := e.pickVP9KeyframeMode(key, tile, 2, 4, 0, 2, common.Block16x16, &mi)
	if got != common.HPred {
		t.Errorf("mode = %d, want HPred", got)
	}
}

func newVP9KeyframeModeTestState(e *VP9Encoder, img *image.YCbCr, width, height int) *vp9KeyframeEncodeState {
	vp9dec.ResetFrameContext(&e.fc)
	hdr := &vp9dec.UncompressedHeader{Width: uint32(width), Height: uint32(height)}
	dq := &vp9dec.DequantTables{}
	var seg vp9dec.SegmentationParams
	vp9dec.SetupSegmentationDequant(&seg, vp9dec.SetupSegmentationDequantArgs{
		BaseQindex: e.vp9EncoderModeDecisionQIndex(),
		BitDepth:   vp9dec.Bits8,
	}, dq)
	return &vp9KeyframeEncodeState{img: img, hdr: hdr, dq: dq}
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
		[2]int16{4, 4}, 0, coeffs[:]) {
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

func TestVP9EncoderKeyframeKeepsOracleDcUvModeWithHorizontalChroma(t *testing.T) {
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
	if got != common.DcPred {
		t.Errorf("UV mode = %d, want DcPred", got)
	}
}

func TestVP9EncoderKeyframeKeepsOracleDcUvModeForWholeBlockChroma(t *testing.T) {
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
	if got != common.DcPred {
		t.Fatalf("UV mode = %d, want DcPred", got)
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
	assertVP9FilledFrameWithin(t, frame, width, height, 91, 143, 37, 1)
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
	if got, want := len(e.vp9CountWorkers), 4; got != want {
		t.Fatalf("VP9 count workers = %d, want %d", got, want)
	}
	if e.vp9TilePool == nil {
		t.Fatal("VP9 tile worker pool was not initialized")
	}
	if got, want := e.vp9TilePool.workerCount, 4; got != want {
		t.Fatalf("VP9 tile worker count = %d, want %d", got, want)
	}
	for i := range e.vp9TilePool.encodeJobs {
		if e.vp9TilePool.encodeJobs[i].size == 0 {
			t.Fatalf("VP9 tile worker job %d wrote zero bytes", i)
		}
	}
	if len(e.vp9CountWorkers[0].miGrid) == 0 || len(e.miGrid) == 0 {
		t.Fatal("VP9 threaded count worker miGrid was not initialized")
	}
	if &e.vp9CountWorkers[0].miGrid[0] == &e.miGrid[0] {
		t.Fatal("VP9 threaded count worker aliases encoder miGrid")
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
	assertVP9FilledFrameWithin(t, frame, width, height, 82, 123, 211, 1)
}

func TestVP9EncoderNoiseSensitivityUsesSerialTileWorkers(t *testing.T) {
	const width, height = 1280, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:            width,
		Height:           height,
		Threads:          4,
		NoiseSensitivity: 3,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if e.vp9TilePool != nil {
		t.Fatal("denoiser initialized VP9 tile worker pool")
	}
	img := newVP9YCbCrForTest(width, height, 82, 123, 211)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	h, _ := parseVP9EncoderHeaderForTest(t, packet)
	if h.Tile.Log2TileCols != 2 {
		t.Fatalf("Log2TileCols = %d, want 2 for Threads=4",
			h.Tile.Log2TileCols)
	}
	if e.vp9TilePool != nil {
		t.Fatal("denoiser encode created VP9 tile worker pool")
	}
	if len(e.vp9CountWorkers) != 0 {
		t.Fatalf("denoiser count workers = %d, want 0", len(e.vp9CountWorkers))
	}
}

func TestVP9EncoderSetNoiseSensitivityClosesTilePool(t *testing.T) {
	const width, height = 1280, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:   width,
		Height:  height,
		Threads: 4,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if e.vp9TilePool == nil {
		t.Fatal("VP9 tile worker pool was not initialized")
	}
	if err := e.SetNoiseSensitivity(3); err != nil {
		t.Fatalf("SetNoiseSensitivity: %v", err)
	}
	if e.vp9TilePool != nil || len(e.vp9CountWorkers) != 0 ||
		len(e.vp9CountCounts) != 0 || len(e.vp9CountJobs) != 0 {
		t.Fatal("SetNoiseSensitivity left VP9 tile worker state installed")
	}
}

func TestVP9EncoderThreadsHintDeterministicAcrossRuns(t *testing.T) {
	const width, height = 1024, 64
	opts := VP9EncoderOptions{
		Width:   width,
		Height:  height,
		Threads: 4,
	}
	a, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder(a): %v", err)
	}
	b, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder(b): %v", err)
	}
	dstA := make([]byte, 1<<20)
	dstB := make([]byte, 1<<20)
	for frame := range 2 {
		src := newVP9PanningYCbCrForRateTest(width, height, frame)
		nA, err := a.EncodeInto(src, dstA)
		if err != nil {
			t.Fatalf("a EncodeInto[%d]: %v", frame, err)
		}
		nB, err := b.EncodeInto(src, dstB)
		if err != nil {
			t.Fatalf("b EncodeInto[%d]: %v", frame, err)
		}
		if !bytes.Equal(dstA[:nA], dstB[:nB]) {
			t.Fatalf("threaded VP9 packet %d differs across runs: %d/%d bytes",
				frame, nA, nB)
		}
	}
}

func TestVP9EncoderLog2TileRowsRowOnlyMatchesSerial(t *testing.T) {
	const width, height = 64, 128
	serial, err := NewVP9Encoder(VP9EncoderOptions{
		Width:        width,
		Height:       height,
		Threads:      1,
		Log2TileRows: 1,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder(serial): %v", err)
	}
	threaded, err := NewVP9Encoder(VP9EncoderOptions{
		Width:        width,
		Height:       height,
		Threads:      2,
		Log2TileRows: 1,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder(threaded): %v", err)
	}

	dstSerial := make([]byte, 1<<20)
	dstThreaded := make([]byte, 1<<20)
	for frame := range 3 {
		src := newVP9PanningYCbCrForRateTest(width, height, frame)
		nSerial, err := serial.EncodeInto(src, dstSerial)
		if err != nil {
			t.Fatalf("serial EncodeInto[%d]: %v", frame, err)
		}
		nThreaded, err := threaded.EncodeInto(src, dstThreaded)
		if err != nil {
			t.Fatalf("threaded EncodeInto[%d]: %v", frame, err)
		}
		if !bytes.Equal(dstSerial[:nSerial], dstThreaded[:nThreaded]) {
			t.Fatalf("tile-row threaded packet %d differs from serial: %d/%d bytes",
				frame, nThreaded, nSerial)
		}
		if frame == 0 {
			d, err := NewVP9Decoder(VP9DecoderOptions{})
			if err != nil {
				t.Fatalf("NewVP9Decoder: %v", err)
			}
			if err := d.Decode(dstSerial[:nSerial]); err != nil {
				t.Fatalf("Decode serial tile-row keyframe: %v", err)
			}
			if _, ok := d.NextFrame(); !ok {
				t.Fatal("NextFrame returned !ok after serial tile-row keyframe")
			}
		}
	}
	if threaded.vp9TilePool != nil {
		t.Fatalf("row-only tile configuration initialized unsafe pool with %d workers",
			threaded.vp9TilePool.workerCount)
	}
}

func TestVP9EncoderLog2TileRowsWithTileColumnsMatchesSerial(t *testing.T) {
	const width, height = 4104, 128
	serial, err := NewVP9Encoder(VP9EncoderOptions{
		Width:        width,
		Height:       height,
		Threads:      1,
		Log2TileRows: 1,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder(serial): %v", err)
	}
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:        width,
		Height:       height,
		Threads:      2,
		Log2TileRows: 1,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	src := newVP9YCbCrForTest(width, height, 82, 123, 211)
	wantPacket, err := serial.Encode(src)
	if err != nil {
		t.Fatalf("serial Encode: %v", err)
	}
	packet, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Equal(packet, wantPacket) {
		t.Fatalf("tile-row packet differs from serial: %d/%d bytes first_diff=%d",
			len(packet), len(wantPacket), firstVP9PacketDiffForTest(packet, wantPacket))
	}

	h, tileStart := parseVP9EncoderHeaderForTest(t, packet)
	if h.Tile.Log2TileRows != 1 {
		t.Fatalf("Log2TileRows = %d, want 1", h.Tile.Log2TileRows)
	}
	if h.Tile.Log2TileCols != 1 {
		t.Fatalf("Log2TileCols = %d, want 1 for minimum wide-frame tiling",
			h.Tile.Log2TileCols)
	}
	if e.vp9TilePool != nil {
		t.Fatalf("tile-row configuration initialized unsafe pool with %d workers",
			e.vp9TilePool.workerCount)
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
		t.Fatal("NextFrame returned !ok after tile-row threaded keyframe")
	}
	assertVP9FilledFrameWithin(t, frame, width, height, 82, 123, 211, 1)
}

func TestVP9EncoderLog2TileRowsSerialMultiColumnDecodes(t *testing.T) {
	const width, height = 4104, 128
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:        width,
		Height:       height,
		Threads:      1,
		Log2TileRows: 1,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	packet, err := e.Encode(newVP9YCbCrForTest(width, height, 82, 123, 211))
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	h, _ := parseVP9EncoderHeaderForTest(t, packet)
	if h.Tile.Log2TileRows != 1 || h.Tile.Log2TileCols == 0 {
		t.Fatalf("tile grid = rows:%d cols:%d, want row tiles and multi-column minimum",
			h.Tile.Log2TileRows, h.Tile.Log2TileCols)
	}
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode serial tile grid: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after serial tile-grid keyframe")
	}
}

func TestVP9EncoderLog2TileRowsResizeValidation(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:        64,
		Height:       128,
		Threads:      2,
		Log2TileRows: 1,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{Width: 64, Height: 64}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetRealtimeTarget invalid tile rows error = %v, want ErrInvalidConfig", err)
	}
	if e.opts.Width != 64 || e.opts.Height != 128 {
		t.Fatalf("encoder dimensions changed after rejected resize: %dx%d",
			e.opts.Width, e.opts.Height)
	}
}

func TestVP9EncoderRuntimeResizeRebuildsTileWorkerPool(t *testing.T) {
	const smallWidth, smallHeight = 64, 64
	const wideWidth, wideHeight = 1280, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:   smallWidth,
		Height:  smallHeight,
		Threads: 4,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if _, err := e.Encode(newVP9YCbCrForTest(smallWidth, smallHeight, 82, 123, 211)); err != nil {
		t.Fatalf("small Encode: %v", err)
	}
	if e.vp9TilePool != nil {
		t.Fatalf("small threaded pool = %d workers, want nil before multi-tile resize",
			e.vp9TilePool.workerCount)
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{
		Width:  wideWidth,
		Height: wideHeight,
	}); err != nil {
		t.Fatalf("SetRealtimeTarget resize: %v", err)
	}
	packet, err := e.Encode(newVP9YCbCrForTest(wideWidth, wideHeight, 91, 143, 37))
	if err != nil {
		t.Fatalf("wide Encode: %v", err)
	}
	h, tileStart := parseVP9EncoderHeaderForTest(t, packet)
	if h.Tile.Log2TileCols != 2 {
		t.Fatalf("Log2TileCols after resize = %d, want 2 for Threads=4",
			h.Tile.Log2TileCols)
	}
	if e.vp9TilePool == nil {
		t.Fatal("VP9 tile worker pool was not rebuilt after resize")
	}
	if got, want := e.vp9TilePool.workerCount, 4; got != want {
		t.Fatalf("resized VP9 tile worker count = %d, want %d", got, want)
	}
	for i := range e.vp9TilePool.encodeJobs {
		if e.vp9TilePool.encodeJobs[i].size == 0 {
			t.Fatalf("resized VP9 tile worker job %d wrote zero bytes", i)
		}
	}
	assertVP9EncoderTilePrefixForTest(t, packet, tileStart)
}

func TestVP9TileWorkerPoolOutputSizeCache(t *testing.T) {
	pool := &vp9TileWorkerPool{
		outputs: make([][]byte, 4),
	}
	pool.ensureOutputSize(256)
	if got, want := pool.outputSize, 256; got != want {
		t.Fatalf("outputSize = %d, want %d", got, want)
	}
	first := make([]*byte, len(pool.outputs))
	for i := range pool.outputs {
		if len(pool.outputs[i]) != 256 {
			t.Fatalf("output %d len = %d, want 256", i, len(pool.outputs[i]))
		}
		first[i] = &pool.outputs[i][0]
	}
	pool.ensureOutputSize(256)
	for i := range pool.outputs {
		if &pool.outputs[i][0] != first[i] {
			t.Fatalf("output %d changed on cached ensure", i)
		}
	}

	allocs := testing.AllocsPerRun(vp9EncoderInterAllocRuns, func() {
		pool.ensureOutputSize(256)
	})
	if allocs != 0 {
		t.Fatalf("cached ensureOutputSize allocs = %f, want 0", allocs)
	}

	pool.ensureOutputSize(128)
	if got, want := pool.outputSize, 128; got != want {
		t.Fatalf("shrunk outputSize = %d, want %d", got, want)
	}
	for i := range pool.outputs {
		if len(pool.outputs[i]) != 128 {
			t.Fatalf("shrunk output %d len = %d, want 128", i, len(pool.outputs[i]))
		}
		if &pool.outputs[i][0] != first[i] {
			t.Fatalf("output %d reallocated while shrinking", i)
		}
	}
}

func TestVP9EncoderThreadedTileEncodeSteadyStateAlloc(t *testing.T) {
	const width, height = 1280, 720
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:   width,
		Height:  height,
		Threads: 4,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	frames := [2]*image.YCbCr{}
	for i := range frames {
		frames[i] = newVP9YCbCrForTest(width, height, 128, 128, 128)
	}
	dstSize, err := vp9AllocatingEncodeBufferSize(width, height)
	if err != nil {
		t.Fatalf("vp9AllocatingEncodeBufferSize: %v", err)
	}
	dst := make([]byte, dstSize)
	for i := range frames {
		if _, err := e.EncodeInto(frames[i], dst); err != nil {
			t.Fatalf("warm EncodeInto[%d]: %v", i, err)
		}
	}
	if e.vp9TilePool == nil {
		t.Fatal("threaded 720p encode did not initialize VP9 tile worker pool")
	}
	if got, want := e.vp9TilePool.workerCount, 4; got != want {
		t.Fatalf("threaded 720p tile worker count = %d, want %d", got, want)
	}
	idx := 0
	allocs := testing.AllocsPerRun(1, func() {
		frame := frames[idx&1]
		idx++
		if _, err := e.EncodeInto(frame, dst); err != nil {
			t.Fatalf("EncodeInto threaded alloc run: %v", err)
		}
	})
	if allocs != 0 {
		t.Fatalf("threaded tile EncodeInto steady-state allocs = %f, want 0", allocs)
	}
	for i := 0; i < e.vp9TilePool.workerCount; i++ {
		if e.vp9TilePool.encodeJobs[i].size <= 0 {
			t.Fatalf("threaded 720p tile job %d wrote %d bytes",
				i, e.vp9TilePool.encodeJobs[i].size)
		}
	}
}

func TestVP9EncoderThreadedTileFeaturePathsSteadyStateAlloc(t *testing.T) {
	const width, height = 1280, 720
	baseCBR := func() VP9EncoderOptions {
		return VP9EncoderOptions{
			Width:              width,
			Height:             height,
			Threads:            4,
			FPS:                30,
			TargetBitrateKbps:  2200,
			RateControlModeSet: true,
			RateControlMode:    RateControlCBR,
			MinQuantizer:       4,
			MaxQuantizer:       56,
		}
	}
	cases := []struct {
		name   string
		opts   VP9EncoderOptions
		before func(*testing.T, *VP9Encoder)
	}{
		{
			name: "vbr",
			opts: func() VP9EncoderOptions {
				opts := baseCBR()
				opts.RateControlMode = RateControlVBR
				return opts
			}(),
		},
		{
			name: "cq",
			opts: func() VP9EncoderOptions {
				opts := baseCBR()
				opts.RateControlMode = RateControlCQ
				opts.CQLevel = 20
				return opts
			}(),
		},
		{
			name: "q",
			opts: func() VP9EncoderOptions {
				opts := baseCBR()
				opts.RateControlMode = RateControlQ
				opts.CQLevel = 20
				return opts
			}(),
		},
		{
			name: "cyclic-aq",
			opts: func() VP9EncoderOptions {
				opts := baseCBR()
				opts.AQMode = VP9AQCyclicRefresh
				return opts
			}(),
		},
		{
			name: "active-map",
			opts: func() VP9EncoderOptions {
				opts := baseCBR()
				opts.MinQuantizer = 20
				opts.MaxQuantizer = 20
				return opts
			}(),
			before: func(t *testing.T, e *VP9Encoder) {
				t.Helper()
				rows := encoderMacroblockRows(height)
				cols := encoderMacroblockCols(width)
				activeMap := make([]uint8, rows*cols)
				for row := range rows {
					for col := range cols {
						activeMap[row*cols+col] = 1
						if (row+col)&1 == 0 {
							activeMap[row*cols+col] = 0
						}
					}
				}
				if err := e.SetActiveMap(activeMap, rows, cols); err != nil {
					t.Fatalf("SetActiveMap: %v", err)
				}
			},
		},
		{
			name: "roi",
			opts: func() VP9EncoderOptions {
				opts := baseCBR()
				opts.MinQuantizer = 20
				opts.MaxQuantizer = 20
				return opts
			}(),
			before: func(t *testing.T, e *VP9Encoder) {
				t.Helper()
				miRows := (height + 7) >> 3
				miCols := (width + 7) >> 3
				roi := ROIMap{
					Enabled:   true,
					Rows:      miRows,
					Cols:      miCols,
					SegmentID: make([]uint8, miRows*miCols),
				}
				for row := range miRows {
					for col := range miCols {
						if row == col || row+col == miCols-1 {
							roi.SegmentID[row*miCols+col] = 1
						}
					}
				}
				roi.DeltaQuantizer[1] = -4
				roi.DeltaLoopFilter[1] = 3
				if err := e.SetROIMap(&roi); err != nil {
					t.Fatalf("SetROIMap: %v", err)
				}
			},
		},
	}
	frames := [3]*image.YCbCr{
		newVP9YCbCrForTest(width, height, 96, 128, 128),
		newVP9YCbCrForTest(width, height, 112, 128, 128),
		newVP9YCbCrForTest(width, height, 128, 128, 128),
	}
	dstSize, err := vp9AllocatingEncodeBufferSize(width, height)
	if err != nil {
		t.Fatalf("vp9AllocatingEncodeBufferSize: %v", err)
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, err := NewVP9Encoder(tc.opts)
			if err != nil {
				t.Fatalf("NewVP9Encoder: %v", err)
			}
			if tc.before != nil {
				tc.before(t, e)
			}
			dst := make([]byte, dstSize)
			for i := range frames {
				if _, err := e.EncodeInto(frames[i], dst); err != nil {
					t.Fatalf("warm EncodeInto[%d]: %v", i, err)
				}
			}
			if e.vp9TilePool == nil {
				t.Fatal("threaded feature path did not initialize VP9 tile worker pool")
			}
			if got, want := e.vp9TilePool.workerCount, 4; got != want {
				t.Fatalf("threaded feature path worker count = %d, want %d",
					got, want)
			}
			idx := 0
			allocs := testing.AllocsPerRun(1, func() {
				frame := frames[idx%len(frames)]
				idx++
				if _, err := e.EncodeInto(frame, dst); err != nil {
					t.Fatalf("EncodeInto threaded feature alloc run: %v", err)
				}
			})
			if allocs != 0 {
				t.Fatalf("threaded feature path steady-state allocs = %f, want 0", allocs)
			}
			for i := 0; i < e.vp9TilePool.workerCount; i++ {
				if e.vp9TilePool.encodeJobs[i].size <= 0 {
					t.Fatalf("threaded feature tile job %d wrote %d bytes",
						i, e.vp9TilePool.encodeJobs[i].size)
				}
			}
		})
	}
}

func TestVP9EncoderTileRowsSteadyStateAlloc(t *testing.T) {
	const width, height = 1024, 128
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:        width,
		Height:       height,
		Threads:      2,
		Log2TileRows: 1,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	frames := [4]*image.YCbCr{}
	for i := range frames {
		frames[i] = newVP9PanningYCbCrForRateTest(width, height, i)
	}
	dst := make([]byte, 1<<20)
	for i := range frames {
		if _, err := e.EncodeInto(frames[i], dst); err != nil {
			t.Fatalf("warm EncodeInto[%d]: %v", i, err)
		}
	}
	idx := 0
	allocs := testing.AllocsPerRun(vp9EncoderInterAllocRuns, func() {
		frame := frames[idx&3]
		idx++
		if _, err := e.EncodeInto(frame, dst); err != nil {
			t.Fatalf("EncodeInto tile-row alloc run: %v", err)
		}
	})
	if allocs != 0 {
		t.Fatalf("tile-row EncodeInto steady-state allocs = %f, want 0", allocs)
	}
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
	allocs := testing.AllocsPerRun(vp9EncoderKeyframeAllocRuns, func() {
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
	allocs := testing.AllocsPerRun(vp9EncoderKeyframeAllocRuns, func() {
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
	allocs := testing.AllocsPerRun(vp9EncoderInterAllocRuns, func() {
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
	interSrc := newVP9YCbCrForTest(256, 192, 113, 123, 210)
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
	allocs := testing.AllocsPerRun(vp9EncoderInterAllocRuns, func() {
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

func TestVP9EncoderCyclicRefreshAQInterSteadyStateAlloc(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              128,
		Height:             128,
		FPS:                30,
		TargetBitrateKbps:  300,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		AQMode:             VP9AQCyclicRefresh,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	keySrc := newVP9YCbCrForTest(128, 128, 81, 123, 210)
	interSrc := newVP9YCbCrForTest(128, 128, 113, 123, 210)
	dst := make([]byte, 65536)

	if _, err := e.EncodeInto(keySrc, dst); err != nil {
		t.Fatalf("warm keyframe EncodeInto: %v", err)
	}
	var keyRef vp9ReferenceFrame
	keyRef.store(e.reconFrame)
	if _, err := e.EncodeInto(interSrc, dst); err != nil {
		t.Fatalf("warm cyclic inter EncodeInto: %v", err)
	}

	var n int
	allocs := testing.AllocsPerRun(vp9EncoderInterAllocRuns, func() {
		e.frameIndex = 1
		e.refFrames[0].store(keyRef.img)
		n, err = e.EncodeInto(interSrc, dst)
	})
	if err != nil {
		t.Fatalf("EncodeInto cyclic inter: %v", err)
	}
	if n == 0 {
		t.Fatal("EncodeInto cyclic inter wrote no bytes")
	}
	if allocs != 0 {
		t.Fatalf("VP9 cyclic AQ inter steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9EncoderActiveMapInterSteadyStateAlloc(t *testing.T) {
	const width, height = 128, 128
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	rows := encoderMacroblockRows(height)
	cols := encoderMacroblockCols(width)
	activeMap := make([]uint8, rows*cols)
	for row := range rows {
		for col := range cols {
			activeMap[row*cols+col] = 1
			if (row+col)&1 == 0 {
				activeMap[row*cols+col] = 0
			}
		}
	}
	if err := e.SetActiveMap(activeMap, rows, cols); err != nil {
		t.Fatalf("SetActiveMap: %v", err)
	}
	keySrc := newVP9YCbCrForTest(width, height, 81, 123, 210)
	interSrc := newVP9YCbCrForTest(width, height, 113, 123, 210)
	dst := make([]byte, 65536)

	if _, err := e.EncodeInto(keySrc, dst); err != nil {
		t.Fatalf("warm keyframe EncodeInto: %v", err)
	}
	var keyRef vp9ReferenceFrame
	keyRef.store(e.reconFrame)
	if _, err := e.EncodeInto(interSrc, dst); err != nil {
		t.Fatalf("warm active-map inter EncodeInto: %v", err)
	}

	var n int
	allocs := testing.AllocsPerRun(vp9EncoderInterAllocRuns, func() {
		e.frameIndex = 1
		e.refFrames[0].store(keyRef.img)
		n, err = e.EncodeInto(interSrc, dst)
	})
	if err != nil {
		t.Fatalf("EncodeInto active-map inter: %v", err)
	}
	if n == 0 {
		t.Fatal("EncodeInto active-map inter wrote no bytes")
	}
	if allocs != 0 {
		t.Fatalf("VP9 active-map inter steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9EncoderROIMapInterSteadyStateAlloc(t *testing.T) {
	const width, height = 128, 128
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	miRows := (height + 7) >> 3
	miCols := (width + 7) >> 3
	roi := ROIMap{
		Enabled:   true,
		Rows:      miRows,
		Cols:      miCols,
		SegmentID: make([]uint8, miRows*miCols),
	}
	for row := range miRows {
		for col := range miCols {
			if row == col || row+col == miCols-1 {
				roi.SegmentID[row*miCols+col] = 1
			}
		}
	}
	roi.DeltaQuantizer[1] = -4
	roi.DeltaLoopFilter[1] = 3
	if err := e.SetROIMap(&roi); err != nil {
		t.Fatalf("SetROIMap: %v", err)
	}
	keySrc := newVP9YCbCrForTest(width, height, 81, 123, 210)
	interSrc := newVP9YCbCrForTest(width, height, 113, 123, 210)
	dst := make([]byte, 65536)

	if _, err := e.EncodeInto(keySrc, dst); err != nil {
		t.Fatalf("warm keyframe EncodeInto: %v", err)
	}
	var keyRef vp9ReferenceFrame
	keyRef.store(e.reconFrame)
	if _, err := e.EncodeInto(interSrc, dst); err != nil {
		t.Fatalf("warm ROI inter EncodeInto: %v", err)
	}

	var n int
	allocs := testing.AllocsPerRun(vp9EncoderInterAllocRuns, func() {
		e.frameIndex = 1
		e.refFrames[0].store(keyRef.img)
		n, err = e.EncodeInto(interSrc, dst)
	})
	if err != nil {
		t.Fatalf("EncodeInto ROI inter: %v", err)
	}
	if n == 0 {
		t.Fatal("EncodeInto ROI inter wrote no bytes")
	}
	if allocs != 0 {
		t.Fatalf("VP9 ROI inter steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9EncoderDenoiserInterSteadyStateAlloc(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:            width,
		Height:           height,
		NoiseSensitivity: 3,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	keySrc := newVP9YCbCrForTest(width, height, 100, 96, 160)
	interSrc := newVP9YCbCrForTest(width, height, 102, 98, 158)
	dst := make([]byte, 65536)

	if _, err := e.EncodeInto(keySrc, dst); err != nil {
		t.Fatalf("warm keyframe EncodeInto: %v", err)
	}
	var keyRef vp9ReferenceFrame
	keyRef.store(e.reconFrame)
	keyAvg := *image.NewYCbCr(image.Rect(0, 0, width, height),
		image.YCbCrSubsampleRatio420)
	copyVP9LookaheadImage(&keyAvg, &e.denoiser.runningAvg[vp9DenoiserAvgLast],
		width, height)
	if _, err := e.EncodeInto(interSrc, dst); err != nil {
		t.Fatalf("warm denoiser inter EncodeInto: %v", err)
	}

	var n int
	allocs := testing.AllocsPerRun(vp9EncoderInterAllocRuns, func() {
		e.frameIndex = 1
		e.refFrames[0].store(keyRef.img)
		copyVP9LookaheadImage(&e.denoiser.runningAvg[vp9DenoiserAvgLast],
			&keyAvg, width, height)
		e.denoiser.reset = false
		n, err = e.EncodeInto(interSrc, dst)
	})
	if err != nil {
		t.Fatalf("EncodeInto denoiser inter: %v", err)
	}
	if n == 0 {
		t.Fatal("EncodeInto denoiser inter wrote no bytes")
	}
	if allocs != 0 {
		t.Fatalf("VP9 denoiser inter steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9EncoderAutoAltRefLookaheadSteadyStateAlloc(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:           width,
		Height:          height,
		LookaheadFrames: 4,
		AutoAltRef:      true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	sources := [8]*image.YCbCr{
		newVP9YCbCrForTest(width, height, 80, 128, 128),
		newVP9YCbCrForTest(width, height, 96, 128, 128),
		newVP9YCbCrForTest(width, height, 112, 128, 128),
		newVP9YCbCrForTest(width, height, 128, 128, 128),
		newVP9YCbCrForTest(width, height, 144, 128, 128),
		newVP9YCbCrForTest(width, height, 160, 128, 128),
		newVP9YCbCrForTest(width, height, 176, 128, 128),
		newVP9YCbCrForTest(width, height, 192, 128, 128),
	}
	dst := make([]byte, 65536)
	for i := 0; i < 5; i++ {
		_, err := e.EncodeIntoWithResult(sources[i], dst)
		if errors.Is(err, ErrFrameNotReady) {
			continue
		}
		if err != nil {
			t.Fatalf("warm EncodeIntoWithResult frame %d: %v", i, err)
		}
	}
	if !e.autoAltRefPendingSet || !e.autoAltRefEmitted {
		t.Fatalf("auto-alt-ref warm state = pending:%t emitted:%t, want true/true",
			e.autoAltRefPendingSet, e.autoAltRefEmitted)
	}

	idx := 5
	var result VP9EncodeResult
	allocs := testing.AllocsPerRun(vp9EncoderInterAllocRuns, func() {
		result, err = e.EncodeIntoWithResult(sources[idx&7], dst)
		idx++
	})
	if err != nil {
		t.Fatalf("EncodeIntoWithResult auto-alt-ref steady state: %v", err)
	}
	if result.Dropped || !result.ShowFrame || len(result.Data) == 0 {
		t.Fatalf("auto-alt-ref steady packet = dropped:%t show:%t bytes:%d, want visible packet",
			result.Dropped, result.ShowFrame, len(result.Data))
	}
	if allocs != 0 {
		t.Fatalf("VP9 auto-alt-ref lookahead steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9EncoderCBRDropSteadyStateAlloc(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              256,
		Height:             192,
		FPS:                30,
		TargetBitrateKbps:  1,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		DropFrameAllowed:   true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	img := newVP9YCbCrForTest(256, 192, 128, 128, 128)
	dst := make([]byte, 65536)
	if _, err := e.EncodeIntoWithResult(img, dst); err != nil {
		t.Fatalf("warm keyframe EncodeIntoWithResult: %v", err)
	}

	var result VP9EncodeResult
	allocs := testing.AllocsPerRun(vp9EncoderInterAllocRuns, func() {
		e.frameIndex = 1
		e.rc.bufferLevelBits = -1
		result, err = e.EncodeIntoWithResult(img, dst)
	})
	if err != nil {
		t.Fatalf("drop EncodeIntoWithResult: %v", err)
	}
	if !result.Dropped || len(result.Data) != 0 {
		t.Fatalf("drop result = dropped:%t data:%d, want dropped empty output",
			result.Dropped, len(result.Data))
	}
	if allocs != 0 {
		t.Fatalf("VP9 CBR drop steady state: got %v allocs/op, want 0", allocs)
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

func assertVP9ImageMatchesYCbCr(t *testing.T, name string, got Image, want *image.YCbCr) {
	t.Helper()
	wantImage := vp9ImageFromYCbCrForTest(want)
	if got.Width != wantImage.Width || got.Height != wantImage.Height {
		t.Fatalf("%s dimensions = %dx%d, want %dx%d",
			name, got.Width, got.Height, wantImage.Width, wantImage.Height)
	}
	checkPlane := func(label string, gotPlane []byte, gotStride int,
		wantPlane []byte, wantStride, width, height int,
	) {
		t.Helper()
		for y := range height {
			gotRow := gotPlane[y*gotStride:]
			wantRow := wantPlane[y*wantStride:]
			for x := range width {
				if gotRow[x] != wantRow[x] {
					t.Fatalf("%s %s[%d,%d] = %d, want %d",
						name, label, y, x, gotRow[x], wantRow[x])
				}
			}
		}
	}
	checkPlane("Y", got.Y, got.YStride, wantImage.Y, wantImage.YStride,
		wantImage.Width, wantImage.Height)
	uvWidth := (wantImage.Width + 1) >> 1
	uvHeight := (wantImage.Height + 1) >> 1
	checkPlane("U", got.U, got.UStride, wantImage.U, wantImage.UStride,
		uvWidth, uvHeight)
	checkPlane("V", got.V, got.VStride, wantImage.V, wantImage.VStride,
		uvWidth, uvHeight)
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
