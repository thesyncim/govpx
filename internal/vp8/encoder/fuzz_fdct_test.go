package encoder

import (
	"encoding/binary"
	"testing"
)

// FuzzVP8DSPFdct is a differential SIMD-vs-scalar fuzz harness for the
// VP8 forward transform family. Mirrors libvpx test/fdct_test.cc and
// test/dct_test.cc cross-check patterns.
//
// Op selector covers:
//
//	0  ForwardDCT4x4   -> vp8_short_fdct4x4   (forwardDCT4x4SIMD vs forwardDCT4x4Scalar)
//	1  ForwardDCT8x4   -> vp8_short_fdct8x4   (composite of two 4x4 transforms)
//	2  ForwardWalsh4x4 -> vp8_short_walsh4x4 (forwardWalsh4x4SIMD vs forwardWalsh4x4Scalar)
//
// The scalar reference is the canonical libvpx vp8/encoder/dct.c port
// in dct.go; SIMD ports must produce byte-identical output for the
// encoder's residual range (|v|<=255 nominally, clamped to that here).

func FuzzVP8DSPFdct(f *testing.F) {
	seeds := [][]byte{
		make([]byte, 128),
		bytes255(128),
		bytesAlt(128),
		bytesRamp(128, 0),
		bytesRamp(128, 9),
		bytesPattern(128, 0x29, 0x53),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		// 1 op byte + 1 stride byte + 16 int16 inputs.
		if len(data) < 1+1+64 {
			return
		}
		op := int(data[0]) % 3
		// stride is bounded to [4, 16] in 4-step increments — matches
		// the encoder's typical 4-row stride patterns.
		strideSel := int(data[1]) % 3
		strides := [3]int{4, 8, 16}
		stride := strides[strideSel]

		// Build a row*stride int16 buffer; clamp residuals to
		// [-256, 255] to stay within the encoder's nominal range
		// (matches dct_simd_test.go:42-46).
		buf := make([]int16, stride*4)
		for i := range 16 {
			v := int16(binary.LittleEndian.Uint16(data[2+i*4:]))
			if v > 255 {
				v = 255
			} else if v < -256 {
				v = -256
			}
			row := i / 4
			col := i % 4
			buf[row*stride+col] = v
		}

		switch op {
		case 0:
			var simd, scalar [16]int16
			forwardDCT4x4SIMD(buf, stride, &simd)
			forwardDCT4x4Scalar(buf, stride, &scalar)
			if simd != scalar {
				t.Fatalf("ForwardDCT4x4 stride=%d simd=%v scalar=%v", stride, simd, scalar)
			}
		case 1:
			// ForwardDCT8x4 needs a wider input: 8 cols × 4 rows. We
			// already have stride*4 ints — if stride >= 8 there's room.
			if stride < 8 {
				return
			}
			var simd, scalar [32]int16
			ForwardDCT8x4(buf, stride, &simd)
			forwardDCT8x4Scalar(buf, stride, &scalar)
			if simd != scalar {
				t.Fatalf("ForwardDCT8x4 stride=%d simd=%v scalar=%v", stride, simd, scalar)
			}
		case 2:
			var simd, scalar [16]int16
			forwardWalsh4x4SIMD(buf, stride, &simd)
			forwardWalsh4x4Scalar(buf, stride, &scalar)
			if simd != scalar {
				t.Fatalf("ForwardWalsh4x4 stride=%d simd=%v scalar=%v", stride, simd, scalar)
			}
		}
	})
}

// forwardDCT8x4Scalar is the obvious scalar oracle for ForwardDCT8x4 —
// just two 4x4 forward DCTs run via the canonical scalar port.
func forwardDCT8x4Scalar(input []int16, stride int, output *[32]int16) {
	var left [16]int16
	var right [16]int16
	forwardDCT4x4Scalar(input, stride, &left)
	forwardDCT4x4Scalar(input[4:], stride, &right)
	copy(output[0:16], left[:])
	copy(output[16:32], right[:])
}

// Helpers — mirror internal/vp8/dsp/fuzz_variance_test.go.

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
