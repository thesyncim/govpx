# Perf phase-3: structural program

Status: implementation started. The oracle-denoise prerequisite, narrow
A1/A2 token-walk safe points, the first A5 pick-buffer safe point, the
A6 bare-quantize / AC-DC skipTxfm commit safe points, and a denoiser
count-copy safe point plus a nonrd picker invariant-hoist safe point, a
small offset-SAD cleanup, a `BlockYrd` EOB-scratch narrowing safe point,
a VP9 MV-pred/fullpel callback shape cleanup, a VP9 ARM64 wide-SAD
dispatch safe point, a VP9 ARM64 32x32 subpel-variance scratch narrowing safe
point, a VP9 lazy NEWMV best-ref MV write/count safe point, a VP9 intra-H
predictor fixed-size fill safe point, a VP9 TM-predictor no-clip fast path,
an exact-window VP9 entropy-context helper safe point, a normal inter
partition-node count/write replay safe point, and the VP9 denoiser tile-MT
gate removal safe point, plus a threaded nonrd intra-cost seed plumbing safe
point, plus threaded non-LAST subpel-reference cache sharing, plus a
flattened nonrd ref-MV/mode-mask call-shape cleanup, plus a phase-stats
`ChoosePartitioning` allocation artifact fix, plus a VP9 inactive SB-search
entropy-wrapper fast path, and the VP8 compact previous-frame MV sidecar plus
fast-picker final-mode copy-elision safe points, plus the complete VP9 decoder
row-MT PARSE/RECON/LPF queue for one- and multi-tile streams, plus the VP9
temporal-denoiser variance-threshold and committed count-state replay safe
point landed on
2026-07-02/03/10; the larger A/B and encoder-MT structural programs remain
pending. This is the execution brief
for implementation agents.
Evidence: 2026-07-02/03 design sprint — three verified blueprints (VP9
single-walk, VP8 MB-walk, MT program) built on first-hand reads of libvpx
v1.16.0 C and the current govpx tree, plus fresh A/B measurements. Supersedes
the *structural* sections of `perf-phase2-plan.md` (whose P0/P1 call-shape
items are largely landed or in-flight); measurement-discipline rules there
still apply.

Baseline drift warning: ~2600 lines of P0/P1 work were uncommitted in the
main worktree at design time. Re-baseline before/after it lands; the
structural deltas below survive it, exact ms/f numbers may shift.

Measurement note, 2026-07-03: the `govpx_phase_stats` build used to make
`ChoosePartitioning` take the address of its by-value args just to recover the
stats pointer, forcing a stats-only heap artifact in the variance-partition
hot path. The stats path now calls `ChoosePartitioningWithStats` directly.
The 120-frame 720p realtime cpu8 4T no-denoise phase report dropped from about
220 allocs/frame to 2.15-2.21 allocs/frame; normal non-phase builds remained
near-zero at about 0.13-0.29 allocs/frame. Treat older phase-stats allocation
rows for VP9 threaded partition work as polluted.

Measurement note, 2026-07-10: the denoiser-enabled variance-partition path
still followed the non-denoiser `scale_part_thresh_sumdiff` branch even though
the pinned libvpx build has `CONFIG_VP9_TEMPORAL_DENOISING=1`; the root
denoiser also re-extracted a level from the raw estimator value every frame
instead of consuming the stored `ne->level`. Porting
`vp9_scale_part_thresh`, threading the live denoiser/SVC temporal state, and
using the stored level moved the 120-frame 1T threshold-2 average from 129075
to 160065 versus libvpx's 159984. Count-pass mode blocks fell from 279770 to
231090 versus libvpx's 238940. Because the real denoiser had previously kept
count-token replay disabled, the final count walk now commits its denoiser
state transactionally and the write walk replays every cached leaf; the phase
spot recorded 216690 replay hits and zero misses. The tagged 1T wall moved
from 12.05 to 10.56 ms/frame; after PGO refresh the normal 120-frame spots
were 10.43 ms/frame 1T and 3.70 ms/frame 4T. Three normal 480-frame 4T repeats
were 4.06/4.08/4.09 ms/frame at 0.57-0.59 allocs/frame. The quality run stayed
close to libvpx at -0.046 dB PSNR and +0.000019 SSIM, with identical 108/12
encoded/drop topology. The pinned 480-frame first divergence remains emitted
packet 1/source 10/byte 4, while its size gap narrowed to 11179 versus 11136
bytes. Focused denoiser/oracle tests, the 4T race gate, full tests, trace,
purego, and PGO checks are the safe-point gates.

Measurement note, 2026-07-10 (threaded cache ownership): an Apple Time
Profiler capture of the 4T workload attributed about 0.51 s of sampled CPU to
copying worker 0's leaf and partition decision caches back to the dispatcher.
The count barrier already gives those buffers exclusive ownership: tile column
0 is written by the dispatcher, while worker 0 stays quiescent until the next
count epoch. The caches and their row/column/version metadata now ping-pong
between worker 0 and the dispatcher instead of copying several megabytes per
frame. Three paired 480-frame 720p realtime cpu8 4T spots improved from
4.024/4.025/4.036 ms/frame to 3.941/3.956/3.959 ms/frame (about 1.8-2.1%),
with identical 4,980,319-byte output, 468/12 encoded/drop topology, and the
same allocation band. The focused ownership test, threaded race gate,
denoiser oracle matrix, full suite, strict byte parity, trace, and purego gates
all passed.

Measurement note, 2026-07-10 (reference border lifetime): every nonzero
refresh mask previously invalidated all per-slot GOLDEN/ALTREF padded luma
planes, so the normal refresh-LAST cadence rebuilt unchanged non-LAST borders
before count workers could share them. Invalidation now follows the physical
refresh bitmask; explicit reference replacement invalidates only the replaced
slot, while frame-parallel clone setup retains its intentional all-slot reset.
Three paired 480-frame spots stayed byte/topology-identical. The 1T median
moved from 11.558 to 11.453 ms/frame (about 0.9%); the 4T median moved from
3.950 to 3.844 ms/frame (about 2.7%). The selective-invalidation unit test,
worker-sharing race gate, denoiser oracle matrix, and full suite passed before
the strict publish gates.

Measurement note, 2026-07-10 (single threaded reconstruction preparation):
the frame entry point already acquired and 128-filled a fresh reconstruction
target for each count attempt, but the threaded count dispatcher repeated the
full padded-plane preparation immediately before launching tile workers. The
workers now reuse the entry-point preparation and only clear the shared
mode-info grid. Three paired 480-frame 720p realtime cpu8 4T spots improved
from 3.782/3.780/3.829 ms/frame to 3.747/3.762/3.802 ms/frame (about 0.5%
median-to-median and 0.5-0.9% pairwise), with identical 4,980,319-byte output,
468/12 encoded/drop topology, and the same near-zero allocation band. The
focused threaded tests, worker race gate, denoiser oracle matrix, full suite,
strict byte parity, trace, purego, and PGO checks all passed.

Measurement note, 2026-07-10 (direct variance-partition handoff): threaded
count collection merged each worker's tile-disjoint variance-partition state
into the dispatcher, copied the merged arrays into 15 replay buffers, then
copied them straight back immediately before tile write. No operation between
the count barrier and tile dispatch mutates those arrays, including the
reduced-tx retry, which starts a fresh count attempt. Tile-write workers now
consume the merged dispatcher state directly. Three paired 480-frame 720p
realtime cpu8 4T spots moved from 3.677/3.726/3.791 ms/frame to
3.681/3.709/3.754 ms/frame (about 0.46% median-to-median), with identical
4,980,319-byte output and 468/12 encoded/drop topology. The change also
removes 134 lines of snapshot plumbing and the retained replay buffers. The
refreshed-PGO artifact measured 3.692 ms/frame; the focused race/oracle gates,
full suite, strict byte parity, trace, purego, and PGO checks all passed.

