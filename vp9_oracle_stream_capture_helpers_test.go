//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func captureVP9StreamParityPackets(t *testing.T, opts VP9EncoderOptions,
	sources []*image.YCbCr, flags []EncodeFlags, extraArgs []string,
) ([][]byte, [][]byte) {
	t.Helper()
	return captureVP9StreamParityPacketsWithHooks(t, opts, sources, flags,
		extraArgs, nil)
}

func captureVP9StreamParityPacketsWithHooks(t *testing.T, opts VP9EncoderOptions,
	sources []*image.YCbCr, flags []EncodeFlags, extraArgs []string,
	beforeFrame func(*VP9Encoder, int),
) ([][]byte, [][]byte) {
	t.Helper()
	return captureVP9StreamParityPacketsWithFrameHooks(t, opts, sources,
		flags, extraArgs, beforeFrame, nil)
}

func captureVP9StreamParityPacketsWithFrameHooks(t *testing.T, opts VP9EncoderOptions,
	sources []*image.YCbCr, flags []EncodeFlags, extraArgs []string,
	beforeFrame func(*VP9Encoder, int), afterFrame func(*VP9Encoder, int),
) ([][]byte, [][]byte) {
	t.Helper()
	if len(sources) == 0 {
		t.Fatal("empty VP9 stream parity source")
	}
	if len(flags) > len(sources) {
		t.Fatalf("VP9 stream parity flag count = %d, want <= %d",
			len(flags), len(sources))
	}
	width := sources[0].Rect.Dx()
	height := sources[0].Rect.Dy()
	for i, src := range sources {
		if src.Rect.Dx() != width || src.Rect.Dy() != height {
			t.Fatalf("source %d dimension mismatch: got %dx%d want %dx%d",
				i, src.Rect.Dx(), src.Rect.Dy(), width, height)
		}
	}

	opts.Width = width
	opts.Height = height
	enc, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dstSize, err := vp9AllocatingEncodeBufferSize(width, height)
	if err != nil {
		t.Fatalf("vp9AllocatingEncodeBufferSize: %v", err)
	}
	dst := make([]byte, dstSize)
	govpxPackets := make([][]byte, len(sources))
	for i, src := range sources {
		if beforeFrame != nil {
			beforeFrame(enc, i)
		}
		var f EncodeFlags
		if i < len(flags) {
			f = flags[i]
		}
		if f&EncodeInvisibleFrame != 0 {
			t.Fatalf("frame %d uses EncodeInvisibleFrame, which has no VP9 libvpx flag bit", i)
		}
		result, err := enc.EncodeIntoWithFlagsResult(src, dst, f)
		if err != nil {
			t.Fatalf("EncodeIntoWithFlagsResult frame %d: %v", i, err)
		}
		if result.Dropped {
			t.Fatalf("EncodeIntoWithFlagsResult frame %d unexpectedly dropped", i)
		}
		if afterFrame != nil {
			afterFrame(enc, i)
		}
		govpxPackets[i] = append([]byte(nil), result.Data...)
	}

	libvpxPackets := vp9test.VpxencFrameFlagPackets(t, sources,
		vp9LibvpxFrameFlags(flags), extraArgs...)
	return govpxPackets, libvpxPackets
}

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
	if pool.jobKind != vp9TileWorkerJobEncode {
		t.Fatalf("frame %d: VP9 tile worker job kind = %d, want encode",
			frame, pool.jobKind)
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

func captureVP9StreamParityPacketRowsWithHooks(t *testing.T,
	opts VP9EncoderOptions, sources []*image.YCbCr, flags []EncodeFlags,
	extraArgs []string, beforeFrame func(*VP9Encoder, int),
) ([]vp9test.RateScoreboardRow, [][]byte, []vp9test.RateScoreboardRow, [][]byte) {
	t.Helper()
	govpxRows, govpxPackets := captureGovpxVP9StreamParityPacketRowsWithHooks(t,
		opts, sources, flags, beforeFrame)
	libvpxRows, libvpxPackets := captureLibvpxVP9StreamParityPacketRows(t,
		sources, flags, extraArgs)
	return govpxRows, govpxPackets, libvpxRows, libvpxPackets
}

func captureGovpxVP9StreamParityPacketRowsWithHooks(t *testing.T,
	opts VP9EncoderOptions, sources []*image.YCbCr, flags []EncodeFlags,
	beforeFrame func(*VP9Encoder, int),
) ([]vp9test.RateScoreboardRow, [][]byte) {
	t.Helper()
	if len(sources) == 0 {
		t.Fatal("empty VP9 stream parity source")
	}
	if len(flags) > len(sources) {
		t.Fatalf("VP9 stream parity flag count = %d, want <= %d",
			len(flags), len(sources))
	}
	width := sources[0].Rect.Dx()
	height := sources[0].Rect.Dy()
	for i, src := range sources {
		if src.Rect.Dx() != width || src.Rect.Dy() != height {
			t.Fatalf("source %d dimension mismatch: got %dx%d want %dx%d",
				i, src.Rect.Dx(), src.Rect.Dy(), width, height)
		}
	}
	opts.Width = width
	opts.Height = height
	enc, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	var trace bytes.Buffer
	enc.setVP9OracleTraceWriter(&trace)
	dstSize, err := vp9AllocatingEncodeBufferSize(width, height)
	if err != nil {
		t.Fatalf("vp9AllocatingEncodeBufferSize: %v", err)
	}
	dst := make([]byte, dstSize)
	packets := make([][]byte, len(sources))
	for i, src := range sources {
		if beforeFrame != nil {
			beforeFrame(enc, i)
		}
		var f EncodeFlags
		if i < len(flags) {
			f = flags[i]
		}
		if f&EncodeInvisibleFrame != 0 {
			t.Fatalf("frame %d uses EncodeInvisibleFrame, which has no VP9 libvpx flag bit", i)
		}
		result, err := enc.EncodeIntoWithFlagsResult(src, dst, f)
		if err != nil {
			t.Fatalf("EncodeIntoWithFlagsResult frame %d: %v", i, err)
		}
		if result.Dropped {
			continue
		}
		packets[i] = append([]byte(nil), result.Data...)
	}
	rows := vp9test.ParseRateScoreboardRows(t, trace.Bytes())
	if len(rows) != len(sources) {
		t.Fatalf("govpx VP9 trace rows = %d, want %d", len(rows), len(sources))
	}
	for i := range rows {
		if rows[i].Dropped {
			continue
		}
		if len(packets[i]) == 0 {
			t.Fatalf("govpx VP9 row %d was not dropped but has no packet", i)
		}
		enrichVP9RateScoreboardRowFromPacket(t, &rows[i], packets[i])
	}
	return rows, packets
}

func captureGovpxVP9VariablePacketRows(t *testing.T,
	opts VP9EncoderOptions, sources []*image.YCbCr, flags []EncodeFlags,
	beforeFrame func(*VP9Encoder, int),
) ([]vp9test.RateScoreboardRow, [][]byte) {
	t.Helper()
	if len(sources) == 0 {
		t.Fatal("empty VP9 variable-size stream source")
	}
	if len(flags) > len(sources) {
		t.Fatalf("VP9 variable-size flag count = %d, want <= %d",
			len(flags), len(sources))
	}
	opts.Width = sources[0].Rect.Dx()
	opts.Height = sources[0].Rect.Dy()
	enc, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	var trace bytes.Buffer
	enc.setVP9OracleTraceWriter(&trace)
	packets := make([][]byte, len(sources))
	for i, src := range sources {
		if beforeFrame != nil {
			beforeFrame(enc, i)
		}
		var f EncodeFlags
		if i < len(flags) {
			f = flags[i]
		}
		dstSize, err := vp9AllocatingEncodeBufferSize(src.Rect.Dx(), src.Rect.Dy())
		if err != nil {
			t.Fatalf("vp9AllocatingEncodeBufferSize frame %d: %v", i, err)
		}
		dst := make([]byte, dstSize)
		result, err := enc.EncodeIntoWithFlagsResult(src, dst, f)
		if err != nil {
			t.Fatalf("EncodeIntoWithFlagsResult frame %d: %v", i, err)
		}
		if result.Dropped {
			t.Fatalf("EncodeIntoWithFlagsResult frame %d unexpectedly dropped", i)
		}
		packets[i] = append([]byte(nil), result.Data...)
	}
	rows := vp9test.ParseRateScoreboardRows(t, trace.Bytes())
	if len(rows) != len(sources) {
		t.Fatalf("govpx VP9 variable trace rows = %d, want %d", len(rows), len(sources))
	}
	for i := range rows {
		enrichVP9RateScoreboardRowFromPacket(t, &rows[i], packets[i])
	}
	return rows, packets
}

func captureLibvpxVP9StreamParityPacketRows(t *testing.T,
	sources []*image.YCbCr, flags []EncodeFlags, extraArgs []string,
) ([]vp9test.RateScoreboardRow, [][]byte) {
	t.Helper()
	rows, packets := vp9test.VpxencFrameFlagTracePackets(t, sources,
		vp9LibvpxFrameFlags(flags), extraArgs...)
	for i := range rows {
		if rows[i].Dropped {
			continue
		}
		enrichVP9RateScoreboardRowFromPacket(t, &rows[i], packets[i])
	}
	return rows, packets
}

func captureLibvpxVP9VariablePacketRows(t *testing.T,
	sources []*image.YCbCr, flags []EncodeFlags, invisible []bool,
	extraArgs []string,
) ([]vp9test.RateScoreboardRow, [][]byte) {
	t.Helper()
	rows, packets := vp9test.VpxencVariableFrameFlagTracePackets(t, sources,
		vp9LibvpxFrameFlags(flags), invisible, extraArgs...)
	for i := range rows {
		enrichVP9RateScoreboardRowFromPacket(t, &rows[i], packets[i])
	}
	return rows, packets
}
