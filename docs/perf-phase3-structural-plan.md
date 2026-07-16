# Perf phase-3: structural program

Status: implementation started. The oracle-denoise prerequisite, narrow
A1/A2 token-walk safe points, the first A5 pick-buffer safe point, the
A5 non-ML plus ML four-buffer ownership safe points, the
A6 compact coefficient staging plus padded-reference edge-prediction and
bare-quantize / AC-DC skipTxfm
commit safe points, the A3 keyframe pure-pack safe point, and a denoiser
count-copy safe point plus a nonrd picker invariant-hoist safe point, a
small offset-SAD cleanup, a `BlockYrd` EOB-scratch narrowing safe point,
a VP9 MV-pred/fullpel callback shape cleanup, a VP9 ARM64 wide-SAD
dispatch safe point, a VP9 ARM64 32x32 subpel-variance scratch narrowing safe
point, a VP9 lazy NEWMV best-ref MV write/count safe point, a VP9 intra-H
predictor fixed-size fill safe point, a VP9 TM-predictor no-clip fast path,
an ARM64 VP9 16x16 fused subpel-variance safe point,
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
point, plus the A4 normal packed-leaf fallback-cache store deletion, first
production C1 count-pass row-dispatch safe point, and transactional denoiser
row-dispatch extension plus the VP9 row-helper blocking-idle and atomic
wavefront safe points, plus a fused tile-helper prepare/write launch, landed on
2026-07-02/03/10/11; the larger A/B and remaining encoder-MT structural
programs remain pending. This is the execution brief
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

Research note, 2026-07-16 (denoiser intra-avg prep copy — candidate with a
parity caveat): the fresh 240-frame 1T profile attributes ~0.2 ms/f of
`runtime.memmove` to `prepareVP9DenoiserSource`'s two frame-entry copies. The
mutable-source copy is parity (libvpx pays the equivalent copy inside
vp9_lookahead_push; govpx's caller image is immutable by API contract). The
intra running-average copy is NOT libvpx's shape: vp9_denoiser.c maintains
running_avg_y[INTRA] per block — `denoise_and_copy` always ends with
vpx_convolve_copy in the filter or copy direction for every visited block —
and full-copies only on keyframe/resize/reset. Deleting govpx's per-frame
prep copy (~0.1 ms/f) requires guaranteeing per-block copy coverage at EVERY
early-return class in `applyVP9DenoiserToInterBlock` (block-size gates,
SSE/motion threshold exits, MC-predict failures, intra/compound decisions),
because with the prep copy govpx's unvisited intra-avg regions hold CURRENT
source where libvpx holds STALE prior-frame content. The pinned denoise lanes
are byte-exact today, which means those region-content differences have not
yet influenced a pinned stream — but they are a latent divergence surface on
unpinned content, and any prep-copy deletion must first reconcile govpx's
visit/write coverage against libvpx's exact per-block copy sites rather than
assume the prep copy is a free redundancy. Do the coverage audit and the
copy deletion as one unit.

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
- Keyframe replay now uses the same pure partition-stream and leaf-pack path:
  it reads committed mode info plus the UV-mode/TOKENEXTRA sidecars, emits the
  live skip-probability row, and commits coefficient contexts without
  re-entering keyframe partition, mode, residue, or decision-cache plumbing.
  The 480-frame 8T row-MT gate stayed exact at 4,981,549 bytes and 468/12
  topology; median tile-write time moved from about 0.422 to 0.418 ms/frame
  while whole-frame timing was neutral. More importantly, the prior good-mode
  control failed every 720p run on the periodic frame-30 keyframe with
  `encoder: VP9 token buffer full`; the pure pack path completed 120/120 with
  four keyframes and stable 1,252,676-byte output. A 31-frame 720p regression
  test pins that production-volume transition. The separate quality-fixtures
  warmup exposed a second boundary: 360p has an odd 45-row MI grid, so the
  committed partition stream reaches legitimate sub-8x8 bottom-edge leaves.
  Pure inter packing does not yet own a byte-safe sub-8x8 mode/MV sidecar;
  lifting its >=8x8 guard removed the frame-65 token-list exhaustion but made
  measured frame 54 undecodable, so that probe was reverted. Count-token
  collection and replay now stay off when either MI dimension is odd, keeping
  those edge shapes on the established full write walk while even-grid 720p
  remains on pure pack. The 66-frame checker regression now encodes and decodes,
  and the full panning/checker quality fixtures complete at 2.11/3.94 ms/frame.
  The later quality target still has inherited ARNR tolerance and cyclic-refresh
  reference-data failures. Normal, pure-Go, trace, conformance, focused race,
  strict byte-parity, and refreshed-PGO gates pass for the keyframe safe point.
  The odd-MI fallback separately passes full normal/pure-Go, focused race,
  strict byte-parity, refreshed-PGO, and the complete fixture quality/decode
  gate before those inherited later failures.
  A later mixed-path probe kept pure packing for the interior and routed only
  sub-8x8 leaves through the full writer. A 66-frame single-thread checker
  passed, but the complete panning fixture became undecodable at frame 107;
  retaining sub-8 decisions in the shared fallback cache also made the
  threaded checker undecodable at frame 25. Both variants were reverted.
  The remaining sub-8 work needs a complete byte-safe mode/MV/reconstruction
  sidecar, not a local full-writer splice.
- Sub-8x8 pure pack landed 2026-07-16. The "sidecar" turned out to already
  exist: the committed NeighborMi carries the finalized per-4x4 Bmi mode/MV
  quartet (exactly what libvpx pack_inter_mode_mvs reads from mi->bmi), and
  WriteInterBlock's sub-8x8 arm consumes it beside the existing UV-mode /
  partition / TOKENEXTRA streams. What actually made the two earlier attempts
  undecodable was a chain of four latent bugs the diagnosis isolated with a
  frame-54 repro, per-leaf count/write field traces, a per-leaf writer/reader
  bit-position diff, and an in-process packed-vs-fallback byte A/B:
  1. Producer-token staging registered sub-8x8 leaves at the folded
     reconBsize == BLOCK_8X8 with the folded tx; the post-encode clamp
     (vp9_encodeframe.c:6117-6118) re-clamps sub-8x8 to TX_4X4, so the
     consume check poisoned the whole frame's collection mid-count.
  2. That poison demoted the frame to the fallback write AFTER canOmit had
     already skipped finalized leaf stores, so the write pass re-picked
     against post-count picker state and silently desynchronized mode bits
     from the frame's committed recon (the frame-54/107/25 class; live on
     main for threaded 360p-class and 368p parity configs).
  3. The sub-8x8 residue ran at the folded block's picked tx (TX_8X8),
     laying out the compact qcoeff/EOB sidecar at the wrong transform size
     for the TX_4X4 write; the token walk truncated and the write-pass
     error was silently swallowed (`err != nil && collectTokens`).
  4. Intra sub-8x8 fallback leaves committed mode != bmi[3] (picked V_PRED
     with unfilled DC bmi), so the wire's uv_mode probability row
     (UvModeProb[mode]) disagreed with what a spec decoder derives from
     bmi[3] — undecodable regardless of staging.
  The landed slice fixes all four: sub-8x8 residue tx is pinned to TX_4X4
  before the final residue (libvpx vp9_pick_inter_mode_sub8x8 semantics),
  producer tx-stability reads the true leaf size (mi.SbType), intra sub-8x8
  leaves stamp the picked mode into every Bmi slot (mode == bmi[3] invariant)
  with libvpx sum_intra_stats per-sub-block y_mode[0] counting, write-pass
  coefficient-walk errors surface through a new vp9WriteWalkErr channel, and
  an omitted-store recovery rerun (collection force-disabled, mirroring the
  tx-demotion rerun) makes any future mid-count poison byte-safe instead of
  silently corrupting. With sub-8x8 leaves packable, the odd-MI disable is
  lifted: 360p/45-row grids now stage count tokens and pure-pack every leaf
  class. The partition-node arena grows to 2*rc+3*(r+c)+4 to cover partial-SB
  and tiny-frame node counts the old 2*rc bound missed. Gates: a 10-config
  decode-every-frame matrix (odd/even MI x threads 1/2/4 x minimal/parity
  options, 66 frames each) is fully green, including four configurations that
  fail on base main (even-368-t1 minimal hard-errors with token-buffer-full;
  odd-360 t2/t4 minimal and even-368-t4 parity silently emit undecodable
  frames — libvpx vpxdec rejects them too, "failed to decode tile data").
  Permanent regressions pin the odd-MI 66-frame checker (1T + threaded), the
  intra-leaf invariant fixture, and a poison-recovery byte-equality test via
  a test-only token-arena clamp. All byte pins stayed exact (native and
  purego 1T 120f/240f/480f at 1,235,511 / 2,483,072 / 4,983,461; t2/t4 at
  1,236,273 / 1,234,903; 8T row-MT denoise 1,235,979; 4T no-denoise
  1,236,037; 0.633 allocs/frame), the full suite passes, focused race is
  clean, and the oracle matrix failing-row set is identical to base
  (49 pass / 1 pre-existing fixed-q-rt-cpu0-constant fail).
- Replay infra (a 120-byte decision in each full-width leaf-cache entry,
  `canReplay` validation, entropy
  snapshots) now sits outside the normal >=8x8 inter leaf pack path, but still
  serves count-side storage, partition/fallback replay, keyframes, and sub-8x8;
  delete it only after those remaining pack classes are pure.
