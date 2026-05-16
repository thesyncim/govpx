// VP9 loop-filter strength picker. Verbatim port of libvpx v1.16.0
// vp9_picklpf.c: get_section_intra_rating, get_max_filter_level,
// try_filter_frame, search_filter_level, and vp9_pick_filter_level.
//
// The three modes are dispatched via cpi->sf.lpf_pick (which govpx
// exposes as e.sf.LpfPick):
//
//   - LpfPickFromFullImage / LpfPickFromSubImage: quadratic search
//     over filter levels, scoring each candidate with the Y-plane SSE
//     between source and post-filter reconstruction. The frame is
//     reconstructed unfiltered, we copy that to a "uf" backup, then
//     re-filter at each trial level and restore the backup before
//     trying the next. Used at speed 0..2 in libvpx good-quality.
//
//   - LpfPickFromQ: closed-form filt_guess = ROUND_POWER_OF_TWO(q *
//     20723 + 1015158, 18). Adjusted for KEY_FRAME (-4), and scaled by
//     5/8 in one-pass CBR cyclic-refresh-AQ on non-key/non-screen.
//     Used at speed 3+ in libvpx good-quality.
//
//   - LpfPickMinimalLpf: zero the filter level if it was non-zero last
//     frame, otherwise leave it; libvpx never selects this mode in the
//     stock dispatcher but the enum is present and the dispatcher
//     branch must be ported for parity.
//
// The govpx encoder mirrors libvpx's per-frame ordering: tiles are
// encoded into the reconstruction buffer before the picker runs, and
// the picker re-runs the loop filter at each trial level to score
// post-filter Y SSE against the source. The uncompressed header is
// re-written in place after the picker returns the chosen level so
// the wire stream carries the picked filter_level (libvpx:
// vp9/encoder/vp9_encoder.c:5391-5467 — encode_with_recode_loop runs
// before loopfilter_frame, which calls vp9_pick_filter_level, which
// runs before vp9_pack_bitstream).
//
// The callback-driven sseFn lets the unit tests exercise the
// quadratic-search algorithm with synthetic SSE landscapes; the
// production encode site supplies vp9PickLpfBuildSSECallback, which
// runs the real Y-only deblock against the reconstructed luma plane
// and computes the Y-plane SSE vs source. libvpx: vp9_picklpf.c:46-76
// (try_filter_frame).

package govpx

