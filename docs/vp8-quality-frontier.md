# VP8 quality frontier

Status: current as of 2026-06-29.

This note tracks the production-quality VP8 lane separately from the assembly
gap plan. It is intentionally focused on libvpx-backed evidence and narrow
follow-up gates.

## Current read

- Decoder quality is lower risk with the current oracle corpus, and the
  focused encoder quality gate is currently healthy.
- The previous largest quality residual was
  `TestVP8BDRate720pTwoPassVBR` in `vp8_bdrate_quality_panning_test.go`.
  It now measures near libvpx on the focused gate: the 2026-06-29 run measured
  govpx-vs-libvpx BD-rate at -0.189% against the +7.0% ceiling.
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

Keep the two-pass VBR fixture as a drift guard rather than the next default
implementation target. If it regresses again, add or tighten frame/rung-level
diagnostics around pass-2 rate control and RD picker decisions for the 720p
panning VBR fixture. Until then, prioritize the measured VP8 realtime
performance gaps and keep decoder work tied to oracle-corpus failures.

## Focused gates

```sh
GOVPX_WITH_ORACLE=1 go test -tags govpx_oracle_trace . -run '^TestVP8OracleEncoderStreamByteParityProductionShortRuns$' -count=1 -timeout 240s
GOVPX_WITH_ORACLE=1 go test -tags govpx_oracle_trace . -run '^TestVP8OracleEncoderStreamByteParityProductionConstantQuality$' -count=1 -timeout 240s
GOVPX_WITH_ORACLE=1 GOVPX_TEST_DATA_PATH=internal/coracle/build/test-data/vp8 GOVPX_TEST_DATA_MIN=58 GOVPX_INVALID_TEST_DATA_MIN=2 go test -tags govpx_oracle_trace . -run '^TestVP8OracleExternal(IVFTestDataMatchesLibvpx|IVFTestDataDecodeIntoMatchesLibvpx|InvalidIVFTestDataRejectedLikeLibvpx)$' -count=1 -timeout 240s
GOVPX_BD_RATE_GATES=1 GOVPX_VPXENC_VP8_BIN=internal/coracle/build/vpxenc GOVPX_BD_RATE_LIBVPX_VP8_REQUIRED=1 go test . -run '^TestVP8BDRate720pTwoPassVBR$' -count=1 -timeout 300s -v
GOVPX_WITH_ORACLE=1 go test -tags govpx_oracle_trace . -run '^TestVP8OracleBenchWorkloadProductionGaps$' -count=1 -timeout 300s -v
```
