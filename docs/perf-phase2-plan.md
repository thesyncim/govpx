# Perf phase-2 plan: closing the remaining libvpx gap

Status: P0.1/P0.2, P0.3a/P0.3b, P1.1a/P1.1b/P1.1c, P1.2a/P1.2b/P1.2c/P1.2d/P1.2e/P1.2f/P1.2g/P1.2h, P1.3a/P1.3b, and P1.5a implemented on
2026-07-02; the phase-3 oracle-denoise prerequisite is also resolved. The full
per-phase CPU P0.3 ledger refresh and larger P1 structural items remain
pending.
Evidence date: 2026-07-02, main @ 564b6f78, M-series arm64, go1.26.3.

Structural sequencing note: `docs/perf-phase3-structural-plan.md` now
supersedes the structural sections below. Keep this document for phase-2
measurements and landed P0/P1 call-shape history, but use phase 3 for
implementation order, denoise-oracle prerequisites, and VP9 single-walk /
pick-buffer / row-mt sequencing.

## Where we are (120-frame 720p realtime benchmark)

| Front | ms/frame govpx vs libvpx | ratio |
|---|---|---|
| VP9 encode 4T cpu8, denoise | 4.24 vs 2.10 | 2.02x |
| VP9 encode 1T cpu8, denoise | 11.91 vs 5.57 | 2.14x |
| VP9 encode 4T tiles, no-denoise | 4.65 vs 2.18 | 2.14x |
| VP9 decode | 1.99 vs 1.51 | 1.32x |
| VP8 decode | 1.95 vs 1.65 | 1.18x |
| VP8 encode overload | 8.01 vs 5.25 | 1.53x |

Commands for the refreshed single-run spot checks used 1280x720, 120 input
frames, realtime cpu8, 2500 kbps, `-encode-only` for encode rows, and
`-decode` for decode rows. The VP9 no-denoise 4T row is explicitly
`-threads=4 -noise-sensitivity=0`; as of the 2026-07-03 phase-3 C2 safe point,
the default denoise path also uses the multi-column tile workers, and the
benchmark mirrors that with libvpx `--threads=4 --tile-columns=2
--noise-sensitivity=4`. As of 2026-07-02 the VP9 libvpx oracle builds also
have `CONFIG_VP9_TEMPORAL_DENOISING 1`; the older denoise + explicit-threads
benchmark compared serial govpx against tiled/no-denoise libvpx and overstated
the 4T gap. A later phase-3 call-shape cleanup bypasses the inactive
`vp9WithSBSearchEntropy` wrapper on the normal realtime path; the latest
byte/topology-identical 4T spots remain in the same band (about
4.64-4.68 ms/frame no-denoise and 4.86-4.98 ms/frame with default denoise).

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
| +0.79 | intra fallback | Older scratchpad attribution. Phase 3 source audit corrects this: libvpx v1.16.0 `estimate_block_intra` still calls `block_yrd`, so do not target this as a cheap-delete row without a newer source citation and byte-identity proof. |
| +0.68 | subpel search | `vp9InterPredictionBorderedSubpelVarianceSSE` stages through bordered copies — same scratch-round-trip family as the +1.31 row. |

Lesser rows: pick-loop self +0.58 (orchestration; batch candidate SADs via
`VpxSad4D`), write-pass misc +0.43 (content checks like sadDotWide/intPro
re-run during the write walk), tokenize/stage +0.26.

## Ceiling report: compiler/runtime levers (all interleaved A/B, byte-verified)

| Lever | measured | verdict |
|---|---|---|
| PGO with VP9 profile merged into default.pgo | **−4.3..−5.3%** (−0.7-0.8 ms/f) | DONE 2026-07-02. Previous default.pgo was VP8-only; the refreshed profile now includes VP9 realtime. |
| `-gcflags=all=-B` bounds-check ceiling | −1.6% | flag unshippable; ~half claimable via fixed-cap re-slicing in the token/write loops |
| GOGC/GODEBUG/GOMAXPROCS/GOARM64 knobs | ≈0 | nothing there — 1.58 allocs/frame already |
| **Artifact warning** | `pthread_cond_signal` 4.3% and `madvise` are Darwin SIGPROF over-attribution (GOMAXPROCS=1 → 0.0% wall change) | never target `runtime.*` rows without a wall-clock A/B first |
| Bench harness overstatement | `growslice` on measuredPackets + 2× ReadMemStats/frame (24 µs), plus VP9 denoise/threads libvpx layout mismatch and a `govpx_phase_stats`-only `ChoosePartitioningArgs` escape | DONE for packet retention / alloc sampling / VP9 layout; phase-stats VP9 4T no-denoise allocation reports now sit around 2.2 allocs/frame instead of ~220; full per-phase ledger still needs a fresh profile pair |

## The plan