import (
	"image"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// vp9PickLpfMaxFilterLevel mirrors libvpx vp9_picklpf.c:37-44 get_max_filter_level.
//
// libvpx: vp9_picklpf.c:37
//
//	static int get_max_filter_level(const VP9_COMP *cpi) {
//	  if (cpi->oxcf.pass == 2) {
//	    unsigned int section_intra_rating = get_section_intra_rating(cpi);
//	    return section_intra_rating > 8 ? MAX_LOOP_FILTER * 3 / 4 : MAX_LOOP_FILTER;
//	  } else {
//	    return MAX_LOOP_FILTER;
//	  }
//	}
func (e *VP9Encoder) vp9PickLpfMaxFilterLevel(isKey bool) int {
	if e.twoPass.enabled() {
		sectionIntraRating := e.vp9PickLpfSectionIntraRating(isKey)
		if sectionIntraRating > 8 {
			return vp9dec.MaxLoopFilter * 3 / 4
		}
		return vp9dec.MaxLoopFilter
	}
	return vp9dec.MaxLoopFilter
}

// vp9PickLpfSectionIntraRating mirrors libvpx vp9_picklpf.c:27-35
// get_section_intra_rating. govpx does not currently track the libvpx
// twopass section_intra_rating / key_frame_section_intra_rating, so
// the rating defaults to 0 (the libvpx calloc'd-but-unwritten value)
// when two-pass stats are not loaded.
//
// libvpx: vp9_picklpf.c:27
//
//	static unsigned int get_section_intra_rating(const VP9_COMP *cpi) {
//	  unsigned int section_intra_rating;
//	  section_intra_rating = (cpi->common.frame_type == KEY_FRAME)
//	                             ? cpi->twopass.key_frame_section_intra_rating
//	                             : cpi->twopass.section_intra_rating;
//	  return section_intra_rating;
//	}
func (e *VP9Encoder) vp9PickLpfSectionIntraRating(isKey bool) int {
	// govpx's two-pass state does not surface per-section intra ratings;
	// the libvpx fallback when these are unset is 0 (calloc'd struct).
	return 0
}

// vp9PickLpfFromQ implements LPF_PICK_FROM_Q. Verbatim port of
// libvpx vp9_picklpf.c:159-202 the `method >= LPF_PICK_FROM_Q` branch
// of vp9_pick_filter_level. Returns the filter level chosen by the
// closed-form formula filt_guess = ROUND_POWER_OF_TWO(q * 20723 +
// 1015158, 18), clamped to [0, max_filter_level], with the KEY_FRAME
// -4 bias and the CYCLIC_REFRESH_AQ 5/8 scale applied. 8-bit only;
// govpx does not support HIGHBITDEPTH.
//
// libvpx: vp9_picklpf.c:168
//
//	} else if (method >= LPF_PICK_FROM_Q) {
//	  const int min_filter_level = 0;
//	  const int max_filter_level = get_max_filter_level(cpi);
//	  const int q = vp9_ac_quant(cm->base_qindex, 0, cm->bit_depth);
//	  // filt_guess = q * 0.316206 + 3.87252
//	  int filt_guess = ROUND_POWER_OF_TWO(q * 20723 + 1015158, 18);
//	  if (cpi->oxcf.pass == 0 && cpi->oxcf.rc_mode == VPX_CBR &&
//	      cpi->oxcf.aq_mode == CYCLIC_REFRESH_AQ && cm->seg.enabled &&
//	      (cm->base_qindex < 200 || cm->width * cm->height > 320 * 240) &&
//	      cpi->oxcf.content != VP9E_CONTENT_SCREEN && cm->frame_type != KEY_FRAME)
//	    filt_guess = 5 * filt_guess >> 3;
//	  if (cm->frame_type == KEY_FRAME) filt_guess -= 4;
//	  lf->filter_level = clamp(filt_guess, min_filter_level, max_filter_level);
//	}
func (e *VP9Encoder) vp9PickLpfFromQ(qindex int, isKey, segEnabled bool, width, height int) int {
	minFilterLevel := 0
	maxFilterLevel := e.vp9PickLpfMaxFilterLevel(isKey)
	q := int(vp9dec.VpxAcQuant(qindex, 0, vp9dec.BitDepth8))
	filtGuess := vp9PickLpfRoundPowerOfTwo(q*20723+1015158, 18)
	onePass := !e.twoPass.enabled()
	cbr := e.opts.RateControlMode == RateControlCBR
	cyclicRefresh := e.opts.AQMode == VP9AQCyclicRefresh
	screen := e.opts.ScreenContentMode == int8(VP9ScreenContentScreen)
	if onePass && cbr && cyclicRefresh && segEnabled &&
		(qindex < 200 || width*height > 320*240) &&
		!screen && !isKey {
		filtGuess = 5 * filtGuess >> 3
	}
	if isKey {
		filtGuess -= 4
	}
	return vp9PickLpfClamp(filtGuess, minFilterLevel, maxFilterLevel)
}

// vp9PickLpfMinimal implements LPF_PICK_MINIMAL_LPF. Verbatim port
// of libvpx vp9_picklpf.c:166-168
//
//	if (method == LPF_PICK_MINIMAL_LPF && lf->filter_level) {
//	  lf->filter_level = 0;
//	}
//
// The libvpx behaviour: when the prior frame had a non-zero filter
// level, force this frame to 0; otherwise leave the prior value
// unchanged. govpx tracks the prior filter level in
// e.vp9LastFiltLevel (refreshed in vp9StoreFilterLevel after every
// encoded frame).
func (e *VP9Encoder) vp9PickLpfMinimal() int {
	if e.vp9LastFiltLevel != 0 {
		return 0
	}
	return int(e.vp9LastFiltLevel)
}

// vp9PickLpfFilterLevelSSEFunc returns the Y-plane SSE between the
// source frame and the post-loop-filter reconstruction at filtLevel.
// The picker reconstructs the unfiltered frame, copies it to a backup,
// then re-filters at each candidate level (libvpx vp9_picklpf.c:52-73
// try_filter_frame). Callers must restore the unfiltered buffer
// between trials.
type vp9PickLpfFilterLevelSSEFunc func(filtLevel int, partial bool) int64

// vp9SearchFilterLevel ports libvpx vp9_picklpf.c:78-157
// search_filter_level. The function performs a quadratic descent over
// filter levels starting at lastFiltLevel, scoring each candidate
// with sseFn. The bias `(best_err >> (15 - (filt_mid / 8))) *
// filter_step` keeps the picker from flipping into a higher filter
// level on near-ties; in two-pass, sections with low intra rating
// further attenuate the bias by section_intra_rating/20. txMode !=
// ONLY_4X4 halves the bias because large-block frames are more
// tolerant of stronger filtering.
//
// libvpx: vp9_picklpf.c:78
//
//	static int search_filter_level(const YV12_BUFFER_CONFIG *sd, VP9_COMP *cpi,
//	                               int partial_frame) {
//	  const VP9_COMMON *const cm = &cpi->common;
//	  const struct loopfilter *const lf = &cm->lf;
//	  const int min_filter_level = 0;
//	  const int max_filter_level = get_max_filter_level(cpi);
//	  int filt_direction = 0;
//	  int64_t best_err;
//	  int filt_best;
//	  int filt_mid = clamp(lf->last_filt_level, min_filter_level, max_filter_level);
//	  int filter_step = filt_mid < 16 ? 4 : filt_mid / 4;
//	  int64_t ss_err[MAX_LOOP_FILTER + 1];
//	  unsigned int section_intra_rating = get_section_intra_rating(cpi);
//	  memset(ss_err, 0xFF, sizeof(ss_err));
//	  ...
//	}
func (e *VP9Encoder) vp9SearchFilterLevel(isKey bool, txMode common.TxMode, partialFrame bool,
	sseFn vp9PickLpfFilterLevelSSEFunc,
) int {
	minFilterLevel := 0
	maxFilterLevel := e.vp9PickLpfMaxFilterLevel(isKey)
	filtDirection := 0

	// libvpx: vp9_picklpf.c:90 — start at the previous frame's level,
	// clamped to the legal range.
	filtMid := vp9PickLpfClamp(int(e.vp9LastFiltLevel), minFilterLevel, maxFilterLevel)
	// libvpx: vp9_picklpf.c:91 — initial step: 4 below 16, else filt_mid/4.
	filterStep := 4
	if filtMid >= 16 {
		filterStep = filtMid / 4
	}

	// libvpx: vp9_picklpf.c:93-97 — ss_err is initialised to -1; we
	// sentinel-mark unscored entries via -1.
	var ssErr [vp9dec.MaxLoopFilter + 1]int64
	for i := range ssErr {
		ssErr[i] = -1
	}
	sectionIntraRating := e.vp9PickLpfSectionIntraRating(isKey)

	// libvpx: vp9_picklpf.c:99-104 — score filt_mid first.
	bestErr := sseFn(filtMid, partialFrame)
	filtBest := filtMid
	ssErr[filtMid] = bestErr

	for filterStep > 0 {
		// libvpx: vp9_picklpf.c:107-108
		filtHigh := min(filtMid+filterStep, maxFilterLevel)
		filtLow := max(filtMid-filterStep, minFilterLevel)

		// libvpx: vp9_picklpf.c:110-117 — bias formula and conditional scaling.
		shift := min(uint(15-(filtMid/8)), 63)
		bias := (bestErr >> shift) * int64(filterStep)
		if e.twoPass.enabled() && sectionIntraRating < 20 {
			bias = (bias * int64(sectionIntraRating)) / 20
		}
		// libvpx: vp9_picklpf.c:117 — txMode != ONLY_4X4 halves the bias.
		if txMode != common.Only4x4 {
			bias >>= 1
		}

		// libvpx: vp9_picklpf.c:119-132 — try lower side.
		if filtDirection <= 0 && filtLow != filtMid {
			if ssErr[filtLow] < 0 {
				ssErr[filtLow] = sseFn(filtLow, partialFrame)
			}
			if (ssErr[filtLow] - bias) < bestErr {
				if ssErr[filtLow] < bestErr {
					bestErr = ssErr[filtLow]
				}
				filtBest = filtLow
			}
		}

		// libvpx: vp9_picklpf.c:134-144 — try upper side.
		if filtDirection >= 0 && filtHigh != filtMid {
			if ssErr[filtHigh] < 0 {
				ssErr[filtHigh] = sseFn(filtHigh, partialFrame)
			}
			if ssErr[filtHigh] < (bestErr - bias) {
				bestErr = ssErr[filtHigh]
				filtBest = filtHigh
			}
		}

		// libvpx: vp9_picklpf.c:146-153 — halve the step if filt_mid
		// stays best, else descend toward filt_best.
		if filtBest == filtMid {
			filterStep /= 2
			filtDirection = 0
		} else {
			if filtBest < filtMid {
				filtDirection = -1
			} else {
				filtDirection = 1
			}
			filtMid = filtBest
		}
	}
	return filtBest
}

// vp9PickFilterLevel dispatches over e.sf.LpfPick. Verbatim port of
// libvpx vp9_picklpf.c:159-203 vp9_pick_filter_level.
//
// libvpx: vp9_picklpf.c:159
//
//	void vp9_pick_filter_level(const YV12_BUFFER_CONFIG *sd, VP9_COMP *cpi,
//	                           LPF_PICK_METHOD method) {
//	  VP9_COMMON *const cm = &cpi->common;
//	  struct loopfilter *const lf = &cm->lf;
//	  lf->sharpness_level = 0;
//	  if (method == LPF_PICK_MINIMAL_LPF && lf->filter_level) {
//	    lf->filter_level = 0;
//	  } else if (method >= LPF_PICK_FROM_Q) {
//	    ... from-Q formula ...
//	  } else {
//	    lf->filter_level = search_filter_level(sd, cpi,
//	                                           method == LPF_PICK_FROM_SUBIMAGE);
//	  }
//	}
//
// The dispatcher returns the chosen filter level; callers should write
// this into the loopfilter header. The production encode site supplies
// a non-nil sseFn that mirrors libvpx try_filter_frame: it applies the
// Y-plane loop filter at the trial level against the (saved &
// restorable) post-tile reconstructed luma, scores Y-plane SSE vs the
// source, and returns the SSE. The test suite drives the same
// dispatcher with synthetic sseFn landscapes to exercise the
// quadratic-search algorithm. When sseFn is nil and the method would
// invoke the search, the dispatcher falls back to the closed-form
// LpfPickFromQ formula so older call sites that have not been
// migrated still produce a valid level.
func (e *VP9Encoder) vp9PickFilterLevel(method LpfPickMethod,
	qindex int, isKey, segEnabled bool, width, height int,
	txMode common.TxMode, partialFrame bool,
	sseFn vp9PickLpfFilterLevelSSEFunc,
) int {
	// libvpx: vp9_picklpf.c:164 — sharpness_level is always reset to 0
	// at picker entry; govpx propagates this via the caller writing
	// header.Loopfilter.SharpnessLevel separately.
	switch method {
	case LpfPickMinimalLpf:
		// libvpx: vp9_picklpf.c:166-167 — only zero if non-zero previously.
		return e.vp9PickLpfMinimal()
	case LpfPickFromQ:
		// libvpx: vp9_picklpf.c:168-198 — the `method >= LPF_PICK_FROM_Q`
		// branch (LpfPickFromQ is the lowest method satisfying the
		// inequality after the LPF_PICK_MINIMAL_LPF early-out).
		return e.vp9PickLpfFromQ(qindex, isKey, segEnabled, width, height)
	case LpfPickFromFullImage, LpfPickFromSubImage:
		// libvpx: vp9_picklpf.c:200-201 — the trailing else branch
		// invokes search_filter_level. govpx falls back to the closed-
		// form formula when sseFn is nil because the encoder structure
		// emits the uncompressed header (which carries filter_level)
		// before tile reconstruction populates the recon buffers, so
		// the search has no luma to score against.
		if sseFn == nil {
			return e.vp9PickLpfFromQ(qindex, isKey, segEnabled, width, height)
		}
		return e.vp9SearchFilterLevel(isKey, txMode, partialFrame, sseFn)
	default:
		// Mirrors libvpx's typed enum: any other LPF_PICK_METHOD value
		// is treated as the default LPF_PICK_FROM_FULL_IMAGE search;
		// in the absence of an sseFn we fall back to from-Q.
		if sseFn == nil {
			return e.vp9PickLpfFromQ(qindex, isKey, segEnabled, width, height)
		}
		return e.vp9SearchFilterLevel(isKey, txMode, partialFrame, sseFn)
	}
}

// vp9PickLpfClamp mirrors libvpx vpx_ports/vpx_clamp.h clamp(value,
// low, high).
func vp9PickLpfClamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// vp9PickLpfRoundPowerOfTwo mirrors libvpx vpx_dsp/vpx_dsp_common.h
// ROUND_POWER_OF_TWO(value, n) == ((value) + (1 << ((n) - 1))) >> (n).
func vp9PickLpfRoundPowerOfTwo(value, n int) int {
	return (value + (1 << uint(n-1))) >> uint(n)
}

// vp9PickLpfYSSE mirrors libvpx vpx_dsp/psnr.c:155 vpx_get_y_sse.
// Computes the Y-plane SSE between source (src) and reconstruction
// (recon) over a width*height window. Both planes are 8-bit; strides
// may differ. libvpx clips to y_crop_width / y_crop_height; govpx
// passes the visible source dims directly.
//
// libvpx: vpx_dsp/psnr.c:155
//
//	int64_t vpx_get_y_sse(const YV12_BUFFER_CONFIG *a, ...) {
//	  return get_sse(a->y_buffer, a->y_stride, b->y_buffer, b->y_stride,
//	                 a->y_crop_width, a->y_crop_height);
//	}
func vp9PickLpfYSSE(src []byte, srcStride int,
	recon []byte, reconStride int,
	width, height int,
) int64 {
	var sse int64
	for row := range height {
		srcRow := src[row*srcStride : row*srcStride+width]
		recRow := recon[row*reconStride : row*reconStride+width]
		for col := range width {
			d := int64(srcRow[col]) - int64(recRow[col])
			sse += d * d
		}
	}
	return sse
}

// vp9PickLpfBuildSSECallback returns the production sseFn for the
// full-image / sub-image search picker. The callback follows libvpx
// vp9_picklpf.c:46-76 try_filter_frame:
//
//  1. Build the loop-filter limits tables for filtLevel via
//     vp9dec.LoopFilterFrameInit (libvpx: vp9_loopfilter.c
//     vp9_loop_filter_frame_init).
//  2. Apply Y-only deblock at filtLevel against the reconstructed
//     luma plane in-place (libvpx vp9_loop_filter_frame(..., y_only=1,
//     partial_frame)). We use applyVP9LoopFilterPlane(plane=Y).
//  3. Compute Y-plane SSE between source and the filtered luma over
//     the visible (Width × Height) window (libvpx vpx_get_y_sse).
//  4. Restore the unfiltered Y plane from the caller-owned backup so
//     the next trial filters fresh recon.
//
// The caller (vp9EncoderPickFilterLevelAfterTiles) snapshots the
// unfiltered Y plane once before the search, and arranges the post-
// search final filter pass via applyVP9EncoderLoopFilter on Y+U+V.
//
// libvpx: vp9_picklpf.c:46-76
//
//	static int64_t try_filter_frame(const YV12_BUFFER_CONFIG *sd,
//	                                VP9_COMP *const cpi, int filt_level,
//	                                int partial_frame) {
//	  ...
//	  vp9_loop_filter_frame(cm->frame_to_show, cm, &cpi->td.mb.e_mbd,
//	                        filt_level, 1, partial_frame);
//	  filt_err = vpx_get_y_sse(sd, cm->frame_to_show);
//	  // Re-instate the unfiltered frame
//	  vpx_yv12_copy_y(&cpi->last_frame_uf, cm->frame_to_show);
//	  return filt_err;
//	}
func (e *VP9Encoder) vp9PickLpfBuildSSECallback(hdr *vp9dec.UncompressedHeader,
	seg *vp9dec.SegmentationParams, img *image.YCbCr, ufBackupY []byte,
) vp9PickLpfFilterLevelSSEFunc {
	layout := vp9FrameBufferLayout(int(hdr.Width), int(hdr.Height))
	miRows := int((hdr.Height + 7) >> 3)
	miCols := int((hdr.Width + 7) >> 3)
	srcY, srcStride, srcW, srcH := vp9EncoderSourcePlane(img, 0)
	return func(filtLevel int, partialFrame bool) int64 {
		if filtLevel == 0 {
			// libvpx: vp9_loop_filter_frame returns immediately when
			// frame_filter_level == 0 (vp9_loopfilter.c:1473). The
			// SSE is then unfiltered-recon vs source.
			return vp9PickLpfYSSE(srcY, srcStride,
				e.reconYFull[layout.yOrigin:], layout.yStride,
				srcW, srcH)
		}
		// Build LoopFilterInfoN for the trial level.
		lfTrial := hdr.Loopfilter
		lfTrial.FilterLevel = uint8(filtLevel)
		vp9dec.LoopFilterFrameInit(&e.lfi, &lfTrial, seg, filtLevel)
		d := VP9Decoder{
			lfi:          e.lfi,
			miGrid:       e.miGrid,
			frameYFull:   e.reconYFull,
			frameUFull:   e.reconUFull,
			frameVFull:   e.reconVFull,
			frameYOrigin: layout.yOrigin,
			frameUOrigin: layout.uvOrigin,
			frameVOrigin: layout.uvOrigin,
			lastFrame:    e.reconFrame,
		}
		// libvpx: vp9_picklpf.c:54-60 — y_only=1 trial filter.
		// partial_frame currently always false on the search-driven
		// path (search_filter_level is invoked with method ==
		// LPF_PICK_FROM_SUBIMAGE only when partial_frame=true; the
		// dispatcher exposes that via the partialFrame argument).
		_ = partialFrame // partial_frame plumbing reserved for future LPF_PICK_FROM_SUBIMAGE.
		// applyVP9LoopFilterPlane runs the Y-only deblock in-place
		// against e.reconYFull.
		if !d.applyVP9LoopFilterPlane(miRows, miCols, vp9LoopFilterPlaneY) {
			// Fall back to no-op SSE on a structural failure; the
			// picker treats a missing entry via ss_err[]<0 as
			// uninitialised, so returning a sentinel here would
			// derail the search. Instead, restore the backup and
			// return a deliberately-large SSE so the level is
			// rejected.
			copy(e.reconYFull[layout.yOrigin:], ufBackupY)
			return int64(1) << 62
		}
		sse := vp9PickLpfYSSE(srcY, srcStride,
			e.reconYFull[layout.yOrigin:], layout.yStride,
			srcW, srcH)
		// libvpx: vp9_picklpf.c:73 — re-instate the unfiltered Y plane
		// from cpi->last_frame_uf.
		copy(e.reconYFull[layout.yOrigin:], ufBackupY)
		return sse
	}
}

// vp9EncoderRunFullImagePicker is the post-tile entry point for the
// LPF full-image / sub-image search picker. The caller pre-gates the
// LpfPickFromQ / LpfPickMinimalLpf methods (which don't consult the
// recon buffer) so this method only fires when a real search is
// required — keeping the steady-state FROM_Q encode path
// allocation-free. Invoked after writeVP9FrameTiles has populated
// e.reconYFull with the unfiltered reconstruction. The method
// allocates / reuses ufBackupY, snapshots the visible Y plane, builds
// the production sseFn via vp9PickLpfBuildSSECallback, and invokes
// the dispatcher (vp9PickFilterLevel). The returned level supersedes
// the pre-tile placeholder; the caller updates
// header.Loopfilter.FilterLevel and re-writes the uncompressed
// header in place so the bitstream carries the picked level.
//
// libvpx flow: encode_with_recode_loop (encodes tiles into recon) →
// loopfilter_frame (calls vp9_pick_filter_level, then applies the
// loop filter at the picked level) → vp9_pack_bitstream (writes the
// header carrying lf->filter_level). govpx mirrors the order with
// in-place header re-write because the uncompressed-header byte
// length is invariant under filter_level: filter_level is always a
// 6-bit literal (internal/vp9/encoder/header_writer.go:384
// EncodeLoopfilterWithPrev).
//
// libvpx: vp9_encoder.c:3405-3471 (loopfilter_frame),
// vp9_encoder.c:5391-5467 (encode_frame_to_data_rate sequencing).
func (e *VP9Encoder) vp9EncoderRunFullImagePicker(
	hdr *vp9dec.UncompressedHeader, seg *vp9dec.SegmentationParams,
	img *image.YCbCr, txMode common.TxMode, isKey bool,
) uint8 {
	method := e.sf.LpfPick
	// Build the production sseFn against the recon buffer. libvpx:
	// vp9_picklpf.c:99-100 — copy the unfiltered recon into
	// last_frame_uf before any try_filter_frame call.
	layout := vp9FrameBufferLayout(int(hdr.Width), int(hdr.Height))
	yVisibleLen := layout.yStride * layout.yHeight
	if cap(e.vp9LpfReconYBackup) < yVisibleLen {
		e.vp9LpfReconYBackup = make([]byte, yVisibleLen)
	} else {
		e.vp9LpfReconYBackup = e.vp9LpfReconYBackup[:yVisibleLen]
	}
	copy(e.vp9LpfReconYBackup, e.reconYFull[layout.yOrigin:layout.yOrigin+yVisibleLen])
	sseFn := e.vp9PickLpfBuildSSECallback(hdr, seg, img, e.vp9LpfReconYBackup)
	level := uint8(e.vp9PickFilterLevel(method, int(hdr.Quant.BaseQindex),
		isKey, hdr.Seg.Enabled, int(hdr.Width), int(hdr.Height),
		txMode, false /* partialFrame */, sseFn))
	// After the search, the recon Y plane holds the last-trial
	// unfiltered state (try_filter_frame's final copy-back at
	// vp9_picklpf.c:73). The caller will run the final
	// applyVP9EncoderLoopFilter at the picked level, matching libvpx
	// vp9_encoder.c:3459-3468 (the unconditional post-pick filter
	// pass inside loopfilter_frame).
	return level
}
