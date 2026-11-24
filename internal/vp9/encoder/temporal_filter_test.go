package encoder

import (
	"bytes"
	"image"
	"testing"
)

func TestTemporalFilterWindowDirections(t *testing.T) {
	tests := []struct {
		name      string
		distance  int
		lookahead int
		maxFrames int
		filter    int
		backward  int
		forward   int
		ok        bool
	}{
		{
			name:      "backward only clamps to max minus center",
			distance:  8,
			lookahead: 10,
			maxFrames: 5,
			filter:    1,
			backward:  4,
			ok:        true,
		},
		{
			name:      "forward only clamps to max minus center",
			distance:  1,
			lookahead: 9,
			maxFrames: 4,
			filter:    2,
			forward:   3,
			ok:        true,
		},
		{
			name:      "centered uses available backward refs at gf end",
			distance:  6,
			lookahead: 7,
			maxFrames: 7,
			filter:    3,
			backward:  6,
			ok:        true,
		},
		{
			name:      "centered balances both sides when available",
			distance:  4,
			lookahead: 10,
			maxFrames: 7,
			filter:    3,
			backward:  3,
			forward:   3,
			ok:        true,
		},
		{
			name:      "invalid type rejects the window",
			distance:  2,
			lookahead: 5,
			maxFrames: 7,
			filter:    0,
		},
		{
			name:      "distance outside lookahead rejects the window",
			distance:  6,
			lookahead: 4,
			maxFrames: 7,
			filter:    3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			backward, forward, ok := TemporalFilterWindow(
				tc.distance, tc.lookahead, tc.maxFrames, tc.filter)
			if ok != tc.ok || backward != tc.backward || forward != tc.forward {
				t.Fatalf("TemporalFilterWindow() = backward=%d forward=%d ok=%v, want backward=%d forward=%d ok=%v",
					backward, forward, ok, tc.backward, tc.forward, tc.ok)
			}
		})
	}
}

func TestTemporalFilterFrameFromYCbCrAliasesImage(t *testing.T) {
	img := solidTemporalFilterImage(16, 16, 40, 90, 150)
	frame := TemporalFilterFrameFromYCbCr(img)

	if frame.width != 16 || frame.height != 16 {
		t.Fatalf("frame dimensions = %dx%d, want 16x16",
			frame.width, frame.height)
	}
	img.Y[0] = 77
	img.Cb[0] = 88
	img.Cr[0] = 99
	if frame.y[0] != 77 || frame.u[0] != 88 || frame.v[0] != 99 {
		t.Fatalf("frame does not alias YCbCr planes: got Y=%d U=%d V=%d",
			frame.y[0], frame.u[0], frame.v[0])
	}
}

func TestIterateTemporalFilterKeepsSingleCenterReference(t *testing.T) {
	img := solidTemporalFilterImage(64, 64, 72, 128, 164)
	wantY := append([]byte(nil), img.Y...)
	wantCb := append([]byte(nil), img.Cb...)
	wantCr := append([]byte(nil), img.Cr...)

	dst := TemporalFilterFrameFromYCbCr(img)
	refs := []TemporalFilterFrame{dst}
	IterateTemporalFilter(&dst, refs, 0, 3)

	if !bytes.Equal(img.Y, wantY) {
		t.Fatal("Y plane changed with only the center reference")
	}
	if !bytes.Equal(img.Cb, wantCb) {
		t.Fatal("Cb plane changed with only the center reference")
	}
	if !bytes.Equal(img.Cr, wantCr) {
		t.Fatal("Cr plane changed with only the center reference")
	}
}

func TestIterateTemporalFilterHonorsZeroStrength(t *testing.T) {
	img := solidTemporalFilterImage(32, 32, 100, 128, 128)
	ref := solidTemporalFilterImage(32, 32, 104, 128, 128)
	wantY := append([]byte(nil), img.Y...)

	dst := TemporalFilterFrameFromYCbCr(img)
	refs := []TemporalFilterFrame{
		TemporalFilterFrameFromYCbCr(img),
		TemporalFilterFrameFromYCbCr(ref),
	}
	IterateTemporalFilter(&dst, refs, 0, 0)

	if !bytes.Equal(img.Y, wantY) {
		t.Fatal("strength zero pulled luma toward the non-center reference")
	}

	strong := solidTemporalFilterImage(32, 32, 100, 128, 128)
	dst = TemporalFilterFrameFromYCbCr(strong)
	refs[0] = TemporalFilterFrameFromYCbCr(strong)
	IterateTemporalFilter(&dst, refs, 0, 3)
	if bytes.Equal(strong.Y, wantY) {
		t.Fatal("strength-sensitive fixture did not change at strength three")
	}
}

func TestTemporalFilterBlockHelpersClampEdges(t *testing.T) {
	src := []byte{
		10, 20,
		30, 40,
	}
	var dst [9]byte
	gatherTemporalFilterBlock(dst[:], 3, src, 2, -1, -1, 2, 2, 3)
	want := []byte{
		10, 10, 20,
		10, 10, 20,
		30, 30, 40,
	}
	if !bytes.Equal(dst[:], want) {
		t.Fatalf("gatherTemporalFilterBlock edge clamp = %v, want %v",
			dst, want)
	}

	accumulator := []uint32{
		10, 20, 30, 40,
	}
	count := []uint32{
		1, 1, 1, 1,
	}
	out := make([]byte, 4)
	writeTemporalFilterBlock(out, 2, 0, 0, 2, 2, 2, accumulator, count)
	if !bytes.Equal(out, []byte{10, 20, 30, 40}) {
		t.Fatalf("writeTemporalFilterBlock = %v, want [10 20 30 40]", out)
	}
}

func TestTemporalFilterModIndexClampAndInvalidIndex(t *testing.T) {
	if got := vp9TemporalFilterModIndex(100, 0, 0, 3, 2); got != 0 {
		t.Fatalf("invalid modifier index = %d, want 0", got)
	}
	got := vp9TemporalFilterModIndex(1<<20, 6, 4, 3, 2)
	want := vp9TemporalFilterModIndex(0xffff, 6, 4, 3, 2)
	if got != want {
		t.Fatalf("large sum clamp = %d, want %d", got, want)
	}
}

func TestVP9ARNRBlock32MVBounds(t *testing.T) {
	got := vp9ARNRBlock32MVBounds(1, 2, 4, 5)
	want := vp9ARNRMVBounds{
		colMin: -69,
		colMax: 69,
		rowMin: -37,
		rowMax: 69,
	}
	if got != want {
		t.Fatalf("vp9ARNRBlock32MVBounds() = %+v, want %+v", got, want)
	}
}

func solidTemporalFilterImage(width, height int, y, cb, cr byte) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height),
		image.YCbCrSubsampleRatio420)
	for i := range img.Y {
		img.Y[i] = y
	}
	for i := range img.Cb {
		img.Cb[i] = cb
		img.Cr[i] = cr
	}
	return img
}
