package govpx

import (
	"image"

	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
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
	miCols, miRows, miStride := encoder.MiDimensionsForFrame(width, height)
	size := encoder.ContentStateBufferSize(miStride, miRows)
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
	encoder.ResetContentStateBuffer(e.contentStateSbFd)
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
	encoder.UpdateContentStateBuffer(e.contentStateSbFd, sbOffset, lowSourceSad)
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
	return encoder.ContentStateAt(e.contentStateSbFd, sbOffset)
}

// vp9EnsureSBLastHighContentCached snapshots content_state_sb_fd into the
// per-SB cache before avg_source_sad updates it for the current frame.
func (e *VP9Encoder) vp9EnsureSBLastHighContentCached(miRows, miCols, miRow, miCol int) {
	if e == nil {
		return
	}
	sbCount := ((miRows + 7) >> 3) * ((miCols + 7) >> 3)
	if sbCount <= 0 {
		return
	}
	e.varPartSBLastHighContent = buffers.EnsureLen(e.varPartSBLastHighContent, sbCount)
	e.varPartSBLastHighContentValid = buffers.EnsureLenZeroTail(
		e.varPartSBLastHighContentValid, sbCount)
	sbMiRow := miRow &^ 7
	sbMiCol := miCol &^ 7
	sbIdx := e.vp9ChoosePartitioningSBIndex(miCols, sbMiRow, sbMiCol)
	if sbIdx < 0 || sbIdx >= sbCount || e.varPartSBLastHighContentValid[sbIdx] {
		return
	}
	sbOffset := encoder.SBOffsetForMi(sbMiRow, sbMiCol, miCols)
	e.varPartSBLastHighContent[sbIdx] = e.vp9ReadContentStateSbFd(sbOffset)
	e.varPartSBLastHighContentValid[sbIdx] = true
}

// vp9LastSBHighContentForPick returns the libvpx x->last_sb_high_content
// value for the SB containing (miRow, miCol).
func (e *VP9Encoder) vp9LastSBHighContentForPick(miRows, miCols, miRow, miCol int) uint8 {
	if e == nil {
		return 0
	}
	e.vp9EnsureSBLastHighContentCached(miRows, miCols, miRow, miCol)
	sbIdx := e.vp9ChoosePartitioningSBIndex(miCols, miRow&^7, miCol&^7)
	if sbIdx < 0 || sbIdx >= len(e.varPartSBLastHighContent) {
		return 0
	}
	return e.varPartSBLastHighContent[sbIdx]
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

// vp9AvgSourceSADStats ports libvpx avg_source_sad
// (vp9_encodeframe.c:1201-1248) for the 64x64 SB rooted at (miRow, miCol).
// It computes x->content_state_sb, x->zero_temp_sad_source, and updates
// cpi->content_state_sb_fd.
func (e *VP9Encoder) vp9AvgSourceSADStats(img *image.YCbCr, miCols, miRow, miCol int) (encoder.AvgSourceSADResult, bool) {
	if e == nil || img == nil || e.sf.UseSourceSad == 0 || !e.lastSourceValid {
		return encoder.AvgSourceSADResult{}, false
	}
	if img.Rect.Dx() != e.opts.Width || img.Rect.Dy() != e.opts.Height ||
		e.lastSource.Rect.Dx() != e.opts.Width ||
		e.lastSource.Rect.Dy() != e.opts.Height {
		return encoder.AvgSourceSADResult{}, false
	}
	sbMiRow := miRow &^ 7
	sbMiCol := miCol &^ 7
	stats, ok := encoder.AvgSourceSAD(encoder.AvgSourceSADArgs{
		SourceY:           img.Y,
		SourceYStride:     img.YStride,
		LastSourceY:       e.lastSource.Y,
		LastSourceYStride: e.lastSource.YStride,
		Width:             e.opts.Width,
		Height:            e.opts.Height,
		MIRow:             sbMiRow,
		MICol:             sbMiCol,
		ScreenContent:     e.opts.ScreenContentMode == int8(VP9ScreenContentScreen),
		CBR:               e.rc.mode == RateControlCBR,
	})
	if !ok {
		return encoder.AvgSourceSADResult{}, false
	}

	sbOffset := encoder.SBOffsetForMi(sbMiRow, sbMiCol, miCols)
	e.vp9UpdateContentStateSbFd(sbOffset, stats.LowSADForContentState)
	return stats, true
}

// vp9SourceSADContentState is the per-frame, per-SB cache for the
// avg_source_sad result. libvpx resets x->content_state_sb at SB entry and
// computes it once before partitioning; every leaf mode pick in that SB reads
// the same value.
func (e *VP9Encoder) vp9SourceSADContentState(img *image.YCbCr, miRows, miCols, miRow, miCol int) (encoder.ContentStateSB, bool) {
	stats, ok := e.vp9SourceSADState(img, miRows, miCols, miRow, miCol)
	if !ok {
		return encoder.ContentStateInvalid, false
	}
	return stats.ContentState, true
}

func (e *VP9Encoder) vp9SourceSADState(img *image.YCbCr, miRows, miCols, miRow, miCol int) (encoder.AvgSourceSADResult, bool) {
	if e == nil || img == nil || e.sf.UseSourceSad == 0 {
		return encoder.AvgSourceSADResult{}, false
	}
	sbCount := ((miRows + 7) >> 3) * ((miCols + 7) >> 3)
	if sbCount <= 0 {
		return encoder.AvgSourceSADResult{}, false
	}
	if len(e.varPartSBContentStateValid) != sbCount {
		e.varPartSBContentStateValid = buffers.EnsureLenZeroTail(
			e.varPartSBContentStateValid, sbCount)
	}
	if len(e.varPartSBContentState) != sbCount {
		e.varPartSBContentState = buffers.EnsureLen(
			e.varPartSBContentState, sbCount)
	}
	if len(e.varPartSBZeroTempSADSource) != sbCount {
		e.varPartSBZeroTempSADSource = buffers.EnsureLen(
			e.varPartSBZeroTempSADSource, sbCount)
	}
	sbMiRow := miRow &^ 7
	sbMiCol := miCol &^ 7
	idx := e.vp9ChoosePartitioningSBIndex(miCols, sbMiRow, sbMiCol)
	if idx < 0 || idx >= sbCount {
		return encoder.AvgSourceSADResult{}, false
	}
	if e.varPartSBContentStateValid[idx] {
		return encoder.AvgSourceSADResult{
			ContentState:      e.varPartSBContentState[idx],
			ZeroTempSADSource: e.varPartSBZeroTempSADSource[idx],
		}, true
	}
	stats, ok := e.vp9AvgSourceSADStats(img, miCols, sbMiRow, sbMiCol)
	if !ok {
		return encoder.AvgSourceSADResult{}, false
	}
	e.varPartSBContentState[idx] = stats.ContentState
	e.varPartSBZeroTempSADSource[idx] = stats.ZeroTempSADSource
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
	miCols, miRows, miStride := encoder.MiDimensionsForFrame(width, height)
	size := encoder.ContentStateBufferSize(miStride, miRows)
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
