package govpx

import (
	"math"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
)

// vp9_nonrd_pick_partition.go ports the ML_BASED_PARTITION branch of libvpx
// v1.16.0 vp9/encoder/vp9_encodeframe.c:4598-4855 nonrd_pick_partition into
// govpx, plus the ml_predict_var_partitioning helper at vp9_encodeframe.c:
// 4530-4596. Both pieces are verbatim ports — no heuristics, no magic
// numbers; constants (FEATURES, score thresholds, dc_q + variance features)
// match libvpx exactly.
//
// The live picker combines the NN evaluator (encoder.NNPredict +
// vp9_var_part_nnconfig_{64,32,16}) with the get_estimated_pred orchestrator
// (encoder.GetEstimatedPred + int-pro motion search):
//   - At entry into pickVP9InterPartitionBlockSize at BLOCK_64X64 the SB-level
//     est_pred buffer is filled once per SB (libvpx vp9_encodeframe.c:5314
//     get_estimated_pred call before nonrd_pick_partition).
//   - At each recursive level (64x64, 32x32, 16x16) ml_predict_var_partitioning
//     reads from the per-SB est_pred buffer at the correct (sb_offset_row,
//     sb_offset_col) and dispatches NONE / SPLIT / -1 (libvpx vp9_encodeframe.c:
//     4530-4596).
//   - When the NN returns -1 (no confidence), the recursive picker would
//     explore both PARTITION_NONE and PARTITION_SPLIT and pick by RD. govpx's
//     existing per-block picker (pickVP9CBRVariancePartitionBlockSize +
//     pickVP9InterReferenceMode fallback) already supplies that comparison so
//     the -1 branch returns BlockInvalid back to the caller, which then
//     enters the variance / RD fallback path.
//
// Scope of ML_BASED_PARTITION on cpu_used=8 with w*h <= 352*288 (libvpx
// vp9_speed_features.c:751-768 + 825-826):
//   - do_rect = 0 (libvpx vp9_encodeframe.c:4633 + 4660-4661 — speed >= 5
//     disables rectangular partitions; use_ml_based_partitioning forces it).
//     The ML picker only chooses between PARTITION_NONE and PARTITION_SPLIT.
//   - auto_min_max_partition_size is enabled at speed >= 5 (libvpx
//     vp9_speed_features.c:233). ML_BASED dispatch pins x->max/min to
//     BLOCK_64X64 / BLOCK_8X8 (vp9_encodeframe.c:5315-5316); do_split is
//     cleared at BLOCK_8X8 so 4x4 leaves are never emitted on this lane.
//   - Forced edge splits at frame boundary: when (mi_row + ms >= mi_rows) the
//     horz split is forced; symmetric for col. The ML picker honours these
//     forced edges by mirroring the partition_horz/vert/none flags from
//     nonrd_pick_partition.

// vp9MLPredictResult mirrors libvpx's ml_predict_var_partitioning return:
//   - PARTITION_NONE (constant 0 in libvpx's PARTITION_TYPE enum).
//   - PARTITION_SPLIT (constant 3 in libvpx's PARTITION_TYPE enum).
//   - -1 — no confidence.
type vp9MLPredictResult int8

const (
	vp9MLPredictNone  vp9MLPredictResult = 0 // PARTITION_NONE
	vp9MLPredictSplit vp9MLPredictResult = 3 // PARTITION_SPLIT
	vp9MLPredictNone1 vp9MLPredictResult = -1
)

// vp9MLPartitionContext carries the per-SB state the ML picker needs.
// Populated once per 64x64 SB at the entry to the recursive picker and
// then re-read at each recursive level for ml_predict_var_partitioning's
// est_pred input.
//
// libvpx counterpart: x->est_pred buffer (64*64 uint8) plus the sb-aligned
// mi_row/mi_col anchors. x->est_pred is allocated per-SB in
// get_estimated_pred (vp9_encodeframe.c:5103 — uint8_t *est_pred =
// (uint8_t *)vpx_memalign(32, 64 * 64);). govpx allocates fresh per
// invocation to avoid encoder-level state.
type vp9MLPartitionContext struct {
	// estPred is the 64x64 luma prediction buffer get_estimated_pred
	// populated for this SB. Indexed row*64+col (stride 64).
	estPred [64 * 64]uint8

	// pickPred mirrors the live luma pd->dst surface used by libvpx while
	// nonrd_pick_partition walks leaves with reuse_inter_pred_sby enabled.
	// It starts as the current reconstructed SB window and is updated by
	// pickmode candidate selection; intra fallback reads its left/above edges.
	pickPred      [64 * 64]uint8
	pickPredReady bool

	// SB-aligned origin (the BLOCK_64X64 top-left mi-row/col).
	sbMiRow int
	sbMiCol int

	// Padded source plane window — points at the border-padded source
	// copy built by vp9BuildPaddedPlane. Offsets are relative to the
	// padded buffer; the variance calc shifts by (srcOriginX, srcOriginY)
	// before indexing.
	src         []uint8
	srcStride   int
	srcOriginX  int
	srcOriginY  int
	srcVisibleW int
	srcVisibleH int

	// Base qindex driving the dc_q feature (libvpx vp9_encodeframe.c:4551).
	baseQindex int

	// libvpx oxcf.speed (libvpx vp9_encodeframe.c:4549 — threshold gate).
	speed int

	// Per-SB ready flag. False when get_estimated_pred could not fill
	// estPred (e.g. missing reference). When false the ML picker returns
	// -1 from ml_predict_var_partitioning for every level.
	ready bool

	// frameValid marks the per-frame slot as populated; reset every frame.
	frameValid bool
}

type vp9MLPickPredSnapshot struct {
	ctx   *vp9MLPartitionContext
	pred  [64 * 64]uint8
	ready bool
	ok    bool
}

func (e *VP9Encoder) saveVP9MLPickPredSnapshot(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int,
) vp9MLPickPredSnapshot {
	if e.sf.PartitionSearchType != MlBasedPartition {
		return vp9MLPickPredSnapshot{}
	}
	ctx := e.vp9MLPickPartitionEntry(inter, miRows, miCols, miRow, miCol)
	if ctx == nil {
		return vp9MLPickPredSnapshot{}
	}
	snap := vp9MLPickPredSnapshot{
		ctx:   ctx,
		ready: ctx.pickPredReady,
		ok:    true,
	}
	copy(snap.pred[:], ctx.pickPred[:])
	return snap
}