Measurement note, 2026-07-10 (caller-backed denoiser rollback): the final
count walk now commits denoiser state transactionally, but each count attempt
still copied both full YUV working images into retained rollback buffers. The
synchronous caller image is immutable for the duration of EncodeInto and is
the exact pre-count state used to initialize both working images, so rollback
now restores from that input only on the reduced-tx/error/cannot-replay paths.
Normal frames take no rollback snapshots and the two retained backup images
are gone. Three paired 480-frame 720p realtime cpu8 4T spots improved from
3.740/3.785/3.740 ms/frame to 3.661/3.725/3.728 ms/frame (0.3-2.1%, median
paired gain about 1.6%), with identical 4,980,319-byte output and 468/12
encoded/drop topology. Under a heavily loaded host, paired 1T whole-process
CPU time was lower in all three runs at 16.62/16.63/16.67 s versus
16.76/16.77/16.69 s controls; the 1T stream stayed identical at 4,983,461
bytes and 468/12.

Measurement note, 2026-07-03: the realtime VP9 count/write leaf path now calls
`prepareVP9InterBlockResidue` directly when no SB-entry skip-encode entropy
snapshot is active, leaving `vp9WithSBSearchEntropy` only on the deep-RD
snapshot path. This avoids a no-op closure wrapper on the normal realtime hot
path without changing the source-shaped skip-encode behavior. Focused
deep-RD/use-partition/replay tests and `git diff --check` stayed green. The
120-frame 720p realtime cpu8 4T phase spots stayed byte/topology-identical:
no-denoise 4.68 / 4.64 ms/frame with count at 431.5 / 428.4 ms total, and
default denoise 4.86 / 4.98 ms/frame with count at 448.7 / 462.1 ms total.
Treat this as a small call-shape cleanup, not closure of A3 or C1.

Probe note, 2026-07-03: a serial-VP9-LF threshold probe was rejected. Forcing
the VP9 loop filter to stay serial while count/write tile work remained
threaded reduced worker wake signals on the 120-frame 720p realtime cpu8 4T
no-denoise phase spot (1296 -> 972), but raised LF apply time from about
24.5 ms total to about 39.4 ms total and wall clock from 4.29 ms/frame to
4.67 / 4.71 ms/frame, with identical bytes/topology. Keep the current threaded
LF split; future C1/C2 work should reduce worker overhead without serializing
the filter.

Probe note, 2026-07-03: a write-pass synchronous-prep worker probe was
rejected. Preparing helper encoders on the dispatcher removed the separate
encode-prep wake epoch (120-frame wake signals 1296 -> 972) and preserved
bytes/topology, but the longer 480-frame normal no-denoise gate regressed from
4.024 ms/frame control to 4.072 ms/frame. Keep the parallel helper-prep epoch;
future worker-overhead work needs a merged handshake or actual row/tile
batching, not serialized state copies.

Probe note, 2026-07-03: a merged encode-prep+encode worker probe was rejected.
Tile column 0 was moved onto a private worker and every tile job prepared its
own worker at encode-job start, removing the standalone encode-prep wake while
keeping state copies parallel. Focused threaded/replay tests stayed green and
bytes/topology were unchanged (`1,236,037` bytes with `108/12` on 120 frames,
`4,981,549` bytes with `468/12` on 480 frames), but wall clock regressed to
about 4.65 ms/frame on both connected no-denoise spots. Keep tile column 0 on
the shared encoder and retain the current helper prep epoch until a real
merged handshake or row/tile batching design is ready.

Probe note, 2026-07-03: a VP9 4-thread no-denoise PGO-coverage probe was
rejected. Adding a 120-frame 720p realtime cpu8 `-threads=4
-noise-sensitivity=0` capture to `make pgo-refresh` and merging it into
`default.pgo` kept bytes/topology stable but did not clear the connected
gate. The 4T paired old/new spots were split with only a small median nudge
(old median about 4.93 ms/frame, new about 4.82 ms/frame), while the 1T
default realtime paired median moved the wrong way (old about 12.32 ms/frame,
new about 12.65 ms/frame). Keep VP9 PGO training on the existing 1T realtime
profile unless a broader production-profile mix can prove no 1T regression.

Probe note, 2026-07-03: a skip/no-residue token-marker probe was rejected.
Omitting explicit EOSB terminators for empty coefficient leaves and allowing
empty SB-row token lists preserved VP9 bytes/topology and replay hits, but it
failed the existing no-residue token-list contract and did not improve the
connected gate: 480-frame normal no-denoise measured 4.05 ms/frame versus about
4.03 ms/frame controls. Keep explicit EOSB terminators unless the broader
single-walk token-stream design replaces this replay contract.

Probe note, 2026-07-03: a VP8 denoiser cached inter-ref-state probe was
rejected. The idea was to reuse the decoder grid fast path's per-reference
geometry inside motion-compensated denoising instead of rebuilding
`frameInterRefState` per macroblock, but the current profile only attributed
about 20 ms over the 480-frame overload run to that setup. The connected
120-frame 720p realtime cpu8 phase spot was byte/topology-identical and
0 allocs/frame, but moved from 7.60 ms/frame and 459.9 ms total inter
reconstruct to 7.68 ms/frame and 465.5 ms total inter reconstruct. After
reverting the probe, the same phase spot remained byte/topology-identical at
1,328,027 bytes and 118 encoded / 2 dropped, with 0 allocs/frame, 7.63
ms/frame, and 461.7 ms total inter reconstruct. Keep the public per-MB
reconstruction API shape until VP8 work targets the real wall: loop-filter
picker trials and the broader inter-reconstruct path.

## Current gaps (720p realtime, post-P0/P1-partial)

| Front | ms/frame govpx vs libvpx | ratio |
|---|---|---|
| VP9 encode 4T cpu8 denoise | 4.24 vs 2.10 | 2.02x |
| VP9 encode 1T cpu8 denoise | 11.91 vs 5.57 | 2.14x |
| VP9 encode 4T no-denoise | 4.65 vs 2.18 | 2.14x |
| VP8 encode overload | 8.0 vs 5.25 | 1.53x |
| VP9 decode | 1.99 vs 1.51 | 1.32x |
| VP8 decode | 1.95 vs 1.65 | 1.18x |

## ALERT — oracle config audit (do before ANY denoise-related work)

Resolved 2026-07-02. `internal/coracle/build_libvpx_vp9.sh` and
`internal/coracle/build_vpxdec_vp9.sh` now configure libvpx with
`--enable-vp9-temporal-denoising`; both rebuilt VP9 config trees report
`CONFIG_VP9_TEMPORAL_DENOISING 1`, and `make vp9-vpxdec-tools` /
`make vp9-dsp-oracle` now audit the relevant `vpx_config.h` before accepting
their outputs. Standing rule remains: check `vpx_config.h` of every oracle
binary before trusting a feature-gated comparison.

## Program A — VP9 encode single-walk + pick-buffer dataflow
Target: 13.6 → ~9.4-10.2 ms/f (~1.6-1.7x). Full blueprint: agent report
"VP9 single-walk architecture" (this session); key verified facts:

- The probability-ordering problem is ALREADY SOLVED in govpx: tokens are
  symbolic (`TokenExtra.ProbOff` ≡ libvpx TOKENEXTRA context_tree pointer;
  libvpx mutates fc in place in write_compressed_header BEFORE encode_tiles
  packs). Only the pack walk's shape is wrong.
- A1 is partial: the count walk no longer emits partition bits,
  keyframe/fallback mode fragments, or inter-leaf mode fragments after the
  explicit syntax histograms have been updated. Coefficient-token replay now
  commits above/left contexts from the staged TOKENEXTRA stream via the combined
  pack+context path for inter, forced-intra, and keyframe/intra leaves instead
  of reopening qcoeff/eob buffers. Residual/token side effects still need the
  later single-walk sidecar/token-stream work before the discard path can be
  deleted outright.
