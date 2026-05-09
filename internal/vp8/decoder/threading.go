package decoder

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/thesyncim/govpx/internal/vp8/common"
)

// Ported from libvpx v1.16.0 vp8/decoder/threading.c row-thread driver. The
// per-MB-row reconstruction and loop-filter pipelines run on distinct
// goroutines so the loop filter for row R-2 overlaps reconstruction of row
// R+1. Reconstruction itself remains row-sequential (intra prediction has a
// strict row-stride dependency on the previous row's bottom samples and
// extended right edge); the loop filter is also row-sequential (the MB
// horizontal-edge filter for row R reads pixels modified by the LF for row
// R-1). The pipeline is therefore a two-stage producer/consumer split: the
// reconstruction goroutine emits "row R reconstructed" events and the LF
// goroutine consumes them while staying two rows behind.
//
// The two-row gap is required because the rightmost MB's vertical inner
// edge filter at column 12 reads cols [codedWidth-4..codedWidth+3] and
// modifies cols [codedWidth-2..codedWidth+1] of every absolute row in the
// MB (including 16R+15 in luma and 8R+7 in chroma). Recon for row R+1
// reads row 16R+15 cols [codedWidth, codedWidth+3] when constructing the
// intra "above" buffer for its rightmost MB (extendIntraRightEdgeForRow
// populates those border bytes). To keep that read race-free, LF row R
// must wait until recon row R+1 has completed (i.e., already consumed the
// extended right edge); LF row R then runs in parallel with recon row R+2.
//
// All other body modifications by LF row R land within absolute rows
// 16R-3..16R+13 (luma) and 8R-3..8R+6 (chroma), which neither recon row
// R+1 nor recon row R+2 touch.
//
// Synchronisation between the two stages uses an atomic counter rather
// than a mutex/cond pair: the producer publishes row progress with
// atomic.Store and the consumer spins (with a runtime.Gosched fallback)
// on atomic.Load. This avoids the ~200ns mutex acquire/wakeup cost on
// every MB row at 720p (45 rows/frame, ~9us pure sync overhead per frame
// removed) and keeps the hot path lock-free.

// ErrThreadingPipelineFailure is returned when a pipeline stage encounters
// an unrecoverable reconstruction or loop-filter error.
var ErrThreadingPipelineFailure = errors.New("govpx: VP8 decoder threading pipeline failure")

