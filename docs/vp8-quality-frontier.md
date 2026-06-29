# VP8 quality frontier

Status: current as of 2026-06-29.

This note tracks the production-quality VP8 lane separately from the assembly
gap plan. It is intentionally focused on libvpx-backed evidence and narrow
follow-up gates.

## Current read

- Encoder quality is the active risk area; decoder quality is lower risk with
  the current oracle corpus.
- The largest live quality residual is
  `TestVP8BDRate720pTwoPassVBR` in `vp8_bdrate_quality_panning_test.go`.
  It passes, but the latest scout run measured about +6.1% govpx-vs-libvpx
  BD-rate against a +7.0% ceiling.
- Production/WebRTC-shaped VP8 encode checks are healthy on the focused
  gates:
  `TestVP8OracleEncoderStreamByteParityProductionShortRuns`,
  `TestVP8OracleEncoderStreamByteParityProductionConstantQuality`, and
  `TestVP8OracleBenchWorkloadProductionGaps`.
- Decoder corpus checks are healthy on the focused external-IVF gates:
  `TestVP8OracleExternalIVFTestDataMatchesLibvpx`,
  `TestVP8OracleExternalIVFTestDataDecodeIntoMatchesLibvpx`, and
  `TestVP8OracleExternalInvalidIVFTestDataRejectedLikeLibvpx`.

## Next code lane

Start with the two-pass VBR residual rather than decoder quality. The first
safe implementation slice should add or tighten frame/rung-level diagnostics
around pass-2 rate control and RD picker decisions for the 720p panning VBR
fixture. Do not adjust BD-rate ceilings or baselines unless the trace proves the
new value is closer to pinned libvpx behavior.

## Focused gates

```sh
GOVPX_WITH_ORACLE=1 go test -tags govpx_oracle_trace . -run '^TestVP8OracleEncoderStreamByteParityProductionShortRuns$' -count=1 -timeout 240s
GOVPX_WITH_ORACLE=1 go test -tags govpx_oracle_trace . -run '^TestVP8OracleEncoderStreamByteParityProductionConstantQuality$' -count=1 -timeout 240s
GOVPX_WITH_ORACLE=1 GOVPX_TEST_DATA_PATH=internal/coracle/build/test-data/vp8 GOVPX_TEST_DATA_MIN=58 GOVPX_INVALID_TEST_DATA_MIN=2 go test -tags govpx_oracle_trace . -run '^TestVP8OracleExternal(IVFTestDataMatchesLibvpx|IVFTestDataDecodeIntoMatchesLibvpx|InvalidIVFTestDataRejectedLikeLibvpx)$' -count=1 -timeout 240s
GOVPX_BD_RATE_GATES=1 GOVPX_VPXENC_VP8_BIN=internal/coracle/build/vpxenc GOVPX_BD_RATE_LIBVPX_VP8_REQUIRED=1 go test . -run '^TestVP8BDRate720pTwoPassVBR$' -count=1 -timeout 300s -v
GOVPX_WITH_ORACLE=1 go test -tags govpx_oracle_trace . -run '^TestVP8OracleBenchWorkloadProductionGaps$' -count=1 -timeout 300s -v
```