func (e *VP9Encoder) restoreVP9MLPickPredSnapshot(snap vp9MLPickPredSnapshot) {
	if !snap.ok || snap.ctx == nil {
		return
	}
	copy(snap.ctx.pickPred[:], snap.pred[:])
	snap.ctx.pickPredReady = snap.ready
}

// vp9MLPartitionBorder is the per-side edge-replication padding the ML
// picker needs in front of LAST_FRAME so the int-pro estimator's `ref_buf -
// (bw>>1)` peek (libvpx vp9_mcomp.c:2317) and the get_estimated_pred
// inter-predictor's clamped 64x64 read window both stay in-bounds. libvpx
// gives every YV12_BUFFER_CONFIG plane a VP9_ENC_BORDER_IN_PIXELS=160 border
// (libvpx vpx_scale/yv12config.h:26); clamp_mv_to_umv_border_sb
// (vp9_reconinter.c:91-111) guarantees the prediction MV never reads past
// that border. govpx mirrors the 160-pixel border so the same clamp keeps
// the convolve source inside the padded plane for partial superblocks.
const vp9MLPartitionBorder = 160

// vp9MLPartitionSbMi is the BLOCK_64X64 superblock size in mi units
// (MI_BLOCK_SIZE = 64 / MI_SIZE = 8). It feeds the mb_to_*_edge formulas
// (libvpx vp9_onyxc_int.h:425/427 set_mi_row_col, bw = bh = 8 mi).
const vp9MLPartitionSbMi = 8

// vp9PaddedLastFrameBuffer is a per-encoder scratch for building the
// border-padded LAST_FRAME copy the int-pro motion search reads against.
type vp9PaddedLastFrameBuffer struct {
	pixels []uint8
	stride int
	rows   int
	w      int
	h      int
}

// vp9BuildPaddedPlane builds (and grows) a border-padded copy of an input
// plane sized (w + 2*border, h + 2*border). Border pixels are edge-
// replicated, plus the y-axis padding rows are constructed by repeating
// the first / last visible rows (libvpx YV12 vpx_extend_frame_borders_*
// semantics). Returns the padded slice, its stride, and the absolute
// origin coordinates (border, border).
//
// libvpx counterpart: vp9_alloc_frame_buffer pre-allocates the YV12
// buffer with VP9_ENC_BORDER_IN_PIXELS=160 border padding on every plane;
// vp9_setup_pre_planes / vp9_setup_src_planes hand out a pointer offset
// to (border, border) so the int-pro motion search's `-(bw>>1)` peek
// stays in-bounds.
func vp9BuildPaddedPlane(buf *vp9PaddedLastFrameBuffer,
	plane []uint8, planeStride, w, h int,
) (pixels []uint8, stride, originY, originX int) {
	stride = w + 2*vp9MLPartitionBorder
	rows := h + 2*vp9MLPartitionBorder
	needed := stride * rows
	buf.pixels = buffers.EnsureLen(buf.pixels, needed)
	buf.stride = stride
	buf.rows = rows
	buf.w = w
	buf.h = h

	for y := range rows {
		srcRow := y - vp9MLPartitionBorder
		if srcRow < 0 {
			srcRow = 0
		} else if srcRow >= h {
			srcRow = h - 1
		}
		dst := buf.pixels[y*stride:]
		src := plane[srcRow*planeStride : srcRow*planeStride+w]
		// Left border.
		left := src[0]
		for x := range vp9MLPartitionBorder {
			dst[x] = left
		}
		// Body.
		copy(dst[vp9MLPartitionBorder:vp9MLPartitionBorder+w], src)
		// Right border.
		right := src[w-1]
		for x := vp9MLPartitionBorder + w; x < stride; x++ {
			dst[x] = right
		}
	}
	return buf.pixels, stride, vp9MLPartitionBorder, vp9MLPartitionBorder
}

// vp9ResetMLPartitionCache clears the per-frame ML partition context
// slots so the next frame re-runs int-pro motion / get_estimated_pred
// for every SB. Called from the frame entry point before the picker
// dispatcher runs.
//
// libvpx counterpart: get_estimated_pred is invoked per SB at
// vp9_encodeframe.c:5314; there is no carry-over between frames since
// x->est_pred is overwritten on each call.
func (e *VP9Encoder) vp9ResetMLPartitionCache(miRows, miCols int) {
	sbRows := (miRows + 7) >> 3
	sbCols := (miCols + 7) >> 3
	need := sbRows * sbCols
	if cap(e.mlPartitionCtx) < need {
		e.mlPartitionCtx = make([]vp9MLPartitionContext, need)
	} else {
		e.mlPartitionCtx = e.mlPartitionCtx[:need]
		for i := range e.mlPartitionCtx {
			e.mlPartitionCtx[i].frameValid = false
		}
	}
	e.mlPartitionCtxLen = need
	e.mlPartitionCtxCols = sbCols
}

