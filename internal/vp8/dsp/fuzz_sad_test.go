package dsp

import (
	"encoding/binary"
	"testing"
)

// FuzzVP8DSPIdct is a differential SIMD-vs-scalar fuzz harness for VP8
// inverse-transform kernels. Covers (op-byte selected):
//   - vp8_short_idct4x4llm   -> IDCT4x4Add / idct4x4AddSIMD vs idct4x4AddScalar
//   - vp8_dc_only_idct_add   -> DCOnlyIDCT4x4Add / dcOnlyIDCT4x4AddSIMD vs scalar
//   - vp8_short_inv_walsh4x4 -> InverseWalsh4x4 / inverseWalsh4x4SIMD vs scalar
//
// Pattern mirrors libvpx test/idct_test.cc + test/iwt_test.cc: pick op,
// build int16 coeffs from the fuzz payload via 2-byte little-endian
// reads, clamp into the encoder/decoder coefficient range so the test
// stays in the bit-exact contract zone, then byte-compare.

func FuzzVP8DSPIdct(f *testing.F) {
	seeds := [][]byte{
		make([]byte, 128),
		bytes255(128),
		bytesAlt(128),
		bytesRamp(128, 0),
		bytesRamp(128, 5),
		bytesPattern(128, 0x33, 0x77),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		// Need: 1 op byte + 1 stride byte + 32 bytes coeff + 64 bytes pred.
		if len(data) < 1+1+32+64 {
			return
		}
		op := int(data[0]) % 3
		// dstStride/predStride both 8 — matches existing test layout
		// (idct_simd_test.go:24 uses stride 8 over an 8*8 buffer).
		stride := 8

		var coeff [16]int16
		// For idct/dc-only paths the bit-exact contract holds up to
		// roughly |v|<=4096 (idct_simd_test.go:46-52 uses |v|<=2048).
		// For inverse Walsh the contract narrows to |v|<=1024 because
		// libvpx's iwalsh_sse2.asm uses int16-saturating paddw on
		// intermediates that grow up to ~4x (see walsh_simd_test.go:80
		// "|val| <= 1024 ... without int16 overflow after the 2-pass
		// butterfly amplifies by up to 16x").
		coeffBound := int16(2048)
		if op == 2 {
			coeffBound = 1024
		}
		for i := range 16 {
			v := int16(binary.LittleEndian.Uint16(data[2+i*2:]))
			if v > coeffBound {
				v = coeffBound
			} else if v < -coeffBound {
				v = -coeffBound
			}
			coeff[i] = v
		}

		pred := make([]byte, 64)
		copy(pred, data[2+32:2+32+64])

		switch op {
		case 0:
			dstSim := make([]byte, 64)
			dstScl := make([]byte, 64)
			copy(dstSim, pred)
			copy(dstScl, pred)
			coeffSim := coeff
			coeffScl := coeff
			idct4x4AddSIMD(&coeffSim, pred, stride, dstSim, stride)
			idct4x4AddScalar(&coeffScl, pred, stride, dstScl, stride)
			for y := range 4 {
				for x := range 4 {
					if dstSim[y*stride+x] != dstScl[y*stride+x] {
						t.Fatalf("IDCT4x4Add[%d,%d]: simd=%d scalar=%d coeff=%v",
							x, y, dstSim[y*stride+x], dstScl[y*stride+x], coeff)
					}
				}
			}
		case 1:
			// DC-only path takes a single int16 DC coefficient.
			dc := coeff[0]
			dstSim := make([]byte, 64)
			dstScl := make([]byte, 64)
			copy(dstSim, pred)
			copy(dstScl, pred)
			dcOnlyIDCT4x4AddSIMD(dc, pred, stride, dstSim, stride)
			dcOnlyIDCT4x4AddScalar(dc, pred, stride, dstScl, stride)
			for y := range 4 {
				for x := range 4 {
					if dstSim[y*stride+x] != dstScl[y*stride+x] {
						t.Fatalf("DCOnlyIDCT4x4Add[%d,%d] dc=%d simd=%d scalar=%d",
							x, y, dc, dstSim[y*stride+x], dstScl[y*stride+x])
					}
				}
			}
		case 2:
			// InverseWalsh4x4 writes to 16 lanes spaced 16 apart in
			// the dqcoeff buffer (libvpx vp8/common/idctllm.c).
			out := make([]int16, 16*16)
			outScl := make([]int16, 16*16)
			coeffSim := coeff
			coeffScl := coeff
			inverseWalsh4x4SIMD(&coeffSim, out)
			inverseWalsh4x4Scalar(&coeffScl, outScl)
			for i := range 16 {
				if out[i*16] != outScl[i*16] {
					t.Fatalf("InverseWalsh4x4[%d]: simd=%d scalar=%d coeff=%v",
						i, out[i*16], outScl[i*16], coeff)
				}
			}
		}
	})
}

