package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vp9/decoder"
)

// Pinning tests for get_estimated_pred. Reference values are
// hand-computed from the libvpx v1.16.0 bodies cited in that file.

// TestVP9GetEstimatedPredKeyFrameFillsWith128 pins the keyframe
// memset(128) branch (libvpx vp9_encodeframe.c:5198).
func TestVP9GetEstimatedPredKeyFrameFillsWith128(t *testing.T) {
	estPred := make([]uint8, 64*64)
	for i := range estPred {
		estPred[i] = 0xFF // poison.
	}
	GetEstimatedPredKeyFrame(estPred)
	for i, v := range estPred {
		if v != 128 {
			t.Fatalf("estPred[%d] = %d, want 128", i, v)
		}
	}
}

// TestVP9GetEstimatedPredSubBsizeFormula pins the per-SB sub-bsize
// formula from libvpx vp9_encodeframe.c:5113-5114.
func TestVP9GetEstimatedPredSubBsizeFormula(t *testing.T) {
	// miCols=8, miRows=8 → a single 64x64 SB at (0,0).
	// (mi_col+4 < 8) = (4 < 8) = true → +2
	// (mi_row+4 < 8) = (4 < 8) = true → +1
	// bsize = BLOCK_32X32 + 2 + 1 = BLOCK_64X64.
	cases := []struct {
		miRow, miCol, miRows, miCols int
		want                         common.BlockSize
	}{
		// Both right and bottom neighbours fit (mi+4 < edge):
		// BLOCK_64X64.
		{0, 0, 8, 8, common.Block64x64},
		// Last-row SB (mi_row+4 == mi_rows): col fits, row doesn't —
		// BLOCK_64X32.
		{4, 0, 8, 16, common.Block64x32},
		// Last-column SB (mi_col+4 == mi_cols): row fits, col doesn't —
		// BLOCK_32X64.
		{0, 4, 16, 8, common.Block32x64},
		// Bottom-right corner SB (neither fits): BLOCK_32X32.
		{4, 4, 8, 8, common.Block32x32},
	}
	for _, tc := range cases {
		got := GetEstimatedPredSubBsize(tc.miRow, tc.miCol, tc.miRows, tc.miCols)
		if got != tc.want {
			t.Errorf("miRow=%d miCol=%d miRows=%d miCols=%d: got %v want %v",
				tc.miRow, tc.miCol, tc.miRows, tc.miCols, got, tc.want)
		}
	}
}

// TestVP9BuildEstimatedPredLuma64x64ZeroMVCopy verifies that a zero
// MV produces a direct copy of the 64x64 reference window.
func TestVP9BuildEstimatedPredLuma64x64ZeroMVCopy(t *testing.T) {
	stride := 128
	ref := make([]uint8, stride*128)
	for y := range 64 {
		for x := range 64 {
			ref[y*stride+x] = uint8((y*3 + x*5) & 0xFF)
		}
	}
	estPred := make([]uint8, 64*64)
	// Edges = 0: the spel border slack (>= 1072 Q4) keeps the small test
	// MVs below unclamped, so this exercises the identity copy path.
	BuildEstimatedPredLuma64x64(estPred, ref, 0, stride, decoder.MV{Row: 0, Col: 0}, 0, 0, 0, 0)
	for y := range 64 {
		for x := range 64 {
			want := ref[y*stride+x]
			got := estPred[y*64+x]
			if got != want {
				t.Fatalf("est[%d,%d] = %d, want %d", y, x, got, want)
			}
		}
	}
}

// TestVP9BuildEstimatedPredLuma64x64FullPelShift verifies that a
// full-pel MV produces a shifted copy. mv = (col=8 full-pel * 8 = 64
// in 1/8-pel, row=0): the predictor should pull pixels from
// ref[y][x+8], so estPred[y][x] = ref[y][x+8].
func TestVP9BuildEstimatedPredLuma64x64FullPelShift(t *testing.T) {
	stride := 256
	ref := make([]uint8, stride*128)
	for y := range 128 {
		for x := range 128 {
			ref[y*stride+x] = uint8((y*3 + x*5) & 0xFF)
		}
	}
	estPred := make([]uint8, 64*64)
	// MV in 1/8-pel: col=64 -> Q4 col=128 -> full-pel offset 8.
	// Edges = 0: spel_right slack (1072 Q4) > 128, so col stays unclamped.
	BuildEstimatedPredLuma64x64(estPred, ref, 0, stride, decoder.MV{Row: 0, Col: 64}, 0, 0, 0, 0)
	// pre += (col_q4 >> 4) = 8 -> ref[y][8..71] -> estPred[y][0..63].
	for y := range 64 {
		for x := range 64 {
			want := ref[y*stride+(x+8)]
			got := estPred[y*64+x]
			if got != want {
				t.Fatalf("est[%d,%d] = %d, want %d (ref[y][x+8])", y, x, got, want)
			}
		}
	}
}