// vp9MLPickPartitionEntry resolves (or populates) the per-SB
// vp9MLPartitionContext for the BLOCK_64X64 SB containing (miRow, miCol).
// Mirrors libvpx vp9_encodeframe.c:5314 — get_estimated_pred is invoked
// exactly once per SB on the top-level dispatch; subsequent recursive
// calls into nonrd_pick_partition (at 32x32 / 16x16 / 8x8) re-read the
// same x->est_pred buffer.
//
// Returns nil when the SB cannot be ML-picked (missing LAST_FRAME buffer,
// scaled reference, etc.); the caller falls through to the variance / RD
// picker.
func (e *VP9Encoder) vp9MLPickPartitionEntry(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int,
) *vp9MLPartitionContext {
	if inter == nil || inter.img == nil || inter.dq == nil {
		return nil
	}
	// libvpx ML_BASED_PARTITION fires only on inter frames at speed>=8 with
	// w*h <= 352*288 (vp9_speed_features.c:751-768 + 825-826).
	if e.sf.PartitionSearchType != MlBasedPartition {
		return nil
	}
	// SB-align (miRow, miCol) down to the BLOCK_64X64 top-left.
	sbMiRow := miRow &^ 7
	sbMiCol := miCol &^ 7
	sbRow := sbMiRow >> 3
	sbCol := sbMiCol >> 3
	if e.mlPartitionCtxCols == 0 || sbCol >= e.mlPartitionCtxCols {
		return nil
	}
	idx := sbRow*e.mlPartitionCtxCols + sbCol
	if idx < 0 || idx >= e.mlPartitionCtxLen {
		return nil
	}
	ctx := &e.mlPartitionCtx[idx]
	if ctx.frameValid {
		if !ctx.ready {
			return nil
		}
		return ctx
	}
	// Mark frame-valid up-front; on failure we leave ready=false and
	// subsequent calls return nil without re-attempting the
	// get_estimated_pred work.
	ctx.frameValid = true
	ctx.ready = false

	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	if len(src) == 0 || srcStride <= 0 {
		return nil
	}
	lastSlot, ok := vp9EncoderReferenceSlot(vp9dec.LastFrame)
	if !ok || !e.refFrames[lastSlot].valid {
		return nil
	}
	lastRef, lastStride, lastW, lastH := vp9ReferenceVisiblePlane(&e.refFrames[lastSlot], 0)
	if len(lastRef) == 0 || lastStride <= 0 {
		return nil
	}
	if lastW != srcW || lastH != srcH {
		// ML_BASED_PARTITION's speed-features gate forbids scaled refs
		// (vp9_speed_features.c:751-768 narrows to dynamic-resolution-off
		// configurations); fall back to the variance picker otherwise.
		return nil
	}

	speed := e.vp9SpeedFeatureCPUUsed()
	ctx.sbMiRow = sbMiRow
	ctx.sbMiCol = sbMiCol
	ctx.baseQindex = inter.baseQindex
	ctx.speed = speed
	ctx.pickPredReady = false

	// libvpx get_estimated_pred uses LAST_FRAME (with possible GOLDEN/ALTREF
	// hijack) and runs int-pro motion_estimation on a per-SB sub-bsize
	// (vp9_encodeframe.c:5113-5114). The int-pro search reads up to 32
	// pixels before the SB origin, which on libvpx is handled by the
	// YV12 buffer's 160-pixel border (vpx_scale/yv12config.h:26 —
	// VP9_ENC_BORDER_IN_PIXELS=160). govpx supplies an edge-replicated
	// padded copy here.
	x0 := sbMiCol * common.MiSize
	y0 := sbMiRow * common.MiSize
	if x0 >= srcW || y0 >= srcH || x0 >= lastW || y0 >= lastH {
		return nil
	}
	if recon, reconStride := e.vp9EncoderReconPlane(0); len(recon) != 0 &&
		reconStride > 0 && len(recon)/reconStride > 0 {
		reconRows := len(recon) / reconStride
		for py := range 64 {
			sy := y0 + py
			if sy >= reconRows {
				sy = reconRows - 1
			}
			for px := range 64 {
				sx := x0 + px
				if sx >= reconStride {
					sx = reconStride - 1
				}
				ctx.pickPred[py*64+px] = recon[sy*reconStride+sx]
			}
		}
		ctx.pickPredReady = true
	}

	paddedRef, paddedRefStride, refOriginY, refOriginX := vp9BuildPaddedPlane(
		&e.mlPartitionPaddedLast, lastRef, lastStride, lastW, lastH)
	paddedSrc, paddedSrcStride, srcOriginY, srcOriginX := vp9BuildPaddedPlane(
		&e.mlPartitionPaddedSrc, src, srcStride, srcW, srcH)

	ctx.src = paddedSrc
	ctx.srcStride = paddedSrcStride
	ctx.srcOriginX = srcOriginX
	ctx.srcOriginY = srcOriginY
	ctx.srcVisibleW = srcW
	ctx.srcVisibleH = srcH

	subBsize := encoder.GetEstimatedPredSubBsize(sbMiRow, sbMiCol, miRows, miCols)

	estIn := &encoder.GetEstimatedPredInterInput{
		Bsize:         subBsize,
		Src:           paddedSrc,
		SrcOff:        (srcOriginY+y0)*paddedSrcStride + (srcOriginX + x0),
		SrcStride:     paddedSrcStride,
		LastRef:       paddedRef,
		LastRefOff:    (refOriginY+y0)*paddedRefStride + (refOriginX + x0),
		LastRefStride: paddedRefStride,
		Speed:         speed,
		// x->mv_limits is the BLOCK_64X64 UMV window set by set_offsets
		// (libvpx vp9_encodeframe.c:296-299 + 5109), not the padding
		// border. EncoderMvLimits ports that formula verbatim.
		MvLimits: encoder.EncoderMvLimits(miRows, miCols, sbMiRow, sbMiCol, common.Block64x64),
		// xd->mb_to_*_edge for the BLOCK_64X64 SB (bw = bh = 8 mi),
		// in 1/8-pel units (libvpx vp9_onyxc_int.h:424-427 set_mi_row_col).
		// These bound clamp_mv_to_umv_border_sb inside the inter-predictor
		// convolve so its 64x64 read window stays inside the padded plane.
		MbToTopEdge:    -((sbMiRow * common.MiSize) * 8),
		MbToBottomEdge: (miRows - vp9MLPartitionSbMi - sbMiRow) * common.MiSize * 8,
		MbToLeftEdge:   -((sbMiCol * common.MiSize) * 8),
		MbToRightEdge:  (miCols - vp9MLPartitionSbMi - sbMiCol) * common.MiSize * 8,
	}
	// Inter call path: encoder.GetEstimatedPred handles the keyframe branch on
	// isKeyFrame=true. Inter dispatch goes through the int-pro search +
	// luma convolve. libvpx's get_estimated_pred also leaves SB-level
	// side effects on MACROBLOCK: sb_use_mv_part / sb_mv*_part for
	// combined_motion_search and pred_mv[LAST_FRAME] for vp9_mv_pred's
	// third candidate. Cache those effects on the encoder so the later
	// per-leaf pickmode calls observe the same state.
	chosenRef, intProMV := encoder.GetEstimatedPred(false, estIn, ctx.estPred[:])
	if e.vp9EnsureVarPartSBMotionCaches(miRows, miCols) > idx {
		e.varPartSBUseMvPart[idx] = true
		e.varPartSBMvPart[idx] = intProMV
		if chosenRef != encoder.RefGolden {
			e.varPartSBPredValid[idx] = true
			e.varPartSBPredLast[idx] = intProMV
		}
	}
	ctx.ready = true
	return ctx
}