// ReconstructAndLoopFilterPipelined reconstructs an entire frame and applies
// the loop filter using a two-stage producer/consumer pipeline backed by
// goroutines. Output pixels are byte-identical to the serial path.
//
// loopFilterEnabled controls whether the loop filter runs at all. When
// false the function still reconstructs the frame but skips the LF stage,
// matching ApplyLoopFilter's "level == 0 / version skips LF" behavior.
func ReconstructAndLoopFilterPipelined(
	img *common.Image,
	last *common.Image,
	golden *common.Image,
	alt *common.Image,
	rows int,
	cols int,
	modes []MacroblockMode,
	tokens []MacroblockTokens,
	dequants *[common.MaxMBSegments]common.MacroblockDequant,
	scratch *IntraReconstructionScratch,
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
	if img == nil || dequants == nil || scratch == nil || len(modes) < required || len(tokens) < required {
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
	if loopFilterEnabled {
		if lfi == nil || frameType < common.KeyFrame || frameType > common.InterFrame {
			return ErrLoopFilterBufferTooSmall
		}
	}

	// Reconstruction state cached once per frame.
	var lastState, goldenState, altState frameInterRefState
	if !keyFrame {
		lastState = newFrameInterRefState(last, cfg)
		goldenState = newFrameInterRefState(golden, cfg)
		altState = newFrameInterRefState(alt, cfg)
	}

	// LF state init (matches ApplyLoopFilter setup exactly).
	var simple bool
	if loopFilterEnabled {
		common.InitLoopFilterInfo(lfi, int(header.SharpnessLevel))
		common.InitLoopFilterFrame(lfi, int(header.Level), loopFilterFrameConfig(header, segmentation))
		simple = header.Type == SimpleLoopFilter
	}

	if rows == 0 {
		return nil
	}

	// reconAt is the lock-free progress counter published by the
	// reconstruction goroutine. The LF goroutine reads it with
	// atomic.Load and spins (Gosched fallback after a small budget) until
	// the required row count is reached. errFlag is a one-shot abort
	// signal so a producer error unblocks the LF consumer immediately.
	state := pipelineStatePool.Get().(*pipelineState)
	state.reset(rows)
	defer pipelineStatePool.Put(state)

	var reconErr, lfErr error

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for row := range rows {
			var err error
			if keyFrame {
				err = reconstructKeyFrameIntraGridRow(img, row, cols, modes, tokens, dequants, scratch)
			} else {
				err = reconstructInterFrameGridRow(img, last, golden, alt, &lastState, &goldenState, &altState, row, cols, modes, tokens, dequants, scratch, cfg)
			}
			if err != nil {
				reconErr = err
				atomic.StoreInt32(&state.errFlag, 1)
				atomic.StoreInt32(&state.reconAt, int32(rows))
				return
			}
			atomic.StoreInt32(&state.reconAt, int32(row+1))
		}
	}()

	go func() {
		defer wg.Done()
		if !loopFilterEnabled {
			// Drain the recon side without doing LF work. We spin on the
			// progress counter so a producer error is visible.
			waitReconAtLeast(state, rows)
			return
		}
		for row := range rows {
			// LF row R must wait for recon row R+1 to finish: recon row
			// R+1's rightmost MB reads the extended right-border at row
			// 16R+15 (and 8R+7 for chroma), which LF row R's rightmost-MB
			// vertical inner-edge filter would otherwise overwrite at
			// cols codedWidth, codedWidth+1. After recon row R+1 has
			// consumed the border, LF row R is free to run in parallel
			// with recon row R+2. The last row (R == rows-1) needs no
			// successor, so it just needs reconAt == rows.
			needed := min(row+2, rows)
			if !waitReconAtLeast(state, needed) {
				lfErr = ErrThreadingPipelineFailure
				return
			}
			if header.Level == 0 {
				continue
			}
			var err error
			if simple {
				err = applySimpleLoopFilterRow(img, row, cols, modes, lfi)
			} else {
				err = applyNormalLoopFilterRow(img, row, cols, modes, frameType, lfi)
			}
			if err != nil {
				lfErr = err
				return
			}
		}
	}()

	wg.Wait()

	if reconErr != nil {
		return reconErr
	}
	if lfErr != nil {
		return lfErr
	}
	return nil
}

// pipelineState shares lock-free progress between the recon producer and
// the LF consumer. reconAt monotonically advances and is only ever
// read/written via atomic.{Load,Store}Int32. errFlag is a one-shot
// poison: setting it lets a waiting consumer abort without first having
// to observe rows == reconAt.
type pipelineState struct {
	reconAt int32
	errFlag int32
}

func (s *pipelineState) reset(rows int) {
	atomic.StoreInt32(&s.reconAt, 0)
	atomic.StoreInt32(&s.errFlag, 0)
	_ = rows
}

// pipelineStatePool reuses pipelineState across frames so the per-frame
// allocation cost (~16 bytes) does not show up in the steady-state decoder
// path. The pool is package-level so multiple decoder instances share the
// same set of free states.
var pipelineStatePool = sync.Pool{
	New: func() any { return &pipelineState{} },
}

// waitReconAtLeast spins on the recon progress counter until it reaches
// at least `want`, returning false if a producer error has been raised in
// the meantime. The spin starts pure-CPU (handful of iterations) before
// falling back to runtime.Gosched so a busy frame never blocks a
// scheduler turn entirely. Empirically a 720p frame (45 rows) sees zero
// Gosched calls — the producer is always ahead of the consumer because
// recon-row work dominates LF-row work.
func waitReconAtLeast(s *pipelineState, want int) bool {
	const spinBudget = 256
	for i := 0; ; i++ {
		if int(atomic.LoadInt32(&s.reconAt)) >= want {
			return true
		}
		if atomic.LoadInt32(&s.errFlag) != 0 {
			return false
		}
		if i >= spinBudget {
			runtime.Gosched()
			i = 0
		}
	}
}
