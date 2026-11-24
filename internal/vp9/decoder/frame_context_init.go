package decoder

import "github.com/thesyncim/govpx/internal/vp9/tables"

// VP9 default frame-context seeder. Ported from libvpx v1.16.0
// vp9/common/vp9_entropymode.c — init_mode_probs + the equivalent
// init_coef_probs / init_mv_probs paths libvpx invokes inside
// vp9_setup_past_independence at keyframes and error-resilient
// resets.
//
// Both encoder and decoder need byte-identical seed probabilities
// before a context-resetting frame is decoded; this function is
// the single source of truth.

// ResetFrameContext seeds every probability slot in fc with the
// libvpx default values. Mirrors the cumulative effect of
// init_mode_probs (mode + ref + skip + partition + interp), plus
// the per-tx-size default_coef_probs copy and default_nmv_context
// copy that vp9_setup_past_independence triggers via
// vp9_default_coef_probs / vp9_init_mv_probs.
//
// Keyframes, error-resilient frames, and any frame whose
// frame_context_idx points at an unset context all start from
// this seed; counts-driven updates ride on top.
func ResetFrameContext(fc *FrameContext) {
	// Mode + ref + skip + partition + switchable interp.
	fc.YModeProb = tables.DefaultIfYProbs
	fc.UvModeProb = tables.DefaultIfUvProbs
	fc.SwitchableInterpProb = tables.DefaultSwitchableInterpProb
	fc.PartitionProb = tables.DefaultPartitionProbs
	fc.IntraInterProb = tables.DefaultIntraInter
	fc.ReferenceModeProbs.CompInterProb = tables.DefaultCompInter
	fc.ReferenceModeProbs.CompRefProb = tables.DefaultCompRef
	fc.ReferenceModeProbs.SingleRefProb = tables.DefaultSingleRef
	fc.TxProbs.P32x32 = tables.DefaultTxProbsP32x32
	fc.TxProbs.P16x16 = tables.DefaultTxProbsP16x16
	fc.TxProbs.P8x8 = tables.DefaultTxProbsP8x8
	fc.SkipProbs = tables.DefaultSkipProbs
	fc.InterModeProbs = tables.DefaultInterModeProbs

	// Coefficient probs (per tx_size).
	fc.CoefProbs[0] = tables.DefaultCoefProbs4x4
	fc.CoefProbs[1] = tables.DefaultCoefProbs8x8
	fc.CoefProbs[2] = tables.DefaultCoefProbs16x16
	fc.CoefProbs[3] = tables.DefaultCoefProbs32x32

	// NMV context.
	fc.Nmvc.Joints = tables.DefaultNmvJoints
	for i := range 2 {
		src := &tables.DefaultNmvComps[i]
		dst := &fc.Nmvc.Comps[i]
		dst.Sign = src.Sign
		dst.Classes = src.Classes
		dst.Class0 = src.Class0
		dst.Bits = src.Bits
		dst.Class0Fp = src.Class0Fp
		dst.Fp = src.Fp
		dst.Class0Hp = src.Class0Hp
		dst.Hp = src.Hp
	}
}
