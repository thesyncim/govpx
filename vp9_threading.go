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
	interModeCostFc    vp9dec.FrameContext
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
	interModeCostValid bool
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
	rowMTSync      *vp9RowMTSync
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

	// rowMTSyncs holds one vp9RowMTSync per tile column. Allocated lazily by
	// ensureRowMTSync when the RowMT option is enabled; released by
	// releaseRowMTSync when the option toggles off. The slice is sized to
	// workerCount so each tile column body can index it by tileCol.
	rowMTSyncs []vp9RowMTSync

	// rowWorkerPools holds one vp9RowWorkerPool per tile column, allocated
	// lazily by ensureRowWorkers when RowMT is enabled. Each pool spins up
	// min(rowMTThreads, sbRows) goroutines that dispatch SB rows under the
	// matching rowMTSyncs[tileCol] wavefront. releaseRowWorkers tears the
	// pools down when SetRowMT(false) flips the option off; shutdownPool
	// closes them when the encoder is reconfigured or finalized.
	rowWorkerPools []*vp9RowWorkerPool

	// rowMTThreadCount caches the per-tile-column row worker count selected
	// by the most recent ensureRowWorkers call. Reset across pool rebuilds.
	rowMTThreadCount int

	jobKind     vp9TileWorkerJobKind
	workerCount int
}

// vp9RowMTSync mirrors libvpx's VP9RowMTSync (vp9/encoder/vp9_ethread.h). It
// tracks the latest column index encoded for each SB row inside a tile column
// and exposes Read / Write primitives matching vp9_row_mt_sync_read /
// vp9_row_mt_sync_write. SyncRange (libvpx's sync_range) caps how far ahead a
// row can advance before signalling, which lets future per-row workers stay
// within the configured wavefront slack. The govpx tile-column body still runs
// on a single goroutine so the Read calls never block; the primitive remains
// fully exercised so byte-identical output is preserved and the wavefront
// foundation is in place for actual per-row parallelism.
type vp9RowMTSync struct {
	mu        []sync.Mutex
	cond      []*sync.Cond
	curCol    []int32
	rows      int
	syncRange int
}

// vp9RowMTSyncDefaultRange mirrors libvpx's row_mt_sync->sync_range = 1 init in
// vp9_row_mt_sync_mem_alloc. A range of one means each completed SB column
// signals the row below; larger values reduce signalling frequency at the cost
// of more wavefront slack.
const vp9RowMTSyncDefaultRange = 1

func (s *vp9RowMTSync) reset(rows int) {
	if s == nil || rows <= 0 {
		return
	}
	if cap(s.mu) < rows {
		s.mu = make([]sync.Mutex, rows)
		s.cond = make([]*sync.Cond, rows)
		s.curCol = make([]int32, rows)
		for r := range s.cond {
			s.cond[r] = sync.NewCond(&s.mu[r])
		}
	} else {
		s.mu = s.mu[:rows]
		s.cond = s.cond[:rows]
		s.curCol = s.curCol[:rows]
		for r := range s.cond {
			if s.cond[r] == nil {
				s.cond[r] = sync.NewCond(&s.mu[r])
			}
		}
	}
	for r := range s.curCol {
		s.curCol[r] = -1
	}
	s.rows = rows
	if s.syncRange == 0 {
		s.syncRange = vp9RowMTSyncDefaultRange
	}
}

func (s *vp9RowMTSync) release() {
	if s == nil {
		return
	}
	s.mu = s.mu[:0]
	s.cond = s.cond[:0]
	s.curCol = s.curCol[:0]
	s.rows = 0
}

// read mirrors vp9_row_mt_sync_read. It blocks until the row above has reached
// column position c - syncRange + 1, ensuring the above and above-right SBs
// are encoded before the caller proceeds. The implementation matches libvpx's
// pthread_mutex / pthread_cond_wait shape exactly.
func (s *vp9RowMTSync) read(r, c int) {
	if s == nil || r <= 0 || r >= s.rows {
		return
	}
	nsync := s.syncRange
	if nsync <= 0 || (c&(nsync-1)) != 0 {
		return
	}
	target := int32(c - nsync + 1)
	mu := &s.mu[r-1]
	mu.Lock()
	for s.curCol[r-1] < target {
		s.cond[r-1].Wait()
	}
	mu.Unlock()
}