- A4 2026-07-16: with sub-8x8 leaves packable, the DefaultMinPartitionSize
  >= BLOCK_8X8 gate is lifted from canPackVP9PartitionTree, so every
  preserved inter frame — including the cpu1-4 realtime class whose interior
  partition trees reach sub-8x8 — takes the pure partition-stream/leaf-pack
  walk. That leaves the write-side leaf-cache replay
  (canReplayVP9CountPassInterLeaf/IntraLeaf + applyVP9CountPassInterLeaf/
  IntraLeaf and the mode-block lookup) and the entire inter partition
  decision cache (ensure/store/lookup, canReplayVP9InterPartitionDecision,
  the miRows*miCols*BlockSizes entry array, and its threaded worker
  ping-pong/aliasing plumbing) with zero callers; all deleted
  (net -434 LOC). What genuinely stays and why: the finalized leaf-decision
  cache itself (vp9LeafInterDecisions) remains the decision source for
  unpreserved fallback walks (EncodeNoUpdateEntropy, collection-ineligible
  frames such as SVC/full-RD-tx, denoiser-not-ready rollback frames, and the
  tx-demotion/poison-recovery count reruns) through the
  prepareVP9InterPredictionBlock replay, plus clampVP9LeafDecisionTxSizes
  after tx-mode demotion; the keyframe decision caches remain for
  collection-ineligible keyframes; the deep-RD SEARCH->WRITE caches are a
  separate experimental surface. Gates: full suite green, oracle matrix
  failing-row set identical to base (49/1), all byte pins exact (native and
  purego 1T 120f/240f/480f, t2/t4, 8T row-MT denoise, 4T no-denoise, and the
  360p odd-MI spot), steady-state allocs/frame improved 0.633 -> 0.625,
  focused race clean. Interleaved 1T pairs vs the sub-8x8 safe point: 720p
  won 4/5 with medians 9.651 -> 9.552 ms/frame (about 1.0%); 360p was a
  noise-level 5/7 split against (medians 2.917 -> 2.925 ms/frame, +0.3%),
  so treat the wall effect as neutral-to-slightly-positive; the structural
  value is the deleted cache and its per-node count-walk stores.
- A4 has started deleting that obsolete ownership. The final leaf caller
  already stores the post-residue decision (including final skip, tx, refs,
  and UV mode) before any later lookup, so the fresh picker-side store was an
  immediately overwritten 120-byte copy on every picked leaf. Removing it
  leaves the finalized fallback cache intact. Three alternating 120-frame
  cpu8 1T no-PGO pairs kept exact 1,235,511-byte output; two pairs improved
  by about 0.08-0.09 ms/frame and median count time improved by about
  0.046 ms/frame.
- A4 now also omits that finalized 120-byte cache copy when the count walk is
  guaranteed to feed the normal packed write: coding-state preservation was
  requested, token staging is active, SVC/denoiser/active-map fallbacks are
  absent, and frame-level tx-mode demotion cannot rerun counts. The fallback
  cache remains intact for every excluded class, including
  `EncodeNoUpdateEntropy` and `TX_MODE_SELECT` with frame-parameter updates.
  On the 120-frame 720p realtime cpu8 4T no-denoise spot, finalized stores
  fell from 237,683 to zero while packed replay remained 237,683 hits and zero
  misses. Output stayed exact at 1,236,037 bytes and 108/12 encoded/dropped.
  With the host saturated above load 170 by unrelated fuzzing, three
  alternating process-level pairs reduced retired instructions by about
  0.05%, 0.29%, and 0.08%; wall time is deliberately not claimed. Focused
  threaded/no-update-entropy/denoiser tests, a threaded race slice, the full
  suite, and refreshed-PGO production replay all passed.
- The next A4 cleanup makes finalized leaf-cache lookup write-only at the mode
  block boundary. Count callers could never satisfy either replay predicate,
  but previously still performed both lookups; write callers now share one
  lookup result across inter and intra replay checks. Three no-PGO 120-frame
  cpu8 4T pairs kept exact 1,236,037-byte output, 108/12 topology, and
  237,683/0 packed replay hits/misses while reducing retired instructions by
  about 0.13%, 0.04%, and 0.12%. After the external fuzzers exited, five
  additional wall pairs were mixed from +2.93% to -3.01% with a -0.03%
  median, so wall time is treated as neutral rather than a win. Focused
  replay/threaded tests, the threaded race slice, and the full suite passed.
