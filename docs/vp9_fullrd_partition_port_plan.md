# VP9 full-RD inter partition port plan

Status: PLANNING. This document is the engineering plan for closing the last
VP9 encoder parity gap — the full-RD **inter** partition + mode engine. It does
**not** change encode behaviour. The full-RD **keyframe** path is byte-exact and
`make test-quality` (BD-rate) is green; nothing here may regress either.

All libvpx citations are against the pinned tree in this repo:
`internal/coracle/build/libvpx-v1.16.0/` (v1.16.0). govpx full-RD files live in
package `govpx` at the repository root (`vp9_*.go`).

Remaining parity seeds (cpu/partition path):
`{0,1,1,0,1}` cpu4, `{1,1,1,1,0}` good cpu8, `{0,2,0,0,2}` cpu0.

---

## 0. Executive summary

libvpx's full-RD encoder is a **single-pass depth-first** `rd_pick_partition`
recursion (`vp9/encoder/vp9_encodeframe.c:3667`). At each square block it
searches `PARTITION_NONE` first (`rd_pick_sb_modes`), then — crucially —
`store_pred_mv(x, ctx)` (`:3879`) snapshots the just-found per-ref motion
vectors into the node context, and each subsequent partition type
(`SPLIT` `:3898`, `HORZ` `:4037`, `VERT` `:4087`) calls `load_pred_mv(x, ctx)`
(`:2987`) to **re-seed `x->pred_mv[]` from the parent's NONE result before
recursing/searching the children**. The children's `single_motion_search`
(`vp9/encoder/vp9_rdopt.c:2599-2601`, `:2211-2218`, `:2247`) then consumes
`x->pred_mv[ref]` as candidate[2] of `vp9_mv_pred` and as the integer-MV seed
(gated on `sf->adaptive_motion_search`, set at speed >= 1 — `vp9_speed_features.c:298,497`).

govpx's full-RD **inter** partition is a different architecture:
`pickVP9InterPartitionBlockSize` (`vp9_encoder_inter_partition.go:10`) does a
**shallow-explore + leaf-decision-cache** — it scores `NONE` vs a *one-level*
`HORZ/VERT/SPLIT` peek (`scoreVP9InterPartitionPairShallow` `:1449`,
`scoreVP9InterPartitionSplitShallow` `:1487`), returns a single block size, and
the recursive `writeVP9ModesSb` walker (`vp9_encoder_mode_tree.go:216`) re-enters
per child. The per-(miRow,miCol,bsize) mode decision is memoised in
`vp9LeafInterDecisions` (`vp9_encoder_decision_cache.go:172-241`) so the count
prepass and the bitstream write pass agree. There is **no `x->pred_mv` thread**:
candidate[2] of `vp9_mv_pred` is read from the SB-level variance-partition
int-pro cache `vp9VarPartSBPredMv` (`vp9_encoder_inter_modes.go:2112`), not from a
parent NONE search.

**Why this blocks parity:** faithful candidate[2] propagation requires libvpx's
NONE-then-children **order** and the `store_pred_mv`/`load_pred_mv` save/restore
around it. govpx's shallow peek evaluates children from a *clean* state (no
parent NONE MV), and its leaf cache commits the first decision it computes for a
cell. Injecting `x->pred_mv` into the current shallow architecture pulls each
sub-block toward the 64x64's local motion minimum out of order, which regresses
the 5 planted-MV partition unit tests in
`vp9_encoder_inter_partition_scoring_test.go`
(`...Vert64x64...`, `...Vert32x32...`, `...Vert16x16...`, `...Horz64x64...`,
`...Splits64x64...`, all `CpuUsed:-3` → `PartitionSearchType=SearchPartition`).

**Plan:** introduce a libvpx-shaped depth-first `rd_pick_partition` recursion for
the inter path behind the existing speed-feature gate, in four byte-safe steps:
(a) recursion skeleton that *reproduces today's decisions* (no behaviour change),
(b) thread `x->pred_mv[]` with `store_pred_mv`/`load_pred_mv` semantics,
(c) enable candidate[2] = `x->pred_mv[ref]` propagation,
(d) converge the per-block mode/ref/filter/tx/coef RD loop.
Each step is gated on full oracle byte-parity + cpu0/cpu4 keyframe parity +
`make test-quality` BD-rate unchanged, with an explicit rollback criterion.

