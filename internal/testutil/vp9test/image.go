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