- Denoiser-active token replay now commits count-side source/intra-average
  state only when every leaf can use packed replay; otherwise it rolls that
  state back before the write walk. A4 still retains finalized leaf-cache
  stores for denoiser frames because that all-leaves commit decision is made
  after the count walk. Full-width denoiser/lookahead image copies use
  contiguous plane copies while padded images keep the row loop.
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
  The normal non-ML `pred_pixel_ready` lanes now use libvpx's full four-buffer
  ownership shape: three compact scratch buffers plus the live reconstruction
  destination as `tmp[3]`. The first candidate predicts directly into dst,
  later candidates and filter trials acquire free compact buffers, new winners
  transfer ownership without copying, and commit copies only when `best_pred`
  is not already dst. If the inter winner still owns dst when intra fallback
  starts, it is moved once into a free compact buffer before the intra predictor
  overwrites the rect. This replaces the hybrid path that copied every new-best
  candidate and carried deferred-capture state whose `InRect` arm was never set.
  The custom-destination parity test now covers an offset exact-span buffer in
  addition to padded compact strides, and a focused pool test pins acquire,
  exhaustion, free, and reuse. The post-change profile no longer attributed a
  `runtime.memmove` sample to the nonrd picker; total sampled memmove fell from
  60 ms to 20 ms on the 120-frame profile. Under the heavily loaded host, two
  order-reversed 480-frame 4T no-denoise whole-process pairs retired about
  0.13-0.15% fewer instructions and used about 1.3-1.8% fewer cycles, with exact
  4,981,549-byte output, 468/12 encoded/drop topology, and the same near-zero
  allocation band. At that safe point the ML partition lane and intra-winner
  predictor carry remained open, so it was a substantial A5 ownership slice,
  not full A5 closure.
  The ML partition lane now uses the same four-buffer ownership model instead
  of copying each candidate through `blockScratch`, `pickPred`, and recon.
  Its `tmp[3]` is the live SB-local `pickPred` rect; filter/mode winners retain
  a compact buffer by ownership, a destination-owned inter winner is captured
  once before intra search overwrites it, and only the final inter winner is
  mirrored into recon. The strided winner-copy test pins the offset geometry.
  Three order-reversed 320x180, 2000-frame cpu8 1T no-denoise pairs kept exact
  4,160,881-byte output and 1997/3 topology. The two stable pairs improved from
  1.537-1.540 ms/frame controls to 1.501 ms/frame candidates (about 2.3-2.5%);
  the paired profile reduced picker cumulative CPU from 1.39 s to 1.32 s and
  total sampled `runtime.memmove` from 100 ms to 20 ms. Intra-winner predictor
  carry remains open; ML ownership unification is closed.
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
  parity or microbench wins. Reusing scene-detection 64x64 SAD samples in the
  later per-SB source-SAD cache was also exact but moved the 120-frame median
  from 10.033 to 10.140 ms/frame and worsened count time, so the apparent
  profile duplication should not be retried without new evidence. A cached
  subpel-variance function pointer also
  failed the hot-path rule directly: the focused scorer benchmark regressed to
  1 alloc/op and slower ns/op. The scorer itself later moved from a value to a
  pointer receiver, removing a roughly 200-byte struct copy per subpel
  candidate without caching a callable. Five focused cached-scorer samples
  moved from a 134.9 ns/op median to 132.7 ns/op at 0 allocs, and five no-PGO
  480-frame 4T no-denoise pairs stayed exact at 4,981,549 bytes and 468/12
  topology while moving median wall time from 3.557 to 3.536 ms/frame (about
  0.6%). A matching ARM64 kernel safe point now fuses the horizontal and
  vertical bilinear stages with 16x16 variance accumulation. Fractional
  candidates no longer write a 17x16 first-pass plane and a 16x16 second-pass
  plane before a third variance walk; horizontal-only and vertical-only
  candidates also accumulate directly. The all-offset/stride differential
  matrix, native, pure-Go, and race DSP suites stayed green. The focused
  two-axis benchmark moved from a 36.2 ns/op median to 25.1 ns/op (about 31%)
  at 0 allocs. Five no-PGO 480-frame 4T no-denoise pairs stayed exact at
  4,981,549 bytes and 468/12 topology; the candidate won all five and moved
  median wall time from 3.529 to 3.512 ms/frame (about 0.5%).
  The full native/pure-Go suites, strict byte parity, PGO refresh/check, and
  pre-commit gate passed; the refreshed-PGO 480-frame spot measured 3.447
  ms/frame with the same bytes/topology. Hoisting subpel MV-cost
  `errorPerBit` out of
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
  A later full-block worker-private carry confirmed the same ownership failure
  on the standard 120-frame no-denoise gate: output moved from 1,236,037 to
  1,235,865 bytes and wall time regressed from 3.281 to 3.355 ms/frame. It was
  reverted. Any future A5 carry must preserve the winner's reconstructed tx
  chain, not copy the model-stage predictor surface.
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
  byte/topology-equivalent at 12.59, 12.63, and 12.42 ms/frame. Candidate luma
  prediction now mirrors libvpx's direct reads from the encoder-owned 160-pixel
  YV12 border when a motion/filter tap window crosses the visible frame edge.
  Interior candidates keep the existing visible-plane fast path; only the
  branch that previously called `vp9ExtendInterPredictSource` consults the
  persistent padded plane, with the exact three-before/four-after 8-tap bounds.
  Padded caches carry the source reference generation as well as dimensions, so
  same-backing reference replacement, retries, ROI/active-map passes, and worker
  aliases rebuild or detach instead of reading stale pixels.
  Four-shape x four-filter parity, including border subpel, stays byte-exact,
  and the border case proves the temporary staging buffer remains untouched.
  Two stable 480-frame 4T no-denoise pairs improved about 0.3-0.5% with exact
  4,981,549-byte output and 468/12 topology; two 1T pairs retired about 0.2%
  fewer instructions, and `vp9ExtendInterPredictSource` disappeared from the
  follow-up profile. The generation-rebuild guard and active-map/ROI zero-alloc
  tests pass. Broader gather/stage removal remains open. A
  compact coefficient-staging safe point replaces the old sparse
  `(4x4_origin * 1024)` q/dq layout with tx-block-major spans sized by the
  actual `maxEob`. Every valid VP9 block-shape x transform-size layout now
  covers a plane's coefficients exactly once through one checked power-of-two
  offset helper, while the 256-cell 4x4-origin EOB map remains independent.
  Across three planes, q+dq storage falls from 3 MiB (1,572,864 `int16` slots)
  to 48 KiB (24,576 slots). Exhaustive overlap/bounds coverage, active-map/ROI
  zero-allocation tests, race, full tests, conformance, strict byte parity,
  trace, and pure-Go gates pass. Two order-reversed no-PGO 480-frame 4T pairs
  stayed exact at 4,981,549 bytes and 468/12 while improving about 0.13-0.17%;
  the paired profile reduced `WriteCoefSb` cumulative CPU from 500 ms to
  340 ms and `PackTokensAndCommitCoefSbContexts` from 390 ms to 370 ms.
  A follow-up removes persistent per-block dqcoeff entirely: transform/
  quantize writes dqcoeff into the encoder's reusable 1024-entry tx scratch,
  inverse-add consumes it immediately, and the later token walk explicitly
  receives nil dqcoeff plus the retained qcoeff/EOB span. The persistent SB
  coefficient store is now qcoeff-only at 24 KiB across three planes. Focused
  staged/direct qcoeff token parity and connected 120-frame plus 2000-frame ML
  byte gates stay exact. Two order-reversed 480-frame 4T pairs improved about
  0.57-0.74% over the compact-layout safe point, with exact 4,981,549-byte
  output and 468/12 topology. A direct compact-sidecar handoff now lets
  `WriteCoefSb` derive each tx span and EOB from the fixed qcoeff/EOB stores
  without four root callback closures or per-tx indirect calls. The production
  pointer entrypoint also avoids copying the leaf argument bundle after token
  collection mutates it in place. Five interleaved post-PGO 480-frame 4T pairs
  kept exact 4,981,549-byte output and 468/12 topology while improving
  0.16-1.59%, with a 0.57% median; the paired profile reduced sampled
  cumulative `writeVP9ModeBlock` time from 120 ms to 40 ms. Full, pure-Go,
  trace, conformance, strict byte-parity, focused changed-path race, refreshed
  PGO, 1T/4T, and 2000-frame ML gates pass. The broad root race run remains red
  on pre-existing frame-parallel token-buffer, decision-cache, and last-source
  sharing, with no report in this sidecar path. Full-leaf producer-time token
  fusion remains open. A producer-adjacent relocation probe kept exact
  4,981,549-byte output but failed the wall gate: the 4T median moved from
  3.977 to 3.995 ms/frame, while stable 8T row-MT candidates were 3.605 and
  3.623 ms/frame versus 3.601-3.606 controls, with one 4.081 ms/frame outlier.
  The probe was reverted; the remaining A6 work must delete the sidecar walk
  by splitting final commit from candidate search, not merely move it. A
  follow-up transaction probe did move final inter residue work outside the
  frozen skip-encode search context, staged one leaf into fixed worker-private
  storage, and published tokens/counts/entropy contexts only after skip and
  transform-size commit. It also rejected post-encode transform-size changes
  before publication and kept all three 480-frame 4T runs exact at 4,981,549
  bytes with 468/12 topology. It still called `StageCoefBlock` immediately
  after quantization, however, so it moved rather than fused the qcoeff walk:
  the three-pair median regressed from 3.571 to 3.643 ms/frame (about 2.0%).
  The probe was reverted. The next A6 slice must produce token classes inside
  the final quantizer scan itself; another post-quantization transaction is not
  a structural deletion. A narrower zero-prefix transaction then removed the
  losing full-leaf copy: all-zero transforms stay as at most 384 private EOB
  descriptors, and the first nonzero transform irrevocably publishes that
  prefix before staging the current and remaining blocks directly into the
  frame arena. All-zero leaves discard the prefix without touching frame tokens
  or coefficient counts. Eligible work remains limited to preserved normal
  inter count passes with stable post-encode transform size; denoiser, SVC,
  forced-reference, dynamic segment-map, sub-8x8, and transform-changing leaves
  keep the established writer. Five interleaved post-PGO 480-frame 4T
  no-denoise pairs stayed exact at 4,981,549 bytes and 468/12 topology while
  moving median wall time from 3.572 to 3.530 ms/frame (about 1.2%). After a
  refreshed profile, an immediate connected trio was 3.508 / 3.511 / 3.521
  ms/frame, but later loaded spots were noisy up to 3.61 ms/frame. A final
  profile-independent five-pair adjudication moved the no-PGO median from
  3.615 to 3.561 ms/frame (about 1.5%), with every run retaining the same bytes
  and topology. Full normal/pure-Go suites, focused race and determinism, the
  pinned benchmark frontier, strict byte parity, and PGO checks pass. Full
  token-class production inside quantization remains open. A follow-up hoisted
  the producer-state and coefficient-count pointers out of the per-transform
  staging call after a long uninstrumented profile sampled
  `stageVP9ProducerBlock`, but five no-PGO 480-frame pairs were exact and
  neutral-to-worse (3.566 baseline versus 3.569 ms/frame candidate medians).
  The hoist was reverted; the sampled row needs deeper token-production work,
  not another leaf-constant pointer reshuffle. Rewriting the hot full-window
  qcoeff tokenizer to walk only `[0,eob)` and append EOB afterward likewise
  kept direct/staged parity and exact 4,981,549-byte connected output, but
  focused sparse/dense tx16 samples overlapped the existing loop and three
  no-PGO 480-frame pairs were neutral (3.552 versus 3.553 ms/frame medians).
  That loop rewrite was reverted as well; the per-token EOB comparison is not
  the remaining A6 cost. A
  narrower attempt to derive `eob_cost` from `txIdx` instead of incrementing it
  in the loop was neutral-to-worse in focused `BenchmarkVP9BlockYrd` samples
  (~515-526 ns/op after a ~511-523 ns/op baseline) and was reverted.
  Measurement note, 2026-07-16 (token classes inside the final quantizer scan
  + denoiser producer staging): the mandated fusion slice landed as one safe
  point. A fused `vp9_quantize_fp` NEON sibling (`quantizeFPFullTokenNEON`)
  now emits one `vp9_pt_energy_class[token(|qcoeff|)]` byte per raster
  position inside the final quantizer scan via a saturating-index table
  lookup (`T[min(|q|,15)]`, five extra instructions per 8-lane group), into a
  compact per-block classes sidecar parallel to the qcoeff store with
  per-4x4-origin validity that every residue producer resets. Token staging
  consumes the span directly (`stageCoefBlockQCoeffClasses`): zero-run tests
  and neighbor contexts read precomputed classes, deleting the incremental
  token-cache writes and the walk's loop-carried context dependency. Kernel
  parity pins classes against the scalar quantizer plus the ground-truth
  energy mapping across randomized dequant/coefficient distributions, and a
  staging gate pins the classes walk token/count-identical to the cache walk.
  Alone the kernel was wall-neutral on the loaded 1T denoise spot (13
  interleaved pairs split 6/7, medians 10.588 control vs 10.627 ms/frame
  candidate): it classifies all maxEob positions while the staging walk only
  visits eob+1. The same safe point therefore extended producer-time token
  staging to denoiser-active count passes: motion-compensated denoising
  mutates the block source before the final residue is prepared, so producer
  tokens equal what count-walk WriteCoefSb staging derives from the same
  committed sidecar, and a failed post-count denoiser commit ignores them
  exactly like count-staged tokens. That deletes the denoise lane's cold
  count-walk staging: `WriteCoefSbFromArgs` (4.1% cum) disappeared from the
  240-frame 1T profile, replaced by producer-time `stageVP9ProducerBlock`
  (4.5% cum, classes walk 1.3%) beside `quantizeFPFullTokenNEON` (1.3%).
  Thirteen interleaved 120-frame 1T denoise pairs under heavy host load: the
  candidate won 12 of 13 with pooled medians 10.489 to 10.334 ms/frame (about
  1.5%; the first ten pairs alone measured 10.492 to 10.280, about 2.0%). The
  1T no-denoise variant won 3 of 3 pairs at 10.684 to 10.554 ms/frame medians
  (about 1.2%) with byte-identical 1,234,834-byte output at 108/12.
  Byte pins stayed exact on every lane: 1T denoise 1,235,511 bytes at 108/12
  (native and purego), 480-frame 8T row-MT denoise 4,983,704 at 468/12,
  480-frame 8T row-MT no-denoise 4,981,549 at 468/12, and 480-frame 4T tiles
  4,981,549 at 468/12. Focused producer/token/denoiser tests, row-MT
  determinism, the focused race slice, and the full suite pass; the PGO
  fingerprint was refreshed. Remaining A6 classes work: the keyframe/intra
  trellis quantizer and sub-8x8/forced-reference/segment classes still stage
  through the incremental token-cache walk.
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
  Measurement note, 2026-07-16 (nonrd prepared-context candidate prediction):
  the A5/A6 candidate-evaluation dataflow of vp9_pick_inter_mode is now
  structurally ported. A per-(block, ref) prepared prediction context
  (`vp9NonrdPredBlockCtx`) mirrors find_predictors/vp9_setup_pred_block: the
  block's UMV clamp edges, source window, and per-ref pointers into the
  persistent padded reference planes are resolved once per block, with a
  one-time coverage proof that every UMV-clamped 8-tap window stays inside
  the padded plane. Every luma candidate — whole-block, filter sweep,
  post-sweep rebuild, and zero-MV — is now the verbatim
  build_inter_predictors leaf (clamp -> q4 subpel split -> convolve/copy
  from pre[0] into the active compact PRED_BUFFER), deleting the
  per-candidate NeighborMi construction, reference slot lookup/validation,
  geometry/edge recomputation, the zero-MV visible-plane special case, and
  the visible/padded/extend window triage. The UV skip test,
  color-sensitivity adds, and encode-breakout chroma checks share the same
  prepared shape, resolved lazily on the first UV consumer per block (chroma
  has no persistent padded plane, so replicated-edge staging remains only
  for tap windows leaving the visible chroma plane). The mv-pred prepass and
  the NEWMV pred_mv_sad[LAST] refresh read the prepared pre[0] pointers
  directly, and the candidate loop's per-candidate source-plane refetches
  are gone. Scaled refs and sub-8x8 shapes keep the legacy route through
  wrapper fallbacks. An exhaustive gate pins ctx == legacy scratch ==
  decoder-recon bytes plus identical (variance, sse, ok) across 10 block
  shapes x 4 filters x 10 MV classes (zero, full-pel, half/quarter/odd/
  one-axis subpel, edge-crossing, beyond-clamp) x 4 positions on aligned and
  ragged dims, with a chroma leg pinning the written recon rect. The root
  cause of the registry's byte-inequivalent scratch-convolve probe is now
  understood and its dead relic deleted: it fed raw 1/8-pel phase (&7, >>3)
  into the 1/16-pel kernel tables, skipped clamp_mv_to_umv_border_sb, and
  convolved the clamped score window instead of the full block — the
  prepared context uses the q4 clamp/phase chain, which the gate proves
  byte-exact. Byte pins stayed exact on every lane: 1T 120f/240f/480f at
  1,235,511 / 2,483,072 / 4,983,461 bytes, threads {2,4} at 1,236,273 /
  1,234,903, 8T row-MT denoise 1,235,979, and 4T no-denoise 1,236,037, all
  108-or-468/12; allocs/frame unchanged (0.633 at 120f). Ten interleaved
  240-frame 1T pairs on the loaded host: the candidate won 6 of 10 with
  medians 11.109 -> 11.025 ms/frame (about 0.8%). The follow-up profile no
  longer samples the legacy predict chain (predictVP9InterBlockLumaToScratch
  / predictVP9ZeroMVLumaCopyToScratch / vp9NonrdUVVariancePlaneSSE) under
  the picker: candidate prediction is now ctx-predict ->
  InterPredictorWithScratch plus one variance leaf. Remaining: the
  intra-winner predictor carry (twice-rejected, needs the reconstructed tx
  chain), commit-side winner reuse at the pick/commit boundary (CLOSED
  2026-07-16, see the reuse_inter_pred boundary note below), and
  sub-8x8/scaled candidates on the legacy route.