// vp9MLPredictVarPartitioning ports ml_predict_var_partitioning at libvpx
// vp9/encoder/vp9_encodeframe.c:4530-4596 verbatim.
//
// Inputs:
//   - bsize: one of BLOCK_64X64, BLOCK_32X32, BLOCK_16X16. Other sizes
//     return -1 (libvpx returns -1 for BLOCK_8X8 and asserts on others).
//   - miRow/miCol: the current recursive position within the SB.
//   - ctx: the per-SB context populated by vp9MLPickPartitionEntry.
//
// Returns:
//   - vp9MLPredictNone (0)  : NN voted PARTITION_NONE — commit current size.
//   - vp9MLPredictSplit (3) : NN voted PARTITION_SPLIT — recurse.
//   - vp9MLPredictNone1 (-1): no confidence — caller falls back.
func vp9MLPredictVarPartitioning(bsize common.BlockSize, miRow, miCol int,
	ctx *vp9MLPartitionContext,
) vp9MLPredictResult {
	if ctx == nil || !ctx.ready {
		return vp9MLPredictNone1
	}

	// libvpx vp9_encodeframe.c:4536-4544 — only the three NN-equipped
	// sizes return a config; BLOCK_8X8 returns -1.
	var nnConfig *encoder.NNConfig
	switch bsize {
	case common.Block64x64:
		nnConfig = &encoder.VarPartNNConfig64
	case common.Block32x32:
		nnConfig = &encoder.VarPartNNConfig32
	case common.Block16x16:
		nnConfig = &encoder.VarPartNNConfig16
	default:
		return vp9MLPredictNone1
	}

	// libvpx vp9_encodeframe.c:4549 — const float thresh =
	//   cpi->oxcf.speed <= 5 ? 1.25f : 0.0f;
	var thresh float32
	if ctx.speed <= 5 {
		thresh = 1.25
	} else {
		thresh = 0.0
	}

	// libvpx vp9_encodeframe.c:4551 — const int dc_q =
	//   vp9_dc_quant(cm->base_qindex, 0, cm->bit_depth);
	dcQ := int(vp9dec.VpxDcQuant(ctx.baseQindex, 0, vp9dec.BitDepth8))

	// libvpx vp9_encodeframe.c:4555 — feature[0] = logf((dc_q*dc_q)/256.0+1.0).
	var features [encoder.NNFeatures]float32
	features[0] = float32(math.Log(float64(dcQ*dcQ)/256.0 + 1.0))

	// libvpx vp9_encodeframe.c:4558-4565:
	//   const int bs = 4 * num_4x4_blocks_wide_lookup[bsize];
	//   const BLOCK_SIZE subsize = get_subsize(bsize, PARTITION_SPLIT);
	//   const int sb_offset_row = 8 * (mi_row & 7);
	//   const int sb_offset_col = 8 * (mi_col & 7);
	bs := 4 * int(common.Num4x4BlocksWideLookup[bsize])
	sbOffsetRow := 8 * (miRow & 7)
	sbOffsetCol := 8 * (miCol & 7)

	// libvpx vp9_encodeframe.c:4562-4565:
	//   const uint8_t *pred = x->est_pred + sb_offset_row * 64 + sb_offset_col;
	//   const uint8_t *src = x->plane[0].src.buf;
	//   const int src_stride = x->plane[0].src.stride;
	//   const int pred_stride = 64;
	predStride := 64
	predOff := sbOffsetRow*64 + sbOffsetCol

	// libvpx vp9_encodeframe.c:4567-4571:
	//   const unsigned int var = cpi->fn_ptr[bsize].vf(src, src_stride, pred,
	//                                                  pred_stride, &sse);
	//   const float factor = (var == 0) ? 1.0f : (1.0f / (float)var);
	//
	// Reads against the border-padded source (vp9BuildPaddedPlane copy)
	// so edge SBs read replicated edge pixels rather than OOB. Matches
	// libvpx's YV12 source border (vpx_scale/yv12config.h:26).
	srcX0 := ctx.sbMiCol * common.MiSize
	srcY0 := ctx.sbMiRow * common.MiSize
	blockSrcX := ctx.srcOriginX + srcX0 + sbOffsetCol
	blockSrcY := ctx.srcOriginY + srcY0 + sbOffsetRow

	varWhole := vp9PredVariance(ctx.src, ctx.srcStride, blockSrcX, blockSrcY,
		ctx.estPred[:], predStride, predOff/predStride, predOff%predStride,
		bs, bs)
	var factor float32
	if varWhole == 0 {
		factor = 1.0
	} else {
		factor = 1.0 / float32(varWhole)
	}

	// libvpx vp9_encodeframe.c:4573 — feature[1] = logf((float)var + 1.0f).
	features[1] = float32(math.Log(float64(varWhole) + 1.0))

	// libvpx vp9_encodeframe.c:4574-4585 — for i in 0..4:
	//   const int x_idx = (i & 1) * bs / 2;
	//   const int y_idx = (i >> 1) * bs / 2;
	//   const int src_offset = y_idx * src_stride + x_idx;
	//   const int pred_offset = y_idx * pred_stride + x_idx;
	//   const unsigned int sub_var = cpi->fn_ptr[subsize].vf(...);
	//   const float var_ratio = (var == 0) ? 1.0f : factor * (float)sub_var;
	//   features[feature_idx++] = var_ratio;
	hbs := bs / 2
	for i := range 4 {
		xIdx := (i & 1) * hbs
		yIdx := (i >> 1) * hbs
		subVar := vp9PredVariance(ctx.src, ctx.srcStride,
			blockSrcX+xIdx, blockSrcY+yIdx,
			ctx.estPred[:], predStride,
			(predOff/predStride)+yIdx, (predOff%predStride)+xIdx,
			hbs, hbs)
		var varRatio float32
		if varWhole == 0 {
			varRatio = 1.0
		} else {
			varRatio = factor * float32(subVar)
		}
		features[2+i] = varRatio
	}

	// libvpx vp9_encodeframe.c:4589-4592:
	//   nn_predict(features, nn_config, score);
	//   if (score[0] > thresh) return PARTITION_SPLIT;
	//   if (score[0] < -thresh) return PARTITION_NONE;
	//   return -1;
	var score [encoder.NNLabels]float32
	encoder.NNPredict(features[:], nnConfig, score[:])
	if score[0] > thresh {
		return vp9MLPredictSplit
	}
	if score[0] < -thresh {
		return vp9MLPredictNone
	}
	return vp9MLPredictNone1
}

