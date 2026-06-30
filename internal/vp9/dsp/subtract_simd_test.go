package dsp

import (
	"fmt"
	"math/rand"
	"testing"
)

func scalarSubtractBlockTest(src []uint8, srcOff, srcStride int,
	pred []uint8, predOff, predStride int,
	out []int16, outOff, outStride int,
	w, h int,
) bool {
	diffMask := 0
	for y := range h {
		for x := range w {
			diff := int(src[srcOff+y*srcStride+x]) -
				int(pred[predOff+y*predStride+x])
			out[outOff+y*outStride+x] = int16(diff)
			diffMask |= diff
		}
	}
	return diffMask != 0
}

func TestSubtractBlockNonZeroMatchesScalar(t *testing.T) {
	rng := rand.New(rand.NewSource(0x5eed5eed))
	cases := []struct {
		name string
		w, h int
	}{
		{"4x4", 4, 4},
		{"8x8", 8, 8},
		{"16x16", 16, 16},
		{"32x32", 32, 32},
		{"8x4", 8, 4},
		{"16x8", 16, 8},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srcStride := tc.w + 17
			predStride := tc.w + 19
			outStride := tc.w
			srcOff := 3*srcStride + 5
			predOff := 2*predStride + 7
			outOff := 11
			src := make([]uint8, srcOff+(tc.h-1)*srcStride+tc.w+32)
			pred := make([]uint8, predOff+(tc.h-1)*predStride+tc.w+32)
			for i := range src {
				src[i] = uint8(rng.Intn(256))
			}
			for i := range pred {
				pred[i] = uint8(rng.Intn(256))
			}

			got := make([]int16, outOff+(tc.h-1)*outStride+tc.w+8)
			want := make([]int16, len(got))
			for i := range got {
				got[i] = 0x7777
				want[i] = 0x7777
			}

			gotNonZero, ok := SubtractBlockNonZero(src, srcOff, srcStride,
				pred, predOff, predStride, got, outOff, outStride,
				tc.w, tc.h)
			if !ok {
				t.Fatal("SubtractBlockNonZero returned !ok")
			}
			wantNonZero := scalarSubtractBlockTest(src, srcOff, srcStride,
				pred, predOff, predStride, want, outOff, outStride,
				tc.w, tc.h)
			if gotNonZero != wantNonZero {
				t.Fatalf("nonZero = %v, want %v", gotNonZero, wantNonZero)
			}
			for i := range got {
				if got[i] != want[i] {
					t.Fatalf("out[%d] = %d, want %d", i, got[i], want[i])
				}
			}
		})
	}
}

func TestSubtractBlockNonZeroZeroBlockOverwritesStaleOutput(t *testing.T) {
	for _, size := range []int{4, 8, 16, 32} {
		t.Run(fmt.Sprintf("%dx%d", size, size), func(t *testing.T) {
			stride := size + 3
			src := make([]uint8, stride*size)
			pred := make([]uint8, stride*size)
			for i := range src {
				src[i] = uint8((i*11 + 23) & 0xff)
				pred[i] = src[i]
			}
			out := make([]int16, size*size)
			for i := range out {
				out[i] = -12345
			}
			nonZero, ok := SubtractBlockNonZero(src, 0, stride,
				pred, 0, stride, out, 0, size, size, size)
			if !ok {
				t.Fatal("SubtractBlockNonZero returned !ok")
			}
			if nonZero {
				t.Fatal("nonZero=true for identical source and prediction")
			}
			for i, v := range out {
				if v != 0 {
					t.Fatalf("out[%d] = %d, want 0", i, v)
				}
			}
		})
	}
}

func TestSubtractBlockNonZeroStridedOutputFallback(t *testing.T) {
	const w, h = 8, 8
	const srcStride, predStride, outStride = 19, 21, 13
	src := make([]uint8, srcStride*h)
	pred := make([]uint8, predStride*h)
	for i := range src {
		src[i] = uint8((i*17 + 9) & 0xff)
	}
	for i := range pred {
		pred[i] = uint8((i*29 + 7) & 0xff)
	}
	got := make([]int16, outStride*h)
	want := make([]int16, outStride*h)
	for i := range got {
		got[i] = 0x5555
		want[i] = 0x5555
	}

	gotNonZero, ok := SubtractBlockNonZero(src, 0, srcStride,
		pred, 0, predStride, got, 0, outStride, w, h)
	if !ok {
		t.Fatal("SubtractBlockNonZero returned !ok")
	}
	wantNonZero := scalarSubtractBlockTest(src, 0, srcStride,
		pred, 0, predStride, want, 0, outStride, w, h)
	if gotNonZero != wantNonZero {
		t.Fatalf("nonZero = %v, want %v", gotNonZero, wantNonZero)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("out[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestSubtractBlockNonZeroRejectsOutOfBounds(t *testing.T) {
	src := make([]uint8, 16)
	pred := make([]uint8, 16)
	out := make([]int16, 16)
	if _, ok := SubtractBlockNonZero(src, 0, 4, pred, 0, 4, out, 0, 4, 8, 8); ok {
		t.Fatal("SubtractBlockNonZero accepted out-of-bounds windows")
	}
}

func BenchmarkSubtractBlockNonZero(b *testing.B) {
	for _, size := range []int{4, 8, 16, 32} {
		b.Run(fmt.Sprintf("%dx%d", size, size), func(b *testing.B) {
			stride := size + 16
			src := make([]uint8, stride*size)
			pred := make([]uint8, stride*size)
			for i := range src {
				src[i] = uint8((i*7 + 3) & 0xff)
				pred[i] = uint8((i*13 + 1) & 0xff)
			}
			out := make([]int16, size*size)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				SubtractBlockNonZero(src, 0, stride, pred, 0, stride,
					out, 0, size, size, size)
			}
		})
	}
}