- Measurement note, 2026-07-16 (reuse_inter_pred pick/commit boundary
  closure): audited the boundary against libvpx ground truth before touching
  it. libvpx semantics: `sf->reuse_inter_pred_sby = 1` at rt speed >= 5
  (vp9_speed_features.c:609); `reuse_inter_pred = sf->reuse_inter_pred_sby &&
  ctx->pred_pixel_ready` (vp9_pickmode.c:1747); pred_pixel_ready is
  WALKER-owned PICK_MODE_CONTEXT state — nonrd_use_partition seeds 1 before
  EVERY >=8x8 leaf pick (vp9_encodeframe.c:5019/5030/5040/5052/5063), the
  sub-8x8 leaf_split pick never gets it and vp9_pick_inter_mode_sub8x8
  forces 0 (vp9_pickmode.c:2776); at the pick tail the retained best_pred is
  vpx_convolve_copy'd into pd->dst when not already aliasing it
  (vp9_pickmode.c:2668-2684); encode_superblock then skips ONLY the luma
  rebuild — `if (!(sf.reuse_inter_pred_sby && ctx->pred_pixel_ready) ||
  seg_skip) vp9_build_inter_predictors_sby` (vp9_encodeframe.c:6073) — and
  ALWAYS rebuilds chroma (sbuv). Instrumented counters on the cpu8 720p 1T
  denoise spot showed the boundary handoff (lumaPredReady, landed 6e4be836
  and preserved by the PRED_BUFFER restructure) already consumed the
  picker-retained winner for 98% of inter commits (637,167 reuse / 12,840
  rebuilds / 120f); ALL 12,840 rebuilds were one class: clipped frame-edge
  leaves (the 40 Block32x16 strips at mi row 88 of the partial bottom SB
  row), where the VarBased gate re-derived pred_pixel_ready from the
  choose_partitioning grid stamp — an invented derivation; the walker's edge
  geometry comes from the clipped dispatch, not a stamped cell, and libvpx
  seeds those leaves like any other. Fix: thread the walker's leaf-commit
  seed (`commitLeaf`, the PICK_MODE_CONTEXT analogue) from the writer's leaf
  commit (SbType >= BLOCK_8X8 at prepareVP9InterPredictionBlock) through
  pickVP9InterReferenceMode(NonRD) into the gate; the VarBased branch now
  returns exactly that seed and the grid re-derivation is gone
  (partition-search probes pass false; ReferencePartition/ML branches keep
  their conservative select/pick-partition models). Post-fix counters:
  rebuild = 0 for all inter-winner commits. Byte pins exact on every lane —
  1T 120/240/480f at 1,235,511 / 2,483,072 / 4,983,461 (native AND purego),
  t2 1,236,273, t4 1,234,903 (repeat-run deterministic), 8T row-mt denoise
  1,235,979, 4T no-denoise 1,236,037; allocs/frame 0.6333. Oracle stream
  matrix failing-row set IDENTICAL to base (49 PASS / 1 pre-existing
  fixed-q-rt-cpu0-constant red on both). Seven interleaved 240f 1T pairs on
  the loaded host: medians 11.005 -> 11.018 ms/f, candidate 2/7 pairwise —
  split = neutral, as expected for ~0.7% of blocks (one 32x16 luma convolve
  x40/frame); the commit is fidelity closure, not a wall win. New gates: the
  gate unit test re-pinned to walker-seed semantics (incl. an unstamped
  clipped-edge leaf case), and an end-to-end
  TestVP9NonrdReuseInterPredReconMatchesDecoder pins encoder recon ==
  decoder recon byte-for-byte at 144x80 (16px bottom-strip geometry) and
  128x128 — the decoder always rebuilds prediction, so any drift in a
  reused predictor fails it. Remaining at the boundary: chroma is rebuilt at
  commit BY DESIGN (libvpx sbuv); intra winners re-predict at encode
  (libvpx-verbatim, and the intra-winner carry stays twice-rejected);
  sub-8x8 stays on the rebuild path (libvpx forces 0); ReferencePartition
  delegated subtrees and ML probe seeds remain conservatively false vs
  libvpx's nonrd_pick_partition rect-probe seeds (reuse there is
  dataflow-only, no byte risk, but unported).
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
dispatcher/cache walk, and keyframes now use the same pure partition/leaf
replay; PARTIAL 2026-07-16: sub-8x8 leaves pure-pack from the committed
miGrid Bmi quartet and the odd-MI staging disable is lifted after fixing the
four latent bugs that broke the earlier attempts (sub-8x8 residue tx pin to
TX_4X4, true-leaf-size producer tx stability, intra sub-8x8 mode==bmi[3]
invariant + per-sub-block y_mode counting, omitted-store poison recovery,
surfaced write-walk errors); count-side partition picking and old cache
deletion remain; remaining −0.3..0.5. CLOSED 2026-07-16 as a non-item at
cpu8: a fresh 240-frame 720p realtime cpu8 1T profile decomposes the
whole count-walk partition dispatch (`pickVP9BlockSizeForRegion` 0.18s
cum of 2.26s) into `GetEstimatedPred`/`IntProEstimate` 0.12s — the
VeryHighSad int-pro branch libvpx also runs under the identical
`speed >= 8 && !low_res && content_state != kVeryHighSad` gate
(vp9_encodeframe.c:1451-1497, parity work on this high-SAD synthetic
fixture) — plus `ChoosePartitioning` 0.03s (the once-per-SB analysis
itself) and only ~0.02s (~0.08 ms/f) of per-node dispatch glue. The
per-node consumption already matches libvpx's
`partition_lookup[bsl][mi[0]->sb_type]` stamp-read shape
(nonrd_use_partition, vp9_encodeframe.c:5009-5010): govpx reads the
`choose_partitioning` grid stamp through
`pickVP9CBRVariancePartitionBlockSize` with an index-test fast path in
`vp9EnsureSBPartitionChosen`. Do not spend the old estimate; the
recoverable glue is ~0.08 ms/f with real dispatch-ordering parity risk); A4 delete replay
infrastructure (PARTIAL 2026-07-11: removed the redundant picker-side leaf
decision store while retaining the finalized fallback entry; PARTIAL
2026-07-16: write-side leaf-cache replay (canReplay*/apply*) and the whole
inter partition decision cache deleted after the min-partition pack gate was
lifted — every preserved inter frame packs purely; the finalized leaf cache
itself stays for unpreserved fallback walks and demotion/recovery reruns;
remaining −0.05..0.15 is that cache plus keyframe fallback storage); A5 pick-buffer
end-state (PARTIAL 2026-07-16: nonrd `search_filter_ref` swaps compact
eval/best ownership, normal non-ML `pred_pixel_ready` picks use three
compact buffers plus dst as libvpx's fourth PRED_BUFFER including final and
pre-intra ownership handoff, the ML partition lane uses the same pool with
SB-local `pickPred` as dst, and the prepared per-(block, ref) context now
makes every luma/chroma candidate a direct clamp/convolve from the
persistent padded (luma) or visible (chroma) reference into those buffers;
2026-07-16 the pick/commit winner handoff is CLOSED — commit-time luma
rebuilds are 0 for inter winners after the walker-seeded pred_pixel_ready
port, clipped frame-edge leaves included — leaving intra-winner pred carry
at −0.2..0.4); A6
subpel direct on padded refs + bare
vp9_xform_quant_fp commit with skipTxfm consumption (PARTIAL 2026-07-11:
realtime inter FP commit bypasses the trellis-capable wrapper, writes q/dq
output directly, and hoists tx/dequant/scan/table checks to plane-level for the
normal residual loop plus tx-candidate/context-stamp loops; luma AC/DC
skipTxfm is consumed for segment-0 non-lossless realtime FP blocks, while
AC-only remains explicitly non-FP/open; `BlockYrd` EOB scratch is narrowed to
int16, and edge candidate prediction reads the persistent padded reference
directly instead of constructing a temporary tap window; q/dq SB staging is
now tx-block compact, dqcoeff is tx-local, and the token walker consumes the
compact qcoeff/EOB sidecar directly without callbacks or a value-copy handoff;
producer-time transactional token staging remains −0.7..1.0; PARTIAL
2026-07-16: token classes are now produced inside the final quantizer scan
via the fused NEON kernel + compact classes sidecar + classes-driven staging
walk, and producer-time staging covers denoiser-active count passes — 1T
denoise moved about −2.0% with all byte pins exact; remaining −0.3..0.6 is
the keyframe/intra quantizer classes plus sub-8x8/forced-reference/segment
staging classes).
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
- B4 is partial as of 2026-07-11: accepted whole-MV predictor commits now
  prepare immutable reference-plane metadata once for each enabled LAST / GOLDEN /
  ALT reference and reuse it across the macroblock walk. The three prepared
  states stay in a frame-local array keyed by reference-frame enum, so the hot
  picker continues to copy only the compact `interAnalysisReference`; split-MV,
  denoiser reconstruction, and zero-value focused-test references retain the
  existing per-MB path. A direct prepared-vs-per-MB parity test covers ZeroMV,
  edge-clamped subpel, six-tap, bilinear, and full-pixel predictors. Five
  order-alternated, explicitly no-PGO 480-frame cpu8 serial pairs kept exact
  5,066,778-byte output, 478 encoded / 2 dropped, and 0 allocs/frame while the
  median moved from 7.479 to 7.425 ms/frame (about 0.7%); the candidate won four
  of five pairs. Three matching 4T pairs kept exact 5,111,925-byte output and
  456/24 topology while nudging the median from 4.245 to 4.238 ms/frame. A
  paired serial profile reduced sampled `newFrameInterRefState` from 20 to
  10 ms cumulative; the remaining sample is the separate denoiser path. This
  removes accepted-path setup only. After refreshing the repository PGO
  profile, three 480-frame pairs were wall-neutral (7.314 baseline versus
  7.320 ms/frame candidate medians, with two candidate pairwise wins) and kept
  the same bytes/topology. Direct encoder-kernel winner construction /
  predictor carry remains open before B4 can be called complete.
