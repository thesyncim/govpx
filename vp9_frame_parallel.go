package govpx

import (
	"image"
	"sync"

	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// vp9FrameParallelScheduler owns the encoder-side concurrent-frame state
// dispatched when VP9EncoderOptions.FrameParallelEncoderThreads is >= 2 and
// LookaheadFrames is non-zero. The scheduler clones the parent VP9Encoder
// state into N independent worker encoders at batch entry, runs each on its
// own goroutine, and stages the produced packets in display order. The
// parent encoder advances its frame counters once the batch retires.
//
// Each worker is dispatched with EncodeNoUpdate{Last,Golden,AltRef,Entropy}
// forced and FrameParallelDecoding=true active, so the per-frame counts are
// never folded back into the shared entropy context and the LAST / GOLDEN /
// ALTREF reference slots stay frozen for the duration of the batch. This
// guarantees byte-identical output relative to a serial encode where each
// frame carries the same NoUpdate flag set and frame_parallel_decoding_mode=1.
type vp9FrameParallelScheduler struct {
	// workers holds the cloned per-frame encoder instances. Indexed 0..N-1.
	// Worker 0 is run on the calling goroutine; workers 1..N-1 run on
	// helper goroutines spawned per batch.
	workers []VP9Encoder

	// scratchDst is the per-worker bitstream output buffer.
	scratchDst [][]byte

	// scratchInputs holds private per-batch copies of the lookahead frame
	// images so the parent's lookahead queue can be drained before workers
	// finish reading their inputs.
	scratchInputs []image.YCbCr

	// results stores per-batch frame results in display order. Reads pop
	// from the front of the queue; writes append at the end.
	results []vp9FrameParallelResult

	// resultsBuf is a slab of byte buffers, one per result slot. Each batch
	// result aliases into the matching slab to avoid per-result allocations.
	resultsBuf [][]byte

	// parentInputs holds the per-batch inputs (flag + frameIndex) that the
	// dispatch reads after popping the lookahead queue. Kept on the scheduler
	// rather than allocated per batch so steady-state batches recycle the
	// slab. libvpx pattern: cpi->twopass.frame_intervals buffer reuse;
	// vp9_encoder.c::vp9_create_compressor sizes everything once.
	parentInputs []vp9FrameParallelBatchInput

	// parentPrevFrameMvsSnapshot / parentPrevSegmentMapSnapshot are the
	// pre-batch copies of the parent's prevFrameMvs / prevSegmentMap that
	// must be restored on batch retire. Owned by the scheduler so steady-
	// state batches recycle the slab instead of allocating fresh per
	// vp9RunFrameParallelBatch call. libvpx pattern: cpi->common.prev_mvs is
	// a fixed CPI-owned buffer; vp9_alloc_context_buffers
	// (vp9_alloccommon.c:160) allocates once and reuses for every frame.
	parentPrevFrameMvsSnapshot   []vp9MvRef
	parentPrevSegmentMapSnapshot []uint8

	wg sync.WaitGroup
}

// vp9FrameParallelBatchInput is the per-slot dispatch input the scheduler
// stages between popVP9Lookahead and the worker goroutines.
type vp9FrameParallelBatchInput struct {
	flags      EncodeFlags
	frameIndex int
}

// vp9FrameParallelResult carries the byte-level output and metadata for one
// completed batch frame.
type vp9FrameParallelResult struct {
	bytes  []byte
	result VP9EncodeResult
	err    error
}

func (s *vp9FrameParallelScheduler) hasPendingResults() bool {
	return s != nil && len(s.results) > 0
}

// release tears down the scheduler. Idempotent. The scheduler does not hold
// any long-lived goroutines (workers are spawned per-batch and join before
// runVP9FrameParallelBatch returns) so this just drops the slices.
func (s *vp9FrameParallelScheduler) release() {
	if s == nil {
		return
	}
	s.workers = nil
	s.scratchDst = nil
	s.scratchInputs = nil
	s.results = nil
	s.resultsBuf = nil
	s.parentInputs = nil
	s.parentPrevFrameMvsSnapshot = nil
	s.parentPrevSegmentMapSnapshot = nil
}

// ensureCapacity grows the scheduler slabs to support a batch of `workers`
// frames with frame-size width x height. The bitstream scratch and result
// bytes are sized to dstSize (the worst-case allocating buffer).
func (s *vp9FrameParallelScheduler) ensureCapacity(workers int, dstSize int, width, height int) {
	if cap(s.workers) < workers {
		s.workers = make([]VP9Encoder, workers)
	} else {
		s.workers = s.workers[:workers]
	}
	if cap(s.scratchDst) < workers {
		s.scratchDst = make([][]byte, workers)
	} else {
		s.scratchDst = s.scratchDst[:workers]
	}
	for i := range s.scratchDst {
		if cap(s.scratchDst[i]) < dstSize {
			s.scratchDst[i] = make([]byte, dstSize)
		} else {
			s.scratchDst[i] = s.scratchDst[i][:dstSize]
		}
	}
	if cap(s.resultsBuf) < workers {
		s.resultsBuf = make([][]byte, workers)
	} else {
		s.resultsBuf = s.resultsBuf[:workers]
	}
	for i := range s.resultsBuf {
		if cap(s.resultsBuf[i]) < dstSize {
			s.resultsBuf[i] = make([]byte, 0, dstSize)
		} else {
			s.resultsBuf[i] = s.resultsBuf[i][:0]
		}
	}
	if cap(s.scratchInputs) < workers {
		s.scratchInputs = make([]image.YCbCr, workers)
	} else {
		s.scratchInputs = s.scratchInputs[:workers]
	}
	rect := image.Rect(0, 0, width, height)
	for i := range s.scratchInputs {
		if s.scratchInputs[i].Rect != rect || len(s.scratchInputs[i].Y) == 0 {
			s.scratchInputs[i] = *image.NewYCbCr(rect, image.YCbCrSubsampleRatio420)
		}
	}
	// libvpx pattern: vp9_encoder.c::vp9_create_compressor allocates
	// the per-frame dispatch buffers once; we grow the per-batch slot
	// slabs the same way so steady-state batches reuse the same backing
	// arrays. miRows*miCols sizes the prev MVs and segment maps to match
	// vp9_alloc_context_buffers (vp9_alloccommon.c:160) which sizes
	// cm->prev_mip / cm->cur_mip from cm->mi_rows * cm->mi_cols.
	if cap(s.parentInputs) < workers {
		s.parentInputs = make([]vp9FrameParallelBatchInput, workers)
	} else {
		s.parentInputs = s.parentInputs[:workers]
	}
	miCols := (width + 7) >> 3
	miRows := (height + 7) >> 3
	need := miRows * miCols
	if cap(s.parentPrevFrameMvsSnapshot) < need {
		s.parentPrevFrameMvsSnapshot = make([]vp9MvRef, need)
	}
	if cap(s.parentPrevSegmentMapSnapshot) < need {
		s.parentPrevSegmentMapSnapshot = make([]uint8, need)
	}
}

// vp9FrameParallelEnabled reports whether the encoder-side frame-parallel
// scheduler should attempt to coalesce lookahead frames into a batch on the
// next EncodeIntoWithFlagsResult call. Requires N >= 2 plus a configured
// lookahead queue.
func (e *VP9Encoder) vp9FrameParallelEnabled() bool {
	return e != nil && e.opts.FrameParallelEncoderThreads >= 2 &&
		e.vp9LookaheadEnabled() && !e.opts.AutoAltRef
}

// vp9FrameParallelEffectiveThreads returns the configured concurrent-frame
// count clamped to the lookahead depth.
func (e *VP9Encoder) vp9FrameParallelEffectiveThreads() int {
	n := e.opts.FrameParallelEncoderThreads
	if n < 2 {
		return 1
	}
	depth := e.opts.LookaheadFrames
	if depth > 0 && n > depth {
		n = depth
	}
	if n > vp9MaxLookaheadFrames {
		n = vp9MaxLookaheadFrames
	}
	return n
}

// ensureVP9FrameParallelScheduler returns a non-nil scheduler when frame
// parallel is enabled, lazily allocating it on the first batch.
func (e *VP9Encoder) ensureVP9FrameParallelScheduler() *vp9FrameParallelScheduler {
	if e == nil || !e.vp9FrameParallelEnabled() {
		return nil
	}
	if e.frameParallel == nil {
		e.frameParallel = &vp9FrameParallelScheduler{}
	}
	return e.frameParallel
}

// vp9PopFrameParallelResultInto drains one staged frame from a completed
// batch into dst. Returns ErrFrameNotReady when the staging ring is empty.
// On success returns (result, true, nil). On error returns (zero, true, err).
// On empty queue returns (zero, false, nil).
func (e *VP9Encoder) vp9PopFrameParallelResultInto(dst []byte) (VP9EncodeResult, bool, error) {
	if e == nil || e.frameParallel == nil || len(e.frameParallel.results) == 0 {
		return VP9EncodeResult{}, false, nil
	}
	staged := e.frameParallel.results[0]
	if staged.err != nil {
		e.frameParallel.results = e.frameParallel.results[1:]
		return VP9EncodeResult{}, true, staged.err
	}
	if len(dst) < len(staged.bytes) {
		return VP9EncodeResult{}, true, ErrBufferTooSmall
	}
	n := copy(dst, staged.bytes)
	result := staged.result
	result.Data = dst[:n]
	e.frameParallel.results = e.frameParallel.results[1:]
	return result, true, nil
}

// vp9RunFrameParallelBatch encodes up to N queued lookahead frames concurrently
// and stages the produced packets in display order. The first staged packet is
// returned in dst; further packets are retrieved by subsequent
// EncodeIntoWithFlagsResult / FlushIntoWithResult calls. drain forces the
// batch to consume every queued frame even if fewer than N are available.
//
// The scheduler declines to parallelize a batch that would include a keyframe
// (the keyframe must refresh every reference slot, which is incompatible with
// the NoUpdate flag mask the batch enforces on its members). In that case the
// caller falls through to the serial path which will emit the keyframe.
func (e *VP9Encoder) vp9RunFrameParallelBatch(dst []byte, drain bool) (VP9EncodeResult, bool, error) {
	if !e.vp9FrameParallelEnabled() {
		return VP9EncodeResult{}, false, nil
	}
	available := e.vp9LookaheadSize()
	if available == 0 {
		return VP9EncodeResult{}, false, nil
	}
	batch := e.vp9FrameParallelEffectiveThreads()
	if !drain && available < batch {
		return VP9EncodeResult{}, false, nil
	}
	if batch > available {
		if !drain {
			return VP9EncodeResult{}, false, nil
		}
		batch = available
	}
	if batch <= 0 {
		return VP9EncodeResult{}, false, nil
	}
	// A batch that opens on a keyframe (the very first frame of a stream or
	// an explicit ForceKeyFrame) cannot be parallelized because keyframes
	// reset every reference slot and the NoUpdate flag mask the batch
	// enforces collides with that requirement. Decline and let the caller
	// fall through to the serial path.
	if e.frameIndex == 0 || e.forceKeyFrame {
		return VP9EncodeResult{}, false, nil
	}
	// Peek at the head of the lookahead queue to check for caller-flagged
	// keyframes within the batch window. If any frame in the prospective
	// batch carries EncodeForceKeyFrame, decline and let the serial path
	// emit that frame; subsequent frames will batch on the next call.
	for i := 0; i < batch; i++ {
		entry, ok := e.peekVP9LookaheadAt(i)
		if !ok {
			break
		}
		if entry.flags&EncodeForceKeyFrame != 0 {
			return VP9EncodeResult{}, false, nil
		}
	}
	// Defensively ensure the parent encoder has populated the LAST slot so
	// the cloned workers have a usable inter-frame reference snapshot. If
	// no reference is valid yet (e.g. the very first frame after construct
	// hasn't run), decline so the first inter frame goes through the
	// serial path which can synthesize the reference state.
	if !e.refValid[vp9LastRefSlot] && !e.refValid[vp9GoldenRefSlot] &&
		!e.refValid[vp9AltRefSlot] {
		return VP9EncodeResult{}, false, nil
	}

	scheduler := e.ensureVP9FrameParallelScheduler()
	if scheduler == nil {
		return VP9EncodeResult{}, false, nil
	}

	dstSize, err := vp9AllocatingEncodeBufferSize(e.opts.Width, e.opts.Height)
	if err != nil {
		return VP9EncodeResult{}, false, err
	}
	scheduler.ensureCapacity(batch, dstSize, e.opts.Width, e.opts.Height)

	// Pop the batch frames from the lookahead and copy into the scheduler's
	// scratch input buffers. The copy is needed because the lookahead ring
	// reuses entries for the next push and because each worker reads its
	// input concurrently while the parent goroutine continues mutating the
	// queue. libvpx pattern: vp9_encoder.c::vp9_create_compressor keeps the
	// per-worker dispatch buffers on the CPI; we likewise read into the
	// scheduler-owned parentInputs slab to avoid a per-batch make().
	inputs := scheduler.parentInputs[:batch]
	baseFrameIndex := e.frameIndex
	baseFramesSinceKey := e.framesSinceKey
	for i := 0; i < batch; i++ {
		entry, ok := e.popVP9Lookahead(true)
		if !ok {
			return VP9EncodeResult{}, false, ErrFrameNotReady
		}
		copyVP9LookaheadImage(&scheduler.scratchInputs[i], &entry.img, e.opts.Width, e.opts.Height)
		inputs[i] = vp9FrameParallelBatchInput{
			flags:      entry.flags,
			frameIndex: baseFrameIndex + i,
		}
		entry.flags = 0
	}

	// Force frame_parallel_decoding_mode=1 across the batch so the workers
	// inherit the bit through the *w = *src clone. Restore the parent's
	// pre-batch setting once the batch retires.
	prevFrameParallelSet := e.opts.FrameParallelDecodingSet
	prevFrameParallel := e.opts.FrameParallelDecoding
	e.opts.FrameParallelDecodingSet = true
	e.opts.FrameParallelDecoding = true

	// Clone the parent state into each worker. Worker 0 stays in place and
	// becomes the "post-batch" parent state; workers 1..N-1 are independent
	// deep clones that own their reconstruction buffers and mode buffers.
	width := e.opts.Width
	height := e.opts.Height
	miRows := (height + 7) >> 3
	miCols := (width + 7) >> 3

	for i := 1; i < batch; i++ {
		worker := &scheduler.workers[i]
		worker.prepareVP9FrameParallelWorker(e, miRows, miCols, width, height)
		worker.frameIndex = inputs[i].frameIndex
		worker.framesSinceKey = baseFramesSinceKey + uint16(i)
		worker.forceKeyFrame = false
	}

	// Stage the results slab for this batch.
	if cap(scheduler.results) < batch {
		scheduler.results = make([]vp9FrameParallelResult, batch)
	} else {
		scheduler.results = scheduler.results[:batch]
	}
	for i := range scheduler.results {
		scheduler.results[i] = vp9FrameParallelResult{}
	}

	// Dispatch helper workers (1..N-1) on goroutines.
	scheduler.wg.Add(batch - 1)
	for i := 1; i < batch; i++ {
		go func(idx int) {
			defer scheduler.wg.Done()
			workerLocal := &scheduler.workers[idx]
			dstBuf := scheduler.scratchDst[idx]
			flags := vp9FrameParallelDispatchFlags(inputs[idx].flags)
			res, encErr := workerLocal.encodeVP9FrameIntoWithFlagsResult(
				&scheduler.scratchInputs[idx], dstBuf, flags, false,
				temporalFrame{LayerCount: 1})
			scheduler.resultsBuf[idx] = append(scheduler.resultsBuf[idx][:0], res.Data...)
			res.Data = scheduler.resultsBuf[idx]
			scheduler.results[idx] = vp9FrameParallelResult{
				bytes:  scheduler.resultsBuf[idx],
				result: res,
				err:    encErr,
			}
		}(i)
	}

	// Snapshot parent state we may need to restore. The per-frame predictor
	// state (prevFrameMvs, prevSegmentMap, lastVP9Header*) is refreshed
	// unconditionally inside encodeVP9FrameIntoWithFlagsResultInternal, so
	// preserving the entry snapshot here keeps every batch member observing
	// the same predictor state across the batch. The prevFrameMvs /
	// prevSegmentMap copies land in scheduler-resident slabs to keep the
	// hot dispatch loop allocation-free (libvpx pattern:
	// vp9_alloccommon.c::vp9_alloc_context_buffers sizes cm->prev_mip once
	// at create time and reuses it for every frame).
	parentRefValid := e.refValid
	parentRefWidth := e.refWidth
	parentRefHeight := e.refHeight
	parentRefSignBias := e.refSignBias
	parentPrevFrameMvsValid := e.prevFrameMvsValid
	parentPrevFrameMvRows := e.prevFrameMvRows
	parentPrevFrameMvCols := e.prevFrameMvCols
	parentPrevFrameMvsLen := len(e.prevFrameMvs)
	if cap(scheduler.parentPrevFrameMvsSnapshot) < parentPrevFrameMvsLen {
		scheduler.parentPrevFrameMvsSnapshot = make([]vp9MvRef, parentPrevFrameMvsLen)
	}
	parentPrevFrameMvs := scheduler.parentPrevFrameMvsSnapshot[:parentPrevFrameMvsLen]
	copy(parentPrevFrameMvs, e.prevFrameMvs)
	parentPrevSegmentMapValid := e.prevSegmentMapValid
	parentPrevSegmentMapRows := e.prevSegmentMapRows
	parentPrevSegmentMapCols := e.prevSegmentMapCols
	parentPrevSegmentMapLen := len(e.prevSegmentMap)
	if cap(scheduler.parentPrevSegmentMapSnapshot) < parentPrevSegmentMapLen {
		scheduler.parentPrevSegmentMapSnapshot = make([]uint8, parentPrevSegmentMapLen)
	}
	parentPrevSegmentMap := scheduler.parentPrevSegmentMapSnapshot[:parentPrevSegmentMapLen]
	copy(parentPrevSegmentMap, e.prevSegmentMap)
	parentPrevFrameActiveMapEnabled := e.prevFrameActiveMapEnabled
	parentLastVP9HeaderFrameType := e.lastVP9HeaderFrameType
	parentLastVP9HeaderValid := e.lastVP9HeaderValid

	// Run worker 0 in-place on the parent encoder.
	flags0 := vp9FrameParallelDispatchFlags(inputs[0].flags)
	e.frameIndex = inputs[0].frameIndex
	// framesSinceKey is unchanged for slot 0 (== parent's pre-batch value).
	res0, err0 := e.encodeVP9FrameIntoWithFlagsResult(
		&scheduler.scratchInputs[0], scheduler.scratchDst[0], flags0, false,
		temporalFrame{LayerCount: 1})
	scheduler.resultsBuf[0] = append(scheduler.resultsBuf[0][:0], res0.Data...)
	res0.Data = scheduler.resultsBuf[0]
	scheduler.results[0] = vp9FrameParallelResult{
		bytes:  scheduler.resultsBuf[0],
		result: res0,
		err:    err0,
	}
	keyFiredAtSlot0 := res0.KeyFrame
	slot0FramesSinceKey := e.framesSinceKey

	// Wait for the helpers to finish before touching parent state further.
	scheduler.wg.Wait()

	// Restore the parent's per-frame predictor state to the pre-batch
	// snapshot. With NoUpdate flags forced across the batch no worker
	// updates reference frames or entropy contexts, but the unconditional
	// post-encode refresh inside encodeVP9FrameIntoWithFlagsResultInternal
	// would otherwise leave the parent's prevFrameMvs / prevSegmentMap /
	// lastVP9Header* set to slot-0's frame state, diverging from the
	// snapshot the next batch's helper workers cloned.
	e.refValid = parentRefValid
	e.refWidth = parentRefWidth
	e.refHeight = parentRefHeight
	e.refSignBias = parentRefSignBias
	e.prevFrameMvsValid = parentPrevFrameMvsValid
	e.prevFrameMvRows = parentPrevFrameMvRows
	e.prevFrameMvCols = parentPrevFrameMvCols
	if cap(e.prevFrameMvs) < len(parentPrevFrameMvs) {
		e.prevFrameMvs = make([]vp9MvRef, len(parentPrevFrameMvs))
	} else {
		e.prevFrameMvs = e.prevFrameMvs[:len(parentPrevFrameMvs)]
	}
	copy(e.prevFrameMvs, parentPrevFrameMvs)
	e.prevSegmentMapValid = parentPrevSegmentMapValid
	e.prevSegmentMapRows = parentPrevSegmentMapRows
	e.prevSegmentMapCols = parentPrevSegmentMapCols
	if cap(e.prevSegmentMap) < len(parentPrevSegmentMap) {
		e.prevSegmentMap = make([]uint8, len(parentPrevSegmentMap))
	} else {
		e.prevSegmentMap = e.prevSegmentMap[:len(parentPrevSegmentMap)]
	}
	copy(e.prevSegmentMap, parentPrevSegmentMap)
	e.prevFrameActiveMapEnabled = parentPrevFrameActiveMapEnabled
	e.lastVP9HeaderFrameType = parentLastVP9HeaderFrameType
	e.lastVP9HeaderValid = parentLastVP9HeaderValid

	// Advance the parent's frame counters by `batch` frames. The frame
	// contexts (e.fc, e.frameContexts) are already in their post-slot-0
	// state; with FrameParallelDecoding forced on, encodeVP9FrameIntoWith
	// FlagsResult does not fold counts back, so the entropy state remains
	// equal to the pre-batch snapshot.
	e.frameIndex = baseFrameIndex + batch
	if keyFiredAtSlot0 {
		// Slot 0 reset framesSinceKey to 1; subsequent batch members each
		// incremented it. Use the slot-0 result as the anchor and add the
		// remaining batch members.
		e.framesSinceKey = slot0FramesSinceKey + uint16(batch-1)
	} else {
		e.framesSinceKey = baseFramesSinceKey + uint16(batch)
	}
	e.forceKeyFrame = false

	// Restore the parent's frame-parallel-decoding-mode bit.
	e.opts.FrameParallelDecodingSet = prevFrameParallelSet
	e.opts.FrameParallelDecoding = prevFrameParallel

	// Return the first staged result via the caller's dst buffer.
	out, ok, err := e.vp9PopFrameParallelResultInto(dst)
	if !ok {
		return VP9EncodeResult{}, true, ErrFrameNotReady
	}
	return out, true, err
}

// vp9FrameParallelDispatchFlags returns the effective per-frame EncodeFlags
// applied to a batch member. The flag set forces NoUpdate on every reference
// slot and entropy so the cloned encoder cannot mutate the parent-visible
// frame contexts or reference frames. The caller's intent flags (key-frame
// requests, invisible-frame markers, etc.) are preserved.
//
// EncodeForceKeyFrame is mutually exclusive with the NoUpdate mask (keyframes
// must refresh every reference slot). The scheduler rejects batches that
// contain a key frame before reaching this helper, so we do not strip the
// mask here.
func vp9FrameParallelDispatchFlags(in EncodeFlags) EncodeFlags {
	if in&EncodeForceKeyFrame != 0 {
		// Defensive: callers must not reach this with a keyframe in the
		// batch; the scheduler declines such batches at peek time.
		return in
	}
	return in | EncodeNoUpdateLast | EncodeNoUpdateGolden |
		EncodeNoUpdateAltRef | EncodeNoUpdateEntropy
}

// prepareVP9FrameParallelWorker deep-clones src's state into w so w can
// encode a complete frame independently on a helper goroutine. Unlike
// prepareVP9TileEncodeWorker (which shares miGrid, reconY/U/V, and reconFrame
// back with src for tile-encode partial writes), this routine preserves the
// worker's own reconstruction and mode-grid buffers so concurrent helper
// frames never alias the parent's recon state.
func (w *VP9Encoder) prepareVP9FrameParallelWorker(src *VP9Encoder, miRows, miCols, width, height int) {
	// Preserve the worker's owned buffers across the *w = *src copy. We do
	// not reuse the parent's slab-allocated buffers because the helpers
	// run concurrently with the parent (worker 0) and would otherwise
	// race on the same backing arrays.
	aboveSegCtx := w.aboveSegCtx
	leftSegCtx := w.leftSegCtx
	miGrid := w.miGrid
	leafDecisions := w.vp9LeafInterDecisions
	keyframeDecisions := w.vp9LeafKeyframeDecisions
	partitionReconScratch := w.partitionReconScratch
	interPredictScratch := w.interPredictScratch
	interPredictor := w.interPredictor
	reconYFull := w.reconYFull
	reconUFull := w.reconUFull
	reconVFull := w.reconVFull
	reconY := w.reconY
	reconU := w.reconU
	reconV := w.reconV
	reconFrame := w.reconFrame
	prevFrameMvs := w.prevFrameMvs
	prevSegmentMap := w.prevSegmentMap
	activeMap := w.activeMap
	lookahead := w.lookahead
	frameParallel := w.frameParallel
	var aboveCtx [vp9dec.MaxMbPlane][]uint8
	var leftCtx [vp9dec.MaxMbPlane][]uint8
	for plane := range vp9dec.MaxMbPlane {
		aboveCtx[plane] = w.planes[plane].AboveContext
		leftCtx[plane] = w.planes[plane].LeftContext
	}

	*w = *src

	// Restore the worker's owned buffer set. The src copy clobbered the
	// slice headers; we now re-point each to the worker-local backing.
	w.aboveSegCtx = aboveSegCtx
	w.leftSegCtx = leftSegCtx
	w.miGrid = miGrid
	// Worker-private leaf-decision cache so helper goroutines don't race
	// the parent's cache when populating per-block picker decisions.
	w.vp9LeafInterDecisions = leafDecisions
	w.vp9LeafKeyframeDecisions = keyframeDecisions
	w.partitionReconScratch = partitionReconScratch
	w.interPredictScratch = interPredictScratch
	w.interPredictor = interPredictor
	w.reconYFull = reconYFull
	w.reconUFull = reconUFull
	w.reconVFull = reconVFull
	w.reconY = reconY
	w.reconU = reconU
	w.reconV = reconV
	w.reconFrame = reconFrame
	w.prevFrameMvs = prevFrameMvs
	w.prevSegmentMap = prevSegmentMap
	w.activeMap = activeMap
	w.lookahead = lookahead
	w.frameParallel = frameParallel
	// Drop helpers that must not be transitively driven by a clone.
	w.vp9CountWorkers = nil
	w.vp9CountCounts = nil
	w.vp9CountJobs = nil
	w.vp9TilePool = nil
	w.vp9RowMTSync = nil
	for plane := range vp9dec.MaxMbPlane {
		w.planes[plane].AboveContext = aboveCtx[plane]
		w.planes[plane].LeftContext = leftCtx[plane]
	}

	// Make sure mode buffers, entropy contexts, and reconstruction buffers
	// are sized correctly for the current frame.
	w.ensureVP9EncoderModeBuffers(miRows, miCols)
	w.prepareVP9EncoderOutputFrame(width, height)
	for i := range w.aboveSegCtx {
		w.aboveSegCtx[i] = 0
	}
	for i := range w.leftSegCtx {
		w.leftSegCtx[i] = 0
	}
	for i := range w.miGrid {
		w.miGrid[i] = vp9dec.NeighborMi{}
	}
	w.resetVP9EncoderAboveEntropyContexts()
	w.resetVP9EncoderLeftEntropyContexts()

	// Snapshot the per-clone copy of frame-level mutable state derived from
	// the source so per-frame mutations on the helper goroutine cannot
	// race with the parent's worker 0 encode. The reference frame backing
	// images are read-only during a NoUpdate-locked encode, so they may be
	// shared with the parent.
	if len(src.prevFrameMvs) > 0 {
		if cap(w.prevFrameMvs) < len(src.prevFrameMvs) {
			w.prevFrameMvs = make([]vp9MvRef, len(src.prevFrameMvs))
		} else {
			w.prevFrameMvs = w.prevFrameMvs[:len(src.prevFrameMvs)]
		}
		copy(w.prevFrameMvs, src.prevFrameMvs)
	}
	if len(src.prevSegmentMap) > 0 {
		if cap(w.prevSegmentMap) < len(src.prevSegmentMap) {
			w.prevSegmentMap = make([]uint8, len(src.prevSegmentMap))
		} else {
			w.prevSegmentMap = w.prevSegmentMap[:len(src.prevSegmentMap)]
		}
		copy(w.prevSegmentMap, src.prevSegmentMap)
	}
	if len(src.activeMap) > 0 {
		if cap(w.activeMap) < len(src.activeMap) {
			w.activeMap = make([]uint8, len(src.activeMap))
		} else {
			w.activeMap = w.activeMap[:len(src.activeMap)]
		}
		copy(w.activeMap, src.activeMap)
	}
}
