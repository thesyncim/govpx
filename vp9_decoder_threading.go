package govpx

import (
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
)

type vp9LoopFilterPlane uint8

const (
	vp9LoopFilterPlaneY vp9LoopFilterPlane = iota
	vp9LoopFilterPlaneU
	vp9LoopFilterPlaneV
)

type vp9DecoderLoopFilterPool struct {
	helperCount int8
	start       []chan struct{}
	done        []chan struct{}
	exited      []chan struct{}
	jobs        []vp9EncodeLfJob
	lfSync      vp9LfSync

	lastActiveWorkers uint8
}

func newVP9DecoderLoopFilterPool(threads int) *vp9DecoderLoopFilterPool {
	helpers := min(threads-1, 63)
	if helpers <= 0 {
		return nil
	}
	p := &vp9DecoderLoopFilterPool{
		helperCount: int8(helpers),
		start:       make([]chan struct{}, helpers),
		done:        make([]chan struct{}, helpers),
		exited:      make([]chan struct{}, helpers),
		jobs:        make([]vp9EncodeLfJob, helpers+1),
	}
	for i := range helpers {
		p.start[i] = make(chan struct{})
		p.done[i] = make(chan struct{})
		p.exited[i] = make(chan struct{})
		go p.workerLoop(i)
	}
	return p
}

func (p *vp9DecoderLoopFilterPool) workerLoop(worker int) {
	defer close(p.exited[worker])
	for range p.start[worker] {
		runVP9EncodeLfJob(&p.jobs[worker+1])
		p.done[worker] <- struct{}{}
	}
}

func (p *vp9DecoderLoopFilterPool) shutdown() {
	if p == nil {
		return
	}
	for i := 0; i < int(p.helperCount); i++ {
		close(p.start[i])
	}
	for i := 0; i < int(p.helperCount); i++ {
		<-p.exited[i]
	}
	for i := range p.jobs {
		p.jobs[i] = vp9EncodeLfJob{}
	}
	p.helperCount = 0
	p.lastActiveWorkers = 0
}

func (d *VP9Decoder) applyVP9LoopFilterThreaded(miRows, miCols, width int) bool {
	p := d.vp9LoopFilterPool
	if p == nil || p.helperCount <= 0 {
		return d.applyVP9LoopFilterSerial(miRows, miCols)
	}
	if !d.prepareVP9LoopFilterMasks(miRows, miCols, 0, miRows) {
		return false
	}

	sbRows := (miRows + common.MiBlockSize - 1) >> common.MiBlockSizeLog2
	workers := min(int(p.helperCount)+1, sbRows)
	p.lastActiveWorkers = uint8(workers)
	if workers <= 1 {
		return d.applyVP9LoopFilterSerialCached(miRows, miCols)
	}
	p.lfSync.reset(sbRows, width)
	for i := range p.jobs {
		start := i
		if i >= workers {
			start = sbRows
		}
		p.jobs[i] = vp9EncodeLfJob{
			d:      *d,
			lfSync: &p.lfSync,
			miRows: miRows,
			miCols: miCols,
			start:  start,
			step:   workers,
			ok:     true,
		}
	}
	helpers := workers - 1
	for worker := range helpers {
		p.start[worker] <- struct{}{}
	}
	runVP9EncodeLfJob(&p.jobs[0])
	for worker := range helpers {
		<-p.done[worker]
	}
	ok := true
	for i := range workers {
		ok = ok && p.jobs[i].ok
	}
	return ok
}

type vp9DecoderTileJobKind uint8

const (
	vp9DecoderTileJobIntra vp9DecoderTileJobKind = iota
	vp9DecoderTileJobInter
)

type vp9DecoderTileDesc struct {
	data []byte
	tile vp9dec.TileBounds
}

const (
	// Mirrors vp9/decoder/vp9_decoder.h row-mt frame storage.
	vp9DecoderRowMTEobsPerSBLog2     = 8
	vp9DecoderRowMTDQCoeffsPerSBLog2 = 12
	vp9DecoderRowMTPartitionsPerSB   = 85
	vp9DecoderRowMTDQCoeffAlign      = 32
)

type vp9DecoderRowMTJobType uint8

const (
	vp9DecoderRowMTJobParse vp9DecoderRowMTJobType = iota
	vp9DecoderRowMTJobRecon
	vp9DecoderRowMTJobLPF
)

type vp9DecoderRowMTJob struct {
	rowNum  int
	tileCol int
	jobType vp9DecoderRowMTJobType
}

// vp9DecoderRowMTJobQueue mirrors libvpx's JobQueueRowMt: a fixed-capacity,
// non-wrapping FIFO that workers block on until a job is queued or the row-mt
// tile pass terminates.
type vp9DecoderRowMTJobQueue struct {
	mu         sync.Mutex
	cond       *sync.Cond
	jobs       []vp9DecoderRowMTJob
	read       int
	terminated bool
}

func (q *vp9DecoderRowMTJobQueue) ensureCapacity(jobCap int) {
	if q == nil || jobCap <= 0 {
		return
	}
	if q.cond == nil {
		q.cond = sync.NewCond(&q.mu)
	}
	if cap(q.jobs) < jobCap {
		q.jobs = make([]vp9DecoderRowMTJob, 0, jobCap)
	}
}

func (q *vp9DecoderRowMTJobQueue) reset() {
	if q == nil {
		return
	}
	q.mu.Lock()
	q.jobs = q.jobs[:0]
	q.read = 0
	q.terminated = false
	q.mu.Unlock()
}