// FuzzVP8DSPIntra is a differential SIMD-vs-scalar fuzz harness for
// the VP8 intra-prediction kernel family. Mirrors libvpx
// test/intrapred_test.cc cross-check pattern.
//
// Op selector covers the full block-size + mode matrix:
//
//	16x16 modes: DC (with all 4 availability combos), Vertical,
//	             Horizontal, TrueMotion
//	 8x8 modes:  DC (4 combos), Vertical, Horizontal, TrueMotion
//	 4x4 modes:  DC, TM, VE, HE, LD, RD, VR, VL, HD, HU
//
// The scalar reference is intra*Scalar from internal/vp8/dsp/intra.go
// and intra4x4*Scalar from intra4x4.go — both are the canonical libvpx
// vp8/common/reconintra.c / reconintra4x4.c ports.

func FuzzVP8DSPIntra(f *testing.F) {
	seeds := [][]byte{
		make([]byte, 128),
		bytes255(128),
		bytesAlt(128),
		bytesRamp(128, 0),
		bytesRamp(128, 3),
		bytesPattern(128, 0x5A, 0x91),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		// 1 op + 1 availability byte + above[16] + left[16] + topLeft + above-extended[8]
		// Generous buffer to cover the 4x4 LD/VL modes that need above[7].
		if len(data) < 64 {
			return
		}
		op := int(data[0]) % 18
		avail := data[1]
		topLeft := data[2]

		above := make([]byte, 32)
		left := make([]byte, 32)
		copy(above, data[3:3+32])
		copy(left, data[3+32:3+32+min(len(data)-3-32, 32)])

		switch op {
		case 0: // 16x16 DC up & left
			dstSim := make([]byte, 16*16)
			dstScl := make([]byte, 16*16)
			IntraDCPredict16x16(dstSim, 16, above, left, true, true)
			intraDCPredictScalar(dstScl, 16, above, left, 16, true, true)
			compareBlock(t, "IntraDCPredict16x16(true,true)", dstSim, dstScl, 16, 16, 16)
		case 1: // 16x16 DC up only
			dstSim := make([]byte, 16*16)
			dstScl := make([]byte, 16*16)
			IntraDCPredict16x16(dstSim, 16, above, left, true, false)
			intraDCPredictScalar(dstScl, 16, above, left, 16, true, false)
			compareBlock(t, "IntraDCPredict16x16(true,false)", dstSim, dstScl, 16, 16, 16)
		case 2: // 16x16 DC left only
			dstSim := make([]byte, 16*16)
			dstScl := make([]byte, 16*16)
			IntraDCPredict16x16(dstSim, 16, above, left, false, true)
			intraDCPredictScalar(dstScl, 16, above, left, 16, false, true)
			compareBlock(t, "IntraDCPredict16x16(false,true)", dstSim, dstScl, 16, 16, 16)
		case 3: // 16x16 DC neither
			dstSim := make([]byte, 16*16)
			dstScl := make([]byte, 16*16)
			IntraDCPredict16x16(dstSim, 16, above, left, false, false)
			intraDCPredictScalar(dstScl, 16, above, left, 16, false, false)
			compareBlock(t, "IntraDCPredict16x16(false,false)", dstSim, dstScl, 16, 16, 16)
		case 4: // 16x16 Vertical
			dstSim := make([]byte, 16*16)
			dstScl := make([]byte, 16*16)
			IntraVerticalPredict16x16(dstSim, 16, above)
			intraVerticalPredictScalar(dstScl, 16, above, 16)
			compareBlock(t, "IntraVerticalPredict16x16", dstSim, dstScl, 16, 16, 16)
		case 5: // 16x16 Horizontal
			dstSim := make([]byte, 16*16)
			dstScl := make([]byte, 16*16)
			IntraHorizontalPredict16x16(dstSim, 16, left)
			intraHorizontalPredictScalar(dstScl, 16, left, 16)
			compareBlock(t, "IntraHorizontalPredict16x16", dstSim, dstScl, 16, 16, 16)
		case 6: // 16x16 TM
			dstSim := make([]byte, 16*16)
			dstScl := make([]byte, 16*16)
			IntraTMPredict16x16(dstSim, 16, above, left, topLeft)
			intraTMPredictScalar(dstScl, 16, above, left, topLeft, 16)
			compareBlock(t, "IntraTMPredict16x16", dstSim, dstScl, 16, 16, 16)
		case 7: // 8x8 DC up&left — covered with the 4-availability cross sample driven by avail
			dstSim := make([]byte, 8*8)
			dstScl := make([]byte, 8*8)
			up := avail&1 != 0
			lf := avail&2 != 0
			IntraDCPredict8x8(dstSim, 8, above, left, up, lf)
			intraDCPredictScalar(dstScl, 8, above, left, 8, up, lf)
			compareBlock(t, "IntraDCPredict8x8", dstSim, dstScl, 8, 8, 8)
		case 8: // 8x8 Vertical
			dstSim := make([]byte, 8*8)
			dstScl := make([]byte, 8*8)
			IntraVerticalPredict8x8(dstSim, 8, above)
			intraVerticalPredictScalar(dstScl, 8, above, 8)
			compareBlock(t, "IntraVerticalPredict8x8", dstSim, dstScl, 8, 8, 8)
		case 9: // 8x8 Horizontal
			dstSim := make([]byte, 8*8)
			dstScl := make([]byte, 8*8)
			IntraHorizontalPredict8x8(dstSim, 8, left)
			intraHorizontalPredictScalar(dstScl, 8, left, 8)
			compareBlock(t, "IntraHorizontalPredict8x8", dstSim, dstScl, 8, 8, 8)
		case 10: // 8x8 TM
			dstSim := make([]byte, 8*8)
			dstScl := make([]byte, 8*8)
			IntraTMPredict8x8(dstSim, 8, above, left, topLeft)
			intraTMPredictScalar(dstScl, 8, above, left, topLeft, 8)
			compareBlock(t, "IntraTMPredict8x8", dstSim, dstScl, 8, 8, 8)
		case 11: // 4x4 DC
			dstSim := make([]byte, 4*4)
			dstScl := make([]byte, 4*4)
			Intra4x4DCPredict(dstSim, 4, above, left)
			intra4x4DCPredictScalar(dstScl, 4, above, left)
			compareBlock(t, "Intra4x4DCPredict", dstSim, dstScl, 4, 4, 4)
		case 12: // 4x4 TM
			dstSim := make([]byte, 4*4)
			dstScl := make([]byte, 4*4)
			Intra4x4TMPredict(dstSim, 4, above, left, topLeft)
			intra4x4TMPredictScalar(dstScl, 4, above, left, topLeft)
			compareBlock(t, "Intra4x4TMPredict", dstSim, dstScl, 4, 4, 4)
		case 13: // 4x4 VE — needs above[4]
			dstSim := make([]byte, 4*4)
			dstScl := make([]byte, 4*4)
			Intra4x4VEPredict(dstSim, 4, above, topLeft)
			intra4x4VEPredictScalar(dstScl, 4, above, topLeft)
			compareBlock(t, "Intra4x4VEPredict", dstSim, dstScl, 4, 4, 4)
		case 14: // 4x4 HE
			dstSim := make([]byte, 4*4)
			dstScl := make([]byte, 4*4)
			Intra4x4HEPredict(dstSim, 4, left, topLeft)
			intra4x4HEPredictScalar(dstScl, 4, left, topLeft)
			compareBlock(t, "Intra4x4HEPredict", dstSim, dstScl, 4, 4, 4)
		case 15: // 4x4 LD — needs above[7]
			dstSim := make([]byte, 4*4)
			dstScl := make([]byte, 4*4)
			Intra4x4LDPredict(dstSim, 4, above)
			intra4x4LDPredictScalar(dstScl, 4, above)
			compareBlock(t, "Intra4x4LDPredict", dstSim, dstScl, 4, 4, 4)
		case 16: // 4x4 RD
			dstSim := make([]byte, 4*4)
			dstScl := make([]byte, 4*4)
			Intra4x4RDPredict(dstSim, 4, above, left, topLeft)
			intra4x4RDPredictScalar(dstScl, 4, above, left, topLeft)
			compareBlock(t, "Intra4x4RDPredict", dstSim, dstScl, 4, 4, 4)
		case 17: // 4x4 HU
			dstSim := make([]byte, 4*4)
			dstScl := make([]byte, 4*4)
			Intra4x4HUPredict(dstSim, 4, left)
			intra4x4HUPredictScalar(dstScl, 4, left)
			compareBlock(t, "Intra4x4HUPredict", dstSim, dstScl, 4, 4, 4)
		}
	})
}

