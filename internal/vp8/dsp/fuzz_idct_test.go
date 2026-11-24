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