- A3 has a narrow new safe point: normal inter partition nodes now get a
  count-pass decision cache keyed like the keyframe/deep-RD partition caches,
  and the write pass replays those nodes only when count coding and token replay
  are preserved under the same no-SVC/no-denoiser/no-active-segment-map safety
  envelope as leaf replay. This is not pure pack: the count pass still runs
  `pickVP9InterPartitionBlockSize`, but the write-side var-part picker is
  essentially gone for inter frames (`varpass count=23540 write=240` on the
  120-frame denoise spot, and `count=3740 write=240` on the 30-frame
  threads=4/no-denoise spot). Gates: focused replay/cache tests, full
  `go test ./... -count=1`, 120-frame cpu8 denoise phase spot at 11.81 ms/frame
  vs 5.55 ms/frame libvpx with count=9.99 ms and tile=1.20 ms, plus a
  threads=4/no-denoise 30-frame spot at 2.95 ms/frame with 100% inter replay
  hits.
- A3 now also has a pack-only >=8x8 inter-source leaf path. Each token-list
  row carries a parallel one-byte UV-mode stream with exactly one record per
  EOSB-terminated leaf; the pack walk reads the committed `miGrid`, that
  compact syntax sidecar, and the TOKENEXTRA stream. Eligible leaves no longer
  re-enter segment selection, `canReplay` / `applyVP9CountPass*`, syntax-count
  updates, residue preparation, decision-cache stores, filter-diff replay, or
  `fillVP9MiGrid`. Forced-reference and segment-skip leaves use the same pure
  path because their committed mode info no longer depends on a leaf picker
  cache entry. The old walker remains the fallback for SVC, active-map coding,
  non-preserved count state, and sub-8x8 leaves. Paired 120-frame 720p cpu8 1T
  runs under the same host load reduced tile-write time from 141.5-143.8 ms to
  134.2-136.7 ms total (about 0.06-0.08 ms/input frame), with identical
  1,235,511-byte output and 0.6167 allocs/frame. The connected denoise spot
  remained byte/topology-equivalent at 108/12 encoded/dropped and 100% inter
  replay hits; the 30-frame 4T/no-denoise spot remained fully replayed at
  2.98 ms/frame. Token-list invariants, threaded replay, race, and focused
  SVC/RTP fallback gates passed.
- The next A3 slice stages one byte per visited partition node in a third
  SB-row stream. Eligible inter frames now descend that committed stream
  directly, emit partition bits, update partition contexts, and dispatch the
  pure leaf packer without entering `writeVP9ModesSb`,
  `pickVP9BlockSizeForRegion`, or the partition decision cache on the write
  pass. The stream has an allocation-free fixed capacity above the maximum
  full-quadtree node count and is cursor-checked beside the leaf/token streams;
  keyframe and fallback replay validate and consume the same records through
  the existing walker. Alternating 120-frame 720p cpu8 1T runs against the
  prior safe point kept exact 1,235,511-byte output and moved median tile-write
  time from about 143.1 ms to 138.6 ms total (about 0.04 ms/input frame).
  The loaded 4T/no-denoise pair also stayed exact at 1,236,037 bytes with
  identical 108/12 topology and 237,683 replayed inter leaves.
- Replay infra (a 120-byte decision in each full-width leaf-cache entry,
  `canReplay` validation, entropy
  snapshots) now sits outside the normal >=8x8 inter leaf pack path, but still
  serves count-side storage, partition/fallback replay, keyframes, and sub-8x8;
  delete it only after those remaining pack classes are pure.
- A4 has started deleting that obsolete ownership. The final leaf caller
  already stores the post-residue decision (including final skip, tx, refs,
  and UV mode) before any later lookup, so the fresh picker-side store was an
  immediately overwritten 120-byte copy on every picked leaf. Removing it
  leaves the finalized fallback cache intact. Three alternating 120-frame
  cpu8 1T no-PGO pairs kept exact 1,235,511-byte output; two pairs improved
  by about 0.08-0.09 ms/frame and median count time improved by about
  0.046 ms/frame.
- Denoiser-active token replay is still intentionally blocked: count pass
  mutates denoiser source/intra-average state, and widening replay safely needs
  an all-leaves-or-rollback transaction. The landed safe point is narrower:
  full-width denoiser/lookahead image copies now use contiguous plane copies
  while padded images keep the old row loop.
