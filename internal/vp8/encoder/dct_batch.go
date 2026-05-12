package encoder

// Whole-MB batched forward 4x4 DCT entry point. Mirrors libvpx v1.16.0
// vp8/encoder/encodemb.c vp8_transform_mb / vp8_transform_mbuv:
// the 16 Y blocks (and the 8 UV blocks) are transformed in a single
// dispatched call; per-arch SIMD ports loop the per-block kernel
// without re-entering the Go-asm boundary for each block. Output is
// byte-identical to ForwardDCT4x4 invoked per block at stride 4.
//
// The input layout is N consecutive 4x4 blocks, each stored as 16
// contiguous int16 values (block stride 4). The output layout is the
// same.
//
// ForwardDCT4x4Batch dispatches to the SIMD or scalar kernel; the
// SIMD entry point is plugged in by per-arch dispatch files
// (dct_batch_arm64.go, dct_batch_amd64.go), and other platforms fall
// through to the scalar reference in dct_batch_other.go.
func ForwardDCT4x4Batch(input []int16, output []int16, count int) {
	forwardDCT4x4BatchSIMD(input, output, count)
}
