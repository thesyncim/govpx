package encoder

import "testing"

func TestBPredBlockHelpersClampAndPlaceSamples(t *testing.T) {
	src := SourceImage{
		Width:   5,
		Height:  5,
		YStride: 5,
		Y:       make([]byte, 5*5),
	}
	for y := range src.Height {
		for x := range src.Width {
			src.Y[y*src.YStride+x] = byte(y*20 + x*3)
		}
	}
	pred := []byte{
		3, 4, 5, 6,
		7, 8, 9, 10,
		11, 12, 13, 14,
		15, 16, 17, 18,
	}

	var residual [16]int16
	FillBPredResidual4x4(src, 0, 0, 5, pred, &residual)

	wantSSE := 0
	for row := range 4 {
		for col := range 4 {
			srcY := clampEncodeCoord(4+row, src.Height)
			srcX := clampEncodeCoord(4+col, src.Width)
			want := int16(int(src.Y[srcY*src.YStride+srcX]) - int(pred[row*4+col]))
			if residual[row*4+col] != want {
				t.Fatalf("residual[%d,%d] = %d, want %d", row, col, residual[row*4+col], want)
			}
			diff := int(want)
			wantSSE += diff * diff
		}
	}
	if got := BPredBlockSSE(src, 0, 0, 5, pred, 4); got != wantSSE {
		t.Fatalf("BPredBlockSSE = %d, want %d", got, wantSSE)
	}

	var dst [16 * 16]byte
	CopyBPredBlock(pred, dst[:], 16, 5)
	for row := range 4 {
		for col := range 4 {
			got := dst[(4+row)*16+4+col]
			want := pred[row*4+col]
			if got != want {
				t.Fatalf("copied block sample[%d,%d] = %d, want %d", row, col, got, want)
			}
		}
	}
}
