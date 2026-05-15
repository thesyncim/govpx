package govpx

import (
	"encoding/binary"

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
	helpers := threads - 1
	if helpers > 2 {
		helpers = 2
	}
	if helpers <= 0 {
		return nil
	}
	p := &vp9DecoderLoopFilterPool{
		helperCount: int8(helpers),
	}
	for i := 0; i < helpers; i++ {
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

	lastTileJobs    uint8
	lastTileJobKind vp9DecoderTileJobKind
}

func newVP9DecoderTileWorkerPool(threads int) *vp9DecoderTileWorkerPool {
	helpers := threads - 1
	if helpers > 63 {
		helpers = 63
	}
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
	for i := 0; i < helpers; i++ {
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
}

func (d *VP9Decoder) vp9DecoderTileThreadingEnabled(
	hdr *vp9dec.UncompressedHeader, tileRows, tileCols int,
) bool {
	return d != nil && d.vp9TilePool != nil && hdr != nil &&
		hdr.FrameParallelDecoding && tileRows == 1 && tileCols > 1
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
	tileRows := 1 << uint(hdr.Tile.Log2TileRows)
	tileCols := 1 << uint(hdr.Tile.Log2TileCols)
	nTiles := tileRows * tileCols
	if cap(p.tileDescs) < nTiles {
		p.tileDescs = make([]vp9DecoderTileDesc, nTiles)
	} else {
		p.tileDescs = p.tileDescs[:nTiles]
	}
	offset := 0
	for tileRow := range tileRows {
		for tileCol := range tileCols {
			idx := tileRow*tileCols + tileCol
			isLast := idx == nTiles-1
			tileSize := len(tileData) - offset
			if !isLast {
				if len(tileData)-offset < 4 {
					return nil, ErrInvalidVP9Data
				}
				size := binary.BigEndian.Uint32(tileData[offset : offset+4])
				offset += 4
				if size > uint32(len(tileData)-offset) {
					return nil, ErrInvalidVP9Data
				}
				tileSize = int(size)
			}
			if tileSize < 0 || offset+tileSize > len(tileData) {
				return nil, ErrInvalidVP9Data
			}
			p.tileDescs[idx] = vp9DecoderTileDesc{
				data: tileData[offset : offset+tileSize],
				tile: vp9dec.TileBounds{
					MiRowStart: vp9DecoderTileOffset(tileRow, miRows,
						hdr.Tile.Log2TileRows),
					MiRowEnd: vp9DecoderTileOffset(tileRow+1, miRows,
						hdr.Tile.Log2TileRows),
					MiColStart: vp9DecoderTileOffset(tileCol, miCols,
						hdr.Tile.Log2TileCols),
					MiColEnd: vp9DecoderTileOffset(tileCol+1, miCols,
						hdr.Tile.Log2TileCols),
				},
			}
			offset += tileSize
		}
	}
	if offset != len(tileData) {
		return nil, ErrInvalidVP9Data
	}
	return p.tileDescs, nil
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
	helpersMax := int(p.helperCount)
	next := 0
	jobsRun := 0
	for next < len(descs) {
		helpers := len(descs) - next - 1
		if helpers > helpersMax {
			helpers = helpersMax
		}
		for worker := 0; worker < helpers; worker++ {
			desc := descs[next+worker+1]
			p.prepareTileJob(worker, d, kind, desc, comp, intraMaps,
				interMaps, miRows, miCols, partitionProbs)
			p.start[worker] <- struct{}{}
		}

		err := d.runVP9DecoderTileDesc(kind, descs[next], &p.header, comp,
			intraMaps, interMaps, miRows, miCols, partitionProbs)
		for worker := 0; worker < helpers; worker++ {
			<-p.done[worker]
			job := &p.jobs[worker]
			if job.unsupported {
				d.unsupportedReconstruct = true
			}
			if err == nil && job.err != nil {
				err = job.err
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
	for plane := range vp9dec.MaxMbPlane {
		job.leftEntropyLen[plane] = uint8(len(parent.planes[plane].LeftContext))
	}
}

func (j *vp9DecoderTileJob) run() {
	worker := &j.worker
	worker.vp9LoopFilterPool = nil
	worker.vp9TilePool = nil
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
