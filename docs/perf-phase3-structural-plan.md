# Perf phase-3: structural program

Status: design complete (research-only), nothing implemented. This is the
execution brief for implementation agents. Evidence: 2026-07-02/03 design
sprint — three verified blueprints (VP9 single-walk, VP8 MB-walk, MT program)
built on first-hand reads of libvpx v1.16.0 C and the current govpx tree,
plus fresh A/B measurements. Supersedes the *structural* sections of
`perf-phase2-plan.md` (whose P0/P1 call-shape items are largely landed or
in-flight); measurement-discipline rules there still apply.

Baseline drift warning: ~2600 lines of P0/P1 work were uncommitted in the
main worktree at design time. Re-baseline before/after it lands; the
structural deltas below survive it, exact ms/f numbers may shift.

## Current gaps (720p realtime, post-P0/P1-partial)

| Front | ms/frame govpx vs libvpx | ratio |
|---|---|---|
| VP9 encode 1T cpu8 denoise | 13.5 vs 5.9 | 2.28x |
| VP9 encode 4T no-denoise | 4.65 vs 2.18 | 2.14x |
| VP9 encode ANY-T denoise | serial (govpx gate) | — |
| VP8 encode overload | 8.0 vs 5.25 | 1.53x |
| VP9 decode | 1.99 vs 1.51 | 1.32x |
| VP8 decode | 1.95 vs 1.65 | 1.18x |

## ALERT — oracle config audit (do before ANY denoise-related work)

`internal/coracle/build/libvpx-v1.16.0-vp9/vpx_config.h:94` has
`CONFIG_VP9_TEMPORAL_DENOISING 0`: all `--noise-sensitivity>0` bench/parity
comparisons so far ran against a NON-denoising libvpx. Rebuild the oracle
binaries with temporal denoising enabled, re-pin denoise-path parity, and
re-baseline the VP9 denoise rows. Add a standing rule: check `vpx_config.h`
of every oracle binary before trusting a feature-gated comparison.

## Program A — VP9 encode single-walk + pick-buffer dataflow
Target: 13.6 → ~9.4-10.2 ms/f (~1.6-1.7x). Full blueprint: agent report
"VP9 single-walk architecture" (this session); key verified facts:

- The probability-ordering problem is ALREADY SOLVED in govpx: tokens are
  symbolic (`TokenExtra.ProbOff` ≡ libvpx TOKENEXTRA context_tree pointer;
  libvpx mutates fc in place in write_compressed_header BEFORE encode_tiles
  packs). Only the pack walk's shape is wrong.
- The count walk runs a live arithmetic coder into a discard sink
  (vp9_encoder_counts.go StartDiscard) — pure tax.
- Replay infra (206B/leaf decision cache, canReplay validation, entropy
  snapshots) exists only to serve the two-pass shape; deletable once pack is
  pure.
- CORRECTION to phase-2: libvpx `estimate_block_intra` DOES call block_yrd —
  the intra-fallback row is mostly legitimate work, not waste.

Steps (each ships green; gate = 120f byte-identity + packet-0
frontier + SVC/RTP + zero-alloc + conformance decode + pre-merge sequence):
A1 kill discard coder (−0.2..0.4); A2 stage tokens for ALL leaf classes incl.
keyframe/forced-intra/segment-skip, per-tile arenas (−0.1..0.2); A3 pure pack
walk reading miGrid + 16-24B/leaf syntax sidecar + token stream — deletes
partition dispatcher, canReplay/applyVP9CountPass, write-pass residue
(−0.6..0.8); A4 delete replay infrastructure (−0.2..0.3); A5 pick-buffer
end-state: tmp[0..3] PRED_BUFFER discipline, dst-as-4th-buffer, direct
convolve into eval buffers, intra-winner pred carry (−1.0..1.4); A6 subpel
direct on padded refs + bare vp9_xform_quant_fp commit with skipTxfm
consumption (−1.0..1.3).
Risks pinned in the blueprint: all-class token staging (SVC leaf visitation
— keep SVC on direct path initially + dual-run byte-compare tag); scratch
convolve byte-inequivalence on recorded filter×size cells (extend the
SADScratch parity test to every cell BEFORE rerouting); bare-quantize tx16
crash history (per-tx-size equivalence + long-bench crash gate first).

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

Steps (gate = TestVP8RealtimeOverloadDropParity SHA + full VP8 parity lane):
B1 compact last-frame {mv,ref,signBias} sidecars (−0.15..0.20); B2
libvpx-shaped intra scoring into contiguous scratch from direct border
pointers (−0.15..0.25); B3 subpel eval fusion after kernel-parity microbench
adjudication (−0.10..0.20); B4 direct winner predictor with encoder kernels /
storePredictor reuse (−0.10..0.15); B5 per-MB denoiser staging (thismb shape)
+ re-enabled FDCT winner cache (−0.10..0.15); B6 glue (−0.05). Then B7: the
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
C2 **MT-with-denoiser** (default-path multiplier): libvpx has NO denoiser
thread gate — writes are block-disjoint into frame-sized buffers. Remove
govpx's `NoiseSensitivity>0 → threads=1` gate after making mcRunningAvg
scratch per-worker and auditing count-pass save/restore. REQUIRES the oracle
rebuild from the ALERT first (govpx's denoiser was validated against a no-op
oracle — algorithmic divergence may surface; fix parity before MT).
C3 **Threaded decode**: reuse the encoder VP9LfSync port for row-based
decode LF-MT (replaces the 3-plane ≤3-way split); then row-mt decode
(PARSE/RECON/LPF queue) for 1-tile streams (+26% measured). Gates:
128-vector conformance × threads {1,2,4,8}.
C4 VP8 encode: already at MT parity — nothing to do.

## Sequencing for implementation agents

- A and B are disjoint codecs — run in parallel (separate agents, file
  boundaries root-vp9 vs vp8).
- C1 touches the same walk/tile files as A1-A4: sequence C1 AFTER A4 lands
  (or coordinate via strict file locks); C2 after C1 + oracle rebuild;
  C3 (decoder) is disjoint — can run parallel with A/B.
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
