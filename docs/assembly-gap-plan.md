# VP8/VP9 assembly gap plan

Status: planning, current as of 2026-06-29.

This ledger tracks the remaining SIMD/assembly work for VP8 and VP9 encoder
and decoder paths. It is intentionally scoped to kernels that matter for
WebRTC/realtime encode and decode. Each implementation should land as a small
safe point with scalar parity tests, the narrow oracle where applicable, and a
fair microbenchmark before any wider suite run.

## Benchmark rules

- Compare one kernel against the same scalar input data, stride, dimensions,
  and output buffers. Do not compare different source clips or different rate
  control settings.
- Report host arch, CPU feature path, `-benchtime`, `-count`, `B/op`, and
  `allocs/op`.
- For assembly, run host-ISA parity against the scalar implementation and then
  run the matching `-tags purego` test for fallback coverage.
- Use amd64 CI or an amd64 runner for amd64 execution. On the arm64 Mac, amd64
  work can be compile-checked but not fairly timed or parity-executed.
- Keep full-suite runs for substantial assembly landings. Routine validation
  should use the specific `go test -run` and `go test -bench` commands listed
  below.

## VP9 priorities

| Priority | Area | Arch | Local surface | Upstream source | Current state | Validation |
| --- | --- | --- | --- | --- | --- | --- |
| P0 | FP quantize | arm64 | `internal/vp9/encoder/transform_quant_dispatch_arm64.go`, new `quant_fp_arm64.s` | `vp9/encoder/arm/neon/vp9_quantize_neon.c::vp9_quantize_fp_neon` | `quantizeFPDispatch` still scalar | `go test ./internal/vp9/encoder -run 'TestVP9QuantizeFP|TestQuantizeFP' -count=1`; `go test ./internal/vp9/encoder -run '^$' -bench '^BenchmarkVP9QuantizeFP' -benchmem -benchtime=500ms -count=5` |
| P0 | FP quantize | amd64 | `internal/vp9/encoder/transform_quant_dispatch_amd64.go`, new `quant_fp_amd64.s` | `vp9/encoder/x86/vp9_quantize_sse2.c::vp9_quantize_fp_sse2` | all VP9 encoder transform/quant dispatchers scalar on amd64 | same focused tests/benchmarks on amd64 runner |
| P0 | Realtime fused block scoring | arm64/amd64 | `internal/vp9/encoder/block_yrd.go`, `transform_quant.go` | `vp9/encoder/vp9_pickmode.c::block_yrd`, `vp9/encoder/vp9_rdopt.c::vp9_block_error_fp_c`, `vp9/encoder/vp9_quantize.c::vp9_quantize_fp_c` | residual gather, transform, quantize, SATD/block-error are separate calls | `go test ./internal/vp9/encoder -run 'TestVP9BlockYrd|TestVP9BlockErrorFP|TestVP9QuantizeFP|TestForward(DCT|WHT)' -count=1`; `go test . -run 'TestVP9EncodeIntoSteadyStateAllocFreeAtBenchParity' -count=1` |
| P0 | FDCT16x16 | arm64 | `internal/vp9/encoder/transform_quant_dispatch_arm64.go`, new `fdct16x16_arm64.s` | `vpx_dsp/arm/fdct16x16_neon.c::vpx_fdct16x16_neon` | scalar fallback | `go test ./internal/vp9/encoder -run 'TestForwardDCT16x16|TestForwardHT16x16' -count=1`; add direct SIMD-vs-scalar tests before routing |
| P0 | FDCT4x4/8x8/WHT4x4 | amd64 | `internal/vp9/encoder/transform_quant_dispatch_amd64.go`, new fdct/fwht asm | `vpx_dsp/x86/fdct_sse2.c`, `vp9/encoder/x86/vp9_dct_sse2.c` | scalar fallback | `go test ./internal/vp9/encoder -run 'TestForward(DCT4x4|DCT8x8|WHT4x4)' -count=1`; direct SIMD-vs-scalar tests and benches |
| P1 | FDCT32x32 and RD | arm64/amd64 | `forwardDCT32x32Dispatch`, `forwardDCT32x32RDDispatch` | `vpx_dsp/arm/fdct32x32_neon.c`, x86 FDCT32 sources | scalar fallback; large generated-style kernels | `go test ./internal/vp9/encoder -run 'TestForwardDCT32x32|TestForwardHT' -count=1`; direct SIMD-vs-scalar tests; bench `BenchmarkForwardDCT32x32*` when added |
| P1 | Decoder inverse transform butterflies | arm64/amd64 | `internal/vp9/dsp/idct_dispatch_*.go` | `vp9/common/*/vp9_idct*`, `vp9_iht*` | row add/DC paths use asm; 1-D butterflies remain scalar | `go test ./internal/vp9/dsp -run 'TestVP9Idct|TestVP9Iht|TestVP9Iwht' -count=1`; `go test ./internal/vp9/dsp -run '^$' -bench '^BenchmarkVP9I(dct|ht|wht)' -benchmem -benchtime=500ms -count=5` |
| P1 | Compound-average convolve | arm64/amd64 | `internal/vp9/dsp/convolve_*.go` | `vpx_dsp/arm/vpx_convolve8_neon.c`, `vpx_dsp/x86/vpx_subpixel_8t_intrin_*` | plain 8-tap horiz/vert/full SIMD exists; avg path still composes via temp + scalar avg | `go test ./internal/vp9/dsp -run '^TestVP9Convolve8' -count=1`; `go test ./internal/vp9/dsp -run '^$' -bench '^BenchmarkVP9Convolve8' -benchmem -benchtime=500ms -count=5` |
| P1 | DC-only IDCT add | amd64 | `internal/vp9/dsp/idct_dispatch_amd64.go`, `idct_amd64.s` | `vpx_dsp/x86/inv_txfm_sse2.c::vpx_idct*_1_add_sse2` | arm64 has DC-add NEON; amd64 DC-only variants stay scalar | `go test ./internal/vp9/dsp -run 'TestVP9Idct.*1Add|TestVP9IdctFull' -count=1`; `go test ./internal/vp9/dsp -run '^$' -bench 'BenchmarkVP9Idct(4x4_1Add|8x8_1Add|16x16_1Add|32x32_1Add)' -benchmem -benchtime=500ms -count=5` on amd64 |
| P2 | Token-cost, trellis, coeff-rate | arm64/amd64 | `internal/vp9/encoder/coeff_cost.go`, `coef_encode.go`, `fullrd_trellis.go` | `vp9/encoder/vp9_rd.c::fill_token_costs`, `vp9/encoder/vp9_tokenize.c`, `vp9/encoder/vp9_encodemb.c::vp9_optimize_b` | pure Go; high bitstream/RD risk | profile-gated only; `go test ./internal/vp9/encoder -run 'TestCoeff|TestVP9CostTokens|Test.*Trellis|Test.*OptimizeB' -count=1`; `go test ./internal/vp9/encoder -run '^$' -bench 'BenchmarkCoeff' -benchmem -benchtime=500ms -count=5` |
| P2 | Loopfilter and intra predictors | arm64/amd64 | `internal/vp9/dsp/loopfilter.go`, `intrapred*.go` | `vp9/common/*/vp9_loopfilter*`, `vp9_reconintra*` | pure Go | add SIMD-vs-scalar tests beside `internal/vp9/dsp`; run focused loopfilter/intra tests only |

