# VP9 cpu0-3 inter coefficient selector: FP vs B quantizer

Root-caused selector bug behind the `{0,2,0,0,2}` (CBR 1200, kf=999, realtime
**cpu0**, one-pass q=145) deep-engine byte-parity gap. The selector bug is
closed as of 2026-06-13: `prepareVP9InterTxResidueWithQ` now follows
`sf.UseQuantFp`, using the zbin "B" quantizer for cpu0-3 and FP only when
libvpx sets `x->quant_fp`.

The larger seed remains a full-RD byte-parity frontier, but the quantizer
selector itself is pinned by `TestVP9InterTxResidueUsesQuantFpSpeedFeature`.
The deep committed-mode probe
`TestVP9FullRDInterNextDivergenceSeed0_2_0_0_2` stayed green after the selector
port and reports the full frame-1 SB0 walk matching libvpx's committed leaves.

## The Closed Bug

`prepareVP9InterTxResidueWithQ` (vp9_encoder_prediction.go) — the committed-inter
residual write, which also feeds the tx-candidate score and the deep-search
entropy-context stamp `stampVP9InterLeafTxContext` — used to hardcode the **FP
quantizer** (`useFastQuant=true`, the round-only quantizer with no zbin).

libvpx's `encode_block` (vp9/encoder/vp9_encodemb.c:590-625) selects the
quantizer on `x->quant_fp`:

* `x->quant_fp` set → `vp9_xform_quant_fp` (FP).
* else → `vp9_xform_quant` (the zbin-gated "b" quantizer).

`x->quant_fp = cpi->sf.use_quant_fp` (vp9_encodeframe.c:5665).
`sf.use_quant_fp` is **0 by default** (vp9_speed_features.c:954) and is set to
`!is_keyframe` **only at REALTIME speed >= 4** (vp9_speed_features.c:573).

So:

| path | use_quant_fp | quantizer | govpx now |
|------|--------------|-----------|-------------|
| cpu0-3 inter (e.g. {0,2,0,0,2}) | 0 | **B** | B |
| cpu4+ inter (e.g. {0,1,1,0,1} cpu4) | 1 | FP | FP — correct |

govpx already computed `sf.UseQuantFp == 0` for cpu0 (pinned by
`vp9_speed_features_rt_cpu_used_0_4_test.go:160`); the write path now consumes
that flag.

## Proof

For `{0,2,0,0,2}` frame-1 mi(0,1) sub-block 2 (a 4x4): the encoder's committed
prediction equals the decoder's (byte-exact w/ libvpx), so the residual is
correct (`12 5 4 1 3 4 6 7 6 1 30 -2 1 -8 8 -13`), and its raw fdct has real AC
energy (`DC130, ACs …151… -108 -90 …`). With FP, govpx zeros all ACs → eob 1
(DC 180 only). Flipping the call to the **B** quantizer yields `DC 1, AC 1@pos3`
→ dq `180@0 + 235@3` == the libvpx oracle exactly.

## Why This Needed Validation

Gating the call on `e.sf.UseQuantFp != 0` is the correct selector port, and it
does NOT touch the cpu4 byte-parity path (cpu4 stays FP, so
`TestVP9FullRDUsePartitionSeed0_1_1_0_1Frame1ByteParity` is unaffected). This
path still needed focused validation because for cpu0 it can cascade:

1. The same function feeds `stampVP9InterLeafTxContext`, so the deep search's
   entropy context changes; the `{0,2,0,0,2}` mode-pins (next-div
   `closedPrefixLen=32`) must stay valid under the B recon/context.
2. Even with the right selector, the full `{0,2,0,0,2}` seed still needs the
   rest of the full-RD inter pipeline wired into the default production path
   before strict full-stream byte parity can replace the skip-list entry.

## Rework Status

The `{0,1,1,0,1}` **cpu4** seed already reaches byte parity (frames 0..29,
`TestVP9FullRDUsePartitionSeed0_1_1_0_1Frame1ByteParity`), proving the deep
engine's machinery is byte-capable when the quantizer is right. To close
`{0,2,0,0,2}` cpu0 to byte parity:

1. Gate `prepareVP9InterTxResidueWithQ` on `e.sf.UseQuantFp` (B for cpu0-3).
   **Done 2026-06-13.**
2. Chase the remaining mi(0,1) residual-value diff with B.
3. Fix the cpu0 mode divergence the FP context was masking (mi(1,6)).
4. Re-derive the `{0,2,0,0,2}` validation from the mode-only next-div probe to a
   byte-parity pin like the cpu4 seed's.

The selector step is now landed and pinned. Keep the remaining steps grouped
with the full-RD production-wire-up work so the strict byte-parity seed moves
from the skip list only when the emitted packet is actually byte-exact.

## mi(1,6) measured RD (2026-06-09 — corrects the entropy-cascade premise)

**Superseded status (2026-06-13):** after the selector port and the current
deep full-RD cost fixes, `TestVP9FullRDInterNextDivergenceSeed0_2_0_0_2`
reports no committed-leaf divergence in the full frame-1 SB0 walk. Keep the
measurement below as history for the thin NONE-vs-SPLIT compare, not as the
current blocker.

Measuring the mi(1,6) NONE-vs-SPLIT RD under FP vs B
(identical seed context) shows the divergence is NOT an entropy-context cascade
through the leaf-tx stamp (the doc earlier hypothesized that); the seed entropy
context is **identical** (above[1,1] left[1,1], same `hasCtx` eob>0) and the
predicted distortions are unchanged (NONE 132672, SPLIT 71842). The B quantizer
only shifts the coefficient **rate** in the RD compare:

| quant | NONE score | NONE rate | SPLIT score | SPLIT rate | winner |
|------|-----------|-----------|-------------|------------|--------|
| FP | 23,080,778 | 22439 | **22,849,024** | 50234 | SPLIT (libvpx-correct) |
| B  | **22,994,076** | 22120 | 23,076,787 | 51072 | NONE (wrong) |

The NONE-vs-SPLIT margin is razor-thin (~0.3-0.4%): FP picks SPLIT by 0.9%, B
flips to NONE by 0.4%. The rate delta (NONE -319, SPLIT +838) enters via
`prepareVP9InterTxResidueWithQ`'s cost_coeffs feeding `scoreVP9InterTxCandidate`
and the cross-leaf chroma entropy context that the preceding 8x8-NONE leaf
mi(0,6) stamps with B vs FP coefficients. The prior mi(1,6) SPLIT closure
(c7ab6566 etc.) was calibrated against the **FP** costs, so switching to the
libvpx-correct B quantizer de-calibrates that thin compare. Closing it means
chasing the exact remaining chroma-context / cost_coeffs cost gap at mi(1,6) so
govpx's B-context NONE/SPLIT scores match libvpx's true B scores — part of the
same unit as steps 2 and 4. Do NOT lower the `closedPrefixLen = 32` regression
gate to absorb this (it is a hard gate).
