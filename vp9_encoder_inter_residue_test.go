package govpx

import (
	"fmt"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
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

// TestVP9EncoderInterSceneCutLeafMatchesNonrdPicker pins the libvpx-faithful
// nonrd decision for a hard scene cut (black keyframe followed by a flat-gray
// 64x64 inter frame). The default govpx encoder runs cpu_used=8 REALTIME, where
// sf->use_nonrd_pick_mode == 1 and sf->max_intra_bsize == BLOCK_32X32
// (vp9_speed_features.c:571). The 64x64 leaf therefore never enters
// vp9_pick_inter_mode's intra fallback (gated by bsize <= max_intra_bsize at
// vp9_pickmode.c:2533), and the nonrd path performs no second intra re-decode
// (vp9_encodeframe.c::nonrd_pick_sb_modes:4422-4435). vpxenc v1.16.0 confirms
// the leaf is coded inter LAST NEARESTMV here, not intra.
func TestVP9EncoderInterSceneCutLeafMatchesNonrdPicker(t *testing.T) {
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
	if got := d.miGrid[0]; got.RefFrame[0] != vp9dec.LastFrame ||
		got.Mode != common.NearestMv {
		t.Fatalf("top-left inter-frame leaf = ref %d mode %d skip %d, want LastFrame/NearestMv (inter)",
			got.RefFrame[0], got.Mode, got.Skip)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after inter frame")
	}
	// The leaf is inter-predicted from the black keyframe plus a coded
	// residual, so the reconstructed flat-gray frame lands near (not exactly
	// at) 128. Allow a small lossy-reconstruction tolerance.
	assertVP9FilledFrameWithin(t, frame, width, height, 128, 128, 128, 8)
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

func TestGatherVP9TxResidualOverwritesActiveScratch(t *testing.T) {
	t.Run("in-bounds", func(t *testing.T) {
		for _, tx := range []common.TxSize{
			common.Tx4x4, common.Tx8x8, common.Tx16x16, common.Tx32x32,
		} {
			t.Run(fmt.Sprintf("tx%d", tx), func(t *testing.T) {
				var e VP9Encoder
				bs := 4 << uint(tx)
				srcW, srcH := bs+2, bs+3
				srcStride := srcW + 5
				dstStride := bs + 3
				src := make([]byte, srcStride*srcH)
				for i := range src {
					src[i] = byte((i*7 + 11) & 0xff)
				}
				dst := make([]byte, dstStride*bs)
				for i := range dst {
					dst[i] = byte((i*5 + 3) & 0xff)
				}
				for i := range e.residueScratch[:bs*bs] {
					e.residueScratch[i] = 0x7777
				}

				if !e.gatherVP9TxResidual(src, srcStride, srcW, srcH,
					dst, dstStride, 2, 3, tx) {
					t.Fatal("gatherVP9TxResidual returned false for non-zero in-bounds residue")
				}
				for y := range bs {
					for x := range bs {
						want := int16(int(src[(3+y)*srcStride+2+x]) -
							int(dst[y*dstStride+x]))
						if got := e.residueScratch[y*bs+x]; got != want {
							t.Fatalf("residue[%d,%d] = %d, want %d", y, x, got, want)
						}
					}
				}
			})
		}
	})

	t.Run("in-bounds-zero", func(t *testing.T) {
		var e VP9Encoder
		const bs, srcW, srcH, srcStride = 16, 20, 20, 23
		src := make([]byte, srcStride*srcH)
		dstStride := bs + 2
		dst := make([]byte, dstStride*bs)
		for y := range bs {
			for x := range bs {
				v := byte((y*17 + x*3 + 5) & 0xff)
				src[(2+y)*srcStride+3+x] = v
				dst[y*dstStride+x] = v
			}
		}
		for i := range e.residueScratch[:bs*bs] {
			e.residueScratch[i] = -12345
		}
		if e.gatherVP9TxResidual(src, srcStride, srcW, srcH, dst, dstStride,
			3, 2, common.Tx16x16) {
			t.Fatal("gatherVP9TxResidual returned true for zero in-bounds residue")
		}
		for i, got := range e.residueScratch[:bs*bs] {
			if got != 0 {
				t.Fatalf("residue[%d] = %d, want 0", i, got)
			}
		}
	})

	t.Run("clamped-edge", func(t *testing.T) {
		var e VP9Encoder
		const srcW, srcH, srcStride = 5, 5, 5
		src := make([]byte, srcStride*srcH)
		for i := range src {
			src[i] = byte((i*13 + 17) & 0xff)
		}
		dst := make([]byte, 8*8)
		for i := range dst {
			dst[i] = byte((i*9 + 1) & 0xff)
		}
		for i := range e.residueScratch[:64] {
			e.residueScratch[i] = -0x2222
		}

		if !e.gatherVP9TxResidual(src, srcStride, srcW, srcH, dst, 8,
			-2, 3, common.Tx8x8) {
			t.Fatal("gatherVP9TxResidual returned false for non-zero clamped residue")
		}
		for y := 0; y < 8; y++ {
			sy := clampVP9ResidualTestCoord(3+y, srcH)
			for x := 0; x < 8; x++ {
				sx := clampVP9ResidualTestCoord(-2+x, srcW)
				want := int16(int(src[sy*srcStride+sx]) - int(dst[y*8+x]))
				if got := e.residueScratch[y*8+x]; got != want {
					t.Fatalf("residue[%d,%d] = %d, want %d", y, x, got, want)
				}
			}
		}
	})
}

func clampVP9ResidualTestCoord(v, limit int) int {
	if v < 0 {
		return 0
	}
	if v >= limit {
		return limit - 1
	}
	return v
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
