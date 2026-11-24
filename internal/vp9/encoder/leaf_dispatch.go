package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
)

// VP9 per-leaf dispatch — the natural cut point at write_modes_b.
// Ported from libvpx v1.16.0 vp9/encoder/vp9_bitstream.c:
// write_modes_b composes the per-leaf mode-info emit (write_mb_modes_kf
// or pack_inter_mode_mvs) with the residual pack (pack_mb_tokens).
// Skip blocks emit no residual — matching libvpx's decode_block which
// short-circuits the residue read entirely when skip=1.

// WriteModesBKeyframe composes WriteKeyframeBlock + WriteKeyframeUvMode
// with the per-leaf coefficient walker for one keyframe leaf. The
// residue walk is skipped when args.Mi.Skip is non-zero so the encoder
// matches libvpx's decoder-side short-circuit (reset_skip_context).
//
// `uvMode` is the encoder-selected UV intra mode; NeighborMi doesn't
// carry a uv_mode slot today so the caller hands it in directly.
func WriteModesBKeyframe(bw *bitstream.Writer, kfArgs WriteKeyframeBlockArgs,
	uvMode common.PredictionMode, coefArgs WriteCoefSbArgs,
) error {
	WriteKeyframeBlock(bw, kfArgs)
	WriteKeyframeUvMode(bw, uvMode, kfArgs.Mi.Mode)
	if kfArgs.Mi.Skip != 0 {
		return nil
	}
	return WriteCoefSb(bw, coefArgs)
}

// WriteModesBInter composes WriteInterBlock with the per-leaf
// coefficient walker for one inter-frame leaf. The residue walk is
// skipped when args.Mi.Skip is non-zero.
func WriteModesBInter(bw *bitstream.Writer, interArgs WriteInterBlockArgs,
	coefArgs WriteCoefSbArgs,
) error {
	WriteInterBlock(bw, interArgs)
	if interArgs.Mi.Skip != 0 {
		return nil
	}
	return WriteCoefSb(bw, coefArgs)
}
