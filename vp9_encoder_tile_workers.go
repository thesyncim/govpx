package govpx

import (
	"encoding/binary"
	"errors"
	"image"
	"sync"
	"sync/atomic"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
)

type vp9CountTileSeed struct {
	keyHeader                      vp9dec.UncompressedHeader
	keyImg                         *image.YCbCr
	interImg                       *image.YCbCr
	interSelectFc                  vp9dec.FrameContext
	interModeCostFc                vp9dec.FrameContext
	interNonrdIntraYModeCosts      [common.IntraModes]int
	interMvCostFc                  vp9dec.FrameContext
	interCompoundRefs              vp9dec.CompoundFrameRefs
	interRefSignBias               [vp9dec.MaxRefFrames]uint8
	counts                         *encoder.FrameCounts
	interRefMask                   uint8
	interReferenceMode             vp9dec.ReferenceMode
	interInterpFilter              vp9dec.InterpFilter
	interBaseQindex                int
	keyLossless                    bool
	interAllowHP                   bool
	interCompound                  bool
	interLossless                  bool
	interModeCostValid             bool
	interNonrdIntraYModeCostsValid bool
	interMvCostBuilt               bool
	interPreserveCodingState       bool
	hasKey                         bool
	hasKeyHeader                   bool
	hasInter                       bool
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
	rowWorkerPool  *vp9RowWorkerPool
	rowMTSync      *vp9RowMTSync
	// prepSrc, counts, tokenFrame and the geometry fields below let the job
	// body run the per-worker frame preparation (the multi-megabyte
	// *worker = *src state copy, counts zeroing and token-frame arming) on
	// the worker's own goroutine. The dispatcher only fills the small job
	// struct; the preparation reads the shared encoder state, which is
	// immutable while count jobs are in flight, so the copies parallelize
	// across tile columns instead of serializing on the dispatcher.
	prepSrc      *VP9Encoder
	counts       *encoder.FrameCounts
	tokenFrame   *encoder.TokenFrameBuffer
	width        int
	height       int
	tileCol      int
	collectToken bool
	miRows       int
	miCols       int
	txMode       common.TxMode
	kind         vp9ModeTreeKind
	hasKey       bool
	hasInter     bool
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
	// prepSrc is consumed by the vp9TileWorkerJobEncodePrep epoch: each
	// helper copies the shared encoder state into its private worker on its
	// own goroutine while the dispatcher is quiescent, so the per-frame
	// multi-megabyte worker preparation parallelizes instead of running
	// serially on the dispatcher. Tile column 0 encodes on the shared
	// encoder itself and needs no preparation.
	prepSrc       *VP9Encoder
	output        []byte
	rowMTSync     *vp9RowMTSync
	replayTokens  bool
	replayFrame   *encoder.TokenFrameBuffer
	replayTileRow int
	replayTileCol int
	miRows        int
	miCols        int
	size          int
	txMode        common.TxMode
	kind          vp9ModeTreeKind
	err           error
	hasKey        bool
	hasInter      bool
}

