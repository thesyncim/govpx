package govpx

import (
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

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
	mbCount := encoderMacroblockCount(width, height)
	mbRows := encoderMacroblockRows(height)
	mbCols := encoderMacroblockCols(width)

	e.cyclicRefreshMap = resizeInt8Slice(e.cyclicRefreshMap, mbCount)
	e.cyclicRefreshAttemptMap = resizeInt8Slice(e.cyclicRefreshAttemptMap, mbCount)
	e.skinMap = resizeUint8Slice(e.skinMap, mbCount)
	e.consecZeroLast = resizeUint8Slice(e.consecZeroLast, mbCount)
	e.consecZeroLastMVBias = resizeUint8Slice(e.consecZeroLastMVBias, mbCount)
	e.dotArtifactChecked = resizeBoolSlice(e.dotArtifactChecked, mbCount)
	e.activeMap = resizeUint8Slice(e.activeMap, mbCount)
	e.keyFrameModes = resizeKeyFrameModeSlice(e.keyFrameModes, mbCount)
	e.interFrameModes = resizeInterFrameModeSlice(e.interFrameModes, mbCount)
	e.lastFrameInterModes = resizeInterFrameModeSlice(e.lastFrameInterModes, mbCount)
	e.lastFrameInterModeBias = resizeBoolSlice(e.lastFrameInterModeBias, mbCount)
	e.keyFrameCoeffs = resizeKeyFrameCoeffSlice(e.keyFrameCoeffs, mbCount)
	e.tokenAbove = resizeTokenContextSlice(e.tokenAbove, mbCols)
	e.reconstructAboveTok = resizeTokenContextSlice(e.reconstructAboveTok, mbCols)
	e.reconstructModes = resizeReconstructModeSlice(e.reconstructModes, mbCount)
	e.reconstructTokens = resizeReconstructTokensSlice(e.reconstructTokens, mbCount)

	vp8enc.ResetInterCoefficientTokenRecords(&e.interCoefTokenRecords, mbRows, mbCount)

	if err := e.initReferenceFrames(width, height); err != nil {
		return err
	}
	if err := e.initPreprocessFrames(width, height); err != nil {
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
	mbRows := encoderMacroblockRows(height)
	mbCols := encoderMacroblockCols(width)
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

func resizeInt8Slice(s []int8, n int) []int8 {
	if cap(s) >= n {
		s = s[:n]
		for i := range s {
			s[i] = 0
		}
		return s
	}
	return make([]int8, n)
}

func resizeUint8Slice(s []uint8, n int) []uint8 {
	if cap(s) >= n {
		s = s[:n]
		for i := range s {
			s[i] = 0
		}
		return s
	}
	return make([]uint8, n)
}

func resizeBoolSlice(s []bool, n int) []bool {
	if cap(s) >= n {
		s = s[:n]
		for i := range s {
			s[i] = false
		}
		return s
	}
	return make([]bool, n)
}

func resizeKeyFrameModeSlice(s []vp8enc.KeyFrameMacroblockMode, n int) []vp8enc.KeyFrameMacroblockMode {
	if cap(s) >= n {
		s = s[:n]
		for i := range s {
			s[i] = vp8enc.KeyFrameMacroblockMode{}
		}
		return s
	}
	return make([]vp8enc.KeyFrameMacroblockMode, n)
}

func resizeInterFrameModeSlice(s []vp8enc.InterFrameMacroblockMode, n int) []vp8enc.InterFrameMacroblockMode {
	if cap(s) >= n {
		s = s[:n]
		for i := range s {
			s[i] = vp8enc.InterFrameMacroblockMode{}
		}
		return s
	}
	return make([]vp8enc.InterFrameMacroblockMode, n)
}

func resizeKeyFrameCoeffSlice(s []vp8enc.MacroblockCoefficients, n int) []vp8enc.MacroblockCoefficients {
	if cap(s) >= n {
		s = s[:n]
		for i := range s {
			s[i] = vp8enc.MacroblockCoefficients{}
		}
		return s
	}
	return make([]vp8enc.MacroblockCoefficients, n)
}

func resizeTokenContextSlice(s []vp8enc.TokenContextPlanes, n int) []vp8enc.TokenContextPlanes {
	if cap(s) >= n {
		s = s[:n]
		for i := range s {
			s[i] = vp8enc.TokenContextPlanes{}
		}
		return s
	}
	return make([]vp8enc.TokenContextPlanes, n)
}

func resizeReconstructModeSlice(s []vp8dec.MacroblockMode, n int) []vp8dec.MacroblockMode {
	if cap(s) >= n {
		s = s[:n]
		for i := range s {
			s[i] = vp8dec.MacroblockMode{}
		}
		return s
	}
	return make([]vp8dec.MacroblockMode, n)
}

func resizeReconstructTokensSlice(s []vp8dec.MacroblockTokens, n int) []vp8dec.MacroblockTokens {
	if cap(s) >= n {
		s = s[:n]
		for i := range s {
			s[i] = vp8dec.MacroblockTokens{}
		}
		return s
	}
	return make([]vp8dec.MacroblockTokens, n)
}