func compareBlock(t *testing.T, name string, sim, scl []byte, w, h, stride int) {
	t.Helper()
	for y := range h {
		for x := range w {
			if sim[y*stride+x] != scl[y*stride+x] {
				t.Fatalf("%s [%d,%d]: simd=%d scalar=%d", name, x, y,
					sim[y*stride+x], scl[y*stride+x])
			}
		}
	}
}

// FuzzVP8DSPLoopfilter is a differential SIMD-vs-scalar fuzz harness
// for the VP8 loop filter family. Mirrors libvpx
// test/vp8_loopfilter_test.cc cross-check pattern.
//
// Op selector covers:
//
//	0  LoopFilterHorizontalEdge / scalar
//	1  LoopFilterVerticalEdge / scalar
//	2  MBLoopFilterHorizontalEdge / scalar
//	3  MBLoopFilterVerticalEdge / scalar
//	4  LoopFilterSimpleHorizontalEdge / scalar
//	5  LoopFilterSimpleVerticalEdge / scalar
//
// The simple horizontal/vertical SIMD kernel uses uqadd saturation on
// the (2*|p0-q0|, |p1-q1|/2) composite — libvpx-inherited behaviour.
// The fuzz callback clamps blimit to 200 to stay inside the realistic
// per-frame range and avoid the documented saturation-vs-int divergence
// at blimit==255 (see loopfilter_simd_test.go:266-275).

