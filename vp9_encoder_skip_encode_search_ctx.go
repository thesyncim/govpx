package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
)

// vp9_encoder_skip_encode_search_ctx.go ports libvpx's x->skip_encode
// search-phase entropy-context freeze for the deep full-RD use-partition path.
//
// libvpx encode_superblock (vp9/encoder/vp9_encodeframe.c:6112-6115):
//
//	x->skip_encode = (!output_enabled && cpi->sf.skip_encode_frame &&
//	                  x->q_index < QIDX_SKIP_THRESH);
//	if (x->skip_encode) return;
//
// The output_enabled==0 encode_superblock calls are the RD-search-phase
// intermediate encodes that rd_use_partition / rd_pick_partition run between a
// leaf's rd_pick_sb_modes and the next sibling's (the encode_sb with do_recon
// at vp9_encodeframe.c:2798/2762 and the per-leaf SPLIT-trial encode_sb). When
// sf->skip_encode_frame is set (computed per frame from the previous frame's
// intra/inter counts, get_skip_encode_frame, vp9_encodeframe.c:5380-5391) AND
// the frame's base_qindex is below QIDX_SKIP_THRESH (115), that intermediate
// encode early-returns WITHOUT calling vp9_encode_sb -> vp9_set_contexts, so it
// never advances pd->above_context / pd->left_context. The consequence: every
// leaf in the superblock runs its RD search (super_block_yrd / super_block_uvrd
// cost_coeffs, which read t_above/t_left seeded from pd->above_context /
// pd->left_context, vp9_rdopt.c:872) against the SUPERBLOCK-ENTRY entropy
// context — the per-leaf committed context is NOT threaded into the search.
//
// The real bitstream encode (output_enabled==1, the final encode_sb at
// vp9_encodeframe.c:2798 for the 64x64) runs normally and DOES thread the
// committed context, so the bitstream + the next superblock are unaffected.
//
// govpx often runs the per-leaf RD search in the same pass that later owns the
// committed coefficient context. To reproduce libvpx's decoupling without
// disturbing commit threading, the deep use-partition path:
//
//  1. snapshots the plane entropy context at 64x64 SB entry
//     (vp9SnapshotSBSearchEntropy), and
//  2. runs each leaf's mode/RD search with the live context temporarily restored
//     to the SB-entry snapshot (vp9WithSBSearchEntropy), restoring the running
//     threaded context immediately after so the committed coefficient path
//     advances the real context.
//
// This is scoped to the VAR_BASED use-partition deep-RD path and is a no-op
// whenever skip_encode is not armed for the frame (e.g. {0,1,1,0,1} frame 1,
// where sf->skip_encode_frame==0 because the previous frame is the all-intra
// keyframe), so the frame-1 byte pin is unaffected.

// vp9SkipEncodeSearchCtxActive reports whether libvpx's x->skip_encode would be
// set for this frame's RD-search-phase encodes, i.e. whether the per-leaf
// search context must be frozen at SB entry. Mirrors the encode_superblock
// predicate (sf->skip_encode_frame && q_index < QIDX_SKIP_THRESH) with the
// output_enabled==0 RD-search-phase always true here.
func (e *VP9Encoder) vp9SkipEncodeSearchCtxActive(inter *vp9InterEncodeState) bool {
	if !e.vp9UseDeepRDUsePartitionPath() || inter == nil {
		return false
	}
	if e.sf.SkipEncodeFrame == 0 {
		return false
	}
	return inter.baseQindex < vp9QIdxSkipThresh
}

// vp9SnapshotSBSearchEntropy captures the current plane entropy context
// (pd->above_context over the SB's column footprint, pd->left_context over the
// SB row) at 64x64 superblock entry. The captured state is the SB-entry context
// every leaf's RD search reads while skip_encode is armed. Called from
// writeVP9ModesSb at the BLOCK_64X64 entry, before partition picking searches
// or commits any leaf in the SB.
func (e *VP9Encoder) vp9SnapshotSBSearchEntropy(miCol int) {
	for plane := range vp9dec.MaxMbPlane {
		pd := &e.planes[plane]
		// above_context footprint: the 64x64 SB spans num_4x4_blocks_wide[64x64]
		// = 16 luma 4x4 columns, scaled by subsampling. left_context is the full
		// SB-row column (len(pd.LeftContext)).
		aboveStart := (miCol * 2) >> pd.SubsamplingX
		aboveLen := 16 >> pd.SubsamplingX
		if aboveStart+aboveLen > len(pd.AboveContext) {
			aboveLen = len(pd.AboveContext) - aboveStart
		}
		if aboveLen < 0 {
			aboveLen = 0
		}
		e.vp9SBEntropyAbove[plane] = buffers.EnsureLen(e.vp9SBEntropyAbove[plane], aboveLen)
		if aboveLen > 0 {
			copy(e.vp9SBEntropyAbove[plane], pd.AboveContext[aboveStart:aboveStart+aboveLen])
		}
		leftLen := len(pd.LeftContext)
		e.vp9SBEntropyLeft[plane] = buffers.EnsureLen(e.vp9SBEntropyLeft[plane], leftLen)
		copy(e.vp9SBEntropyLeft[plane], pd.LeftContext)
		e.vp9SBEntropySaveBuf[plane] = buffers.EnsureLen(e.vp9SBEntropySaveBuf[plane],
			aboveLen+leftLen)
	}
	e.vp9SBEntropyValid = true
}

