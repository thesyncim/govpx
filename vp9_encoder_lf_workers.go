package govpx

import (
	"sync"

	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
)

// vp9LfSync mirrors libvpx's VP9LfSync (vp9/common/vp9_thread_common.c): a
// per-SB-row column progress tracker that lets the row-interleaved loop
// filter workers stay one sync range behind the row above, since filtering a
// superblock's top horizontal edge writes pixels that the row above's
// vertical pass must have finished first.
type vp9LfSync struct {
	mu        []sync.Mutex
	cond      []*sync.Cond
	curSbCol  []int32
	rows      int
	syncRange int
}

// vp9LfSyncRange mirrors get_sync_range (vp9_thread_common.c:249):
//
//	// nsync numbers are picked by testing. For example, for 4k
//	// video, using 4 gives best performance.
//	if (width < 640) return 1;
//	else if (width <= 1280) return 2;
//	else if (width <= 4096) return 4;
//	else return 8;
func vp9LfSyncRange(width int) int {
	switch {
	case width < 640:
		return 1
	case width <= 1280:
		return 2
	case width <= 4096:
		return 4
	default:
		return 8
	}
}

// reset re-arms the sync for a frame of sbRows rows, mirroring
// vp9_loop_filter_alloc's cur_sb_col allocation plus the memset to -1 in
// vp9_loop_filter_frame_mt (vp9_thread_common.c:214-216).
func (s *vp9LfSync) reset(sbRows, width int) {
	if s == nil || sbRows <= 0 {
		return
	}
	if cap(s.mu) < sbRows {
		s.mu = make([]sync.Mutex, sbRows)
		s.cond = make([]*sync.Cond, sbRows)
		s.curSbCol = make([]int32, sbRows)
		for r := range s.cond {
			s.cond[r] = sync.NewCond(&s.mu[r])
		}
	} else {
		s.mu = s.mu[:sbRows]
		s.cond = s.cond[:sbRows]
		s.curSbCol = s.curSbCol[:sbRows]
		for r := range s.cond {
			if s.cond[r] == nil {
				s.cond[r] = sync.NewCond(&s.mu[r])
			}
		}
	}
	for r := range s.curSbCol {
		s.curSbCol[r] = -1
	}
	s.rows = sbRows
	s.syncRange = vp9LfSyncRange(width)
}

// read mirrors sync_read (vp9_thread_common.c:22): before filtering SB
// (r, c) the worker waits until row r-1 has advanced at least sync range
// columns past c.
func (s *vp9LfSync) read(r, c int) {
	if s == nil || r <= 0 || r >= s.rows {
		return
	}
	nsync := s.syncRange
	if nsync <= 0 || (c&(nsync-1)) != 0 {
		return
	}
	mu := &s.mu[r-1]
	mu.Lock()
	for int32(c) > s.curSbCol[r-1]-int32(nsync) {
		s.cond[r-1].Wait()
	}
	mu.Unlock()
}

// write mirrors sync_write (vp9_thread_common.c:42): record the row's
// progress and signal waiters when crossing a sync range boundary or when
// the row completes.
func (s *vp9LfSync) write(r, c, sbCols int) {
	if s == nil || r < 0 || r >= s.rows {
		return
	}
	nsync := s.syncRange
	if nsync <= 0 {
		return
	}
	var cur int32
	sig := true
	if c < sbCols-1 {
		cur = int32(c)
		if c%nsync != 0 {
			sig = false
		}
	} else {
		cur = int32(sbCols + nsync)
	}
	if !sig {
		return
	}
	mu := &s.mu[r]
	mu.Lock()
	s.curSbCol[r] = cur
	mu.Unlock()
	s.cond[r].Signal()
}

// markRowsDone releases any rows this worker still owns so sibling workers
// blocked in read never deadlock when a defensive filter-error bail fires.
func (s *vp9LfSync) markRowsDone(startSbRow, step, sbRows, sbCols int) {
	for r := startSbRow; r < sbRows; r += step {
		s.write(r, sbCols-1, sbCols)
	}
}