type vp9TileWorkerPool struct {
	vp9TileWorkerPhaseStatsOptions

	workers     []VP9Encoder
	countJobs   []vp9CountTileJob
	countCounts []encoder.FrameCounts
	countTokens []encoder.TokenFrameBuffer
	encodeJobs  []vp9EncodeTileJob
	outputs     [][]byte
	outputSize  int

	// start / done mirror libvpx's VPxWorker launch/sync handshake
	// (vpx_util/vpx_thread.c thread_loop + change_state). Each helper
	// blocks on its start channel exactly like a pthread_cond_wait in
	// idling mode, runs one job per received token, and posts one token
	// to done — the sync side (waitHelperWorkers) collects exactly one
	// token per helper, matching the cond-signal handshake libvpx uses
	// for sync(). No spinning: libvpx workers sleep between jobs.
	start    []chan struct{}
	done     chan struct{}
	shutdown chan struct{}
	wg       sync.WaitGroup

	// lfJobs / lfSync back the vp9TileWorkerJobLoopFilter epoch: the same
	// worker set that encodes the tiles also runs the frame loop filter as
	// row-interleaved workers, mirroring libvpx dispatching
	// vp9_loop_filter_frame_mt over cpi->workers with cpi->lf_row_sync.
	lfJobs []vp9EncodeLfJob
	lfSync vp9LfSync

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

	jobKind     atomic.Uint32
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
	p.rowMTSyncs = buffers.EnsureLen(p.rowMTSyncs, p.workerCount)
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
func (p *vp9TileWorkerPool) ensureRowWorkers(rowMTThreads, tileCols, sbRows int) {
	if p == nil || p.workerCount <= 0 || sbRows <= 0 {
		return
	}
	rowThreads := vp9RowMTThreadsPerTile(rowMTThreads, tileCols, sbRows)
	// Single-worker case collapses to the serial path. Tear down any
	// existing pools so we do not keep goroutines parked for a layout
	// that no longer wants them.
	if rowThreads <= 1 {
		p.releaseRowWorkers()
		p.rowMTThreadCount = 1
		return
	}
	if p.rowMTThreadCount == rowThreads && len(p.rowWorkerPools) == p.workerCount {
		return
	}
	// Worker count changed: shut down stale pools and build fresh ones.
	for i := range p.rowWorkerPools {
		if pool := p.rowWorkerPools[i]; pool != nil {
			pool.shutdownPool()
			p.rowWorkerPools[i] = nil
		}
	}
	p.rowWorkerPools = buffers.EnsureLenZeroed(p.rowWorkerPools, p.workerCount)
	for i := 0; i < p.workerCount; i++ {
		p.rowWorkerPools[i] = newVP9RowWorkerPool(rowThreads)
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
	// vp9TileWorkerJobEncodePrep runs prepareVP9TileEncodeWorker on each
	// helper's goroutine ahead of the encode epoch. The write pass encodes
	// tile column 0 directly on the shared encoder, so the helpers must
	// finish reading it before the encode epoch starts mutating it — the
	// dedicated epoch provides that barrier.
	vp9TileWorkerJobEncodePrep
	// vp9TileWorkerJobLoopFilter runs one row-interleaved share of the
	// frame loop filter per worker (libvpx vp9_loop_filter_frame_mt).
	vp9TileWorkerJobLoopFilter
)

func (e *VP9Encoder) initVP9TileWorkerPool() {
	if e == nil || e.vp9TileWorkerThreadHint() <= 1 {
		return
	}
	miCols := (e.opts.Width + 7) >> 3
	tileInfo := vp9EncoderTileInfoForTargetLevel(miCols, e.opts.Width,
		e.opts.Height, e.vp9TileWorkerThreadHint(), e.opts.Log2TileRows,
		e.opts.TargetLevel)
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
	if e == nil || e.vp9EffectiveThreadHint() <= 1 || tileJobs <= 1 {
		return nil
	}
	if pool := e.vp9TilePool; pool != nil && pool.workerCount == tileJobs {
		if vp9PhaseStatsEnabled {
			pool.setVP9TileWorkerPhaseStats(e.vp9PhaseStats())
		}
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
	if vp9PhaseStatsEnabled {
		pool.setVP9TileWorkerPhaseStats(e.vp9PhaseStats())
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

func (e *VP9Encoder) retireVP9TileWorkerPoolForCurrentConfig() {
	if e == nil || e.vp9TilePool == nil {
		return
	}
	threadHint := e.vp9TileWorkerThreadHint()
	if threadHint <= 1 {
		e.closeVP9TileWorkerPool()
		return
	}
	miCols := (e.opts.Width + 7) >> 3
	tileInfo := vp9EncoderTileInfoForTargetLevel(miCols, e.opts.Width,
		e.opts.Height, threadHint, e.opts.Log2TileRows, e.opts.TargetLevel)
	if tileInfo.Log2TileRows != 0 {
		e.closeVP9TileWorkerPool()
		return
	}
	tileCols := 1 << uint(tileInfo.Log2TileCols)
	if tileCols <= 1 || e.vp9TilePool.workerCount != tileCols {
		e.closeVP9TileWorkerPool()
	}
}

func newVP9TileWorkerPool(workers int) *vp9TileWorkerPool {
	if workers <= 1 {
		return nil
	}
	pool := &vp9TileWorkerPool{
		workers:     make([]VP9Encoder, workers),
		countJobs:   make([]vp9CountTileJob, workers),
		countCounts: make([]encoder.FrameCounts, workers),
		countTokens: make([]encoder.TokenFrameBuffer, workers),
		encodeJobs:  make([]vp9EncodeTileJob, workers),
		outputs:     make([][]byte, workers),
		start:       make([]chan struct{}, workers),
		done:        make(chan struct{}, workers-1),
		shutdown:    make(chan struct{}),
		workerCount: workers,
	}
	for i := 1; i < workers; i++ {
		start := make(chan struct{}, 1)
		pool.start[i] = start
		pool.wg.Add(1)
		go pool.workerLoop(i, start)
	}
	for i := range pool.workers {
		pool.workers[i].initVP9NmvCostCache()
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

// workerLoop mirrors libvpx's thread_loop (vpx_util/vpx_thread.c): the
// helper sleeps until launched, runs exactly one job per launch token, and
// signals completion back to the sync side. Blocking on the channel is the
// Go analogue of pthread_cond_wait; spinning here previously burned entire
// cores in runtime.Gosched -> wakep -> pthread_cond_signal storms during
// the serial sections between tile passes.
func (p *vp9TileWorkerPool) workerLoop(workerIndex int, start <-chan struct{}) {
	defer p.wg.Done()
	for {
		select {
		case _, ok := <-start:
			if !ok {
				return
			}
		case <-p.shutdown:
			return
		}
		p.runHelperWorkerJob(workerIndex)
		p.done <- struct{}{}
	}
}

func (p *vp9TileWorkerPool) runHelperWorkerJob(workerIndex int) {
	if p == nil || workerIndex <= 0 || workerIndex >= p.workerCount {
		return
	}
	switch vp9TileWorkerJobKind(p.jobKind.Load()) {
	case vp9TileWorkerJobCount:
		runVP9CountTileJobNoWG(&p.countJobs[workerIndex])
	case vp9TileWorkerJobEncodePrep:
		runVP9EncodeTilePrepJob(&p.encodeJobs[workerIndex])
	case vp9TileWorkerJobLoopFilter:
		if workerIndex < len(p.lfJobs) {
			runVP9EncodeLfJob(&p.lfJobs[workerIndex])
		}
	default:
		runVP9EncodeTileJob(&p.encodeJobs[workerIndex])
	}
}

// runVP9EncodeTilePrepJob performs the per-frame worker state preparation for
// one write-pass helper. It only reads the shared encoder (prepSrc) and
// writes the helper's private worker, so all helpers can prepare
// concurrently while the dispatcher waits on the prep epoch.
func runVP9EncodeTilePrepJob(job *vp9EncodeTileJob) {
	if job == nil || job.worker == nil || job.prepSrc == nil {
		return
	}
	job.worker.prepareVP9TileEncodeWorker(job.prepSrc, job.miRows, job.miCols)
}

func (w *VP9Encoder) prepareVP9WorkerSubpelRefBordered(
	private [common.RefFrames]common.YV12BorderBuffer, src *VP9Encoder,
) {
	if w == nil {
		return
	}
	for slot := range w.subpelRefBordered {
		if src != nil && slot < len(src.subpelRefBorderedValid) &&
			src.subpelRefBorderedValid[slot] &&
			len(src.subpelRefBordered[slot].Pixels) > 0 {
			w.subpelRefBordered[slot] = src.subpelRefBordered[slot]
			w.subpelRefBorderedGeneration[slot] = src.subpelRefBorderedGeneration[slot]
			w.subpelRefBorderedValid[slot] = true
			w.subpelRefBorderedShared[slot] = true
			continue
		}
		w.subpelRefBordered[slot] = private[slot]
		w.subpelRefBorderedGeneration[slot] = 0
		w.subpelRefBorderedValid[slot] = false
		w.subpelRefBorderedShared[slot] = false
	}
}

// startHelperWorkers mirrors libvpx's vpx_worker launch (vpx_thread.c
// change_state to VPX_WORKER_STATUS_WORKING + cond signal): publish the job
// kind, then post one launch token per helper. Every token is consumed by
// exactly one job, so the buffered(1) channels are always empty here and the
// sends never block.
func (p *vp9TileWorkerPool) startHelperWorkers(kind vp9TileWorkerJobKind) {
	if p == nil || p.workerCount <= 1 {
		return
	}
	if vp9PhaseStatsEnabled && p.vp9TileWorkerPhaseStatsActive() {
		p.vp9PhaseStartTileWorkerEpoch(kind)
		p.vp9PhaseAddTileWorkerWakeSignals(int64(p.workerCount - 1))
	}
	p.jobKind.Store(uint32(kind))
	for workerIndex := 1; workerIndex < p.workerCount; workerIndex++ {
		p.start[workerIndex] <- struct{}{}
	}
}

// waitHelperWorkers mirrors libvpx's vpx_worker sync (vpx_thread.c
// change_state waiting for VPX_WORKER_STATUS_OK): collect exactly one
// completion token per launched helper. Helpers run one job per launch
// token, so the accounting is exact and no spinning is needed.
func (p *vp9TileWorkerPool) waitHelperWorkers() {
	if p == nil || p.workerCount <= 1 {
		return
	}
	for workerIndex := 1; workerIndex < p.workerCount; workerIndex++ {
		<-p.done
	}
}

func (p *vp9TileWorkerPool) shutdownPool() {
	if p == nil {
		return
	}
	// Tear down any per-tile-column row worker pools first so their
	// helper goroutines drain before the tile-level workers shut down.
	p.releaseRowWorkers()
	for i := range p.countTokens {
		p.countTokens[i].Release()
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
		seed.interModeCostFc = inter.modeCostFc
		seed.interModeCostValid = inter.modeCostFcValid
		seed.interNonrdIntraYModeCosts = inter.nonrdIntraYModeCosts
		seed.interNonrdIntraYModeCostsValid = inter.nonrdIntraYModeCostsValid
		seed.interMvCostFc = inter.mvCostFc
		seed.interMvCostBuilt = inter.mvCostFcBuilt
		seed.interCompoundRefs = inter.compoundRefs
		seed.interRefSignBias = inter.refSignBias
		seed.interRefMask = inter.refMask
		seed.interReferenceMode = inter.referenceMode
		seed.interInterpFilter = inter.interpFilter
		seed.interBaseQindex = inter.baseQindex
		seed.interAllowHP = inter.allowHP
		seed.interCompound = inter.compoundAllowed
		seed.interLossless = inter.lossless
		seed.interPreserveCodingState = inter.preserveCodingState
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
	if e == nil || e.vp9EffectiveThreadHint() <= 1 || tileRows != 1 || tileCols <= 1 {
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
	e.vp9TokenCollect = vp9TokenCollectState{}
	e.vp9TokenReplay = vp9TokenReplayState{}
	e.vp9TokenFrame.Reset()
	e.ensureVP9CountWorkers(tileCols)
	// The frame/count-attempt entry point has already prepared and 128-filled
	// the shared reconstruction buffers. Clear only the shared mode-info grid
	// before the workers launch; every count job aliases the reconstruction
	// buffers and writes only its own tile columns.
	for i := range e.miGrid {
		e.miGrid[i] = vp9dec.NeighborMi{}
	}
	if seed.hasInter {
		e.prepareVP9SharedSubpelRefBordered(seed.interRefMask)
	}

	var wg sync.WaitGroup
	wg.Add(tileCols)
	for tileCol := range tileCols {
		worker := &e.vp9CountWorkers[tileCol]
		counts := &e.vp9CountCounts[tileCol]
		job := &e.vp9CountJobs[tileCol]
		prepareVP9CountTileJob(job, worker, counts, miRows, miCols,
			vp9EncoderTileBounds(0, tileCol, miRows, miCols, tileInfo),
			partitionProbs, seg, baseMi, txMode, kind, seed)
		job.prepSrc = e
		job.counts = counts
		job.width = width
		job.height = height
		job.tileCol = tileCol
		go runVP9CountTileJob(job, &wg)
	}
	wg.Wait()

	for i := range tileCols {
		addVP9FrameCounts(dstCounts, &e.vp9CountCounts[i])
		addVP9FilterDiff(&e.vp9FilterDiff, &e.vp9CountWorkers[i].vp9FilterDiff)
	}
	if tileCols > 0 {
		e.adoptVP9CountWorkerLeafDecisionCaches(&e.vp9CountWorkers[0])
	}
	if !e.mergeVP9CountWorkerCodingState(width, height, miRows, miCols,
		tileCols, e.vp9CountJobs) {
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
	if tileRows != 1 {
		return false
	}
	pool := e.ensureVP9TileWorkerPool(tileCols)
	if pool == nil || pool.workerCount != tileCols || dstCounts == nil {
		return false
	}
	e.vp9CountWorkers = pool.workers
	e.vp9CountCounts = pool.countCounts
	e.vp9CountJobs = pool.countJobs
	collectTokens := e.beginVP9ThreadedCountTokenCollection(pool, miRows, miCols,
		tileRows, tileCols, kind)
	if e.opts.RowMT {
		sbRows := (miRows + common.MiBlockSize - 1) >> common.MiBlockSizeLog2
		pool.ensureRowMTSync(sbRows)
		pool.ensureRowWorkers(e.vp9EffectiveThreadHint(), tileCols, sbRows)
	} else {
		if len(pool.rowMTSyncs) > 0 {
			pool.releaseRowMTSync()
		}
		if len(pool.rowWorkerPools) > 0 {
			pool.releaseRowWorkers()
		}
	}
	// The frame/count-attempt entry point has already prepared and 128-filled
	// the shared reconstruction buffers. Clear only the shared mode-info grid
	// before the workers launch; every count job aliases the reconstruction
	// buffers and writes only its own tile columns, mirroring libvpx's shared
	// cm->frame_to_show / cm->mi.
	for i := range e.miGrid {
		e.miGrid[i] = vp9dec.NeighborMi{}
	}
	if seed.hasInter {
		e.prepareVP9SharedSubpelRefBordered(seed.interRefMask)
	}
	for tileCol := range tileCols {
		worker := &pool.workers[tileCol]
		counts := &pool.countCounts[tileCol]
		job := &pool.countJobs[tileCol]
		tile := vp9EncoderTileBounds(0, tileCol, miRows, miCols, tileInfo)
		prepareVP9CountTileJob(job, worker, counts, miRows, miCols,
			tile, partitionProbs, seg, baseMi, txMode, kind, seed)
		job.prepSrc = e
		job.counts = counts
		job.width = width
		job.height = height
		job.tileCol = tileCol
		if collectTokens {
			job.collectToken = true
			job.tokenFrame = &pool.countTokens[tileCol]
		}
		if e.opts.RowMT && tileCol < len(pool.rowWorkerPools) &&
			tileCol < len(pool.rowMTSyncs) {
			job.rowWorkerPool = pool.rowWorkerPools[tileCol]
			job.rowMTSync = &pool.rowMTSyncs[tileCol]
		}
	}
	pool.startHelperWorkers(vp9TileWorkerJobCount)
	runVP9CountTileJobNoWG(&pool.countJobs[0])
	pool.waitHelperWorkers()
	if collectTokens && !e.finishVP9ThreadedCountTokenCollection(pool, miRows, tileCols) {
		e.vp9TokenFrame.Reset()
	}
	for i := range tileCols {
		addVP9FrameCounts(dstCounts, &pool.countCounts[i])
		addVP9FilterDiff(&e.vp9FilterDiff, &pool.workers[i].vp9FilterDiff)
	}
	if tileCols > 0 {
		e.adoptVP9CountWorkerLeafDecisionCaches(&pool.workers[0])
	}
	if !e.mergeVP9CountWorkerCodingState(width, height, miRows, miCols,
		tileCols, pool.countJobs) {
		return false
	}
	return true
}

func (e *VP9Encoder) mergeVP9CountWorkerCodingState(width, height, miRows, miCols, tileCols int,
	jobs []vp9CountTileJob,
) bool {
	// The mode-info grid needs no merge: count workers write their tile
	// columns directly into the dispatcher's shared grid.
	if e == nil || len(e.miGrid) < miRows*miCols {
		return false
	}
	if !e.mergeVP9CountWorkerVarPartState(miRows, miCols, tileCols, jobs) {
		return false
	}
	// Reconstruction needs no merge: every count worker writes its tile
	// columns directly into the dispatcher's recon buffers (shared by
	// prepareVP9CountWorker), matching libvpx's shared cm->frame_to_show.
	for tileCol := range tileCols {
		if jobs[tileCol].worker == nil {
			return false
		}
	}
	return true
}

func (e *VP9Encoder) mergeVP9CountWorkerVarPartState(miRows, miCols, tileCols int,
	jobs []vp9CountTileJob,
) bool {
	if e == nil || tileCols <= 0 || len(jobs) < tileCols {
		return false
	}
	miGridLen := miRows * miCols
	sbCols := (miCols + 7) >> 3
	sbRows := (miRows + 7) >> 3
	sbCount := sbRows * sbCols
	merged := false
	for tileCol := range tileCols {
		job := &jobs[tileCol]
		w := job.worker
		if w == nil || !w.varPartFrameValid {
			continue
		}
		if len(e.varPartGrid) < miGridLen || len(w.varPartGrid) < miGridLen ||
			len(e.varPartSBComputed) < sbCount ||
			len(w.varPartSBComputed) < sbCount {
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
			copy(e.varPartGrid[off:off+end-start],
				w.varPartGrid[off:off+end-start])
		}
		sbStart := start >> 3
		sbEnd := min((end+7)>>3, sbCols)
		if sbStart >= sbEnd {
			return false
		}
		for sbRow := range sbRows {
			off := sbRow*sbCols + sbStart
			n := sbEnd - sbStart
			var mergedSBs int64
			if vp9PhaseStatsEnabled {
				for _, computed := range w.varPartSBComputed[off : off+n] {
					if computed {
						mergedSBs++
					}
				}
			}
			copy(e.varPartSBComputed[off:off+n], w.varPartSBComputed[off:off+n])
			if vp9PhaseStatsEnabled {
				e.vp9PhaseAddVarPartMergedSBs(mergedSBs)
			}
			if len(e.varPartSBUseMvPart) >= off+n && len(w.varPartSBUseMvPart) >= off+n {
				copy(e.varPartSBUseMvPart[off:off+n], w.varPartSBUseMvPart[off:off+n])
			}
			if len(e.varPartSBMvPart) >= off+n && len(w.varPartSBMvPart) >= off+n {
				copy(e.varPartSBMvPart[off:off+n], w.varPartSBMvPart[off:off+n])
			}
			if len(e.varPartSBPredValid) >= off+n && len(w.varPartSBPredValid) >= off+n {
				copy(e.varPartSBPredValid[off:off+n], w.varPartSBPredValid[off:off+n])
			}
			if len(e.varPartSBPredLast) >= off+n && len(w.varPartSBPredLast) >= off+n {
				copy(e.varPartSBPredLast[off:off+n], w.varPartSBPredLast[off:off+n])
			}
			if len(e.varPartSBVarLow) >= off+n && len(w.varPartSBVarLow) >= off+n {
				copy(e.varPartSBVarLow[off:off+n], w.varPartSBVarLow[off:off+n])
			}
			if len(e.varPartSBCopiedPartition) >= off+n &&
				len(w.varPartSBCopiedPartition) >= off+n {
				copy(e.varPartSBCopiedPartition[off:off+n],
					w.varPartSBCopiedPartition[off:off+n])
			}
			if len(e.varPartSBSegmentID) >= off+n && len(w.varPartSBSegmentID) >= off+n {
				copy(e.varPartSBSegmentID[off:off+n], w.varPartSBSegmentID[off:off+n])
			}
			if len(e.varPartSBContentState) >= off+n &&
				len(w.varPartSBContentState) >= off+n {
				copy(e.varPartSBContentState[off:off+n],
					w.varPartSBContentState[off:off+n])
			}
			if len(e.varPartSBContentStateValid) >= off+n &&
				len(w.varPartSBContentStateValid) >= off+n {
				copy(e.varPartSBContentStateValid[off:off+n],
					w.varPartSBContentStateValid[off:off+n])
			}
			if len(e.varPartSBZeroTempSADSource) >= off+n &&
				len(w.varPartSBZeroTempSADSource) >= off+n {
				copy(e.varPartSBZeroTempSADSource[off:off+n],
					w.varPartSBZeroTempSADSource[off:off+n])
			}
			if len(e.varPartSBColorSensitivity) >= off+n &&
				len(w.varPartSBColorSensitivity) >= off+n {
				copy(e.varPartSBColorSensitivity[off:off+n],
					w.varPartSBColorSensitivity[off:off+n])
			}
			if len(e.varPartSBLastHighContent) >= off+n &&
				len(w.varPartSBLastHighContent) >= off+n {
				copy(e.varPartSBLastHighContent[off:off+n],
					w.varPartSBLastHighContent[off:off+n])
			}
			if len(e.varPartSBLastHighContentValid) >= off+n &&
				len(w.varPartSBLastHighContentValid) >= off+n {
				copy(e.varPartSBLastHighContentValid[off:off+n],
					w.varPartSBLastHighContentValid[off:off+n])
			}
		}
		merged = true
	}
	if merged {
		e.varPartFrameValid = true
	}
	return true
}

func vp9CopyReplaySlice[S ~[]E, E any](dst S, src S, limit int) S {
	if limit <= 0 || len(src) == 0 {
		return dst[:0]
	}
	if len(src) < limit {
		limit = len(src)
	}
	dst = buffers.EnsureLen(dst, limit)
	copy(dst, src[:limit])
	return dst
}

func (e *VP9Encoder) adoptVP9CountWorkerLeafDecisionCaches(w *VP9Encoder) {
	if e == nil || w == nil {
		return
	}
	// Worker 0 is quiescent at this barrier and tile-column 0 is written by
	// the dispatcher. Exchange cache ownership instead of copying several
	// megabytes of decisions back to the dispatcher every frame. The old
	// dispatcher buffers become worker 0's private buffers for the next count
	// epoch, so the pair naturally ping-pongs without allocation or aliasing.
	if len(w.vp9LeafInterDecisions) > 0 {
		e.vp9LeafInterDecisions, w.vp9LeafInterDecisions =
			w.vp9LeafInterDecisions, e.vp9LeafInterDecisions
		e.vp9LeafInterDecisionsRows, w.vp9LeafInterDecisionsRows =
			w.vp9LeafInterDecisionsRows, e.vp9LeafInterDecisionsRows
		e.vp9LeafInterDecisionsCols, w.vp9LeafInterDecisionsCols =
			w.vp9LeafInterDecisionsCols, e.vp9LeafInterDecisionsCols
		e.vp9LeafInterDecisionsVer, w.vp9LeafInterDecisionsVer =
			w.vp9LeafInterDecisionsVer, e.vp9LeafInterDecisionsVer
	}
	if len(w.vp9InterPartitionDecisions) > 0 {
		e.vp9InterPartitionDecisions, w.vp9InterPartitionDecisions =
			w.vp9InterPartitionDecisions, e.vp9InterPartitionDecisions
		e.vp9InterPartitionDecisionsRows, w.vp9InterPartitionDecisionsRows =
			w.vp9InterPartitionDecisionsRows, e.vp9InterPartitionDecisionsRows
		e.vp9InterPartitionDecisionsCols, w.vp9InterPartitionDecisionsCols =
			w.vp9InterPartitionDecisionsCols, e.vp9InterPartitionDecisionsCols
		e.vp9InterPartitionDecisionsVer, w.vp9InterPartitionDecisionsVer =
			w.vp9InterPartitionDecisionsVer, e.vp9InterPartitionDecisionsVer
	}
	if len(w.vp9LeafKeyframeDecisions) > 0 {
		e.vp9LeafKeyframeDecisions, w.vp9LeafKeyframeDecisions =
			w.vp9LeafKeyframeDecisions, e.vp9LeafKeyframeDecisions
		e.vp9LeafKeyframeDecisionsRows, w.vp9LeafKeyframeDecisionsRows =
			w.vp9LeafKeyframeDecisionsRows, e.vp9LeafKeyframeDecisionsRows
		e.vp9LeafKeyframeDecisionsCols, w.vp9LeafKeyframeDecisionsCols =
			w.vp9LeafKeyframeDecisionsCols, e.vp9LeafKeyframeDecisionsCols
		e.vp9LeafKeyframeDecisionsVer, w.vp9LeafKeyframeDecisionsVer =
			w.vp9LeafKeyframeDecisionsVer, e.vp9LeafKeyframeDecisionsVer
	}
}

func (e *VP9Encoder) writeVP9FrameTilesThreadedEnabled(tileRows, tileCols int) bool {
	// Tile rows depend on reconstructed pixels and above entropy contexts from
	// the previous row, so this pool only dispatches independent columns in the
	// default single-row tile layout.
	return e != nil && e.vp9TileWorkerThreadHint() > 1 &&
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
	if !e.opts.RowMT {
		if len(pool.rowMTSyncs) > 0 {
			pool.releaseRowMTSync()
		}
		if len(pool.rowWorkerPools) > 0 {
			pool.releaseRowWorkers()
		}
	}
	seed := vp9CountTileSeedForState(key, inter)
	if seed.hasInter {
		e.prepareVP9SharedSubpelRefBordered(seed.interRefMask)
	}
	for tileCol := range tileCols {
		worker := e
		if tileCol > 0 {
			worker = &pool.workers[tileCol]
		}
		prepareVP9EncodeTileJob(&pool.encodeJobs[tileCol], worker,
			pool.outputs[tileCol], miRows, miCols,
			vp9EncoderTileBounds(0, tileCol, miRows, miCols, tileInfo),
			partitionProbs, seg, baseMi, txMode, kind, seed, nil)
		if tileCol > 0 {
			pool.encodeJobs[tileCol].prepSrc = e
		}
		if e.vp9TokenReplay.active {
			pool.encodeJobs[tileCol].replayTokens = true
			if e.vp9ThreadedTokenReplayReady && tileCol < len(pool.countTokens) {
				pool.encodeJobs[tileCol].replayFrame = &pool.countTokens[tileCol]
			}
			pool.encodeJobs[tileCol].replayTileRow = 0
			pool.encodeJobs[tileCol].replayTileCol = tileCol
		}
	}

	// Helper workers copy the shared encoder state on their own goroutines.
	// The dispatcher stays quiescent during this epoch: tile column 0 runs
	// on the shared encoder itself, and mutating it before the helpers
	// finish reading it would race with their state copies.
	pool.startHelperWorkers(vp9TileWorkerJobEncodePrep)
	pool.waitHelperWorkers()

	pool.startHelperWorkers(vp9TileWorkerJobEncode)
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
	e.vp9CountWorkers = buffers.EnsureLen(e.vp9CountWorkers, workers)
	e.vp9CountCounts = buffers.EnsureLen(e.vp9CountCounts, workers)
	e.vp9CountJobs = buffers.EnsureLen(e.vp9CountJobs, workers)
}

func (w *VP9Encoder) prepareVP9CountWorker(src *VP9Encoder, width, height, miRows, miCols int) {
	aboveSegCtx := w.aboveSegCtx
	leftSegCtx := w.leftSegCtx
	leafDecisions := w.vp9LeafInterDecisions
	interPartitionDecisions := w.vp9InterPartitionDecisions
	if len(interPartitionDecisions) > 0 && len(src.vp9InterPartitionDecisions) > 0 &&
		&interPartitionDecisions[0] == &src.vp9InterPartitionDecisions[0] {
		interPartitionDecisions = nil
	}
	keyframeDecisions := w.vp9LeafKeyframeDecisions
	partitionReconScratch := w.partitionReconScratch
	interPredictScratch := w.interPredictScratch
	interPredictor := w.interPredictor
	varPartGrid := w.varPartGrid
	varPartSBComputed := w.varPartSBComputed
	varPartSBUseMvPart := w.varPartSBUseMvPart
	varPartSBMvPart := w.varPartSBMvPart
	varPartSBPredLast := w.varPartSBPredLast
	varPartSBPredValid := w.varPartSBPredValid
	varPartSBVarLow := w.varPartSBVarLow
	varPartSBCopiedPartition := w.varPartSBCopiedPartition
	varPartSBSegmentID := w.varPartSBSegmentID
	varPartSBContentState := w.varPartSBContentState
	varPartSBContentStateValid := w.varPartSBContentStateValid
	varPartSBZeroTempSADSource := w.varPartSBZeroTempSADSource
	varPartSBColorSensitivity := w.varPartSBColorSensitivity
	varPartSBLastHighContent := w.varPartSBLastHighContent
	varPartSBLastHighContentValid := w.varPartSBLastHighContentValid
	mlPartitionCtx := w.mlPartitionCtx
	mlPartitionPaddedLast := w.mlPartitionPaddedLast
	mlPartitionPaddedSrc := w.mlPartitionPaddedSrc
	subpelRefBordered := w.subpelRefBordered
	intProSrcBordered := w.intProSrcBordered
	nmvCostCache := w.vp9NmvCostCache
	blockCoeffScratch := w.blockCoeffScratch
	rdThresh := w.rdThresh
	var aboveCtx [vp9dec.MaxMbPlane][]uint8
	var leftCtx [vp9dec.MaxMbPlane][]uint8
	for plane := range vp9dec.MaxMbPlane {
		aboveCtx[plane] = w.planes[plane].AboveContext
		leftCtx[plane] = w.planes[plane].LeftContext
	}

	*w = *src
	// Worker clones never touch the parent's reference buffer pool.
	w.dropVP9EncoderFramePool()
	w.aboveSegCtx = aboveSegCtx
	w.leftSegCtx = leftSegCtx
	// Each worker owns its own leaf-decision cache so concurrent
	// tile-column writers don't race on the shared slice; the worker's
	// pre-existing slice is preserved and ensureVP9LeafInterDecisionCache
	// below re-sizes it to the active miRows*miCols extent.
	w.vp9LeafInterDecisions = leafDecisions
	w.vp9InterPartitionDecisions = interPartitionDecisions
	w.vp9LeafKeyframeDecisions = keyframeDecisions
	w.partitionReconScratch = partitionReconScratch
	w.interPredictScratch = interPredictScratch
	w.interPredictor = interPredictor
	// Reconstruction buffers are shared with the dispatching encoder, the
	// same way libvpx tile workers all write into cm->frame_to_show: the
	// struct copy above leaves w.recon* aliased to src's buffers and each
	// tile column only writes its own column range. The dispatcher fills
	// the shared buffers once per frame before launching the jobs.
	w.varPartGrid = varPartGrid
	w.varPartSBComputed = varPartSBComputed
	w.varPartSBUseMvPart = varPartSBUseMvPart
	w.varPartSBMvPart = varPartSBMvPart
	w.varPartSBPredLast = varPartSBPredLast
	w.varPartSBPredValid = varPartSBPredValid
	w.varPartSBVarLow = varPartSBVarLow
	w.varPartSBCopiedPartition = varPartSBCopiedPartition
	w.varPartSBSegmentID = varPartSBSegmentID
	w.varPartSBContentState = varPartSBContentState
	w.varPartSBContentStateValid = varPartSBContentStateValid
	w.varPartSBZeroTempSADSource = varPartSBZeroTempSADSource
	w.varPartSBColorSensitivity = varPartSBColorSensitivity
	w.varPartSBLastHighContent = varPartSBLastHighContent
	w.varPartSBLastHighContentValid = varPartSBLastHighContentValid
	// Worker-private ML-partition context cache: ensureVP9EncoderModeBuffers
	// resets every per-SB slot at frame preparation, and with the
	// preparation running on the worker goroutines a shared backing array
	// would let one tile column's reset race another's in-flight encode.
	w.mlPartitionCtx = mlPartitionCtx
	w.mlPartitionPaddedLast = mlPartitionPaddedLast
	w.mlPartitionPaddedSrc = mlPartitionPaddedSrc
	// Reference pixels live in refcounted pool buffers that are immutable
	// while a tile epoch runs, so the parent's border-padded LAST mirror
	// (rebuilt at the previous frame's reference refresh) is immutable
	// too: adopt it read-only through the struct copy instead of
	// rebuilding a private ~1MB padded plane per worker per epoch.
	// ensureLastBordered detaches to a private buffer before any
	// cold-path rebuild on a worker.
	w.lastBorderedShared = true
	w.prepareVP9WorkerSubpelRefBordered(subpelRefBordered, src)
	w.intProSrcBordered = intProSrcBordered
	w.intProSrcBorderedValid = false
	w.vp9NmvCostCache = nmvCostCache
	rdThresh.AdoptFrameThresholdsFrom(&src.rdThresh)
	w.rdThresh = rdThresh
	// Workers keep a private coefficient staging scratch: the shared
	// encoder's pointer must not leak in via the struct copy or the
	// tile columns would race on the same arrays.
	if blockCoeffScratch == nil {
		blockCoeffScratch = &vp9EncoderBlockCoeffScratch{}
	}
	w.blockCoeffScratch = blockCoeffScratch
	w.vp9CountWorkers = nil
	w.vp9CountCounts = nil
	w.vp9CountJobs = nil
	w.vp9TilePool = nil
	w.vp9RowMTSync = nil
	for plane := range vp9dec.MaxMbPlane {
		w.planes[plane].AboveContext = aboveCtx[plane]
		w.planes[plane].LeftContext = leftCtx[plane]
	}
	w.ensureVP9EncoderModeBuffersImpl(miRows, miCols, false)
	// The shared reconstruction buffers and the shared mode-info grid were
	// already prepared (filled / cleared) once by the dispatcher; the
	// struct copy above leaves w.recon* and w.miGrid aliased to src's
	// buffers, matching libvpx's shared cm->frame_to_show and cm->mi.
	// Every tile column writes only its own column range, so only the
	// worker-private syntax contexts are reset here.
	w.resetVP9EncoderSyntaxContexts()
}

func (w *VP9Encoder) prepareVP9TileEncodeWorker(src *VP9Encoder, miRows, miCols int) {
	aboveSegCtx := w.aboveSegCtx
	leftSegCtx := w.leftSegCtx
	leafDecisions := w.vp9LeafInterDecisions
	leafDecisionRows := w.vp9LeafInterDecisionsRows
	leafDecisionCols := w.vp9LeafInterDecisionsCols
	leafDecisionVer := w.vp9LeafInterDecisionsVer
	interPartitionDecisions := w.vp9InterPartitionDecisions
	interPartitionRows := w.vp9InterPartitionDecisionsRows
	interPartitionCols := w.vp9InterPartitionDecisionsCols
	interPartitionVer := w.vp9InterPartitionDecisionsVer
	keyframeDecisions := w.vp9LeafKeyframeDecisions
	keyframeDecisionRows := w.vp9LeafKeyframeDecisionsRows
	keyframeDecisionCols := w.vp9LeafKeyframeDecisionsCols
	keyframeDecisionVer := w.vp9LeafKeyframeDecisionsVer
	partitionReconScratch := w.partitionReconScratch
	interPredictScratch := w.interPredictScratch
	interPredictor := w.interPredictor
	varPartGrid := w.varPartGrid
	varPartSBComputed := w.varPartSBComputed
	varPartSBUseMvPart := w.varPartSBUseMvPart
	varPartSBMvPart := w.varPartSBMvPart
	varPartSBPredLast := w.varPartSBPredLast
	varPartSBPredValid := w.varPartSBPredValid
	varPartSBVarLow := w.varPartSBVarLow
	varPartSBCopiedPartition := w.varPartSBCopiedPartition
	varPartSBSegmentID := w.varPartSBSegmentID
	varPartSBContentState := w.varPartSBContentState
	varPartSBContentStateValid := w.varPartSBContentStateValid
	varPartSBZeroTempSADSource := w.varPartSBZeroTempSADSource
	varPartSBColorSensitivity := w.varPartSBColorSensitivity
	varPartSBLastHighContent := w.varPartSBLastHighContent
	varPartSBLastHighContentValid := w.varPartSBLastHighContentValid
	mlPartitionCtx := w.mlPartitionCtx
	mlPartitionPaddedLast := w.mlPartitionPaddedLast
	mlPartitionPaddedSrc := w.mlPartitionPaddedSrc
	subpelRefBordered := w.subpelRefBordered
	intProSrcBordered := w.intProSrcBordered
	nmvCostCache := w.vp9NmvCostCache
	blockCoeffScratch := w.blockCoeffScratch
	rdThresh := w.rdThresh
	var aboveCtx [vp9dec.MaxMbPlane][]uint8
	var leftCtx [vp9dec.MaxMbPlane][]uint8
	for plane := range vp9dec.MaxMbPlane {
		aboveCtx[plane] = w.planes[plane].AboveContext
		leftCtx[plane] = w.planes[plane].LeftContext
	}

	*w = *src
	// Tile workers never rotate or refresh reference buffers; detach the
	// clone from the parent's pool so any stray call is inert.
	w.dropVP9EncoderFramePool()
	w.aboveSegCtx = aboveSegCtx
	w.leftSegCtx = leftSegCtx
	// Worker-private leaf-decision cache; see prepareVP9CountWorker.
	w.vp9LeafInterDecisions = leafDecisions
	w.vp9LeafKeyframeDecisions = keyframeDecisions
	w.partitionReconScratch = partitionReconScratch
	w.interPredictScratch = interPredictScratch
	w.interPredictor = interPredictor
	w.varPartGrid = varPartGrid
	w.varPartSBComputed = varPartSBComputed
	w.varPartSBUseMvPart = varPartSBUseMvPart
	w.varPartSBMvPart = varPartSBMvPart
	w.varPartSBPredLast = varPartSBPredLast
	w.varPartSBPredValid = varPartSBPredValid
	w.varPartSBVarLow = varPartSBVarLow
	w.varPartSBCopiedPartition = varPartSBCopiedPartition
	w.varPartSBSegmentID = varPartSBSegmentID
	w.varPartSBContentState = varPartSBContentState
	w.varPartSBContentStateValid = varPartSBContentStateValid
	w.varPartSBZeroTempSADSource = varPartSBZeroTempSADSource
	w.varPartSBColorSensitivity = varPartSBColorSensitivity
	w.varPartSBLastHighContent = varPartSBLastHighContent
	w.varPartSBLastHighContentValid = varPartSBLastHighContentValid
	// Worker-private ML-partition context cache: ensureVP9EncoderModeBuffers
	// resets every per-SB slot at frame preparation, and with the
	// preparation running on the worker goroutines a shared backing array
	// would let one tile column's reset race another's in-flight encode.
	w.mlPartitionCtx = mlPartitionCtx
	w.mlPartitionPaddedLast = mlPartitionPaddedLast
	w.mlPartitionPaddedSrc = mlPartitionPaddedSrc
	// Reference pixels live in refcounted pool buffers that are immutable
	// while a tile epoch runs, so the parent's border-padded LAST mirror
	// (rebuilt at the previous frame's reference refresh) is immutable
	// too: adopt it read-only through the struct copy instead of
	// rebuilding a private ~1MB padded plane per worker per epoch.
	// ensureLastBordered detaches to a private buffer before any
	// cold-path rebuild on a worker.
	w.lastBorderedShared = true
	w.prepareVP9WorkerSubpelRefBordered(subpelRefBordered, src)
	w.intProSrcBordered = intProSrcBordered
	w.intProSrcBorderedValid = false
	w.vp9NmvCostCache = nmvCostCache
	rdThresh.AdoptFrameThresholdsFrom(&src.rdThresh)
	w.rdThresh = rdThresh
	// Workers keep a private coefficient staging scratch: the shared
	// encoder's pointer must not leak in via the struct copy or the
	// tile columns would race on the same arrays.
	if blockCoeffScratch == nil {
		blockCoeffScratch = &vp9EncoderBlockCoeffScratch{}
	}
	w.blockCoeffScratch = blockCoeffScratch
	w.vp9CountWorkers = nil
	w.vp9CountCounts = nil
	w.vp9CountJobs = nil
	w.vp9TilePool = nil
	w.vp9RowMTSync = nil
	for plane := range vp9dec.MaxMbPlane {
		w.planes[plane].AboveContext = aboveCtx[plane]
		w.planes[plane].LeftContext = leftCtx[plane]
	}
	// Non-clearing variant: w.miGrid aliases the shared grid (assigned
	// below from src), which may carry preserved count-pass coding state
	// for the token-replay path; clearing it here would destroy it and
	// race the dispatcher's tile column 0.
	w.ensureVP9EncoderModeBuffersImpl(miRows, miCols, false)
	if leafDecisionRows == miRows && leafDecisionCols == miCols &&
		leafDecisionVer == src.vp9LeafInterDecisionsVer &&
		len(leafDecisions) >= miRows*miCols {
		w.vp9LeafInterDecisions = leafDecisions[:miRows*miCols]
		w.vp9LeafInterDecisionsRows = leafDecisionRows
		w.vp9LeafInterDecisionsCols = leafDecisionCols
		w.vp9LeafInterDecisionsVer = leafDecisionVer
	}
	interPartitionLen := miRows * miCols * int(common.BlockSizes)
	if interPartitionRows == miRows && interPartitionCols == miCols &&
		interPartitionVer == src.vp9InterPartitionDecisionsVer &&
		len(interPartitionDecisions) >= interPartitionLen {
		w.vp9InterPartitionDecisions = interPartitionDecisions[:interPartitionLen]
		w.vp9InterPartitionDecisionsRows = interPartitionRows
		w.vp9InterPartitionDecisionsCols = interPartitionCols
		w.vp9InterPartitionDecisionsVer = interPartitionVer
	} else {
		w.vp9InterPartitionDecisions = src.vp9InterPartitionDecisions
		w.vp9InterPartitionDecisionsRows = src.vp9InterPartitionDecisionsRows
		w.vp9InterPartitionDecisionsCols = src.vp9InterPartitionDecisionsCols
		w.vp9InterPartitionDecisionsVer = src.vp9InterPartitionDecisionsVer
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
	if src.varPartFrameValid {
		w.copyVP9VarPartStateFrom(src, miRows, miCols)
	}
}

func (w *VP9Encoder) copyVP9VarPartStateFrom(src *VP9Encoder, miRows, miCols int) bool {
	if w == nil || src == nil || !src.varPartFrameValid ||
		miRows <= 0 || miCols <= 0 {
		return false
	}
	miGridLen := miRows * miCols
	sbCount := ((miRows + 7) >> 3) * ((miCols + 7) >> 3)
	if miGridLen <= 0 || sbCount <= 0 ||
		len(src.varPartGrid) < miGridLen ||
		len(src.varPartSBComputed) < sbCount {
		return false
	}
	w.varPartGrid = buffers.EnsureLen(w.varPartGrid, miGridLen)
	copy(w.varPartGrid, src.varPartGrid[:miGridLen])
	w.varPartSBComputed = buffers.EnsureLen(w.varPartSBComputed, sbCount)
	copy(w.varPartSBComputed, src.varPartSBComputed[:sbCount])
	w.varPartSBUseMvPart = vp9CopyReplaySlice(w.varPartSBUseMvPart,
		src.varPartSBUseMvPart, sbCount)
	w.varPartSBMvPart = vp9CopyReplaySlice(w.varPartSBMvPart,
		src.varPartSBMvPart, sbCount)
	w.varPartSBPredLast = vp9CopyReplaySlice(w.varPartSBPredLast,
		src.varPartSBPredLast, sbCount)
	w.varPartSBPredValid = vp9CopyReplaySlice(w.varPartSBPredValid,
		src.varPartSBPredValid, sbCount)
	w.varPartSBVarLow = vp9CopyReplaySlice(w.varPartSBVarLow,
		src.varPartSBVarLow, sbCount)
	w.varPartSBCopiedPartition = vp9CopyReplaySlice(w.varPartSBCopiedPartition,
		src.varPartSBCopiedPartition, sbCount)
	w.varPartSBSegmentID = vp9CopyReplaySlice(w.varPartSBSegmentID,
		src.varPartSBSegmentID, sbCount)
	w.varPartSBContentState = vp9CopyReplaySlice(w.varPartSBContentState,
		src.varPartSBContentState, sbCount)
	w.varPartSBContentStateValid = vp9CopyReplaySlice(w.varPartSBContentStateValid,
		src.varPartSBContentStateValid, sbCount)
	w.varPartSBZeroTempSADSource = vp9CopyReplaySlice(w.varPartSBZeroTempSADSource,
		src.varPartSBZeroTempSADSource, sbCount)
	w.varPartSBColorSensitivity = vp9CopyReplaySlice(w.varPartSBColorSensitivity,
		src.varPartSBColorSensitivity, sbCount)
	w.varPartSBLastHighContent = vp9CopyReplaySlice(w.varPartSBLastHighContent,
		src.varPartSBLastHighContent, sbCount)
	w.varPartSBLastHighContentValid = vp9CopyReplaySlice(w.varPartSBLastHighContentValid,
		src.varPartSBLastHighContentValid, sbCount)
	w.varPartFrameValid = true
	return true
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
			img:                       seed.interImg,
			dq:                        &worker.dqScratch,
			ref:                       &worker.refFrames[0],
			refMask:                   seed.interRefMask,
			allowHP:                   seed.interAllowHP,
			selectFc:                  seed.interSelectFc,
			modeCostFc:                seed.interModeCostFc,
			modeCostFcValid:           seed.interModeCostValid,
			nonrdIntraYModeCosts:      seed.interNonrdIntraYModeCosts,
			nonrdIntraYModeCostsValid: seed.interNonrdIntraYModeCostsValid,
			mvCostFc:                  seed.interMvCostFc,
			mvCostFcBuilt:             seed.interMvCostBuilt,
			referenceMode:             seed.interReferenceMode,
			compoundAllowed:           seed.interCompound,
			refSignBias:               seed.interRefSignBias,
			compoundRefs:              seed.interCompoundRefs,
			interpFilter:              seed.interInterpFilter,
			lossless:                  seed.interLossless,
			baseQindex:                seed.interBaseQindex,
			counts:                    counts,
			preserveCodingState:       seed.interPreserveCodingState,
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
			img:                       seed.interImg,
			dq:                        &worker.dqScratch,
			ref:                       &worker.refFrames[0],
			refMask:                   seed.interRefMask,
			allowHP:                   seed.interAllowHP,
			selectFc:                  seed.interSelectFc,
			modeCostFc:                seed.interModeCostFc,
			modeCostFcValid:           seed.interModeCostValid,
			nonrdIntraYModeCosts:      seed.interNonrdIntraYModeCosts,
			nonrdIntraYModeCostsValid: seed.interNonrdIntraYModeCostsValid,
			mvCostFc:                  seed.interMvCostFc,
			mvCostFcBuilt:             seed.interMvCostBuilt,
			referenceMode:             seed.interReferenceMode,
			compoundAllowed:           seed.interCompound,
			refSignBias:               seed.interRefSignBias,
			compoundRefs:              seed.interCompoundRefs,
			interpFilter:              seed.interInterpFilter,
			lossless:                  seed.interLossless,
			baseQindex:                seed.interBaseQindex,
		}
	}
}

func runVP9CountTileJob(job *vp9CountTileJob, wg *sync.WaitGroup) {
	defer wg.Done()
	runVP9CountTileJobNoWG(job)
}

func runVP9CountTileJobNoWG(job *vp9CountTileJob) {
	if vp9PhaseStatsEnabled && job != nil && job.worker.vp9PhaseStatsActive() {
		job.worker.vp9PhaseIncTileWorkerJob(vp9TileWorkerJobCount)
	}
	// Per-worker frame preparation runs here, on the job's goroutine, so
	// the heavy encoder-state copy and counts zeroing parallelize across
	// tile columns. prepSrc is only read; it stays immutable while count
	// jobs are in flight.
	if job.prepSrc != nil {
		if job.counts != nil {
			*job.counts = encoder.FrameCounts{}
		}
		job.worker.prepareVP9CountWorker(job.prepSrc, job.width, job.height,
			job.miRows, job.miCols)
		if job.collectToken && job.tokenFrame != nil {
			job.tokenFrame.EnsureForTile(job.miRows,
				job.tile.MiColEnd-job.tile.MiColStart, 0, job.tileCol)
			job.tokenFrame.Reset()
			job.worker.vp9TokenFrame = *job.tokenFrame
			job.worker.vp9TokenCollect = vp9TokenCollectState{
				active:  true,
				tileRow: 0,
				tileCol: job.tileCol,
			}
		}
	}
	var countKey *vp9KeyframeEncodeState
	if job.hasKey {
		countKey = &job.key
	}
	var countInter *vp9InterEncodeState
	if job.hasInter {
		countInter = &job.inter
	}
	if runVP9CountTileRows(job, countInter) {
		return
	}
	var bw bitstream.Writer
	bw.StartDiscard()
	job.worker.writeVP9FrameTile(&bw, job.miRows, job.miCols, job.tile,
		&job.partitionProbs, &job.seg, job.baseMi, job.txMode, job.kind,
		countKey, countInter)
	_, _ = bw.Stop()
}

func runVP9CountTileRows(job *vp9CountTileJob,
	countInter *vp9InterEncodeState,
) bool {
	if job == nil || job.worker == nil || job.rowWorkerPool == nil ||
		job.rowMTSync == nil || !job.collectToken || job.tokenFrame == nil ||
		job.kind != vp9ModeTreeInterSource || countInter == nil ||
		countInter.counts == nil || job.worker.svc.UseSvc ||
		job.worker.vp9ActiveSegmentMapCodingChooser() {
		return false
	}
	if job.worker.denoiser.active() &&
		!job.worker.canDispatchVP9DenoiserCountRows(countInter, job.kind, &job.seg) {
		return false
	}
	if job.worker.sf.PartitionSearchType != VarBasedPartition ||
		job.worker.sf.AdaptiveRdThreshRowMt == 0 {
		return false
	}
	sbRows := (job.tile.MiRowEnd - job.tile.MiRowStart + common.MiBlockSize - 1) >>
		common.MiBlockSizeLog2
	if sbRows <= 1 {
		return false
	}
	tileMiCols := job.tile.MiColEnd - job.tile.MiColStart
	pool := job.rowWorkerPool
	pool.resetCountWorkers(job.worker, job.width, job.height, job.miRows,
		job.miCols, tileMiCols, job.tileCol, sbRows)
	job.rowMTSync.reset(sbRows)
	if vp9PhaseStatsEnabled && job.worker.vp9PhaseStatsActive() {
		job.worker.vp9PhaseIncFrameTile(true)
		job.worker.vp9PhaseAddRowWorkerCountEpoch(sbRows)
	}
	pool.queue = buffers.EnsureLen(pool.queue, sbRows)
	for row := range sbRows {
		pool.queue[row] = row
	}
	err := pool.dispatch(pool.queue, vp9RowWorkerJob{
		countJob:   job,
		countInter: countInter,
	})
	if err != nil {
		job.worker.vp9TokenCollect.err = err
		return true
	}
	for row := range sbRows {
		if !job.worker.vp9TokenFrame.AppendRowTokenList(0, job.tileCol, row,
			&pool.rowTokens[row]) {
			job.worker.vp9TokenCollect.err = encoder.ErrTokenBufferFull
			return true
		}
	}
	for i := range pool.workers {
		state := &pool.workers[i]
		addVP9FrameCounts(job.counts, &state.counts)
		addVP9FilterDiff(&job.worker.vp9FilterDiff, &state.worker.vp9FilterDiff)
	}
	job.worker.varPartFrameValid = true
	return true
}

func runVP9CountTileRow(job *vp9CountTileJob, countInter *vp9InterEncodeState,
	row int, state *vp9RowEncoderState,
) error {
	pool := job.rowWorkerPool
	w := &state.worker
	rowTokens := &pool.rowTokens[row]
	w.vp9TokenFrame = *rowTokens
	w.vp9TokenCollect = vp9TokenCollectState{
		active:        true,
		tileRow:       0,
		tileCol:       job.tileCol,
		listSBRowBase: row,
	}
	w.vp9RowMTSync = job.rowMTSync
	inter := *countInter
	inter.dq = &w.dqScratch
	inter.ref = &w.refFrames[0]
	inter.counts = &state.counts
	miRow := job.tile.MiRowStart + row*common.MiBlockSize
	var bw bitstream.Writer
	bw.StartDiscard()
	w.writeVP9ModesTileRow(&bw, job.miRows, job.miCols, miRow,
		job.tile, &job.partitionProbs, &job.seg, job.baseMi, job.txMode,
		job.kind, nil, &inter, job.rowMTSync, false, 0)
	_, _ = bw.Stop()
	*rowTokens = w.vp9TokenFrame
	return w.vp9TokenCollect.err
}

func runVP9EncodeTileJob(job *vp9EncodeTileJob) {
	if vp9PhaseStatsEnabled && job != nil && job.worker.vp9PhaseStatsActive() {
		job.worker.vp9PhaseIncTileWorkerJob(vp9TileWorkerJobEncode)
	}
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
	if job.replayTokens {
		job.worker.vp9TokenReplay = vp9TokenReplayState{
			active:  true,
			tileRow: job.replayTileRow,
			tileCol: job.replayTileCol,
			frame:   job.replayFrame,
		}
	}
	defer func() { job.worker.vp9RowMTSync = nil }()
	var bw bitstream.Writer
	bw.Start(job.output)
	job.worker.writeVP9FrameTile(&bw, job.miRows, job.miCols, job.tile,
		&job.partitionProbs, &job.seg, job.baseMi, job.txMode, job.kind,
		key, inter)
	if job.replayTokens && job.worker.vp9TokenReplay.err != nil {
		job.err = job.worker.vp9TokenReplay.err
		return
	}
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
