# VP9 keyframe RD-partition search context: thread vs restore

Root-caused divergence behind the `{0,1,1,0,1}` (CBR 700, kf=30, realtime
**cpu4**, one-pass) deep-engine byte-parity gap. The deep stack
(`TestVP9FullRDUsePartitionSeed0_1_1_0_1Frame1ByteParity`) reproduces frames
**0..29 byte-identically** to the libvpx v1.16.0 vpxenc-vp9 oracle. **Frame 30**
(the second keyframe) is the first divergence: govpx over-splits the 16x16 at
mi(0,2), committing `BLOCK_8X4` (PARTITION_SPLIT) where libvpx keeps
`BLOCK_16X16` (PARTITION_NONE). Both DC_PRED.

## The bug

The keyframe RD partition is decided by a **per-level dispatch**
(`pickVP9KeyframeRDPartitionBlockSize`, vp9_encoder_key_partition.go) that walks
the SB top-down and **commits** each block's reconstruction + entropy context
before deciding the next sibling. So when it searches the partition for the
16x16 at mi(0,2), the left coeff entropy context already carries the **committed
mi(0,1) coeffs** (`left = [1,1,1,1]`).

libvpx's `rd_pick_partition` (vp9/encoder/vp9_encodeframe.c) does **not** work
that way. It searches the whole tree with `save_context`/`restore_context` and
defers the real `encode_sb` until after the full recursive search. Each child's
`rd_pick_partition` save/restores its own context, so the SPLIT loop
(vp9_encodeframe.c:3917-3966) does **not** thread sibling coeffs: when it
searches the 16x16 at mi(0,2), the just-searched mi(0,0)/(0,1) coeffs have been
**restored away** (`left = [0,0,0,0]`).

A nonzero left coeff-context predicts the (real, nonzero) residual better, so it
makes the four SPLIT sub-blocks **~8000 rate units each cheaper** — enough to
flip SPLIT from losing to winning at this block. For frames 0..29 the NONE/SPLIT
margin is wide enough that the thread-vs-restore difference does not change the
decision; at frame-30 mi(0,2) it does.

## Proof (libvpx ground truth)

Instrumenting libvpx's keyframe `rd_pick_partition` at frame-30 mi(0,2)
(`cm->current_video_frame == 30 && bsize == BLOCK_16X16 && mi_row == 0 &&
mi_col == 2`) and capturing the `{0,1,1,0,1}` cpu4 encode:

```
NONE  rate=680711 dist=28476 rdcost=24063599 pl=6
SPLIT sumrate=531997 sumdist=26915 rdcost=INT64_MAX splitcost=533 pl=6 i=3
```

The `i=3` is decisive: the SPLIT loop's best-RD continuation guard
(`sum_rdc.rdcost < best_rdc.rdcost`) bailed after **3 of 4** children because the
partial SPLIT cost already exceeded NONE — so the `i == 4` gate fails and SPLIT
is discarded; NONE wins.

Matching these against govpx's two context variants for the same block:

| value            | libvpx | govpx per-level (left=1) | govpx recursion (left=0) |
|------------------|--------|--------------------------|--------------------------|
| NONE rate        | 680711 | 673438                   | **680333**               |
| SPLIT 3-child rate | 531997 | 507382                   | **533087**               |

libvpx tracks the **restored (left=0)** variant within ~0.1-0.2%, **not** the
threaded (left=1) per-level dispatch. So libvpx's partition *search* uses the
restored context; the per-level dispatch's threaded context is the bug.

Note this is the *search* context only. The eventual bitstream *write* does use
the real threaded neighbor coeffs (which is why govpx's committed frames 0..29
are byte-exact) — libvpx's deferred `encode_sb` writes with the real context
too. The divergence is purely in which context the partition RD *decision* sees.

## Why it is not a one-line fix

govpx's per-level dispatch commits coeffs as it walks (to thread the context for
later blocks and to drive the count pre-pass), so the committed sibling coeffs
are already in the entropy context by the time the next block's partition is
searched. Matching libvpx requires the keyframe partition **search** to score
against a context with the sibling coeffs **restored** to the parent-SPLIT
entry, while the **write** keeps the real threaded context — the SEARCH→WRITE
entropy-context discipline, but applied to the keyframe partition picker.

The recursion (`scoreVP9KeyframeRDPartitionTree`'s save/restore) already produces
the libvpx-faithful left=0 numbers, so the machinery exists; the open work is
routing the authoritative per-block decision through a restored-context search
without regressing the byte-exact frames 0..29 (all of which flow through the
same per-level dispatch). A prior naive attempt — making the count pre-pass
replay the SB-root recursion's cached decision — regressed frame 0 (20 mode
diffs) and frame 1; the recursion's *blanket* restore is not uniformly correct
across every block, so the fix must restore the search context per node the way
libvpx does, not swap decision sources wholesale.

## Rework path

1. In the keyframe partition **search** (the `apply=false`/scoring path of
   `scoreVP9KeyframeRDPartitionTree` and `scoreVP9KeyframeRDPartitionSplit`),
   restore the left/above coeff entropy context to the parent-SPLIT entry so the
   sub-block RD is scored against `left=0`, matching libvpx.
2. Keep the **write**/commit path on the real threaded neighbor context (frames
   0..29 stay byte-exact).
3. Re-validate `TestVP9FullRDUsePartitionSeed0_1_1_0_1Frame1ByteParity` — the
   matched-frame prefix should advance past 30.

This is a deliberate context-discipline change; do it as a unit and gate it on
the full 0..29 byte-parity prefix, not piecemeal.