// write mirrors vp9_row_mt_sync_write. It records the row's current column
// position and broadcasts to waiters when crossing a syncRange boundary or
// when the row finishes. cols is the SB column count for the tile.
func (s *vp9RowMTSync) write(r, c, cols int) {
	if s == nil || r < 0 || r >= s.rows {
		return
	}
	nsync := s.syncRange
	if nsync <= 0 {
		return
	}
	var cur int32
	sig := true
	if c < cols-1 {
		cur = int32(c)
		if c%nsync != nsync-1 {
			sig = false
		}
	} else {
		cur = int32(cols + nsync)
	}
	if !sig {
		return
	}
	mu := &s.mu[r]
	mu.Lock()
	s.curCol[r] = cur
	mu.Unlock()
	s.cond[r].Signal()
}

// ensureRowMTSync arms one vp9RowMTSync per tile column sized to sbRows when
// the RowMT option is enabled. It is called from the per-frame encode path
// just before dispatching workers so steady-state allocations only happen on
// dimension changes.
func (p *vp9TileWorkerPool) ensureRowMTSync(sbRows int) {
	if p == nil || p.workerCount <= 0 || sbRows <= 0 {
		return
	}
	if cap(p.rowMTSyncs) < p.workerCount {
		p.rowMTSyncs = make([]vp9RowMTSync, p.workerCount)
	} else {
		p.rowMTSyncs = p.rowMTSyncs[:p.workerCount]
	}
	for i := range p.rowMTSyncs {
		p.rowMTSyncs[i].reset(sbRows)
	}
}

// releaseRowMTSync drops the per-tile-column vp9RowMTSync state. It is invoked
// when SetRowMT(false) flips the option so future encodes do not pay the
// wavefront overhead nor keep the primitive arrays resident.
func (p *vp9TileWorkerPool) releaseRowMTSync() {
	if p == nil {
		return
	}
	for i := range p.rowMTSyncs {
		p.rowMTSyncs[i].release()
	}
	p.rowMTSyncs = p.rowMTSyncs[:0]
}

// ensureRowWorkers arms one vp9RowWorkerPool per tile column with the row
// worker count clamped by vp9RowMTThreadCount(rowMTThreads, sbRows). It is
// called from the per-frame encode path immediately after ensureRowMTSync
// when RowMT is enabled. Pools are reused across frames; if the desired
// worker count changes (resize) the existing pools are torn down and
// replaced so the worker goroutine count stays in sync.
func (p *vp9TileWorkerPool) ensureRowWorkers(rowMTThreads, sbRows int) {
	if p == nil || p.workerCount <= 0 || sbRows <= 0 {
		return
	}
	rowThreads := vp9RowMTThreadCount(rowMTThreads, sbRows)
	// Single-worker case collapses to the serial path. Tear down any
	// existing pools so we do not keep goroutines parked for a layout
	// that no longer wants them.
	if rowThreads <= 1 {
		p.releaseRowWorkers()
		p.rowMTThreadCount = 1
		return
	}
	if p.rowMTThreadCount == rowThreads && len(p.rowWorkerPools) == p.workerCount {
		// Steady-state: re-arm per-row scratch but reuse the goroutines.
		for i := range p.rowWorkerPools {
			if pool := p.rowWorkerPools[i]; pool != nil {
				pool.reset(&p.workers[i], 0)
			}
		}
		return
	}
	// Worker count changed: shut down stale pools and build fresh ones.
	for i := range p.rowWorkerPools {
		if pool := p.rowWorkerPools[i]; pool != nil {
			pool.shutdownPool()
			p.rowWorkerPools[i] = nil
		}
	}
	if cap(p.rowWorkerPools) < p.workerCount {
		p.rowWorkerPools = make([]*vp9RowWorkerPool, p.workerCount)
	} else {
		p.rowWorkerPools = p.rowWorkerPools[:p.workerCount]
		for i := range p.rowWorkerPools {
			p.rowWorkerPools[i] = nil
		}
	}
	for i := 0; i < p.workerCount; i++ {
		p.rowWorkerPools[i] = newVP9RowWorkerPool(rowThreads)
		if p.rowWorkerPools[i] != nil {
			p.rowWorkerPools[i].reset(&p.workers[i], 0)
		}
	}
	p.rowMTThreadCount = rowThreads
}