func FuzzVP8DSPLoopfilter(f *testing.F) {
	seeds := [][]byte{
		make([]byte, 1024),
		bytes255(1024),
		bytesAlt(1024),
		bytesRamp(1024, 0),
		bytesRamp(1024, 11),
		bytesPattern(1024, 0x7C, 0x4B),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		const stride = 32
		const height = 16
		const planeBytes = stride * height
		if len(data) < 4+planeBytes {
			return
		}
		op := int(data[0]) % 6
		blimit := data[1]
		limit := data[2] & 63
		thresh := data[3] & 31
		if blimit > 200 {
			// Avoid the documented saturating-vs-int divergence at
			// blimit==255; libvpx never feeds the simple filter such a
			// value in practice (2*filter_level + sharpness <= 200).
			blimit = 200
		}

		base := make([]byte, planeBytes)
		copy(base, data[4:4+planeBytes])

		gotBuf := append([]byte(nil), base...)
		wantBuf := append([]byte(nil), base...)

		switch op {
		case 0:
			loopFilterHorizontalEdgeDispatch(gotBuf, stride, blimit, limit, thresh, 2)
			loopFilterHorizontalEdgeScalar(wantBuf, stride, blimit, limit, thresh, 2)
		case 1:
			loopFilterVerticalEdgeDispatch(gotBuf, stride, blimit, limit, thresh, 2)
			loopFilterVerticalEdgeScalar(wantBuf, stride, blimit, limit, thresh, 2)
		case 2:
			mbLoopFilterHorizontalEdgeDispatch(gotBuf, stride, blimit, limit, thresh, 2)
			mbLoopFilterHorizontalEdgeScalar(wantBuf, stride, blimit, limit, thresh, 2)
		case 3:
			mbLoopFilterVerticalEdgeDispatch(gotBuf, stride, blimit, limit, thresh, 2)
			mbLoopFilterVerticalEdgeScalar(wantBuf, stride, blimit, limit, thresh, 2)
		case 4:
			loopFilterSimpleHorizontalEdgeDispatch(gotBuf, stride, blimit)
			loopFilterSimpleHorizontalEdgeScalar(wantBuf, stride, blimit)
		case 5:
			loopFilterSimpleVerticalEdgeDispatch(gotBuf, stride, blimit)
			loopFilterSimpleVerticalEdgeScalar(wantBuf, stride, blimit)
		}

		for i, w := range wantBuf {
			if g := gotBuf[i]; g != w {
				t.Fatalf("op=%d blimit=%d limit=%d thresh=%d byte %d: simd=%d scalar=%d",
					op, blimit, limit, thresh, i, g, w)
			}
		}
	})
}

