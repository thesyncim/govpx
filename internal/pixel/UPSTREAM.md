# Extracted SIMD kernels

This package is self-contained and imports only the Go standard library.
The VP8/VP9 packages supply codec tables, scan orders and transform glue.
No Go module dependency on goav1 or gopus is added.

Copied from goav1 commit `ff46346e73f063ff1ea3a8d7f06ee26cd9baaad4`:

| Local files | Source | Adaptation |
| --- | --- | --- |
| `goav1_sad_{arm64,amd64}.go` | `internal/av1/encoder/sad_simd_{arm64,amd64}.go` | Extracted the used pointer kernels; x4 receives independent source/reference strides. |
| `goav1_ops_{arm64,amd64}.go` | `internal/av1/encoder/gosimd_ops_{arm64,amd64}.go` | Extracted widening and exact square-accumulation helpers. |
| `goav1_loopfilter_bytes_simd.go`, `goav1_loopfilter_arm64.go`, `loopfilter_uv_arith_simd.go`, `loopfilter_nonflat_arith_simd.go`, `loopfilter_internal_arith_simd.go`, `loopfilter_internal_fused_simd.go` | `internal/av1/loopfilter/filter4_wide_arm64.go` | Copied the sixteen-byte filter dataflow; added VP8/VP9 full p3-to-q3 masks, simple and macroblock variants, exact row transposes and bounded loads/stores; shared amd64 byte-domain glue uses AVX2-safe shifts and narrowing; generated paired-chroma arithmetic reuses this network with disjoint U/V planes packed into one sixteen-byte packet; generated VP9 non-flat arithmetic rejects any active flat lane before writing; generated VP8 internal edges fix sixteen positions and four output columns with exact-width stores; ARM three-edge packets retain four neighboring rows/columns and inline the copied four-column transpose after sequential stores. |
| `goav1_projection_{arm64,amd64}.go` | `internal/av1/encoder/pframe_intpro_gosimd_arm64.go` | Extracted four-row column projection; safe slice reads, VP9 widths 16/32/64, variable normalization and remainder rows; local amd64 adaptation uses independent PSADBW sums. |
| `goav1_pixelstats_simd.go` | `internal/av1/encoder/pixelstats_gosimd.go` | Removed AV1 dispatch registration and shape wrappers; fallback bridges to govpx scalar kernels; local pairwise dot products and widening multiplies reduce intermediate widening. |
| `transform_arm64.go` | `internal/av1/transform/fhybrid_idtx_gosimd_arm64.go` | Extracted 8x8 int16 transpose for VP9's eight-line inverse DCT. |
| `transform_avx2.go` | `internal/av1/transform/fdct_gosimd_vec_amd64.go` | Extracted eight-lane AVX2 register transpose; local exact load/store helpers. |
| `goav1_convolve_simd.go` | `internal/av1/motion/compound8_gosimd.go` | Extracted horizontal/vertical tap sums; eight-byte loads preserve exact VP8/VP9 final-row bounds. |

ARM64 transform rounding also follows goav1's fixed register-shift technique
from `fdct_gosimd_vec_arm64.go`; local wrapping uses truncation instead of
AV1 saturation to preserve VP9 arithmetic. Fixed sample-width specialization
also follows `internal/av1/loopfilter/filter_lanes_gosimd.go`; the local
subpixel kernel specializes four/eight-byte loads without row-loop branches.

The adjacent checked entry points provide feature dispatch, dimensions,
codec-specific rounding and clipping, tails and scalar fallbacks. AV1's
convolution normalization is not imported: VP8/VP9 retain their own rounding
and clipping after each pass. All extracted code is tested against the
independent libvpx scalar ports and the existing codec parity tests.

License and upstream notices are retained in `third_party/goav1/`.
The available gopus SIMD kernels were also inspected (speech branch commit
`70f0a344625fbe19cc4be9674a32e959321ac95b`); its audio-specific floating-point,
FIR and recurrent kernels are not required by these byte-pixel operations.
