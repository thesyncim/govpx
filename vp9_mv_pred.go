package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// vp9_mv_pred.go ports libvpx v1.16.0's vp9_mv_pred candidate-set SAD scan
// (vp9/encoder/vp9_rd.c:588-639) and the vp9_NEWMV_diff_bias adjustment
// (vp9/encoder/vp9_pickmode.c:1309-1372).
//
// vp9_mv_pred consumes the per-ref MV candidate triple:
//
//	pred_mv[0] = x->mbmi_ext->ref_mvs[ref_frame][0].as_mv
//	pred_mv[1] = x->mbmi_ext->ref_mvs[ref_frame][1].as_mv
//	pred_mv[2] = x->pred_mv[ref_frame]
//
// and walks 2 or 3 candidates depending on (block_size < x->max_partition_size).
// For each candidate it converts the eighth-pel MV to a full-pel offset, SADs
// the source block against ref_y at that offset, and tracks the best SAD and
// best index. The output is written to x->pred_mv_sad[ref_frame] (used by the
// reference-masking gate in vp9_pickmode.c:2204-2228) and x->mv_best_ref_index
// (used in vp9_rdopt.c::single_motion_search for the integer MV seed).
//
// vp9_NEWMV_diff_bias biases the NEWMV RD cost when the candidate MV is far
// from the average of the above/left neighbour MVs, and adds an extra noise-
// estimate-driven bias toward small motion on LAST_FRAME for large blocks.
// libvpx gates the call to vp9_NEWMV_diff_bias on (rc_mode == VPX_CBR &&
// speed >= 5 && content != SCREEN); govpx mirrors that gate at the call site.
//
// The govpx port preserves libvpx's:
//   - num_mv_refs formula (MAX_MV_REF_CANDIDATES + (block_size <
//     max_partition_size))
//   - INT16_MAX skip semantics for unfilled candidates
//   - the near_same_nearest dedup that skips i==1 when ref_mvs[0]==ref_mvs[1]
//   - the zero_seen dedup that visits the (0,0) candidate at most once
//   - the (mv.row + 3 + (mv.row >= 0)) >> 3 sub-pel-to-full-pel rounding
//   - the cpi->fn_ptr[block_size].sdf size-specialized SAD entry point
//   - the max_mv tracker output (vp9_rd.c:618)
//
// libvpx ref: vp9/encoder/vp9_rd.c:588-639
//
//	void vp9_mv_pred(VP9_COMP *cpi, MACROBLOCK *x, uint8_t *ref_y_buffer,
//	                 int ref_y_stride, int ref_frame, BLOCK_SIZE block_size)

// vp9MvPredMaxCandidates is the maximum candidate-set size for vp9_mv_pred.
// libvpx: vp9_rd.c:599-601:
//
//	const int num_mv_refs = MAX_MV_REF_CANDIDATES +
//	    (block_size < x->max_partition_size);
//
// MAX_MV_REF_CANDIDATES = 2 (vp9_mvref_common.h:21); the (block_size <
// max_partition_size) test contributes 0 or 1, so the candidate set is 2 or 3.
const vp9MvPredMaxCandidates = 3

// vp9MvPredInputCandidate is one of the three MVs that vp9_mv_pred SADs against
// the source block. libvpx packs these into a stack-local MV[3] array; govpx
// passes the same shape via a slice so callers can build it from the
// ref_mvs[0..1] candidate list plus the optional x->pred_mv[ref] entry.
//
// libvpx: vp9_rd.c:602-606:
//
//	MV pred_mv[3];
//	pred_mv[0] = x->mbmi_ext->ref_mvs[ref_frame][0].as_mv;
//	pred_mv[1] = x->mbmi_ext->ref_mvs[ref_frame][1].as_mv;
//	pred_mv[2] = x->pred_mv[ref_frame];
type vp9MvPredInputCandidate struct {
	mv    vp9dec.MV
	valid bool // libvpx encodes "absent" as INT16_MAX in either component.
}