**First PR boundary:** step (a) only — the no-op recursion skeleton plus its
parity proof. It must be byte-identical on every gate before any pred_mv work
lands.

---

## 1. Architecture diff

### 1.1 libvpx `rd_pick_partition` (the reference)

Entry per SB row: `encode_rd_sb_row` (`vp9_encodeframe.c:4166`). Before each
64x64 SB it resets `x->pred_mv[i] = {INT16_MAX, INT16_MAX}` for all refs
(`:4215-4218`) and calls `rd_pick_partition(..., BLOCK_64X64, ..., td->pc_root)`
(`:4268`).

`rd_pick_partition` (`:3667-4164`), per square `bsize`:

1. **State + gating** (`:3677-3783`): `mi_step`, `pl =
   partition_plane_context`, `do_split = bsize>=BLOCK_8X8`, `do_rect=1`,
   `force_horz_split`/`force_vert_split` from frame edges, `must_split` from
   sub-block energy (`:3751-3756`), and `partition_{none,horz,vert}_allowed`
   clamped by `auto_min_max_partition_size` (`:3760-3767`) and
   `use_square_partition_only` (`:3769-3781`).
2. **`save_context`** (`:3783`): snapshots entropy contexts `a/l` and partition
   contexts `sa/sl` for the block; every candidate restores from these.
3. **PARTITION_NONE** (`:3811-3876`): `rd_pick_sb_modes(... ctx ...)` searches
   the whole block. Adds `cpi->partition_cost[pl][PARTITION_NONE]` (`:3826`,
   unconditional full-tree cost), updates `best_rdc`, may set
   `do_split/do_rect=0` on RD breakout. `restore_context` (`:3872`).
   - **else branch** (`:3873-3876`): when NONE not allowed,
     `vp9_zero(ctx->pred_mv)` + `interp_filter = EIGHTTAP`.
4. **`store_pred_mv(x, ctx)`** (`:3879`): `memcpy(ctx->pred_mv, x->pred_mv)` —
   stash the per-ref MVs the NONE search left in `x->pred_mv[]`.
5. **PARTITION_SPLIT** (`:3889-3996`): `load_pred_mv(x, ctx)` (`:3898`) then
   recurse `rd_pick_partition` on 4 children (`:3946`) accumulating `sum_rdc`;
   bails early when `sum_rdc.rdcost >= best_rdc.rdcost` unless `must_split`
   (`:3918`). Adds `partition_cost[pl][PARTITION_SPLIT]` (`:3969`).
   `restore_context` (`:3995`).
6. **PARTITION_HORZ** (`:4032-4080`): `load_pred_mv(x, ctx)` (`:4037`),
   `rd_pick_sb_modes` top half, `update_state`+`encode_superblock` to lay the top
   half's recon as context, `rd_pick_sb_modes` bottom half (`:4057`). Adds
   `partition_cost[pl][PARTITION_HORZ]`. `restore_context` (`:4079`).
7. **PARTITION_VERT** (`:4082-4126`): mirror of HORZ with `load_pred_mv` (`:4087`).
8. **64x64 INT64_MAX fallback** (`:4128-4139`).
9. **Commit** (`:4143-4153`): if `should_encode_sb && pc_tree->index != 3`,
   `encode_sb(... pc_tree ...)` walks the chosen `pc_tree->partitioning` and
   emits the block. Returns `should_encode_sb`.

The carried state that makes candidate[2] correct:

- **`x->pred_mv[MAX_REF_FRAMES]`** — per-ref last NEWMV result. Written in
  `single_motion_search` (`vp9_rdopt.c:2247`, `joint`/sub8x8 at `:2738`,
  `:2247`), seeded back at `:2211-2218`, reset to INT16_MAX on intra
  (`:2645-2646`). `store_pred_mv`/`load_pred_mv` save/restore it around the
  NONE→{SPLIT,HORZ,VERT} fan-out.
- **`x->mbmi_ext->ref_mvs[ref][0..1]`** — the NEAREST/NEAR candidate list;
  `pred_mv[0]`,`pred_mv[1]` of `vp9_mv_pred` (`vp9_rdopt.c:2599-2600`). `x->mbmi_ext`
  is re-based per (mi_row,mi_col) at `set_offsets` (`vp9_encodeframe.c:237`) and
  saved/restored per PICK_MODE_CONTEXT (`:1795`, `:2409`, `:4470`...).