- A5 is partial: the nonrd pred-filter `search_filter_ref` sweep now keeps the
  winning luma predictor alive by swapping two compact PRED_BUFFER-style
  buffers (`blockScratch` and `nonrdFilterPredScratch`) instead of copying or
  rebuilding the best filter predictor. The picker also hoists its finalized
  RD multiplier plus interpolation-filter bit costs, shares the already-cached
  source-SAD classification with intra fallback, and reuses a per-frame intra
  Y-mode cost table from the frozen nonrd mode-cost context. Threaded
  count/encode tile jobs now carry that precomputed intra Y-mode cost
  table through `vp9CountTileSeed` instead of rebuilding it inside each worker's
  fallback probe. `TestVP9TileWorkerSeedCarriesNonrdIntraYModeCosts`,
  threaded tile alloc/parity tests, and `go test ./... -count=1` stayed green;
  the 480-frame 720p realtime cpu8 4T no-denoise spot stayed byte/topology-
  identical at 4.011 ms/frame versus the fresh 4.054 ms/frame control band,
  and the follow-up profile no longer sampled `VP9CostTokens` from
  `vp9NonrdEstimateIntraFallback` (fallback cum about 60 ms versus the prior
  120 ms sample). Count this as threaded A5 cache plumbing, not closure of the
  broader pick-buffer end-state. A smaller picker cleanup now caches the
  effective speed value and NEWMV diff-bias noise inputs once per nonrd picker
  invocation, while keeping the lowvar source-SAD lookup lazy, and reuses those
  values for the speed>=8, mv-part, large-model, and NEWMV-diff-bias gates;
  focused nonrd/threaded parity tests stayed green, the profiling 480-frame
  no-denoise spot stayed byte/topology-identical at 4.003 ms/frame, and the
  follow-up profile no longer samples nested `vp9SpeedFeatureCPUUsed` or
  diff-bias closure call sites. Later post-refresh repeats were noisy, so no
  wall-clock win is claimed; count it as cleanup only. A fresh current-frontier
  profile then showed worker-local non-LAST `vp9SubpelReferencePlane` rebuilds:
  `YV12BuildBorderedPlane` sampled at 100 ms total, with 70 ms under
  subpel-reference setup. The new safe point prebuilds usable GOLDEN/ALTREF
  padded luma references once on the dispatcher before threaded count/write
  helpers clone state, lets helpers alias those immutable buffers read-only,
  and detaches before any cold-path rebuild. This is worker cache plumbing, not
  the earlier rejected direct/scorer reference-view bypass. Guard
  `TestVP9TileWorkerPrepSharesSubpelRefBorderedReadOnly` pins alias + detach;
  focused nonrd/subpel/threaded parity tests and full `go test ./... -count=1`
  stayed green. The 480-frame 720p realtime cpu8 4T no-denoise profile stayed
  byte/topology-identical at 4,981,549 bytes and 468 encoded / 12 dropped,
  moving `vp9SubpelReferencePlane` rebuild cost to 10 ms and total
  `YV12BuildBorderedPlane` to 30 ms. The phase-timed 120-frame spot stayed
  byte/topology-identical at 1,236,037 bytes and 108/12, with count phase at
  397.6 ms total and tile write at 57.1 ms total. Normal 480-frame repeats were
  still in the loaded 4.30-4.31 ms/frame band, so count this as profile/count-
  phase cleanup rather than a wall-clock claim; `make pgo-refresh` and
  `make pgo-check` passed. A tiny follow-up flattened the nonrd picker setup
  that already matched libvpx's pre-loop `find_predictors` shape: per-ref MV
  lists are now direct table reads after a single prepass, and the hot
  `inter_mode_mask` gate is the direct bit test instead of a closure call.
  Focused nonrd/threaded parity tests stayed green. The 120-frame no-denoise
  phase spot stayed byte/topology-identical at 1,236,037 bytes and 108/12; the
  immediate restored-control comparison was a near tie but slightly behind on
  wall/count (4.692 ms/frame and 433.4 ms count) versus the flattened candidate
  (4.672 ms/frame and 432.1 ms count). The 480-frame topology stayed fixed at
  4,981,549 bytes and 468/12 with a loaded 4.42 ms/frame spot. Count this as
  picker call-shape cleanup only, not structural A5 closure. After
  `make pgo-refresh` and `make pgo-check`, the post-PGO 120-frame spot stayed
  byte/topology-identical at 1,236,037 bytes and 108/12, measuring
  4.49 ms/frame with count phase at 415.7 ms and tile write at 59.8 ms. The
  subpel tree scorer
  now also caches
  padded-reference row/min/max bounds once per block/ref
  before testing nearby MVs; the post-PGO 120-frame cpu8 spot stayed
  topology-equivalent at 13.27 ms/frame. The scratch luma predictor now takes a
  narrow zero-MV copy path before the generic decoder predictor, preserving the
  compact-scratch SAD/variance dataflow that the rejected direct-reference
  variance shortcut lost; after PGO refresh/check, the repeat 120-frame cpu8
  spot measured 13.17 ms/frame with unchanged output topology. A follow-up
  offset-walked the compact predictor scratch<->recon row copies to avoid
  per-row multiply/slice recomputation; clean pre-load 120-frame cpu8 spots
  stayed byte/topology-equivalent at 13.01 and 12.88 ms/frame with
  `vp9_count_ns` 1,324,989,751 / 1,312,619,880. PGO refresh/check stayed green.
  The source-SAD per-SB content-state cache now also bypasses repeated generic
  `EnsureLen*` calls once frame setup has already sized the slices; after PGO
  refresh/check, a loaded 120-frame spot stayed byte/topology-equivalent at
  12.92 ms/frame with `vp9_count_ns` 1,312,742,958.
  A direct single-reference luma scratch predictor now handles the non-scaled
  compact-buffer path without routing through the decoder reconstruction
  wrapper; unsupported, compound, sub-8, and scaled shapes still fall back.
  After PGO refresh/check, the 120-frame cpu8 spot stayed byte/topology-
  equivalent at 12.58 ms/frame with `vp9_count_ns` 1,284,022,135 and tile write
  147,371,508.
  A matching direct single-reference chroma predictor now handles the
  non-scaled chroma-only and single-plane chroma paths for committed nonrd
  winners and UV variance checks without routing through the decoder
  reconstruction wrapper; unsupported, compound, sub-8, and scaled shapes still
  fall back. Focused tests compare copy, border-subpel, inner-subpel, and
  single-plane U/V output against the generic predictor and keep
  `vp9NonrdUVVarianceSSE` at 0 allocs. Pre-PGO 120-frame cpu8 spots stayed
  byte/topology-equivalent at 108 encoded / 12 dropped, measuring
  12.53 / 12.21 / 12.16 ms/frame after a 12.42 ms/frame pre-change profile
  sample. After final PGO refresh/check, the guarded direct path stayed
  byte/topology-equivalent at 12.19 ms/frame with count phase at
  10.33 ms/frame.
  A small source-shaped MV-pred/fullpel callback cleanup now keeps
  `vp9_mv_pred` on libvpx's fixed three-candidate stack shape in hot callers
  and avoids building a temporary coordinate array for each 4-way fullpel SAD
  callback. Focused `BenchmarkVP9MvPredScan...` samples stayed at 0 allocs and
  moved the hot fixed-array path to ~75.2-75.7 ns/op versus ~78.4-79.0 ns/op
  for the generic slice wrapper. Pre-PGO 120-frame cpu8 phase spots stayed
  byte/topology-equivalent at 12.27 / 12.26 / 12.39 ms/frame, and the
  post-PGO guarded spot stayed byte/topology-equivalent at 12.25 ms/frame with
  count phase at 10.36 ms/frame.
  A source-shaped mode-write cleanup now computes the block-level NEWMV
  best-ref MV only when the committed leaf actually has a NEWMV consumer.
  Whole-block non-NEWMV leaves skip the neighbour scan entirely, sub-8x8 count
  pass calls the existing sub-block counter only when any BMI mode is NEWMV,
  and write pass still supplies the same libvpx reference MV when a NEWMV bit
  is emitted. `go test ./... -count=1` stayed green. The lazy path measured
  byte/topology-equivalent 120-frame cpu8 spots at 12.18 / 12.12 /
  12.20 ms/frame versus eager-control spots at 12.24 / 12.24 / 12.21
  ms/frame. After PGO refresh/check, the guarded spot stayed byte/topology-
  equivalent at 12.12 ms/frame with count phase at 10.28 ms/frame and tile
  write at 1.20 ms/frame. This is a narrow mode-write cleanup, not a substitute
  for A3 pure pack or A5 pick-buffer end-state work.
  A lower-level ARM64 SAD dispatch safe point now keeps FEAT_DotProd for
  32-wide single-reference SAD but uses the base NEON `sad64xNNEON` path for
  64-wide blocks; focused private-kernel benches showed dot-product winning
  32-wide shapes but losing 64-wide shapes on Apple M4 Max, and public
  `BenchmarkVP9Sad64x64` moved from the prior ~82-83 ns/op band to
  ~48.6-49.1 ns/op at 0 allocs. Repeated pre-PGO 120-frame cpu8 spots stayed
  byte/topology-equivalent at 12.20 / 12.26 / 12.24 ms/frame, and the
  post-PGO guarded spot stayed byte/topology-equivalent at 12.07 ms/frame with
  count phase at 10.23 ms/frame.
  A lower-level intra-predictor safe point now routes the VP9 H_PRED
  4x4/8x8/16x16/32x32 wrappers through size-specialized row fills instead of
  the dynamic helper loop, while keeping the original helper for non-specialized
  call shapes. Focused `BenchmarkVP9IntraDirectionalPredictors` samples stayed
  at 0 allocs and moved H16 from ~89-91 ns/op to ~35-36 ns/op and H32 from
  ~308-318 ns/op to ~133-136 ns/op. `TestVP9IntraDirectionalPredictors`,
  `TestDSPMatchesLibvpx`, and same-run 120-frame cpu8 A/B stayed byte/topology-
  equivalent; specialized spots were 11.79 / 11.89 / 11.91 ms/frame versus
  restored-control spots at 11.84 / 11.95 / 11.97 ms/frame. After
  `make pgo-refresh` + `make pgo-check`, the guarded 120-frame cpu8 spot stayed
  byte/topology-equivalent at 12.03 ms/frame with count phase at 10.16 ms/frame
  and tile write at 1.24 ms/frame.
  A sibling TM_PRED safe point now scans the top predictor row once and skips
  per-pixel clamp branches for rows whose true-motion range is provably inside
  `[0,255]`, falling back to the original clamp loop for clipped rows. Focused
  `BenchmarkVP9IntraDirectionalPredictors` samples stayed 0 allocs and moved
  TM16 from the old ~158-224 ns/op band to ~99-101 ns/op and TM32 from
  ~573-848 ns/op to ~330-335 ns/op; the explicit clip-heavy fallback benchmark
  stayed correct and 0 allocs while carrying the expected small worst-case tax.
  Interleaved tagged-PGO 120-frame cpu8 A/B stayed byte/topology-equivalent:
  fast-path spots were 11.86 / 11.94 / 11.83 ms/frame versus old-loop control
  spots at 11.97 / 11.88 / 11.94 ms/frame. After `make pgo-refresh` +
  `make pgo-check`, the guarded spot stayed byte/topology-equivalent at
  12.00 ms/frame versus 5.65 ms/frame libvpx, with count phase at 10.13 ms/frame
  and tile write at 1.24 ms/frame.
  The broader tmp[0..3] PRED_BUFFER discipline, dst-as-4th-buffer handoff, and
  intra-winner pred carry remain open.