// vp9PredVariance computes the libvpx-equivalent variance of (src - pred)
// over a block. Inputs are (src, srcStride, srcX, srcY) and (pred, predStride,
// predY, predX) — both indexed (y, x) in pixel space. Returns the libvpx
// variance: SSE - (sum*sum)/N, matching vpx_variance_NxN's pre-rounding form.
func vp9PredVariance(src []uint8, srcStride int, srcX, srcY int,
	pred []uint8, predStride int, predY, predX int,
	w, h int,
) uint32 {
	var sum int64
	var sse uint64
	for y := range h {
		srcRow := src[(srcY+y)*srcStride+srcX:]
		predRow := pred[(predY+y)*predStride+predX:]
		for x := range w {
			diff := int64(srcRow[x]) - int64(predRow[x])
			sum += diff
			sse += uint64(diff * diff)
		}
	}
	n := int64(w * h)
	if n <= 0 {
		return 0
	}
	meanSquares := uint64((sum * sum) / n)
	if sse <= meanSquares {
		return 0
	}
	return uint32(sse - meanSquares)
}

// vp9NonrdPickPartition ports the recursive ML_BASED_PARTITION decision body
// of nonrd_pick_partition (libvpx vp9_encodeframe.c:4598-4855) restricted to
// the ml_based_partitioning=1 path:
//
//   - do_rect = 0 (vp9_encodeframe.c:4633 + 4660-4661 force it for speed>=5
//     and ML-based).
//   - partition_none_allowed / partition_horz_allowed / partition_vert_allowed
//     follow the forced-edge-split logic (vp9_encodeframe.c:4617-4626).
//   - When both partition_none_allowed && do_split, ml_predict_var_partitioning
//     decides (vp9_encodeframe.c:4662-4667):
//     PARTITION_NONE  -> do_split=0
//     PARTITION_SPLIT -> partition_none_allowed=0
//     -1              -> both allowed (RD compare).
//
// Returns:
//   - (root, true)      : commit BLOCK_64X64 / 32x32 / 16x16 at the current
//     level (PARTITION_NONE outcome).
//   - (splitSize, true) : recurse to next level (PARTITION_SPLIT outcome).
//   - (BlockInvalid, false) : ML undecided — caller falls back to the
//     variance / RD picker.
//
// Edge cases (forced-split honoring):
//   - If both row+ms >= miRows and col+ms >= miCols → BLOCK_64X64 is forced
//     to the edge geometry. govpx's existing partition-lookup table handles
//     this; we return root.
//   - If only one axis triggers a forced split, we still funnel through the
//     NN. The downstream caller honours the partition direction.
//
// The -1 branch ("no confidence") invokes the libvpx-faithful
// PARTITION_NONE vs PARTITION_SPLIT RDCOST compare ported
// from vp9/encoder/vp9_encodeframe.c:4675-4746. The two candidates are
// scored via pickVP9InterReferenceMode (PARTITION_NONE — libvpx's
// nonrd_pick_sb_modes at vp9_encodeframe.c:4677 invoking
// vp9_pick_inter_mode at vp9_pickmode.c:1696) and
// scoreVP9InterPartitionSplit (PARTITION_SPLIT — libvpx's recursive
// nonrd_pick_partition call at vp9_encodeframe.c:4725-4727) plus the
// partition_cost token rate at vp9_encodeframe.c:4686 / 4715. The picker
// commits to whichever candidate has the smaller RDCOST. The dispatcher in
// vp9_encoder.go now reaches this path by default for ML_BASED_PARTITION,
// matching libvpx's use_ml_based_partitioning predicate.
//
// libvpx ref: vp9/encoder/vp9_encodeframe.c:4598-4855 nonrd_pick_partition
// with use_ml_based_partitioning=1.
func (e *VP9Encoder) vp9NonrdPickPartition(ctx *vp9MLPartitionContext,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
) (common.BlockSize, bool) {
	if ctx == nil || !ctx.ready {
		return common.BlockInvalid, false
	}

	// libvpx vp9_encodeframe.c:4608 — const int ms = num_8x8_blocks_wide_lookup[bsize]/2.
	ms := int(common.Num8x8BlocksWideLookup[bsize]) / 2

	// libvpx vp9_encodeframe.c:4614 — do_split = bsize >= BLOCK_8X8.
	doSplit := bsize >= common.Block8x8

	// libvpx vp9_encodeframe.c:4617-4618 — forced rectangular splits at edges.
	forceHorzSplit := miRow+ms >= miRows
	forceVertSplit := miCol+ms >= miCols

	// libvpx vp9_encodeframe.c:4622 — partition_none_allowed = !force_horz_split
	// && !force_vert_split.
	partitionNoneAllowed := !forceHorzSplit && !forceVertSplit

	// libvpx vp9_encodeframe.c:4644-4653 — auto_min_max_partition_size gate.
	// ML_BASED dispatch pins max/min to 64x64/8x8 (vp9_encodeframe.c:5315-5316).
	maxPartitionSize := e.sf.DefaultMaxPartitionSize
	minPartitionSize := e.sf.DefaultMinPartitionSize
	if e.sf.PartitionSearchType == MlBasedPartition {
		maxPartitionSize = common.Block64x64
		minPartitionSize = common.Block8x8
	}
	if maxPartitionSize == 0 {
		maxPartitionSize = common.Block64x64
	}
	if minPartitionSize == 0 {
		minPartitionSize = common.Block4x4
	}
	if e.sf.AutoMinMaxPartitionSize != AutoMinMaxNotInUse {
		partitionNoneAllowed = partitionNoneAllowed &&
			bsize <= maxPartitionSize && bsize >= minPartitionSize
		doSplit = doSplit && bsize > minPartitionSize
	}

	// libvpx vp9_encodeframe.c:4660-4667 — ML predictor dispatch.
	if partitionNoneAllowed && doSplit {
		pred := vp9MLPredictVarPartitioning(bsize, miRow, miCol, ctx)
		switch pred {
		case vp9MLPredictNone:
			// libvpx: do_split = 0 — commit current bsize.
			return bsize, true
		case vp9MLPredictSplit:
			// libvpx: partition_none_allowed = 0 — recurse.
			splitSize, ok := vp9MLSplitSize(bsize)
			if !ok {
				return bsize, true
			}
			return splitSize, true
		default:
			// -1: defer to caller's RD-compare fallback (libvpx
			// vp9_encodeframe.c:4675-4746). The dispatcher in
			// pickVP9InterPartitionBlockSize invokes
			// vp9NonrdPickPartitionRDFallback when this path returns
			// (BlockInvalid, false).
			return common.BlockInvalid, false
		}
	}

	// Forced edge: split is mandatory. Recurse to the split size.
	if !partitionNoneAllowed && doSplit {
		splitSize, ok := vp9MLSplitSize(bsize)
		if !ok {
			return bsize, true
		}
		return splitSize, true
	}

	// No split possible (BLOCK_4X4 or smaller after auto_min_max). Commit.
	return bsize, true
}

