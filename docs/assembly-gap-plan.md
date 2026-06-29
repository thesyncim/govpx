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
| P0 | FP quantize | arm64 | `internal/vp9/encoder/transform_quant_dispatch_arm64.go`, `internal/vp9/encoder/quant_fp_arm64.s` | `vp9/encoder/arm/neon/vp9_quantize_neon.c::vp9_quantize_fp_neon` | `QuantizeFPLibvpx`, `QuantizeFP`, and `QuantizeFPWithQ` use the NEON AC kernel for supported libvpx FP tables; purego and unusual tables fall back scalar | `go test ./internal/vp9/encoder -run 'TestVP9QuantizeFP|TestQuantizeFP' -count=1`; `go test -tags purego ./internal/vp9/encoder -run 'TestVP9QuantizeFP|TestQuantizeFP' -count=1`; `go test ./internal/vp9/encoder -run '^$' -bench '^BenchmarkVP9QuantizeFP' -benchmem -benchtime=500ms -count=5` |
| P0 | FP quantize | amd64 | `internal/vp9/encoder/transform_quant_dispatch_amd64.go`, new `quant_fp_amd64.s` | `vp9/encoder/x86/vp9_quantize_sse2.c::vp9_quantize_fp_sse2` | all VP9 encoder transform/quant dispatchers scalar on amd64 | same focused tests/benchmarks on amd64 runner |
| P0 | Realtime fused block scoring | arm64/amd64 | `internal/vp9/encoder/block_yrd.go`, `transform_quant.go` | `vp9/encoder/vp9_pickmode.c::block_yrd`, `vp9/encoder/vp9_rdopt.c::vp9_block_error_fp_c`, `vp9/encoder/vp9_quantize.c::vp9_quantize_fp_c` | residual gather, transform, quantize, SATD/block-error are separate calls | `go test ./internal/vp9/encoder -run 'TestVP9BlockYrd|TestVP9BlockErrorFP|TestVP9QuantizeFP|TestForward(DCT|WHT)' -count=1`; `go test . -run 'TestVP9EncodeIntoSteadyStateAllocFreeAtBenchParity' -count=1` |
| P0 | FDCT16x16 | arm64 | `internal/vp9/encoder/transform_quant_dispatch_arm64.go`, `internal/vp9/encoder/fdct16x16_arm64.s` | `vpx_dsp/arm/fdct16x16_neon.c::vpx_fdct16x16_neon` | NEON kernel routed on arm64 with scalar fallback for invalid shapes | `go test ./internal/vp9/encoder -run 'TestForwardDCT16x16|TestForwardHT16x16|TestForwardDCT16x16NEON' -count=1`; `go test -tags purego ./internal/vp9/encoder -run 'TestForwardDCT16x16|TestForwardHT16x16' -count=1`; `go test ./internal/vp9/encoder -run '^$' -bench '^BenchmarkForwardDCT16x16' -benchmem -benchtime=500ms -count=5` |
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
| P0 | Six-tap split predictors | amd64 | `internal/vp8/dsp/subpixel_amd64.go`, `subpixel_amd64.s` | `vp8/common/x86/subpixel_sse2.asm`, `vp8/common/x86/vp8_asm_stubs.c` | 16x16/8x8/8x4/4x4 SSE2 exist; 16x8 and 8x16 split-block predictors fall back on amd64 | `go test ./internal/vp8/dsp -run 'TestSixTapPredict.*|FuzzVP8DSPSubpixel' -count=1`; `go test ./internal/vp8/dsp -run '^$' -bench 'BenchmarkSixTapPredict(16x8|8x16)$' -benchmem -benchtime=500ms -count=5` on amd64 |
| P0 | Fused subpel variance16x16 | amd64 | `internal/vp8/dsp/variance_subpel16_fused_other.go`, `variance_subpel_amd64.go` | `vpx_dsp/x86/subpel_variance_sse2.asm`, `vpx_dsp/x86/variance_sse2.c` | arm64 has fused horizontal/vertical/bilinear 16x16; amd64 stages predict then variance | `go test ./internal/vp8/dsp -run 'TestSubpelVariance.*SIMD|TestVarianceBlocks' -count=1`; `go test ./internal/vp8/dsp -run '^$' -bench 'BenchmarkSubpelVariance16x16(Dispatch|PtrFast|HorizontalOnly|VerticalOnly)' -benchmem -benchtime=500ms -count=5` on amd64 |
| P1 | Fused split-block subpel variance | arm64/amd64 | `internal/vp8/dsp/variance.go`, `variance_subpel_*.go` | `vpx_dsp/*/subpel_variance*`, `vpx_dsp/*/variance*` | useful for SPLITMV/subpel RD; many sizes and rounding cases | `go test ./internal/vp8/dsp -run 'TestSubpelVariance.*SIMD|TestVarianceBlocks' -count=1`; `go test ./internal/vp8/dsp -run '^$' -bench 'BenchmarkSubpelVariance(16x8|8x16|8x8|4x4)' -benchmem -benchtime=500ms -count=5` |
| P1 | Direct vertical loopfilter | amd64 | `internal/vp8/dsp/loopfilter_dispatch_amd64.go`, `loopfilter_amd64.s`, `loopfilter_transpose_amd64.s` | `vp8/common/x86/loopfilter_sse2.asm::vp8_loop_filter_vertical_edge_sse2`, MB/UV/simple variants | vertical path gathers/transposes into horizontal kernels | `go test ./internal/vp8/dsp -run 'TestLoopFilter' -count=1`; `go test ./internal/vp8/dsp -run '^$' -bench 'Benchmark(LoopFilter|MBLoopFilter).*(Vertical|VerticalEdgesY|VerticalEdgeUV)' -benchmem -benchtime=500ms -count=5` on amd64 |
| P1 | Fused decoder dequant + IDCT + add | arm64/amd64 | `internal/vp8/decoder/reconstruct.go`, `internal/vp8/dsp/dequant.go`, `idct_simd_*.go` | `vp8/common/x86/idct_blk_sse2.c`, `vp8/common/arm/neon/idct_blk_neon.c` | per-block SIMD exists; block-pair dequant+IDCT+add fusion is missing | `go test ./internal/vp8/decoder -run 'TestTransformMacroblockTokens|TestReconstruct|TestChroma' -count=1`; `go test ./internal/vp8/dsp -run 'TestIDCT4x4AddSIMDMatchesScalar|TestDequantIDCT4x4AddMatchesManualAndZerosInput' -count=1` |
| P2 | Denoiser UV sum/copy fusion | arm64/amd64 | `internal/vp8/encoder/denoiser_simd_amd64.go`, `denoiser_simd_arm64.go` | `vp8/encoder/x86/denoising_sse2.c`, `vp8/encoder/arm/neon/denoising_neon.c` | lower priority unless realtime denoising is enabled; UV scalar precheck/copy remains | `go test ./internal/vp8/encoder -run 'TestDenoiser' -count=1`; `go test ./internal/vp8/encoder -run '^$' -bench 'BenchmarkDenoiserFilter(Y|UV)(Dispatch|Scalar)' -benchmem -benchtime=500ms -count=5` |
| P3 | Existing encoder DCT/quant batch, residual gather, Walsh, intra predictors, basic SAD/variance | arm64/amd64 | `internal/vp8/encoder`, `internal/vp8/dsp` | libvpx matching kernel families | meaningful SIMD already exists or the path is less tied to WebRTC realtime | keep existing focused tests/benchmarks as regression coverage, not first-wave implementation work |

## Landing order

1. VP9 realtime fused block scoring, once the component kernels have direct
   parity tests and microbenchmarks.
2. VP8 amd64 fused SAD16x16x4, six-tap split predictors, and fused 16x16
   subpel variance, but only with an amd64 runner for execution.
3. VP9 amd64 FP quantize plus 4x4/8x8/WHT dispatch, validated on amd64.
4. VP9 FDCT16x16 arm64, then 32x32/32x32RD as a generated/raw-WORD port.
5. VP9 decoder full inverse-transform butterflies and convolve-average paths.
6. VP8 smaller fused subpel variance, direct vertical loopfilter, and fused
   decoder dequant+IDCT+add after fresh profiles show they still dominate.

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

After high-risk VP8 DSP or decoder assembly, keep validation focused on the
affected path:

```sh
go test ./internal/vp8/dsp -run 'TestSAD|TestSixTap|TestSubpelVariance|TestLoopFilter|TestIDCT' -count=1
go test ./internal/vp8/decoder -run 'TestTransformMacroblockTokens|TestReconstruct|TestChroma' -count=1
go test . -run '^TestVP8WebRTCRTPLongTemporalNoLossStreamDecodes$' -count=1
```
