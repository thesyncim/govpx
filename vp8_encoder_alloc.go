package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"github.com/thesyncim/govpx/internal/vpx/geometry"
)

// applyResolutionChange rebuilds the encoder's size-dependent state for
// new visible dimensions and arranges for the next encoded frame to be
// a key frame at that size. Validation runs before any mutation so a
// rejected request leaves the encoder usable at its current dimensions.
//
// Mirrors libvpx's `vpx_codec_enc_config_set` with a new width/height:
// references must be invalidated (any prior LAST/GOLDEN/ALTREF pixels
// cannot be used as prediction at the new size) and the next frame is
// promoted to a key frame.
//
// Resize is refused while lookahead is non-empty or while a hidden
// alt-ref input is staged, because draining those queues at the old
// dimensions and pushing new frames at the new dimensions would mix
// resolutions inside a single buffered window.
func (e *VP8Encoder) applyResolutionChange(width int, height int) error {
	if !validDimension(width) || !validDimension(height) {
		return ErrInvalidConfig
	}
	if e.lookaheadEnabled() && e.lookaheadCount > 0 {
		return ErrInvalidConfig
	}
	if e.autoAltRefStashValid {
		return ErrInvalidConfig
	}
	e.clearPendingLookaheadReferenceSets()
	e.clearLatestLookaheadReferenceSets()
	// Roll back to a pre-mutation snapshot if anything below fails, so
	// the encoder remains usable at the previous dimensions.
	prevWidth := e.opts.Width
	prevHeight := e.opts.Height
	e.opts.Width = width
	e.opts.Height = height
	if err := e.reallocateForDimensions(width, height); err != nil {
		e.opts.Width = prevWidth
		e.opts.Height = prevHeight
		// Restore prior allocations so encode state is coherent at the
		// previous size. Reference pixels and per-MB scratch may have
		// been overwritten by the partial attempt, so also invalidate
		// references and force the next frame to be a key frame at the
		// previous size to keep parity with the libvpx "resize failed"
		// recovery contract.
		_ = e.reallocateForDimensions(prevWidth, prevHeight)
		e.referenceFrameNumbers = [vp8common.MaxRefFrames]uint64{}
		e.forceKeyFrame = true
		return err
	}
	if err := e.ensureRowWorkerPool(width, height); err != nil {
		e.opts.Width = prevWidth
		e.opts.Height = prevHeight
		_ = e.reallocateForDimensions(prevWidth, prevHeight)
		_ = e.ensureRowWorkerPool(prevWidth, prevHeight)
		e.referenceFrameNumbers = [vp8common.MaxRefFrames]uint64{}
		e.forceKeyFrame = true
		return err
	}

	// Invalidate all three references: their pixel content is at the
	// previous coded dimensions and cannot legally predict an inter
	// frame at the new size. The explicit forceKeyFrame below drives the
	// next encode to a keyframe at the new dimensions.
	e.referenceFrameNumbers = [vp8common.MaxRefFrames]uint64{}
	e.goldenRefAliasesLast = false
	e.altRefAliasesLast = false
	e.goldenRefAliasesAlt = false
	e.lastFrameInterModesValid = false
	e.lastCodedFrameType = vp8common.KeyFrame
	e.lastInterZeroMVCount = 0
	e.lastInterSkipCount = 0

	// Denoiser running-average buffers were sized for the old picture;
	// the next inter frame's ensureAllocated call will reallocate to
	// match. Reset clears the per-MB FILTER/COPY/NoFilter state map.
	e.denoiser.reset()

	// Activity map / ROI segment slice / active map are sized by MB
	// count; the active map already got resized in
	// reallocateForDimensions, the activity map is rebuilt lazily on
	// the next inter frame, and ROI is disabled because the supplied
	// segmentID grid no longer matches the new MB grid.
	e.activityMapValid = false
	if e.roi.enabled {
		e.roi.reset()
	}

	// Two-pass per-frame budgets depend on MB count.
	e.twoPass.configureFrameDims(width, height)

	// Refresh the rate-control raw-target-rate cap for the new
	// dimensions. setBitrateKbps reads frameWidth / frameHeight when
	// the cap is recomputed (any subsequent SetBitrateKbps /
	// SetRateControl call), so leaving the cached dims stale would
	// leak the old cap into the post-resize bitrate envelope.
	e.rc.setFrameDimensions(width, height)

	// Force the next frame to be a key frame at the new size. The
	// encode path also treats "no references available" as a
	// keyframe trigger via shouldEncodeKeyFrame, but setting the
	// flag explicitly mirrors how libvpx's resize path drives the
	// next frame.
	e.forceKeyFrame = true

	return nil
}