// TestVP9GetEstimatedPredKeyFramePath validates the GetEstimatedPred
// orchestrator on the keyframe branch.
func TestVP9GetEstimatedPredKeyFramePath(t *testing.T) {
	estPred := make([]uint8, 64*64)
	for i := range estPred {
		estPred[i] = 0xFF
	}
	chosenRef, mv, _ := GetEstimatedPred(true, nil, estPred)
	if chosenRef != RefIntra {
		t.Errorf("keyframe chosen ref: got %v want INTRA", chosenRef)
	}
	if mv.Row != 0 || mv.Col != 0 {
		t.Errorf("keyframe MV: got (%d,%d) want (0,0)", mv.Row, mv.Col)
	}
	for i, v := range estPred {
		if v != 128 {
			t.Fatalf("estPred[%d] = %d, want 128", i, v)
		}
	}
}

// TestVP9GetEstimatedPredInterIdentityRouteLast validates the inter
// branch when src == last_ref and no GOLDEN probe fires: int_pro
// motion search returns (0,0), chosenRef = LAST.
func TestVP9GetEstimatedPredInterIdentityRouteLast(t *testing.T) {
	stride := 256
	frame := make([]uint8, stride*stride)
	originX, originY := 128, 128
	for y := -64; y < 128; y++ {
		for x := -64; x < 128; x++ {
			frame[(originY+y)*stride+(originX+x)] = uint8((y + x*3) & 0xFF)
		}
	}
	srcOff := originY*stride + originX
	in := &GetEstimatedPredInterInput{
		Bsize:                  common.Block64x64,
		Src:                    frame,
		SrcOff:                 srcOff,
		SrcStride:              stride,
		LastRef:                frame,
		LastRefOff:             srcOff,
		LastRefStride:          stride,
		HaveGolden:             false,
		HaveAltRef:             false,
		Speed:                  8, // Speed 8 disables the golden probe.
		MvLimits:               MvLimits{ColMin: -16, ColMax: 16, RowMin: -16, RowMax: 16},
		ShortCircuitLowTempVar: false,
	}
	chosenRef, mv, _ := GetEstimatedPredInter(in)
	if chosenRef != RefLast {
		t.Errorf("chosenRef: got %v want LAST", chosenRef)
	}
	if mv.Row != 0 || mv.Col != 0 {
		t.Errorf("identity MV: got (%d,%d) want (0,0)", mv.Row, mv.Col)
	}
}

// TestVP9GetEstimatedPredInterAltRefHijack validates the ALTREF-as-LAST
// path: with lag_in_frames > 0, rc=VBR, is_src_frame_alt_ref → libvpx
// hijacks LAST and points it at ALTREF (vp9_encodeframe.c:5142-5148).
func TestVP9GetEstimatedPredInterAltRefHijack(t *testing.T) {
	stride := 256
	src := make([]uint8, stride*stride)
	altRef := make([]uint8, stride*stride)
	lastRef := make([]uint8, stride*stride)
	originX, originY := 128, 128
	for y := -64; y < 128; y++ {
		for x := -64; x < 128; x++ {
			v := uint8((y + x*3) & 0xFF)
			src[(originY+y)*stride+(originX+x)] = v
			altRef[(originY+y)*stride+(originX+x)] = v
			lastRef[(originY+y)*stride+(originX+x)] = ^v // contrast.
		}
	}
	srcOff := originY*stride + originX
	in := &GetEstimatedPredInterInput{
		Bsize:            common.Block64x64,
		Src:              src,
		SrcOff:           srcOff,
		SrcStride:        stride,
		LastRef:          lastRef,
		LastRefOff:       srcOff,
		LastRefStride:    stride,
		HaveAltRef:       true,
		AltRef:           altRef,
		AltRefOff:        srcOff,
		AltRefStride:     stride,
		Speed:            8,
		LagInFrames:      25,
		RcModeIsVBR:      true,
		IsSrcFrameAltRef: true,
		MvLimits:         MvLimits{ColMin: -16, ColMax: 16, RowMin: -16, RowMax: 16},
	}
	chosenRef, mv, _ := GetEstimatedPredInter(in)
	if chosenRef != RefAlt {
		t.Errorf("chosenRef: got %v want ALTREF", chosenRef)
	}
	// With src == altRef, the int-pro motion search must return mv = (0, 0).
	if mv.Row != 0 || mv.Col != 0 {
		t.Errorf("altref identity MV: got (%d,%d) want (0,0)", mv.Row, mv.Col)
	}
}