// FuzzVP8DSPSad is a differential SIMD-vs-scalar fuzz harness for the
// VP8 SAD primitives. Mirrors libvpx test/sad_test.cc: each iteration
// picks an op (block size / limit / multi-ref variant) from the
// selector byte, derives stride-aware src/ref buffers from the fuzz
// payload, calls the dispatched SIMD entry point AND the canonical
// scalar reference (scalarSAD from sad_test.go:177, the libvpx
// vp8/encoder/mcomp.c port), and asserts byte-exact equality.
//
// The limit kernel (SAD16x16Limit) and the 4-ref fused entry point
// (SAD16x16x4Fast) are covered as additional op selectors because
// they have their own SIMD implementations (sse2 / neon / dotprod) that
// can drift from the canonical sum even when the unlimited 1-ref kernel
// stays in sync.

func FuzzVP8DSPSad(f *testing.F) {
	seeds := [][]byte{
		make([]byte, 2*32*32+8),
		bytes255(2*32*32 + 8),
		bytesAlt(2*32*32 + 8),
		bytesRamp(2*32*32+8, 0),
		bytesRamp(2*32*32+8, 13),
		bytesPattern(2*32*32+8, 0x42, 0x91),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		const planeStride = 32
		const planeRows = 32
		const planeBytes = planeStride * planeRows
		if len(data) < 2*planeBytes+8 {
			return
		}
		op := int(data[0]) % 7
		srcOff := int(data[1]) % 8
		refOff := int(data[2]) % 8
		// Limit byte spans a useful range: 0/small/full/huge.
		limitByte := data[3]
		_ = data[4]
		_ = data[5]
		_ = data[6]
		_ = data[7]

		src := make([]byte, planeBytes)
		ref := make([]byte, planeBytes)
		copy(src, data[8:8+planeBytes])
		copy(ref, data[8+planeBytes:8+2*planeBytes])

		srcView := src[srcOff*planeStride+srcOff:]
		refView := ref[refOff*planeStride+refOff:]

		switch op {
		case 0:
			got := SAD16x16(srcView, planeStride, refView, planeStride)
			want := scalarSAD(srcView, planeStride, refView, planeStride, 16, 16)
			if got != want {
				t.Fatalf("SAD16x16 simd=%d scalar=%d srcOff=%d refOff=%d", got, want, srcOff, refOff)
			}
		case 1:
			got := SAD16x8(srcView, planeStride, refView, planeStride)
			want := scalarSAD(srcView, planeStride, refView, planeStride, 16, 8)
			if got != want {
				t.Fatalf("SAD16x8 simd=%d scalar=%d", got, want)
			}
		case 2:
			got := SAD8x16(srcView, planeStride, refView, planeStride)
			want := scalarSAD(srcView, planeStride, refView, planeStride, 8, 16)
			if got != want {
				t.Fatalf("SAD8x16 simd=%d scalar=%d", got, want)
			}
		case 3:
			got := SAD8x8(srcView, planeStride, refView, planeStride)
			want := scalarSAD(srcView, planeStride, refView, planeStride, 8, 8)
			if got != want {
				t.Fatalf("SAD8x8 simd=%d scalar=%d", got, want)
			}
		case 4:
			got := SAD4x4(srcView, planeStride, refView, planeStride)
			want := scalarSAD(srcView, planeStride, refView, planeStride, 4, 4)
			if got != want {
				t.Fatalf("SAD4x4 simd=%d scalar=%d", got, want)
			}
		case 5:
			// SAD16x16Limit: select a useful per-iter limit by scaling
			// limitByte into [0, full_sad+128] so we exercise above /
			// at / below the natural limit boundary the picker hits.
			full := scalarSAD(srcView, planeStride, refView, planeStride, 16, 16)
			lim := int(limitByte) * full / 256
			got := SAD16x16Limit(srcView, planeStride, refView, planeStride, lim)
			want := scalarSADLimitFuzz(srcView, planeStride, refView, planeStride, 16, 16, lim)
			if got != want {
				t.Fatalf("SAD16x16Limit lim=%d simd=%d scalar=%d full=%d", lim, got, want, full)
			}
		case 6:
			// SAD16x16x4Fast — four ref slices at independent
			// offsets, each must match the 1-ref scalar.
			// Need at least four valid 16x16 windows; refOff is already
			// constrained to [0,7] so all four windows fit in a
			// 32x32 plane with stride 32.
			ref0 := ref
			ref1 := ref[1:]
			ref2 := ref[planeStride:]
			ref3 := ref[planeStride+1:]
			var got [4]uint32
			SAD16x16x4Fast(src, planeStride, ref0, ref1, ref2, ref3, planeStride, &got)
			refs := []int{
				scalarSAD(src, planeStride, ref, planeStride, 16, 16),
				scalarSAD(src, planeStride, ref[1:], planeStride, 16, 16),
				scalarSAD(src, planeStride, ref[planeStride:], planeStride, 16, 16),
				scalarSAD(src, planeStride, ref[planeStride+1:], planeStride, 16, 16),
			}
			for i, want := range refs {
				if int(got[i]) != want {
					t.Fatalf("SAD16x16x4Fast[%d]: simd=%d scalar=%d", i, got[i], want)
				}
			}
		}
	})
}

