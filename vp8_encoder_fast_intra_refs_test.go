package govpx

import (
	"math/rand"
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

// TestVP8FastPickerLumaIntraRefsMatchesBuilder pins the fast picker's
// interior direct-alias intra-neighbor resolution against the canonical
// edge-aware BuildIntraPredictorRefsLuma across every macroblock position,
// including the top row, left column, rightmost column (border-extended
// above-right), and bottom row.
func TestVP8FastPickerLumaIntraRefsMatchesBuilder(t *testing.T) {
	rng := rand.New(rand.NewSource(0x8f2a))
	for _, dim := range [][2]int{{128, 96}, {80, 48}, {65, 63}} {
		fb, err := vp8common.NewFrameBuffer(dim[0], dim[1], 32, 16)
		if err != nil {
			t.Fatalf("NewFrameBuffer(%dx%d): %v", dim[0], dim[1], err)
		}
		for i := range fb.Img.YFull {
			fb.Img.YFull[i] = byte(rng.Intn(256))
		}
		fb.ExtendBorders()
		img := &fb.Img
		mbRows := (img.CodedHeight + 15) / 16
		mbCols := (img.CodedWidth + 15) / 16
		var fastScratch, refScratch vp8dec.IntraPredictorScratch
		for mbRow := range mbRows {
			for mbCol := range mbCols {
				got := fastPickerLumaIntraRefs(img, mbRow, mbCol, &fastScratch)
				want := vp8dec.BuildIntraPredictorRefsLuma(img, mbRow, mbCol, &refScratch)
				if got.UpAvailable != want.UpAvailable || got.LeftAvailable != want.LeftAvailable {
					t.Fatalf("%dx%d MB(%d,%d): availability got (%v,%v) want (%v,%v)",
						dim[0], dim[1], mbRow, mbCol, got.UpAvailable, got.LeftAvailable, want.UpAvailable, want.LeftAvailable)
				}
				if got.YTopLeft != want.YTopLeft {
					t.Fatalf("%dx%d MB(%d,%d): top-left got %d want %d", dim[0], dim[1], mbRow, mbCol, got.YTopLeft, want.YTopLeft)
				}
				if len(got.YAbove) != len(want.YAbove) {
					t.Fatalf("%dx%d MB(%d,%d): above len got %d want %d", dim[0], dim[1], mbRow, mbCol, len(got.YAbove), len(want.YAbove))
				}
				for i := range want.YAbove {
					if got.YAbove[i] != want.YAbove[i] {
						t.Fatalf("%dx%d MB(%d,%d): above[%d] got %d want %d", dim[0], dim[1], mbRow, mbCol, i, got.YAbove[i], want.YAbove[i])
					}
				}
				if len(got.YLeft) != len(want.YLeft) {
					t.Fatalf("%dx%d MB(%d,%d): left len got %d want %d", dim[0], dim[1], mbRow, mbCol, len(got.YLeft), len(want.YLeft))
				}
				for i := range want.YLeft {
					if got.YLeft[i] != want.YLeft[i] {
						t.Fatalf("%dx%d MB(%d,%d): left[%d] got %d want %d", dim[0], dim[1], mbRow, mbCol, i, got.YLeft[i], want.YLeft[i])
					}
				}
			}
		}
	}
}

// TestVP8MacroblockLumaVarianceSSEAgainstBufferMatchesFrame pins the
// contiguous-predictor variance helper against the frame-backed
// MacroblockLumaVarianceSSE reference on full-interior and clamped
// partial-edge macroblocks: predicting the same pixels into a frame region
// and into the stride-16 buffer must produce identical (variance, sse).
func TestVP8MacroblockLumaVarianceSSEAgainstBufferMatchesFrame(t *testing.T) {
	rng := rand.New(rand.NewSource(0x51c3))
	for _, dim := range [][2]int{{64, 64}, {65, 63}, {48, 33}} {
		fb, err := vp8common.NewFrameBuffer(dim[0], dim[1], 32, 16)
		if err != nil {
			t.Fatalf("NewFrameBuffer(%dx%d): %v", dim[0], dim[1], err)
		}
		img := &fb.Img
		srcW, srcH := dim[0], dim[1]
		srcStride := img.CodedWidth + 16
		srcY := make([]byte, srcStride*(srcH+16))
		for i := range srcY {
			srcY[i] = byte(rng.Intn(256))
		}
		src := vp8enc.SourceImage{Y: srcY, YStride: srcStride, Width: srcW, Height: srcH}
		mbRows := (img.CodedHeight + 15) / 16
		mbCols := (img.CodedWidth + 15) / 16
		var pred [256]byte
		for mbRow := range mbRows {
			for mbCol := range mbCols {
				for i := range pred {
					pred[i] = byte(rng.Intn(256))
				}
				// Mirror the buffer into the analysis-frame region so the
				// canonical frame-backed helper sees identical predictor
				// pixels.
				yOff := mbRow*16*img.YStride + mbCol*16
				for r := range 16 {
					copy(img.Y[yOff+r*img.YStride:yOff+r*img.YStride+16], pred[r*16:r*16+16])
				}
				gotVar, gotSSE := macroblockLumaVarianceSSEAgainstBuffer(src, mbRow, mbCol, &pred)
				wantVar, wantSSE := vp8enc.MacroblockLumaVarianceSSE(src, img, mbRow, mbCol)
				if gotVar != wantVar || gotSSE != wantSSE {
					t.Fatalf("%dx%d MB(%d,%d): got (%d,%d) want (%d,%d)", dim[0], dim[1], mbRow, mbCol, gotVar, gotSSE, wantVar, wantSSE)
				}
			}
		}
	}
}