- A narrow post-A5 cleanup routes source-SAD, variance-partition chroma/CBR SAD,
  and compact motion-candidate SAD through offset-based SAD calls once callers
  have already validated the windows. A follow-up source-SAD edge safe point
  keeps full 64x64 SBs on the existing SIMD path but stops the clamped-edge
  fallback from re-walking repeated bottom/right pixels. Focused bottom-edge
  samples moved from ~3.94-4.25 us to ~0.91-0.92 us at 0 allocs; 120-frame
  cpu8 4T no-denoise phase spots stayed byte/topology-identical at
  4.06 / 4.12 / 4.18 ms/frame with count phase at 374-385 ms total, and
  480-frame normal spots stayed byte-identical at 4.017 / 4.031 / 4.036
  ms/frame versus the prior 4.062 ms/frame sample. The follow-up profile no
  longer sampled `avgSourceSAD64`; count this only as a narrow edge cleanup,
  not as closure of A5. A later `chroma_check` cleanup kept the full UV SAD
  but writes the temporary chroma predictor into existing block scratch instead
  of live recon planes before the SAD. This is distinct from the earlier
  rejected UV-scratch picker probe: focused parity/recon-mutation tests stayed
  green, 120-frame cpu8 4T no-denoise phase spots stayed byte/topology-
  identical at 4.086 / 4.029 / 4.130 ms/frame with count phase at 370-381 ms
  total, and 480-frame normal spots stayed byte-identical at 4.027 / 4.027 /
  4.031 ms/frame. After PGO refresh/check, a guarded phase spot measured
  4.014 ms/frame with count phase at 365.9 ms total, and a normal 480-frame
  spot measured 3.991 ms/frame, both with unchanged bytes/topology. Treat it
  as a small live-recon side-effect removal and profile cleanup, not as closure
  of A5. Measured rejects from the same pass:
  clipped scalar chroma-SAD thresholding, token-pack index cursor walking, and a
  residual source-plane hoist all regressed phase spots and should not be retried
  without a new profile or compiler change. Follow-up rejects: token band-table
  hoisting, entropy-context offset helpers, MV-pred no-limit SAD hoisting,
  intra-fallback `BlockYrd` source-window hoisting, precomputed `BlockYrd` FP
  params, UV scratch-only prediction, direct coefficient-window `WriteCoefSb`
  call shapes, zero-MV luma copy+variance, and qcoeff-value caching inside
  `stageCoefBlockQCoeff` all failed the 120-frame phase spot despite focused
  parity or microbench wins. A cached subpel-variance function pointer also
  failed the hot-path rule directly: the focused scorer benchmark regressed to
  1 alloc/op and slower ns/op; hoisting subpel MV-cost `errorPerBit` out of
  the per-MV closure stayed byte/topology-safe but did not improve the 120-frame
  phase spot. Hoisting the luma AC/DC skipTxfm predicate out of the commit-loop
  tx walk and bypassing `BlockDiffVarianceSSEClampedSource` with a caller-side
  visible-window variance fast path were also byte/topology-safe but failed to
  improve the phase spot. Precomputing per-plane block sizes for coefficient
  accessor closures likewise kept topology stable but regressed the 120-frame
  phase spot. A local three-buffer compact predictor ownership swap for
  nonrd candidate new-best capture also kept bytes/topology stable but regressed
  repeated loaded phase spots, so leave that exact shape closed while the
  broader tmp[0..3] PRED_BUFFER end-state remains open. Hoisting
  `WriteCoefSb`/context-stamp `maxEob` and `step*step` to per-plane scope was
  also byte/topology-stable but only neutral in the connected phase spot and
  did not improve focused coefficient benches; unrolling
  `stampCoefContextBytes` likewise stayed neutral-to-worse in focused
  pack+commit/stage+pack benches and was reverted. A cached subpel reference-view
  thread through nonrd NEWMV and the subpel scorer kept the focused scorer at
  ~173 ns versus the helper at ~209 ns, but guarded 120-frame phase spots were
  12.60 / 12.46 / 12.58 ms/frame and the lean version was 12.53 / 12.61 /
  12.65 ms/frame, so leave that plumbing closed unless a fresh profile shows
  `vp9SubpelReferencePlane` or scorer setup dominance. A staged-token
  no-discard writer specialization removed generic `Writer.Write` from one
  profile and kept pack cum around 80-90 ms, but post-PGO 120-frame spots
  regressed to ~12.61 ms/frame; the patch was reverted. A prechecked
  `SubtractBlockNonZero` route from `gatherVP9TxResidual` kept focused
  subtract/residual tests green, but connected 120-frame spots stayed in the
  loaded/regressed 13.57-13.71 ms/frame band, so the patch was reverted. An
  ARM64 32x16 subpel-variance scratch specialization moved the focused
  `BenchmarkVP9SubPixelVariance32x16` shape from ~110-111 ns/op to
  ~74.7-75.3 ns/op at 0 allocs, but connected 120-frame spots tied the generic
  wrapper: specialized 12.20 / 12.23 / 12.25 / 12.29 ms/frame versus generic
  12.21 / 12.26 / 12.27 ms/frame, all byte/topology-equivalent. The production
  specialization was reverted; keep only the rectangular-shape benchmark probes
  unless a fresh profile makes 32x16 subpel dominant. Follow-up current-frontier
  probes on 2026-07-03 also closed four small shapes: offset-walking generic
  `CopyPlane` regressed focused 16x16/32x32 stride-640 copies versus the row
  multiply loop; a raw-field luma scratch predictor helper improved
  `BenchmarkVP9InterPredictionVarianceSSE/scratch` by a few ns/op but tied the
  120-frame phase A/B (field 12.16 / 12.06 / 12.16 ms/frame, control 12.12 /
  12.22 / 12.03); branchless boolean-writer arithmetic regressed
  `BenchmarkWriterWrite`/`BenchmarkWriterWriteMixedProb`; and removing the
  dead-looking `BlockYrd` inner width guard regressed focused `BlockYrd`
  samples. A `FindInterMvRefsFields` probe added focused coverage for the
  current pprof row and measured the full `NEARMV` walk at ~9.1-9.4 ns/op and
  early-break `NEARESTMV` at ~6.3-6.4 ns/op, so the row is call-count noise and
  not a useful leaf target. A coefficient-SB args-reuse probe moved
  plane-constant `WriteCoefBlockArgs` fields outside the tx loop but did not
  improve focused staging/pack benches and regressed dense direct-writer samples
  (~9.4-9.9 µs versus the prior ~9.0-9.2 µs), so it was reverted. An
  intra-winner luma-predictor carry probe captured the nonrd intra fallback's
  winning predictor and tried to skip the commit-time luma intra rebuild, but
  even the narrowed single-tx guard broke ROI/active-map encode tests with
  `encoder: VP9 token buffer full`; the picker surface is predictor-only and
  not a safe substitute for the sequential predict + inverse-add commit surface.
  Focused DC/V/H/TM intra-predictor scans after the H/TM safe points showed DC
  and V are already too cheap to chase (`dc32` about 55-56 ns/op and `v32`
  about 19-20 ns/op, with smaller sizes in single-digit or low-double-digit
  ns/op). A guarded `LeftReady` probe that skipped the generic builder's
  duplicate left-edge self-copy/extension when tx wrappers had already filled
  `intraScratch.Left` stayed byte/topology-equivalent, but connected 120-frame
  spots overlapped or favored control (candidate 12.10 / 11.99 / 12.04 ms/frame
  versus disabled-control 12.05 / 11.96 / 11.88), so it was reverted.
  A visible-reference subpel scorer bypass tried to score in-frame MVs directly
  from the unbordered reference plane and fall back to the padded scorer at
  edges; focused scorer samples stayed 0 allocs and around ~152-156 ns/op, but
  connected 120-frame spots tied or lost to the disabled-control path
  (visible 11.97 / 12.04 / 11.91 ms/frame versus disabled-control 11.97 /
  11.97 / 11.79), so the bypass was removed and only the existing padded-
  reference scorer path remains. Inlining the non-zero checks inside
  `GetEntropyContextFull` moved the focused helper for tx16 from ~2.02-2.06 ns
  to ~1.85-1.87 ns and tx32 from ~2.56-2.60 ns to ~2.51-2.54 ns, but connected
  spots were neutral-to-worse at 11.88 / 11.83 / 11.86 ms/frame versus the
  current 11.81 / 11.84 band, so that probe was reverted too.
  A follow-up thresholded `chroma_check` UV SAD probe removed the sampled
  chroma-SAD leaf but failed the connected wall-clock gate: patched
  phase-timed spots were 12.13 / 12.03 ms/frame versus the restored
  11.84 ms/frame band, so the full offset-SAD path remains. Three small token
  and coefficient helper probes were also reverted after byte/topology-safe but
  neutral-to-worse spots: an interior fast path in `planeMaxBlocks4x4`
  measured 11.87 / 11.91 ms/frame, a `WriteCoefSb` caller-token-cache outline
  measured 11.90 ms/frame, and a per-transform `stageCoefBlockQCoeff`
  token-cache outline measured 11.80 / 11.81 ms/frame against a
  production-shaped 11.74 ms/frame control. A non-ML intra-fallback scratch
  scorer probe reused the existing scratch/live helper instead of overwriting
  the recon rect; focused helper parity stayed green, but the connected
  120-frame phase spot regressed to 12.32 ms/frame versus the current
  11.81 ms/frame band, so the picker wiring was reverted and only the helper
  equivalence test remains.
  Treat those as closed unless a fresh profile shows a different dominant cost.