// vp9MvPredResult is the output tuple vp9_mv_pred writes to MACROBLOCK in
// libvpx. govpx returns these by value because the picker is invoked
// per-(ref_frame, block_size) and the caller stashes the values into the
// per-call predMvSad/maxMvContext/mvBestRefIndex arrays.
//
// libvpx writes:
//
//	x->mv_best_ref_index[ref_frame] = best_index;
//	x->max_mv_context[ref_frame]    = max_mv;
//	x->pred_mv_sad[ref_frame]       = best_sad;
//
// (vp9_rd.c:636-638)
type vp9MvPredResult struct {
	bestSad      uint64 // libvpx's pred_mv_sad units (uint32 in C; we widen).
	bestIndex    int    // index into the input candidate set, 0..num_mv_refs-1.
	maxMvContext int    // max(|row|, |col|) >> 3 across the input candidates.
}

// vp9MvPredScanCandidates ports the body of vp9_mv_pred verbatim. It does
// not depend on a particular VP9_COMP / MACROBLOCK shape — the caller hands
// in the candidate triple plus the source / ref planes and block geometry.
//
// libvpx: vp9_rd.c:588-639.
//
//	int zero_seen = 0;
//	int best_index = 0;
//	int best_sad = INT_MAX;
//	int max_mv = 0;
//	int near_same_nearest = ref_mvs[0].as_int == ref_mvs[1].as_int;
//	for (i = 0; i < num_mv_refs; ++i) {
//	  const MV *this_mv = &pred_mv[i];
//	  int fp_row, fp_col;
//	  if (this_mv->row == INT16_MAX || this_mv->col == INT16_MAX) continue;
//	  if (i == 1 && near_same_nearest) continue;
//	  fp_row = (this_mv->row + 3 + (this_mv->row >= 0)) >> 3;
//	  fp_col = (this_mv->col + 3 + (this_mv->col >= 0)) >> 3;
//	  max_mv = VPXMAX(max_mv, VPXMAX(|row|, |col|) >> 3);
//	  if (fp_row == 0 && fp_col == 0 && zero_seen) continue;
//	  zero_seen |= (fp_row == 0 && fp_col == 0);
//	  ref_y_ptr = &ref_y_buffer[ref_y_stride * fp_row + fp_col];
//	  this_sad = cpi->fn_ptr[block_size].sdf(src, src_stride, ref_y_ptr,
//	                                         ref_y_stride);
//	  if (this_sad < best_sad) { best_sad = this_sad; best_index = i; }
//	}
//
// The src / ref slices use govpx's vp9BlockSAD entry, which already routes
// size-specialized callers to vpx_sad{NxM} (vpx_dsp/sad.c). The (src_x,
// src_y) anchor is the source-plane offset to the top-left of the SB; libvpx
// uses x->plane[0].src.buf directly because the buf pointer already includes
// the offset.
//
// ref_y_anchor_x / ref_y_anchor_y are the reference-plane offsets to the
// same block origin BEFORE the per-candidate (fp_col, fp_row) offset is
// added. Callers compute these once per ref/block.
func vp9MvPredScanCandidates(
	candidates []vp9MvPredInputCandidate, numMvRefs int,
	src []byte, srcStride int, srcX, srcY int,
	refY []byte, refYStride int, refAnchorX, refAnchorY int,
	refW, refH int,
	blockW, blockH int,
) vp9MvPredResult {
	// libvpx: vp9_rd.c:590-593.
	//   int zero_seen = 0; int best_index = 0; int best_sad = INT_MAX;
	//   int max_mv = 0;
	out := vp9MvPredResult{bestSad: ^uint64(0)}
	zeroSeen := false

	// libvpx: vp9_rd.c:608-609.
	//   near_same_nearest =
	//     x->mbmi_ext->ref_mvs[ref_frame][0].as_int ==
	//     x->mbmi_ext->ref_mvs[ref_frame][1].as_int;
	nearSameNearest := false
	if len(candidates) >= 2 && candidates[0].valid && candidates[1].valid &&
		candidates[0].mv == candidates[1].mv {
		nearSameNearest = true
	}

	// libvpx: vp9_rd.c:611 — for (i = 0; i < num_mv_refs; ++i).
	for i := range numMvRefs {
		if i >= len(candidates) {
			break
		}
		c := candidates[i]
		// libvpx: vp9_rd.c:614 — INT16_MAX skip semantics. The govpx
		// "valid" boolean covers the same predicate (callers populate
		// valid=false for INT16_MAX components).
		if !c.valid {
			continue
		}
		// libvpx: vp9_rd.c:615 — if (i == 1 && near_same_nearest) continue;
		if i == 1 && nearSameNearest {
			continue
		}
		// libvpx: vp9_rd.c:616-617 — sub-pel-to-full-pel rounding:
		//   fp_row = (this_mv->row + 3 + (this_mv->row >= 0)) >> 3;
		//   fp_col = (this_mv->col + 3 + (this_mv->col >= 0)) >> 3;
		// The (this_mv->row >= 0) test is +1 when non-negative, 0 when
		// negative; libvpx relies on signed >>3 arithmetic-shifting toward
		// minus-infinity. Go's int >> on signed types is also arithmetic.
		row := int(c.mv.Row)
		col := int(c.mv.Col)
		var rowAdj, colAdj int
		if row >= 0 {
			rowAdj = 1
		}
		if col >= 0 {
			colAdj = 1
		}
		fpRow := (row + 3 + rowAdj) >> 3
		fpCol := (col + 3 + colAdj) >> 3

		// libvpx: vp9_rd.c:618.
		//   max_mv = VPXMAX(max_mv, VPXMAX(abs(row), abs(col)) >> 3);
		absRow := row
		if absRow < 0 {
			absRow = -absRow
		}
		absCol := col
		if absCol < 0 {
			absCol = -absCol
		}
		thisMag := max(absCol, absRow)
		thisMag >>= 3
		if thisMag > out.maxMvContext {
			out.maxMvContext = thisMag
		}

		// libvpx: vp9_rd.c:620-621 — zero_seen dedup.
		if fpRow == 0 && fpCol == 0 && zeroSeen {
			continue
		}
		if fpRow == 0 && fpCol == 0 {
			zeroSeen = true
		}

		// libvpx: vp9_rd.c:623-626 — SAD at the candidate offset.
		//   ref_y_ptr = &ref_y_buffer[ref_y_stride * fp_row + fp_col];
		//   this_sad = cpi->fn_ptr[block_size].sdf(src, src_stride,
		//                                          ref_y_ptr, ref_y_stride);
		// In libvpx, ref_y_buffer is the YV12 buffer with a 160-pixel
		// border, so fp_row / fp_col can be negative without indexing
		// out of bounds. govpx's ref buffer is a frame-sized plane with
		// no padding, so out-of-bounds candidates are skipped (libvpx's
		// equivalent skip happens via the YV12 plane scaling, not the
		// SAD kernel). The clamp matches the source-plane bounds check
		// the existing reference-masking path uses.
		refXLeft := refAnchorX + fpCol
		refYTop := refAnchorY + fpRow
		if refXLeft < 0 || refYTop < 0 ||
			refXLeft+blockW > refW || refYTop+blockH > refH {
			continue
		}

		// libvpx: vp9_rd.c:624 fn_ptr[bsize].sdf — size-specialized SAD.
		// vp9BlockSAD dispatches to the same vpx_sad{NxM} kernels.
		thisSad := vp9BlockSAD(src, srcStride, refY, refYStride,
			srcX, srcY, refXLeft, refYTop, blockW, blockH, ^uint64(0))

		// libvpx: vp9_rd.c:629-632 — track best.
		if thisSad < out.bestSad {
			out.bestSad = thisSad
			out.bestIndex = i
		}
	}
	return out
}