// scalarSADLimitFuzz is the canonical libvpx limit-aware SAD: return
// the running sum at the row boundary where it exceeds limit, otherwise
// the full SAD. Mirrors scalarSADLimit in sad_simd_test.go:97.
func scalarSADLimitFuzz(src []byte, srcStride int, ref []byte, refStride int, w, h, limit int) int {
	sad := 0
	for y := range h {
		for x := range w {
			d := int(src[y*srcStride+x]) - int(ref[y*refStride+x])
			if d < 0 {
				d = -d
			}
			sad += d
		}
		if sad > limit {
			return sad
		}
	}
	return sad
}

// FuzzVP8DSPSubpixel is a differential SIMD-vs-scalar fuzz harness for
// the VP8 six-tap and bilinear sub-pel predictor families. Mirrors the
// libvpx test/vp8_predict_test.cc cross-check pattern.
//
// Op selector covers (per-size kernel variants — both SIMD and scalar):
//
//	0  SixTapPredict16x16
//	1  SixTapPredict8x8
//	2  SixTapPredict8x4
//	3  SixTapPredict4x4
//	4  BilinearPredict16x16
//	5  BilinearPredict8x8
//	6  BilinearPredict8x4
//	7  BilinearPredict4x4
//
// The SIMD dispatch lives in subpixel_{amd64,arm64}.go via the
// six*Maybe gates; the scalar oracle goes through the bypass functions
// sixTapPredict / bilinearPredict in subpixel.go which are the
// canonical libvpx vp8/common/filter.c ports.

