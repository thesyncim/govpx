package encoder

import (
	"math/rand"
	"testing"
)

// TestForwardDCT4x4BatchMatchesPerBlock verifies the batched 4x4 DCT
// dispatcher produces byte-identical output to the per-block
// ForwardDCT4x4 entry point for every realistic VP8 residual range.
// The libvpx reference (vp8_short_fdct8x4 looping vp8_short_fdct4x4)
// has the same per-block byte-identity invariant.
func TestForwardDCT4x4BatchMatchesPerBlock(t *testing.T) {
	r := rand.New(rand.NewSource(0xBADC0DE))
	for _, count := range []int{1, 2, 8, 16, 24, 25} {
		t.Run("", func(t *testing.T) {
			input := make([]int16, count*16)
			for i := range input {
				input[i] = int16(r.Intn(512) - 256)
			}
			outBatch := make([]int16, count*16)
			outPer := make([]int16, count*16)
			ForwardDCT4x4Batch(input, outBatch, count)
			for i := range count {
				var out [16]int16
				ForwardDCT4x4(input[i*16:i*16+16], 4, &out)
				copy(outPer[i*16:i*16+16], out[:])
			}
			for i := range outBatch {
				if outBatch[i] != outPer[i] {
					t.Fatalf("count=%d block=%d lane=%d: batch=%d per=%d", count, i/16, i%16, outBatch[i], outPer[i])
				}
			}
		})
	}
}

// TestForwardDCT4x4BatchSentinels covers the sentinel inputs the
// per-block tests already check, applied across multiple blocks at
// once so the asm loop's inter-iteration state is exercised.
func TestForwardDCT4x4BatchSentinels(t *testing.T) {
	cases := []struct {
		name string
		in   [16]int16
	}{
		{name: "zero"},
		{name: "dc_pos", in: [16]int16{5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5}},
		{name: "dc_neg", in: [16]int16{-5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5}},
		{name: "ramp", in: [16]int16{-8, -7, -6, -5, -4, -3, -2, -1, 0, 1, 2, 3, 4, 5, 6, 7}},
		{name: "alt_signs", in: [16]int16{255, -255, 255, -255, -255, 255, -255, 255, 255, -255, 255, -255, -255, 255, -255, 255}},
		{name: "max_pos", in: [16]int16{255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255}},
		{name: "max_neg", in: [16]int16{-256, -256, -256, -256, -256, -256, -256, -256, -256, -256, -256, -256, -256, -256, -256, -256}},
		{name: "single_top_left", in: [16]int16{255}},
		{name: "single_bottom_right", in: [16]int16{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 255}},
	}

	const blocks = 25
	input := make([]int16, blocks*16)
	for bi, tc := range cases {
		for i := range blocks {
			copy(input[i*16:i*16+16], tc.in[:])
		}
		outBatch := make([]int16, blocks*16)
		outPer := make([]int16, blocks*16)
		ForwardDCT4x4Batch(input, outBatch, blocks)
		for i := range blocks {
			var out [16]int16
			ForwardDCT4x4(input[i*16:i*16+16], 4, &out)
			copy(outPer[i*16:i*16+16], out[:])
		}
		for i := range outBatch {
			if outBatch[i] != outPer[i] {
				t.Fatalf("case=%d(%s) block=%d lane=%d: batch=%d per=%d", bi, tc.name, i/16, i%16, outBatch[i], outPer[i])
			}
		}
	}
}

// BenchmarkForwardDCT4x4Batch25 measures the batched cost of the 25
// blocks an MB worth of forward DCTs, which is the call pattern
// libvpx's vp8_transform_mb hides behind a single dispatch.
func BenchmarkForwardDCT4x4Batch25(b *testing.B) {
	input := make([]int16, 25*16)
	for i := range input {
		input[i] = int16((i * 7) - 200)
	}
	output := make([]int16, 25*16)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ForwardDCT4x4Batch(input, output, 25)
	}
}

// BenchmarkForwardDCT4x4PerBlock25 measures the same 25 blocks but
// dispatched one at a time, the layout of the existing govpx
// encoder paths before R9-11.
func BenchmarkForwardDCT4x4PerBlock25(b *testing.B) {
	input := make([]int16, 25*16)
	for i := range input {
		input[i] = int16((i * 7) - 200)
	}
	output := make([]int16, 25*16)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for j := range 25 {
			var out [16]int16
			ForwardDCT4x4(input[j*16:j*16+16], 4, &out)
			copy(output[j*16:j*16+16], out[:])
		}
	}
}