- A2 is partial: threaded count-token collection now gives each tile worker a
  tile-local token-list arena (`EnsureForTile`) while preserving global
  tile-row/tile-col lookup semantics for replay. Coefficient token-body replay
  now keeps the hot probability row as a fixed `[UnconstrainedNodes]uint8`
  pointer, and staged/direct EOB/ZERO/PIVOT branch-count recording uses
  constant-index helpers. The normal `WriteCoefSb` token path now stages into
  transform-sized TOKENEXTRA windows before falling back to checked tiny-buffer
  staging, combined pack+context replay consumes full blocks through local
  token windows instead of absolute cursor indexing, and staged/direct
  coefficient walkers share the masked token-cache context helper after
  full-window neighbor preflight. The full-window qcoeff path now trusts the
  prechecked scan/qcoeff windows for EOB discovery and qcoeff token loads, and
  optional branch-stats selection is narrowed to the active tx/plane/ref rows
  before token walking. Replay/context-only coefficient-context stamping now
  uses offset-based fixed-width stores instead of building above/left slices
  solely for the stamp step. Exact-width coefficient entropy-context callers
  now use `GetEntropyContextFull`, a fixed 1/2/4/8-byte helper for windows the
  tx walkers have already sliced to `1<<txSize`; the generic length-clipped
  helper remains for defensive callers. Focused parity covers all tx sizes,
  120-frame cpu8 spots stayed byte/topology-equivalent at 11.99 / 11.93 /
  12.00 ms/frame versus generic-control spots at 12.12 / 11.92 / 12.00, and
  the old sampled `GetEntropyContext` row disappeared/inlined in the follow-up
  profile. After the follow-up `make pgo-refresh` + `make pgo-check`
  (fingerprint `c7a1ecf7caf1edbcaf4e4f1ce2d3ad7925dabf06`), the guarded
  120-frame cpu8 spot stayed byte/topology-equivalent at 11.81 ms/frame versus
  5.58 ms/frame libvpx, with count phase at 9.96 ms/frame and tile write at
  1.22 ms/frame.
  The remaining dynamic checks are deeper
  scan/probability specialization work, not another SB-capacity wrapper. Full
  all-class syntax sidecar staging is still open.
- A6 is partial: realtime inter FP commit now bypasses the trellis-capable
  wrapper and writes qcoeff/dqcoeff output directly into the committed buffers
  before inverse-add. The residual loop, tx-candidate scorer, and commit-time
  context stamper now validate tx/dequant/scan/table invariants once per plane
  and call a prechecked DCT/FP helper per tx block. The realtime FP commit loop
  now consumes libvpx's luma-only `SKIP_TXFM_AC_DC` gate for segment-0
  non-lossless blocks; `SKIP_TXFM_AC_ONLY` stays open/non-FP-only per the
  `vp9_encodemb.c` source check. `BlockYrd` now stores per-tx EOB scratch as
  `int16` after the realtime TX_16X16 clamp, reducing stack-clear work; focused
  samples moved from ~540-549 ns/op to ~517-522 ns/op, repeated post-PGO
  120-frame spots stayed byte/topology-equivalent at 12.44, 12.58, and
  12.46 ms/frame, and the EOB-buffer declaration line was absent from the
  profiled sample. A follow-up now right-sizes that EOB scratch to 16/64/256
  slots by tx-unit count; focused samples moved from ~514-518 ns/op to
  ~506-513 ns/op, still 0 allocs, and post-PGO 120-frame spots stayed
  byte/topology-equivalent at 12.59, 12.63, and 12.42 ms/frame. Broader
  gather/stage removal and subpel direct-on-padded-ref work are still open. A
  narrower attempt to derive `eob_cost` from `txIdx` instead of incrementing it
  in the loop was neutral-to-worse in focused `BenchmarkVP9BlockYrd` samples
  (~515-526 ns/op after a ~511-523 ns/op baseline) and was reverted.
  A narrow subpel-variance scratch safe point now routes the ARM64 32x32
  wrapper through size-specific 32x32/32x33 stack buffers instead of the generic
  32-wide 32x64/32x65 scratch. Focused `BenchmarkVP9SubPixelVariance32x32...`
  samples stayed at 0 allocs and moved two-axis 32x32 from ~164-167 ns/op to
  ~140-142 ns/op, half-pel from ~140-144 ns/op to ~115-117 ns/op, and one-axis
  from ~109-111 ns/op to ~94-96 ns/op. Connected 120-frame cpu8 spots stayed
  byte/topology-equivalent at 11.83 / 12.08 / 11.90 ms/frame pre-PGO; post-PGO
  spots were noisy at 12.18 / 12.39 / 12.37 ms/frame, but an immediate wrapper
  A/B with the generic 32x32 path measured 12.49 ms/frame under the same noisy
  conditions. Keep the scratch narrowing, but do not treat it as closing the
  broader subpel direct-on-padded-ref work.
- CORRECTION to phase-2: libvpx `estimate_block_intra` DOES call block_yrd —
  the intra-fallback row is mostly legitimate work, not waste.

Steps (each ships green; gate = 120f byte-identity + packet-0
frontier + SVC/RTP + zero-alloc + conformance decode + pre-merge sequence):
A1 kill discard coder (PARTIAL 2026-07-02: partition, keyframe/fallback, and
inter-leaf count-pass syntax emits skipped; keyframe/intra replay uses
combined pack+context commits, now extended to all inter-source token replays;
remaining work still −0.2..0.4);
A2 stage tokens for ALL leaf classes incl. keyframe/forced-intra/segment-skip,
per-tile arenas (PARTIAL 2026-07-02: threaded token-list backing is tile-local
per worker, fixed probability-row token body and constant EOB/ZERO/PIVOT
branch counters plus transform-sized TOKENEXTRA staging/replay windows landed;
staged/direct coefficient walkers now share the masked token-cache context
helper after full-window neighbor preflight, and the qcoeff full-window path
now uses trusted scan/qcoeff windows plus tx/plane/ref-narrowed branch-stats
rows; replay/context-only context stamping uses offset-based fixed-width stores;
all-class syntax sidecar staging remains −0.1..0.2);
A3 pure pack
walk reading miGrid + compact per-leaf syntax sidecar + token stream — deletes
partition dispatcher, canReplay/applyVP9CountPass, write-pass residue
(PARTIAL 2026-07-11: normal inter partition-node replay removes the write-side
inter partition picker under count-token replay, and a one-byte UV-mode
sidecar lets the normal >=8x8 inter-source pack path consume committed miGrid
plus tokens without leaf replay/application or write-side residue work, and a
parallel partition-node stream removes the normal inter write-side partition
dispatcher/cache walk; keyframe/sub-8x8/fallback pure packing, count-side
partition picking, and old cache deletion remain; remaining −0.45..0.65); A4 delete replay
infrastructure (PARTIAL 2026-07-11: removed the redundant picker-side leaf
decision store while retaining the finalized fallback entry; remaining
−0.15..0.25); A5 pick-buffer
end-state (PARTIAL 2026-07-02: nonrd `search_filter_ref` uses two compact
luma eval/best buffers and swaps ownership on new-best filters): tmp[0..3]
PRED_BUFFER discipline, dst-as-4th-buffer, direct convolve into eval buffers,
intra-winner pred carry (−1.0..1.4); A6 subpel direct on padded refs + bare
vp9_xform_quant_fp commit with skipTxfm consumption (PARTIAL 2026-07-02:
realtime inter FP commit bypasses the trellis-capable wrapper, writes q/dq
output directly, and hoists tx/dequant/scan/table checks to plane-level for the
normal residual loop plus tx-candidate/context-stamp loops; luma AC/DC
skipTxfm is consumed for segment-0 non-lossless realtime FP blocks, while
AC-only remains explicitly non-FP/open; `BlockYrd` EOB scratch is narrowed to
int16 with post-PGO byte/topology-stable 12.44-12.58 ms/frame spots; broader
gather/stage removal remains −1.0..1.3).
Risks pinned in the blueprint: all-class token staging (SVC leaf visitation
— keep SVC on direct path initially + dual-run byte-compare tag); scratch
convolve byte-inequivalence on recorded filter x size cells (the first
four-shape x four-filter custom-scratch parity gate is landed; extend the
SADScratch parity test to every recorded cell BEFORE broader rerouting);
bare-quantize tx16 crash history (per-tx-size equivalence + long-bench crash
gate first).

