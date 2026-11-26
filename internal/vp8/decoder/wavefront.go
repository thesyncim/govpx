package decoder

import (
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/thesyncim/govpx/internal/vp8/boolcoder"
	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// paddedAtomicInt32 isolates per-row wavefront progress counters onto distinct
// 64-byte cache lines so concurrent row workers never false-share.
type paddedAtomicInt32 struct {
	v atomic.Int32
	_ [60]byte
}

type wavefrontWorker struct {
	id      int
	startCh chan struct{}
	doneCh  chan struct{}
	scratch IntraReconstructionScratch
	tokens  MacroblockTokens
	err     error
}

type wavefrontJob struct {
	img               *common.Image
	last              *common.Image
	golden            *common.Image
	alt               *common.Image
	lastState         InterFrameRefState
	goldenState       InterFrameRefState
	altState          InterFrameRefState
	readers           []boolcoder.Decoder
	probs             *tables.CoefficientProbs
	modes             []MacroblockMode
	dequants          *[common.MaxMBSegments]common.MacroblockDequant
	cfg               InterPredictionConfig
	rows              int
	cols              int
	workers           int
	partitions        int
	keyFrame          bool
	loopFilterEnabled bool
	simpleLF          bool
	frameType         common.FrameType
	lfi               *common.LoopFilterInfo
}

// WavefrontPool is a persistent, zero-allocation-per-frame worker pool that
// fuses VP8 token decoding, macroblock reconstruction, and loop filtering in a
// row wavefront across multiple token partitions.
//
// Each wavefront worker w in [0, W) (where W is a power of two dividing
// TokenCount) owns rows r = w, w+W, w+2W, ... and therefore has exclusive
// ownership of readers[r & (TokenCount-1)] with zero locking. Token decode and
// reconstruction for each macroblock run back-to-back using a single L1-hot
// MacroblockTokens struct per worker instead of writing and re-reading a
// multi-megabyte frame token grid.
//
// A dedicated loop-filter worker runs concurrently two rows behind the
// reconstruction wavefront, matching libvpx's row-thread dependency contract.
type WavefrontPool struct {
	mu          sync.Mutex
	closed      bool
	maxWorkers  int
	bgWorkers   []wavefrontWorker
	lfStartCh   chan struct{}
	lfDoneCh    chan struct{}
	lfErr       error
	exitWg      sync.WaitGroup
	mainScratch IntraReconstructionScratch
	mainTokens  MacroblockTokens
	aboveGrid   []EntropyContextPlanes
	rowProgress []paddedAtomicInt32
	aborted     atomic.Bool
	job         wavefrontJob
	lfActive    bool
	activeBG    int
}

// NewWavefrontPool creates a persistent pool sized for up to threads workers.
// Background goroutines are started lazily on the first Run call and stopped
// when Close is called.
func NewWavefrontPool(threads int) *WavefrontPool {
	if threads < 2 {
		threads = 2
	}
	if threads > 8 {
		threads = 8
	}
	return &WavefrontPool{
		maxWorkers: threads,
	}
}

func (p *WavefrontPool) ensureStarted() {
	if len(p.bgWorkers) > 0 || p.lfStartCh != nil {
		return
	}
	bgCount := p.maxWorkers - 1
	p.bgWorkers = make([]wavefrontWorker, bgCount)
	for i := range p.bgWorkers {
		w := &p.bgWorkers[i]
		w.id = i + 1
		w.startCh = make(chan struct{}, 1)
		w.doneCh = make(chan struct{}, 1)
		p.exitWg.Add(1)
		go p.runWavefrontWorker(w)
	}
	p.lfStartCh = make(chan struct{}, 1)
	p.lfDoneCh = make(chan struct{}, 1)
	p.exitWg.Add(1)
	go p.runLoopFilterWorker()
}

// Close shuts down all background goroutines owned by the pool.
func (p *WavefrontPool) Close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	for i := range p.bgWorkers {
		close(p.bgWorkers[i].startCh)
	}
	if p.lfStartCh != nil {
		close(p.lfStartCh)
	}
	p.mu.Unlock()
	p.exitWg.Wait()
}

func (p *WavefrontPool) runWavefrontWorker(w *wavefrontWorker) {
	defer p.exitWg.Done()
	for range w.startCh {
		w.err = p.runRowsForWorker(w.id, &w.scratch, &w.tokens)
		if w.err != nil {
			p.aborted.Store(true)
		}
		w.doneCh <- struct{}{}
	}
}

