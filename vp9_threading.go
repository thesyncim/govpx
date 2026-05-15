package govpx

import (
	"encoding/binary"
	"errors"
	"image"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

type vp9CountTileSeed struct {
	keyHeader          vp9dec.UncompressedHeader
	keyImg             *image.YCbCr
	interImg           *image.YCbCr
	interSelectFc      vp9dec.FrameContext
	interCompoundRefs  vp9dec.CompoundFrameRefs
	interRefSignBias   [vp9dec.MaxRefFrames]uint8
	counts             *encoder.FrameCounts
	interRefMask       uint8
	interReferenceMode vp9dec.ReferenceMode
	interInterpFilter  vp9dec.InterpFilter
	keyLossless        bool
	interAllowHP       bool
	interCompound      bool
	interLossless      bool
	hasKey             bool
	hasKeyHeader       bool
	hasInter           bool
}

type vp9CountTileJob struct {
	partitionProbs [common.PartitionContexts][common.PartitionTypes - 1]uint8
	seg            vp9dec.SegmentationParams
	baseMi         vp9dec.NeighborMi
	tile           vp9dec.TileBounds
	keyHeader      vp9dec.UncompressedHeader
	key            vp9KeyframeEncodeState
	inter          vp9InterEncodeState
	worker         *VP9Encoder
	miRows         int
	miCols         int
	txMode         common.TxMode
	kind           vp9ModeTreeKind
	hasKey         bool
	hasInter       bool
}

type vp9EncodeTileJob struct {
	partitionProbs [common.PartitionContexts][common.PartitionTypes - 1]uint8
	seg            vp9dec.SegmentationParams
	baseMi         vp9dec.NeighborMi
	tile           vp9dec.TileBounds
	keyHeader      vp9dec.UncompressedHeader
	key            vp9KeyframeEncodeState
	inter          vp9InterEncodeState
	worker         *VP9Encoder
	output         []byte
	miRows         int
	miCols         int
	size           int
	txMode         common.TxMode
	kind           vp9ModeTreeKind
	err            error
	hasKey         bool
	hasInter       bool
}

type vp9TileWorkerPool struct {
	workers     []VP9Encoder
	countJobs   []vp9CountTileJob
	countCounts []encoder.FrameCounts
	encodeJobs  []vp9EncodeTileJob
	outputs     [][]byte
	outputSize  int

	start     []chan struct{}
	shutdown  chan struct{}
	wg        sync.WaitGroup
	doneCount atomic.Int32

	jobKind     vp9TileWorkerJobKind
	workerCount int
}

type vp9TileWorkerJobKind uint8

const (
	vp9TileWorkerJobEncode vp9TileWorkerJobKind = iota
	vp9TileWorkerJobCount
)

func (e *VP9Encoder) initVP9TileWorkerPool() {
	if e == nil || e.opts.Threads <= 1 {
		return
	}
	miCols := (e.opts.Width + 7) >> 3
	tileInfo := vp9EncoderTileInfo(miCols, e.opts.Threads)
	if tileInfo.Log2TileRows != 0 {
		return
	}
	tileCols := 1 << uint(tileInfo.Log2TileCols)
	if tileCols <= 1 {
		return
	}
	pool := e.ensureVP9TileWorkerPool(tileCols)
	if pool == nil {
		return
	}
	if size, err := vp9AllocatingEncodeBufferSize(e.opts.Width, e.opts.Height); err == nil {
		pool.ensureOutputSize(size)
	}
}

func (e *VP9Encoder) ensureVP9TileWorkerPool(tileCols int) *vp9TileWorkerPool {
	if e == nil || e.opts.Threads <= 1 || tileCols <= 1 {
		return nil
	}
	if pool := e.vp9TilePool; pool != nil && pool.workerCount == tileCols {
		return pool
	}
	if e.vp9TilePool != nil {
		e.vp9TilePool.shutdownPool()
	}
	pool := newVP9TileWorkerPool(tileCols)
	if pool == nil {
		e.vp9TilePool = nil
		e.vp9CountWorkers = nil
		e.vp9CountCounts = nil
		e.vp9CountJobs = nil
		return nil
	}
	e.vp9TilePool = pool
	e.vp9CountWorkers = pool.workers
	e.vp9CountCounts = pool.countCounts
	e.vp9CountJobs = pool.countJobs
	return pool
}

func newVP9TileWorkerPool(workers int) *vp9TileWorkerPool {
	if workers <= 1 {
		return nil
	}
	pool := &vp9TileWorkerPool{
		workers:     make([]VP9Encoder, workers),
		countJobs:   make([]vp9CountTileJob, workers),
		countCounts: make([]encoder.FrameCounts, workers),
		encodeJobs:  make([]vp9EncodeTileJob, workers),
		outputs:     make([][]byte, workers),
		start:       make([]chan struct{}, workers),
		shutdown:    make(chan struct{}),
		workerCount: workers,
	}
	for i := 1; i < workers; i++ {
		start := make(chan struct{})
		pool.start[i] = start
		pool.wg.Add(1)
		go pool.workerLoop(i, start)
	}
	return pool
}

func (p *vp9TileWorkerPool) ensureOutputSize(size int) {
	if p == nil || size <= 0 {
		return
	}
	if p.outputSize == size {
		return
	}
	for i := range p.outputs {
		if cap(p.outputs[i]) < size {
			p.outputs[i] = make([]byte, size)
		} else {
			p.outputs[i] = p.outputs[i][:size]
		}
	}
	p.outputSize = size
}

func (p *vp9TileWorkerPool) workerLoop(workerIndex int, start <-chan struct{}) {
	defer p.wg.Done()
	for {
		if !p.waitForWorkerStart(start) {
			return
		}
		if workerIndex < p.workerCount {
			switch p.jobKind {
			case vp9TileWorkerJobCount:
				runVP9CountTileJobNoWG(&p.countJobs[workerIndex])
			default:
				runVP9EncodeTileJob(&p.encodeJobs[workerIndex])
			}
		}
		p.doneCount.Add(1)
	}
}

func (p *vp9TileWorkerPool) waitForWorkerStart(start <-chan struct{}) bool {
	for spins := range rowWorkerIdleSpinBudget {
		select {
		case _, ok := <-start:
			return ok
		default:
		}
		runtimeProcYield(30)
		if spins >= rowWorkerIdleSchedulerBackoff && spins%rowWorkerIdleSchedulerBackoff == 0 {
			runtime.Gosched()
		}
	}
	_, ok := <-start
	return ok
}

func (p *vp9TileWorkerPool) startHelperWorkers() {
	if p == nil || p.workerCount <= 1 {
		return
	}
	p.doneCount.Store(0)
	for workerIndex := 1; workerIndex < p.workerCount; workerIndex++ {
		p.start[workerIndex] <- struct{}{}
	}
}

func (p *vp9TileWorkerPool) waitHelperWorkers() {
	if p == nil || p.workerCount <= 1 {
		return
	}
	want := int32(p.workerCount - 1)
	for spins := 0; p.doneCount.Load() < want; spins++ {
		runtimeProcYield(30)
		if spins >= rowWorkerIdleSchedulerBackoff && spins%rowWorkerIdleSchedulerBackoff == 0 {
			runtime.Gosched()
		}
	}
}

func (p *vp9TileWorkerPool) shutdownPool() {
	if p == nil {
		return
	}
	select {
	case <-p.shutdown:
	default:
		close(p.shutdown)
		for workerIndex := 1; workerIndex < len(p.start); workerIndex++ {
			close(p.start[workerIndex])
		}
	}
	p.wg.Wait()
}

func vp9CountTileSeedForState(key *vp9KeyframeEncodeState,
	inter *vp9InterEncodeState,
) vp9CountTileSeed {
	var seed vp9CountTileSeed
	if key != nil {
		seed.hasKey = true
		seed.keyImg = key.img
		seed.keyLossless = key.lossless
		seed.counts = key.counts
		if key.hdr != nil {
			seed.keyHeader = *key.hdr
			seed.hasKeyHeader = true
		}
	}
	if inter != nil {
		seed.hasInter = true
		seed.interImg = inter.img
		seed.counts = inter.counts
		seed.interSelectFc = inter.selectFc
		seed.interCompoundRefs = inter.compoundRefs
		seed.interRefSignBias = inter.refSignBias
		seed.interRefMask = inter.refMask
		seed.interReferenceMode = inter.referenceMode
		seed.interInterpFilter = inter.interpFilter
		seed.interAllowHP = inter.allowHP
		seed.interCompound = inter.compoundAllowed
		seed.interLossless = inter.lossless
	}
	return seed
}

func (e *VP9Encoder) collectVP9FrameTileCountsThreaded(width, height, miRows, miCols int,
	tileInfo vp9dec.TileInfo,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi, txMode common.TxMode,
	kind vp9ModeTreeKind, seed vp9CountTileSeed,
) bool {
	tileRows := 1 << uint(tileInfo.Log2TileRows)
	tileCols := 1 << uint(tileInfo.Log2TileCols)
	if e == nil || e.opts.Threads <= 1 || tileRows != 1 || tileCols <= 1 {
		return false
	}
	dstCounts := seed.counts
	if dstCounts == nil {
		return false
	}
	if e.collectVP9FrameTileCountsWithPool(width, height, miRows, miCols,
		tileInfo, partitionProbs, seg, baseMi, txMode, kind, seed, dstCounts) {
		return true
	}
	e.ensureVP9CountWorkers(tileCols)

	var wg sync.WaitGroup
	wg.Add(tileCols)
	for tileCol := range tileCols {
		worker := &e.vp9CountWorkers[tileCol]
		counts := &e.vp9CountCounts[tileCol]
		job := &e.vp9CountJobs[tileCol]
		*counts = encoder.FrameCounts{}
		worker.prepareVP9CountWorker(e, width, height, miRows, miCols)
		prepareVP9CountTileJob(job, worker, counts, miRows, miCols,
			vp9EncoderTileBounds(0, tileCol, miRows, miCols, tileInfo),
			partitionProbs, seg, baseMi, txMode, kind, seed)
		go runVP9CountTileJob(job, &wg)
	}
	wg.Wait()

	for i := range tileCols {
		addVP9FrameCounts(dstCounts, &e.vp9CountCounts[i])
	}
	return true
}

func (e *VP9Encoder) collectVP9FrameTileCountsWithPool(width, height, miRows, miCols int,
	tileInfo vp9dec.TileInfo,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi, txMode common.TxMode,
	kind vp9ModeTreeKind, seed vp9CountTileSeed, dstCounts *encoder.FrameCounts,
) bool {
	tileCols := 1 << uint(tileInfo.Log2TileCols)
	pool := e.ensureVP9TileWorkerPool(tileCols)
	if pool == nil || pool.workerCount != tileCols || dstCounts == nil {
		return false
	}
	e.vp9CountWorkers = pool.workers
	e.vp9CountCounts = pool.countCounts
	e.vp9CountJobs = pool.countJobs
	for tileCol := range tileCols {
		worker := &pool.workers[tileCol]
		counts := &pool.countCounts[tileCol]
		job := &pool.countJobs[tileCol]
		*counts = encoder.FrameCounts{}
		worker.prepareVP9CountWorker(e, width, height, miRows, miCols)
		prepareVP9CountTileJob(job, worker, counts, miRows, miCols,
			vp9EncoderTileBounds(0, tileCol, miRows, miCols, tileInfo),
			partitionProbs, seg, baseMi, txMode, kind, seed)
	}
	pool.jobKind = vp9TileWorkerJobCount
	pool.startHelperWorkers()
	runVP9CountTileJobNoWG(&pool.countJobs[0])
	pool.waitHelperWorkers()
	for i := range tileCols {
		addVP9FrameCounts(dstCounts, &pool.countCounts[i])
	}
	return true
}

func (e *VP9Encoder) writeVP9FrameTilesThreadedEnabled(tileRows, tileCols int) bool {
	return e != nil && e.opts.Threads > 1 && tileRows == 1 && tileCols > 1
}

func (e *VP9Encoder) writeVP9FrameTilesThreaded(output []byte, miRows, miCols int,
	tileInfo vp9dec.TileInfo,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi, txMode common.TxMode,
	kind vp9ModeTreeKind, key *vp9KeyframeEncodeState, inter *vp9InterEncodeState,
) (int, error, bool) {
	tileRows := 1 << uint(tileInfo.Log2TileRows)
	tileCols := 1 << uint(tileInfo.Log2TileCols)
	if !e.writeVP9FrameTilesThreadedEnabled(tileRows, tileCols) {
		return 0, nil, false
	}
	pool := e.ensureVP9TileWorkerPool(tileCols)
	if pool == nil || pool.workerCount != tileCols {
		return 0, nil, false
	}
	pool.ensureOutputSize(len(output))
	seed := vp9CountTileSeedForState(key, inter)
	for tileCol := range tileCols {
		worker := e
		if tileCol > 0 {
			worker = &pool.workers[tileCol]
			worker.prepareVP9TileEncodeWorker(e, miRows, miCols)
		}
		prepareVP9EncodeTileJob(&pool.encodeJobs[tileCol], worker,
			pool.outputs[tileCol], miRows, miCols,
			vp9EncoderTileBounds(0, tileCol, miRows, miCols, tileInfo),
			partitionProbs, seg, baseMi, txMode, kind, seed)
	}

	pool.jobKind = vp9TileWorkerJobEncode
	pool.startHelperWorkers()
	runVP9EncodeTileJob(&pool.encodeJobs[0])
	pool.waitHelperWorkers()

	totalSize := 0
	for idx := range tileCols {
		job := &pool.encodeJobs[idx]
		if job.err != nil {
			return totalSize, job.err, true
		}
		isLast := idx == tileCols-1
		if !isLast {
			if totalSize+4 > len(output) {
				return totalSize, encoder.ErrTileBufferFull, true
			}
			binary.BigEndian.PutUint32(output[totalSize:totalSize+4], uint32(job.size))
			totalSize += 4
		}
		if job.size > len(output)-totalSize {
			return totalSize, encoder.ErrTileBufferFull, true
		}
		copy(output[totalSize:], job.output[:job.size])
		totalSize += job.size
	}
	return totalSize, nil, true
}

func (e *VP9Encoder) ensureVP9CountWorkers(workers int) {
	if workers <= 0 {
		return
	}
	if cap(e.vp9CountWorkers) < workers {
		e.vp9CountWorkers = make([]VP9Encoder, workers)
	} else {
		e.vp9CountWorkers = e.vp9CountWorkers[:workers]
	}
	if cap(e.vp9CountCounts) < workers {
		e.vp9CountCounts = make([]encoder.FrameCounts, workers)
	} else {
		e.vp9CountCounts = e.vp9CountCounts[:workers]
	}
	if cap(e.vp9CountJobs) < workers {
		e.vp9CountJobs = make([]vp9CountTileJob, workers)
	} else {
		e.vp9CountJobs = e.vp9CountJobs[:workers]
	}
}

func (w *VP9Encoder) prepareVP9CountWorker(src *VP9Encoder, width, height, miRows, miCols int) {
	aboveSegCtx := w.aboveSegCtx
	leftSegCtx := w.leftSegCtx
	miGrid := w.miGrid
	partitionReconScratch := w.partitionReconScratch
	interPredictScratch := w.interPredictScratch
	interPredictor := w.interPredictor
	reconYFull := w.reconYFull
	reconUFull := w.reconUFull
	reconVFull := w.reconVFull
	var aboveCtx [vp9dec.MaxMbPlane][]uint8
	var leftCtx [vp9dec.MaxMbPlane][]uint8
	for plane := range vp9dec.MaxMbPlane {
		aboveCtx[plane] = w.planes[plane].AboveContext
		leftCtx[plane] = w.planes[plane].LeftContext
	}

	*w = *src
	w.aboveSegCtx = aboveSegCtx
	w.leftSegCtx = leftSegCtx
	w.miGrid = miGrid
	w.partitionReconScratch = partitionReconScratch
	w.interPredictScratch = interPredictScratch
	w.interPredictor = interPredictor
	w.reconYFull = reconYFull
	w.reconUFull = reconUFull
	w.reconVFull = reconVFull
	w.vp9CountWorkers = nil
	w.vp9CountCounts = nil
	w.vp9CountJobs = nil
	w.vp9TilePool = nil
	for plane := range vp9dec.MaxMbPlane {
		w.planes[plane].AboveContext = aboveCtx[plane]
		w.planes[plane].LeftContext = leftCtx[plane]
	}
	w.ensureVP9EncoderModeBuffers(miRows, miCols)
	w.resetVP9EncoderCodingState(width, height)
}

func (w *VP9Encoder) prepareVP9TileEncodeWorker(src *VP9Encoder, miRows, miCols int) {
	aboveSegCtx := w.aboveSegCtx
	leftSegCtx := w.leftSegCtx
	miGrid := w.miGrid
	partitionReconScratch := w.partitionReconScratch
	interPredictScratch := w.interPredictScratch
	interPredictor := w.interPredictor
	reconYFull := w.reconYFull
	reconUFull := w.reconUFull
	reconVFull := w.reconVFull
	var aboveCtx [vp9dec.MaxMbPlane][]uint8
	var leftCtx [vp9dec.MaxMbPlane][]uint8
	for plane := range vp9dec.MaxMbPlane {
		aboveCtx[plane] = w.planes[plane].AboveContext
		leftCtx[plane] = w.planes[plane].LeftContext
	}

	*w = *src
	w.aboveSegCtx = aboveSegCtx
	w.leftSegCtx = leftSegCtx
	w.miGrid = miGrid
	w.partitionReconScratch = partitionReconScratch
	w.interPredictScratch = interPredictScratch
	w.interPredictor = interPredictor
	w.reconYFull = reconYFull
	w.reconUFull = reconUFull
	w.reconVFull = reconVFull
	w.vp9CountWorkers = nil
	w.vp9CountCounts = nil
	w.vp9CountJobs = nil
	w.vp9TilePool = nil
	for plane := range vp9dec.MaxMbPlane {
		w.planes[plane].AboveContext = aboveCtx[plane]
		w.planes[plane].LeftContext = leftCtx[plane]
	}
	w.ensureVP9EncoderModeBuffers(miRows, miCols)
	for i := range w.aboveSegCtx {
		w.aboveSegCtx[i] = 0
	}
	for i := range w.leftSegCtx {
		w.leftSegCtx[i] = 0
	}
	w.resetVP9EncoderAboveEntropyContexts()
	w.resetVP9EncoderLeftEntropyContexts()
	w.miGrid = src.miGrid
	w.reconYFull = src.reconYFull
	w.reconUFull = src.reconUFull
	w.reconVFull = src.reconVFull
	w.reconY = src.reconY
	w.reconU = src.reconU
	w.reconV = src.reconV
	w.reconFrame = src.reconFrame
}

func prepareVP9CountTileJob(job *vp9CountTileJob, worker *VP9Encoder,
	counts *encoder.FrameCounts, miRows, miCols int, tile vp9dec.TileBounds,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi, txMode common.TxMode,
	kind vp9ModeTreeKind, seed vp9CountTileSeed,
) {
	*job = vp9CountTileJob{
		partitionProbs: *partitionProbs,
		seg:            *seg,
		baseMi:         baseMi,
		tile:           tile,
		worker:         worker,
		miRows:         miRows,
		miCols:         miCols,
		txMode:         txMode,
		kind:           kind,
	}
	if seed.hasKey {
		job.hasKey = true
		job.key = vp9KeyframeEncodeState{
			img:      seed.keyImg,
			dq:       &worker.dqScratch,
			lossless: seed.keyLossless,
			counts:   counts,
		}
		if seed.hasKeyHeader {
			job.keyHeader = seed.keyHeader
			job.key.hdr = &job.keyHeader
		}
	}
	if seed.hasInter {
		job.hasInter = true
		job.inter = vp9InterEncodeState{
			img:             seed.interImg,
			dq:              &worker.dqScratch,
			ref:             &worker.refFrames[0],
			refMask:         seed.interRefMask,
			allowHP:         seed.interAllowHP,
			selectFc:        seed.interSelectFc,
			referenceMode:   seed.interReferenceMode,
			compoundAllowed: seed.interCompound,
			refSignBias:     seed.interRefSignBias,
			compoundRefs:    seed.interCompoundRefs,
			interpFilter:    seed.interInterpFilter,
			lossless:        seed.interLossless,
			counts:          counts,
		}
	}
}

func prepareVP9EncodeTileJob(job *vp9EncodeTileJob, worker *VP9Encoder,
	output []byte, miRows, miCols int, tile vp9dec.TileBounds,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi, txMode common.TxMode,
	kind vp9ModeTreeKind, seed vp9CountTileSeed,
) {
	*job = vp9EncodeTileJob{
		partitionProbs: *partitionProbs,
		seg:            *seg,
		baseMi:         baseMi,
		tile:           tile,
		worker:         worker,
		output:         output,
		miRows:         miRows,
		miCols:         miCols,
		txMode:         txMode,
		kind:           kind,
	}
	if seed.hasKey {
		job.hasKey = true
		job.key = vp9KeyframeEncodeState{
			img:      seed.keyImg,
			dq:       &worker.dqScratch,
			lossless: seed.keyLossless,
		}
		if seed.hasKeyHeader {
			job.keyHeader = seed.keyHeader
			job.key.hdr = &job.keyHeader
		}
	}
	if seed.hasInter {
		job.hasInter = true
		job.inter = vp9InterEncodeState{
			img:             seed.interImg,
			dq:              &worker.dqScratch,
			ref:             &worker.refFrames[0],
			refMask:         seed.interRefMask,
			allowHP:         seed.interAllowHP,
			selectFc:        seed.interSelectFc,
			referenceMode:   seed.interReferenceMode,
			compoundAllowed: seed.interCompound,
			refSignBias:     seed.interRefSignBias,
			compoundRefs:    seed.interCompoundRefs,
			interpFilter:    seed.interInterpFilter,
			lossless:        seed.interLossless,
		}
	}
}

func runVP9CountTileJob(job *vp9CountTileJob, wg *sync.WaitGroup) {
	defer wg.Done()
	runVP9CountTileJobNoWG(job)
}

func runVP9CountTileJobNoWG(job *vp9CountTileJob) {
	var countKey *vp9KeyframeEncodeState
	if job.hasKey {
		countKey = &job.key
	}
	var countInter *vp9InterEncodeState
	if job.hasInter {
		countInter = &job.inter
	}
	var bw bitstream.Writer
	bw.Start(job.worker.scratch[:])
	job.worker.writeVP9FrameTile(&bw, job.miRows, job.miCols, job.tile,
		&job.partitionProbs, &job.seg, job.baseMi, job.txMode, job.kind,
		countKey, countInter)
	_, _ = bw.Stop()
}

func runVP9EncodeTileJob(job *vp9EncodeTileJob) {
	var key *vp9KeyframeEncodeState
	if job.hasKey {
		key = &job.key
	}
	var inter *vp9InterEncodeState
	if job.hasInter {
		inter = &job.inter
	}
	job.size = 0
	job.err = nil
	var bw bitstream.Writer
	bw.Start(job.output)
	job.worker.writeVP9FrameTile(&bw, job.miRows, job.miCols, job.tile,
		&job.partitionProbs, &job.seg, job.baseMi, job.txMode, job.kind,
		key, inter)
	size, err := bw.Stop()
	if err != nil {
		if errors.Is(err, bitstream.ErrBufferOverflow) {
			err = encoder.ErrTileBufferFull
		}
		job.err = err
		return
	}
	job.size = size
}

func addVP9FrameCounts(dst *encoder.FrameCounts, src *encoder.FrameCounts) {
	if dst == nil || src == nil {
		return
	}
	for tx := range dst.CoefBranchStats {
		for plane := range dst.CoefBranchStats[tx] {
			for ref := range dst.CoefBranchStats[tx][plane] {
				for band := range dst.CoefBranchStats[tx][plane][ref] {
					for ctx := range dst.CoefBranchStats[tx][plane][ref][band] {
						for node := range dst.CoefBranchStats[tx][plane][ref][band][ctx] {
							dst.CoefBranchStats[tx][plane][ref][band][ctx][node][0] +=
								src.CoefBranchStats[tx][plane][ref][band][ctx][node][0]
							dst.CoefBranchStats[tx][plane][ref][band][ctx][node][1] +=
								src.CoefBranchStats[tx][plane][ref][band][ctx][node][1]
						}
					}
				}
			}
		}
	}
	for i := range dst.TxTotals {
		dst.TxTotals[i] += src.TxTotals[i]
	}
	for i := range dst.TxMode.P8x8 {
		for j := range dst.TxMode.P8x8[i] {
			dst.TxMode.P8x8[i][j] += src.TxMode.P8x8[i][j]
		}
		for j := range dst.TxMode.P16x16[i] {
			dst.TxMode.P16x16[i][j] += src.TxMode.P16x16[i][j]
		}
		for j := range dst.TxMode.P32x32[i] {
			dst.TxMode.P32x32[i][j] += src.TxMode.P32x32[i][j]
		}
	}
	for i := range dst.Skip {
		dst.Skip[i][0] += src.Skip[i][0]
		dst.Skip[i][1] += src.Skip[i][1]
	}
	for i := range dst.InterMode {
		for j := range dst.InterMode[i] {
			dst.InterMode[i][j] += src.InterMode[i][j]
		}
	}
	for i := range dst.SwitchableInterp {
		for j := range dst.SwitchableInterp[i] {
			dst.SwitchableInterp[i][j] += src.SwitchableInterp[i][j]
		}
	}
	for i := range dst.IntraInter {
		dst.IntraInter[i][0] += src.IntraInter[i][0]
		dst.IntraInter[i][1] += src.IntraInter[i][1]
	}
	for i := range dst.ReferenceMode.CompInter {
		dst.ReferenceMode.CompInter[i][0] += src.ReferenceMode.CompInter[i][0]
		dst.ReferenceMode.CompInter[i][1] += src.ReferenceMode.CompInter[i][1]
	}
	for i := range dst.ReferenceMode.SingleRef {
		for j := range dst.ReferenceMode.SingleRef[i] {
			dst.ReferenceMode.SingleRef[i][j][0] += src.ReferenceMode.SingleRef[i][j][0]
			dst.ReferenceMode.SingleRef[i][j][1] += src.ReferenceMode.SingleRef[i][j][1]
		}
	}
	for i := range dst.ReferenceMode.CompRef {
		dst.ReferenceMode.CompRef[i][0] += src.ReferenceMode.CompRef[i][0]
		dst.ReferenceMode.CompRef[i][1] += src.ReferenceMode.CompRef[i][1]
	}
	for i := range dst.YMode {
		for j := range dst.YMode[i] {
			dst.YMode[i][j] += src.YMode[i][j]
		}
	}
	for i := range dst.Partition {
		for j := range dst.Partition[i] {
			dst.Partition[i][j] += src.Partition[i][j]
		}
	}
	for i := range dst.Mv.Joints {
		dst.Mv.Joints[i] += src.Mv.Joints[i]
	}
	for comp := range dst.Mv.Comps {
		addVP9NmvComponentCounts(&dst.Mv.Comps[comp], &src.Mv.Comps[comp])
	}
}

func addVP9NmvComponentCounts(dst *encoder.NmvComponentCounts, src *encoder.NmvComponentCounts) {
	for i := range dst.Sign {
		dst.Sign[i] += src.Sign[i]
	}
	for i := range dst.Classes {
		dst.Classes[i] += src.Classes[i]
	}
	for i := range dst.Class0 {
		dst.Class0[i] += src.Class0[i]
	}
	for i := range dst.Bits {
		dst.Bits[i][0] += src.Bits[i][0]
		dst.Bits[i][1] += src.Bits[i][1]
	}
	for i := range dst.Class0Fp {
		for j := range dst.Class0Fp[i] {
			dst.Class0Fp[i][j] += src.Class0Fp[i][j]
		}
	}
	for i := range dst.Fp {
		dst.Fp[i] += src.Fp[i]
	}
	for i := range dst.Class0Hp {
		dst.Class0Hp[i] += src.Class0Hp[i]
	}
	for i := range dst.Hp {
		dst.Hp[i] += src.Hp[i]
	}
}