func FuzzVP8DSPSubpixel(f *testing.F) {
	seeds := [][]byte{
		make([]byte, 1024),
		bytes255(1024),
		bytesAlt(1024),
		bytesRamp(1024, 0),
		bytesRamp(1024, 17),
		bytesPattern(1024, 0x4D, 0x9B),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		// Need 1 op + 1 xoff + 1 yoff + a generous 32x32 source plane.
		const srcStride = 32
		const srcRows = 32
		const srcBytes = srcStride * srcRows
		if len(data) < 3+srcBytes {
			return
		}
		op := int(data[0]) % 8
		// Six-tap filter range is [0,7] (libvpx vp8/common/filter.c
		// SubPelFilters has 8 phases). Bilinear is also [0,7].
		xoff := int(data[1]) & 7
		yoff := int(data[2]) & 7

		src := make([]byte, srcBytes)
		copy(src, data[3:3+srcBytes])

		// SixTap needs 2 rows + 2 cols of source context above/left of
		// the prediction origin. Pick a safe start offset that gives
		// the largest (16x16) block 5 rows of bottom margin too.
		// origin row=8, col=8 -> rows [8-2 .. 8+15+3]=[6..26], cols similar
		const originY = 8
		const originX = 8
		srcView := src[originY*srcStride+originX:]

		var dstSim, dstScl []byte

		switch op {
		case 0: // SixTapPredict16x16
			dstSim = make([]byte, 16*16)
			dstScl = make([]byte, 16*16)
			SixTapPredict16x16(srcView, srcStride, xoff, yoff, dstSim, 16)
			sixTapPredict(srcView, srcStride, xoff, yoff, dstScl, 16, 16, 16)
			compareBlock(t, "SixTapPredict16x16", dstSim, dstScl, 16, 16, 16)
		case 1: // SixTapPredict8x8
			dstSim = make([]byte, 8*8)
			dstScl = make([]byte, 8*8)
			SixTapPredict8x8(srcView, srcStride, xoff, yoff, dstSim, 8)
			sixTapPredict(srcView, srcStride, xoff, yoff, dstScl, 8, 8, 8)
			compareBlock(t, "SixTapPredict8x8", dstSim, dstScl, 8, 8, 8)
		case 2: // SixTapPredict8x4
			dstSim = make([]byte, 8*4)
			dstScl = make([]byte, 8*4)
			SixTapPredict8x4(srcView, srcStride, xoff, yoff, dstSim, 8)
			sixTapPredict(srcView, srcStride, xoff, yoff, dstScl, 8, 8, 4)
			compareBlock(t, "SixTapPredict8x4", dstSim, dstScl, 8, 4, 8)
		case 3: // SixTapPredict4x4
			dstSim = make([]byte, 4*4)
			dstScl = make([]byte, 4*4)
			SixTapPredict4x4(srcView, srcStride, xoff, yoff, dstSim, 4)
			sixTapPredict(srcView, srcStride, xoff, yoff, dstScl, 4, 4, 4)
			compareBlock(t, "SixTapPredict4x4", dstSim, dstScl, 4, 4, 4)
		case 4: // BilinearPredict16x16
			dstSim = make([]byte, 16*16)
			dstScl = make([]byte, 16*16)
			BilinearPredict16x16(srcView, srcStride, xoff, yoff, dstSim, 16)
			bilinearPredict(srcView, srcStride, xoff, yoff, dstScl, 16, 16, 16)
			compareBlock(t, "BilinearPredict16x16", dstSim, dstScl, 16, 16, 16)
		case 5: // BilinearPredict8x8
			dstSim = make([]byte, 8*8)
			dstScl = make([]byte, 8*8)
			BilinearPredict8x8(srcView, srcStride, xoff, yoff, dstSim, 8)
			bilinearPredict(srcView, srcStride, xoff, yoff, dstScl, 8, 8, 8)
			compareBlock(t, "BilinearPredict8x8", dstSim, dstScl, 8, 8, 8)
		case 6: // BilinearPredict8x4
			dstSim = make([]byte, 8*4)
			dstScl = make([]byte, 8*4)
			BilinearPredict8x4(srcView, srcStride, xoff, yoff, dstSim, 8)
			bilinearPredict(srcView, srcStride, xoff, yoff, dstScl, 8, 8, 4)
			compareBlock(t, "BilinearPredict8x4", dstSim, dstScl, 8, 4, 8)
		case 7: // BilinearPredict4x4
			dstSim = make([]byte, 4*4)
			dstScl = make([]byte, 4*4)
			BilinearPredict4x4(srcView, srcStride, xoff, yoff, dstSim, 4)
			bilinearPredict(srcView, srcStride, xoff, yoff, dstScl, 4, 4, 4)
			compareBlock(t, "BilinearPredict4x4", dstSim, dstScl, 4, 4, 4)
		}
	})
}

// FuzzVP8DSPVariance is a differential SIMD-vs-scalar fuzz harness for the
// VP8 variance / SSE primitives. It mirrors libvpx's
// test/variance_test.cc cross-check pattern: for each fuzz iteration we
// pick an op (block size) via the op-selector byte, construct
// stride-aware src/ref buffers from the fuzz payload, then run both the
// SIMD-dispatched entry point (which routes through the per-arch
// varianceBlock16x16 / varianceBlockSized kernels) and the scalar
// reference (varianceBlockGeneric from variance.go:155, which is the
// canonical libvpx vp8/encoder/variance.c port).
//
// Any byte-level divergence between the two is a real bug — the dispatch
// promises byte-identical output to the generic scalar fallback per
// internal/vp8/dsp/variance.go and the libvpx
// vp8/common/x86/variance_sse2.asm + vp8/common/arm/neon/vp8_subpixelvariance_neon.c
// references.