- **`x->pred_mv[ref]`** — `pred_mv[2]` of `vp9_mv_pred` (`vp9_rdopt.c:2601`).
- **`vp9_mv_pred` outputs** (`vp9/encoder/vp9_rd.c:636-638`):
  `x->mv_best_ref_index[ref]` (integer-MV seed index, consumed `vp9_rdopt.c:2576`),
  `x->max_mv_context[ref]`, `x->pred_mv_sad[ref]` (ref-mask gate
  `vp9_pickmode.c`/`vp9_rdopt.c` ref pruning).
- **PICK_MODE_CONTEXT (`pc_tree->{none,horizontal[2],vertical[2],split[4],
  leaf_split[0]}`)** — per-node save/restore of `mbmi`, `mbmi_ext`, `pred_mv`,
  recon, entropy ctx. The substrate `store_pred_mv`/`load_pred_mv` write into.

### 1.2 govpx full-RD inter partition (current)

Driver: `writeVP9ModesSb` (`vp9_encoder_mode_tree.go:216`) is itself the recursive
partition walker. Per region it calls `pickVP9BlockSizeForRegion`
(`:323`) → for inter, `pickVP9InterPartitionBlockSize`
(`vp9_encoder_inter_partition.go:10`), writes the partition token, then recurses
into children (`:256-283`). The decision is computed in the **count prepass**
(`key.counts!=nil` / equivalent inter flag) and replayed in the **write pass**.

Key govpx functions/types:

| govpx symbol (file:line) | role | libvpx analogue |
|---|---|---|
| `pickVP9InterPartitionBlockSize` (`vp9_encoder_inter_partition.go:10`) | top dispatcher: FIXED / ML / REFERENCE / VAR / **shallow-RD** partition selection; returns one block size | the partition-type selection inside `rd_pick_partition` (but **shallow**) |
| `scoreVP9InterPartitionPairShallow` (`:1449`) | one-level HORZ/VERT peek: pick mode for the 1–2 children, sum `score`, restore mi-rect | `rd_pick_sb_modes` ×2 for HORZ/VERT, **without** depth recursion |
| `scoreVP9InterPartitionSplitShallow` (`:1487`) | one-level SPLIT peek: pick mode for 4 quadrant leaves at `splitSize`, sum `score` | the SPLIT arm, **without** recursing `rd_pick_partition` |
| `pickVP9InterPartitionRD` (`:1287`) | a *separate*, genuinely recursive NONE/SPLIT/HORZ/VERT RD search with full save/restore (used by `scoreVP9InterPartitionSplit`'s deep arm) | closest existing analogue to `rd_pick_partition`, but **not** the path the walker commits, and **no `pred_mv` thread** |
| `scoreVP9InterPartition{None,Rect,Split}` (`:1176/:1200/:1238`) | leaf/recursive RD scorers; add unconditional `RDPartitionCost` | `:3826/:4035/:4085/:3969` cost adds |
| `pickVP9InterReferenceMode` (`vp9_encoder_inter_modes.go:574`) | the per-block mode/ref/filter/MV RD decision | `rd_pick_sb_modes` → `vp9_rd_pick_inter_mode_sb` |
| `vp9InterMvPredStateForRef` (`vp9_encoder_inter_modes.go:2077`) | builds `vp9_mv_pred` candidate triple + runs `MvPredScanCandidates` | `vp9_mv_pred` (`vp9_rd.c:588-639`) |
| `vp9VarPartSBPredMv` (`vp9_encoder_inter_partition.go:344`) | **SB-level** int-pro MV cache used as candidate[2] | a *substitute* for `x->pred_mv[ref]` (only valid on the VAR_BASED path) |
| `vp9LeafInterDecisions` + `lookup/store` (`vp9_encoder_decision_cache.go:172-241`) | per-(miRow,miCol,bsize) mode decision memo (prepass→write) | the committed `mi_grid`/PICK_MODE_CONTEXT replay |
| `vp9KeyframePartitionDecisions` (`:27-87`) | per-(miRow,miCol,root) keyframe partition memo | committed partition replay |
| `saveVP9PartitionReconSnapshot` / `restore*` / `release*` (`:1649/:1712/:1733`) | stacked recon scratch save/restore (LIFO) | `save_context`/`restore_context` recon half |
| `snapshotVP9PartitionContexts` / `restore*` (`:1598/:1625`) | above/left seg-ctx save/restore | `sa/sl` half of `save_context`/`restore_context` |
| `snapshotVP9MiRect` / `restoreVP9MiRect` (`:1558/:1577`) | mi-grid rect save/restore | per-node `mbmi` save/restore |
| `RDPartitionCost` (`vp9_fullrd_partition_cost.go:53`) | unconditional full-tree partition rate | `cpi->partition_cost[pl][type]` (`vp9_rd.c:430-432`) |
| `VP9RDCost` / `vp9RDPartitionBetter` (`vp9_fullrd_partition_cost.go:77/:89`) | RDCOST macro + strict-less tie-break | `RDCOST` (`vp9_rd.h:29-30`), `:3829/:3973` |

The **keyframe** RD partition is already libvpx-shaped: `pickVP9KeyframeRDPartitionBlockSize`
(`vp9_encoder_key_partition.go:116`) → `scoreVP9KeyframeRDPartitionTree`
(`:167`) is a depth-first NONE/SPLIT/HORZ/VERT recursion with the same
save/restore discipline and the `auto_min_max` / `use_square_partition_only`
gating ported (`:190-202`). **This is the template for the inter skeleton** —
the inter version is the keyframe tree plus the `pred_mv` thread and the inter
mode loop.

### 1.3 The structural mismatch in one line

libvpx: `NONE → store_pred_mv → (load_pred_mv → recurse)×{SPLIT,HORZ,VERT}` in a
single depth-first pass that *carries* `x->pred_mv[]`.
govpx inter: `pick one size by a shallow peek → commit → walker recurses fresh`,
candidate[2] sourced from an SB cache, mode decision memoised per cell.

---

## 2. Why the shallow/cache architecture cannot carry `x->pred_mv`

1. **Order is load-bearing.** candidate[2] (`x->pred_mv[ref]`) is only meaningful
   *after* the parent's PARTITION_NONE `single_motion_search` has written it
   (`vp9_rdopt.c:2247`) and `load_pred_mv` has re-seeded it (`:3898/:4037/:4087`).
   govpx's `scoreVP9InterPartition*Shallow` evaluate children from a *restored,
   parent-NONE-free* mi/recon/ctx state (`snapshotVP9MiRect`+restore around each
   peek, `vp9_encoder_inter_partition.go:1459-1484`). There is no point in that
   flow where the parent's NONE MV is live in a per-ref slot the children read.
   Today candidate[2] comes from `vp9VarPartSBPredMv` — an SB-wide int-pro MV
   that is (a) only populated on the VAR_BASED choose_partitioning path
   (`:809-825`), and (b) the *same* value for every child, so it cannot encode
   "the NONE search at *this* node found MV X."

2. **The leaf cache freezes the first decision.** `vp9LeafInterDecisions`
   memoises `pickVP9InterReferenceMode(miRow,miCol,bsize)` so the write pass
   replays the prepass. If candidate[2] were injected, the *value* injected
   would depend on which partition arm is being scored (NONE vs SPLIT-child vs
   HORZ-child) — but the cache key is only `(miRow,miCol,bsize)`. The first arm
   to populate the cell wins; later arms (and the write pass) read a decision
   computed under the wrong `pred_mv`. libvpx avoids this because each
   PICK_MODE_CONTEXT (`pc_tree->none`, `->horizontal[i]`, `->split[i]`) is a
   *distinct* slot, and `load_pred_mv` resets `x->pred_mv[]` from the parent
   `ctx` before *each* arm.

3. **Shallow ≠ depth-first RD.** The committed size from
   `pickVP9InterPartitionBlockSize` is chosen by a one-level peek
   (`scoreVP9InterPartition*Shallow` do **not** recurse `rd_pick_partition`;
   `scoreVP9InterPartitionSplitShallow:1510-1526` sums leaf scores at `splitSize`
   directly). libvpx's SPLIT arm recurses to the bottom (`:3946`), so the SPLIT
   RD it compares against NONE already reflects the children's *own*
   NONE/HORZ/VERT/SPLIT sub-decisions seeded by the carried `pred_mv`. The
   shallow peek can never produce the same `sum_rdc`.

State that must thread through a faithful recursion (and has no home in the
current architecture):

- `x->pred_mv[ref]` per node, with `store_pred_mv`/`load_pred_mv` save/restore.
- `x->mbmi_ext->ref_mvs[ref][0..1]` re-based per (mi_row,mi_col).
- `x->pred_mv_sad[ref]`, `x->mv_best_ref_index[ref]`, `x->max_mv_context[ref]`
  (the `vp9_mv_pred` outputs) — currently returned by value from
  `vp9InterMvPredStateForRef` and dropped after one block.
- Per-node PICK_MODE_CONTEXT save/restore of the above + recon + entropy ctx
  (govpx has the recon/ctx/mi halves; it lacks the per-node `pred_mv` slot).

---

## 3. Incremental port plan (byte-safe at every step)

Design rule: the new recursion lives behind the **existing** gate
`PartitionSearchType == SearchPartition` (`vp9_speed_features_types.go:180`;
set for good cpu0-? and the `CpuUsed:-3` test path). The default speed-8 path is
VAR_BASED and is untouched. The keyframe path is untouched. Each step is a
separate PR; none merges to main without all three gates green.

Validation gates referenced below (exact commands):

- **G-bp** full oracle byte-parity: `make test-byte-parity`
  (`BYTE_PARITY_TESTS`, Makefile:287; includes the VP9 keyframe byte-parity
  cases `Checker320KeyframeByteParity`, `Stepped320FixedQuantizerKeyframeByteParity`,
  `CBRKeyframeByteParity`, `CBRCyclicRefreshKeyframeByteParity`).
- **G-kf** cpu0/cpu4 keyframe parity: the VP9 vpxenc keyframe oracle suite
  (`vp9_oracle_encoder_vpxenc_keyframe*_test.go`,
  `vp9_speed_features_good_cpu_used_0_test.go`,
  `vp9_speed_features_rt_cpu_used_0_4_test.go`).
- **G-bd** BD-rate: `make test-quality` (Makefile:124; quality-gate + the
  `TestVP9BDRate(ARNR|PerceptualAQ|CyclicRefresh|LoopFilter)` lane), must be
  unchanged.
- **G-unit** the 5 planted-MV partition tests
  (`vp9_encoder_inter_partition_scoring_test.go`) + `go test -short .`.
- **G-fmt** `go fmt ./...` + `make pgo-check` + `go vet ./...` (pre-merge).

Note: oracle binaries are referenced via `GOVPX_VPXENC_VP9_BIN` /
`GOVPX_VPXDEC_VP9_BIN` (= `internal/coracle/build/vpxenc-vp9` / `vpxdec-vp9` at
the real repo root); G-bp/G-bd build libvpx + fetch test data.

### Step (a) — depth-first recursion skeleton, provable NO-OP

Goal: stand up `rdPickVP9InterPartition` (new, in a new file
`vp9_encoder_inter_partition_rd.go`) shaped exactly like
`scoreVP9KeyframeRDPartitionTree` (`vp9_encoder_key_partition.go:167`) but for the
inter mode loop, and have `pickVP9InterPartitionBlockSize` delegate to it **only
in a mode where it reproduces the current committed size**. Concretely:

- Introduce the function and a per-node `vp9InterPartitionRDNode` carrying the
  fields a faithful recursion needs (`predMv [MaxRefFrames]vp9dec.MV`,
  `partitioning`, child RD) — **but do not read `predMv` yet** (steps b/c).
- Wire it so the *returned block size* and the *committed mode decisions* are
  byte-identical to today. Two safe ways to guarantee no-op:
  1. **Compute-and-assert (preferred for the proof):** keep
     `pickVP9InterPartitionBlockSize` authoritative; call the new recursion in a
     shadow capacity behind a build-tag/elided debug flag that compares its
     committed size against the shallow result and is compiled out of normal
     builds (per MEMORY: no probes in hot path — use compile-elided, like
     `recordVP9FullRDFirstInterMv` in `vp9_encoder_fullrd_motion.go:88`). This
     lets us validate the recursion reproduces decisions on real fixtures with
     zero bitstream change.
  2. **Structural delegation:** make `pickVP9InterPartitionBlockSize`'s
     shallow-RD tail (`vp9_encoder_inter_partition.go:198-248`) call the new
     recursion *configured to skip the pred_mv thread and use the same shallow
     child scoring*, i.e. the recursion's NONE/HORZ/VERT/SPLIT arms call the
     existing `scoreVP9InterPartition*Shallow`/`scoreVP9InterPartitionNone` and
     pick by the same `vp9AddModeDecisionRate(... RDPartitionCost ...)`
     comparison already at `:209-243`. This is a pure refactor: same scoring,
     same tie-break (`vp9RDPartitionBetter`), same restore discipline.

- libvpx ref: `rd_pick_partition` skeleton `:3667-4164`; keyframe template
  `scoreVP9KeyframeRDPartitionTree`.
- govpx files touched: new `vp9_encoder_inter_partition_rd.go`; minimal hook in
  `vp9_encoder_inter_partition.go`; possibly the compile-elided shadow in the
  existing trace-flag file.
- Validation gate: **G-bp + G-kf + G-bd ALL byte/score-identical** + G-unit
  (5 planted-MV tests still pass) + G-fmt.
- Rollback criterion: any byte difference on G-bp, any G-kf failure, any G-bd
  metric delta, or any of the 5 planted-MV tests flips → revert the hook, keep
  the (dead) recursion file out of the commit. **Do not merge an unproven
  skeleton.**

### Step (b) — thread `x->pred_mv[]` (still no candidate[2] consumption)

Goal: add the `store_pred_mv`/`load_pred_mv` save/restore around the
NONE→{SPLIT,HORZ,VERT} arms so the recursion *carries* a per-node
`predMv[MaxRefFrames]`, populated from each block's chosen NEWMV
(`pickVP9InterReferenceMode` → the subpel result, mirroring `vp9_rdopt.c:2247`),
and reset to the INT16_MAX sentinel at SB entry (`vp9_encodeframe.c:4215-4218`)
and on intra (`:2645-2646`). **Consumers stay disabled** —
`vp9InterMvPredStateForRef` still ignores it; this step only proves the plumbing
is inert.

- libvpx ref: `store_pred_mv`/`load_pred_mv` `:2983-2989`; reset `:4215-4218`;
  writeback `vp9_rdopt.c:2247`,`:2738`; intra reset `:2645-2646`.
- govpx files: `vp9_encoder_inter_partition_rd.go` (node save/restore),
  `vp9_encoder_inter_modes.go` (surface the chosen NEWMV into the node).
- Validation gate: G-bp + G-kf + G-bd unchanged (the thread is write-only here),
  G-unit, G-fmt.
- Rollback criterion: any G-bp/G-kf byte diff or G-bd delta (would mean the
  thread is being read somewhere) → revert.

### Step (c) — enable candidate[2] = `x->pred_mv[ref]` propagation

Goal: switch `vp9InterMvPredStateForRef` candidate[2]
(`vp9_encoder_inter_modes.go:2112`) from `vp9VarPartSBPredMv` to the recursion's
threaded `predMv[ref]` **on the SearchPartition path only**, with the
`adaptive_motion_search` gate (`single_motion_search` seeding is gated on it,
`vp9_rdopt.c:2211`; govpx `e.sf.AdaptiveMotionSearch`,
`vp9_encoder_inter_modes.go:1154`). Honour `num_mv_refs = MAX_MV_REF_CANDIDATES +
(block_size < max_partition_size)` exactly (`vp9_rd.c:599-601`; govpx
`MvPredNumCandidates` `mv_pred.go:247`). Also wire the integer-MV seed path
(`mv_best_ref_index`, `vp9_rdopt.c:2576`; the `x->pred_mv` `>>3` seed
`:2214-2215`).

This is the step that **moves bytes**. Expect — and require — that the regressed
planted-MV behaviour is now *correct by libvpx order* (the 5 unit tests assert
the libvpx-correct split/MV outcome; if a faithful recursion changes any
assertion, re-derive the expected value from a libvpx trace rather than
hand-tuning — per MEMORY "port libvpx, no heuristics").

- libvpx ref: `vp9_mv_pred` `vp9_rd.c:588-639`; seed `vp9_rdopt.c:2211-2218`;
  candidate triple `:2599-2601`; output consume `:2576`.
- govpx files: `vp9_encoder_inter_modes.go` (candidate[2] source + gate),
  `vp9_encoder_inter_partition_rd.go` (expose threaded predMv to the mode loop).
- Validation gate: G-kf + G-bd unchanged (keyframe/BD-rate must not move —
  candidate[2] is inter-only); G-bp must stay green on keyframe cases; **G-cmp**
  new oracle delta on the 3 parity seeds (`{0,1,1,0,1}` cpu4, `{1,1,1,1,0}` cpu8,
  `{0,2,0,0,2}` cpu0) must *improve toward* libvpx; G-unit re-baselined from
  libvpx trace; G-fmt.
- Rollback criterion: any G-kf/G-bd regression, or the 3 seeds do not move
  toward parity, or a planted-MV expected value cannot be confirmed from a
  libvpx trace → revert candidate[2] source, keep the thread (step b) in place.

### Step (d) — per-block mode/ref/filter/tx/coef RD loop convergence

Goal: with the partition order + pred_mv correct, close the residual mode-loop
deltas: NEWMV `vp9_NEWMV_diff_bias` gate (`mv_pred.go:290`, `vp9_pickmode.c:1309`),
ref-frame masking via `pred_mv_sad` (`vp9_rdopt.c` ref pruning;
`vp9_encoder_inter_modes.go:1145-1197`), interp-filter selection,
`choose_tx_size_from_rd` (`fullrd_txsize.go`), `cost_coeffs`. Drive each sub-delta
to byte parity per seed.

- libvpx ref: `vp9_rd_pick_inter_mode_sb` (`vp9_rdopt.c`), `single_motion_search`
  `:2560-2740`, `vp9_NEWMV_diff_bias` `vp9_pickmode.c:1309-1372`.
- govpx files: `vp9_encoder_inter_modes.go`, `vp9_encoder_mode_block.go`,
  `fullrd_txsize.go`, coeff-cost helpers.
- Validation gate: per-seed oracle byte parity (target: all 3 seeds byte-exact),
  G-kf + G-bd unchanged, G-unit, G-fmt.
- Rollback criterion: any seed regresses vs the step-(c) baseline, or G-kf/G-bd
  moves → revert the offending sub-delta.

---

## 4. Risk / effort and first PR boundary

**Effort (rough):** (a) 1 PR, ~1–2 days incl. proof runs. (b) 1 PR, ~1 day.
(c) 1–2 PRs, multi-day (this is where parity is won/lost; re-baselining unit
tests from libvpx traces dominates). (d) several PRs, the long tail — likely the
bulk of remaining calendar time, mirroring the keyframe convergence history.

**Risk register:**

- *High:* step (c) re-baselines the 5 planted-MV unit tests. Mitigation:
  derive every new expected value from a libvpx trace; never hand-tune (MEMORY:
  port libvpx, no heuristics; see `project_vp9_fullrd_inter_bisection.md` for the
  bisection discipline — resume at frontier, don't restart).
- *High:* candidate[2] is inter-only but shares `vp9InterMvPredStateForRef`,
  `MvPredScanCandidates`, and `RDPartitionCost` with code paths that also feed
  keyframe/VAR_BASED. Mitigation: gate strictly on
  `PartitionSearchType==SearchPartition` **and** `AdaptiveMotionSearch!=0`;
  G-kf/G-bd are blocking on every step.
- *Medium:* the leaf-decision cache (`vp9LeafInterDecisions`) keyed on
  `(miRow,miCol,bsize)` is incompatible with per-arm `pred_mv`. The recursion
  must either bypass the cache on the SearchPartition path or key it on the
  partition context. Decide in step (a); validate the prepass/write-pass still
  agree byte-for-byte (G-bp keyframe cases exercise the two-pass replay).
- *Medium:* `must_split` / sub-block-energy gating (`:3751-3756`) and
  `auto_min_max`/`use_square_partition_only` clamps must match the keyframe tree
  port (`vp9_encoder_key_partition.go:190-202`) exactly, or NONE-vs-SPLIT flips.
- *Low:* recon/ctx/mi save-restore is already battle-tested
  (`saveVP9PartitionReconSnapshot` LIFO is unit-tested in
  `TestVP9PartitionReconSnapshotStacksNestedSaves`).

**First PR boundary: step (a) only** — the no-op depth-first inter recursion
skeleton plus its byte-parity proof. Acceptance: G-bp + G-kf + G-bd
byte/score-identical to pre-PR `main`, all 5 planted-MV tests pass, G-fmt clean.
No `x->pred_mv` consumption ships in this PR. Steps (b)/(c)/(d) follow only after
(a) is proven green.