## VP8 priorities

| Priority | Area | Arch | Local surface | Upstream source | Current state | Validation |
| --- | --- | --- | --- | --- | --- | --- |
| P0 | Fused 4-reference SAD16x16 | amd64 | `internal/vp8/dsp/sad_dispatch_amd64.go`, new fused asm | `vpx_dsp/x86/sad4d_sse2.asm::vpx_sad16x16x4d_sse2`, AVX2 equivalent if worthwhile | wrapper calls four independent SADs; arm64 already has fused NEON/DOTPROD | `go test ./internal/vp8/dsp -run 'TestSAD16x16x4PtrFast|TestSADSIMDMatchesScalar' -count=1`; `go test ./internal/vp8/dsp -run '^$' -bench '^BenchmarkSAD16x16x4PtrFast$' -benchmem -benchtime=500ms -count=5` on amd64 |
| P1 | Fused subpel variance sizes below 16x16 | arm64/amd64 | `internal/vp8/dsp/variance_subpel*.go`, `subpixel*.go` | `vpx_dsp/*/subpel_variance*`, `vp8/common/*/subpixel*` | 16x16 fused and many sized paths exist; smaller fused realtime search paths still compose | `go test ./internal/vp8/dsp -run 'TestSubpelVariance.*SIMD|TestVarianceBlockSizedSIMD' -count=1`; bench `BenchmarkSubpelVariance(16x8|8x16|8x8|4x4)` |
| P1 | VP8 loopfilter grouping | amd64 | `internal/vp8/dsp/loopfilter_dispatch_amd64.go`, `loopfilter_amd64.s` | `vp8/common/x86/loopfilter_*` | SIMD exists; AVX2 and grouped edge coverage should be checked before more ports | `go test ./internal/vp8/dsp -run 'TestLoopFilter.*SIMD|TestLoopFilterYEdgeGroups|TestLoopFilterUVDispatch' -count=1`; amd64 AVX2 runner for `Test.*AVX2` |
| P1 | Encoder quant/DCT batch | arm64/amd64 | `internal/vp8/encoder/quant_batch_*.go`, `dct_batch_*.go` | `vp8/encoder/*/vp8_quantize*`, `vp8/encoder/*/dct*` | SIMD and batched entry points exist; next work is benchmarking and call-site coverage, not new kernels | `go test ./internal/vp8/encoder -run 'TestFastQuantizeBlock(Batch|SIMD)|TestForwardDCT4x4SIMD' -count=1`; bench `BenchmarkFastQuantizeBlock(Batch25|PerBlock25|SIMD|Scalar)` and `BenchmarkForwardDCT4x4(Batch25|PerBlock25|SIMD|Scalar)` |
| P2 | Decoder token/reconstruct helpers | arm64/amd64 | `internal/vp8/decoder/tokens*.go`, `reconstruct_*` | `vp8/decoder/*`, `vp8/common/*/recon*` | mostly pure Go; likely branchy, profile before asm | `go test ./internal/vp8/decoder -run 'TestDecode.*Coeff|TestTransformMacroblockTokens' -count=1`; bench `BenchmarkDecodeBlockCoeffs|BenchmarkTransformMacroblockTokens` |