### P0 — quick wins + measurement hygiene (do first, ~days)
1. **PGO profile merge** — DONE 2026-07-02 (−0.7-0.8 ms/f VP9,
   keeps VP8's −1%): add a VP9
   realtime scenario to `make pgo-refresh`, merge profiles
   (`go tool pprof -proto` merge), refresh fingerprint. Zero risk.
2. **Bench honesty** — DONE 2026-07-02: preallocate `measuredPackets`,
   keep ReadMemStats out of timed loops (builds on the alloc-sampling split
   already in benchcmd), and skip packet-retention copies for encode-only
   profile runs.
3. **Corrected ledger**: PARTIAL 2026-07-02 — the headline rows above were
   re-baselined after fixing VP9 denoise/thread parity in the libvpx side of
   `govpx-bench`. The VP8 encode overload gap is now 1.53x on this 120f run.
   Fresh `govpx_phase_stats` runs now pin both VP9 topology and coarse VP9
   durations. After the phase-3 oracle rebuild and A1 count-pass safe point,
   the refreshed VP9 1T run was 12.97 ms/frame govpx vs 5.80 ms/frame libvpx
   (2.24x), 108 encoded / 12 dropped. After later phase-3 safe points through
   the ARM64 wide-SAD dispatch, intra-H/TM predictor safe points, and the
   exact-window entropy-context helper safe point, and a follow-up PGO refresh
   after the rejected visible-reference subpel probe, the current post-PGO spot
   is 11.81 ms/frame govpx vs 5.58 ms/frame libvpx
   (2.12x), still 108 encoded / 12 dropped; per input frame, the VP9 duration
   row is count prepass 9.96 ms, tile write/replay 1.22 ms, loop-filter apply
   0.31 ms, header write 0.02 ms, and
   loop-filter pick 0 ms because the default realtime FROM_Q path skips the
   full-image picker. The
   same run's topology counters report 279770 mode blocks in both count/write
   walks, 265370 inter picks all replay-hit in the write pass, 2155213
   predictor blocks, 1921077 predictor planes, 1021710 prediction-variance
   calls, 211599 fullpel searches, 758967 SAD calls, and 1383063 SAD
   candidates. VP8's same refreshed phase run is duration-backed: about
   7.96 ms/frame govpx vs 5.24 ms/frame libvpx (1.52x), with the measured
   govpx input-frame ledger dominated by inter reconstruction (4.10 ms),
   loop-filter pick (2.39 ms), packet write (0.77 ms), loop-filter apply
   (0.27 ms), and key reconstruction (0.14 ms). Remaining: re-publish the
   full per-phase CPU ledgers with runtime rows wall-adjudicated so phase-2 VP8
   and VP9 work is sized from fresh profiles, not the older scratchpad.

### P1 — VP9 encode structural (the ledger's big five; target −3.5..−4.5 ms/f → ~1.7x)
1. **Direct-to-buffer candidate prediction**: PARTIAL 2026-07-02 — the
   non-tree subpel SAD walker no longer does an initial unused SSE/recon build,
   and its SAD predictor can be built into encoder scratch with byte parity
   against the decoder-recon path for copy, subpel, border, and filter cases.
   The model-RD luma variance scorer now uses that same compact scratch
   predictor instead of writing every candidate into the live recon plane, and
   the nonrd picker treats the compact predictor as the current candidate for
   `ModelRdForSbYLarge`, BlockYrd, filter-sweep winner retention, and best-pred
   capture. The pred-filter `search_filter_ref` sweep now uses two compact
   luma buffers (`blockScratch` plus `nonrdFilterPredScratch`) and swaps eval /
   best ownership when a new best filter wins, so the winning predictor is
   preserved without a post-pick copy or rebuild. `ModelRdForSbYLarge` consumes
   the active eval buffer during that sweep.
   `TestVP9InterPredictionSADScratchMatchesReconPredictor` pins scratch bytes
   plus SAD and variance/SSE against the old recon-derived rectangle across
   four block shapes x four interp filters, including padded custom-scratch
   strides whose row padding must stay untouched. Focused arm64 samples stayed
   0 allocs and measured the scratch variance scorer at
   ~287.6-289.7 ns/op versus ~293.3-294.2 ns/op for the recon-reference shape.
   A local 120-frame cpu8 encode-only spot check stayed topology-equivalent at
   108 encoded / 12 dropped and unchanged govpx bytes (1,235,578), measuring
   13.24 ms/frame govpx vs 5.82 ms/frame libvpx, with `vp9_count_ns`
   1,356,916,537 and tile write 153,458,752.
   The subpel tree-search variance path now caches the per-block/ref source and
   bordered-reference geometry once per refinement instead of rebuilding those
   invariants for every nearby MV. Helper-vs-cached-scorer parity is pinned for
   fullpel, subpel, and visible-edge blocks; focused arm64 samples stayed
   0 allocs and measured the old helper at ~197.8-199.4 ns/op versus
   ~173.6-176.0 ns/op for the reused scorer. A local phase-timed 120-frame cpu8
   spot check stayed topology-equivalent at 108 encoded / 12 dropped and
   unchanged govpx bytes (1,235,578), measuring 13.45 ms/frame govpx vs
   6.16 ms/frame libvpx on the final noisy single run. A follow-up bounds
   cache now stores the padded-reference row/min/max limits in the scorer
   instead of rebuilding them for each nearby MV. Focused scorer samples stayed
   0 allocs at ~173.9-175.3 ns/op, and two 120-frame cpu8 spots stayed
   topology-equivalent at 108 encoded / 12 dropped with 1,234,808 govpx bytes
   versus 1,235,204 libvpx bytes, measuring 13.04 and 13.17 ms/frame. After
   PGO refresh/check, the same spot measured 13.27 ms/frame with `vp9_count_ns`
   1,353,870,914 and tile write 156,081,292. The scratch predictor now takes a
   narrow zero-MV luma copy path before falling back to the decoder predictor,
   preserving compact-scratch SAD/variance cache locality while skipping the
   full decoder setup for source-shaped copy predictors. Focused scratch-vs-recon
   parity stayed green; 120-frame cpu8 phase spots stayed topology-equivalent
   and byte-equivalent at 108 encoded / 12 dropped with 1,234,808 govpx bytes,
   measuring 13.15 ms/frame profiled and 13.03 ms/frame unprofiled. After PGO
   refresh/check, the repeat spot measured 13.17 ms/frame with `vp9_count_ns`
   1,341,502,253 and tile write 152,387,671. The compact predictor
   scratch<->recon copy helpers now walk source/destination offsets instead of
   recomputing row slices; focused scratch parity stayed green, and clean
   pre-load 120-frame cpu8 spots stayed byte/topology-equivalent at 13.01 and
   12.88 ms/frame with `vp9_count_ns` 1,324,989,751 / 1,312,619,880. PGO
   refresh/check stayed green.
   The phase-3 A5 continuation now ports libvpx's `tmp[0..3]` ownership for
   normal non-ML `pred_pixel_ready` picks: three compact buffers plus the live
   destination, ownership transfer on new-best candidates and filter trials,
   and a single final or pre-intra copy only when required. The 480-frame 4T
   no-denoise frontier stayed exact at 4,981,549 bytes and 468/12 topology;
   two order-reversed whole-process pairs used about 1.3-1.8% fewer cycles on
   the heavily loaded host, and picker-attributed `runtime.memmove` disappeared
   from the follow-up profile. The ML partition lane now uses the same pool,
   with SB-local `pickPred` as `tmp[3]`, and captures a destination-owned inter
   winner once before intra search. Three order-reversed 320x180, 2000-frame
   pairs kept exact 4,160,881-byte output and 1997/3 topology; the two stable
   pairs improved about 2.3-2.5%, while picker cumulative CPU fell from 1.39 s
   to 1.32 s and sampled `runtime.memmove` from 100 ms to 20 ms. Intra-winner
   predictor carry remains open. The phase-3 A6 continuation now reads
   edge-crossing candidate tap windows directly from the persistent padded
   reference instead of rebuilding
   temporary border staging; stable 480-frame 4T pairs improved about 0.3-0.5%
   with exact bytes/topology, and `vp9ExtendInterPredictSource` disappeared
   from the follow-up profile. Reference-generation stamps keep those padded
   reads valid across same-buffer replacement, retries, ROI/active-map passes,
   and worker aliases.
   Remaining work: the broader +1.31/+0.68 rows are not closed until candidate
   prediction storage is source-shaped end-to-end and the remaining residual
   gather/stage layers are removed. Port the vp9_pickmode.c data flow. Biggest
   single item; medium parity risk — land behind the existing byte-identity
   gates and the pinned 480f benchmark frontier below. Promote the 480f
   frontier to full byte-identity only after that drift is closed.
2. **Token/bitstream pipeline**: PARTIAL 2026-07-02 — phase-3 A1 now skips
   partition bit emission, keyframe/fallback fixed-probability mode fragments,
   and the inter-leaf `WriteInterBlock` call in the count pass after all
   explicit syntax histograms are updated. The rebuilt
   denoise-capable 120-frame VP9 spot check stayed topology-equivalent at
   108 encoded / 12 dropped and unchanged govpx bytes (1,235,578), measuring
   12.97 ms/frame govpx vs 5.80 ms/frame libvpx; `vp9_count_ns` was
   1,325,286,292 and tile write was 152,942,964. The broader coefficient
   walkers now use the already-validated qcoeff/dq magnitude helpers directly,
   letting those helpers inline in `StageCoefBlock` and `WriteCoefBlock`; `StageCoefBlock`
   also has a full-buffer fast path that keeps tiny-buffer checks on a
   fallback, and the stage/write walkers now use the active transform index
   directly for band lookup instead of a second induction variable. A follow-up
   asserts the staged-token tail invariants (`ONE..CAT6`, non-zero pivot prob)
   before table lookup, clearing the pack-side token/pareto bounds checks
   without changing valid bitstreams. The surrounding coefficient-SB walker now
   uses pointer-form scan dispatch (`GetScanPtr`) so intra-Y tx blocks avoid
   copying `ScanOrder` slice headers. Replayed leaf tokens now pack and stamp
   above/left coefficient contexts from the same EOSB-terminated TOKENEXTRA
   stream, avoiding the separate qcoeff/eob context replay walk; the focused
   leaf replay microbench moved from ~322-328 ns to ~317-325 ns, still
   0 allocs. A full qcoeff-window staging path now skips the dqcoeff fallback
   branches in `StageCoefBlock`; the stage+pack benchmark stays at 0 allocs
   and improves from ~2.63 µs / ~13.1 µs to ~2.38-2.43 µs / mostly
   ~12.31-12.43 µs for sparse/dense tx16. The combined pack+context replay
   path also has a fixed-window TOKENEXTRA fast path and remains in the
   ~319-324 ns range. The above/left coefficient-context stampers now share a
   fixed-width 1/2/4/8-byte helper, replacing the per-byte dynamic loop checks
   left in the WriteCoefSb and token-replay walkers. The qcoeff staging path
   now reuses the qcoeff token table for token, packed extra/sign, and energy
   class metadata; focused samples stayed 0 allocs and measured ~2.40-2.46 µs
   sparse, ~12.04-12.49 µs dense, and ~321 ns combined pack+commit. Token
   replay now writes the fixed VP9 coefficient-token tail directly from the
   staged token class instead of replaying the generic `CoefConTree` walker;
   direct-writer parity stayed green, focused samples stayed 0 allocs and
   measured ~2.39-2.41 µs sparse, ~12.07-12.16 µs dense, ~319-320 ns combined
   pack+commit, and local cpu8 samples stayed 0 allocs in the ~9.92-10.09
   ms/op band with one 10.25 ms/op outlier. The fixed token-tail fragment is
   now batched through a tiny boolean-writer primitive (`WritePacked`) that
   keeps arithmetic state local for 2-4 known bits while preserving the normal
   writer; byte-for-byte packed-vs-sequential writer tests and direct-writer
   token parity stayed green. Focused samples stayed 0 allocs and measured
   ~2.33-2.34 µs sparse, ~11.05-11.22 µs dense, and ~310-312 ns combined
   pack+commit. A local cpu8 sample stayed 0 allocs and measured mostly
   ~9.70-9.83 ms/op with one 10.06 ms/op outlier. Category payload bits now
   share the same packed-writer primitive: CAT2-CAT6 extra-bit chunks are
   batched through `WritePacked` while a stream-level test proves the batched
   writer matches the old sequential `VP9ExtraBits` loop over all CAT1-CAT5
   payloads and all 8-bit-profile CAT6 payloads. Focused samples stayed
   0 allocs and measured sparse staging mostly ~2.29-2.35 µs (one 2.40 µs
   outlier), dense staging mostly ~11.04-11.27 µs, separate pack+commit
   ~306-315 ns, and combined pack+commit ~290-295 ns. The direct coefficient
   writer now uses that same fixed token-tail path instead of the generic
   `CoefConTree` replay, with an equivalence test covering byte output and
   branch-count slots against the old tree walk. The focused tail benchmark
   moved from ~133.5-136.5 ns for generic tree+counts to ~130.8-131.9 ns for
   packed tail+counts; staged-token samples stayed 0 allocs and held around
   ~2.29-2.31 µs sparse, ~10.75-10.81 µs dense, and ~292-294 ns combined
   pack+commit. The token branch-count tail now shares the same fixed VP9
   token-tree cases, and non-counting callers return after token validation
   instead of walking `CoefConTree`. `TestWritePackedCoefTokenTailMatchesTree`
   still proves branch-slot equivalence against the old tree walk; focused
   samples stayed 0 allocs and measured ~2.25-2.29 µs sparse, ~10.25-10.36 µs
   dense, ~293-297 ns combined pack+commit, and mostly ~130-131 ns for the
   packed tail+counts microbench (one 137.9 ns outlier). The combined
   pack+context replay now stays limited to confirmed count-pass leaf replays;
   SVC encoders (`svc.UseSvc`) use the conservative non-replay token path
   because spatial-layer count/write leaf visitation can differ, as pinned by
   `TestVP9SpatialSVCEncodeResultPacketizeRTP`. A 120-frame cpu8 encode-only
   spot check stayed topology-equivalent at 108 encoded / 12 dropped,
   measuring 13.74 ms/frame govpx vs 6.13 ms/frame libvpx on this noisy single
   run. Staged replay now batches the current non-zero decision plus the
   coefficient token body through the boolean writer's packed-fragment path
   (widened to 5-8 bit fragments for common low-token shapes) without changing
   direct writer semantics. Focused samples stayed 0 allocs and measured
   ~2.17-2.31 µs sparse, ~8.83-8.94 µs dense, ~295-297 ns separate
   pack+commit, and ~278-286 ns combined pack+commit. Local cpu8 spot checks
   kept 108 encoded / 12 dropped and unchanged govpx bytes (1,235,578), with
   the final noisy single run at 13.36 ms/frame govpx vs 5.95 ms/frame libvpx.
   Keyframe/intra coefficient replay now uses the same combined
   pack+context path (`packVP9ReplayCoefTokenLeafWithContexts`) as confirmed
   count-pass leaf replays, committing above/left contexts from the staged
   TOKENEXTRA stream instead of a separate qcoeff/eob replay. Focused token
   replay and keyframe/SVC tests stayed green; the 120-frame phase-timed spot
   remained topology-equivalent at 108 encoded / 12 dropped and unchanged
   govpx bytes (1,235,578), measuring 13.34 ms/frame govpx vs 5.92 ms/frame
   libvpx with `vp9_count_ns` 1,363,197,920 and tile write 154,458,203.
   The same combined pack+context path is now used for every inter-source
   token replay, including forced-intra/non-replayed leaves, so the root encoder
   no longer packs staged tokens and then reopens qcoeff/eob buffers solely to
   stamp coefficient contexts. Focused pack+context samples stayed 0 allocs and
   measured ~279-281 ns combined pack+commit; the 120-frame phase-timed spot
   stayed topology-equivalent at 108 encoded / 12 dropped and unchanged govpx
   bytes (1,235,578), measuring 13.24 ms/frame govpx vs 5.85 ms/frame libvpx
   with `vp9_count_ns` 1,356,209,668 and tile write 152,271,875.
   Threaded count-token workers now use tile-local token-list arenas:
   `TokenFrameBuffer.EnsureForTile` preserves the global
   `(tileRow,tileCol,sbRow)` lookup API while allocating/clearing only the
   worker's tile-column list slots instead of a full MAX_TILE_ROWS x
   MAX_TILE_COLS grid per worker. `TestTokenFrameBufferTileLocalListIndexAndSlices`
   pins the local-index mapping, and a 30-frame 1280x720 cpu8
   no-denoise/threads=4 encode-only spot exercised the path with
   `vp9_tile_worker_count_job_runs` 68 and `vp9_tile_worker_encode_job_runs`
   72, keeping 18 encoded / 12 dropped and near-identical bytes versus libvpx
   (380,701 vs 380,681).
   Coefficient branch-count recording now uses constant-index EOB/ZERO/PIVOT
   helpers in the staged/direct writers, and the direct token-body helper takes
   a fixed `[UnconstrainedNodes]uint8` probability row instead of slicing
   `ctxTree[2]` on the hot path. The BCE check no longer reports the prior
   token-body row accesses or the constant EOB/ZERO branch-count call sites;
   the remaining `coef_encode.go` checks are in the generic tree walker.
   Focused samples stayed 0 allocs and measured ~2.19-2.34 µs sparse,
   ~8.84-9.03 µs dense, and ~279.7-283.3 ns combined pack+commit.
   `WriteCoefSb` now hands the staged coefficient walker a transform-sized
   TOKENEXTRA window on the normal arena-backed path, while retaining the old
   checked fallback for intentionally tiny buffers. The focused staging samples
   stayed 0 allocs and measured ~2.14-2.16 µs sparse, ~8.91-9.03 µs dense,
   and ~280.8-284.4 ns combined pack+commit; the BCE check still reports the
   raster-derived coefficient/token-cache checks, so the remaining bounds-check
   work is in deeper per-transform scan/qcoeff specialization rather than
   another SB-level capacity wrapper. The combined pack+context replay now
   consumes each full TOKENEXTRA block through a local window instead of
   absolute cursor indexing; the full-window replay checks at the former
   `token_pack.go:486/502` call sites dropped out of the BCE report, while
   focused samples stayed 0 allocs at ~279.9-282.2 ns combined pack+commit.
   The staged full-window walkers and direct `WriteCoefBlock` now share a
   masked token-cache helper for coefficient-context reads and writes after
   preflighting the full neighbor table. Focused direct/staged parity tests
   stayed green; the BCE report no longer shows the former direct
   token-cache-store / `GetCoefContext` call sites, leaving the expected
   scan/qcoeff/probability/branch-stat checks. Focused samples stayed 0 allocs:
   the new direct writer benchmark measured ~1.78-1.80 µs sparse and
   ~9.14-9.19 µs dense, staged+pack measured ~2.10-2.13 µs sparse and
   ~8.88-9.18 µs dense, and combined pack+commit measured ~278.6-281.4 ns.
   The qcoeff full-window path now also trusts the already-preflighted scan and
   qcoeff windows for EOB discovery and per-token qcoeff loads, and the
   optional coefficient branch-stats table is narrowed to its tx/plane/ref rows
   before the token walk. Focused parity stayed green; the BCE report no
   longer shows the qcoeff zero-loop load or per-token branch-stats
   `[band][ctx]` selection sites. Focused samples stayed 0 allocs and measured
   ~1.80-1.82 µs direct sparse, ~9.15-9.24 µs direct dense, ~2.07-2.09 µs
   staged sparse, ~8.80-8.84 µs staged dense, and ~281.2-285.7 ns combined
   pack+commit.
   Coefficient-context stamping now uses an offset-based fixed-width helper for
   replay/context-only paths instead of materializing above/left slices solely
   to stamp them. Focused pack+context tests stayed green; the replay stamp
   store checks collapsed to the helper preflight edges, and combined
   pack+commit samples stayed 0 allocs at ~278.8-279.3 ns.
   The VP9 lookahead/denoiser image-copy helper now bulk-copies contiguous
   full-width Y/Cb/Cr planes, keeping the old row-loop behavior for padded
   planes. This trims the default realtime denoiser count save/restore copies:
   `BenchmarkCopyVP9LookaheadImage` moved a 720p 4:2:0 copy from
   ~28.1-28.4 µs to ~17.2-17.3 µs, still 0 allocs, while
   `TestCopyVP9LookaheadImageMatchesRowLoop` pins padded and contiguous
   equivalence.
   After PGO refresh, repeated 120-frame phase spots stayed
   topology-equivalent at 108 encoded / 12 dropped with govpx at 1,234,808
   bytes versus 1,235,204 libvpx bytes; the latest post-copy run measured
   12.78 ms/frame govpx vs 5.68 ms/frame libvpx with `vp9_count_ns`
   1,302,999,374 and tile write 153,222,785.
   The nonrd picker now also hoists block-invariant RD multiplier state and
   interpolation-filter bit costs, passes the already-cached source-SAD
   classification into the intra fallback, and reuses a per-frame intra Y-mode
   cost table built from the frozen nonrd mode-cost context instead of
   rebuilding it for every fallback probe. Focused nonrd/denoiser tests stayed
   green, `make pgo-refresh` + `make pgo-check` passed, and the post-refresh
   120-frame VP9 spot stayed topology-equivalent at 108 encoded / 12 dropped
   with the same 1,234,808 govpx bytes versus 1,235,204 libvpx bytes; the run
   measured 12.83 ms/frame govpx vs 5.75 ms/frame libvpx with `vp9_count_ns`
   1,307,315,494 and tile write 152,503,054. A later source-SAD cache guard
   skips the generic `EnsureLen*` helpers on already-sized per-SB content-state
   slices; focused content/source-SAD and VP9 predictor tests stayed green,
   `make pgo-refresh` + `make pgo-check` passed, and the post-refresh loaded
   120-frame spot stayed byte/topology-equivalent at 108 encoded / 12 dropped
   with 1,234,808 govpx bytes, measuring 12.92 ms/frame with `vp9_count_ns`
   1,312,742,958 and tile write 157,071,132.
   A follow-up offset-only SAD cleanup now routes already-bounded source-SAD,
   variance-partition chroma SAD, the CBR 64x64 SAD guard, and the compact
   motion-candidate SAD through `BlockSADOffsets` after their callers compute
   byte offsets. Focused source-SAD/color-sensitivity/prediction tests stayed
   green, `make pgo-refresh` + `make pgo-check` passed, and the post-refresh
   120-frame VP9 spot stayed topology-equivalent at 108 encoded / 12 dropped
   with 1,234,808 govpx bytes versus 1,235,204 libvpx bytes, measuring
   12.86 ms/frame govpx vs 5.82 ms/frame libvpx with `vp9_count_ns`
   1,308,973,457 and tile write 154,538,004.
   The luma scratch predictor now also direct-convolves single-reference
   non-scaled blocks into the caller's compact buffer, while compound/sub-8/
   scaled and unsupported shapes still fall back to the decoder reconstruction
   wrapper. The existing scratch-vs-recon predictor gate covers copy, edge,
   border subpel, inner subpel, all four filters, and padded custom scratch;
   `make pgo-refresh` + `make pgo-check` passed, and the post-refresh
   120-frame VP9 spot stayed byte/topology-equivalent at 108 encoded / 12
   dropped with 1,234,808 bytes, measuring 12.58 ms/frame with `vp9_count_ns`
   1,284,022,135 and tile write 147,371,508.
   Remaining work: per-transform fixed scan/qcoeff/token-cache specialization
   for the remaining StageCoefBlock / WriteCoefSb bounds checks, then evaluate
   single-walk packing: store tokens at count time and pack from the token list
   (vp9_bitstream.c shape) instead of re-walking modes — also deletes the +0.43
   write-pass re-checks. The
   `Writer.Write` byte-emit outline attempt is rejected below; do not retry
   without a new compiler profile. Also rejected by 2026-07-02 phase spots:
   clipped scalar chroma-SAD thresholding (lost to the NEON full-SAD path), an
   index-cursor rewrite of `packTokenBlockAndHasResidueWindow` (tile write
   regressed on repeats), and a residual-loop source-plane hoist (count time
   regressed despite identical topology). The next measured lane rejected
   per-plane token band-table hoisting, offset-based entropy-context read
   helpers, MV-pred no-limit SAD offset hoisting, intra-fallback `BlockYrd`
   source-window hoisting, precomputed `BlockYrd` FP params (microbench win but
   flat/regressed end-to-end), a per-plane UV scratch predictor, direct
   coefficient-window `WriteCoefSb` args/pointer bundles, and a zero-MV luma
   copy+variance shortcut whose full-frame ref reads lost the cache-locality
   benefit of the compact predictor scratch. A qcoeff-value cache inside
   `stageCoefBlockQCoeff` also failed its phase spot after neutral/worse
   focused samples. A cached subpel reference-view thread through nonrd NEWMV
   and subpel scoring kept the microbench win (~173 ns vs ~209 ns helper) but
   did not improve the 120-frame phase spot. A narrower visible-reference
   subpel scorer bypass also failed the connected A/B despite a focused scorer
   win: visible spots were 11.97 / 12.04 / 11.91 ms/frame versus disabled-
   control spots at 11.97 / 11.97 / 11.79, so the bypass was removed and the
   padded-reference scorer stayed. A staged-token no-discard writer
   specialization lost after PGO despite removing generic `Writer.Write` from
   one profile. A prechecked `SubtractBlockNonZero` route from
   `gatherVP9TxResidual` kept focused subtract/residual tests green, but
   connected 120-frame spots stayed in the loaded/regressed 13.57-13.71 ms/frame
   band, so it was reverted.
3. **Final-recon quantize path**: PARTIAL 2026-07-02 — `BlockYrd` now uses a
   package-internal validated quantize_fp entry on arm64, avoiding the public
   dispatch guard after the caller has already bounded transform sizes and
   scratch. The realtime final FP path now uses the same validated scan-order
   entry with encoder-owned qcoeff scratch; `BenchmarkVP9QuantizeFPWithQScanOrderValidated`
   improved arm64 regular→validated from ~13.2→11.0 ns (4x4), ~17.1→14.6 ns
   (8x8), and ~36.1→32.0 ns (16x16), all 0 allocs. The final-recon fast
   quantize caller now holds a `*ScanOrder` and routes through the pointer-form
   validated entry; public `QuantizeFPLibvpx` stays guarded. The realtime
   inter FP quantizer now also uses per-frame/per-segment FP tables derived
   alongside the dequant tables, matching libvpx's plane-table ownership
   instead of rebuilding `(round_fp, quant_fp)` for every transform block.
   `TestQuantizeFPWithPrecomputedTablesMatchesValidated` proves qcoeff,
   dqcoeff, and EOB equivalence against the old validated wrapper. Focused
   arm64 samples stayed 0 allocs and moved regular / validated / precomputed
   from ~13.34-13.39 / ~13.02-13.06 / ~7.52-7.64 ns (4x4),
   ~16.95-17.15 / ~16.52-16.62 / ~11.21-11.28 ns (8x8), and
   ~35.91-36.14 / ~35.29-35.61 / ~28.79-29.03 ns (16x16). A 120-frame cpu8
   encode-only spot check stayed topology-equivalent at 108 encoded / 12
   dropped, measuring 13.48 ms/frame govpx vs 5.80 ms/frame libvpx on this
   noisy single run. The realtime inter FP commit path now bypasses the
   trellis-capable wrapper when no trellis hook is possible and quantizes
   directly into the committed qcoeff/dqcoeff buffers before inverse-add,
   matching libvpx's `vp9_xform_quant_fp` output ownership. The focused
   wrapper-vs-bare benchmark stayed 0 allocs and moved tx8 from ~56.4-57.0 ns
   to ~54.0-54.5 ns, and tx16 from ~229-231 ns to ~217-219 ns; a focused
   equivalence test covers qOut/no-qOut and tx4/8/16/32 (including 32x32 LP
   variants). A follow-up prechecked DCT/FP commit helper now lets the realtime
   residual loop validate tx/dequant/scan/table invariants once per plane and
   call the hot quantize+inverse path directly for each tx block. The
   tx-candidate scorer and commit-time context stamper now use the same
   prechecked helper and clear local q/dq scratch with `clear`, leaving the
   checked wrapper for external/safety-call shapes. The wrapper/bare/prechecked
   benchmark stayed 0 allocs and measured tx8 ~56.6-57.2 / ~54.3-54.5 /
   ~53.1-54.3 ns and tx16 ~228.8-229.7 / ~217.8-218.7 / ~217.2-218.1 ns.
   Source-checking libvpx `vp9_encodemb.c` showed realtime `quant_fp` consumes
   only `SKIP_TXFM_AC_DC` for luma; `SKIP_TXFM_AC_ONLY` is a non-FP path fast
   case and must not be invented here. The commit loop now honors that AC/DC
   luma skip for realtime nonrd, segment-0, non-lossless FP blocks while still
   letting chroma decide the final skip bit. After PGO refresh, the latest
   120-frame cpu8 phase spot stayed topology-equivalent at 108 encoded / 12
   dropped and moved govpx bytes to 1,234,808 vs 1,235,204 libvpx, measuring
   13.12 ms/frame govpx vs 5.90 ms/frame libvpx on a noisy single run with
   `vp9_count_ns` 1,338,440,002 and tile write 156,041,546. A narrow
   `BlockYrd` follow-up now stores per-tx EOB scratch as `int16`; EOB is bounded
   by the tx coefficient count (<=256 after the realtime TX_16X16 clamp), and
   this cuts the stack-clear footprint without changing the libvpx scoring
   shape. Focused `BenchmarkVP9BlockYrd` samples moved from ~540-549 ns/op to
   ~517-522 ns/op, still 0 allocs. After `make pgo-refresh` + `make pgo-check`,
   repeated 120-frame VP9 spots stayed byte/topology-equivalent at 108 encoded /
   12 dropped with 1,234,808 bytes and measured 12.44, 12.58, and
   12.46 ms/frame; a profiled repeat measured 12.58 ms/frame with
   `vp9_count_ns` 1,280,381,709 and tile write 151,097,543, and the old
   `BlockYrd` EOB-buffer declaration sample was absent from the line profile.
   A follow-up now right-sizes that EOB scratch to 16/64/256 `int16` slots by
   actual tx-unit count, so the common TX_16X16 path avoids clearing the TX_4X4
   worst case. Focused samples moved from ~514-518 ns/op to ~506-513 ns/op,
   still 0 allocs; after PGO refresh/check, 120-frame spots stayed
   byte/topology-equivalent at 12.59, 12.63, and 12.42 ms/frame.
   A narrower attempt to derive `eob_cost` from `txIdx` instead of incrementing
   it in the loop was neutral-to-worse in focused `BenchmarkVP9BlockYrd` samples
   (~515-526 ns/op after a ~511-523 ns/op baseline) and was reverted.
   The phase-3 A6 continuation now compacts per-SB q/dq coefficient staging by
   tx-block `maxEob` instead of reserving 1024 slots at every 4x4 origin. The
   three-plane q+dq footprint falls from 3 MiB to 48 KiB while the 256-cell EOB
   map remains unchanged. Exhaustive block-shape/tx layout coverage proves
   non-overlap and exact coefficient coverage; broad parity gates stay green.
   Two order-reversed no-PGO 480-frame 4T pairs kept exact 4,981,549-byte output
   and 468/12 topology while improving about 0.13-0.17%, and the paired profile
   moved `WriteCoefSb` cumulative CPU from 500 ms to 340 ms.
   The next safe point removes persistent dqcoeff staging: quantize and
   inverse-add share the encoder's reusable 1024-entry tx scratch, while the
   later token walk consumes only retained qcoeff plus EOB. Persistent SB
   coefficient storage is now qcoeff-only at 24 KiB across three planes. Two
   order-reversed 480-frame 4T pairs improved another 0.57-0.74% with exact
   bytes/topology, and the 120-frame plus 2000-frame ML gates stayed exact.
   The following safe point removes the root encoder's per-tx qcoeff/EOB
   callbacks from the count-token walk. `WriteCoefSb` now indexes the existing
   compact tx-block-major stores directly through two fixed-array pointers,
   and the production pointer entrypoint avoids copying the full leaf argument
   bundle after token collection has already mutated it in place. Five
   interleaved post-PGO 480-frame 4T pairs kept exact 4,981,549-byte output and
   468/12 topology while improving 0.16-1.59%, with a 0.57% median. In the
   paired profile, sampled cumulative `writeVP9ModeBlock` time fell from 120 ms
   to 40 ms. Full, pure-Go, trace, conformance, strict byte-parity, focused
   changed-path race, post-PGO 1T/4T, and 2000-frame ML gates pass. The broad
   root race run still reports the pre-existing frame-parallel token-buffer,
   decision-cache, and last-source sharing races; no report touched this
   coefficient sidecar path.
   The phase-3 A4 continuation now removes the finalized 120-byte inter-leaf
   cache store from normal packed count walks while retaining it for
   SVC/denoiser/active-map, non-preserved coding state, inactive or
   already-failed token staging, and tx-mode-demotion reruns. The 120-frame
   cpu8 4T no-denoise phase spot stayed
   exact at 1,236,037 bytes and 108/12 topology, with 237,683 packed replay
   hits, zero misses, and cache stores reduced from 237,683 to zero. Three
   heavily loaded alternating pairs reduced process retired instructions by
   about 0.05%, 0.29%, and 0.08%; wall time was not accepted under that load.
   A follow-up makes the finalized cache lookup write-only at the mode-block
   boundary and shares one result across inter/intra replay checks. Three
   no-PGO pairs stayed exact while reducing retired instructions by about
   0.13%, 0.04%, and 0.12%; a later five-pair wall check was mixed with a
   -0.03% median and is treated as neutral.
   Remaining work:
   stage coefficient tokens transactionally during residue production without
   emitting tokens when the final leaf skip decision wins, then continue
   toward single-walk packing. Port exactly what vp9_encodemb.c does at this
   speed level.
4. **Intra fallback diet**: source-check before changing. The original plan
   note was too aggressive: pinned libvpx v1.16.0 `estimate_block_intra` still
   calls `block_yrd` for plane 0, and the nonrd intra fallback path calls
   `model_rd_for_sb_y` followed by `block_yrd`. Do not delete govpx's
   tx-domain/token-cost work here without a newer source citation or a
   byte-identity proof that the source-shaped path changed.
5. **Pick-loop batching**: PARTIAL 2026-07-02 — the regular full-RD
   `BIGDIA` / `HEX` / `SQUARE` pattern searches now have `VpxSad4D` batch
   wrappers and the full-RD dispatcher uses them when the active block is at
   least 16 pixels wide. The existing fast-hex/fast-diamond/N-step batch hooks
   already covered the other full-pel search branches. Focused medians on the
   real VP9 SAD/x4 SAD kernels improved bigdia from ~1456 ns to ~897 ns, hex
   from ~1137 ns to ~848 ns, and square from ~1436 ns to ~893 ns, all 0 allocs.
   Remaining: re-profile the full pick loop, flatten any still-hot
   per-candidate recomputation, and stop when re-profile shows <0.2 ms/f in
   reach.

### P2 — VP9 encode 4T serial-fraction round 2 (target 4.2-4.7 → ~3.6 ms/f)
Sequenced after P1.1 (it changes what workers share):
varPart-state sharing by design (dispatcher-clears-once + tile-column
ownership, ~15 arrays — the recipe that worked for miGrid/recon; real parity
risk, own round); enc-border recon layout (160px borders in pool buffers →
in-place `vpx_extend_frame_borders`, deletes the last LAST-mirror build);
parallel FrameCounts accumulation; lf mask-prep parallelization.

### P3 — VP8 encode (current headline gap 1.50x; target ~1.25-1.35x)
Attack the three real deltas: pick/scoring +0.42 (selectFastInterFrameMode
loop shape vs vp8_pick_inter_mode), final recon +0.40 (fdct batch + fused-path
completion), search +0.30, denoiser apply +0.14. Reuse the instrumented-vpxenc
counter method from round 1.

### P4 — decoders (optional; near floor)
VP9: MI pointer-grid (est 1-1.5%), inter-predict plane-fn arg flattening,
selective-walk window checks. VP8: mode-grid read fusion. Only if a sprint
has spare capacity.

### Parity workstream (parallel, independent)
1. **480-frame benchmark frontier**: PINNED 2026-07-02 by
   `TestVP9BenchmarkSyntheticByteParityFrontier` (opt-in
   `GOVPX_VP9_BENCH_SYNTH_FRONTIER=1`, `govpx_oracle_trace`): the 720p /
   2500 kbps / realtime cpu8 synthetic benchmark emits 468 packets on both
   govpx and libvpx and drops the same 12 input frames. Emitted packet 0 is
   byte-exact; the first divergence is emitted packet 1, source frame 10
   (as of the 2026-07-10 temporal-denoiser variance-threshold/count-state
   replay safe point: govpx 11179 bytes, libvpx 11136 bytes, first byte diff
   4). Keep this
   frontier stable while landing perf work, and update this section if it
   moves.
2. **480-frame drift**: the 480f synthetic is NOT byte-exact (4.76 vs 4.75
   MiB). Earlier notes treated 120f as the standard clean gate, but the
   packet-aligned 480f benchmark frontier above is the current benchmark
   workload truth. The 2026-07-10 safe point activated the stored denoiser
   level, ported the denoiser-specific variance threshold, and narrowed this
   packet's size gap from 260 bytes to 43 bytes, but did not make the stream
   byte-exact. Diagnose the remaining first-packet drift before claiming 480f
   byte-identity for P1.1.
3. Production-stream fuzz reds seeds 2-5 (pre-existing, task #6).

## Method discipline (unchanged from campaign, plus new rules)
- Byte-identity gates per commit; extend the standard gate from 120f to also
  cover 480f once the drift item is resolved.
- Interleaved A/B medians (≥5 pairs) for every claim; microbench + profile
  share for attribution; one clean serial run for headline numbers.
- Wall-clock adjudication REQUIRED before targeting any `runtime.*` profile
  row (SIGPROF artifact rule).
- Rejected-experiments registry (do not retry): branchless bool reads (−10%
  on Apple predictors), GOGC/GOARM64/preempt knobs (nil), eager predbuf
  copies (copy overhead ate the win), `Writer.Write` byte-emit outlining
  (inline cost improved 192→157 but arm64 microbench regressed ~5-7%),
  direct final-recon q/dq output slices (copy removal was neutral-to-worse on
  the 720p cpu8 1T workload: 13.54 / 14.06 / 13.70 ms samples against the prior
  13.52 ms spot), per-frame token-cost table (breaks zero-alloc via escape
  analysis — redo only as pre-allocated per-worker), padded coefficient-band
  table pointer in token staging (moved BCE around but regressed sparse
  stage+pack from ~2.44 µs to ~2.46 µs), scratch-only filter-search convolve
  via `vp9SubpelReferencePlane` (not byte-equivalent to the recon path in the
  local oracle: 64x64 eighttap subpel, 32x32 smooth fullpel, and 16x16 sharp
  subpel cases all mismatched), bespoke final-recon fast wrapper around
  `quantizeVP9TxResidualWithQTrellis` (focused equivalence passed, but the long
  benchmark crashed in the tx16 general path and the surviving short run only
  showed a sub-2% microbench delta; keep the smaller validated quantizer change),
  local `WriterState` token replay (the first version escaped at 1 alloc/op;
  the pointer-free version restored 0 allocs but regressed combined pack+commit
  to ~327-335 ns),
  routing the exported `GetCoefContext` mirror through the decoder's array
  helper (parity stayed green, but it did not clear the encoder BCE sites and
  focused token benches were noise-neutral to worse),
  small-block scratch `vp9InterPredictionVarianceSSE` for the simple-yrd /
  no-reuse subset (byte-equivalent, but cpu8 samples regressed badly after the
  safe gate; the missing zero-MV copy fast path explained a plane-count spike,
  but even after restoring counts the branch did not recover on the target run),
  cached subpel reference-view plumbing through NEWMV/scorer (focused scorer
  won, but guarded/lean 120-frame phase spots were neutral-to-worse), visible-
  reference subpel scorer bypass (focused scorer won, but visible 120-frame
  spots tied/lost to the disabled-control run), staged-token no-discard writer
  specialization (removed generic `Writer.Write` in profile, but post-PGO
  120-frame spots regressed to ~12.61 ms/frame), prechecked
  `SubtractBlockNonZero` from `gatherVP9TxResidual` (focused tests passed but
  connected phase spots stayed in the 13.57-13.71 ms/frame loaded/regressed
  band), deriving `BlockYrd` `eob_cost` from `txIdx` (focused samples were
  neutral-to-worse).
- Agent split: root-package vs internal/ file boundaries; coordinator merges
  with the full parity lane on every VP8-touching merge.

## Expected end-state
P0+P1 land: VP9 1T ≈ 9.5-10.5 ms/f (~1.7x). P2: 4T ≈ 3.6 ms/f. P3: VP8
≈ 1.3x true. Decoders already at 1.16-1.36x. Honest floor for Go vs C+NEON
on the encode paths is likely ~1.4-1.6x; anything below that requires
rethinking Go codegen per-loop (unrolled asm mega-kernels for the token
loops), which is out of scope for phase 2.