func (p *WavefrontPool) runLoopFilterWorker() {
	defer p.exitWg.Done()
	for range p.lfStartCh {
		p.lfErr = p.runLoopFilterRows()
		if p.lfErr != nil {
			p.aborted.Store(true)
		}
		p.lfDoneCh <- struct{}{}
	}
}

// EffectiveWavefrontWorkers returns the largest power-of-two worker count W
// with 1 <= W <= min(threads, partitions, rows). Because W divides partitions,
// worker w owning rows r = w, w+W, ... has exclusive access to
// readers[r & (partitions-1)].
func EffectiveWavefrontWorkers(threads int, partitions int, rows int) int {
	limit := min(threads, min(partitions, rows))
	w := 1
	for (w << 1) <= limit {
		w <<= 1
	}
	return w
}

// DecodeReconstructLoopFilterWavefront fuses token decoding, macroblock
// reconstruction, and loop filtering across persistent wavefront workers.
// Output pixels and mode.MBSkipCoeff updates are byte-identical to the serial
// DecodeTokenGrid + reconstruct + ApplyLoopFilter pipeline.
func (p *WavefrontPool) DecodeReconstructLoopFilterWavefront(
	img *common.Image,
	last *common.Image,
	golden *common.Image,
	alt *common.Image,
	rows int,
	cols int,
	readers []boolcoder.Decoder,
	probs *tables.CoefficientProbs,
	modes []MacroblockMode,
	dequants *[common.MaxMBSegments]common.MacroblockDequant,
	cfg InterPredictionConfig,
	keyFrame bool,
	loopFilterEnabled bool,
	frameType common.FrameType,
	header LoopFilterHeader,
	segmentation SegmentationHeader,
	lfi *common.LoopFilterInfo,
) error {
	if rows < 0 || cols < 0 {
		return ErrReconstructGridBufferTooSmall
	}
	if rows != 0 && cols > int(^uint(0)>>1)/rows {
		return ErrReconstructGridBufferTooSmall
	}
	required := rows * cols
	partitions := len(readers)
	if partitions != 1 && partitions != 2 && partitions != 4 && partitions != 8 {
		return ErrTokenGridBufferTooSmall
	}
	if img == nil || probs == nil || dequants == nil || len(modes) < required {
		return ErrReconstructGridBufferTooSmall
	}
	if !keyFrame {
		if last == nil || golden == nil || alt == nil {
			return ErrReconstructGridBufferTooSmall
		}
		if !imageHasMacroblockGrid(last, rows, cols) || !imageHasMacroblockGrid(golden, rows, cols) || !imageHasMacroblockGrid(alt, rows, cols) {
			return ErrReconstructGridBufferTooSmall
		}
	}
	if !imageHasMacroblockGrid(img, rows, cols) {
		return ErrReconstructGridBufferTooSmall
	}
	if rows == 0 || cols == 0 {
		return nil
	}

	runLF := loopFilterEnabled && header.Level != 0
	if runLF {
		if lfi == nil || frameType < common.KeyFrame || frameType > common.InterFrame {
			return ErrLoopFilterBufferTooSmall
		}
		common.InitLoopFilterInfo(lfi, int(header.SharpnessLevel))
		common.InitLoopFilterFrame(lfi, int(header.Level), loopFilterFrameConfig(header, segmentation))
	}

	p.ensureStarted()

	workers := EffectiveWavefrontWorkers(p.maxWorkers, partitions, rows)
	if cap(p.aboveGrid) < required {
		p.aboveGrid = make([]EntropyContextPlanes, required)
	} else {
		p.aboveGrid = p.aboveGrid[:required]
	}
	if cap(p.rowProgress) < rows {
		p.rowProgress = make([]paddedAtomicInt32, rows)
	} else {
		p.rowProgress = p.rowProgress[:rows]
	}
	for r := range rows {
		p.rowProgress[r].v.Store(0)
	}
	p.aborted.Store(false)

	var lastState, goldenState, altState InterFrameRefState
	if !keyFrame {
		lastState = newFrameInterRefState(last, cfg)
		goldenState = newFrameInterRefState(golden, cfg)
		altState = newFrameInterRefState(alt, cfg)
	}

	p.job = wavefrontJob{
		img:               img,
		last:              last,
		golden:            golden,
		alt:               alt,
		lastState:         lastState,
		goldenState:       goldenState,
		altState:          altState,
		readers:           readers,
		probs:             probs,
		modes:             modes,
		dequants:          dequants,
		cfg:               cfg,
		rows:              rows,
		cols:              cols,
		workers:           workers,
		partitions:        partitions,
		keyFrame:          keyFrame,
		loopFilterEnabled: runLF,
		simpleLF:          header.Type == SimpleLoopFilter,
		frameType:         frameType,
		lfi:               lfi,
	}

	// Launch background wavefront workers and loop-filter worker.
	p.activeBG = workers - 1
	p.lfActive = runLF
	for i := 0; i < p.activeBG; i++ {
		p.bgWorkers[i].startCh <- struct{}{}
	}
	if p.lfActive {
		p.lfStartCh <- struct{}{}
	}

	// Main goroutine runs worker 0.
	mainErr := p.runRowsForWorker(0, &p.mainScratch, &p.mainTokens)
	if mainErr != nil {
		p.aborted.Store(true)
	}

	// Wait for all background workers and LF worker to finish.
	firstErr := mainErr
	for i := 0; i < p.activeBG; i++ {
		<-p.bgWorkers[i].doneCh
		if firstErr == nil && p.bgWorkers[i].err != nil {
			firstErr = p.bgWorkers[i].err
		}
	}
	if p.lfActive {
		<-p.lfDoneCh
		if firstErr == nil && p.lfErr != nil {
			firstErr = p.lfErr
		}
	}
	return firstErr
}

