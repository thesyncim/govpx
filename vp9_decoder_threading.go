package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
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

type vp9DecoderTileJob struct {
	data           []byte
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

	lastTileJobs    uint8
	lastTileJobKind vp9DecoderTileJobKind
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
	if cap(p.rowMTSyncs) < tileCols {
		p.rowMTSyncs = make([]vp9RowMTSync, tileCols)
	} else {
		p.rowMTSyncs = p.rowMTSyncs[:tileCols]
	}
	for i := range p.rowMTSyncs {
		p.rowMTSyncs[i].reset(sbRows)
	}
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
	} else if len(p.rowMTSyncs) > 0 {
		// Reset stale state when the option toggled off mid-stream.
		p.rowMTSyncs = p.rowMTSyncs[:0]
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

func (p *vp9DecoderTileWorkerPool) prepareTileJob(worker int,
	parent *VP9Decoder, kind vp9DecoderTileJobKind, desc vp9DecoderTileDesc,
	comp vp9dec.CompressedHeader,
	intraMaps vp9dec.IntraSegmentMaps, interMaps vp9dec.InterSegmentMaps,
	miRows, miCols int,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
) {
	job := &p.jobs[worker]
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

func (j *vp9DecoderTileJob) run() {
	worker := &j.worker
	worker.vp9LoopFilterPool = nil
	worker.vp9TilePool = nil
	worker.rowMTSync = j.rowMTSync
	worker.leftSegCtx = j.leftSegCtx[:]
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
