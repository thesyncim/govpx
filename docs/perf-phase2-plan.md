# Perf phase-2 plan: closing the remaining libvpx gap

Status: research complete, plan approved-pending, nothing implemented.
Evidence date: 2026-07-02, main @ 564b6f78, M-series arm64, go1.26.3.

## Where we are (clean serial, 720p realtime, byte-identical output)

| Front | ms/frame govpx vs libvpx | ratio |
|---|---|---|
| VP9 encode 1T cpu8 | 14.2 vs 5.8 | 2.44x |
| VP9 encode 4T tiles | 4.28 vs 1.72 | 2.49x |
| VP9 decode | 2.03 vs 1.52 | 1.34x (1.31x @1080p) |
| VP8 decode | 1.94 vs 1.63 | 1.19x |
| VP8 encode overload | 9.16 vs 5.42 | 1.69x |

Kernel-level parity is done: fullpel search, loopfilter, BlockYrd scoring,
variance partition, tokenize kernels all measure at/near libvpx cost (several
faster). What remains is *structural work govpx does that libvpx does not*,
plus two compiler-level levers. This plan is built on two measurement
artifacts produced this session (scratchpad `attrib.py` ledger + ceiling
report): a per-phase CPU ms/frame ledger of govpx pprof vs `sample`d vpxenc on
identical input, and an A/B-measured Go compiler/runtime ceiling.

## Ledger: the five structural deltas (VP9 encode 1T, CPU ms/frame)

| Δ ms/f | phase | root cause |
|---|---|---|
| +1.31 | mode-eval prediction | every candidate routes through the decoder-recon path (`predictVP9InterBlockOpts` → `reconstructVP9InterPredictBlock`: ref setup, CopyPlane staging, `vp9CopyPredRectToScratch/FromScratch` round-trips). libvpx `build_inter_predictors` convolves directly into the pick buffer (vp9_pickmode.c). |
| +1.12 | bitstream pack | tile walked twice (count pass 10.1 ms/f cum, replay pack 1.5) and `bitstream.(*Writer).Write` not inlinable (cost 192 vs budget 80; 0.63 ms/f flat). Token staging carries 55+ bounds-check sites (token_pack.go, coef_sb.go, writer.go). |
| +0.87 | final recon | chosen-mode re-encode goes through `quantizeVP9TxResidualWithQTrellis` + gather/stage layers where cpu8 libvpx calls plain `vp9_quantize_fp`; measured dispatch guard (`quantizeFPLibvpxNEONOK` re-validating scratch every call) costs as much as the n=16 kernel itself (7.3 vs 3.6 ns). |
| +0.79 | intra fallback | `vp9NonrdEstimateIntraFallback` computes full tx-domain residual stats + token costs; libvpx `estimate_block_intra` does prediction + model-rd variance only. Bytes match at 120f, so the heavier path reproduces the same decisions — port the cheap shape verbatim. |
| +0.68 | subpel search | `vp9InterPredictionBorderedSubpelVarianceSSE` stages through bordered copies — same scratch-round-trip family as the +1.31 row. |

Lesser rows: pick-loop self +0.58 (orchestration; batch candidate SADs via
`VpxSad4D`), write-pass misc +0.43 (content checks like sadDotWide/intPro
re-run during the write walk), tokenize/stage +0.26.

## Ceiling report: compiler/runtime levers (all interleaved A/B, byte-verified)

| Lever | measured | verdict |
|---|---|---|
| PGO with VP9 profile merged into default.pgo | **−4.3..−5.3%** (−0.7-0.8 ms/f) | SHIP FIRST. Current default.pgo is VP8-only; VP9 gets ~nothing today. |
| `-gcflags=all=-B` bounds-check ceiling | −1.6% | flag unshippable; ~half claimable via fixed-cap re-slicing in the token/write loops |
| GOGC/GODEBUG/GOMAXPROCS/GOARM64 knobs | ≈0 | nothing there — 1.58 allocs/frame already |
| **Artifact warning** | `pthread_cond_signal` 4.3% and `madvise` are Darwin SIGPROF over-attribution (GOMAXPROCS=1 → 0.0% wall change) | never target `runtime.*` rows without a wall-clock A/B first |
| Bench harness overstatement | `growslice` on measuredPackets + 2× ReadMemStats/frame (24 µs) | ~0.3-0.5% honesty fix in benchcmd |

## The plan

### P0 — quick wins + measurement hygiene (do first, ~days)
1. **PGO profile merge** (−0.7-0.8 ms/f VP9, keeps VP8's −1%): add a VP9
   realtime scenario to `make pgo-refresh`, merge profiles
   (`go tool pprof -proto` merge), refresh fingerprint. Zero risk.
2. **Bench honesty**: preallocate `measuredPackets`, keep ReadMemStats out of
   timed loops (builds on the alloc-sampling split already in benchcmd).
3. **Corrected ledger**: re-publish both ledgers with runtime rows
   wall-adjudicated; re-baseline VP8's TRUE gap (likely ~1.4-1.5x, not 1.66x,
   once artifact rows are removed) so phase-2 VP8 work is sized honestly.