// TestVP9GetEstimatedPredInterGoldenBeatsLast validates the GOLDEN
// pick path: with cpi->oxcf.speed<8 and a GOLDEN ref that has lower
// SAD than the int-pro LAST-frame estimate, libvpx switches to
// GOLDEN and zeros the MV.
//
// Setup: src exactly matches GOLDEN at (0,0); LAST is fully
// mismatched (constant 0xFF), so int_pro will return a non-trivial
// SAD against LAST while y_sad_g = 0.
func TestVP9GetEstimatedPredInterGoldenBeatsLast(t *testing.T) {
	stride := 256
	src := make([]uint8, stride*stride)
	goldenRef := make([]uint8, stride*stride)
	lastRef := make([]uint8, stride*stride)
	originX, originY := 128, 128
	for y := -64; y < 128; y++ {
		for x := -64; x < 128; x++ {
			v := uint8((y + x*3) & 0xFF)
			src[(originY+y)*stride+(originX+x)] = v
			goldenRef[(originY+y)*stride+(originX+x)] = v
			lastRef[(originY+y)*stride+(originX+x)] = 0xFF // contrast.
		}
	}
	srcOff := originY*stride + originX
	in := &GetEstimatedPredInterInput{
		Bsize:           common.Block64x64,
		Src:             src,
		SrcOff:          srcOff,
		SrcStride:       stride,
		LastRef:         lastRef,
		LastRefOff:      srcOff,
		LastRefStride:   stride,
		HaveGolden:      true,
		GoldenRef:       goldenRef,
		GoldenRefOff:    srcOff,
		GoldenRefStride: stride,
		Speed:           7,    // < 8 enables the golden probe.
		RefFlagsGoldOn:  true, // VP9_GOLD_FLAG set.
		MvLimits:        MvLimits{ColMin: -16, ColMax: 16, RowMin: -16, RowMax: 16},
	}
	chosenRef, mv, _ := GetEstimatedPredInter(in)
	if chosenRef != RefGolden {
		t.Errorf("chosenRef: got %v want GOLDEN", chosenRef)
	}
	// libvpx zeroes mi->mv[0] when switching to GOLDEN.
	if mv.Row != 0 || mv.Col != 0 {
		t.Errorf("golden-switch MV: got (%d,%d) want (0,0)", mv.Row, mv.Col)
	}
}

// TestVP9GetEstimatedPredInterSpeed8SuppressesGolden validates the
// speed<8 gate on the GOLDEN probe (libvpx vp9_encodeframe.c:5128).
// At speed=8 the golden probe is suppressed even when GOLDEN matches
// the source perfectly: chosenRef stays at LAST.
func TestVP9GetEstimatedPredInterSpeed8SuppressesGolden(t *testing.T) {
	stride := 256
	src := make([]uint8, stride*stride)
	goldenRef := make([]uint8, stride*stride)
	lastRef := make([]uint8, stride*stride)
	originX, originY := 128, 128
	for y := -64; y < 128; y++ {
		for x := -64; x < 128; x++ {
			v := uint8((y + x*3) & 0xFF)
			src[(originY+y)*stride+(originX+x)] = v
			goldenRef[(originY+y)*stride+(originX+x)] = v
			lastRef[(originY+y)*stride+(originX+x)] = v
		}
	}
	srcOff := originY*stride + originX
	in := &GetEstimatedPredInterInput{
		Bsize:           common.Block64x64,
		Src:             src,
		SrcOff:          srcOff,
		SrcStride:       stride,
		LastRef:         lastRef,
		LastRefOff:      srcOff,
		LastRefStride:   stride,
		HaveGolden:      true,
		GoldenRef:       goldenRef,
		GoldenRefOff:    srcOff,
		GoldenRefStride: stride,
		Speed:           8, // gate disabled.
		RefFlagsGoldOn:  true,
		MvLimits:        MvLimits{ColMin: -16, ColMax: 16, RowMin: -16, RowMax: 16},
	}
	chosenRef, _, _ := GetEstimatedPredInter(in)
	if chosenRef != RefLast {
		t.Errorf("speed=8 chosenRef: got %v want LAST (golden probe suppressed)", chosenRef)
	}
}

