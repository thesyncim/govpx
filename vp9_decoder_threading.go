package govpx

import (
	"unsafe"

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

type vp9DecoderLoopFilterJob struct {
	d      *VP9Decoder
	miRows int
	miCols int
	plane  vp9LoopFilterPlane
	ok     bool
}

type vp9DecoderLoopFilterPool struct {
	helperCount int8
	start       [2]chan struct{}
	done        [2]chan struct{}
	exited      [2]chan struct{}
	jobs        [2]vp9DecoderLoopFilterJob
}

func newVP9DecoderLoopFilterPool(threads int) *vp9DecoderLoopFilterPool {
	helpers := min(threads-1, 2)
	if helpers <= 0 {
		return nil
	}
	p := &vp9DecoderLoopFilterPool{
		helperCount: int8(helpers),
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
		job := &p.jobs[worker]
		job.ok = job.d.applyVP9LoopFilterPlane(job.miRows, job.miCols,
			job.plane)
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
		p.jobs[i] = vp9DecoderLoopFilterJob{}
	}
	p.helperCount = 0
}

func (d *VP9Decoder) applyVP9LoopFilterThreaded(miRows, miCols int) bool {
	p := d.vp9LoopFilterPool
	if p == nil || p.helperCount <= 0 {
		return d.applyVP9LoopFilterSerial(miRows, miCols)
	}

	helpers := int(p.helperCount)
	p.jobs[0] = vp9DecoderLoopFilterJob{
		d:      d,
		miRows: miRows,
		miCols: miCols,
		plane:  vp9LoopFilterPlaneU,
	}
	p.start[0] <- struct{}{}
	if helpers > 1 {
		p.jobs[1] = vp9DecoderLoopFilterJob{
			d:      d,
			miRows: miRows,
			miCols: miCols,
			plane:  vp9LoopFilterPlaneV,
		}
		p.start[1] <- struct{}{}
	}

	ok := d.applyVP9LoopFilterPlane(miRows, miCols, vp9LoopFilterPlaneY)
	<-p.done[0]
	ok = ok && p.jobs[0].ok
	if helpers > 1 {
		<-p.done[1]
		ok = ok && p.jobs[1].ok
	} else {
		ok = ok && d.applyVP9LoopFilterPlane(miRows, miCols,
			vp9LoopFilterPlaneV)
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

type vp9DecoderRowMTFrameStorage struct {
	numSBs int

	eob          [vp9dec.MaxMbPlane][]int
	dqcoeffBytes [vp9dec.MaxMbPlane][]byte
	dqcoeff      [vp9dec.MaxMbPlane][]int16
	partition    []common.PartitionType
	reconMap     []int8
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
		s.reconMap = s.reconMap[:0]
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
}

func (s *vp9DecoderRowMTFrameStorage) release() {
	if s == nil {
		return
	}
	s.numSBs = 0
	for plane := range vp9dec.MaxMbPlane {
		s.eob[plane] = nil
		s.dqcoeffBytes[plane] = nil
		s.dqcoeff[plane] = nil
	}
	s.partition = nil
	s.reconMap = nil
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

	leftSegCtx          [common.MiBlockSize]int8
	leftEntropy         [vp9dec.MaxMbPlane][16]uint8
	leftEntropyLen      [vp9dec.MaxMbPlane]uint8
	interPredictScratch []byte

	// rowMTSync, when non-nil, is the per-tile-column wavefront sync the
	// decoder body should call read / write against. Populated by the tile
	// worker pool when VP9D_SET_ROW_MT is enabled.
	rowMTSync *vp9RowMTSync

	err         error
	unsupported bool
}

type vp9DecoderTileWorkerPool struct {
	helperCount int8
	start       []chan struct{}
	done        []chan struct{}
	exited      []chan struct{}
	jobs        []vp9DecoderTileJob
	tileDescs   []vp9DecoderTileDesc
	header      vp9dec.UncompressedHeader

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
		done:        make([]chan struct{}, helpers),
		exited:      make([]chan struct{}, helpers),
		jobs:        make([]vp9DecoderTileJob, helpers),
	}
	for i := range helpers {
		p.start[i] = make(chan struct{})
		p.done[i] = make(chan struct{})
		p.exited[i] = make(chan struct{})
		go p.workerLoop(i)
	}
	return p
}

func (p *vp9DecoderTileWorkerPool) workerLoop(worker int) {
	defer close(p.exited[worker])
	for range p.start[worker] {
		p.jobs[worker].run()
		p.done[worker] <- struct{}{}
	}
}

func (p *vp9DecoderTileWorkerPool) shutdown() {
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
		p.jobs[i] = vp9DecoderTileJob{}
	}
	p.tileDescs = nil
	p.helperCount = 0
	p.lastTileJobs = 0
	p.rowMTSyncs = nil
	p.rowMTFrame.release()
	p.rowMTArmed = false
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

func (p *vp9DecoderTileWorkerPool) ensureRowMTFrameStorage(miRows, miCols int) {
	if p == nil || miRows <= 0 || miCols <= 0 {
		return
	}
	sbRows := common.AlignToSB(miRows) >> common.MiBlockSizeLog2
	sbCols := common.AlignToSB(miCols) >> common.MiBlockSizeLog2
	p.rowMTFrame.reset(sbRows * sbCols)
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
	return d != nil && d.vp9TilePool != nil && hdr != nil &&
		tileRows == 1 && tileCols > 1 && !d.vp9TileFilterActive() &&
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
		p.ensureRowMTFrameStorage(miRows, miCols)
	} else if len(p.rowMTSyncs) > 0 {
		// Reset stale state when the option toggled off mid-stream.
		p.rowMTSyncs = p.rowMTSyncs[:0]
		p.rowMTFrame.release()
	}
	if !d.opts.DecoderRowMT {
		return d.runVP9DecoderScheduledTileJobs(descs, kind, comp,
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
			p.start[worker] <- struct{}{}
		}

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
		for worker := range helpers {
			<-p.done[worker]
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
	for worker := 0; worker < helpers; worker++ {
		count := base + (remain+worker)/workers
		bufEnd := bufStart + count
		p.prepareTileJobRange(worker, d, kind, descs[bufStart:bufEnd], comp,
			intraMaps, interMaps, miRows, miCols, partitionProbs)
		p.start[worker] <- struct{}{}
		bufStart = bufEnd
	}

	mainCount := base + (remain+helpers)/workers
	mainEnd := bufStart + mainCount
	for _, desc := range descs[bufStart:mainEnd] {
		if runErr := d.runVP9DecoderTileDesc(kind, desc, &p.header, comp,
			intraMaps, interMaps, miRows, miCols, partitionProbs); runErr != nil && err == nil {
			err = runErr
		}
	}

	for worker := 0; worker < helpers; worker++ {
		<-p.done[worker]
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
	for i := 0; i < n; i++ {
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
}

func (j *vp9DecoderTileJob) run() {
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