- B5 has a tiny prepared-state safe point as of 2026-07-11: the four persistent
  denoiser running-average buffers now prepare their immutable reference-plane
  metadata when they resize, then reuse it for motion-compensated per-MB
  prediction. Zero-value states still fall back to the original path for tests
  that manually assemble denoiser buffers. Ten order-alternated, explicitly
  no-PGO 480-frame serial pairs kept exact 5,066,778-byte output, 478/2
  topology, and 0 allocs/frame; the candidate won eight pairs and the aggregate
  median moved from 7.470 to 7.443 ms/frame (about 0.36%). A candidate profile
  removed `newFrameInterRefState` from the sampled MB walk. Three 4T pairs kept
  exact 5,111,925-byte output and 456/24 topology but were wall-neutral (4.302
  baseline versus 4.310 ms/frame candidate medians). Three refreshed-PGO
  serial pairs also all favored the candidate, but severe thermal drift from
  the 8.0 to 9.6 ms/frame band makes that result directional only. Count this
  only as setup glue: the per-MB `thismb` staging/filter shape and FDCT-cache
  opportunity remain open.
- B5 now also has compact whole-MV denoiser predictor staging as of 2026-07-11.
  Prepared whole-MV reconstruction writes directly into encoder-owned
  16x16 Y and 8x8 U/V arrays, which row-worker encoder copies own independently,
  instead of touching the macroblock region of the full-frame `mcRunning`
  buffer. Split-MV and validation failures retain the frame-backed fallback.
  A focused decoder test pins compact destinations against canonical
  frame-backed output for zero-MV, subpel, bilinear, and full-pixel predictors.
  The tagged 120-frame phase spot was wall/phase-neutral with exact 1,328,027
  bytes and 118/2 topology. Five explicitly no-PGO 480-frame serial pairs kept
  exact 5,066,778-byte output, 478/2 topology, and 0 allocs/frame; the candidate
  won four pairs and moved the median from 7.108 to 7.041 ms/frame (about 0.9%).
  Three 4T pairs kept exact 5,111,925-byte output and 456/24 topology, with two
  wins and a 3.854 to 3.844 ms/frame median. After PGO refresh, three serial
  pairs stayed exact and were wall-neutral (7.006 baseline versus 7.002
  ms/frame candidate median, with two pairwise wins). Full compact Split-MV
  staging and the disconnected FDCT winner cache remain open.
- B7 has a direct ARM64 vertical-fusion safe point as of 2026-07-11. The grouped
  luma kernel processes two 8-row halves, loading each 16-pixel row once and
  transposing it into eight paired column vectors (columns 0-7 in low lanes,
  8-15 in high lanes). It then applies edges 4, 8, and 12 sequentially in that
  register domain, preserving the modified-pixel dependency between adjacent
  edges, before the inverse transpose and one store per row. Expanded grouped /
  separate parity covers zero/max thresholds and randomized blocks. Same-state
  hot-buffer medians moved from 45.53 to 35.65 ns/op (about 21.7%) at 0
  allocs/op. Three tagged 120-frame pairs kept exact 1,328,027-byte output,
  118/2 topology, and 579 trials while median trial-filter time fell from 277.6
  to 227.9 ms total (about 0.414 ms/frame or 17.9%); median wall time moved from
  8.911 to 8.453 ms/frame (about 5.1%). Five explicitly no-PGO 480-frame serial
  pairs all won with exact 5,066,778-byte output, 478/2 topology, and 0
  allocs/frame, moving the median from 9.122 to 8.715 ms/frame (about 4.5%).
  Three matching 4T pairs kept exact 5,111,925-byte output and 456/24 topology
  while moving the median from 4.396 to 4.079 ms/frame (about 7.2%). After
  refreshing the repository PGO profile, three order-alternated serial pairs
  all favored the fused kernel with exact 5,066,778-byte output, 478/2
  topology, and 0 allocs/frame; the median moved from 8.956 to 8.452 ms/frame
  (about 5.6%).
- B7 also has a direct ARM64 horizontal-fusion safe point as of 2026-07-11.
  A rolling eight-row register window carries the four rows shared by adjacent
  edges, so edges 4, 8, and 12 keep their dependency order while reducing the
  grouped luma path from 24 row loads to 16. The expanded grouped / separate
  parity gate covers fixed extremes, randomized pixels, and randomized
  threshold triples. Seven order-alternated hot-buffer samples moved from a
  26.36 to 20.15 ns/op median (about 23.6%) at 0 allocs/op. Three tagged
  120-frame pairs kept exact 1,328,027-byte output, 118/2 topology, and 579
  trials while median trial-filter time fell from 207.1 to 190.7 ms total
  (about 0.137 ms/frame or 7.9%); median wall time moved from 7.468 to 7.426
  ms/frame. Five explicitly no-PGO 480-frame serial pairs kept exact
  5,066,778-byte output, 478/2 topology, and 0 allocs/frame; the candidate won
  four pairs and moved the median from 7.874 to 7.591 ms/frame (about 3.6%).
  Three matching 4T pairs all won with exact 5,111,925-byte output and 456/24
  topology, moving the median from 7.863 to 7.291 ms/frame (about 7.3%) under
  degraded machine load. After refreshing the repository PGO profile, all
  three order-alternated serial pairs favored the candidate with exact bytes /
  topology; severe thermal drift makes the 9.641 to 9.296 ms/frame median
  (about 3.6%) directional rather than a stable absolute timing claim.
