package govpx

import (
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

type encodeSourceMetadata struct {
	lookaheadDepth int
	arnrFiltered   bool
	denoised       bool
}

func (e *VP8Encoder) initPreprocessFrames(width int, height int) error {
	if err := e.preprocess.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	if err := e.arnrScratch.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	if err := e.arnrLastSource.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	if err := e.firstPassLastRef.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	if err := e.firstPassGoldenRef.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	if err := e.firstPassLastSource.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	if err := e.firstPassNewRef.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	return nil
}
func (e *VP8Encoder) preprocessSource(source vp8enc.SourceImage, flags EncodeFlags, meta encodeSourceMetadata) (vp8enc.SourceImage, encodeSourceMetadata) {
	src := source
	// ARNR (libvpx vp8/encoder/temporal_filter.c vp8_temporal_filter_prepare_c)
	// only fires for the hidden alt-ref source. libvpx's onyx_if.c
	// vp8_get_compressed_data gates it on
	// `cpi->source_alt_ref_pending && oxcf.arnr_max_frames > 0` and, on the
	// firing branch, redirects `force_src_buffer = &cpi->alt_ref_buffer` so
	// every subsequent encode-pass read of `cpi->Source` consumes the filtered
	// output rather than the raw lookahead frame. govpx schedules the hidden
	// ARF emission with the EncodeInvisibleFrame|EncodeForceAltRefFrame flag
	// pair (see `autoAltRefHiddenFlags` in encoder_altref_driver.go), so the
	// same flag pair gates the filter here. Without the gate, ARNR runs on
	// every popped frame including the keyframe, which mutates the source
	// before the keyframe encode reads it and pushes Y reconstruction off
	// byte-identity (the gate was originally landed in commit 0af0a25 as
	// "Gate VP8 ARNR temporal filter on hidden alt-ref source" but was lost
	// in a subsequent merge of d2c00ed; this restores it).
	hiddenAltRefFrame := flags&(EncodeInvisibleFrame|EncodeForceAltRefFrame) == EncodeInvisibleFrame|EncodeForceAltRefFrame
	if hiddenAltRefFrame && e.opts.ARNRMaxFrames > 1 && e.lookaheadEnabled() {
		if e.applyARNRFilter(src, flags) {
			// The filtered output lives in `arnrScratch`, govpx's analogue
			// of libvpx's `cpi->alt_ref_buffer`. Returning it here is the
			// equivalent of libvpx's
			// `cpi->Source = force_src_buffer ? force_src_buffer : ...`
			// branch — every downstream read of the source for this hidden
			// ARF encode (motion search, RD picker, transform residual,
			// loop-filter trial SSE) consumes the filtered pixels.
			src = sourceImageFromVP8(&e.arnrScratch.Img)
			meta.arnrFiltered = true
		}
	}
	if e.opts.ARNRMaxFrames > 1 && e.lookaheadEnabled() {
		copySourceToFrameBuffer(&e.arnrLastSource, source)
		e.arnrLastReady = true
	} else {
		e.arnrLastReady = false
	}
	if e.opts.NoiseSensitivity > 0 {
		// Allocate the libvpx-style running average buffers and per-MB
		// state map on first inter frame; the actual filter runs per-MB
		// after mode decision in buildReconstructingInterFrameCoefficients.
		_ = e.denoiser.ensureAllocated(e.opts.Width, e.opts.Height)
		mode := denoiserModeForSensitivity(e.opts.NoiseSensitivity)
		e.denoiser.mode = mode
		_, e.denoiser.params = denoiserSetParameters(mode)
		meta.denoised = true
	}
	return src, meta
}
