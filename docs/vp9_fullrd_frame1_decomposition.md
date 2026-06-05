# VP9 full-RD frame-1 block decomposition — the three remaining parity-gap seeds

This note is the libvpx **ground-truth block decomposition of frame 1** (the
first inter frame) for the three full-RD seeds still deferred in
`vp9LongFixtureParityGapSeeds` (`vp9_oracle_encoder_long_fixture_fuzz_test.go`):

| seed | config | cpu | path | partition_search_type | recode |
|------|--------|-----|------|-----------------------|--------|
| `{0,2,0,0,2}` | CBR 1200 kbps kf=999 realtime | 0 | full-RD | `SEARCH_PARTITION` (0) | no |
| `{0,1,1,0,1}` | CBR 700 kbps kf=30 realtime  | 4 | full-RD | `VAR_BASED_PARTITION` (3) | no |
| `{1,1,1,1,0}` | VBR 700 kbps kf=30 good-qual **(TWO-PASS)** | 8 | full-RD | `SEARCH_PARTITION` (0) | **no** |

All three are **confirmed full-RD** (`use_nonrd_pick_mode == 0`); none is
non-RD. The fixtures are 64x64 → a single 64x64 superblock per frame (SB0), an
8x8 MODE_INFO grid (mi 0..7 in each axis). Frame 0 (keyframe) is already
byte-exact for every seed; the first divergence is **frame 1**.

## How the ground truth was captured

A private libvpx v1.16.0 `vpxenc` was built in `$TMPDIR` from a **copy** of the
pre-configured oracle source tree
(`internal/coracle/build/libvpx-v1.16.0-vpxdec-vp9/`). A single env-gated
(`GOVPX_FULLRD_TRACE`) `fprintf` probe (`govpx_emit_block`) was added at the
**full-RD commit point** `encode_b` (`vp9/encoder/vp9_encodeframe.c:2226`,
right after `update_state` finalises `xd->mi[0]`) and at the non-RD commit
point `encode_b_rt` (`:2463`); a per-row dispatch line was added at
`vp9_encode_sb_row` (`:5494`, where `use_nonrd_pick_mode` selects
`encode_rd_sb_row` vs `encode_nonrd_sb_row`). The probe dumps, per committed
block, in encode order: mi position, `sb_type`, `mode`, `uv_mode`,
`ref_frame[0]/[1]`, `interp_filter`, `skip`, `tx_size`, `mv[0]/[1]`, and for
sub-8x8 every `bmi[i].as_mode` / `as_mv`.

**Soundness:** the two-frame IVF produced by the patched binary for `{0,2,0,0,2}`
is `md5 c41fc299791d7f2a04312f5e2d55eb3c`, **byte-identical** to the output of
the unmodified pinned `vpxenc-vp9` oracle (`md5 758eb78456b3a300de053d9217728dfc`,
unchanged throughout). The probe is therefore provably non-mutating and the
maps below are exactly what libvpx commits. The capture is deterministic across
repeated runs. Source frames = `vp9test.NewPanningSources(64,64,256)`; args =
the exact set `newVP9LongFixtureFuzzCase` emits per seed (incl. `--timebase=1/30`).

The `{0,2,0,0,2}` map is also embedded + self-validated as a regression anchor
in `vp9_oracle_fullrd_frame1_decomposition_test.go`
(`TestVP9FullRDFrame1DecompositionSeed0_2_0_0_2`).

## Field legend

- **bsize** (`BLOCK_SIZE`, `vp9/common/vp9_enums.h`): `0`=4x4 `1`=4x8 `2`=8x4 `3`=8x8.
  All committed leaves here are ≤ 8x8 (no 16x16/32x32/64x64 inter block survives).
- **mode** (`PREDICTION_MODE`, `vp9/common/vp9_blockd.h`): `0`=DC `1`=V `2`=H
  `3`=D45 `4`=D135 `5`=D117 `6`=D153 `7`=D207 `8`=D63 `9`=TM; `10`=NEARESTMV
  `11`=NEARMV `12`=ZEROMV `13`=NEWMV. For a sub-8x8 block, the block-level `mode`
  is the last sub-block's mode; the real per-sub modes are in `bmi[]`.
- **ref0/ref1** (`ref_frame[0]/[1]`): `0`=INTRA `1`=LAST `2`=GOLDEN `3`=ALTREF;
  `-1`=NONE. ref1≥1 ⇒ compound.