// releaseRowWorkers tears down the per-tile-column row worker pools. It is
// invoked when SetRowMT(false) flips the option off so steady-state
// encoders that disabled row-MT do not keep helper goroutines parked.
func (p *vp9TileWorkerPool) releaseRowWorkers() {
	if p == nil {
		return
	}
	for i := range p.rowWorkerPools {
		if pool := p.rowWorkerPools[i]; pool != nil {
			pool.shutdownPool()
			p.rowWorkerPools[i] = nil
		}
	}
	p.rowWorkerPools = p.rowWorkerPools[:0]
	p.rowMTThreadCount = 0
}

type vp9TileWorkerJobKind uint8

const (
	vp9TileWorkerJobEncode vp9TileWorkerJobKind = iota
	vp9TileWorkerJobCount
)

func (e *VP9Encoder) initVP9TileWorkerPool() {
	if e == nil || e.opts.Threads <= 1 || e.opts.NoiseSensitivity > 0 {
		return
	}
	miCols := (e.opts.Width + 7) >> 3
	tileInfo := vp9EncoderTileInfo(miCols, e.opts.Threads, e.opts.Log2TileRows)
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

func (e *VP9Encoder) ensureVP9TileWorkerPool(tileJobs int) *vp9TileWorkerPool {
	if e == nil || e.opts.Threads <= 1 || tileJobs <= 1 {
		return nil
	}
	if pool := e.vp9TilePool; pool != nil && pool.workerCount == tileJobs {
		return pool
	}
	if e.vp9TilePool != nil {
		e.vp9TilePool.shutdownPool()
	}
	pool := newVP9TileWorkerPool(tileJobs)
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

func (e *VP9Encoder) closeVP9TileWorkerPool() {
	if e == nil {
		return
	}
	if e.vp9TilePool != nil {
		e.vp9TilePool.shutdownPool()
	}
	e.vp9TilePool = nil
	e.vp9CountWorkers = nil
	e.vp9CountCounts = nil
	e.vp9CountJobs = nil
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
	// Tear down any per-tile-column row worker pools first so their
	// helper goroutines drain before the tile-level workers shut down.
	p.releaseRowWorkers()
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
		seed.interModeCostFc = inter.modeCostFc
		seed.interModeCostValid = inter.modeCostFcValid
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
	if tileCols > 0 {
		e.adoptVP9CountWorkerLeafDecisionCaches(&e.vp9CountWorkers[0])
	}
	if e.vp9ActiveSegmentMapCodingChooser() &&
		!e.mergeVP9CountWorkerMiGrid(miRows, miCols, tileCols, e.vp9CountJobs) {
		return false
	}
	return true
}

func (e *VP9Encoder) collectVP9FrameTileCountsWithPool(width, height, miRows, miCols int,
	tileInfo vp9dec.TileInfo,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi, txMode common.TxMode,
	kind vp9ModeTreeKind, seed vp9CountTileSeed, dstCounts *encoder.FrameCounts,
) bool {
	tileRows := 1 << uint(tileInfo.Log2TileRows)
	tileCols := 1 << uint(tileInfo.Log2TileCols)
	if tileRows != 1 || e.opts.NoiseSensitivity > 0 {
		return false
	}
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
	if tileCols > 0 {
		e.adoptVP9CountWorkerLeafDecisionCaches(&pool.workers[0])
	}
	if e.vp9ActiveSegmentMapCodingChooser() &&
		!e.mergeVP9CountWorkerMiGrid(miRows, miCols, tileCols, pool.countJobs) {
		return false
	}
	return true
}

func (e *VP9Encoder) mergeVP9CountWorkerMiGrid(miRows, miCols, tileCols int,
	jobs []vp9CountTileJob,
) bool {
	if e == nil || tileCols <= 0 || len(jobs) < tileCols ||
		len(e.miGrid) < miRows*miCols {
		return false
	}
	for tileCol := range tileCols {
		job := &jobs[tileCol]
		if job.worker == nil || len(job.worker.miGrid) < miRows*miCols {
			return false
		}
		start, end := job.tile.MiColStart, job.tile.MiColEnd
		if start < 0 {
			start = 0
		}
		if end > miCols {
			end = miCols
		}
		if start >= end {
			return false
		}
		for row := range miRows {
			off := row*miCols + start
			copy(e.miGrid[off:off+end-start],
				job.worker.miGrid[off:off+end-start])
		}
	}
	return true
}

func (e *VP9Encoder) adoptVP9CountWorkerLeafDecisionCaches(w *VP9Encoder) {
	if e == nil || w == nil {
		return
	}
	if n := len(w.vp9LeafInterDecisions); n > 0 {
		if cap(e.vp9LeafInterDecisions) < n {
			e.vp9LeafInterDecisions = make([]vp9LeafInterDecisionEntry, n)
		} else {
			e.vp9LeafInterDecisions = e.vp9LeafInterDecisions[:n]
		}
		copy(e.vp9LeafInterDecisions, w.vp9LeafInterDecisions)
		e.vp9LeafInterDecisionsRows = w.vp9LeafInterDecisionsRows
		e.vp9LeafInterDecisionsCols = w.vp9LeafInterDecisionsCols
		e.vp9LeafInterDecisionsVer = w.vp9LeafInterDecisionsVer
	}
	if n := len(w.vp9LeafKeyframeDecisions); n > 0 {
		if cap(e.vp9LeafKeyframeDecisions) < n {
			e.vp9LeafKeyframeDecisions = make([]vp9LeafKeyframeDecisionEntry, n)
		} else {
			e.vp9LeafKeyframeDecisions = e.vp9LeafKeyframeDecisions[:n]
		}
		copy(e.vp9LeafKeyframeDecisions, w.vp9LeafKeyframeDecisions)
		e.vp9LeafKeyframeDecisionsRows = w.vp9LeafKeyframeDecisionsRows
		e.vp9LeafKeyframeDecisionsCols = w.vp9LeafKeyframeDecisionsCols
		e.vp9LeafKeyframeDecisionsVer = w.vp9LeafKeyframeDecisionsVer
	}
}

func (e *VP9Encoder) writeVP9FrameTilesThreadedEnabled(tileRows, tileCols int) bool {
	// Tile rows depend on reconstructed pixels and above entropy contexts from
	// the previous row, so this pool only dispatches independent columns in the
	// default single-row tile layout.
	return e != nil && e.opts.Threads > 1 && e.opts.NoiseSensitivity == 0 &&
		tileRows == 1 && tileCols > 1
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
	if e.opts.RowMT {
		sbRows := (miRows + (1 << common.MiBlockSizeLog2) - 1) >> common.MiBlockSizeLog2
		pool.ensureRowMTSync(sbRows)
		pool.ensureRowWorkers(e.opts.Threads, sbRows)
	} else {
		if len(pool.rowMTSyncs) > 0 {
			pool.releaseRowMTSync()
		}
		if len(pool.rowWorkerPools) > 0 {
			pool.releaseRowWorkers()
		}
	}
	seed := vp9CountTileSeedForState(key, inter)
	for tileCol := range tileCols {
		worker := e
		if tileCol > 0 {
			worker = &pool.workers[tileCol]
			worker.prepareVP9TileEncodeWorker(e, miRows, miCols)
		}
		var sync *vp9RowMTSync
		if e.opts.RowMT && tileCol < len(pool.rowMTSyncs) {
			sync = &pool.rowMTSyncs[tileCol]
		}
		prepareVP9EncodeTileJob(&pool.encodeJobs[tileCol], worker,
			pool.outputs[tileCol], miRows, miCols,
			vp9EncoderTileBounds(0, tileCol, miRows, miCols, tileInfo),
			partitionProbs, seg, baseMi, txMode, kind, seed, sync)
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
	leafDecisions := w.vp9LeafInterDecisions
	keyframeDecisions := w.vp9LeafKeyframeDecisions
	partitionReconScratch := w.partitionReconScratch
	interPredictScratch := w.interPredictScratch
	interPredictor := w.interPredictor
	reconYFull := w.reconYFull
	reconUFull := w.reconUFull
	reconVFull := w.reconVFull
	varPartGrid := w.varPartGrid
	varPartSBComputed := w.varPartSBComputed
	varPartSBUseMvPart := w.varPartSBUseMvPart
	varPartSBMvPart := w.varPartSBMvPart
	varPartSBPredLast := w.varPartSBPredLast
	varPartSBPredValid := w.varPartSBPredValid
	varPartSBVarLow := w.varPartSBVarLow
	varPartSBContentState := w.varPartSBContentState
	varPartSBContentStateValid := w.varPartSBContentStateValid
	varPartSBZeroTempSADSource := w.varPartSBZeroTempSADSource
	subpelRefBordered := w.subpelRefBordered
	intProSrcBordered := w.intProSrcBordered
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
	// Each worker owns its own leaf-decision cache so concurrent
	// tile-column writers don't race on the shared slice; the worker's
	// pre-existing slice is preserved and ensureVP9LeafInterDecisionCache
	// below re-sizes it to the active miRows*miCols extent.
	w.vp9LeafInterDecisions = leafDecisions
	w.vp9LeafKeyframeDecisions = keyframeDecisions
	w.partitionReconScratch = partitionReconScratch
	w.interPredictScratch = interPredictScratch
	w.interPredictor = interPredictor
	w.reconYFull = reconYFull
	w.reconUFull = reconUFull
	w.reconVFull = reconVFull
	w.varPartGrid = varPartGrid
	w.varPartSBComputed = varPartSBComputed
	w.varPartSBUseMvPart = varPartSBUseMvPart
	w.varPartSBMvPart = varPartSBMvPart
	w.varPartSBPredLast = varPartSBPredLast
	w.varPartSBPredValid = varPartSBPredValid
	w.varPartSBVarLow = varPartSBVarLow
	w.varPartSBContentState = varPartSBContentState
	w.varPartSBContentStateValid = varPartSBContentStateValid
	w.varPartSBZeroTempSADSource = varPartSBZeroTempSADSource
	w.subpelRefBordered = subpelRefBordered
	w.subpelRefBorderedValid = false
	w.intProSrcBordered = intProSrcBordered
	w.intProSrcBorderedValid = false
	w.vp9CountWorkers = nil
	w.vp9CountCounts = nil
	w.vp9CountJobs = nil
	w.vp9TilePool = nil
	w.vp9RowMTSync = nil
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
	leafDecisions := w.vp9LeafInterDecisions
	leafDecisionRows := w.vp9LeafInterDecisionsRows
	leafDecisionCols := w.vp9LeafInterDecisionsCols
	leafDecisionVer := w.vp9LeafInterDecisionsVer
	keyframeDecisions := w.vp9LeafKeyframeDecisions
	keyframeDecisionRows := w.vp9LeafKeyframeDecisionsRows
	keyframeDecisionCols := w.vp9LeafKeyframeDecisionsCols
	keyframeDecisionVer := w.vp9LeafKeyframeDecisionsVer
	partitionReconScratch := w.partitionReconScratch
	interPredictScratch := w.interPredictScratch
	interPredictor := w.interPredictor
	reconYFull := w.reconYFull
	reconUFull := w.reconUFull
	reconVFull := w.reconVFull
	varPartGrid := w.varPartGrid
	varPartSBComputed := w.varPartSBComputed
	varPartSBUseMvPart := w.varPartSBUseMvPart
	varPartSBMvPart := w.varPartSBMvPart
	varPartSBPredLast := w.varPartSBPredLast
	varPartSBPredValid := w.varPartSBPredValid
	varPartSBVarLow := w.varPartSBVarLow
	varPartSBContentState := w.varPartSBContentState
	varPartSBContentStateValid := w.varPartSBContentStateValid
	varPartSBZeroTempSADSource := w.varPartSBZeroTempSADSource
	subpelRefBordered := w.subpelRefBordered
	intProSrcBordered := w.intProSrcBordered
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
	// Worker-private leaf-decision cache; see prepareVP9CountWorker.
	w.vp9LeafInterDecisions = leafDecisions
	w.vp9LeafKeyframeDecisions = keyframeDecisions
	w.partitionReconScratch = partitionReconScratch
	w.interPredictScratch = interPredictScratch
	w.interPredictor = interPredictor
	w.reconYFull = reconYFull
	w.reconUFull = reconUFull
	w.reconVFull = reconVFull
	w.varPartGrid = varPartGrid
	w.varPartSBComputed = varPartSBComputed
	w.varPartSBUseMvPart = varPartSBUseMvPart
	w.varPartSBMvPart = varPartSBMvPart
	w.varPartSBPredLast = varPartSBPredLast
	w.varPartSBPredValid = varPartSBPredValid
	w.varPartSBVarLow = varPartSBVarLow
	w.varPartSBContentState = varPartSBContentState
	w.varPartSBContentStateValid = varPartSBContentStateValid
	w.varPartSBZeroTempSADSource = varPartSBZeroTempSADSource
	w.subpelRefBordered = subpelRefBordered
	w.subpelRefBorderedValid = false
	w.intProSrcBordered = intProSrcBordered
	w.intProSrcBorderedValid = false
	w.vp9CountWorkers = nil
	w.vp9CountCounts = nil
	w.vp9CountJobs = nil
	w.vp9TilePool = nil
	w.vp9RowMTSync = nil
	for plane := range vp9dec.MaxMbPlane {
		w.planes[plane].AboveContext = aboveCtx[plane]
		w.planes[plane].LeftContext = leftCtx[plane]
	}
	w.ensureVP9EncoderModeBuffers(miRows, miCols)
	if leafDecisionRows == miRows && leafDecisionCols == miCols &&
		leafDecisionVer == src.vp9LeafInterDecisionsVer &&
		len(leafDecisions) >= miRows*miCols {
		w.vp9LeafInterDecisions = leafDecisions[:miRows*miCols]
		w.vp9LeafInterDecisionsRows = leafDecisionRows
		w.vp9LeafInterDecisionsCols = leafDecisionCols
		w.vp9LeafInterDecisionsVer = leafDecisionVer
	}
	if keyframeDecisionRows == miRows && keyframeDecisionCols == miCols &&
		keyframeDecisionVer == src.vp9LeafKeyframeDecisionsVer &&
		len(keyframeDecisions) >= miRows*miCols {
		w.vp9LeafKeyframeDecisions = keyframeDecisions[:miRows*miCols]
		w.vp9LeafKeyframeDecisionsRows = keyframeDecisionRows
		w.vp9LeafKeyframeDecisionsCols = keyframeDecisionCols
		w.vp9LeafKeyframeDecisionsVer = keyframeDecisionVer
	}
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
			modeCostFc:      seed.interModeCostFc,
			modeCostFcValid: seed.interModeCostValid,
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
	kind vp9ModeTreeKind, seed vp9CountTileSeed, rowMTSync *vp9RowMTSync,
) {
	*job = vp9EncodeTileJob{
		partitionProbs: *partitionProbs,
		seg:            *seg,
		baseMi:         baseMi,
		tile:           tile,
		worker:         worker,
		output:         output,
		rowMTSync:      rowMTSync,
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
			modeCostFc:      seed.interModeCostFc,
			modeCostFcValid: seed.interModeCostValid,
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
	job.worker.vp9RowMTSync = job.rowMTSync
	defer func() { job.worker.vp9RowMTSync = nil }()
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