- A follow-up B7 vertical-lane packing probe is closed. Packing both 8-row
  transposes into 16-lane column vectors halved the number of filter bodies and
  kept 5,000 randomized C parity cases exact. It improved the focused Go
  kernel median from 35.79 to 34.69 ns/op (about 3.1%) at 0 allocs/op, but the
  connected filter phase was neutral (186.89 versus 187.43 ms total over 120
  frames). Five no-PGO 480-frame serial pairs stayed exact at 5,066,778 bytes,
  478/2 topology, and 0 allocs/frame, but the candidate lost four pairs and
  regressed the median from 7.436 to 7.467 ms/frame (about 0.4%). The 144-byte
  spill/save frame and packed-lane shuffles were removed; keep the published
  two-half kernel unless a new code shape removes that pressure.
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
  those exact shapes without a fresh profile. A grouped ARM64 vertical-edge
  probe transposed each 16x16 luma macroblock once, ran the three sequential
  inner filters on a stack-local transposed tile, then transposed back. It
  reduced strided frame loads and kept grouped/separate edge parity, but the
  focused hot-buffer benchmark regressed from about 39.2 to 45.1 ns/op. One
  120-frame phase pair lowered trial-filter time from 225.1 to 220.3 ms total,
  but the longer 480-frame gate was unstable and ultimately negative: one pair
  won, two lost, and the three-run median moved from 7.363 to 7.408 ms/frame,
  with exact 5,066,778-byte output and 478/2 topology throughout. The probe was
  removed; future B7 vertical fusion must operate directly on the frame rather
  than round-trip the full macroblock through a transposed scratch tile. A
  follow-up direct-frame probe compiled a whole-TU three-edge wrapper from the
  pinned libvpx NEON source, producing a 410-instruction inlined body that
  hoisted threshold splats and stride setup while preserving sequential edge
  dependencies. Grouped/separate parity passed, but same-state hot-buffer
  medians regressed from 42.81 to 43.11 ns/op (about 0.7%). The generated
  kernel was removed; B7 needs to reduce the repeated 16x8 load/transpose/store
  work itself rather than only inline the three existing kernels. Reusing the
  existing UV-pair vertical kernel with luma rows 0-7 and 8-15 treated as its
  two planes also preserved grouped/separate parity, but its dual-plane
  load/store schedule regressed the grouped median to 53.75 ns/op (about 25%
  slower than the current V16 path), so that dispatch was removed too.
- The lf-pick wall-stall front is CLOSED as diagnosed parity on 2026-07-16
  (base 62e9c05d, M4 Max, canonical 720p realtime cpu8 spots). Fresh 480-frame
  phase timing fully reconciles the phase with zero unattributed wall:
  lf_pick 2.04 ms/f = 5.31 trials/frame x (copy 19.98 us + filter 323.5 us +
  sse 33.0 us per trial) + ~0.04 ms/f context glue. Two structural facts the
  front's framing missed: (1) the canonical fixture pins auto-Speed at 4,
  which in libvpx realtime speed mapping sets sf->auto_filter=1 and selects
  the FULL picker (vp8cx_pick_filter_level: full-plane vpx_yv12_copy_y +
  vp8_loop_filter_frame_yonly over all 45 MB rows + full-frame
  vp8_calc_ss_err per trial); a first-vs-rest trial bucket probe measured 0
  partial fast-picker trials in 2533, so per-trial costs are full-frame, not
  the 5-MB-row partial window. (2) There is no memory stall: the committed
  production-geometry benchmark (BenchmarkVP8LoopFilterPickTrial720p)
  reproduces the connected sub-phase numbers cache-warm (copy ~18.5 us,
  filter ~330 us, sse ~56-58 us vs connected 19.98 / 323.5 / 33.0 — the
  connected SSE is faster than isolated because the just-filtered band is
  still cache-resident), and the V16 inner-edge kernel's in-situ per-call
  cost from the 480-frame CPU profile (0.30 s flat / ~19k calls per frame =
  33 ns) equals its hot-buffer microbench exactly. The earlier "2.39 wall vs
  1.33 CPU" reading was a sampling/attribution undercount (the fresh profile
  captured 84% of wall; scaled LF-path samples equal the phase wall) taken
  before the B7 fusions reduced the phase to 2.04 ms/f. Remaining inventory
  was priced and rejected as below-noise for this front: per-MB dispatch/walk
  glue is ~0.13 ms/f of samples (a one-call-per-MB fused dispatch could
  reclaim at most about half), and a single-pass copy+filter+SSE trial
  pipeline is bounded by the connected-vs-isolated delta at ~0.05-0.12 ms/f
  because production trials already run effectively warm. Neither approaches
  the ~1.0 ms/f this front hoped to recover. libvpx runs the identical
  full-trial structure with per-MB kernel shapes no cheaper than the fused
  B7 kernels (its ARM bv/bh wrappers do three separate load/transpose rounds
  per MB where the fused V16/H16 kernels load once), so lf-pick is at parity
  and the remaining 1T encode gap versus libvpx lives essentially entirely
  in the MB walk and packet write. Non-ARM64 kernels remain unexamined.
- B2 has a predictor-scratch scoring safe point as of 2026-07-16: the fast
  picker's whole-MB intra candidates (DC/V/H/TM) now predict into a
  contiguous 256-byte stride-16 scratch in the picker loop context — the
  verbatim libvpx pickinter.c shape (vp8_build_intra_predictors_mby_s into
  x->e_mbd.predictor + vpx_variance16x16 against it) — instead of writing
  every candidate strided into the analysis frame; the accepted path
  re-predicts the winner exactly as before, and rejected candidates no
  longer touch frame pixels. The per-MB neighbor stripes also resolve
  through a single-gate interior fast path (direct above/top-left alias +
  inline left-column gather) that defers to the full edge-aware builder for
  frame-edge and rightmost-column MBs (whose 20-byte above stripe needs the
  extended border). A new focused test pins the fast-path refs against
  BuildIntraPredictorRefsLuma at every MB position on three geometries
  including 65x63, and a second pins the buffer-variance helper against the
  frame-backed variance on interior and clamped partial-edge MBs. Eight
  order-alternated, explicitly no-PGO 480-frame cpu8 serial pairs kept
  exact 5,066,778-byte output, 478/2 topology, and 0 allocs/frame; the
  candidate won five pairs and aggregate medians moved from 7.392 to
  7.379 ms/frame — count this as byte-exact structural glue with a
  directional (within host noise) wall reading, not a measured ms/f win.
  A tokenpack tweak (pointer-load of the per-token extra-bit encodings so
  the common ONE..FOUR tokens skip a 14-byte struct copy) rode along and
  was not separately adjudicated.
- REJECTED probe, 2026-07-16: hoisting the ~700-byte fastInterModeLoopContext
  from the picker stack into a persistent encoder field (per-MB flag reset
  instead of full memclr) lost five of five interleaved no-PGO 480-frame
  pairs (medians 7.40 → 7.44 ms/frame) with byte-identical output. The
  heap-pointer form forces the compiler to assume aliasing between the
  context and other encoder fields across the picker body, which costs more
  than the per-MB stack zeroing it saves (measured at only ~10-30 ms cum
  per 480 frames). Do not retry that exact shape; any future persistent
  context must isolate the state from *VP8Encoder-reachable memory.
- B5 FDCT-winner-cache line item CLOSED for realtime cpu8 on libvpx ground
  truth, 2026-07-16: vp8_pick_inter_mode (pickinter.c) populates no
  DCT/predictor cache at any speed — the encode side
  (vp8_encode_inter16x16, encodemb.c) unconditionally re-runs
  vp8_build_inter_predictors_mb + subtract + transform + quantize for the
  winner. govpx's fast-picker rd_cache=0 / dct_hits=0 telemetry therefore
  matches libvpx's structure, not a missing optimization; the winner cache
  only pays on the RD picker path where it is already wired. Reconnecting
  it for the fast picker would be a govpx-only heuristic and is out of
  scope.
- B4 whole-MV audit note, 2026-07-16: the accepted-path winner rebuild
  (reconstructWholeMVInterMacroblockFast on prepared per-reference state)
  was read side-by-side against libvpx vp8_build_inter16x16_predictors_mb —
  both run one 16x16 sixtap/copy plus one fused 8x8 UV pair into the
  reconstruction frame with precomputed plane metadata, and libvpx itself
  rebuilds the winner rather than carrying the picker predictor. What
  remains of B4 is wrapper glue (one ~100-byte MacroblockMode copy and the
  validation gates, ~0.02-0.04 ms/f of samples), priced below the byte-risk
  of restructuring; treat whole-MV B4 as at kernel parity and fold any
  remaining work into a future thismb/staging redesign.
- B5 denoiser-overlay safe point LANDED 2026-07-16: the full-frame denoiser
  working copy (CopySourceToFrameBuffer + border extension at every inter
  build, including recodes) is gone. An audit of every coeffSource consumer
  (picker scoring, residual gather, breakout, chroma re-pick, denoiser sig
  reads/writes, near-SAD and dot-artifact sources) proved all source reads
  are current-MB-scoped, so the walk now reads the raw source in place and
  the denoiser writes into a per-MB overlay: filter-candidate MBs stage
  their 16x16 Y (and 8x8 UV when the UV filter is reachable) into the
  overlay right before the filter kernels — the same per-MB thismb copy
  libvpx pays on every MB, here only where a kernel must read-modify the
  signal — and applyDenoiserToInterMacroblock returns a per-plane mask
  that routes the coefficient build through the overlay only for planes
  the denoiser actually wrote. No-filter MBs stay raw-backed with zero
  copies. Partial-edge macroblocks are staged wholesale (clamped
  replication, value-identical to the old PadFrameVisibleToCoded working
  copy) and keep the historic aliased flow, so coded-dimension clamp
  semantics are bit-preserved; complete MBs take identical kernel fast
  paths under either dimension view. Five order-alternated no-PGO
  480-frame cpu8 serial pairs against the just-landed e0fae426 control
  ALL favored the overlay with exact 5,066,778-byte output, 478/2
  topology, and 0 allocs/frame, moving medians from 7.543 to
  7.317 ms/frame (about 3.0%; the win exceeds the ~0.08 ms/f direct copy
  samples because the 2.7 MB/frame staging sweep no longer flushes the
  reference/recon working set). The 4T spot stayed byte/topology-exact at
  5,111,925 bytes, 456/24. Remaining B5 scope after this: per-MB thismb
  staging for the picker's own scoring reads (contiguous stride-16 source
  view) is now purely a locality play with no copy elimination attached;
  re-adjudicate only with a fresh profile showing source-side read misses.