// vp9MvPredNumCandidates ports the num_mv_refs formula at vp9_rd.c:599-601.
//
//	const int num_mv_refs = MAX_MV_REF_CANDIDATES +
//	                        (block_size < x->max_partition_size);
//
// MAX_MV_REF_CANDIDATES = 2. The optional third candidate (x->pred_mv[ref])
// is included only at sub-max_partition_size leaves. govpx's nonrd walker
// runs with max_partition_size = BLOCK_64X64 (libvpx vp9_encodeframe.c:5315
// — ML_BASED_PARTITION sets x->max_partition_size = BLOCK_64X64) so any bsize
// strictly less than BLOCK_64X64 includes the third candidate.
func vp9MvPredNumCandidates(blockSize, maxPartitionSize common.BlockSize) int {
	// libvpx MAX_MV_REF_CANDIDATES (vp9_mvref_common.h:21).
	n := 2
	if blockSize < maxPartitionSize {
		n++
	}
	return n
}

// vp9NewmvDiffBiasResult is the (rate, dist, rdcost) override that
// vp9_NEWMV_diff_bias rewrites in place on the candidate RD_COST. govpx
// returns the new rdcost so callers don't mutate the candidate state until
// the rest of the per-candidate work has completed.
//
// libvpx: vp9_pickmode.c:1309-1372.
type vp9NewmvDiffBiasResult struct {
	rdcost   uint64 // possibly-adjusted rdcost.
	adjusted bool   // true when at least one of the two branches fired.
}