- **interp** (`interp_filter`, `vp9/common/vp9_filter.h`): `0`=EIGHTTAP
  `1`=EIGHTTAP_SMOOTH `2`=EIGHTTAP_SHARP; `3`=SWITCHABLE_FILTERS (left as the
  don't-care default on intra blocks).

---

## Seed `{0,2,0,0,2}` (cpu0, SEARCH_PARTITION) — frame 1 SB0

Dispatch: `frame=1 type=INTER use_nonrd=0 psearch=0 base_q=145`.
`rd_pick_partition` (`vp9_encodeframe.c:4288`) **split 64→32→16→8 across the whole
SB**; every leaf is an 8x8 NONE block **or a sub-8x8 SPLIT**. **56 of 64 leaves
are sub-8x8.** Single-ref LAST only; one intra block; no compound.

Per-block (encode order; sub-8x8 expanded with bmi y-modes):

```
mi(0,0) 8x8  NEWMV    LAST  eighttap  mv(9,15)
mi(0,1) 4x4  [bmi: NEAREST,NEAREST,NEWMV,NEAREST] LAST eighttap
mi(1,0) 8x4  INTRA DC [bmi y: V_PRED, DC_PRED]  uv=D63      <-- sub-8x8 INTRA in inter frame
mi(1,1) 4x8  NEWMV    LAST  smooth   [bmi: NEAREST, NEWMV]
mi(0,2) 4x4  NEARMV   LAST  eighttap [bmi: NEW,NEW,NEAREST,NEAR]
mi(0,3) 8x4  NEWMV    LAST  eighttap [bmi: NEW, NEW]
mi(1,2) 4x8  NEWMV    LAST  eighttap [bmi: NEW, NEW]
mi(1,3) 8x8  NEWMV    LAST  eighttap mv(0,8)
mi(2,0) 4x8  NEWMV    LAST  eighttap [bmi: NEAREST, NEW]
mi(2,1) 4x8  NEWMV    LAST  eighttap [bmi: NEW, NEW]
mi(3,0) 8x4  NEWMV    LAST  eighttap [bmi: NEW, NEW]
mi(3,1) 4x4  NEARMV   LAST  eighttap [bmi: NEW,NEAREST,NEW,NEAR]
mi(2,2) 4x8  NEWMV    LAST  eighttap [bmi: NEAR, NEW]
mi(2,3) 4x4  NEWMV    LAST  eighttap [bmi: NEW,NEW,NEW,NEW]
mi(3,2) 4x8  NEWMV    LAST  eighttap [bmi: NEW, NEW]
mi(3,3) 4x8  NEWMV    LAST  eighttap [bmi: NEW, NEW]
mi(0,4) 4x4  NEARMV   LAST  eighttap [bmi: NEAREST,NEAR,NEW,NEAR]
mi(0,5) 4x8  NEWMV    LAST  eighttap [bmi: NEW, NEW]
mi(1,4) 4x4  NEARESTMV LAST eighttap [bmi: NEW,NEW,NEW,NEAREST]
mi(1,5) 8x4  NEWMV    LAST  eighttap [bmi: NEW, NEW]
mi(0,6) 8x8  NEARESTMV LAST eighttap mv(8,8)
mi(0,7) 8x4  NEARESTMV LAST smooth   [bmi: NEW, NEAREST]
mi(1,6) 4x4  NEARESTMV LAST eighttap [bmi: NEW,NEAREST,NEW,NEAREST]
mi(1,7) 4x8  NEWMV    LAST  eighttap [bmi: NEW, NEW]
mi(2,4) 4x8  NEWMV    LAST  eighttap [bmi: NEAR, NEW]
mi(2,5) 4x4  NEWMV    LAST  smooth   [bmi: NEW,NEAR,NEAREST,NEW]
mi(3,4) 8x4  NEWMV    LAST  eighttap [bmi: NEAR, NEW]
mi(3,5) 4x8  NEWMV    LAST  smooth   [bmi: NEW, NEW]
mi(2,6) 4x8  NEWMV    LAST  smooth   [bmi: NEAREST, NEW]
mi(2,7) 8x4  NEARESTMV LAST eighttap [bmi: NEAREST, NEAREST]
mi(3,6) 4x4  NEWMV    LAST  eighttap [bmi: NEAR,NEAR,NEW,NEW]
mi(3,7) 8x8  NEWMV    LAST  eighttap mv(10,8)
mi(4,0) 8x4  NEWMV    LAST  eighttap [bmi: NEW, NEW]
mi(4,1) 8x4  NEWMV    LAST  eighttap [bmi: NEW, NEW]
mi(5,0) 8x4  NEWMV    LAST  eighttap [bmi: NEAREST, NEW]
mi(5,1) 8x4  NEWMV    LAST  smooth   [bmi: NEW, NEW]
mi(4,2) 4x4  NEWMV    LAST  sharp    [bmi: NEW,NEW,NEW,NEW]      <-- EIGHTTAP_SHARP
mi(4,3) 4x4  NEWMV    LAST  eighttap [bmi: NEW,NEW,NEW,NEW]
mi(5,2) 4x8  NEWMV    LAST  eighttap [bmi: NEW, NEW]
mi(5,3) 8x4  NEWMV    LAST  eighttap [bmi: NEW, NEW]
mi(6,0) 4x4  NEWMV    LAST  eighttap [bmi: NEW,NEW,NEW,NEW]
mi(6,1) 4x8  NEWMV    LAST  eighttap [bmi: NEW, NEW]
mi(7,0) 4x8  NEARMV   LAST  eighttap [bmi: NEW, NEAR]
mi(7,1) 8x4  NEWMV    LAST  eighttap [bmi: NEAREST, NEW]
mi(6,2) 8x4  NEARMV   LAST  eighttap [bmi: NEAREST, NEAR]
mi(6,3) 4x4  NEWMV    LAST  eighttap [bmi: NEW,NEW,NEW,NEW]
mi(7,2) 4x4  NEARMV   LAST  eighttap [bmi: NEW,NEAR,NEAREST,NEAR]
mi(7,3) 8x4  NEWMV    LAST  eighttap [bmi: NEAREST, NEW]
mi(4,4) 8x4  NEARMV   LAST  smooth   [bmi: NEAR, NEAR]
mi(4,5) 8x4  NEARESTMV LAST smooth   [bmi: NEW, NEAREST]
mi(5,4) 8x4  NEWMV    LAST  eighttap [bmi: NEAR, NEW]
mi(5,5) 4x4  NEWMV    LAST  smooth   [bmi: NEW,NEW,NEW,NEW]
mi(4,6) 4x4  NEWMV    LAST  eighttap [bmi: NEAREST,NEW,NEW,NEW]
mi(4,7) 4x4  NEWMV    LAST  eighttap [bmi: NEW,NEW,NEW,NEW]
mi(5,6) 8x8  NEWMV    LAST  smooth   mv(10,10)
mi(5,7) 4x4  NEWMV    LAST  eighttap [bmi: NEAREST,NEW,NEW,NEW]
mi(6,4) 8x8  NEWMV    LAST  eighttap mv(13,8)
mi(6,5) 8x8  NEWMV    LAST  eighttap mv(15,1)
mi(7,4) 4x8  NEWMV    LAST  smooth   [bmi: NEAREST, NEW]
mi(7,5) 4x4  NEWMV    LAST  eighttap [bmi: NEW,NEW,NEAR,NEW]
mi(6,6) 8x8  NEARMV   LAST  smooth   mv(15,1)
mi(6,7) 4x4  NEARMV   LAST  smooth   [bmi: NEAREST,NEW,NEW,NEAR]
mi(7,6) 8x4  NEWMV    LAST  eighttap [bmi: NEW, NEW]
mi(7,7) 4x8  NEARMV   LAST  eighttap [bmi: NEW, NEAR]
```

Histograms: bsize {4x4:20, 4x8:17, 8x4:19, 8x8:8}; top-level mode
{NEW:46, NEAR:10, NEAREST:7, intra:1}; ref0 {LAST:63, INTRA:1}; ref1 {NONE:64};
interp {eighttap:49, smooth:13, sharp:1, na/intra:1}.

### Distinct remaining code paths for `{0,2,0,0,2}` (build exactly these)

1. **`rd_pick_partition` recursive square SPLIT search** down to BLOCK_8X8
   (`vp9_encodeframe.c:2566`-onward; the 64→32→16→8 RD comparison, partition
   cost via `cpi->partition_cost`).
2. **Full-RD single-ref (LAST) inter mode/MV RD at 8x8**: NEWMV / NEARESTMV /
   NEARMV, i.e. `vp9_rd_pick_inter_mode_sb` + `single_motion_search`
   (`vp9_rdopt.c:2673`) + `full_pixel_diamond` (`vp9_mcomp.c:2487`) +
   `vp9_get_mvpred_var` variance re-scoring (`:1454`).
3. **Sub-8x8 SPLIT inter RD** with per-bmi NEW/NEAREST/NEAR MVs
   (`rd_pick_best_sub8x8_mode` / `vp9_rd_pick_inter_mode_sub8x8`); 56/64 leaves.
4. **Sub-8x8 INTRA in an inter frame**: mi(1,0) is 8x4 intra with per-sub y-modes
   (`bmi[0]=V_PRED`, `bmi[2]=DC_PRED`) and `uv_mode=D63`
   (`rd_pick_intra_sub_8x8_y_mode` reachable from the inter SB picker's intra leg).
5. **SWITCHABLE interp-filter RD** producing EIGHTTAP + EIGHTTAP_SMOOTH +
   EIGHTTAP_SHARP (the filter loop in the inter RD that feeds
   `get_interp_filter` @ `vp9_encodeframe.c:5779`).

**NOT needed:** compound prediction, GOLDEN/ALTREF refs, ZEROMV, ≥16x16 inter
blocks, VAR_BASED/FIXED partition.

---

## Seed `{0,1,1,0,1}` (cpu4, VAR_BASED_PARTITION) — frame 1 SB0

Dispatch: `frame=1 type=INTER use_nonrd=0 psearch=3 base_q=145`. cpu4 keeps
`use_nonrd_pick_mode==0` but selects `VAR_BASED_PARTITION`, so the path is
`choose_partitioning` → `rd_use_partition` (`vp9_encodeframe.c:2566`,
`encode_rd_sb_row` `:4259`-`4263`) — a full-RD mode/coef decision over a
variance-derived partition map. **`choose_partitioning` chose all-8x8** here:
every committed leaf is **BLOCK_8X8 NONE; zero sub-8x8.** Single-ref LAST only;
one whole-8x8 intra block (mi(7,2), DC); no compound.

Per-block (all 8x8 NONE):

```
(0,0)NEW (0,1)NEAREST (1,0)NEAREST (1,1)NEAREST (0,2)NEW (0,3)NEW (1,2)NEAR (1,3)NEW
(2,0)NEAREST (2,1)NEAREST (3,0)NEW (3,1)NEAR (2,2)NEW (2,3)NEW (3,2)NEAREST (3,3)NEAR
(0,4)NEAR (0,5)NEW (1,4)NEW (1,5)NEAR (0,6)NEAREST (0,7)NEW (1,6)NEW (1,7)NEAREST
(2,4)NEAREST (2,5)NEAREST (3,4)NEAREST (3,5)NEAREST (2,6)NEAREST (2,7)NEAREST (3,6)NEW (3,7)NEW
(4,0)NEW (4,1)NEAREST (5,0)NEW (5,1)NEAREST (4,2)NEW (4,3)NEW (5,2)NEAREST (5,3)NEW
(6,0)NEAREST (6,1)NEAREST (7,0)NEAREST (7,1)NEAR (6,2)NEAREST (6,3)NEAREST (7,2)INTRA-DC (7,3)NEAREST
(4,4)NEW (4,5)NEAREST (5,4)NEW (5,5)NEAR (4,6)NEW (4,7)NEW (5,6)NEW (5,7)NEW
(6,4)NEAR (6,5)NEAR (7,4)NEAREST (7,5)NEAREST (6,6)NEAR (6,7)NEW (7,6)NEAREST (7,7)NEAREST
```

Histograms: bsize {8x8:64}; mode {NEAREST:28, NEW:25, NEAR:10, intra:1};
ref0 {LAST:63, INTRA:1}; ref1 {NONE:64}; interp {eighttap:24, smooth:39, na:1}
(**no SHARP**).

### Distinct remaining code paths for `{0,1,1,0,1}`

1. **`choose_partitioning` (VAR_BASED) → `rd_use_partition`** glue: the variance
   tree from `choose_partitioning` (`vp9_encodeframe.c` var-based partition) fed
   into the full-RD per-block decision (`rd_use_partition`), **not** the
   RD-split search `rd_pick_partition`. This is the one path axis that differs
   from `{0,2,0,0,2}`.
2. **Full-RD single-ref (LAST) inter mode/MV RD at 8x8** (same engine as seed A,
   NEAREST/NEAR/NEW).
3. **Whole-8x8 intra DC in an inter frame** (mi(7,2)): the intra leg of the
   8x8 inter SB picker.
4. **SWITCHABLE interp-filter RD** producing EIGHTTAP + EIGHTTAP_SMOOTH (SHARP
   not selected here).

**NOT needed:** sub-8x8 (intra or inter), SPLIT partition search, compound,
GOLDEN/ALTREF, EIGHTTAP_SHARP, ≥16x16 inter blocks.

---

## Seed `{1,1,1,1,0}` (cpu8, good-quality, SEARCH_PARTITION) — frame 1 SB0

> **CORRECTION (2026-06-05, supersedes the rest of this section).** A direct
> `$TMPDIR` vpxenc capture (probe md5 == stock oracle, non-mutating) DISPROVED
> the recode premise below. Ground truth, authoritatively pinned in
> `vp9_oracle_recode_seed_1_1_1_1_0_test.go`: this seed is **TWO-PASS** VBR
> (`--good` overrides `--rt` → `passes==2`), there is **NO recode** (good speed
> sets `recode_loop==ALLOW_RECODE_KFMAXBW`, below the dummy-pack gate; 0 recodes
> across all 256 frames), committed **KF q=16, frame-1 q=39** (NOT 54/83), and
> the committed map has **ONE** intra block (mi(1,7) DC), not two — histograms
> (q=39): mode {NEW:46, NEAREST:13, NEAR:4, intra:1}, interp {eighttap:36,
> smooth:26, sharp:1}. The REAL blocker is **one-pass-vs-two-pass q selection**:
> the fuzz harness supplies no first-pass stats, so govpx runs one-pass VBR
> while libvpx runs two-pass, and q diverges at the **keyframe** (govpx 29 vs
> libvpx 16) → matched-prefix **0/256**. So `{1,1,1,1,0}` is blocked by the
> two-pass VBR q path, NOT the full-RD inter engine — defer it from the
> inter-engine campaign. The (incorrect) recode narrative is retained below
> only for history.

Dispatch shows frame 1 **twice**: `base_q=54` then `base_q=83`. Good-quality
runs a **recode loop** (`vp9_encoder.c` recode: encode at q=54 → reject →
re-encode at q=83). The committed bitstream is the **second pass (q=83)**; the
map below is that pass. `use_nonrd_pick_mode==0 psearch=0` (SEARCH_PARTITION).
`rd_pick_partition` converged to **all-8x8 NONE** (unlike seed A — no sub-8x8
survived). Single-ref LAST only; **two** whole-8x8 intra DC blocks; no compound.

Per-block (committed q=83 pass, all 8x8 NONE):

```
(0,0)NEW (0,1)NEW (1,0)NEW (1,1)NEW (0,2)NEW (0,3)NEW (1,2)NEAREST (1,3)NEW
(2,0)NEW (2,1)NEAREST (3,0)NEW (3,1)NEW (2,2)NEW (2,3)NEW (3,2)NEW (3,3)NEAREST
(0,4)NEW (0,5)NEW (1,4)NEW (1,5)NEW (0,6)NEW (0,7)NEW (1,6)NEW (1,7)INTRA-DC
(2,4)NEAREST (2,5)NEW (3,4)NEAREST (3,5)NEAREST (2,6)NEW (2,7)NEW (3,6)NEAR (3,7)NEW
(4,0)NEW (4,1)NEAREST (5,0)NEW (5,1)NEW (4,2)NEW (4,3)NEAREST (5,2)NEW (5,3)NEW
(6,0)NEW (6,1)NEAREST (7,0)NEW (7,1)NEW (6,2)NEAR (6,3)NEW (7,2)NEW (7,3)NEW
(4,4)INTRA-DC (4,5)NEAR (5,4)NEAREST (5,5)NEW (4,6)NEW (4,7)NEW (5,6)NEAR (5,7)NEW
(6,4)NEW (6,5)NEW (7,4)NEAR (7,5)NEW (6,6)NEAR (6,7)NEAR (7,6)NEW (7,7)NEW
```

Histograms: bsize {8x8:64}; mode {NEW:45, NEAREST:10, NEAR:7, intra:2};
ref0 {LAST:62, INTRA:2}; ref1 {NONE:64}; interp {eighttap:37, smooth:24,
sharp:1, na:2}. `tx_size` is TX_8X8 (1) on essentially every block.

### Distinct remaining code paths for `{1,1,1,1,0}`

1. **Good-quality recode loop** (`vp9_encoder.c` `recode_loop` q-search): the
   q=54→q=83 re-encode. Frame 1 is committed only after the recode converges, so
   the byte-exact target is the **final** pass; the q-loop control itself must be
   ported (this is the axis unique to this seed).
2. **`rd_pick_partition` square search converging to all-8x8 NONE** (same
   function as seed A, but here no SPLIT-to-sub-8x8 wins).
3. **Full-RD single-ref (LAST) inter mode/MV RD at 8x8** (NEAREST/NEAR/NEW).
4. **Whole-8x8 intra DC in an inter frame** ×2 (mi(1,7), mi(4,4)).
5. **SWITCHABLE interp-filter RD** producing EIGHTTAP + SMOOTH + SHARP.

**NOT needed:** sub-8x8, compound, GOLDEN/ALTREF, VAR_BASED partition, ≥16x16
inter blocks. (Note: good-quality uses the GoodQuality `SPEED_FEATURES`, but at
this fixture size it still reduces to the SEARCH_PARTITION + 8x8-NONE RD path.)

---

## Campaign roll-up — union of distinct paths

Ordered roughly by how many seeds each unblocks:

| code path | `{0,2,0,0,2}` | `{0,1,1,0,1}` | `{1,1,1,1,0}` |
|-----------|:---:|:---:|:---:|
| full-RD single-ref LAST inter 8x8 (NEAREST/NEAR/NEW) | ✅ | ✅ | ✅ |
| whole-8x8 intra (DC) in inter frame | — | ✅ (×1) | ✅ (×2) |
| SWITCHABLE interp RD (EIGHTTAP+SMOOTH) | ✅ | ✅ | ✅ |
| EIGHTTAP_SHARP selected | ✅ | — | ✅ |
| `rd_pick_partition` square RD split search | ✅ | — | ✅ |
| sub-8x8 SPLIT inter RD (per-bmi NEW/NEAREST/NEAR) | ✅ | — | — |
| sub-8x8 INTRA in inter frame (per-sub y-modes) | ✅ | — | — |
| `choose_partitioning`(VAR_BASED) → `rd_use_partition` | — | ✅ | — |
| good-quality recode (q) loop | — | — | ✅ |

**None of the three needs compound, GOLDEN/ALTREF, ZEROMV, or any inter block
larger than 8x8.** The shared spine is the **full-RD single-ref-LAST 8x8 inter
mode/MV/coef RD + SWITCHABLE interp-filter RD**; once that lands, the per-seed
deltas are: `{0,1,1,0,1}` adds the VAR_BASED→`rd_use_partition` driver,
`{1,1,1,1,0}` adds the recode loop (and SHARP + the RD square-split search),
and `{0,2,0,0,2}` is the hardest — it additionally needs the sub-8x8 inter
**and** sub-8x8 intra RD plus the deepest SPLIT search.

### Suggested build order

1. **`{0,1,1,0,1}` first** — smallest surface: all 8x8 NONE, VAR_BASED map
   (its partition is variance-driven, removing the RD-split search variable),
   single intra, no SHARP, no sub-8x8, no recode. Closing it validates the
   core single-ref-LAST 8x8 inter RD + interp RD spine in isolation.
2. **`{1,1,1,1,0}` next** — same 8x8-NONE leaf shape but exercises the RD
   square-split search (must converge to NONE), SHARP, and the good-quality
   recode loop.
3. **`{0,2,0,0,2}` last** — needs everything above plus the full sub-8x8 inter
   and sub-8x8 intra RD and the deepest SPLIT recursion.

## Capture-point citations (`vp9/encoder/vp9_encodeframe.c`, v1.16.0)

- `:5475` `vp9_encode_sb_row` / `:5494` the `use_nonrd_pick_mode` dispatch
  (full-RD ⇒ `encode_rd_sb_row`).
- `:4186` `encode_rd_sb_row` → `:4259-4263` VAR_BASED `rd_use_partition`
  branch (cpu4) / `:4288` `rd_pick_partition` branch (cpu0, cpu8-good).
- `:2566` `rd_pick_partition` (SEARCH_PARTITION RD split search).
- `:2253` `encode_sb` (partition walk) → `:2226` `encode_b` **commit point**
  (`update_state` finalises `xd->mi[0]`; probe emits here).
- `:5779` `get_interp_filter` (frame-level SWITCHABLE filter selection feeding
  the compressed-header prob deltas).
