package govpx

import (
	"image"

	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// VP9 per-SB content / ARF / RC state buffers ported verbatim from libvpx
// v1.16.0.
//
// libvpx parity references:
//   - vp9/encoder/vp9_encoder.h:883     content_state_sb_fd field
//   - vp9/encoder/vp9_encoder.h:891-892 count_arf_frame_usage,
//                                       count_lastgolden_frame_usage fields
//   - vp9/encoder/vp9_speed_features.c:676-683 content_state_sb_fd allocation
//   - vp9/encoder/vp9_speed_features.c:828-844 count_arf_frame_usage and
//                                              count_lastgolden_frame_usage
//                                              allocation + FIXED_PARTITION
//                                              override on ARF overlay
//   - vp9/encoder/vp9_encoder.c:4079-4082 content_state_sb_fd reset on
//                                          SVC/resize transitions
//   - vp9/encoder/vp9_encodeframe.c:1238-1244 content_state_sb_fd
//                                              increment/reset per SB
//   - vp9/encoder/vp9_encodeframe.c:1346-1347 content_state_sb_fd read
//                                              into x->last_sb_high_content
//   - vp9/encoder/vp9_encodeframe.c:5363-5371 count_arf_frame_usage and
//                                              count_lastgolden_frame_usage
//                                              per-SB write
//   - vp9/encoder/vp9_ratectrl.c:1802-1819 update_altref_usage —
//                                          recomputes rc.perc_arf_usage
//                                          from the per-SB counters
//   - vp9/encoder/vp9_ratectrl.h:118 is_src_frame_alt_ref field

// vp9SbStateMiBlock is the libvpx mi-units-per-content-state-SB constant.
// libvpx writes counters at SB boundaries with stride
// (mi_stride >> 3) and row stride (mi_rows >> 3) — see vp9_speed_features.c:680
// (cpi->content_state_sb_fd alloc) and vp9_encodeframe.c:5367 (sb_offset
// computation), which equals 8 mi units per SB step.
const vp9SbStateMiBlock = 8

// vp9ContentStateSbFdSize computes the libvpx allocation size for the per-SB
// content-state buffer:
//
//	(mi_stride >> 3) * ((mi_rows >> 3) + 1) * sizeof(uint8_t)
//
// libvpx: vp9_speed_features.c:680.
func vp9ContentStateSbFdSize(miStride, miRows int) int {
	if miStride <= 0 || miRows < 0 {
		return 0
	}
	return (miStride >> 3) * ((miRows >> 3) + 1)
}

// vp9CalcMiSize ports libvpx's calc_mi_size (vp9_onyxc_int.h:416):
//
//	static INLINE int calc_mi_size(int len) {
//	  return len + MI_BLOCK_SIZE;
//	}
//
// MI_BLOCK_SIZE is 8 (vp9_onyxc_int.h:48). mi_stride / mi_size buffers in
// libvpx are sized as calc_mi_size(mi_cols) / calc_mi_size(mi_rows).
//
// libvpx: vp9_onyxc_int.h:416 calc_mi_size,
// vp9_alloccommon.c:26 *mi_stride = calc_mi_size(*mi_cols).
func vp9CalcMiSize(length int) int {
	return length + 8
}

// vp9MiDimensionsForFrame returns (miCols, miRows, miStride) for the given
// frame dimensions, mirroring the libvpx common allocation path:
//
//	mi_cols  = (width  + MI_SIZE - 1) >> MI_SIZE_LOG2   (==> aligned_width / 8)
//	mi_rows  = (height + MI_SIZE - 1) >> MI_SIZE_LOG2
//	mi_stride = calc_mi_size(mi_cols)
//
// libvpx: vp9_alloccommon.c:21-27 set_mb_mi.
func vp9MiDimensionsForFrame(width, height int) (miCols, miRows, miStride int) {
	miCols = (width + 7) >> 3
	miRows = (height + 7) >> 3
	miStride = vp9CalcMiSize(miCols)
	return
}

// vp9SbOffsetForMi returns the per-SB index libvpx uses to address
// count_arf_frame_usage / count_lastgolden_frame_usage / content_state_sb_fd:
//
//	((mi_cols + 7) >> 3) * (mi_row >> 3) + (mi_col >> 3)
//
// libvpx: vp9_encodeframe.c:5367 sboffset,
// vp9_encodeframe.c:1232 sb_offset (with the same expression).
//
// Note: the buffer is *sized* with mi_stride (= calc_mi_size(mi_cols)) but
// addressed with ((mi_cols + 7) >> 3); since mi_stride = mi_cols + 8, the
// addressed range fits trivially inside the allocation.
func vp9SbOffsetForMi(miRow, miCol, miCols int) int {
	return ((miCols+7)>>3)*(miRow>>3) + (miCol >> 3)
}

// vp9EnsureContentStateSbFd allocates cpi->content_state_sb_fd lazily, mirroring
// libvpx's vp9_speed_features.c:676-683:
//
//	if (cpi->content_state_sb_fd == NULL &&
//	    (!cpi->use_svc ||
//	     svc->spatial_layer_id == svc->number_spatial_layers - 1)) {
//	  CHECK_MEM_ERROR(&cm->error, cpi->content_state_sb_fd,
//	                  (uint8_t *)vpx_calloc(
//	                      (cm->mi_stride >> 3) * ((cm->mi_rows >> 3) + 1),
//	                      sizeof(uint8_t)));
//	}
//
// govpx is single-layer; the !use_svc clause is always satisfied. When the
// caller resizes the frame the existing buffer is freed and re-allocated, to
// mirror the rest of libvpx's free + CHECK_MEM_ERROR pattern on resize.  The
// buffer is zeroed on allocation, exactly like vpx_calloc.
func (e *VP9Encoder) vp9EnsureContentStateSbFd(width, height int) {
	if e == nil {
		return
	}
	miCols, miRows, miStride := vp9MiDimensionsForFrame(width, height)
	size := vp9ContentStateSbFdSize(miStride, miRows)
	if size <= 0 {
		return
	}
	if e.contentStateSbFd != nil &&
		e.contentStateSbFdMiCols == miCols &&
		e.contentStateSbFdMiRows == miRows &&
		e.contentStateSbFdMiStride == miStride {
		return
	}
	e.contentStateSbFd = make([]uint8, size)
	e.contentStateSbFdMiCols = miCols
	e.contentStateSbFdMiRows = miRows
	e.contentStateSbFdMiStride = miStride
}

// vp9ResetContentStateSbFd ports libvpx vp9_encoder.c:4079-4082:
//
//	if (cpi->content_state_sb_fd != NULL)
//	  memset(cpi->content_state_sb_fd, 0,
//	         (cm->mi_stride >> 3) * ((cm->mi_rows >> 3) + 1) *
//	             sizeof(*cpi->content_state_sb_fd));
//
// libvpx invokes this on SVC base-layer / resize transitions so the per-SB
// frame-distance counter restarts from zero. govpx exposes it as a stand-alone
// helper because the encoder calls it from the same setup-frame paths.
func (e *VP9Encoder) vp9ResetContentStateSbFd() {
	if e == nil || e.contentStateSbFd == nil {
		return
	}
	for i := range e.contentStateSbFd {
		e.contentStateSbFd[i] = 0
	}
}

// vp9UpdateContentStateSbFd ports libvpx vp9_encodeframe.c:1238-1244:
//
//	if (cpi->content_state_sb_fd != NULL) {
//	  if (tmp_sad < avg_source_sad_threshold2) {
//	    if (cpi->content_state_sb_fd[sb_offset] < 255)
//	      cpi->content_state_sb_fd[sb_offset]++;
//	  } else {
//	    cpi->content_state_sb_fd[sb_offset] = 0;
//	  }
//	}
//
// lowSourceSad mirrors `tmp_sad < avg_source_sad_threshold2`.
func (e *VP9Encoder) vp9UpdateContentStateSbFd(sbOffset int, lowSourceSad bool) {
	if e == nil || e.contentStateSbFd == nil {
		return
	}
	if sbOffset < 0 || sbOffset >= len(e.contentStateSbFd) {
		return
	}
	if lowSourceSad {
		if e.contentStateSbFd[sbOffset] < 255 {
			e.contentStateSbFd[sbOffset]++
		}
	} else {
		e.contentStateSbFd[sbOffset] = 0
	}
}

// vp9ReadContentStateSbFd ports libvpx vp9_encodeframe.c:1346-1347:
//
//	if (cpi->content_state_sb_fd != NULL)
//	  x->last_sb_high_content = cpi->content_state_sb_fd[sb_offset2];
//
// Returns zero when the buffer is disabled. libvpx leaves
// x->last_sb_high_content at whatever the previous frame stored; govpx
// returns the libvpx-default uninitialized value of 0 in that case so
// downstream gates that test `> 0` short-circuit cleanly.
func (e *VP9Encoder) vp9ReadContentStateSbFd(sbOffset int) uint8 {
	if e == nil || e.contentStateSbFd == nil {
		return 0
	}
	if sbOffset < 0 || sbOffset >= len(e.contentStateSbFd) {
		return 0
	}
	return e.contentStateSbFd[sbOffset]
}

// vp9CommitLastSource mirrors the previous-source lookahead slot libvpx exposes
// to avg_source_sad through cpi->Last_Source.
//
// libvpx: vp9_encoder.c:4086-4101 scales cpi->unscaled_last_source into
// cpi->Last_Source when compute_source_sad_onepass is live; cpi->unscaled_last_
// source itself is the previous lookahead source at vp9_encoder.c:6282.
func (e *VP9Encoder) vp9CommitLastSource(img *image.YCbCr, showFrame, dropped bool) {
	if e == nil {
		return
	}
	if img == nil || dropped {
		e.lastSourceValid = false
		return
	}
	if !showFrame {
		return
	}
	rect := image.Rect(0, 0, e.opts.Width, e.opts.Height)
	if e.lastSource.Rect != rect ||
		e.lastSource.SubsampleRatio != image.YCbCrSubsampleRatio420 ||
		len(e.lastSource.Y) == 0 {
		e.lastSource = *image.NewYCbCr(rect, image.YCbCrSubsampleRatio420)
	}
	copyVP9LookaheadImage(&e.lastSource, img, e.opts.Width, e.opts.Height)
	e.lastSourceValid = true
}

type vp9AvgSourceSADResult struct {
	contentState      vp9ContentStateSB
	zeroTempSADSource bool
}

// vp9AvgSourceSADStats ports libvpx avg_source_sad
// (vp9_encodeframe.c:1201-1248) for the 64x64 SB rooted at (miRow, miCol).
// It computes x->content_state_sb, x->zero_temp_sad_source, and updates
// cpi->content_state_sb_fd.
func (e *VP9Encoder) vp9AvgSourceSADStats(img *image.YCbCr, miCols, miRow, miCol int) (vp9AvgSourceSADResult, bool) {
	if e == nil || img == nil || e.sf.UseSourceSad == 0 || !e.lastSourceValid {
		return vp9AvgSourceSADResult{}, false
	}
	if img.Rect.Dx() != e.opts.Width || img.Rect.Dy() != e.opts.Height ||
		e.lastSource.Rect.Dx() != e.opts.Width ||
		e.lastSource.Rect.Dy() != e.opts.Height {
		return vp9AvgSourceSADResult{}, false
	}
	sbMiRow := miRow &^ 7
	sbMiCol := miCol &^ 7
	x0 := sbMiCol * 8
	y0 := sbMiRow * 8
	if x0 < 0 || y0 < 0 || x0+64 > e.opts.Width || y0+64 > e.opts.Height {
		return vp9AvgSourceSADResult{}, false
	}

	tmpSad := encoder.BlockSAD(img.Y, img.YStride, e.lastSource.Y,
		e.lastSource.YStride, x0, y0, x0, y0, 64, 64, ^uint64(0))
	tmpVariance, tmpSSE := encoder.BlockDiffVarianceSSE(img.Y, img.YStride,
		e.lastSource.Y, e.lastSource.YStride, x0, y0, x0, y0, 64, 64)
	sumdiffSquare := tmpSSE - tmpVariance

	const avgSourceSADThreshold uint64 = 10000
	const avgSourceSADThreshold2 uint64 = 12000

	contentState := vp9ContentStateHighSadHighSumdiff
	if tmpSad < avgSourceSADThreshold {
		if sumdiffSquare < 25 {
			contentState = vp9ContentStateLowSadLowSumdiff
		} else {
			contentState = vp9ContentStateLowSadHighSumdiff
		}
	} else if sumdiffSquare < 25 {
		contentState = vp9ContentStateHighSadLowSumdiff
	}

	if e.opts.ScreenContentMode != int8(VP9ScreenContentScreen) &&
		e.rc.mode == RateControlCBR && tmpVariance < (tmpSSE>>3) &&
		sumdiffSquare > 10000 {
		contentState = vp9ContentStateLowVarHighSumdiff
	} else if tmpSad > (avgSourceSADThreshold << 1) {
		contentState = vp9ContentStateVeryHighSad
	}

	sbOffset := vp9SbOffsetForMi(sbMiRow, sbMiCol, miCols)
	e.vp9UpdateContentStateSbFd(sbOffset, tmpSad < avgSourceSADThreshold2)
	return vp9AvgSourceSADResult{
		contentState:      contentState,
		zeroTempSADSource: tmpSad == 0,
	}, true
}

// vp9AvgSourceSAD preserves the older content-state-only call surface.
func (e *VP9Encoder) vp9AvgSourceSAD(img *image.YCbCr, miCols, miRow, miCol int) (vp9ContentStateSB, bool) {
	stats, ok := e.vp9AvgSourceSADStats(img, miCols, miRow, miCol)
	if !ok {
		return vp9ContentStateInvalid, false
	}
	return stats.contentState, true
}

// vp9SourceSADContentState is the per-frame, per-SB cache for the
// avg_source_sad result. libvpx resets x->content_state_sb at SB entry and
// computes it once before partitioning; every leaf mode pick in that SB reads
// the same value.
func (e *VP9Encoder) vp9SourceSADContentState(img *image.YCbCr, miRows, miCols, miRow, miCol int) (vp9ContentStateSB, bool) {
	stats, ok := e.vp9SourceSADState(img, miRows, miCols, miRow, miCol)
	if !ok {
		return vp9ContentStateInvalid, false
	}
	return stats.contentState, true
}

func (e *VP9Encoder) vp9SourceSADState(img *image.YCbCr, miRows, miCols, miRow, miCol int) (vp9AvgSourceSADResult, bool) {
	if e == nil || img == nil || e.sf.UseSourceSad == 0 {
		return vp9AvgSourceSADResult{}, false
	}
	sbCount := ((miRows + 7) >> 3) * ((miCols + 7) >> 3)
	if sbCount <= 0 {
		return vp9AvgSourceSADResult{}, false
	}
	if cap(e.varPartSBContentStateValid) < sbCount {
		e.varPartSBContentStateValid = make([]bool, sbCount)
	} else if len(e.varPartSBContentStateValid) < sbCount {
		tail := e.varPartSBContentStateValid[len(e.varPartSBContentStateValid):sbCount]
		for i := range tail {
			tail[i] = false
		}
		e.varPartSBContentStateValid = e.varPartSBContentStateValid[:sbCount]
	}
	if cap(e.varPartSBContentState) < sbCount {
		e.varPartSBContentState = make([]vp9ContentStateSB, sbCount)
	} else if len(e.varPartSBContentState) < sbCount {
		e.varPartSBContentState = e.varPartSBContentState[:sbCount]
	}
	if cap(e.varPartSBZeroTempSADSource) < sbCount {
		e.varPartSBZeroTempSADSource = make([]bool, sbCount)
	} else if len(e.varPartSBZeroTempSADSource) < sbCount {
		e.varPartSBZeroTempSADSource = e.varPartSBZeroTempSADSource[:sbCount]
	}
	sbMiRow := miRow &^ 7
	sbMiCol := miCol &^ 7
	idx := e.vp9ChoosePartitioningSBIndex(miCols, sbMiRow, sbMiCol)
	if idx < 0 || idx >= sbCount {
		return vp9AvgSourceSADResult{}, false
	}
	if e.varPartSBContentStateValid[idx] {
		return vp9AvgSourceSADResult{
			contentState:      e.varPartSBContentState[idx],
			zeroTempSADSource: e.varPartSBZeroTempSADSource[idx],
		}, true
	}
	stats, ok := e.vp9AvgSourceSADStats(img, miCols, sbMiRow, sbMiCol)
	if !ok {
		return vp9AvgSourceSADResult{}, false
	}
	e.varPartSBContentState[idx] = stats.contentState
	e.varPartSBZeroTempSADSource[idx] = stats.zeroTempSADSource
	e.varPartSBContentStateValid[idx] = true
	return stats, true
}

// vp9EnsureArfFrameUsage allocates cpi->count_arf_frame_usage and
// cpi->count_lastgolden_frame_usage lazily, mirroring libvpx
// vp9_speed_features.c:833-844:
//
//	if (cpi->count_arf_frame_usage == NULL) {
//	  CHECK_MEM_ERROR(
//	      &cm->error, cpi->count_arf_frame_usage,
//	      (uint8_t *)vpx_calloc((cm->mi_stride >> 3) * ((cm->mi_rows >> 3) + 1),
//	                            sizeof(*cpi->count_arf_frame_usage)));
//	}
//	if (cpi->count_lastgolden_frame_usage == NULL)
//	  CHECK_MEM_ERROR(
//	      &cm->error, cpi->count_lastgolden_frame_usage,
//	      (uint8_t *)vpx_calloc((cm->mi_stride >> 3) * ((cm->mi_rows >> 3) + 1),
//	                            sizeof(*cpi->count_lastgolden_frame_usage)));
//
// libvpx's CHECK_MEM_ERROR aborts on OOM; govpx propagates via Go's panic if
// make() fails (the same end-effect: process termination).
func (e *VP9Encoder) vp9EnsureArfFrameUsage(width, height int) {
	if e == nil {
		return
	}
	miCols, miRows, miStride := vp9MiDimensionsForFrame(width, height)
	size := vp9ContentStateSbFdSize(miStride, miRows)
	if size <= 0 {
		return
	}
	dimsChanged := e.countArfFrameUsageMiCols != miCols ||
		e.countArfFrameUsageMiRows != miRows ||
		e.countArfFrameUsageMiStride != miStride
	if e.countArfFrameUsage == nil || dimsChanged {
		e.countArfFrameUsage = make([]uint8, size)
	}
	if e.countLastgoldenFrameUsage == nil || dimsChanged {
		e.countLastgoldenFrameUsage = make([]uint8, size)
	}
	e.countArfFrameUsageMiCols = miCols
	e.countArfFrameUsageMiRows = miRows
	e.countArfFrameUsageMiStride = miStride
}

// vp9WriteArfFrameUsage ports libvpx vp9_encodeframe.c:5363-5371. The picker
// stamps the per-SB ARF / last-golden hit counts collected during
// vp9_pick_inter_mode at the end of each non-ARF, non-overlay frame in the
// ARF group:
//
//	if (!cpi->rc.is_src_frame_alt_ref && !cpi->refresh_golden_frame &&
//	    !cpi->refresh_alt_ref_frame && cpi->rc.alt_ref_gf_group &&
//	    cpi->sf.use_altref_onepass) {
//	  int sboffset = ((cm->mi_cols + 7) >> 3) * (mi_row >> 3) + (mi_col >> 3);
//	  if (cpi->count_arf_frame_usage != NULL)
//	    cpi->count_arf_frame_usage[sboffset] = x->arf_frame_usage;
//	  if (cpi->count_lastgolden_frame_usage != NULL)
//	    cpi->count_lastgolden_frame_usage[sboffset] = x->lastgolden_frame_usage;
//	}
//
// The libvpx-level guard (`alt_ref_gf_group && use_altref_onepass && ...`)
// is the caller's responsibility; this helper is the libvpx body inside the
// guard. arfFrameUsage / lastGoldenFrameUsage are the per-SB hit counts
// produced by the inter-mode picker.
func (e *VP9Encoder) vp9WriteArfFrameUsage(sbOffset int, arfFrameUsage, lastGoldenFrameUsage uint8) {
	if e == nil {
		return
	}
	if e.countArfFrameUsage != nil &&
		sbOffset >= 0 && sbOffset < len(e.countArfFrameUsage) {
		e.countArfFrameUsage[sbOffset] = arfFrameUsage
	}
	if e.countLastgoldenFrameUsage != nil &&
		sbOffset >= 0 && sbOffset < len(e.countLastgoldenFrameUsage) {
		e.countLastgoldenFrameUsage[sbOffset] = lastGoldenFrameUsage
	}
}

// vp9UpdateAltrefUsage ports libvpx's update_altref_usage
// (vp9_ratectrl.c:1802-1819) verbatim:
//
//	static void update_altref_usage(VP9_COMP *const cpi) {
//	  VP9_COMMON *const cm = &cpi->common;
//	  int sum_ref_frame_usage = 0;
//	  int arf_frame_usage = 0;
//	  int mi_row, mi_col;
//	  if (cpi->rc.alt_ref_gf_group && !cpi->rc.is_src_frame_alt_ref &&
//	      !cpi->refresh_golden_frame && !cpi->refresh_alt_ref_frame)
//	    for (mi_row = 0; mi_row < cm->mi_rows; mi_row += 8) {
//	      for (mi_col = 0; mi_col < cm->mi_cols; mi_col += 8) {
//	        int sboffset = ((cm->mi_cols + 7) >> 3) * (mi_row >> 3) +
//	                       (mi_col >> 3);
//	        sum_ref_frame_usage += cpi->count_arf_frame_usage[sboffset] +
//	                               cpi->count_lastgolden_frame_usage[sboffset];
//	        arf_frame_usage += cpi->count_arf_frame_usage[sboffset];
//	      }
//	    }
//	  if (sum_ref_frame_usage > 0) {
//	    double altref_count = 100.0 * arf_frame_usage / sum_ref_frame_usage;
//	    cpi->rc.perc_arf_usage =
//	        0.75 * cpi->rc.perc_arf_usage + 0.25 * altref_count;
//	  }
//	}
//
// The gating booleans (alt_ref_gf_group, is_src_frame_alt_ref,
// refresh_golden_frame, refresh_alt_ref_frame) are supplied by the caller so
// govpx's frame driver can reuse its existing accessors. miCols / miRows are
// the frame's mi grid extents.
func (e *VP9Encoder) vp9UpdateAltrefUsage(altRefGfGroup, isSrcFrameAltRef,
	refreshGoldenFrame, refreshAltRefFrame bool, miCols, miRows int) {
	if e == nil {
		return
	}
	if e.countArfFrameUsage == nil || e.countLastgoldenFrameUsage == nil {
		return
	}
	sumRefFrameUsage := 0
	arfFrameUsage := 0
	if altRefGfGroup && !isSrcFrameAltRef &&
		!refreshGoldenFrame && !refreshAltRefFrame {
		for miRow := 0; miRow < miRows; miRow += vp9SbStateMiBlock {
			for miCol := 0; miCol < miCols; miCol += vp9SbStateMiBlock {
				sbOffset := vp9SbOffsetForMi(miRow, miCol, miCols)
				if sbOffset < 0 || sbOffset >= len(e.countArfFrameUsage) ||
					sbOffset >= len(e.countLastgoldenFrameUsage) {
					continue
				}
				sumRefFrameUsage += int(e.countArfFrameUsage[sbOffset]) +
					int(e.countLastgoldenFrameUsage[sbOffset])
				arfFrameUsage += int(e.countArfFrameUsage[sbOffset])
			}
		}
	}
	if sumRefFrameUsage > 0 {
		altrefCount := 100.0 * float64(arfFrameUsage) / float64(sumRefFrameUsage)
		e.rc.percArfUsage = 0.75*e.rc.percArfUsage + 0.25*altrefCount
	}
}