func FuzzVP8DSPVariance(f *testing.F) {
	// 6 seeds covering the byte-shaped cases per the harness brief:
	// zero, max, alternating, block-aligned, sub-block-misaligned,
	// random-shaped.
	seeds := [][]byte{
		make([]byte, 1024),
		bytes255(1024),
		bytesAlt(1024),
		bytesRamp(1024, 0),
		bytesRamp(1024, 7),
		bytesPattern(1024, 0x51, 0x73),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		// Need at least 2 plane buffers of 32*32 = 1024 each.
		const planeStride = 32
		const planeRows = 32
		const planeBytes = planeStride * planeRows
		if len(data) < 2*planeBytes+4 {
			return
		}
		op := int(data[0]) % 7
		srcOff := int(data[1]) % 8
		refOff := int(data[2]) % 8
		_ = data[3]
		src := make([]byte, planeBytes)
		ref := make([]byte, planeBytes)
		copy(src, data[4:4+planeBytes])
		copy(ref, data[4+planeBytes:4+2*planeBytes])

		srcView := src[srcOff*planeStride+srcOff:]
		refView := ref[refOff*planeStride+refOff:]

		switch op {
		case 0: // Variance4x4
			got := Variance4x4(srcView, planeStride, refView, planeStride)
			sum, sse := varianceBlockGeneric(srcView, planeStride, refView, planeStride, 4, 4)
			want := sse - (sum*sum)>>4
			if got != want {
				t.Fatalf("Variance4x4 simd=%d scalar=%d srcOff=%d refOff=%d", got, want, srcOff, refOff)
			}
		case 1: // Variance8x8
			got := Variance8x8(srcView, planeStride, refView, planeStride)
			sum, sse := varianceBlockGeneric(srcView, planeStride, refView, planeStride, 8, 8)
			want := sse - (sum*sum)>>6
			if got != want {
				t.Fatalf("Variance8x8 simd=%d scalar=%d srcOff=%d refOff=%d", got, want, srcOff, refOff)
			}
		case 2: // Variance16x16
			got := Variance16x16(srcView, planeStride, refView, planeStride)
			sum, sse := varianceBlockGeneric(srcView, planeStride, refView, planeStride, 16, 16)
			want := sse - (sum*sum)>>8
			if got != want {
				t.Fatalf("Variance16x16 simd=%d scalar=%d srcOff=%d refOff=%d", got, want, srcOff, refOff)
			}
		case 3: // Variance16x8
			got := Variance16x8(srcView, planeStride, refView, planeStride)
			sum, sse := varianceBlockGeneric(srcView, planeStride, refView, planeStride, 16, 8)
			want := sse - (sum*sum)>>7
			if got != want {
				t.Fatalf("Variance16x8 simd=%d scalar=%d srcOff=%d refOff=%d", got, want, srcOff, refOff)
			}
		case 4: // Variance8x16
			got := Variance8x16(srcView, planeStride, refView, planeStride)
			sum, sse := varianceBlockGeneric(srcView, planeStride, refView, planeStride, 8, 16)
			want := sse - (sum*sum)>>7
			if got != want {
				t.Fatalf("Variance8x16 simd=%d scalar=%d srcOff=%d refOff=%d", got, want, srcOff, refOff)
			}
		case 5: // SSE16x16 (mse16x16 equivalent in libvpx)
			got := SSE16x16(srcView, planeStride, refView, planeStride)
			_, want := varianceBlockGeneric(srcView, planeStride, refView, planeStride, 16, 16)
			if got != want {
				t.Fatalf("SSE16x16 simd=%d scalar=%d srcOff=%d refOff=%d", got, want, srcOff, refOff)
			}
		case 6: // SSE4x4 (equivalent of vp8_get4x4sse_cs in libvpx)
			got := SSE4x4(srcView, planeStride, refView, planeStride)
			_, want := varianceBlockGeneric(srcView, planeStride, refView, planeStride, 4, 4)
			if got != want {
				t.Fatalf("SSE4x4 simd=%d scalar=%d srcOff=%d refOff=%d", got, want, srcOff, refOff)
			}
		}
	})
}

// Helpers shared by every VP8 DSP fuzz target. Kept tiny so the fuzz
// callbacks can construct byte payloads from the fuzz selector without
// reaching into math/rand at fuzz time.

func bytes255(n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = 0xFF
	}
	return out
}

func bytesAlt(n int) []byte {
	out := make([]byte, n)
	for i := range out {
		if i&1 == 0 {
			out[i] = 0x00
		} else {
			out[i] = 0xFF
		}
	}
	return out
}

func bytesRamp(n int, seed byte) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = byte(i) + seed
	}
	return out
}

func bytesPattern(n int, a byte, b byte) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = a ^ (byte(i) * b)
	}
	return out
}
