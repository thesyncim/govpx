package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// VP9 per-block mode-info compose layer (encoder side). Ported from
// libvpx v1.16.0 vp9/encoder/vp9_bitstream.c — write_mb_modes_kf
// covers the keyframe path. The inter-frame pack_inter_mode_mvs
// composer lands separately because it needs the per-block
// mode_context table the encoder runs the MV-ref search against.
//
// These compose-layer writers mirror the matching decoder
// functions (ReadIntraFrameModeInfo, ReadIntraBlockModeInfo,
// ReadInterBlockModeInfo) and produce the wire fragment the
// decoder reads back byte-for-byte.

// WriteKeyframeBlockArgs bundles the inputs WriteKeyframeBlock
// consults.
type WriteKeyframeBlockArgs struct {
	Seg       *vp9dec.SegmentationParams
	Mi        *vp9dec.NeighborMi
	AboveMi   *vp9dec.NeighborMi
	LeftMi    *vp9dec.NeighborMi
	TxMode    common.TxMode
	MaxTxSize common.TxSize
	TxProbs   []uint8 // selected tx_probs row for this max + ctx
	SkipProbs [3]uint8
}

// WriteKeyframeBlock mirrors libvpx's write_mb_modes_kf. Emits the
// per-block keyframe wire fragment in order:
//
//  1. segment id (when seg.UpdateMap) via WriteSegmentId.
//  2. skip bit via WriteSkip.
//  3. selected tx size if bsize >= 8x8 && txMode == TxModeSelect.
//  4. Y mode(s): one per block for >=8x8; one per 4x4 sub-block for
//     smaller block sizes (4x4, 4x8, 8x4).
//  5. UV mode: from kfUvModeProb[Y mode].
func WriteKeyframeBlock(bw *bitstream.Writer, args WriteKeyframeBlockArgs) {
	bsize := args.Mi.SbType
	segID := int(args.Mi.SegIDPredicted) // keyframe path: id is stored here

	WriteSegmentId(bw, args.Seg, segID)
	WriteSkip(bw, WriteSkipArgs{
		Seg:       args.Seg,
		SegID:     segID,
		SkipProbs: args.SkipProbs,
		Above:     args.AboveMi,
		Left:      args.LeftMi,
	}, int(args.Mi.Skip))

	if bsize >= common.Block8x8 && args.TxMode == common.TxModeSelect {
		WriteSelectedTxSize(bw, args.Mi.TxSize, args.MaxTxSize, args.TxProbs)
	}

	if bsize >= common.Block8x8 {
		probs := vp9dec.GetYModeProbs(args.Mi, args.AboveMi, args.LeftMi, 0)
		WriteIntraMode(bw, args.Mi.Mode, probs)
	} else {
		writeSub8x8IntraModes(bw, args)
	}
	// UV emit is intentionally separate: NeighborMi doesn't carry a
	// uv_mode field, so the caller hands the encoder-selected
	// uv_mode in via WriteKeyframeUvMode after this call.
}

// WriteKeyframeUvMode emits the UV intra mode bit pattern given the
// already-chosen Y mode. Caller hands in the encoder-selected
// uvMode separately since NeighborMi doesn't surface a uv_mode
// field today.
func WriteKeyframeUvMode(bw *bitstream.Writer, uvMode, yMode common.PredictionMode) {
	uvProbs := tables.KfUvModeProb[yMode]
	WriteIntraMode(bw, uvMode, uvProbs[:])
}

func writeSub8x8IntraModes(bw *bitstream.Writer, args WriteKeyframeBlockArgs) {
	bsize := args.Mi.SbType
	num4x4W := int(common.Num4x4BlocksWideLookup[bsize])
	num4x4H := int(common.Num4x4BlocksHighLookup[bsize])
	for idy := 0; idy < 2; idy += num4x4H {
		for idx := 0; idx < 2; idx += num4x4W {
			block := idy*2 + idx
			probs := vp9dec.GetYModeProbs(args.Mi, args.AboveMi, args.LeftMi, block)
			WriteIntraMode(bw, args.Mi.Bmi[block].AsMode, probs)
		}
	}
}