// vp9NonrdPickPartitionRDFallback ports the PARTITION_NONE vs
// PARTITION_SPLIT RDCOST compare body of libvpx nonrd_pick_partition at
// vp9/encoder/vp9_encodeframe.c:4675-4746. Invoked from the
// pickVP9InterPartitionBlockSize dispatcher on the NN=-1 ("no
// confidence") branch in vp9MLPredictVarPartitioning. The two candidates
// are:
//
//   - PARTITION_NONE (libvpx vp9_encodeframe.c:4676-4707): one call to
//     nonrd_pick_sb_modes at the current bsize. govpx's equivalent is
//     pickVP9InterReferenceMode, which dispatches to
//     pickVP9InterReferenceModeNonRD when sf->use_nonrd_pick_mode != 0
//     (libvpx vp9_pickmode.c:1696 vp9_pick_inter_mode). The score is the
//     RDCOST already returned by the picker (Lagrangian Rate + Dist).
//     Adds cpi->partition_cost[pl][PARTITION_NONE] at line 4686.
//
//   - PARTITION_SPLIT (libvpx vp9_encodeframe.c:4713-4746): four
//     recursive calls to nonrd_pick_partition at the split sub-bsize.
//     govpx's equivalent is scoreVP9NonrdMLPartitionSplit: it keeps the
//     recursive ML predictor / NONE-vs-SPLIT fallback shape and deliberately
//     does not enter the generic inter RD splitter, because speed-8 libvpx
//     has rectangular partitions disabled on this lane. Adds
//     cpi->partition_cost[pl][PARTITION_SPLIT] at line 4715.
//
// The picker commits to whichever candidate has the lower aggregate
// RDCOST. On a tie (or scorer failure on the split candidate) we fall
// through to BlockInvalid which routes the caller into the variance / RD
// fallback path — matching libvpx's behaviour when sum_rdc.rdcost
// >= best_rdc.rdcost (line 4738) leaves best_rdc holding the PARTITION_-
// NONE candidate.
//
// libvpx call shape:
//
//	if (partition_none_allowed) {
//	  nonrd_pick_sb_modes(cpi, ...);                          // 4677
//	  this_rdc.rate += cpi->partition_cost[pl][PARTITION_NONE]; // 4686
//	  this_rdc.rdcost = RDCOST(...);                            // 4687
//	  best_rdc = this_rdc;                                      // 4690
//	}
//	if (do_split) {
//	  sum_rdc.rate += cpi->partition_cost[pl][PARTITION_SPLIT]; // 4715
//	  sum_rdc.rdcost = RDCOST(...);                             // 4716
//	  for (i = 0; i < 4; ++i)                                   // 4718
//	    nonrd_pick_partition(...);                              // 4725
//	  if (sum_rdc.rdcost < best_rdc.rdcost) {                   // 4738
//	    best_rdc = sum_rdc; pc_tree->partitioning = SPLIT;      // 4740
//	  }
//	}
func (e *VP9Encoder) vp9NonrdPickPartitionRDFallback(
	inter *vp9InterEncodeState,
	tile vp9dec.TileBounds,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	miRows, miCols, miRow, miCol int,
	bsize common.BlockSize,
) (common.BlockSize, bool) {
	if inter == nil {
		return common.BlockInvalid, false
	}

	// libvpx vp9_encodeframe.c:4608 — const int ms =
	// num_8x8_blocks_wide_lookup[bsize] / 2.
	ms := int(common.Num8x8BlocksWideLookup[bsize]) / 2

	// libvpx vp9_encodeframe.c:4614 — do_split = bsize >= BLOCK_8X8.
	doSplit := bsize >= common.Block8x8

	// libvpx vp9_encodeframe.c:4617-4618 — forced rectangular splits at
	// edges. The RD compare only fires when both PARTITION_NONE and
	// PARTITION_SPLIT are reachable; forced-edge cases are handled by
	// vp9NonrdPickPartition's edge branch above.
	forceHorzSplit := miRow+ms >= miRows
	forceVertSplit := miCol+ms >= miCols
	partitionNoneAllowed := !forceHorzSplit && !forceVertSplit
	maxPartitionSize := e.sf.DefaultMaxPartitionSize
	minPartitionSize := e.sf.DefaultMinPartitionSize
	if e.sf.PartitionSearchType == MlBasedPartition {
		maxPartitionSize = common.Block64x64
		minPartitionSize = common.Block8x8
	}
	if maxPartitionSize == 0 {
		maxPartitionSize = common.Block64x64
	}
	if minPartitionSize == 0 {
		minPartitionSize = common.Block4x4
	}
	if e.sf.AutoMinMaxPartitionSize != AutoMinMaxNotInUse {
		partitionNoneAllowed = partitionNoneAllowed &&
			bsize <= maxPartitionSize && bsize >= minPartitionSize
		doSplit = doSplit && bsize > minPartitionSize
	}
	if !partitionNoneAllowed || !doSplit {
		return common.BlockInvalid, false
	}

	if _, ok := vp9MLSplitSize(bsize); !ok {
		return common.BlockInvalid, false
	}

	// Source for partition rate-cost probabilities mirrors the RD
	// path at vp9_encoder.go:5892-5895 — prefer the pre-WriteCompressed-
	// Header snapshot inter.selectFc so the prepass and write pass use
	// the same probs.
	rateCostProbs := partitionProbs
	if inter != nil {
		rateCostProbs = &inter.selectFc.PartitionProb
	}

	qindex := e.vp9EncoderModeDecisionQIndex()
	snap, snapOK := e.saveVP9NonrdMLPartitionSnapshot(inter, miRows, miCols,
		miRow, miCol, bsize)
	if !snapOK {
		return common.BlockInvalid, false
	}

	rd, ok := e.scoreVP9NonrdMLPartitionCompare(inter, tile, rateCostProbs,
		miRows, miCols, miRow, miCol, bsize, qindex)
	e.restoreVP9NonrdMLPartitionSnapshot(inter, snap)
	e.releaseVP9NonrdMLPartitionSnapshot(snap)
	if !ok {
		return common.BlockInvalid, false
	}
	return rd.target, true
}