func (q *vp9DecoderRowMTJobQueue) release() {
	if q == nil {
		return
	}
	q.mu.Lock()
	q.jobs = nil
	q.read = 0
	q.terminated = false
	q.mu.Unlock()
}

func (q *vp9DecoderRowMTJobQueue) terminate() {
	if q == nil {
		return
	}
	q.mu.Lock()
	q.terminated = true
	if q.cond != nil {
		q.cond.Broadcast()
	}
	q.mu.Unlock()
}

func (q *vp9DecoderRowMTJobQueue) queue(job vp9DecoderRowMTJob) bool {
	if q == nil {
		return false
	}
	q.mu.Lock()
	ok := !q.terminated && len(q.jobs) < cap(q.jobs)
	if ok {
		q.jobs = append(q.jobs, job)
		if q.cond != nil {
			q.cond.Signal()
		}
	}
	q.mu.Unlock()
	return ok
}

func (q *vp9DecoderRowMTJobQueue) dequeue(blocking bool) (vp9DecoderRowMTJob, bool) {
	if q == nil {
		return vp9DecoderRowMTJob{}, false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	for {
		if q.read < len(q.jobs) {
			job := q.jobs[q.read]
			q.read++
			return job, true
		}
		if q.terminated || !blocking {
			return vp9DecoderRowMTJob{}, false
		}
		if q.cond == nil {
			q.cond = sync.NewCond(&q.mu)
		}
		q.cond.Wait()
	}
}

func (q *vp9DecoderRowMTJobQueue) done() bool {
	if q == nil {
		return true
	}
	q.mu.Lock()
	done := q.terminated && q.read >= len(q.jobs)
	q.mu.Unlock()
	return done
}

type vp9DecoderRowMTFrameStorage struct {
	numSBs   int
	numJobs  int
	tileCols int

	eob           [vp9dec.MaxMbPlane][]int
	dqcoeffBytes  [vp9dec.MaxMbPlane][]byte
	dqcoeff       [vp9dec.MaxMbPlane][]int16
	partition     []common.PartitionType
	uvMode        []common.PredictionMode
	residueParsed []bool
	reconMap      []int8
	reconMu       []sync.Mutex
	reconCond     []*sync.Cond
	jobq          vp9DecoderRowMTJobQueue
	reader        bitstream.Reader
	lfSync        vp9LfSync
	tileStates    []vp9DecoderRowMTTileState
	tilesDone     atomic.Int32

	loopFilterActive  bool
	loopFilterApplied bool
}

type vp9DecoderRowMTTileState struct {
	decoder VP9Decoder
	reader  bitstream.Reader
	tile    vp9dec.TileBounds

	leftSegCtx  [common.MiBlockSize]int8
	leftEntropy [vp9dec.MaxMbPlane][16]uint8
}

func (s *vp9DecoderRowMTFrameStorage) reset(numSBs int) {
	if s == nil {
		return
	}
	if numSBs <= 0 {
		s.numSBs = 0
		for plane := range vp9dec.MaxMbPlane {
			s.eob[plane] = s.eob[plane][:0]
			s.dqcoeffBytes[plane] = s.dqcoeffBytes[plane][:0]
			s.dqcoeff[plane] = s.dqcoeff[plane][:0]
		}
		s.partition = s.partition[:0]
		s.uvMode = s.uvMode[:0]
		s.residueParsed = s.residueParsed[:0]
		s.reconMap = s.reconMap[:0]
		s.numJobs = 0
		s.tileCols = 0
		s.tileStates = s.tileStates[:0]
		s.tilesDone.Store(0)
		s.loopFilterActive = false
		s.loopFilterApplied = false
		return
	}

	s.numSBs = numSBs
	eobLen := numSBs << vp9DecoderRowMTEobsPerSBLog2
	dqcoeffLen := numSBs << vp9DecoderRowMTDQCoeffsPerSBLog2
	for plane := range vp9dec.MaxMbPlane {
		s.eob[plane] = buffers.EnsureLenZeroed(s.eob[plane], eobLen)
		s.dqcoeffBytes[plane] = buffers.EnsureAlignedCapacity(
			s.dqcoeffBytes[plane], dqcoeffLen*2, vp9DecoderRowMTDQCoeffAlign)
		s.dqcoeff[plane] = unsafe.Slice(
			(*int16)(unsafe.Pointer(&s.dqcoeffBytes[plane][0])), dqcoeffLen)
		clear(s.dqcoeff[plane])
	}
	s.partition = buffers.EnsureLenZeroed(s.partition,
		numSBs*vp9DecoderRowMTPartitionsPerSB)
	s.reconMap = buffers.EnsureLenZeroed(s.reconMap, numSBs)
	s.tileCols = 0
	s.tileStates = s.tileStates[:0]
	s.tilesDone.Store(0)
	s.loopFilterActive = false
	s.loopFilterApplied = false
}

func (s *vp9DecoderRowMTFrameStorage) ensureModeStorage(miRows, miCols int) {
	if s == nil || miRows <= 0 || miCols <= 0 {
		return
	}
	s.uvMode = buffers.EnsureLenZeroed(s.uvMode, miRows*miCols)
	s.residueParsed = buffers.EnsureLenZeroed(s.residueParsed, miRows*miCols)
}

func (s *vp9DecoderRowMTFrameStorage) ensureJobQueue(tileCols, sbRows int) {
	if s == nil || tileCols <= 0 || sbRows <= 0 {
		return
	}
	numJobs := tileCols * sbRows
	s.numJobs = numJobs
	s.tileCols = tileCols
	s.tilesDone.Store(0)
	if cap(s.reconMu) < numJobs {
		s.reconMu = make([]sync.Mutex, numJobs)
		s.reconCond = make([]*sync.Cond, numJobs)
	} else {
		s.reconMu = s.reconMu[:numJobs]
		s.reconCond = s.reconCond[:numJobs]
	}
	for i := range s.reconCond {
		if s.reconCond[i] == nil {
			s.reconCond[i] = sync.NewCond(&s.reconMu[i])
		}
	}
	s.jobq.ensureCapacity(tileCols*sbRows*2 + sbRows)
	s.jobq.reset()
}

func (s *vp9DecoderRowMTFrameStorage) release() {
	if s == nil {
		return
	}
	s.numSBs = 0
	s.numJobs = 0
	s.tileCols = 0
	for plane := range vp9dec.MaxMbPlane {
		s.eob[plane] = nil
		s.dqcoeffBytes[plane] = nil
		s.dqcoeff[plane] = nil
	}
	s.partition = nil
	s.uvMode = nil
	s.residueParsed = nil
	s.reconMap = nil
	s.reconMu = nil
	s.reconCond = nil
	s.jobq.release()
	s.lfSync = vp9LfSync{}
	s.tileStates = nil
	s.tilesDone.Store(0)
	s.loopFilterActive = false
	s.loopFilterApplied = false
}

func (s *vp9DecoderRowMTFrameStorage) prepareTileStates(parent *VP9Decoder,
	descs []vp9DecoderTileDesc,
) error {
	if s == nil || parent == nil || len(descs) == 0 || len(descs) != s.tileCols {
		return ErrInvalidVP9Data
	}
	s.tileStates = buffers.EnsureLen(s.tileStates, len(descs))
	for tileCol := range descs {
		state := &s.tileStates[tileCol]
		state.decoder = *parent
		state.decoder.vp9LoopFilterPool = nil
		state.decoder.vp9TilePool = nil
		state.decoder.rowMTSync = nil
		state.decoder.leftSegCtx = state.leftSegCtx[:]
		clear(state.leftSegCtx[:])
		for plane := range vp9dec.MaxMbPlane {
			leftLen := len(parent.planes[plane].LeftContext)
			if leftLen > len(state.leftEntropy[plane]) {
				return ErrInvalidVP9Data
			}
			state.decoder.planes[plane].LeftContext =
				state.leftEntropy[plane][:leftLen]
			clear(state.leftEntropy[plane][:leftLen])
		}
		state.decoder.counts = vp9dec.FrameCounts{}
		state.tile = descs[tileCol].tile
		if err := state.reader.Init(descs[tileCol].data); err != nil {
			return ErrInvalidVP9Data
		}
	}
	return nil
}

func (s *vp9DecoderRowMTFrameStorage) tileState(tileCol int) *vp9DecoderRowMTTileState {
	if s == nil || tileCol < 0 || tileCol >= len(s.tileStates) {
		return nil
	}
	return &s.tileStates[tileCol]
}

func (s *vp9DecoderRowMTFrameStorage) eobForSB(plane, sb int) []int {
	base := sb << vp9DecoderRowMTEobsPerSBLog2
	return s.eob[plane][base : base+(1<<vp9DecoderRowMTEobsPerSBLog2)]
}

func (s *vp9DecoderRowMTFrameStorage) dqcoeffForSB(plane, sb int) []int16 {
	base := sb << vp9DecoderRowMTDQCoeffsPerSBLog2
	return s.dqcoeff[plane][base : base+(1<<vp9DecoderRowMTDQCoeffsPerSBLog2)]
}

func (s *vp9DecoderRowMTFrameStorage) partitionsForSB(sb int) []common.PartitionType {
	base := sb * vp9DecoderRowMTPartitionsPerSB
	return s.partition[base : base+vp9DecoderRowMTPartitionsPerSB]
}

func (s *vp9DecoderRowMTFrameStorage) reconMapWrite(mapIdx, syncIdx int) bool {
	if s == nil || mapIdx < 0 || mapIdx >= len(s.reconMap) ||
		syncIdx < 0 || syncIdx >= len(s.reconCond) {
		return false
	}
	mu := &s.reconMu[syncIdx]
	mu.Lock()
	s.reconMap[mapIdx] = 1
	mu.Unlock()
	s.reconCond[syncIdx].Signal()
	return true
}

func (s *vp9DecoderRowMTFrameStorage) reconMapRead(mapIdx, syncIdx int) bool {
	if s == nil || mapIdx < 0 || mapIdx >= len(s.reconMap) ||
		syncIdx < 0 || syncIdx >= len(s.reconCond) {
		return false
	}
	mu := &s.reconMu[syncIdx]
	mu.Lock()
	for s.reconMap[mapIdx] == 0 {
		s.reconCond[syncIdx].Wait()
	}
	mu.Unlock()
	return true
}

type vp9DecoderTileJob struct {
	data           []byte
	descs          []vp9DecoderTileDesc
	hdr            *vp9dec.UncompressedHeader
	comp           vp9dec.CompressedHeader
	intraMaps      vp9dec.IntraSegmentMaps
	interMaps      vp9dec.InterSegmentMaps
	tile           vp9dec.TileBounds
	miRows         int
	miCols         int
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8
	kind           vp9DecoderTileJobKind
	worker         VP9Decoder
	rowMTParent    *VP9Decoder

	leftSegCtx          [common.MiBlockSize]int8
	leftEntropy         [vp9dec.MaxMbPlane][16]uint8
	leftEntropyLen      [vp9dec.MaxMbPlane]uint8
	interPredictScratch []byte

	// rowMTSync, when non-nil, is the per-tile-column wavefront sync the
	// decoder body should call read / write against. Populated by the tile
	// worker pool when VP9D_SET_ROW_MT is enabled.
	rowMTSync *vp9RowMTSync

	rowMTStorage *vp9DecoderRowMTFrameStorage

	err         error
	unsupported bool
}

type vp9DecoderTileWorkerPool struct {
	helperCount  int8
	start        []chan struct{}
	jobs         []vp9DecoderTileJob
	mainRowMTJob vp9DecoderTileJob
	tileDescs    []vp9DecoderTileDesc
	header       vp9dec.UncompressedHeader

	shutdownCh    chan struct{}
	wg            sync.WaitGroup
	jobEpoch      atomic.Uint64
	activeHelpers atomic.Uint32
	doneEpoch     []atomic.Uint64
	parked        []atomic.Uint32

	// rowMTSyncs holds one vp9RowMTSync per tile-column slot. Allocated
	// lazily by ensureRowMTSync when VP9D_SET_ROW_MT is enabled; released
	// by releaseRowMTSync when the option toggles off. Mirrors the encoder
	// vp9TileWorkerPool layout so the decoder body can index by tile-column
	// slot.
	rowMTSyncs []vp9RowMTSync
	// rowMTArmed records whether SetRowMT(true) has been observed without
	// a matching SetRowMT(false). It is sticky until releaseRowMTSync runs.
	rowMTArmed bool
	// rowMTFrame mirrors libvpx RowMTWorkerData frame slabs. It is reset per
	// frame while VP9D_SET_ROW_MT is armed and released with the row-mt sync
	// state when the option is disabled.
	rowMTFrame vp9DecoderRowMTFrameStorage

	lastTileJobs        uint8
	lastTileJobKind     vp9DecoderTileJobKind
	lastTileSchedule    [64]int
	lastTileScheduleLen uint8
}

func newVP9DecoderTileWorkerPool(threads int) *vp9DecoderTileWorkerPool {
	helpers := min(threads-1, 63)
	if helpers <= 0 {
		return nil
	}
	p := &vp9DecoderTileWorkerPool{
		helperCount: int8(helpers),
		start:       make([]chan struct{}, helpers),
		jobs:        make([]vp9DecoderTileJob, helpers),
		shutdownCh:  make(chan struct{}),
		doneEpoch:   make([]atomic.Uint64, helpers),
		parked:      make([]atomic.Uint32, helpers),
	}
	for i := range helpers {
		p.start[i] = make(chan struct{}, 1)
		p.wg.Add(1)
		go p.workerLoop(i)
	}
	return p
}

func (p *vp9DecoderTileWorkerPool) workerLoop(worker int) {
	defer p.wg.Done()
	var epoch uint64
	idleSpins := 0
	for {
		next := p.jobEpoch.Load()
		if next != epoch {
			epoch = next
			if worker < int(p.activeHelpers.Load()) {
				p.jobs[worker].run()
			}
			p.doneEpoch[worker].Store(epoch)
			idleSpins = 0
			continue
		}
		select {
		case <-p.shutdownCh:
			return
		default:
		}
		if idleSpins >= rowWorkerIdleSpinBudget {
			if !p.parkHelperWorker(worker, epoch) {
				return
			}
			idleSpins = 0
			continue
		}
		idleSpins++
		runtimeProcYield(30)
		if idleSpins >= rowWorkerIdleSchedulerBackoff &&
			idleSpins%rowWorkerIdleSchedulerBackoff == 0 {
			runtime.Gosched()
		}
	}
}

func (p *vp9DecoderTileWorkerPool) shutdown() {
	if p == nil {
		return
	}
	close(p.shutdownCh)
	p.wg.Wait()
	for i := range p.jobs {
		p.jobs[i] = vp9DecoderTileJob{}
	}
	p.mainRowMTJob = vp9DecoderTileJob{}
	p.tileDescs = nil
	p.helperCount = 0
	p.lastTileJobs = 0
	p.rowMTSyncs = nil
	p.rowMTFrame.release()
	p.rowMTArmed = false
}

func (p *vp9DecoderTileWorkerPool) parkHelperWorker(worker int, epoch uint64) bool {
	if p == nil {
		return false
	}
	if worker >= 0 && worker < len(p.parked) {
		p.parked[worker].Store(1)
		defer p.parked[worker].Store(0)
	}
	if p.jobEpoch.Load() != epoch {
		return true
	}
	select {
	case _, ok := <-p.start[worker]:
		return ok
	case <-p.shutdownCh:
		return false
	}
}

func (p *vp9DecoderTileWorkerPool) startHelperWorkers(helpers int) uint64 {
	if p == nil || helpers <= 0 {
		return 0
	}
	if helpers > int(p.helperCount) {
		helpers = int(p.helperCount)
	}
	p.activeHelpers.Store(uint32(helpers))
	epoch := p.jobEpoch.Add(1)
	for worker := 0; worker < helpers; worker++ {
		if worker >= len(p.parked) || p.parked[worker].Load() == 0 {
			continue
		}
		select {
		case p.start[worker] <- struct{}{}:
		default:
		}
	}
	return epoch
}

func (p *vp9DecoderTileWorkerPool) waitHelperWorkers(epoch uint64, helpers int) {
	if p == nil || helpers <= 0 {
		return
	}
	if helpers > int(p.helperCount) {
		helpers = int(p.helperCount)
	}
	spins := 0
	for ; !p.helperWorkersDone(epoch, helpers); spins++ {
		runtimeProcYield(30)
		if spins >= rowWorkerIdleSchedulerBackoff &&
			spins%rowWorkerIdleSchedulerBackoff == 0 {
			runtime.Gosched()
		}
	}
}

func (p *vp9DecoderTileWorkerPool) helperWorkersDone(epoch uint64, helpers int) bool {
	if p == nil {
		return true
	}
	if helpers > len(p.doneEpoch) {
		helpers = len(p.doneEpoch)
	}
	for worker := 0; worker < helpers; worker++ {
		if p.doneEpoch[worker].Load() < epoch {
			return false
		}
	}
	return true
}

// armRowMT marks the tile worker pool as serving VP9D_SET_ROW_MT decode
// frames. The per-tile-column wavefront primitive is allocated lazily in
// ensureRowMTSync once miRows is known for a frame.
func (p *vp9DecoderTileWorkerPool) armRowMT() {
	if p == nil {
		return
	}
	p.rowMTArmed = true
}

// ensureRowMTSync arms one vp9RowMTSync per tile-column slot sized to sbRows
// when VP9D_SET_ROW_MT is active. Mirrors the encoder helper of the same
// name so the wavefront primitive layout stays in sync across encode and
// decode. tileCols is the number of tile columns in the current frame.
func (p *vp9DecoderTileWorkerPool) ensureRowMTSync(tileCols, sbRows int) {
	if p == nil || tileCols <= 0 || sbRows <= 0 {
		return
	}
	p.rowMTSyncs = buffers.EnsureLen(p.rowMTSyncs, tileCols)
	for i := range p.rowMTSyncs {
		p.rowMTSyncs[i].reset(sbRows)
	}
}

func (p *vp9DecoderTileWorkerPool) ensureRowMTFrameStorage(miRows, miCols,
	tileCols int,
) {
	if p == nil || miRows <= 0 || miCols <= 0 || tileCols <= 0 {
		return
	}
	sbRows := common.AlignToSB(miRows) >> common.MiBlockSizeLog2
	sbCols := common.AlignToSB(miCols) >> common.MiBlockSizeLog2
	p.rowMTFrame.reset(sbRows * sbCols)
	p.rowMTFrame.ensureModeStorage(miRows, miCols)
	p.rowMTFrame.ensureJobQueue(tileCols, sbRows)
}

// releaseRowMTSync drops the per-tile-column vp9RowMTSync state. It is
// invoked when SetRowMT(false) flips the option so future decodes do not
// pay the wavefront overhead nor keep the primitive arrays resident.
func (p *vp9DecoderTileWorkerPool) releaseRowMTSync() {
	if p == nil {
		return
	}
	for i := range p.rowMTSyncs {
		p.rowMTSyncs[i].release()
	}
	p.rowMTSyncs = p.rowMTSyncs[:0]
	p.rowMTFrame.release()
	p.rowMTArmed = false
}

func (d *VP9Decoder) vp9DecoderTileThreadingEnabled(
	hdr *vp9dec.UncompressedHeader, tileRows, tileCols int,
) bool {
	rowMT := d != nil && d.opts.DecoderRowMT
	return d != nil && d.vp9TilePool != nil && hdr != nil &&
		tileRows == 1 && (tileCols > 1 || rowMT) && !d.vp9TileFilterActive() &&
		!d.opts.InvertTileDecodeOrder
}

func (d *VP9Decoder) parseVP9IntraModeTilesThreaded(tileData []byte,
	hdr vp9dec.UncompressedHeader, comp vp9dec.CompressedHeader,
	miRows, miCols int,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
) error {
	descs, err := d.vp9TilePool.prepareTileDescs(tileData, hdr, miRows,
		miCols)
	if err != nil {
		return err
	}
	maps := vp9dec.IntraSegmentMaps{
		CurrentFrameSegMap: d.segMap,
		LastFrameSegMap:    d.lastSegMap,
		MiCols:             miCols,
	}
	return d.runVP9DecoderTileJobs(descs, vp9DecoderTileJobIntra, hdr, comp,
		maps, vp9dec.InterSegmentMaps{}, miRows, miCols, partitionProbs)
}

func (d *VP9Decoder) parseVP9InterModeTilesThreaded(tileData []byte,
	hdr vp9dec.UncompressedHeader, comp vp9dec.CompressedHeader,
	miRows, miCols int,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
) error {
	descs, err := d.vp9TilePool.prepareTileDescs(tileData, hdr, miRows,
		miCols)
	if err != nil {
		return err
	}
	maps := vp9dec.InterSegmentMaps{
		IntraSegmentMaps: vp9dec.IntraSegmentMaps{
			CurrentFrameSegMap: d.segMap,
			LastFrameSegMap:    d.lastSegMap,
			MiCols:             miCols,
		},
	}
	return d.runVP9DecoderTileJobs(descs, vp9DecoderTileJobInter, hdr, comp,
		vp9dec.IntraSegmentMaps{}, maps, miRows, miCols, partitionProbs)
}

func (p *vp9DecoderTileWorkerPool) prepareTileDescs(tileData []byte,
	hdr vp9dec.UncompressedHeader, miRows, miCols int,
) ([]vp9DecoderTileDesc, error) {
	descs, err := prepareVP9DecoderTileDescs(p.tileDescs, tileData, hdr,
		miRows, miCols)
	if err != nil {
		return nil, err
	}
	p.tileDescs = descs
	return descs, nil
}

func (d *VP9Decoder) runVP9DecoderTileJobs(descs []vp9DecoderTileDesc,
	kind vp9DecoderTileJobKind, hdr vp9dec.UncompressedHeader,
	comp vp9dec.CompressedHeader, intraMaps vp9dec.IntraSegmentMaps,
	interMaps vp9dec.InterSegmentMaps, miRows, miCols int,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
) error {
	p := d.vp9TilePool
	p.header = hdr
	p.lastTileJobs = 0
	p.lastTileJobKind = kind
	p.lastTileScheduleLen = 0
	if len(descs) == 0 {
		return nil
	}
	// Allocate per-tile-column wavefront sync arrays when VP9D_SET_ROW_MT
	// is enabled and we have multiple tile columns. The sync primitive is
	// armed across the parent decoder and all tile workers so the
	// tile-column body can read / write its row state without further
	// dispatch changes when per-row goroutines land.
	if d.opts.DecoderRowMT && p.rowMTArmed {
		sbRows := (miRows + common.MiBlockSize - 1) >> common.MiBlockSizeLog2
		p.ensureRowMTSync(len(descs), sbRows)
		p.ensureRowMTFrameStorage(miRows, miCols, len(descs))
	} else if len(p.rowMTSyncs) > 0 {
		// Reset stale state when the option toggled off mid-stream.
		p.rowMTSyncs = p.rowMTSyncs[:0]
		p.rowMTFrame.release()
	}
	if !d.opts.DecoderRowMT {
		return d.runVP9DecoderScheduledTileJobs(descs, kind, comp,
			intraMaps, interMaps, miRows, miCols, partitionProbs)
	}
	if len(descs) > 1 {
		return d.runVP9DecoderMultiTileRowMTJobs(descs, kind, comp,
			intraMaps, interMaps, miRows, miCols, partitionProbs)
	}
	mergeCounts := !hdr.FrameParallelDecoding
	helpersMax := int(p.helperCount)
	next := 0
	jobsRun := 0
	for next < len(descs) {
		helpers := min(len(descs)-next-1, helpersMax)
		for worker := range helpers {
			desc := descs[next+worker+1]
			p.prepareTileJob(worker, d, kind, desc, comp, intraMaps,
				interMaps, miRows, miCols, partitionProbs)
			tileSlot := next + worker + 1
			if tileSlot < len(p.rowMTSyncs) {
				p.jobs[worker].rowMTSync = &p.rowMTSyncs[tileSlot]
			} else {
				p.jobs[worker].rowMTSync = nil
			}
		}
		epoch := p.startHelperWorkers(helpers)

		// The lead tile decode runs on this goroutine. When row-MT is
		// armed, plumb the lead tile's wavefront sync through the parent
		// decoder so parseVP9*ModeTile observes the same shape as worker
		// invocations.
		if next < len(p.rowMTSyncs) {
			d.rowMTSync = &p.rowMTSyncs[next]
		} else {
			d.rowMTSync = nil
		}
		err := d.runVP9DecoderTileDesc(kind, descs[next], &p.header, comp,
			intraMaps, interMaps, miRows, miCols, partitionProbs)
		d.rowMTSync = nil
		p.waitHelperWorkers(epoch, helpers)
		for worker := range helpers {
			job := &p.jobs[worker]
			if job.unsupported {
				d.unsupportedReconstruct = true
			}
			if err == nil && job.err != nil {
				err = job.err
			}
			if mergeCounts && job.err == nil {
				vp9dec.MergeFrameCounts(&d.counts, &job.worker.counts)
			}
		}
		if err != nil {
			return err
		}
		jobsRun += helpers + 1
		next += helpers + 1
	}
	p.lastTileJobs = uint8(jobsRun)
	return nil
}

func (d *VP9Decoder) runVP9DecoderScheduledTileJobs(descs []vp9DecoderTileDesc,
	kind vp9DecoderTileJobKind, comp vp9dec.CompressedHeader,
	intraMaps vp9dec.IntraSegmentMaps, interMaps vp9dec.InterSegmentMaps,
	miRows, miCols int,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
) error {
	p := d.vp9TilePool
	workers := min(int(p.helperCount)+1, len(descs))
	if workers <= 1 {
		err := d.runVP9DecoderTileDesc(kind, descs[0], &p.header, comp,
			intraMaps, interMaps, miRows, miCols, partitionProbs)
		if err == nil {
			p.lastTileJobs = 1
			p.recordTileSchedule(descs)
		}
		return err
	}

	scheduleVP9DecoderTileDescsForThreading(descs, workers)
	p.recordTileSchedule(descs)

	mergeCounts := !p.header.FrameParallelDecoding
	base := len(descs) / workers
	remain := len(descs) % workers
	bufStart := 0
	var err error
	helpers := workers - 1
	for worker := range helpers {
		count := base + (remain+worker)/workers
		bufEnd := bufStart + count
		p.prepareTileJobRange(worker, d, kind, descs[bufStart:bufEnd], comp,
			intraMaps, interMaps, miRows, miCols, partitionProbs)
		bufStart = bufEnd
	}
	epoch := p.startHelperWorkers(helpers)

	mainCount := base + (remain+helpers)/workers
	mainEnd := bufStart + mainCount
	for _, desc := range descs[bufStart:mainEnd] {
		if runErr := d.runVP9DecoderTileDesc(kind, desc, &p.header, comp,
			intraMaps, interMaps, miRows, miCols, partitionProbs); runErr != nil && err == nil {
			err = runErr
		}
	}

	p.waitHelperWorkers(epoch, helpers)
	for worker := range helpers {
		job := &p.jobs[worker]
		if job.unsupported {
			d.unsupportedReconstruct = true
		}
		if err == nil && job.err != nil {
			err = job.err
		}
		if mergeCounts && job.err == nil {
			vp9dec.MergeFrameCounts(&d.counts, &job.worker.counts)
		}
	}
	if err != nil {
		return err
	}
	p.lastTileJobs = uint8(len(descs))
	return nil
}

func scheduleVP9DecoderTileDescsForThreading(descs []vp9DecoderTileDesc, workers int) {
	if len(descs) <= 1 || workers <= 1 {
		return
	}
	// libvpx decode_tiles_mt sorts tile buffers by compressed size descending.
	for i := 1; i < len(descs); i++ {
		desc := descs[i]
		j := i - 1
		for j >= 0 && len(descs[j].data) < len(desc.data) {
			descs[j+1] = descs[j]
			j--
		}
		descs[j+1] = desc
	}
	if workers >= len(descs) {
		largest := descs[0]
		copy(descs, descs[1:])
		descs[len(descs)-1] = largest
		return
	}
	start, end := 0, len(descs)-2
	for start < end {
		descs[start], descs[end] = descs[end], descs[start]
		start += 2
		end -= 2
	}
}

func (p *vp9DecoderTileWorkerPool) recordTileSchedule(descs []vp9DecoderTileDesc) {
	if p == nil {
		return
	}
	n := min(len(descs), len(p.lastTileSchedule))
	for i := range n {
		p.lastTileSchedule[i] = descs[i].tile.MiColStart
	}
	p.lastTileScheduleLen = uint8(n)
}

func (p *vp9DecoderTileWorkerPool) prepareTileJob(worker int,
	parent *VP9Decoder, kind vp9DecoderTileJobKind, desc vp9DecoderTileDesc,
	comp vp9dec.CompressedHeader,
	intraMaps vp9dec.IntraSegmentMaps, interMaps vp9dec.InterSegmentMaps,
	miRows, miCols int,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
) {
	job := &p.jobs[worker]
	job.descs = nil
	job.worker = *parent
	job.data = desc.data
	job.hdr = &p.header
	job.comp = comp
	job.intraMaps = intraMaps
	job.interMaps = interMaps
	job.tile = desc.tile
	job.miRows = miRows
	job.miCols = miCols
	job.partitionProbs = partitionProbs
	job.kind = kind
	job.rowMTParent = nil
	job.rowMTStorage = nil
	job.err = nil
	job.unsupported = false
	if !p.header.FrameParallelDecoding {
		job.worker.counts = vp9dec.FrameCounts{}
	}
	for plane := range vp9dec.MaxMbPlane {
		job.leftEntropyLen[plane] = uint8(len(parent.planes[plane].LeftContext))
	}
}

func (p *vp9DecoderTileWorkerPool) prepareTileJobRange(worker int,
	parent *VP9Decoder, kind vp9DecoderTileJobKind, descs []vp9DecoderTileDesc,
	comp vp9dec.CompressedHeader,
	intraMaps vp9dec.IntraSegmentMaps, interMaps vp9dec.InterSegmentMaps,
	miRows, miCols int,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
) {
	p.prepareTileJob(worker, parent, kind, vp9DecoderTileDesc{}, comp,
		intraMaps, interMaps, miRows, miCols, partitionProbs)
	p.jobs[worker].descs = descs
	p.jobs[worker].rowMTSync = nil
	p.jobs[worker].rowMTStorage = nil
}

func (j *vp9DecoderTileJob) prepareRowMTJob(
	parent *VP9Decoder, kind vp9DecoderTileJobKind, hdr *vp9dec.UncompressedHeader,
	comp vp9dec.CompressedHeader, tile vp9dec.TileBounds,
	intraMaps vp9dec.IntraSegmentMaps, interMaps vp9dec.InterSegmentMaps,
	miRows, miCols int,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	storage *vp9DecoderRowMTFrameStorage,
) {
	j.descs = nil
	j.worker = *parent
	j.rowMTParent = parent
	j.data = nil
	j.hdr = hdr
	j.comp = comp
	j.intraMaps = intraMaps
	j.interMaps = interMaps
	j.tile = tile
	j.miRows = miRows
	j.miCols = miCols
	j.partitionProbs = partitionProbs
	j.kind = kind
	j.rowMTSync = nil
	j.rowMTStorage = storage
	j.err = nil
	j.unsupported = false
}

func (p *vp9DecoderTileWorkerPool) prepareRowMTJob(worker int,
	parent *VP9Decoder, kind vp9DecoderTileJobKind, hdr *vp9dec.UncompressedHeader,
	comp vp9dec.CompressedHeader, tile vp9dec.TileBounds,
	intraMaps vp9dec.IntraSegmentMaps, interMaps vp9dec.InterSegmentMaps,
	miRows, miCols int,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	storage *vp9DecoderRowMTFrameStorage,
) {
	p.jobs[worker].prepareRowMTJob(parent, kind, hdr, comp, tile, intraMaps,
		interMaps, miRows, miCols, partitionProbs, storage)
}

func (j *vp9DecoderTileJob) run() {
	if j.rowMTStorage != nil {
		j.runRowMT()
		return
	}
	worker := &j.worker
	worker.vp9LoopFilterPool = nil
	worker.vp9TilePool = nil
	worker.rowMTSync = j.rowMTSync
	worker.leftSegCtx = j.leftSegCtx[:]
	if len(j.descs) > 0 {
		j.runDescs(worker, j.descs)
		j.descs = nil
		return
	}
	j.resetLeftContexts(worker)
	worker.interPredictScratch = j.interPredictScratch
	j.err = worker.runVP9DecoderTileDesc(j.kind, vp9DecoderTileDesc{
		data: j.data,
		tile: j.tile,
	}, j.hdr, j.comp, j.intraMaps, j.interMaps, j.miRows, j.miCols,
		j.partitionProbs)
	j.interPredictScratch = worker.interPredictScratch
	j.unsupported = worker.unsupportedReconstruct
	worker.rowMTSync = nil
}

func (j *vp9DecoderTileJob) runRowMT() {
	worker := &j.worker
	worker.vp9LoopFilterPool = nil
	worker.vp9TilePool = nil
	worker.rowMTSync = nil
	worker.interPredictScratch = j.interPredictScratch
	spins := 0
	for {
		rowJob, ok := j.rowMTStorage.jobq.dequeue(false)
		if !ok {
			if j.rowMTStorage.jobq.done() {
				break
			}
			runtimeProcYield(30)
			spins++
			if spins >= rowWorkerIdleSchedulerBackoff &&
				spins%rowWorkerIdleSchedulerBackoff == 0 {
				runtime.Gosched()
			}
			continue
		}
		spins = 0
		tile := j.tile
		if state := j.rowMTStorage.tileState(rowJob.tileCol); state != nil {
			tile = state.tile
		}
		switch rowJob.jobType {
		case vp9DecoderRowMTJobParse:
			if err := j.runRowMTParse(rowJob); err != nil {
				j.err = err
				j.rowMTStorage.jobq.terminate()
				break
			}
			continue
		case vp9DecoderRowMTJobLPF:
			if !worker.applyVP9LoopFilterRowMT(j.miRows, j.miCols,
				rowJob.rowNum, &j.rowMTStorage.lfSync) {
				j.err = ErrInvalidVP9Data
				j.rowMTStorage.jobq.terminate()
				break
			}
			if rowJob.rowNum+common.MiBlockSize >= j.miRows {
				j.rowMTStorage.loopFilterApplied = true
				j.rowMTStorage.jobq.terminate()
			}
			continue
		case vp9DecoderRowMTJobRecon:
		default:
			j.err = ErrInvalidVP9Data
			j.rowMTStorage.jobq.terminate()
			break
		}
		if j.err != nil {
			break
		}
		var err error
		if j.kind == vp9DecoderTileJobIntra {
			err = worker.reconstructVP9IntraModeTileRowMTRow(
				j.hdr, j.rowMTStorage, tile, j.miRows, j.miCols,
				rowJob.rowNum, rowJob.tileCol)
		} else {
			err = worker.reconstructVP9InterModeTileRowMTRow(
				j.hdr, j.rowMTStorage, tile, j.miRows, j.miCols,
				rowJob.rowNum, rowJob.tileCol)
		}
		if err != nil {
			j.err = err
			j.rowMTStorage.jobq.terminate()
			break
		}
		lastRow := rowJob.rowNum+common.MiBlockSize >= tile.MiRowEnd
		if !j.rowMTStorage.loopFilterActive && lastRow &&
			j.rowMTStorage.tilesDone.Add(1) == int32(j.rowMTStorage.tileCols) {
			j.rowMTStorage.jobq.terminate()
		}
	}
	j.interPredictScratch = worker.interPredictScratch
	j.unsupported = worker.unsupportedReconstruct
	worker.rowMTSync = nil
}

func (j *vp9DecoderTileJob) runDescs(worker *VP9Decoder, descs []vp9DecoderTileDesc) {
	worker.interPredictScratch = j.interPredictScratch
	for _, desc := range descs {
		j.resetLeftContexts(worker)
		if err := worker.runVP9DecoderTileDesc(j.kind, desc, j.hdr, j.comp,
			j.intraMaps, j.interMaps, j.miRows, j.miCols,
			j.partitionProbs); err != nil {
			j.err = err
			j.interPredictScratch = worker.interPredictScratch
			j.unsupported = worker.unsupportedReconstruct
			worker.rowMTSync = nil
			return
		}
	}
	j.err = nil
	j.interPredictScratch = worker.interPredictScratch
	j.unsupported = worker.unsupportedReconstruct
	worker.rowMTSync = nil
}

func (j *vp9DecoderTileJob) resetLeftContexts(worker *VP9Decoder) {
	for i := range worker.leftSegCtx {
		worker.leftSegCtx[i] = 0
	}
	for plane := range vp9dec.MaxMbPlane {
		leftLen := int(j.leftEntropyLen[plane])
		if leftLen > len(j.leftEntropy[plane]) {
			j.err = ErrInvalidVP9Data
			return
		}
		worker.planes[plane].LeftContext = j.leftEntropy[plane][:leftLen]
		for i := range worker.planes[plane].LeftContext {
			worker.planes[plane].LeftContext[i] = 0
		}
	}
}

func (d *VP9Decoder) runVP9DecoderTileDesc(kind vp9DecoderTileJobKind,
	desc vp9DecoderTileDesc, hdr *vp9dec.UncompressedHeader,
	comp vp9dec.CompressedHeader, intraMaps vp9dec.IntraSegmentMaps,
	interMaps vp9dec.InterSegmentMaps, miRows, miCols int,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
) error {
	if kind == vp9DecoderTileJobIntra {
		return d.parseVP9IntraModeTile(desc.data, hdr, comp, &intraMaps,
			desc.tile, miRows, miCols, partitionProbs)
	}
	return d.parseVP9InterModeTile(desc.data, hdr, comp, &interMaps, desc.tile,
		miRows, miCols, partitionProbs)
}