- Token/packet-write front status, 2026-07-16: writePreparedCoefficientTokenRecords
  already runs libvpx's vp8_pack_tokens shape — one pass over compact
  4-byte prepared records with bool-coder state (low/range/count/pos) held
  in locals for the whole row slice, vp8_norm via CLZ, and per-token
  probability rows resolved by precomputed offset (cheaper than libvpx's
  16-byte TOKENEXTRA with its context_tree pointer). A side-by-side read
  of bitstream.c found no structural difference left to port; the
  remaining cost is intrinsic bool-coder arithmetic. After the extra-bit
  pointer-load tweak, the fresh post-slice profile attributes 0.13 s cum
  (~0.27 ms/f) to the writer, down from 0.19 s. The prior one-shot grid
  writer rejection stands; treat this front as at shape parity unless a
  future profile shows the writer regressing.
- Fresh 480-frame profile after the two 2026-07-16 safe points
  (7.26 ms/f sampled run): runtime.memmove fell from 0.15 s to 0.06 s
  (the eliminated staging sweep), the MB walk is 1.56 s cum (50.5%), the
  denoise picker 0.58 s cum, coefficient build 0.32 s cum, LF kernels
  ~25% (parity). The largest remaining govpx-vs-libvpx deltas now live in
  picker candidate scoring flat cost and the coefficient-build pipeline.
- B3 status note (2026-07-16): the subpel eval path already routes through
  the fused one-axis/bilinear NEON subpel-variance kernels
  (subpelVariance16x16{Horizontal,Vertical,Bilinear}NEON behind
  SubpelVariance16x16PtrFast), and the fresh 480-frame profile attributes
  only ~0.06 ms/f cumulative to the whole SubpelVariance16x16 path
  (~1048 fused evals per frame). Nothing material remains of the
  −0.10..0.20 estimate on the canonical realtime cpu8 fixture; treat B3 as
  closed there unless a different fixture shows a subpel-heavy profile.
- Picker candidate-scoring ledger + safe point LANDED 2026-07-16 (second
  session): an item-by-item diff of selectFastInterFrameModeDecisionDenoise
  against vp8_pick_inter_mode's loop (fresh 480f profile, 3.21s samples,
  picker 0.55s cum / 0.14s flat) priced every per-candidate op. Parity
  items (variance16x16 ~30ms sampled, mode-rate/cost_mv_ref ~10ms,
  fast encode-breakout ~20ms, dot-artifact/skin/zeromv-adjust ~10ms,
  motion search glue ~40ms, per-MB intra neighbor stripes ~60ms — libvpx
  reads the same strided left column inside its predictor builder) were
  left alone. Three non-parity items were fixed, all byte-exact by
  construction:
  (1) interModeMVSlots ran the identical findNearInterMotionVectors
  neighbor walk THREE times per MB (predictors, best, counts; 110ms cum,
  the single largest picker line) where vp8_find_near_mvs_bias produces
  all four outputs in one sweep — replaced with a single-pass
  InterFrameNearMVsCountsAt helper (also feeds the RD picker);
  (2) every scored candidate re-derived (rdMult, rdDiv) — including the
  pass-2 iiratio lift and activity multiplier — where libvpx reads the
  precomputed x->rdmult/x->rddiv MACROBLOCK fields; now cached once per
  MB in the picker loop context (~30-50ms sampled);
  (3) the per-MB threshold call copied two [20]int arrays by value plus a
  third copy in the best-threshold gate, where libvpx's loop reads
  x->rd_threshes[] in place — Hot/Denoise now read through pointers to
  the live derived table (reads are value-identical: raise/lower only
  mutate the already-consumed index). Ten order-alternated no-PGO 480f
  serial pairs: 6/10 wins, medians 7.381 -> 7.255 ms/f (~1.7%; the three
  losses were 9.1-9.5 ms thermal-band samples), exact 5,066,778 bytes,
  478/2, 0 allocs/frame; 4T exact 5,111,925 / 456/24; full oracle lane
  1881 subtests PASS / 0 skips. Commit a6682789.
