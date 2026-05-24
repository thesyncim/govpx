package govpx

import (
	"image"

	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
)

// libvpx parity references for the VP9 first-pass collector:
//   - vp9/encoder/vp9_firstpass.c:1353 vp9_first_pass (the macro-block-level
//     analysis loop internal/vp9/encoder paraphrases without a Lagrangian RD
//     path).
//   - vp9/encoder/vp9_firstpass_stats.h:20 FIRSTPASS_STATS (the on-disk
//     packet layout that VP9FirstPassFrameStats mirrors field-for-field).
//   - vp9/encoder/vp9_firstpass.c:759 first_pass_stat_calc (the
//     accumulator-to-FIRSTPASS_STATS finalization step).
//
// The Q3 motion-vector convention, intra penalty, new-MV penalty, and search
// radius live in internal/vp9/encoder with the motion-analysis helpers.

// VP9FirstPassFrameStats mirrors libvpx VP9 FIRSTPASS_STATS for one analyzed
// source frame or for the finalized sequence total.
//
// libvpx: vp9/encoder/vp9_firstpass_stats.h:20
type VP9FirstPassFrameStats = encoder.FirstPassFrameStats

// FinalizeVP9FirstPassStats appends the libvpx-style terminal total-stats
// record to per-frame VP9 first-pass stats. If stats is empty or already ends
// in a total row, the input slice is returned unchanged.
func FinalizeVP9FirstPassStats(stats []VP9FirstPassFrameStats) []VP9FirstPassFrameStats {
	return encoder.FinalizeFirstPassStats(stats)
}

// CollectFirstPassStats runs VP9 first-pass source analysis for future
// two-pass VOD planning. The returned row should be accumulated across input
// frames and passed through [FinalizeVP9FirstPassStats].
func (e *VP9Encoder) CollectFirstPassStats(img *image.YCbCr, pts uint64, duration uint64, flags EncodeFlags) (VP9FirstPassFrameStats, error) {
	if e == nil || e.closed {
		return VP9FirstPassFrameStats{}, ErrClosed
	}
	if err := e.validateVP9EncoderSource(img); err != nil {
		return VP9FirstPassFrameStats{}, err
	}
	if err := validateVP9EncodeFlags(flags); err != nil {
		return VP9FirstPassFrameStats{}, err
	}
	_ = pts

	stats := e.computeVP9FirstPassStats(img, duration)
	if e.vp9FirstPassCount > 0 && stats.PcntInter > 0.20 &&
		stats.CodedError > 0 && stats.IntraError/stats.CodedError > 2.0 &&
		vp9FirstPassImageMatches(&e.vp9FirstPassLast, e.opts.Width, e.opts.Height) {
		ensureVP9FirstPassImage(&e.vp9FirstPassGF, e.opts.Width, e.opts.Height)
		copyVP9LookaheadImage(&e.vp9FirstPassGF, &e.vp9FirstPassLast,
			e.opts.Width, e.opts.Height)
	}
	ensureVP9FirstPassImage(&e.vp9FirstPassLast, e.opts.Width, e.opts.Height)
	copyVP9LookaheadImage(&e.vp9FirstPassLast, img, e.opts.Width, e.opts.Height)
	if e.vp9FirstPassCount == 0 {
		ensureVP9FirstPassImage(&e.vp9FirstPassGF, e.opts.Width, e.opts.Height)
		copyVP9LookaheadImage(&e.vp9FirstPassGF, &e.vp9FirstPassLast,
			e.opts.Width, e.opts.Height)
	}
	e.vp9FirstPassCount++
	return stats, nil
}

func (e *VP9Encoder) computeVP9FirstPassStats(img *image.YCbCr, duration uint64) VP9FirstPassFrameStats {
	width := e.opts.Width
	height := e.opts.Height
	src, srcStride, _, _ := vp9EncoderSourcePlane(img, 0)
	hasLast := e.vp9FirstPassCount > 0 &&
		vp9FirstPassImageMatches(&e.vp9FirstPassLast, width, height)
	hasGF := e.vp9FirstPassCount > 1 &&
		vp9FirstPassImageMatches(&e.vp9FirstPassGF, width, height)
	last, lastStride, _, _ := vp9EncoderSourcePlane(&e.vp9FirstPassLast, 0)
	gf, gfStride, _, _ := vp9EncoderSourcePlane(&e.vp9FirstPassGF, 0)

	return encoder.AnalyzeFirstPassFrame(encoder.FirstPassFrameAnalysis{
		Width:        width,
		Height:       height,
		Frame:        e.vp9FirstPassCount,
		Duration:     duration,
		SourceY:      src,
		SourceStride: srcStride,
		HasLast:      hasLast,
		LastY:        last,
		LastStride:   lastStride,
		HasGolden:    hasGF,
		GoldenY:      gf,
		GoldenStride: gfStride,
	})
}

func ensureVP9FirstPassImage(img *image.YCbCr, width int, height int) {
	if vp9FirstPassImageMatches(img, width, height) {
		return
	}
	*img = *image.NewYCbCr(image.Rect(0, 0, width, height),
		image.YCbCrSubsampleRatio420)
}

func vp9FirstPassImageMatches(img *image.YCbCr, width int, height int) bool {
	if img == nil || img.Rect.Dx() != width || img.Rect.Dy() != height ||
		img.SubsampleRatio != image.YCbCrSubsampleRatio420 {
		return false
	}
	uvWidth, uvHeight := buffers.Chroma420Dimensions(width, height)
	return img.YStride >= width && img.CStride >= uvWidth &&
		len(img.Y) >= buffers.PlaneLen(img.YStride, height, width) &&
		len(img.Cb) >= buffers.PlaneLen(img.CStride, uvHeight, uvWidth) &&
		len(img.Cr) >= buffers.PlaneLen(img.CStride, uvHeight, uvWidth)
}
