//go:build govpx_oracle_trace

package govpx

import (
	"testing"
)

func resetVP9OracleThreadedTileJobsForTest(enc *VP9Encoder) {
	if enc == nil || enc.vp9TilePool == nil {
		return
	}
	for i := range enc.vp9TilePool.encodeJobs {
		enc.vp9TilePool.encodeJobs[i].size = 0
		enc.vp9TilePool.encodeJobs[i].err = nil
	}
}

func assertVP9OracleThreadedTileWriterUsed(t *testing.T, enc *VP9Encoder,
	frame int, wantJobs int,
) {
	t.Helper()
	if enc == nil {
		t.Fatalf("frame %d: nil VP9 encoder while checking threaded tile writer", frame)
	}
	pool := enc.vp9TilePool
	if pool == nil {
		t.Fatalf("frame %d: VP9 threaded tile worker pool was not initialized", frame)
	}
	if got := pool.workerCount; got != wantJobs {
		t.Fatalf("frame %d: VP9 threaded tile worker count = %d, want %d",
			frame, got, wantJobs)
	}
	// The last pool epoch of a frame is the encode pass or, on reference
	// frames, the row-interleaved loop filter that follows it (libvpx
	// vp9_loop_filter_frame_mt reusing cpi->workers).
	if got := vp9TileWorkerJobKind(pool.jobKind.Load()); got != vp9TileWorkerJobEncode &&
		got != vp9TileWorkerJobLoopFilter {
		t.Fatalf("frame %d: VP9 tile worker job kind = %d, want encode or loopfilter",
			frame, got)
	}
	if len(pool.encodeJobs) < wantJobs {
		t.Fatalf("frame %d: VP9 threaded tile jobs = %d, want at least %d",
			frame, len(pool.encodeJobs), wantJobs)
	}
	for i := 0; i < wantJobs; i++ {
		job := &pool.encodeJobs[i]
		if job.err != nil {
			t.Fatalf("frame %d: VP9 threaded tile job %d error = %v",
				frame, i, job.err)
		}
		if job.size <= 0 {
			t.Fatalf("frame %d: VP9 threaded tile job %d wrote %d bytes; threaded tile path was not exercised",
				frame, i, job.size)
		}
	}
}