const wavefrontChunkCols = 4

func (p *WavefrontPool) waitRowCol(row int, want int) bool {
	const spinBudget = 4096
	want32 := int32(want)
	cell := &p.rowProgress[row].v
	for i := 0; ; i++ {
		if cell.Load() >= want32 {
			return true
		}
		if i&15 == 15 && p.aborted.Load() {
			return false
		}
		if i >= spinBudget {
			runtime.Gosched()
			i = 0
		}
	}
}

func (p *WavefrontPool) runLoopFilterRows() error {
	j := &p.job
	rows := j.rows
	cols := j.cols
	for row := range rows {
		// LF row R must wait until recon row R and recon row R+1 (if any) have
		// completed, matching the border-extension contract in threading.go.
		if !p.waitRowCol(row, cols) {
			return ErrThreadingPipelineFailure
		}
		if row+1 < rows {
			if !p.waitRowCol(row+1, cols) {
				return ErrThreadingPipelineFailure
			}
		}
		var err error
		if j.simpleLF {
			err = applySimpleLoopFilterRow(j.img, row, cols, j.modes, j.lfi)
		} else {
			err = applyNormalLoopFilterRow(j.img, row, cols, j.modes, j.frameType, j.lfi)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (p *WavefrontPool) runRowsForWorker(workerID int, scratch *IntraReconstructionScratch, tok *MacroblockTokens) error {
	j := &p.job
	rows := j.rows
	cols := j.cols
	partMask := j.partitions - 1
	img := j.img
	probs := j.probs
	dequants := j.dequants
	aboveGrid := p.aboveGrid

	for row := workerID; row < rows; row += j.workers {
		reader := &j.readers[row&partMask]
		rowBase := row * cols
		rowModes := j.modes[rowBase : rowBase+cols]
		rowAbove := aboveGrid[rowBase : rowBase+cols]
		var prevAbove []EntropyContextPlanes
		if row > 0 {
			prevAbove = aboveGrid[rowBase-cols : rowBase]
		}

		yRow := row * 16 * img.YStride
		uRow := row * 8 * img.UStride
		vRow := row * 8 * img.VStride
		left := EntropyContextPlanes{}

		for chunkStart := 0; chunkStart < cols; chunkStart += wavefrontChunkCols {
			chunkEnd := min(chunkStart+wavefrontChunkCols, cols)
			if row > 0 {
				// Wait for row-1 through min(chunkEnd+1, cols) so token above
				// context AND intra B_LD_PRED top-right (col+1) / extended
				// right border (when chunkEnd == cols) are all published.
				needed := min(chunkEnd+1, cols)
				if !p.waitRowCol(row-1, needed) {
					return ErrThreadingPipelineFailure
				}
			}

			for col := chunkStart; col < chunkEnd; col++ {
				mode := &rowModes[col]
				if mode.SegmentID >= common.MaxMBSegments {
					if j.keyFrame {
						return ErrUnsupportedIntraReconstructionMode
					}
					return ErrUnsupportedInterReconstructionMode
				}

				var above EntropyContextPlanes
				if row > 0 {
					above = prevAbove[col]
				}

				// Decode tokens into worker-local L1-hot tok.
				if mode.MBSkipCoeff {
					ResetMacroblockTokenContext(&above, &left, mode.Is4x4)
					rowAbove[col] = above
				} else {
					eobTotal := DecodeMacroblockTokens(reader, probs, mode.Is4x4, &above, &left, tok)
					if eobTotal == 0 {
						mode.MBSkipCoeff = true
					}
					rowAbove[col] = above
				}

				// Reconstruct macroblock immediately while tok is hot in L1.
				yOff := yRow + col*16
				uOff := uRow + col*8
				vOff := vRow + col*8

				if j.keyFrame {
					refs := BuildIntraPredictorRefs(img, row, col, &scratch.Refs)
					if !ReconstructIntraMacroblock(mode, tok, &(*dequants)[mode.SegmentID], refs, img.Y[yOff:], img.YStride, img.U[uOff:], img.UStride, img.V[vOff:], img.VStride, &scratch.Residual) {
						return ErrUnsupportedIntraReconstructionMode
					}
					continue
				}

				if mode.RefFrame == common.IntraFrame {
					refs := BuildIntraPredictorRefs(img, row, col, &scratch.Refs)
					if !ReconstructIntraMacroblock(mode, tok, &(*dequants)[mode.SegmentID], refs, img.Y[yOff:], img.YStride, img.U[uOff:], img.UStride, img.V[vOff:], img.VStride, &scratch.Residual) {
						return ErrUnsupportedInterReconstructionMode
					}
					continue
				}

				var refState *InterFrameRefState
				var ref *common.Image
				switch mode.RefFrame {
				case common.LastFrame:
					refState = &j.lastState
					ref = j.last
				case common.GoldenFrame:
					refState = &j.goldenState
					ref = j.golden
				case common.AltRefFrame:
					refState = &j.altState
					ref = j.alt
				default:
					return ErrUnsupportedInterReconstructionMode
				}

				if ref != img && mode.Mode == common.ZeroMV && mode.MBSkipCoeff && !mode.Is4x4 && mode.MV.IsZero() {
					// Check for a run of skipped ZeroMV MBs within the current chunk.
					run := zeroMVSkippedRunLength(rowModes, col, chunkEnd, mode.RefFrame)
					if run > 1 {
						// Reset token contexts for the remaining MBs in the run.
						for k := 1; k < run; k++ {
							var a EntropyContextPlanes
							if row > 0 {
								a = prevAbove[col+k]
							}
							ResetMacroblockTokenContext(&a, &left, false)
							rowAbove[col+k] = a
						}
						if copyZeroMVInterMacroblockRunFast(refState, img.Y[yOff:], img.YStride, img.U[uOff:], img.UStride, img.V[vOff:], img.VStride, row, col, run) {
							col += run - 1
							continue
						}
						// Fallback: reconstruct the first MB individually; loop continues at col+1.
						// Note: if fallback ran, we'd have already overwritten rowAbove[col+1..],
						// which is identical since they are also MBSkipCoeff && !Is4x4.
					}
					if copyZeroMVInterMacroblockFast(refState, img.Y[yOff:], img.YStride, img.U[uOff:], img.UStride, img.V[vOff:], img.VStride, row, col) {
						continue
					}
				}

				if mode.Mode == common.SplitMV {
					if !reconstructSplitMVInterMacroblockFast(refState, mode, tok, &(*dequants)[mode.SegmentID], img.Y[yOff:], img.YStride, img.U[uOff:], img.UStride, img.V[vOff:], img.VStride, &scratch.Residual, row, col, j.cfg) {
						return ErrUnsupportedInterReconstructionMode
					}
					continue
				}
				if !reconstructWholeMVInterMacroblockFast(refState, mode, tok, &(*dequants)[mode.SegmentID], img.Y[yOff:], img.YStride, img.U[uOff:], img.UStride, img.V[vOff:], img.VStride, &scratch.Residual, row, col) {
					return ErrUnsupportedInterReconstructionMode
				}
			}

			if chunkEnd < cols {
				p.rowProgress[row].v.Store(int32(chunkEnd))
			}
		}

		// Extend intra right edge before publishing full row completion.
		extendIntraRightEdgeForRow(img, row)
		p.rowProgress[row].v.Store(int32(cols))
	}
	return nil
}