## Program B — VP8 encode MB-walk + wall-stall front
Target: walk 4.10 → ~3.2 ms/f; encode ~1.49x → ~1.31-1.36x. Blueprint:
agent report "VP8 encode recon redesign"; key verified facts:

- No unattributed lump: the 4.10 duration phase IS the whole MB walk; deltas
  reconcile with the CPU ledger (mv-pred start +0.24, intra scoring +0.22,
  subpel +0.19, denoiser +0.14, winner predictor +0.11).
- Dual-state tax is minor (~0.1); the real pattern is copy-based picker
  support machinery vs libvpx pointer-aliasing (96B mode structs vs 4B
  lfmv/lf_ref_frame sidecars; border-stripe copies per intra candidate;
  winner rebuilt via decoder path; full-frame denoiser source copy which also
  disables the FDCT winner cache).
- NEW separate front: lf-pick "parity" is CPU-only — 2.39 wall vs 1.33 CPU,
  ~1.0 ms/f memory-stall/refault inside the phase. Wall-clock A/B mandatory.
- B1 is partial: the live encoder now snapshots the previous coded inter frame
  as a compact `{mv, ref, signBias}` sidecar (`InterFrameMVRef`) for
  `vp8_mv_pred` instead of copying the full `InterFrameMacroblockMode` grid.
  The follow-up removed the old private full-mode/test fallback too, leaving
  the compact sidecar as the only previous-frame predictor representation.
  Focused predictor tests pin sidecar reads, stale-intra-MV suppression,
  border sentinels, and sign-bias handling; the 120-frame 720p realtime spot
  stayed byte/topology-equivalent at 1,328,027 bytes, 118 encoded / 2 dropped,
  0 allocs/frame. Sequential wall samples remain effectively neutral on this
  small slice (baseline 7.626 ms/frame, pre-cleanup post spots 7.638/7.653
  ms/frame, refreshed-PGO spots 7.821/7.684 ms/frame, post-cleanup/post-PGO
  7.755 ms/frame), so do not count B1's ms/f win until the remaining VP8
  MB-walk sidecar/layout work lands.
- B2 is partial: the luma-only fast intra predictor-ref builder now aliases
  the contiguous above row directly from the visible or extended frame buffer
  when edge semantics allow it, falling back to scratch for top rows and
  synthetic right-edge fills; left samples remain copied into contiguous
  scratch because the current VP8 intra DSP entrypoints consume contiguous
  left stripes. Focused decoder tests compare luma refs against the canonical
  full Y/U/V builder, pin visible-row and extended-row aliasing, synthetic-edge
  fallback, and zero allocations. The 120-frame 720p realtime cpu8 phase spot
  stayed byte/topology-equivalent at 1,328,027 bytes, 118 encoded / 2 dropped,
  measuring 7.64 ms/frame after PGO refresh with inter-recon at
  3.88 ms/frame.
- B6 has a tiny safe point: the fast inter picker now defers copying the full
  `bestIntraMode` value into the returned decision until the winning mode is
  actually intra, while preserving the old `interMode` copy and the default
  DCPred intra placeholder for inter winners. This trims an unnecessary
  96-byte mode-value copy in the common inter-selected path without changing
  picker scoring or denoiser fallback behavior. Focused fast-inter/denoiser
  tests stayed green. Interleaved 120-frame cpu8 overload spots were effectively
  tied but directionally positive and byte/topology-identical: control
  7.65 ms/frame, patched 7.63 and 7.60 ms/frame, all 1.27 MiB, 118 encoded /
  2 dropped. The longer patched 480-frame spot stayed exact against libvpx for
  this fixture at 7.92 vs 5.34 ms/frame, 4.83 MiB, 478 encoded / 2 dropped.
  Count this only as B6 glue, not as closure of B3/B4/B5 or the lf-pick wall
  front.
- A small B5/B6 denoiser-copy safe point skips redundant filtered-buffer copies
  when the normal encode path passes the same coded source view for both the
  denoiser source and signal buffer. Running-average copies, spatial-filter
  copies, and separate filtered-buffer callers are preserved and pinned by a
  focused no-filter copy test. Focused denoiser/VP8 overload tests stayed green;
  the 120-frame cpu8 phase spot stayed byte/topology-identical at
  7.64 ms/frame, and 480-frame spots measured 7.863 / 7.892 ms/frame versus an
  immediate restored-control 7.904 ms/frame, all 5,066,778 bytes, 478 encoded /
  2 dropped. After PGO refresh/check, the 480-frame spot measured
  7.846 ms/frame with unchanged bytes/topology. Count this as copy glue, not
  closure of per-MB denoiser staging.
- Current-frontier VP8 probes closed on 2026-07-03: inlining/hoisting the
  full-luma loopfilter trial body tied focused 1024x1024 trial samples and was
  reverted; VP8 subpel/DSP one-axis and split-shape benches were already
  tight/0 allocs and did not identify a connected B3 edit; libvpx's ARM VP8
  loopfilter wrapper also calls the vertical Y edges individually, so fusing
  those edges is a new assembly program, not a quick source-shaped safe point;
  carrying a denoiser "changed" return into the FDCT cache was disconnected from
  realtime cpu8 because the fast picker reports `rd_cache=0` / `dct_hits=0`;
  removing the denoiser spatial-filter closure tied the interleaved phase A/B
  and was reverted; collapsing `interModeMVSlots` to a combined near/best/count
  accessor looked source-shaped but regressed the connected 120f/480f spots
  (7.73 / 7.98 ms/frame) and was reverted; and public inter-predictor state
  setup was only about 10 ms cum over the 480-frame profile, with predictor
  kernels doing the real work. A 64-wide ARM64 dotprod luma-SSE scorer for
  loop-filter trials won the synthetic 64x80 microbench (~64-65 ns versus
  ~140-144 ns for four 16-wide calls) but lost the connected scorer phase:
  patched 120-frame spots reported ~41-42 us/trial SSE versus restored-control
  ~33-34 us/trial, so the call site and assembly probe were reverted. Shrinking
  the fast-inter variance cache from 16 to 8 entries also stayed neutral in the
  connected phase A/B (8-entry spots 7.74 / 7.72 / 7.62 ms/frame versus
  immediate 16-entry control 7.69 / 7.74 / 7.58), so leave the existing cache
  size alone unless a new profile shows stack-clear dominance. A no-cache
  accepted-coefficient probe that merged the 16-block Y and 8-block UV FDCT
  calls into one 24-block `ForwardDCT4x4Batch` dispatch also stayed neutral to
  worse in the connected 120-frame phase spots (7.83 / 7.65 / 7.58 ms/frame),
  with no stable inter-recon drop, and was reverted. Routing the B_PRED
  luma-only RD pickers through `BuildIntraPredictorRefsLuma` won the focused
  refs micro shape (~15-17 ns/op luma vs noisy ~42-68 ns/op full, 0 allocs)
  but did not survive the connected VP8 overload gate: candidate 7.84 ms/frame
  versus restored full-builder controls at 7.75 / 7.61 ms/frame with identical
  bytes/topology, so the production call sites stay on the full builder. A
  prepared-token one-shot grid writer for single-token-partition inter packets
  kept focused token benches and packet/drop parity green, but connected
  480-frame realtime cpu8 spots were neutral-to-worse (7.970 / 7.932 / 7.961
  ms/frame versus the current ~7.94 ms/frame band) with identical
  bytes/topology, so the row-sliced writer remains. Hoisting the hot/denoise
  fast-picker mode/ref order globals into local loop arrays matched the existing
  oracle/cold picker shape and kept focused parity green, but candidate
  480-frame spots at 8.052 / 7.963 ms/frame lost to the immediate restored
  control at 7.904 ms/frame, so keep the existing hot-loop shape. Do not retry
  those exact shapes without a fresh profile.