- Coefficient-pipeline staging elision LANDED 2026-07-16 (second session):
  the accepted inter winner path ran ConvertMacroblockCoefficients per MB
  (50-70ms sampled) to stage the quantizer's MacroblockCoefficients into a
  decoder MacroblockTokens that only the immediately-following fused
  residual add consumed — a staging copy with NO libvpx analogue
  (vp8_inverse_transform_mb reads the MACROBLOCKD qcoeff/eobs the
  quantizer just wrote). The fused dequant+IDCT+add core was refactored to
  a raw qcoeff/eob buffer form (decoder callers unchanged, floor 0 — the
  decoder token reader already records luma EOBs with the +skipDC
  promotion) and a new DequantIDCTAddMacroblockCoefficients entry consumes
  encoder buffers directly with a luma EOB floor of 1 for non-4x4 MBs
  (exactly the conversion's max(eob,1) promotion). Audited equivalences:
  encoder quantizers write all 16 slots (zeros at/past EOB) so the
  kernels' sanitize guards are value-neutral; skip-DC luma blocks carry
  qcoeff[0]==0 so the transient Y2 seed/unseed leaves the caller's buffer
  bit-identical. Whole-block intra winners keep the tokens staging for
  reconstructAnalysisMacroblock; B_PRED never read the staged tokens
  (that Convert was already dead work and is now skipped). Serial and
  threaded builders both rewired. Five order-alternated no-PGO 480f
  serial pairs: 5/5 wins, medians 7.205 -> 6.976 ms/f (~3.2%; the win
  exceeds the direct Convert samples because the per-frame 3 MB
  reconstructTokens staging traffic left the walk's working set), exact
  bytes/topology/allocs on 1T, 4T, and 120f pins.
- Post-slice state 2026-07-16 (second session): ten order-alternated
  no-PGO 480f pairs of base d3d203f2 versus the two landed slices gave
  8/10 candidate wins with medians 6.972 -> 6.813 ms/f in a cool window
  (hotter windows earlier in the day showed the same direction from a
  7.31-7.38 base band); all 20 runs byte-exact 5,066,778 / 478/2 /
  0 allocs. Against libvpx 5.35 the 1T ratio is now ~1.27-1.31x (from
  1.37x). The fresh post-slice profile shows every remaining MB-walk
  item at documented parity or previously-rejected shape: picker
  0.46s cum sampled (single near-MV walk 30-40ms intrinsic, variance /
  mode-rate / breakout / motion-search all parity), coefficient walk
  1.34s cum (gather+FDCT+quant batches at kernel parity, fused
  residual add 0.10s, token count+record single-pass at
  vp8_tokenize_mb shape 0.10s, winner rebuild at B4 kernel parity
  0.07s, denoiser 0.19s matching vp8_denoiser_denoise_mb's
  copy/MC/filter structure), LF kernels ~25% (parity), packet writer
  at shape parity. A newly-quantified Go-floor item: ~0.17s sampled
  (~0.4 ms/f) of runtime scheduler preemption cost
  (gopreempt_m -> wakep -> pthread_cond_signal waking spare Ms at
  default GOMAXPROCS on the 1T encode) that libvpx does not pay —
  runtime overhead, not addressable in encoder code. Priced-below-risk
  leftovers: accepted-path StaticInterRDEncodeBreakout re-check
  ~0.09 ms/f (removal risks skip-semantics divergence),
  MacroblockCoefficientsEmpty EOB scan already in its cheap 25-byte
  form, per-MB intra neighbor-stripe gather ~0.05-0.1 ms/f (matching
  libvpx exactly requires strided-left variants of the shared intra
  predictor DSP entrypoints — kernel surgery, thin upside). Honest
  end-state: the remaining ~1.4-1.5 ms/f versus libvpx decomposes into
  parity-shaped phases whose per-op delta is Go codegen versus C+asm,
  plus the ~0.4 ms/f runtime-scheduler floor; no identified structural
  (shape-level) delta remains on the canonical realtime cpu8 fixture.

Steps (gate = TestVP8RealtimeOverloadDropParity SHA + full VP8 parity lane):
B1 compact last-frame {mv,ref,signBias} sidecars (PARTIAL 2026-07-03:
previous-frame improved-MV sidecar landed and legacy full-mode fallback
removed; remaining MB-walk layout work is still open before claiming the
−0.15..0.20); B2 libvpx-shaped intra scoring into contiguous scratch from
direct border pointers (PARTIAL 2026-07-03: direct-above luma aliasing landed;
direct-left / fuller picker scratch layout remains −0.15..0.25); B3 subpel eval fusion after kernel-parity microbench
adjudication (CLOSED on the canonical fixture 2026-07-16: fused subpel-variance
kernels already carry the eval path at ~0.06 ms/f cum); B4 direct winner predictor with encoder kernels /
storePredictor reuse (PARTIAL 2026-07-11: accepted whole-MV commits reuse
prepared per-reference decoder state; 2026-07-16 audit: whole-MV rebuild is
at kernel parity with libvpx's vp8_build_inter16x16_predictors_mb — only
~0.02-0.04 ms/f wrapper glue remains, deprioritized); B5 per-MB denoiser
staging (thismb shape)
+ re-enabled FDCT winner cache (the FDCT-cache line item is CLOSED for
realtime cpu8 on 2026-07-16 libvpx ground truth — pickinter populates no
cache and vp8_encode_inter16x16 always re-runs the pipeline;
PARTIAL 2026-07-03: aliased source/signal no-filter copies are skipped
on the normal denoise path; PARTIAL 2026-07-11: persistent running-average
buffers reuse prepared predictor metadata and whole-MV predictors use compact
per-worker staging; LANDED 2026-07-16: full-frame denoiser working copy
replaced by per-MB overlay staging with per-plane raw/overlay routing,
~3.0% serial wall); B6 glue (PARTIAL 2026-07-03:
final-mode copy elision landed; remaining glue still −0.05). Then B7: PARTIAL
2026-07-11, direct ARM64 inner-horizontal and inner-vertical fusion landed;
the lf-pick wall-stall investigation is CLOSED on ARM64 as of 2026-07-16
(diagnosed parity, no stall — see the dated bullet above); non-ARM64 kernels
remain unexamined.
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

Measurement note, 2026-07-11: `cmd/govpx-bench` now exposes `-row-mt` and
applies it symmetrically to `VP9EncoderOptions.RowMT` and libvpx
`--row-mt={0,1}`. The first 120-frame 720p realtime cpu8 8T no-denoise
baseline measured 3.62 ms/frame govpx versus 2.26 ms/frame libvpx (1.60x),
with identical 108/12 topology and govpx count time at 2.72 ms/frame. The
govpx phase counters still showed only 108 tile count epochs and no row-job
execution: the existing row pools and wavefront are lifecycle-tested scaffold,
not production dispatch. This pins the C1 starting point and prevents future
row-MT comparisons from accidentally benchmarking libvpx with `--row-mt=0`.

Measurement note, 2026-07-11 (first production C1 dispatch): eligible normal
inter count passes now dispatch SB rows through persistent per-tile worker
pools. Each row owns a token/leaf-mode/partition arena and worker-private
decision, transform, predictor, coefficient, left-context, and count scratch;
the barrier merges staged syntax and counts in raster order. Reconstruction,
mode-info, above contexts, and row-indexed variance-partition/RD state remain
shared under the `VP9RowMTSync` dependency. The total thread budget is divided
across tile columns before the SB-row clamp, avoiding the old N-workers-per-tile
oversubscription shape. Production dispatch is deliberately limited to
realtime variance-partition inter frames with count-token staging,
`adaptive_rd_thresh_row_mt`, and no SVC, denoiser, active segment-map chooser,
or count fallback; tile write remains serial within each tile.

Against the published no-PGO `9a090068` control, three interleaved 480-frame
720p realtime cpu8 8T no-denoise runs moved median wall time from 4.04 to
3.86 ms/frame (about 4.5%) and median count time from 3.08 to 2.75 ms/frame
(about 10.7%). All six runs retained 4,981,549 bytes and 468/12 topology; a
length-delimited packet hash matched exactly at
`2adae6a6b4eb95833492055dc23a75b76d8fc30fc34f3c75c0ef8caa34de6b54`.

Measurement note, 2026-07-11 (atomic row wavefront): the active row batch still
used a `sync.Mutex`/`sync.Cond` handoff for nearly every SB progress update,
which dominated the post-C1 CPU profile with pthread wait/signal and scheduler
work. `VP9RowMTSync.curCol` is now an atomic row-progress array: the dependent
row uses a short `runtime_procyield` loop while the row above is active, and the
producer publishes progress with one atomic store. Helpers continue to block
between frame count passes, so the previous idle-policy win is preserved.

Against `4e3f1ea0`, three interleaved no-PGO 480-frame default-denoiser runs
moved median wall time from 3.57 to 3.50 ms/frame (about 2.0%) and median count
time from 2.58 to 2.50 ms/frame (about 3.1%). The no-denoise lane moved from
3.69 to 3.63 ms/frame (about 1.6%) with count moving from 2.74 to
2.68 ms/frame (about 2.2%). Tile-write and loop-filter phases stayed flat,
outputs remained exact, the active-denoiser/no-denoiser thread tests remained
byte-identical, and the focused race gate passed.
Phase counters report 1,868 count epochs and 22,416 row jobs. The full motion
search, block-shape, predictor, and tile-walk ledger also matches the baseline,
the `{2,4,8}` production thread test is byte-identical, and steady-state row
dispatch is zero-allocation. Remaining C1 work is a global multi-tile stealing
queue and extension beyond the conservative eligibility envelope; C2 still
owns denoiser row scaling.

Probe note, 2026-07-11 (global tile stealing): a source-shaped per-tile FIFO
coordinator let every active row worker switch to the queue with the most
unclaimed rows after its assigned tile emptied. The 480-frame 720p cpu8 8T
no-denoise field performed 284-314 cross-tile steals per run and stayed
byte/topology-identical, but median wall time regressed from 3.86 to
3.89 ms/frame and count time from 2.75 to 2.79 ms/frame. Govpx's per-stolen-row
tile-state rebinding and atomic queue scans cost more than the modest tile
imbalance they recovered. The probe was removed completely; keep the fixed
per-tile pools until worker/tile ownership is restructured more deeply.

Measurement note, 2026-07-11 (transactional denoiser rows): denoiser-active
count passes now enter the same per-tile row workers only when the prospective
count state is guaranteed to commit: token replay and coding-state preservation
are requested, minimum partitions are at least 8x8, and SVC, active segment-map
coding, and forced-reference segments are absent. The post-count commit still
checks the finalized preservation state. Denoiser source, intra-average, and
motion-compensated images are shared exactly as in the existing tile-MT path;
row workers write disjoint Y/UV blocks while reconstruction/reference handles
remain worker-private.

Against `ffa236ee`, three interleaved no-PGO 480-frame 720p realtime cpu8 8T
default-denoiser runs moved median wall time from 3.97 to 3.71 ms/frame (about
6.5%) and median count time from 3.02 to 2.58 ms/frame (about 14.6%). Output
remained exact at 4,983,704 bytes and 468/12 topology; the length-delimited
packet hash matched at
`537d43329c94e6c52f0ed8341b43841b431fed7c8f8d55ee4cfb0a4a578701be`.
The field executed 1,868 row epochs / 22,416 row jobs, retained the complete
baseline search/topology ledger, stayed byte-identical across threads {4,8},
and remained zero-allocation in steady state.

Measurement note, 2026-07-11 (row-helper idle policy): VP9 row helpers used the
VP8 row pool's 65,536-iteration `runtime_procyield` idle budget after completing
their only row batch of the count pass. They therefore competed with the
immediately following header, packed tile write, and loop filter even though no
new VP9 row work can arrive until the next frame. Helpers now block on their
start channel between count passes; the dispatcher still uses the existing
short join spin while a batch is active.

Against `4f7b2bdf`, three interleaved no-PGO 480-frame default-denoiser runs
moved median wall time from 3.72 to 3.58 ms/frame (about 3.8%). Count stayed
essentially flat at 2.59 versus 2.58 ms/frame, while tile write fell from about
0.515 to 0.412 ms/frame and loop-filter apply from about 0.219 to
0.202 ms/frame. The no-denoise lane moved from 3.87 to 3.70 ms/frame (about
4.4%), with count flat around 2.75 ms/frame and tile write falling from about
0.545 to 0.424 ms/frame. Outputs remained exact at 4,983,704 / 4,981,549 bytes
and retained the existing denoise/no-denoise packet hashes
`537d43329c94e6c52f0ed8341b43841b431fed7c8f8d55ee4cfb0a4a578701be` /
`2adae6a6b4eb95833492055dc23a75b76d8fc30fc34f3c75c0ef8caa34de6b54`.

Measurement note, 2026-07-11 (fused helper prepare/write launch): threaded
tile writes previously woke each helper once to clone shared encoder state,
parked it, then immediately woke it again to pack its tile. Helpers now clone
and encode under one launch with an atomic preparation barrier; tile column 0
remains quiescent until every helper has finished reading shared state. On the
480-frame 8T row-MT no-denoise lane, three interleaved post-PGO pairs moved
median wall time from 3.605 to 3.581 ms/frame (about 0.7%) while tile-helper
wake signals fell from 5,616 to 4,212. The default-denoiser median moved from
2.610 to 2.598 ms/frame (about 0.45%). Both lanes retained exact
4,981,549/4,983,704-byte output and 468/12 topology. Full normal/pure-Go,
focused threading race, strict byte-parity, trace, and refreshed-PGO gates
pass.

C2 **MT-with-denoiser** (default-path multiplier): PARTIAL 2026-07-03/11. The
VP9 `NoiseSensitivity>0 → tile workers disabled` gate is removed for the
existing tile-MT path; denoiser writes are block/tile-column disjoint in the
source, intra running average, and motion-compensated scratch image, while the
count pass keeps the existing save/restore transaction. The bench oracle now
mirrors this layout instead of forcing libvpx serial for denoise. Gates:
focused thread/bench layout tests, deterministic threaded-denoise packet test,
and a 120-frame 720p realtime cpu8 denoise spot at 4.24 ms/frame govpx vs
2.10 ms/frame libvpx with `--threads=4 --tile-columns=2 --noise-sensitivity=4`,
count=3.27 ms and tile=0.47 ms. The transactional normal inter path now also
uses C1 row workers at 8T, with the 480-frame result recorded above. Remaining
C2 work is extending row dispatch to denoiser fallback/forced-reference cases
and any oracle-denoise algorithmic parity fixes that surface under the broader
option grid.

Probe note, 2026-07-11 (denoiser finalized-decision cache omission): the
prospective denoiser count-state envelope was reused to omit count-side
finalized leaf-decision stores when the later tile pass could pure-pack from
committed `miGrid` plus tokens. On the 120-frame 8T row-MT default-denoiser
spot, `replay_store` fell from about 220,000 to zero while every write leaf
still replayed. Output stayed exact, but three no-PGO 480-frame pairs were
wall-neutral (2.631 vs 2.632 ms/frame medians), and the serial 240-frame spot
regressed from 10.684 to 10.772 ms/frame. The probe was reverted; do not infer
a wall win from deleting this cache store without a deeper ownership change.

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
