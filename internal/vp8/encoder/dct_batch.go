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
// through to the scalar reference.
func ForwardDCT4x4Batch(input []int16, output []int16, count int) {
	if count <= 0 {
		return
	}
	forwardDCT4x4BatchSIMD(input, output, count)
}

// forwardDCT4x4BatchScalar is the canonical scalar reference for
// batched 4x4 DCTs at block-stride 4. It exists so the SIMD ports
// can be cross-checked block-for-block.
//
//lint:ignore U1000 libvpx parity helper, retained for SIMD cross-check / non-arm64 builds
func forwardDCT4x4BatchScalar(input []int16, output []int16, count int) {
	for i := range count {
		var out [16]int16
		forwardDCT4x4Scalar(input[i*16:i*16+16], 4, &out)
		copy(output[i*16:i*16+16], out[:])
	}
}