// reallocateForDimensions sizes every dimension-dependent encoder buffer
// for the given width/height. It is the single source of truth for the
// allocation block that lives in both [NewVP8Encoder] (cold start) and
// the runtime resize path (see [VP8Encoder.SetRealtimeTarget]).
//
// Buffers are grown in place when capacity is already sufficient and
// re-allocated otherwise, so a resize never copies pixels twice and a
// steady-state encode at fixed dimensions performs zero work here.
//
// The function never mutates the encoder's reference picture data: the
// caller is responsible for invalidating reference identity (so the next
// frame at the new size is a key frame) when this is invoked for resize.
func (e *VP8Encoder) reallocateForDimensions(width int, height int) error {
	if !validDimension(width) || !validDimension(height) {
		return ErrInvalidConfig
	}
	mbCount := geometry.MacroblockCount(width, height)
	mbRows := geometry.MacroblockRows(height)
	mbCols := geometry.MacroblockCols(width)

	e.cyclicRefreshMap = vp8enc.ResizeInt8Slice(e.cyclicRefreshMap, mbCount)
	e.cyclicRefreshAttemptMap = vp8enc.ResizeInt8Slice(e.cyclicRefreshAttemptMap, mbCount)
	e.skinMap = vp8enc.ResizeUint8Slice(e.skinMap, mbCount)
	e.consecZeroLast = vp8enc.ResizeUint8Slice(e.consecZeroLast, mbCount)
	e.consecZeroLastMVBias = vp8enc.ResizeUint8Slice(e.consecZeroLastMVBias, mbCount)
	e.dotArtifactChecked = vp8enc.ResizeBoolSlice(e.dotArtifactChecked, mbCount)
	e.activeMap = vp8enc.ResizeUint8Slice(e.activeMap, mbCount)
	e.keyFrameModes = vp8enc.ResizeKeyFrameModeSlice(e.keyFrameModes, mbCount)
	e.interFrameModes = vp8enc.ResizeInterFrameModeSlice(e.interFrameModes, mbCount)
	e.gfActiveMap = vp8enc.ResizeBoolSlice(e.gfActiveMap, mbCount)
	e.lastFrameInterModes = vp8enc.ResizeInterFrameModeSlice(e.lastFrameInterModes, mbCount)
	e.lastFrameInterModeBias = vp8enc.ResizeBoolSlice(e.lastFrameInterModeBias, mbCount)
	e.keyFrameCoeffs = vp8enc.ResizeMacroblockCoefficientSlice(e.keyFrameCoeffs, mbCount)
	e.tokenAbove = vp8enc.ResizeTokenContextSlice(e.tokenAbove, mbCols)
	e.reconstructAboveTok = vp8enc.ResizeTokenContextSlice(e.reconstructAboveTok, mbCols)
	e.reconstructModes = vp8dec.ResizeMacroblockModeSlice(e.reconstructModes, mbCount)
	e.reconstructTokens = vp8dec.ResizeMacroblockTokenSlice(e.reconstructTokens, mbCount)

	e.interCoefTokenRecords.Reset(mbRows, mbCount)

	if err := e.initReferenceFrames(width, height); err != nil {
		return err
	}
	if err := e.initPreprocessFrames(width, height); err != nil {
		return err
	}
	if err := e.resizeLookaheadFrames(width, height); err != nil {
		return err
	}
	return nil
}

// ensureRowWorkerPool allocates or resizes the row-parallel worker pool
// to match the configured thread count and frame dimensions. At
// Threads<=1 it leaves e.rowWorkers nil so the canonical single-thread
// hot path stays zero-cost. Reusable across NewVP8Encoder and the
// runtime resize path.
func (e *VP8Encoder) ensureRowWorkerPool(width int, height int) error {
	eff := e.effectiveThreadCount()
	if eff < 2 {
		// Threads=1 path: keep e.rowWorkers nil so the picker / reconstruct
		// hot paths branch on a single nil-check before any threading code
		// path executes. Mirrors libvpx vp8cx_create_encoder_threads early
		// return when cpi->oxcf.multi_threaded < 2.
		return nil
	}
	mbRows := geometry.MacroblockRows(height)
	mbCols := geometry.MacroblockCols(width)
	if e.rowWorkers == nil {
		e.rowWorkers = newRowWorkerPool(eff, mbRows, mbCols)
	} else {
		// Resize the wave-front progress slice and recompute the
		// width-dependent sync stride. The persistent worker goroutines
		// keep running; the per-frame reset() consumes the new mbRows.
		if cap(e.rowWorkers.rowProgress) < mbRows {
			e.rowWorkers.rowProgress = make([]paddedAtomicInt64, mbRows)
		} else {
			e.rowWorkers.rowProgress = e.rowWorkers.rowProgress[:mbRows]
		}
		e.rowWorkers.syncRange = encoderThreadSyncRange(mbCols)
	}
	if e.rowWorkers != nil {
		// loopFilterPickAlt is the second LF-trial scratch used only on
		// the parallel filt_low/filt_high dispatch in pickFull. It is
		// allocated only when a row-worker pool exists so Threads=1 stays
		// zero-cost.
		if err := e.loopFilterPickAlt.Resize(width, height, 32, 32); err != nil {
			return ErrInvalidConfig
		}
	}
	return nil
}
