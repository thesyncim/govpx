package vp9test

import (
	"bytes"
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func NewYCbCr(width, height int, y, u, v byte) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	FillYCbCr(img, y, u, v)
	return img
}

func FillYCbCr(img *image.YCbCr, y, u, v byte) {
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

func NewPanningYCbCr(width, height int, frame int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	for y := range height {
		row := img.Y[y*img.YStride:]
		for x := range width {
			row[x] = byte(24 + ((x+frame*3)*7+y*11+(x*y+frame*13)%37)%208)
		}
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for y := range uvHeight {
		cb := img.Cb[y*img.CStride:]
		cr := img.Cr[y*img.CStride:]
		for x := range uvWidth {
			cb[x] = byte(64 + ((x+frame)*5+y*3)%128)
			cr[x] = byte(72 + (x*3+(y+frame)*7)%112)
		}
	}
	return img
}

func NewCheckerYCbCr(width, height int, lo, hi, u, v byte) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	for y := 0; y < height; y++ {
		row := img.Y[y*img.YStride:]
		for x := 0; x < width; x++ {
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

func NewHorizontalBandsYCbCr(width, height int, u, v byte) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	for y := 0; y < height; y++ {
		row := img.Y[y*img.YStride:]
		value := byte(32 + (y*5)%192)
		for x := 0; x < width; x++ {
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

func NewChromaHorizontalBandsYCbCr(width, height int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	for i := range img.Y {
		img.Y[i] = 128
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for y := 0; y < uvHeight; y++ {
		cb := img.Cb[y*img.CStride:]
		cr := img.Cr[y*img.CStride:]
		cbValue := byte(32 + (y*7)%192)
		crValue := byte(48 + (y*11)%176)
		for x := 0; x < uvWidth; x++ {
			cb[x] = cbValue
			cr[x] = crValue
		}
	}
	return img
}

func NewMotionYCbCr(width, height int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	for y := 0; y < height; y++ {
		row := img.Y[y*img.YStride:]
		for x := 0; x < width; x++ {
			row[x] = byte(16 + (x*7+y*11+(x*y)%37)%224)
		}
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for y := 0; y < uvHeight; y++ {
		cb := img.Cb[y*img.CStride:]
		cr := img.Cr[y*img.CStride:]
		for x := 0; x < uvWidth; x++ {
			cb[x] = byte(64 + (x*5+y*3)%128)
			cr[x] = byte(48 + (x*3+y*7)%160)
		}
	}
	return img
}

func NewCompoundAverageYCbCr(width, height, delta int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	for y := 0; y < height; y++ {
		row := img.Y[y*img.YStride:]
		for x := 0; x < width; x++ {
			base := 96 + (x*5+y*7+(x*y)%19)%64
			row[x] = byte(base + delta)
		}
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for y := 0; y < uvHeight; y++ {
		cb := img.Cb[y*img.CStride:]
		cr := img.Cr[y*img.CStride:]
		for x := 0; x < uvWidth; x++ {
			baseCb := 104 + (x*3+y*5)%32
			baseCr := 112 + (x*7+y*2)%32
			cb[x] = byte(baseCb + delta/2)
			cr[x] = byte(baseCr + delta/2)
		}
	}
	return img
}

func NewCompoundPairYCbCr(width, height int, variant bool) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	for y := 0; y < height; y++ {
		row := img.Y[y*img.YStride:]
		for x := 0; x < width; x++ {
			if variant {
				row[x] = byte(88 + (x*29+y*7+((x+3)*(y+5))%83)%104)
			} else {
				row[x] = byte(48 + (x*17+y*31+(x*y)%67)%120)
			}
		}
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for y := 0; y < uvHeight; y++ {
		cb := img.Cb[y*img.CStride:]
		cr := img.Cr[y*img.CStride:]
		for x := 0; x < uvWidth; x++ {
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

func AverageYCbCr(a, b *image.YCbCr) *image.YCbCr {
	width, height := a.Rect.Dx(), a.Rect.Dy()
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	avgPlane := func(dst []byte, dstStride int, ap []byte, aStride int, bp []byte, bStride int, w, h int) {
		for y := 0; y < h; y++ {
			dstRow := dst[y*dstStride:]
			aRow := ap[y*aStride:]
			bRow := bp[y*bStride:]
			for x := 0; x < w; x++ {
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

func AppendI420(out []byte, img *image.YCbCr) []byte {
	return testutil.AppendYCbCrI420(out, img)
}

func EqualYCbCr(a *image.YCbCr, b *image.YCbCr, width int, height int) bool {
	for y := range height {
		if !bytes.Equal(a.Y[y*a.YStride:][:width], b.Y[y*b.YStride:][:width]) {
			return false
		}
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for y := range uvHeight {
		if !bytes.Equal(a.Cb[y*a.CStride:][:uvWidth], b.Cb[y*b.CStride:][:uvWidth]) ||
			!bytes.Equal(a.Cr[y*a.CStride:][:uvWidth], b.Cr[y*b.CStride:][:uvWidth]) {
			return false
		}
	}
	return true
}

func FirstPacketDiff(a, b []byte) int {
	n := min(len(a), len(b))
	for i := range n {
		if a[i] != b[i] {
			return i
		}
	}
	if len(a) != len(b) {
		return n
	}
	return -1
}

func ParseHeader(t testing.TB, packet []byte) (vp9dec.UncompressedHeader, int) {
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