type vp9NonrdMLPartitionSnapshot struct {
	ref      *vp9ReferenceFrame
	recon    vp9PartitionReconSnapshot
	pickPred vp9MLPickPredSnapshot
	context  vp9PartitionContextSnapshot
	mi       [64]vp9dec.NeighborMi
	miRows   int
	miCols   int
	miRow    int
	miCol    int
	rows     int
	cols     int
	ok       bool
}

func (e *VP9Encoder) saveVP9NonrdMLPartitionSnapshot(
	inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int,
	bsize common.BlockSize,
) (vp9NonrdMLPartitionSnapshot, bool) {
	var snap vp9NonrdMLPartitionSnapshot
	if inter == nil {
		return snap, false
	}
	recon, ok := e.saveVP9PartitionReconSnapshot(miRow, miCol, bsize)
	if !ok {
		return snap, false
	}
	rootRows := int(common.Num8x8BlocksHighLookup[bsize])
	rootCols := int(common.Num8x8BlocksWideLookup[bsize])
	rows, cols, miOK := e.snapshotVP9MiRect(miRows, miCols, miRow, miCol,
		rootRows, rootCols, snap.mi[:])
	context, ctxOK := e.snapshotVP9PartitionContexts(miRow, miCol, bsize)
	if !miOK || !ctxOK {
		e.releaseVP9PartitionReconSnapshot(recon)
		return snap, false
	}
	snap.ref = inter.ref
	snap.recon = recon
	snap.pickPred = e.saveVP9MLPickPredSnapshot(inter, miRows, miCols,
		miRow, miCol)
	snap.context = context
	snap.miRows = miRows
	snap.miCols = miCols
	snap.miRow = miRow
	snap.miCol = miCol
	snap.rows = rows
	snap.cols = cols
	snap.ok = true
	return snap, true
}

func (e *VP9Encoder) restoreVP9NonrdMLPartitionSnapshot(
	inter *vp9InterEncodeState,
	snap vp9NonrdMLPartitionSnapshot,
) {
	if !snap.ok || inter == nil {
		return
	}
	e.restoreVP9MiRect(snap.miRows, snap.miCols, snap.miRow, snap.miCol,
		snap.rows, snap.cols, snap.mi[:])
	e.restoreVP9PartitionContexts(snap.context)
	e.restoreVP9MLPickPredSnapshot(snap.pickPred)
	e.restoreVP9PartitionReconSnapshotPixels(snap.recon)
	inter.ref = snap.ref
}

func (e *VP9Encoder) releaseVP9NonrdMLPartitionSnapshot(
	snap vp9NonrdMLPartitionSnapshot,
) {
	if !snap.ok {
		return
	}
	e.releaseVP9PartitionReconSnapshot(snap.recon)
}

func (e *VP9Encoder) scoreVP9NonrdMLPartitionTree(
	inter *vp9InterEncodeState,
	tile vp9dec.TileBounds,
	rateCostProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	miRows, miCols, miRow, miCol int,
	bsize common.BlockSize,
	qindex int,
) (vp9InterPartitionRD, bool) {
	if bsize < common.Block8x8 {
		return e.scoreVP9InterPartitionLeaf(inter, tile, miRows, miCols,
			miRow, miCol, bsize, 0, false)
	}
	if bsize >= common.BlockSizes {
		return vp9InterPartitionRD{}, false
	}
	ctx := e.vp9MLPickPartitionEntry(inter, miRows, miCols, miRow, miCol)
	if ctx == nil || !ctx.ready {
		return vp9InterPartitionRD{}, false
	}
	picked, ok := e.vp9NonrdPickPartition(ctx, miRows, miCols, miRow,
		miCol, bsize)
	if !ok {
		return e.scoreVP9NonrdMLPartitionCompare(inter, tile, rateCostProbs,
			miRows, miCols, miRow, miCol, bsize, qindex)
	}
	if picked == bsize {
		hasRows, hasCols := vp9NonrdPartitionHasRowsCols(miRows, miCols,
			miRow, miCol, bsize)
		return e.scoreVP9InterPartitionNone(inter, tile, rateCostProbs,
			miRows, miCols, miRow, miCol, bsize, hasRows, hasCols,
			qindex)
	}
	splitSize, splitOK := vp9MLSplitSize(bsize)
	if splitOK && picked == splitSize {
		hasRows, hasCols := vp9NonrdPartitionHasRowsCols(miRows, miCols,
			miRow, miCol, bsize)
		return e.scoreVP9NonrdMLPartitionSplit(inter, tile, rateCostProbs,
			miRows, miCols, miRow, miCol, bsize, splitSize, hasRows,
			hasCols, qindex)
	}
	return vp9InterPartitionRD{}, false
}