// vp9NewmvDiffBias ports vp9_NEWMV_diff_bias verbatim.
//
// libvpx: vp9_pickmode.c:1309-1372:
//
//	static void vp9_NEWMV_diff_bias(const NOISE_ESTIMATE *ne, MACROBLOCKD *xd,
//	                                PREDICTION_MODE this_mode, RD_COST *this_rdc,
//	                                BLOCK_SIZE bsize, int mv_row, int mv_col,
//	                                int is_last_frame, int lowvar_highsumdiff,
//	                                int is_skin) {
//	  if (this_mode == NEWMV) { ... above_mi + left_mi average; row_diff/col_diff;
//	                            if any diff > 48 or < -48 — shift rdcost left
//	                            by 1 for bsize > 32x32, else multiply by 3/2. }
//	  if (ne->enabled && ne->level >= kMedium && bsize >= 32x32 &&
//	      is_last_frame && |mv_row| < 8 && |mv_col| < 8)
//	    this_rdc->rdcost = 7 * (this_rdc->rdcost >> 3);
//	  else if (lowvar_highsumdiff && !is_skin && bsize >= 16x16 &&
//	      is_last_frame && |mv_row| < 16 && |mv_col| < 16)
//	    this_rdc->rdcost = 7 * (this_rdc->rdcost >> 3);
//	}
//
// govpx's encoder does not yet surface noise_estimate / lowvar_highsumdiff /
// sb_is_skin (libvpx wires them through cpi->noise_estimate, x->lowvar_*,
// x->sb_is_skin). The deferred RefControl seeds run with noise_estimate
// disabled (cpi->oxcf.noise_sensitivity == 0 forces ne->enabled = 0) and
// content == VP9E_CONTENT_DEFAULT, so the second/third clauses are unreachable
// under those configurations. The verbatim port keeps the signature so the
// future agents can wire ne/lowvar_highsumdiff/is_skin without re-touching
// the kernel.
func vp9NewmvDiffBias(thisMode common.PredictionMode, rdcost uint64,
	bsize common.BlockSize, mvRow, mvCol int,
	aboveMi, leftMi *vp9dec.NeighborMi,
	isLastFrame bool, noiseEnabled bool, noiseAtLeastMedium bool,
	lowvarHighsumdiff bool, isSkin bool,
) vp9NewmvDiffBiasResult {
	out := vp9NewmvDiffBiasResult{rdcost: rdcost}

	// libvpx: vp9_pickmode.c:1313 — if (this_mode == NEWMV).
	if thisMode == common.NewMv {
		// libvpx: vp9_pickmode.c:1316-1320 — int above_mv_valid = 0;
		//   int left_mv_valid = 0; int above_row = 0, above_col = 0;
		var aboveRow, aboveCol int
		var leftRow, leftCol int
		aboveMvValid := false
		leftMvValid := false

		// libvpx: vp9_pickmode.c:1322-1326 — read above neighbour MV.
		//   if (xd->above_mi) {
		//     above_mv_valid = xd->above_mi->mv[0].as_int != INVALID_MV;
		//     above_row = xd->above_mi->mv[0].as_mv.row;
		//     above_col = xd->above_mi->mv[0].as_mv.col;
		//   }
		// libvpx's INVALID_MV literal is 0x80008000; govpx neighbour MVs are
		// always valid when the neighbour MI exists (the encoder zeroes the
		// MV slot when the neighbour is intra), so the validity check
		// reduces to (aboveMi != nil).
		if aboveMi != nil {
			aboveMvValid = true
			aboveRow = int(aboveMi.Mv[0].Row)
			aboveCol = int(aboveMi.Mv[0].Col)
		}
		// libvpx: vp9_pickmode.c:1327-1331 — read left neighbour MV.
		if leftMi != nil {
			leftMvValid = true
			leftRow = int(leftMi.Mv[0].Row)
			leftCol = int(leftMi.Mv[0].Col)
		}

		// libvpx: vp9_pickmode.c:1332-1343 — average row/col of valid
		// neighbours.
		var alRow, alCol int
		switch {
		case aboveMvValid && leftMvValid:
			alRow = (aboveRow + leftRow + 1) >> 1
			alCol = (aboveCol + leftCol + 1) >> 1
		case aboveMvValid:
			alRow = aboveRow
			alCol = aboveCol
		case leftMvValid:
			alRow = leftRow
			alCol = leftCol
		default:
			alRow = 0
			alCol = 0
		}

		// libvpx: vp9_pickmode.c:1344-1345 — row_diff / col_diff.
		rowDiff := alRow - mvRow
		colDiff := alCol - mvCol

		// libvpx: vp9_pickmode.c:1346-1351 — out-of-band shift.
		if rowDiff > 48 || rowDiff < -48 || colDiff > 48 || colDiff < -48 {
			if bsize > common.Block32x32 {
				out.rdcost = out.rdcost << 1
			} else {
				out.rdcost = (3 * out.rdcost) >> 1
			}
			out.adjusted = true
		}
	}

	// libvpx: vp9_pickmode.c:1354-1356 — noise-estimate bias toward small
	// motion on LAST_FRAME for large blocks. Gated on
	//   ne->enabled && ne->level >= kMedium && bsize >= BLOCK_32X32 &&
	//   is_last_frame && |mv_row| < 8 && |mv_col| < 8.
	absRow := mvRow
	if absRow < 0 {
		absRow = -absRow
	}
	absCol := mvCol
	if absCol < 0 {
		absCol = -absCol
	}
	if noiseEnabled && noiseAtLeastMedium && bsize >= common.Block32x32 &&
		isLastFrame && absRow < 8 && absCol < 8 {
		out.rdcost = 7 * (out.rdcost >> 3)
		out.adjusted = true
	} else if lowvarHighsumdiff && !isSkin && bsize >= common.Block16x16 &&
		// libvpx: vp9_pickmode.c:1358-1361 — low-var/high-sum-diff bias.
		isLastFrame && absRow < 16 && absCol < 16 {
		out.rdcost = 7 * (out.rdcost >> 3)
		out.adjusted = true
	}
	return out
}