Steps (gate = TestVP8RealtimeOverloadDropParity SHA + full VP8 parity lane):
B1 compact last-frame {mv,ref,signBias} sidecars (PARTIAL 2026-07-03:
previous-frame improved-MV sidecar landed and legacy full-mode fallback
removed; remaining MB-walk layout work is still open before claiming the
−0.15..0.20); B2 libvpx-shaped intra scoring into contiguous scratch from
direct border pointers (PARTIAL 2026-07-03: direct-above luma aliasing landed;
direct-left / fuller picker scratch layout remains −0.15..0.25); B3 subpel eval fusion after kernel-parity microbench
adjudication (−0.10..0.20); B4 direct winner predictor with encoder kernels /
storePredictor reuse (−0.10..0.15); B5 per-MB denoiser staging (thismb shape)
+ re-enabled FDCT winner cache (−0.10..0.15, but the 2026-07-03 FDCT-cache
return probe is closed for realtime cpu8 until an RD-cache-producing fast path
exists; PARTIAL 2026-07-03: aliased source/signal no-filter copies are skipped
on the normal denoise path); B6 glue (PARTIAL 2026-07-03: final-mode copy
elision landed; remaining glue still −0.05). Then B7: the
lf-pick wall-stall investigation (buffer reuse/scavenger; separate round).
Risks: threshold cascade (any scoring rounding change → global mode
avalanche — the SHA pin is the tripwire); border semantics at frame edges;
sidecar capture timing vs mode fixups; denoiser running-avg order; threaded
rows share helpers (keep threads=2/4 determinism pins green).

## Program C — MT structural (row-mt, denoiser-MT, threaded decode)
Blueprint: agent report "MT scaling structural plan"; measured on vpxenc:
720p is HARD-CAPPED at 4 tile columns, so row-mt is the only route past
~2.4 ms/f: 8T tiles-only 2.39 vs 8T row-mt 1.88 (+27%); denoiser build:
ns1 serial 15.89 → 4T tiles 4.78 → 8T row-mt 3.31 (4.8x), byte-identical
t2=t4=t8.

C1 **VP9 encoder row-mt** (core): per-tile SB-row job queue + tile stealing
(vp9_multi_thread.c), VP9RowMTSync nsync=1. Determinism is by construction:
VP9 needs NO top-right sync (no mv-ref offset with col ≥ block width; intra
have_right never crosses; above-context column-disjoint) — sync rule is
"SB(r−1,c) complete". Port the per-row thresh-freq-fact tables and libvpx's
`adaptive_rd_thresh` disable gated on threadHint>1 (this is exactly how
libvpx makes row-mt bit-exact; measured t2..t8 byte-identical, t1 differs
only via that gate). Steps: thresh tables/gate → row dispatch within one
tile → multi-tile queue+stealing → count-pass extension. Gates: threads
{1,2,4,8} byte-equal on the option grid + vpxenc --row-mt=1 oracle pins.
C2 **MT-with-denoiser** (default-path multiplier): PARTIAL 2026-07-03. The
VP9 `NoiseSensitivity>0 → tile workers disabled` gate is removed for the
existing tile-MT path; denoiser writes are block/tile-column disjoint in the
source, intra running average, and motion-compensated scratch image, while the
count pass keeps the existing save/restore transaction. The bench oracle now
mirrors this layout instead of forcing libvpx serial for denoise. Gates:
focused thread/bench layout tests, deterministic threaded-denoise packet test,
and a 120-frame 720p realtime cpu8 denoise spot at 4.24 ms/frame govpx vs
2.10 ms/frame libvpx with `--threads=4 --tile-columns=2 --noise-sensitivity=4`,
count=3.27 ms and tile=0.47 ms. Remaining C2 work is row-mt denoiser scaling
after C1, plus any oracle-denoise algorithmic parity fixes that surface under
the broader option grid.

C3 **Threaded decode**: COMPLETE 2026-07-10. The decoder loop-filter path now
reuses the encoder VP9LfSync port for row-based LF-MT, replacing the old
3-plane ≤3-way split; the official corpus threading helper covers the
{1,2,4,8} matrix plus DecoderLoopFilterOpt and DecoderRowMT. The
source-shaped row-mt decode queue/recon-map scaffolding is also ported and
tested against libvpx's JobQueueRowMt / RowMTWorkerData layout; one-tile
DecoderRowMT frames now enter that scaffold and replay a split
parse/reconstruct walk through per-SB partition/EOB/dqcoeff slabs. The row
queue now seeds PARSE jobs, advances the shared tile reader row-by-row through
workers, enqueues matching RECON jobs, and drains the fixed-capacity queue
with the main goroutine participating alongside helpers; the local-header and
steady-state decode paths remain 0 allocs/op. Completed RECON rows now enqueue
LPF jobs with libvpx's one-SB-row lag, sharing the existing VP9LfSync wavefront
and building masks per released row; the post-frame filter is skipped only
after the final queued LPF row succeeds. The 128-vector conformance gate across
threads {1,2,4,8} passed on 2026-07-10 across 7 official IVF vectors, 101
profile-0 WebM vectors, the unsupported-profile corpus, and invalid-stream
rejection; the one-tile PARSE/RECON/LPF queue is complete. Multi-tile row-MT
decode now uses the same shared queue with tile-local readers, left contexts,
and frame counts, while reconstruction jobs remain stealable across workers.
The final tile reconstruction for each SB row releases exactly one LPF job,
and that release happens before publishing the row's final recon-map cell to
preserve libvpx's FIFO dependency order. Focused four-tile coverage exercises
16 PARSE, 16 RECON, and 4 LPF jobs with serial-identical output; multi-tile
steady-state decode remains 0 allocs/op, and the full official threading /
conformance matrix passes.
C4 VP8 encode: already at MT parity — nothing to do.

## Sequencing for implementation agents

- A and B are disjoint codecs — run in parallel (separate agents, file
  boundaries root-vp9 vs vp8).
- C1 touches the same walk/tile files as A1-A4: sequence C1 AFTER A4 lands
  (or coordinate via strict file locks); the C2 tile-MT denoiser gate is
  landed, but C2 row-mt denoiser scaling remains after C1; C3 (decoder) is
  disjoint — can run parallel with A/B.
- The oracle-config audit + rebuild is a prerequisite task; small, do first.
- Every agent: pre-merge gate sequence, byte-parity lanes per commit,
  wall-clock adjudication before targeting any runtime.*/stall row,
  interleaved A/B medians, no probes in hot paths, PGO fingerprint refresh
  with hot-path edits, push branch only — coordinator merges.

## End-state estimate (720p)
A: VP9 1T ~9.4-10.2 ms/f (~1.6-1.7x). B: VP8 ~1.31-1.36x + wall-stall upside.
C: VP9 4T→8T ~1.9 ms/f class with row-mt; denoise default path goes from
serial to ~4.8x scaling; decode 1-tile +26%. Combined with the honest Go
floor (~1.4-1.6x per-thread), this puts every front at or near its
achievable limit; beyond that lies token-loop asm mega-kernels (out of
scope).