// vp9WithSBSearchEntropy runs fn with the plane entropy context over the given
// leaf footprint temporarily restored to the SB-entry snapshot
// (vp9SnapshotSBSearchEntropy), then restores the running (threaded) context.
// This is the per-leaf wrapper that makes the leaf's RD search read the
// SB-entry context (libvpx's frozen skip_encode search context) while leaving
// the committed context advance to the real coefficient commit afterwards.
//
// Only the leaf's own above/left footprint is swapped (the offsets the leaf's
// producers read), so siblings already committed into the live context are
// untouched outside this leaf's window. A no-op (just runs fn) when the SB
// snapshot is not valid.
func (e *VP9Encoder) vp9WithSBSearchEntropy(miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, fn func(),
) {
	if !e.vp9SBEntropyValid {
		fn()
		return
	}
	aboveOffsets, leftOffsets := e.vp9EncoderPlaneContextOffsets(miRow, miCol)
	sbCol := (miCol >> common.MiBlockSizeLog2) << common.MiBlockSizeLog2
	// Per-plane: save live footprint, restore SB-entry footprint, run fn,
	// restore live footprint. Mirror libvpx: the leaf search reads t_above /
	// t_left over the plane block size's 4x4 footprint.
	type swap struct {
		aboveOff, aboveLen int
		leftOff, leftLen   int
	}
	var swaps [vp9dec.MaxMbPlane]swap
	for plane := range vp9dec.MaxMbPlane {
		pd := &e.planes[plane]
		planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
		if planeBsize >= common.BlockSizes {
			continue
		}
		aboveLen := int(common.Num4x4BlocksWideLookup[planeBsize])
		leftLen := int(common.Num4x4BlocksHighLookup[planeBsize])
		ao := aboveOffsets[plane]
		lo := leftOffsets[plane]
		// Index of the leaf's above footprint inside the SB-entry above snapshot
		// (the snapshot starts at the SB's left column).
		sbAboveStart := (sbCol * 2) >> pd.SubsamplingX
		snapAbove := e.vp9SBEntropyAbove[plane]
		snapLeft := e.vp9SBEntropyLeft[plane]
		buf := e.vp9SBEntropySaveBuf[plane]
		s := swap{aboveOff: ao, aboveLen: aboveLen, leftOff: lo, leftLen: leftLen}
		// Save live above footprint into buf[0:aboveLen], then restore snapshot.
		if vp9ContextWindowOK(ao, aboveLen, len(pd.AboveContext)) &&
			vp9ContextWindowOK(0, aboveLen, len(buf)) {
			copy(buf[:aboveLen], pd.AboveContext[ao:ao+aboveLen])
			if ao >= sbAboveStart {
				relAbove := ao - sbAboveStart
				if vp9ContextWindowOK(relAbove, aboveLen, len(snapAbove)) {
					copy(pd.AboveContext[ao:ao+aboveLen],
						snapAbove[relAbove:relAbove+aboveLen])
				}
			}
		} else {
			s.aboveLen = 0
		}
		// Save live left footprint into buf[aboveLen:aboveLen+leftLen], restore snapshot.
		if vp9ContextWindowOK(lo, leftLen, len(pd.LeftContext)) &&
			vp9ContextWindowOK(s.aboveLen, leftLen, len(buf)) &&
			vp9ContextWindowOK(lo, leftLen, len(snapLeft)) {
			copy(buf[s.aboveLen:s.aboveLen+leftLen], pd.LeftContext[lo:lo+leftLen])
			copy(pd.LeftContext[lo:lo+leftLen], snapLeft[lo:lo+leftLen])
		} else {
			s.leftLen = 0
		}
		swaps[plane] = s
	}

	fn()

	// Restore the running (threaded) context for the real commit.
	for plane := range vp9dec.MaxMbPlane {
		pd := &e.planes[plane]
		s := swaps[plane]
		buf := e.vp9SBEntropySaveBuf[plane]
		if s.aboveLen > 0 && vp9ContextWindowOK(s.aboveOff, s.aboveLen, len(pd.AboveContext)) {
			copy(pd.AboveContext[s.aboveOff:s.aboveOff+s.aboveLen], buf[:s.aboveLen])
		}
		if s.leftLen > 0 && vp9ContextWindowOK(s.leftOff, s.leftLen, len(pd.LeftContext)) {
			copy(pd.LeftContext[s.leftOff:s.leftOff+s.leftLen],
				buf[s.aboveLen:s.aboveLen+s.leftLen])
		}
	}
}