func (e *VP9Encoder) scoreVP9NonrdMLPartitionCompare(
	inter *vp9InterEncodeState,
	tile vp9dec.TileBounds,
	rateCostProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	miRows, miCols, miRow, miCol int,
	bsize common.BlockSize,
	qindex int,
) (vp9InterPartitionRD, bool) {
	splitSize, ok := vp9MLSplitSize(bsize)
	if !ok {
		return vp9InterPartitionRD{}, false
	}
	hasRows, hasCols := vp9NonrdPartitionHasRowsCols(miRows, miCols,
		miRow, miCol, bsize)
	snap, snapOK := e.saveVP9NonrdMLPartitionSnapshot(inter, miRows, miCols,
		miRow, miCol, bsize)
	if !snapOK {
		return vp9InterPartitionRD{}, false
	}

	e.restoreVP9NonrdMLPartitionSnapshot(inter, snap)
	noneRD, noneOK := e.scoreVP9InterPartitionNone(inter, tile, rateCostProbs,
		miRows, miCols, miRow, miCol, bsize, hasRows, hasCols, qindex)

	e.restoreVP9NonrdMLPartitionSnapshot(inter, snap)
	splitRD, splitOK := e.scoreVP9NonrdMLPartitionSplit(inter, tile,
		rateCostProbs, miRows, miCols, miRow, miCol, bsize, splitSize,
		hasRows, hasCols, qindex)

	bestTarget := common.BlockInvalid
	if noneOK {
		bestTarget = bsize
	}
	if splitOK && (!noneOK || splitRD.score < noneRD.score) {
		bestTarget = splitSize
	}
	if bestTarget == common.BlockInvalid {
		e.restoreVP9NonrdMLPartitionSnapshot(inter, snap)
		e.releaseVP9NonrdMLPartitionSnapshot(snap)
		return vp9InterPartitionRD{}, false
	}

	e.restoreVP9NonrdMLPartitionSnapshot(inter, snap)
	var committed vp9InterPartitionRD
	var committedOK bool
	switch bestTarget {
	case bsize:
		committed, committedOK = e.scoreVP9InterPartitionNone(inter, tile, rateCostProbs,
			miRows, miCols, miRow, miCol, bsize, hasRows, hasCols,
			qindex)
	case splitSize:
		committed, committedOK = e.scoreVP9NonrdMLPartitionSplit(inter, tile, rateCostProbs,
			miRows, miCols, miRow, miCol, bsize, splitSize, hasRows,
			hasCols, qindex)
	}
	if !committedOK {
		e.restoreVP9NonrdMLPartitionSnapshot(inter, snap)
		e.releaseVP9NonrdMLPartitionSnapshot(snap)
		return vp9InterPartitionRD{}, false
	}
	e.releaseVP9NonrdMLPartitionSnapshot(snap)
	return committed, true
}

func (e *VP9Encoder) scoreVP9NonrdMLPartitionSplit(
	inter *vp9InterEncodeState,
	tile vp9dec.TileBounds,
	rateCostProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	miRows, miCols, miRow, miCol int,
	root, child common.BlockSize,
	hasRows, hasCols bool,
	qindex int,
) (vp9InterPartitionRD, bool) {
	rate := 0
	var distortion uint64
	ctx := vp9dec.PartitionPlaneContext(e.aboveSegCtx, e.leftSegCtx,
		miRow, miCol, root)
	// libvpx nonrd_pick_partition adds the unconditional full-tree
	// cpi->partition_cost[pl][PARTITION_SPLIT] (vp9_encodeframe.c:4715), not
	// the writer's hasRows/hasCols-clamped form. See
	// vp9_fullrd_partition_cost.go.
	rate += RDPartitionCost(rateCostProbs, ctx, common.PartitionSplit)
	if child < common.Block8x8 {
		rd, ok := e.scoreVP9InterPartitionLeaf(inter, tile, miRows, miCols,
			miRow, miCol, child, 0, false)
		if !ok {
			return vp9InterPartitionRD{}, false
		}
		rate += rd.rate
		distortion += rd.distortion
	} else {
		stepMi := int(common.Num8x8BlocksWideLookup[child])
		for rowOff := 0; rowOff <= stepMi; rowOff += stepMi {
			for colOff := 0; colOff <= stepMi; colOff += stepMi {
				if miRow+rowOff >= miRows || miCol+colOff >= miCols {
					continue
				}
				rd, ok := e.scoreVP9NonrdMLPartitionTree(inter, tile,
					rateCostProbs, miRows, miCols, miRow+rowOff,
					miCol+colOff, child, qindex)
				if !ok {
					return vp9InterPartitionRD{}, false
				}
				rate += rd.rate
				distortion += rd.distortion
			}
		}
	}
	e.updateVP9PartitionContextForChoice(miRow, miCol, root,
		common.PartitionSplit, child)
	return vp9InterPartitionRD{
		target:     child,
		rate:       rate,
		distortion: distortion,
		score:      e.vp9InterModeScore(distortion, rate, qindex),
	}, true
}

func vp9NonrdPartitionHasRowsCols(miRows, miCols, miRow, miCol int,
	bsize common.BlockSize,
) (bool, bool) {
	ms := int(common.Num8x8BlocksWideLookup[bsize]) / 2
	return miRow+ms < miRows, miCol+ms < miCols
}

// vp9MLSplitSize maps an ML-eligible bsize to its split (PARTITION_SPLIT) sub-
// block size. Mirrors libvpx's get_subsize(bsize, PARTITION_SPLIT) at the
// three ML levels: 64x64 -> 32x32, 32x32 -> 16x16, 16x16 -> 8x8 (libvpx
// vp9/common/vp9_common_data.c subsize_lookup).
func vp9MLSplitSize(bsize common.BlockSize) (common.BlockSize, bool) {
	switch bsize {
	case common.Block64x64:
		return common.Block32x32, true
	case common.Block32x32:
		return common.Block16x16, true
	case common.Block16x16:
		return common.Block8x8, true
	default:
		return common.BlockInvalid, false
	}
}