## Landing order

1. VP9 FP quantize NEON on arm64, because it is in the realtime encode hot path
   and can be natively validated here.
2. VP9 realtime fused block scoring, once the component kernels have direct
   parity tests and microbenchmarks.
3. VP8 amd64 fused SAD16x16x4, but only with an amd64 runner for execution.
4. VP9 amd64 FP quantize plus 4x4/8x8/WHT dispatch, validated on amd64.
5. VP9 FDCT16x16 arm64, then 32x32/32x32RD as a generated/raw-WORD port.
6. VP9 decoder full inverse-transform butterflies and convolve-average paths.
7. VP8 smaller fused subpel variance and loopfilter grouping after fresh
   profiles show they still dominate.

## High-risk WebRTC guards

After any P0/P1 VP9 encoder SIMD landing, run the narrow production guards:

```sh
go test ./examples/webrtc-vp9 -run 'Test(PlainVP9|VP9WebRTC|WebRTC|SDP)' -count=1
GOVPX_WITH_ORACLE=1 go test -tags govpx_oracle_trace ./examples/webrtc-vp9 -run 'Test(WebRTCEndToEnd.*DecodesWithVpxdec|VP9WebRTCPacketizer.*DecodesWithVpxdec)' -count=1
go test ./cmd/govpx-bench/benchcmd -run 'TestRunVP9Benchmark|TestVP9EncodeDecodeRoundtripsSolidColor|TestLibvpxVP9FrameFlagsCLIArgsMapping' -count=1
```

VP9 SAD, variance, subpel variance, and plain horizontal/vertical/full convolve
already have arm64 and amd64 SIMD dispatch. Keep them in regression coverage
unless fresh profiles show those kernels still dominate.