### P1 — VP9 encode structural (the ledger's big five; target −3.5..−4.5 ms/f → ~1.7x)
1. **Direct-to-buffer candidate prediction** (+1.31 and +0.68 rows share this
   root): candidates convolve straight into the pick/eval buffer, killing the
   decoder-recon detour, the pred-rect scratch round-trip, and the bordered
   staging in subpel variance. Port the vp9_pickmode.c data flow. Biggest
   single item; medium parity risk — land behind byte-identity gates at 120f
   AND 480f (see parity workstream).
2. **Token/bitstream pipeline**: (a) outline `Writer.Write`'s once-per-8-bits
   carry/byte-emit path so the hot body inlines (callers keep
   lowValue/rng/count in registers, libvpx-equivalent); (b) fixed-cap
   re-slicing to clear the ~55 bounds checks in StageCoefBlock / WriteCoefSb /
   PackTokens; (c) evaluate single-walk packing: store tokens at count time
   and pack from the token list (vp9_bitstream.c shape) instead of re-walking
   modes — also deletes the +0.43 write-pass re-checks. Order (a),(b) first —
   low risk; (c) is a bigger restructure, decide after (a)+(b) re-profile.
3. **Final-recon quantize path**: route cpu8 commit through plain quantize_fp
   shape (no trellis-capable wrapper layers); hoist the dispatch guard's
   scratch validation to per-frame. Port exactly what vp9_encodemb.c does at
   this speed level.
4. **Intra fallback diet**: port `estimate_block_intra` (pred + model-rd only)
   verbatim; delete the token-cost computation after verifying (first step)
   that no decision consumes it.
5. **Pick-loop batching**: batch candidate SADs through `VpxSad4D`, flatten
   remaining per-candidate recomputation. (~40 symbols at 1-2% — accept
   diffuseness, stop when re-profile shows <0.2 ms/f in reach.)

### P2 — VP9 encode 4T serial-fraction round 2 (target 4.28 → ~3.6 ms/f)
Sequenced after P1.1 (it changes what workers share):
varPart-state sharing by design (dispatcher-clears-once + tile-column
ownership, ~15 arrays — the recipe that worked for miGrid/recon; real parity
risk, own round); enc-border recon layout (160px borders in pool buffers →
in-place `vpx_extend_frame_borders`, deletes the last LAST-mirror build);
parallel FrameCounts accumulation; lf mask-prep parallelization.

### P3 — VP8 encode (after P0.3 re-baseline; target true-gap → ~1.25-1.35x)
Attack the three real deltas: pick/scoring +0.42 (selectFastInterFrameMode
loop shape vs vp8_pick_inter_mode), final recon +0.40 (fdct batch + fused-path
completion), search +0.30, denoiser apply +0.14. Reuse the instrumented-vpxenc
counter method from round 1.

### P4 — decoders (optional; near floor)
VP9: MI pointer-grid (est 1-1.5%), inter-predict plane-fn arg flattening,
selective-walk window checks. VP8: mode-grid read fusion. Only if a sprint
has spare capacity.

### Parity workstream (parallel, independent)
1. **480-frame drift**: the 480f synthetic is NOT byte-exact (4.76 vs 4.75
   MiB) while 120f is — a new frontier. Possibly linked to the ledger oddity
   that libvpx spends 0.21 ms/f in `vp9_deblock` source denoise
   (noise-sensitivity path) with no visible govpx counterpart. Diagnose
   before P1.1 lands (it must gate on 480f byte-identity; if 480f is already
   red, pin the divergence frame first so gates stay meaningful).
2. Production-stream fuzz reds seeds 2-5 (pre-existing, task #6).

## Method discipline (unchanged from campaign, plus new rules)
- Byte-identity gates per commit; extend the standard gate from 120f to also
  cover 480f once the drift item is resolved.
- Interleaved A/B medians (≥5 pairs) for every claim; microbench + profile
  share for attribution; one clean serial run for headline numbers.
- Wall-clock adjudication REQUIRED before targeting any `runtime.*` profile
  row (SIGPROF artifact rule).
- Rejected-experiments registry (do not retry): branchless bool reads (−10%
  on Apple predictors), GOGC/GOARM64/preempt knobs (nil), eager predbuf
  copies (copy overhead ate the win), per-frame token-cost table (breaks
  zero-alloc via escape analysis — redo only as pre-allocated per-worker).
- Agent split: root-package vs internal/ file boundaries; coordinator merges
  with the full parity lane on every VP8-touching merge.

## Expected end-state
P0+P1 land: VP9 1T ≈ 9.5-10.5 ms/f (~1.7x). P2: 4T ≈ 3.6 ms/f. P3: VP8
≈ 1.3x true. Decoders already at 1.19-1.34x. Honest floor for Go vs C+NEON
on the encode paths is likely ~1.4-1.6x; anything below that requires
rethinking Go codegen per-loop (unrolled asm mega-kernels for the token
loops), which is out of scope for phase 2.