// TestVP9GetEstimatedPredOrchestratorLastPath runs the full
// GetEstimatedPred orchestrator and verifies that the est_pred
// buffer matches the LAST reference window when src == last_ref.
func TestVP9GetEstimatedPredOrchestratorLastPath(t *testing.T) {
	stride := 256
	frame := make([]uint8, stride*stride)
	originX, originY := 128, 128
	for y := -64; y < 128; y++ {
		for x := -64; x < 128; x++ {
			frame[(originY+y)*stride+(originX+x)] = uint8((y + x*3) & 0xFF)
		}
	}
	srcOff := originY*stride + originX
	in := &GetEstimatedPredInterInput{
		Bsize:         common.Block64x64,
		Src:           frame,
		SrcOff:        srcOff,
		SrcStride:     stride,
		LastRef:       frame,
		LastRefOff:    srcOff,
		LastRefStride: stride,
		HaveGolden:    false,
		HaveAltRef:    false,
		Speed:         8,
		MvLimits:      MvLimits{ColMin: -16, ColMax: 16, RowMin: -16, RowMax: 16},
	}
	estPred := make([]uint8, 64*64)
	for i := range estPred {
		estPred[i] = 0xAA
	}
	chosenRef, mv, _ := GetEstimatedPred(false, in, estPred)
	if chosenRef != RefLast {
		t.Errorf("chosenRef: got %v want LAST", chosenRef)
	}
	if mv.Row != 0 || mv.Col != 0 {
		t.Errorf("identity MV: got (%d,%d) want (0,0)", mv.Row, mv.Col)
	}
	for y := range 64 {
		for x := range 64 {
			want := frame[(originY+y)*stride+(originX+x)]
			got := estPred[y*64+x]
			if got != want {
				t.Fatalf("est[%d,%d] = %d, want %d", y, x, got, want)
			}
		}
	}
}

func TestVP9GetEstimatedPredOrchestratorAppliesIntProMVScale(t *testing.T) {
	stride := 256
	src := make([]uint8, stride*stride)
	ref := make([]uint8, stride*stride)
	originX, originY := 96, 96
	shiftX := 16
	for y := -64; y < 128; y++ {
		for x := -80; x < 160; x++ {
			ref[(originY+y)*stride+(originX+x)] = uint8((x*17 + y*29 + x*y) & 0xFF)
		}
	}
	for y := range 64 {
		for x := range 64 {
			src[(originY+y)*stride+(originX+x)] = ref[(originY+y)*stride+(originX+x+shiftX)]
		}
	}
	srcOff := originY*stride + originX
	in := &GetEstimatedPredInterInput{
		Bsize:         common.Block64x64,
		Src:           src,
		SrcOff:        srcOff,
		SrcStride:     stride,
		LastRef:       ref,
		LastRefOff:    srcOff,
		LastRefStride: stride,
		Speed:         8,
		MvLimits:      MvLimits{ColMin: -16, ColMax: 16, RowMin: -16, RowMax: 16},
	}
	estPred := make([]uint8, 64*64)
	chosenRef, mv, _ := GetEstimatedPred(false, in, estPred)
	if chosenRef != RefLast {
		t.Fatalf("chosenRef: got %v want LAST", chosenRef)
	}
	if mv.Row != 0 || mv.Col != int16(shiftX*8) {
		t.Fatalf("int-pro MV: got (%d,%d) want (0,%d)", mv.Row, mv.Col, shiftX*8)
	}
	for y := range 64 {
		for x := range 64 {
			want := src[(originY+y)*stride+(originX+x)]
			got := estPred[y*64+x]
			if got != want {
				t.Fatalf("est[%d,%d] = %d, want %d", y, x, got, want)
			}
		}
	}
}