// vp9EncodeLfJob carries one worker's share of the row-interleaved loop
// filter walk, mirroring libvpx's LFWorkerData for thread_loop_filter_rows.
type vp9EncodeLfJob struct {
	d      VP9Decoder
	lfSync *vp9LfSync
	miRows int
	miCols int
	start  int
	step   int
	ok     bool
}

func runVP9EncodeLfJob(job *vp9EncodeLfJob) {
	if job == nil {
		return
	}
	job.ok = job.d.applyVP9LoopFilterRowsInterleaved(job.miRows, job.miCols,
		job.start, job.step, job.lfSync)
}

// applyVP9LoopFilterRowsInterleaved mirrors thread_loop_filter_rows
// (vp9_thread_common.c:74): the worker filters SB rows start, start+step,
// start+2*step, ... walking columns left to right under the lf sync
// wavefront. Masks must already be prepared for the whole frame.
func (d *VP9Decoder) applyVP9LoopFilterRowsInterleaved(miRows, miCols,
	start, step int, lfSync *vp9LfSync,
) bool {
	if step <= 0 {
		return false
	}
	sbRows := (miRows + common.MiBlockSize - 1) >> common.MiBlockSizeLog2
	sbCols := (miCols + common.MiBlockSize - 1) >> common.MiBlockSizeLog2
	for miRow := start * common.MiBlockSize; miRow < miRows; miRow += step * common.MiBlockSize {
		r := miRow >> common.MiBlockSizeLog2
		for miCol := 0; miCol < miCols; miCol += common.MiBlockSize {
			c := miCol >> common.MiBlockSizeLog2
			lfSync.read(r, c)
			lfm, ok := d.vp9LoopFilterMaskAt(miRow, miCol)
			if !ok || !d.vp9FilterLoopBlock(miRows, miRow, miCol, lfm) {
				// Defensive bail: release the rows this worker owns so
				// siblings blocked in read never deadlock, then report
				// the failure to the dispatcher.
				lfSync.write(r, sbCols-1, sbCols)
				lfSync.markRowsDone(r+step, step, sbRows, sbCols)
				return false
			}
			lfSync.write(r, c, sbCols)
		}
	}
	return true
}

// applyVP9EncoderLoopFilterMT runs the frame loop filter across the tile
// worker pool, mirroring vp9_loop_filter_frame_mt (vp9_thread_common.c:203):
// masks are prepared up front (vp9_build_mask_frame in the encoder's
// loopfilter_frame at vp9_encoder.c:3461), then the pool's workers filter
// interleaved SB rows under the VP9LfSync wavefront. The number of active
// workers is clamped to the SB row count exactly like
// VPXMIN(cpi->num_workers, sb_rows) in loop_filter_rows_mt.
func (e *VP9Encoder) applyVP9EncoderLoopFilterMT(d *VP9Decoder,
	pool *vp9TileWorkerPool, miRows, miCols, width int,
) bool {
	if !d.prepareVP9LoopFilterMasks(miRows, miCols, 0, miRows) {
		return false
	}
	sbRows := (miRows + common.MiBlockSize - 1) >> common.MiBlockSizeLog2
	workers := min(pool.workerCount, sbRows)
	if workers <= 1 {
		return d.applyVP9LoopFilterSerialCached(miRows, miCols)
	}
	pool.lfJobs = buffers.EnsureLen(pool.lfJobs, pool.workerCount)
	pool.lfSync.reset(sbRows, width)
	for i := 0; i < pool.workerCount; i++ {
		job := &pool.lfJobs[i]
		start := i
		if i >= workers {
			// Inactive worker: no rows. Mirrors num_active_workers in
			// thread_loop_filter_rows bounding both the row assignment
			// and the interleave step.
			start = sbRows
		}
		*job = vp9EncodeLfJob{
			d:      *d,
			lfSync: &pool.lfSync,
			miRows: miRows,
			miCols: miCols,
			start:  start,
			step:   workers,
			ok:     true,
		}
	}
	pool.startHelperWorkers(vp9TileWorkerJobLoopFilter)
	runVP9EncodeLfJob(&pool.lfJobs[0])
	pool.waitHelperWorkers()
	ok := true
	for i := range pool.lfJobs {
		ok = ok && pool.lfJobs[i].ok
	}
	return ok
}
