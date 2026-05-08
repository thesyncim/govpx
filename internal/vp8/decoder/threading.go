package decoder

import (
	"errors"
	"sync"

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

	// rowDone[i] is published by the reconstruct goroutine once row i is
	// fully reconstructed (including extendIntraRightEdgeForRow). The LF
	// goroutine waits on rowDone[i+1] before processing LF row i, so it
	// never reads pixels that recon has not yet written. Using sync.Cond
	// with a single shared counter avoids per-row channel allocation.
	state := &pipelineState{rows: rows}
	state.cond = sync.NewCond(&state.mu)

	var reconErr error
	var lfErr error

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for row := 0; row < rows; row++ {
			var err error
			if keyFrame {
				err = reconstructKeyFrameIntraGridRow(img, row, cols, modes, tokens, dequants, scratch)
			} else {
				err = reconstructInterFrameGridRow(img, last, golden, alt, &lastState, &goldenState, &altState, row, cols, modes, tokens, dequants, scratch, cfg)
			}
			if err != nil {
				state.publishError(err)
				reconErr = err
				return
			}
			state.publishReconRow(row + 1)
		}
	}()

	go func() {
		defer wg.Done()
		if !loopFilterEnabled {
			// Drain the recon side without doing LF work. We still must
			// consume rows so a producer error is visible.
			for {
				done, err := state.waitForReconAtLeast(rows)
				if err != nil {
					lfErr = err
					return
				}
				if done {
					return
				}
			}
		}
		for row := 0; row < rows; row++ {
			// LF row R must wait for recon row R+1 to finish: recon row
			// R+1's rightmost MB reads the extended right-border at row
			// 16R+15 (and 8R+7 for chroma), which LF row R's rightmost-MB
			// vertical inner-edge filter would otherwise overwrite at
			// cols codedWidth, codedWidth+1. After recon row R+1 has
			// consumed the border, LF row R is free to run in parallel
			// with recon row R+2. The last row (R == rows-1) needs no
			// successor, so it just needs reconAt == rows.
			needed := row + 2
			if needed > rows {
				needed = rows
			}
			if _, err := state.waitForReconAtLeast(needed); err != nil {
				lfErr = err
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

// pipelineState shares progress between the recon producer and the LF
// consumer. row counter advances monotonically; an error in either stage
// poisons the pipeline so the other goroutine can unblock and exit.
type pipelineState struct {
	mu      sync.Mutex
	cond    *sync.Cond
	rows    int
	reconAt int   // number of rows fully reconstructed
	err     error // first error from either stage
}

func (s *pipelineState) publishReconRow(count int) {
	s.mu.Lock()
	if count > s.reconAt {
		s.reconAt = count
	}
	s.cond.Broadcast()
	s.mu.Unlock()
}

func (s *pipelineState) publishError(err error) {
	s.mu.Lock()
	if s.err == nil {
		s.err = err
	}
	s.cond.Broadcast()
	s.mu.Unlock()
}

// waitForReconAtLeast blocks until s.reconAt >= want (or an error has been
// published). The bool return reports whether the producer has finished
// the entire frame (used by the drain path when LF is disabled).
func (s *pipelineState) waitForReconAtLeast(want int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for s.reconAt < want && s.err == nil {
		s.cond.Wait()
	}
	if s.err != nil {
		return false, s.err
	}
	return s.reconAt >= s.rows, nil
}
