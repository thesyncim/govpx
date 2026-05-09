package govpx

import (
	"bytes"
	"errors"
	"runtime"
	"testing"
)

// TestEncoderOptionsThreadsValidation pins the public configuration
// surface for EncoderOptions.Threads. Negative values must be rejected
// (mirrors libvpx's reject path in vp8/encoder/onyx_if.c when
// VP8E_SET_NUMBER_OF_THREADS receives a bogus argument); zero and
// positive values must succeed and be folded onto a non-zero internal
// representation so downstream call sites never have to special-case
// the historical zero default.
func TestEncoderOptionsThreadsValidation(t *testing.T) {
	if _, err := NewVP8Encoder(EncoderOptions{
		Width:             64,
		Height:            64,
		FPS:               30,
		TargetBitrateKbps: 1200,
		Threads:           -1,
	}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("Threads=-1 error = %v, want ErrInvalidConfig", err)
	}

	for _, threads := range []int{0, 1, 2, 4, 8} {
		t.Run("threads_"+itoaSmall(threads), func(t *testing.T) {
			e, err := NewVP8Encoder(EncoderOptions{
				Width:             64,
				Height:            64,
				FPS:               30,
				RateControlMode:   RateControlCBR,
				TargetBitrateKbps: 1200,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				Deadline:          DeadlineRealtime,
				CpuUsed:           8,
				Threads:           threads,
			})
			if err != nil {
				t.Fatalf("NewVP8Encoder Threads=%d returned error: %v", threads, err)
			}
			if e.opts.Threads <= 0 {
				t.Fatalf("normalized Threads=%d, want >=1 (input %d)", e.opts.Threads, threads)
			}
			if eff := e.effectiveThreadCount(); eff < 1 || eff > runtime.NumCPU() {
				t.Fatalf("effectiveThreadCount=%d outside [1,%d]", eff, runtime.NumCPU())
			}
		})
	}
}

// TestEncoderThreadsExceedingMaxIsClamped verifies the validator
// accepts a request larger than the runtime's NumCPU but the runtime
// thread count is clamped against runtime.NumCPU(). Mirrors libvpx's
// vp8cx_create_encoder_threads ceiling against
// cm->processor_core_count.
func TestEncoderThreadsExceedingMaxIsClamped(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             32,
		Height:            32,
		FPS:               30,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: 1200,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		Deadline:          DeadlineRealtime,
		CpuUsed:           8,
		Threads:           maxEncoderThreads + 64,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder Threads=%d returned error: %v", maxEncoderThreads+64, err)
	}
	if e.opts.Threads != maxEncoderThreads {
		t.Fatalf("normalized Threads=%d, want %d", e.opts.Threads, maxEncoderThreads)
	}
	if eff := e.effectiveThreadCount(); eff > runtime.NumCPU() {
		t.Fatalf("effectiveThreadCount=%d > NumCPU=%d", eff, runtime.NumCPU())
	}
}

// TestEncoderThreadsProducesIdenticalBitstream pins the byte-for-byte
// invariant the parity scoreboards depend on at Threads=1: this must
// stay byte-identical to the historical serial macroblock loop forever.
// Threads=0 is normalised to 1 by the option validator so it lands on
// the same path. Threads>=2 may diverge once the row-threaded
// macroblock pipeline lands (libvpx itself produces a different
// bitstream when ethreading is enabled, since the MV predictor's
// last-coded-MV cache and the entropy probabilities update at a
// different cadence under threading); deterministic-at-fixed-N parity
// is checked by TestEncoderThreadsProducesDeterministicAtFixedN below.
func TestEncoderThreadsProducesIdenticalBitstream(t *testing.T) {
	const (
		width  = 64
		height = 48
		frames = 4
	)
	// Threads=0 (validator normalises to 1) and Threads=1 must remain
	// byte-identical to each other and to the canonical Threads=1
	// baseline. This is the regression gate for the zero-cost serial
	// path.
	zeroCostThreadCounts := []int{0, 1}

	makeFrame := func(index int) Image {
		img := testImage(width, height)
		for i := range img.Y {
			img.Y[i] = byte((i*7 + index*13) & 0xFF)
		}
		for i := range img.U {
			img.U[i] = byte(96 + ((i + index*3) & 0x3F))
		}
		for i := range img.V {
			img.V[i] = byte(144 + ((i*2 + index*5) & 0x3F))
		}
		return img
	}

	encode := func(t *testing.T, threads int) [][]byte {
		t.Helper()
		e, err := NewVP8Encoder(EncoderOptions{
			Width:               width,
			Height:              height,
			FPS:                 30,
			RateControlMode:     RateControlCBR,
			TargetBitrateKbps:   1200,
			MinQuantizer:        4,
			MaxQuantizer:        56,
			DropFrameAllowed:    false,
			Deadline:            DeadlineRealtime,
			CpuUsed:             8,
			KeyFrameInterval:    120,
			ErrorResilient:      true,
			BufferSizeMs:        600,
			BufferInitialSizeMs: 400,
			BufferOptimalSizeMs: 500,
			Threads:             threads,
		})
		if err != nil {
			t.Fatalf("NewVP8Encoder Threads=%d returned error: %v", threads, err)
		}
		packets := make([][]byte, 0, frames)
		buf := make([]byte, max(8192, width*height*4))
		for i := range frames {
			res, err := e.EncodeInto(buf, makeFrame(i), uint64(i), 1, 0)
			if err != nil {
				t.Fatalf("EncodeInto Threads=%d frame %d: %v", threads, i, err)
			}
			if res.Dropped {
				t.Fatalf("EncodeInto Threads=%d frame %d unexpectedly dropped", threads, i)
			}
			packets = append(packets, append([]byte(nil), res.Data...))
		}
		return packets
	}

	baseline := encode(t, 1)
	for _, threads := range zeroCostThreadCounts {
		t.Run("threads_"+itoaSmall(threads), func(t *testing.T) {
			got := encode(t, threads)
			if len(got) != len(baseline) {
				t.Fatalf("threads=%d produced %d packets, baseline=%d", threads, len(got), len(baseline))
			}
			for i := range got {
				if !bytes.Equal(got[i], baseline[i]) {
					t.Fatalf("threads=%d frame %d bitstream diverges from Threads=1 baseline (%d vs %d bytes)", threads, i, len(got[i]), len(baseline[i]))
				}
			}
		})
	}
}

// TestEncoderThreadsProducesDeterministicAtFixedN verifies the encoder
// produces a byte-stable bitstream at every fixed Threads value: two
// runs with identical inputs and identical Threads must yield identical
// packets. The bitstream may differ across Threads values (libvpx
// allows this once ethreading turns on), but at any given fixed N the
// encoder must be deterministic. This is the regression gate for the
// row-threaded pipeline once it ships.
func TestEncoderThreadsProducesDeterministicAtFixedN(t *testing.T) {
	const (
		width  = 64
		height = 48
		frames = 4
	)
	threadCounts := []int{1, 2, 4, 8}
	if n := runtime.NumCPU(); n > 8 && n != 1 {
		threadCounts = append(threadCounts, n)
	}

	makeFrame := func(index int) Image {
		img := testImage(width, height)
		for i := range img.Y {
			img.Y[i] = byte((i*11 + index*17) & 0xFF)
		}
		for i := range img.U {
			img.U[i] = byte(112 + ((i + index*5) & 0x3F))
		}
		for i := range img.V {
			img.V[i] = byte(128 + ((i*3 + index*7) & 0x3F))
		}
		return img
	}

	encode := func(t *testing.T, threads int) [][]byte {
		t.Helper()
		e, err := NewVP8Encoder(EncoderOptions{
			Width:               width,
			Height:              height,
			FPS:                 30,
			RateControlMode:     RateControlCBR,
			TargetBitrateKbps:   1200,
			MinQuantizer:        4,
			MaxQuantizer:        56,
			DropFrameAllowed:    false,
			Deadline:            DeadlineRealtime,
			CpuUsed:             8,
			KeyFrameInterval:    120,
			ErrorResilient:      true,
			BufferSizeMs:        600,
			BufferInitialSizeMs: 400,
			BufferOptimalSizeMs: 500,
			Threads:             threads,
		})
		if err != nil {
			t.Fatalf("NewVP8Encoder Threads=%d returned error: %v", threads, err)
		}
		packets := make([][]byte, 0, frames)
		buf := make([]byte, max(8192, width*height*4))
		for i := range frames {
			res, err := e.EncodeInto(buf, makeFrame(i), uint64(i), 1, 0)
			if err != nil {
				t.Fatalf("EncodeInto Threads=%d frame %d: %v", threads, i, err)
			}
			if res.Dropped {
				t.Fatalf("EncodeInto Threads=%d frame %d unexpectedly dropped", threads, i)
			}
			packets = append(packets, append([]byte(nil), res.Data...))
		}
		return packets
	}

	for _, threads := range threadCounts {
		t.Run("threads_"+itoaSmall(threads), func(t *testing.T) {
			runA := encode(t, threads)
			runB := encode(t, threads)
			if len(runA) != len(runB) {
				t.Fatalf("threads=%d run A produced %d packets, run B=%d", threads, len(runA), len(runB))
			}
			for i := range runA {
				if !bytes.Equal(runA[i], runB[i]) {
					t.Fatalf("threads=%d frame %d not deterministic across runs (%d vs %d bytes)", threads, i, len(runA[i]), len(runB[i]))
				}
			}
		})
	}
}

// BenchmarkEncodeIntoThreadingMatrix sweeps Threads={1,2,4,8,NumCPU} on
// a 1280x720 RT CBR cpu_used=8 inter-frame encode so Threads=1
// regressions vs. the historical zero-cost baseline are visible at
// per-commit cadence, and so the row-threaded pipeline (when it lands)
// has a single fixture to demonstrate scaling against. Each sub-bench
// drives a fresh encoder so per-frame state caches do not bleed between
// thread counts.
func BenchmarkEncodeIntoThreadingMatrix(b *testing.B) {
	const (
		width  = 1280
		height = 720
	)
	threadCounts := []int{1, 2, 4, 8}
	if n := runtime.NumCPU(); n > 8 && n != 1 && n != 2 && n != 4 && n != 8 {
		threadCounts = append(threadCounts, n)
	}

	makeFrame := func(index int) Image {
		img := testImage(width, height)
		for i := range img.Y {
			img.Y[i] = byte((i*7 + index*13) & 0xFF)
		}
		for i := range img.U {
			img.U[i] = byte(96 + ((i + index*3) & 0x3F))
		}
		for i := range img.V {
			img.V[i] = byte(144 + ((i*2 + index*5) & 0x3F))
		}
		return img
	}

	for _, threads := range threadCounts {
		b.Run("threads_"+itoaSmall(threads), func(b *testing.B) {
			e, err := NewVP8Encoder(EncoderOptions{
				Width:               width,
				Height:              height,
				FPS:                 30,
				RateControlMode:     RateControlCBR,
				TargetBitrateKbps:   2500,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				DropFrameAllowed:    false,
				Deadline:            DeadlineRealtime,
				CpuUsed:             8,
				KeyFrameInterval:    120,
				BufferSizeMs:        600,
				BufferInitialSizeMs: 400,
				BufferOptimalSizeMs: 500,
				Threads:             threads,
			})
			if err != nil {
				b.Fatalf("NewVP8Encoder Threads=%d returned error: %v", threads, err)
			}
			buf := make([]byte, width*height*4)
			// Prime: encode a key frame so subsequent encodes are inter.
			if _, err := e.EncodeInto(buf, makeFrame(0), 0, 1, 0); err != nil {
				b.Fatalf("prime EncodeInto Threads=%d: %v", threads, err)
			}
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := e.EncodeInto(buf, makeFrame(i+1), uint64(i+1), 1, 0); err != nil {
					b.Fatalf("EncodeInto Threads=%d frame %d: %v", threads, i+1, err)
				}
			}
		})
	}
}

// TestEncoderThreadsRowWorkerPoolGated pins the contract that the
// row-parallel worker pool is allocated only when EncoderOptions.Threads
// >= 2. Threads=1 must leave e.rowWorkers nil so the canonical serial
// hot path performs no atomic ops, no goroutine spawn, and no per-row
// scratch allocation.
func TestEncoderThreadsRowWorkerPoolGated(t *testing.T) {
	cases := []struct {
		threads     int
		wantPoolNil bool
		wantWorkerN int
	}{
		{threads: 1, wantPoolNil: true},
		{threads: 2, wantPoolNil: false, wantWorkerN: 2},
		{threads: 4, wantPoolNil: false, wantWorkerN: 4},
	}
	for _, tc := range cases {
		t.Run("threads_"+itoaSmall(tc.threads), func(t *testing.T) {
			e, err := NewVP8Encoder(EncoderOptions{
				Width:             64,
				Height:            64,
				FPS:               30,
				RateControlMode:   RateControlCBR,
				TargetBitrateKbps: 1200,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				Deadline:          DeadlineRealtime,
				CpuUsed:           8,
				Threads:           tc.threads,
			})
			if err != nil {
				t.Fatalf("NewVP8Encoder Threads=%d: %v", tc.threads, err)
			}
			defer e.Close()
			if tc.wantPoolNil {
				if e.rowWorkers != nil {
					t.Fatalf("Threads=%d: rowWorkers must be nil for the zero-cost serial path", tc.threads)
				}
				return
			}
			if e.rowWorkers == nil {
				t.Fatalf("Threads=%d: rowWorkers must be allocated", tc.threads)
			}
			eff := e.effectiveThreadCount()
			if got := len(e.rowWorkers.workers); got != eff {
				t.Fatalf("Threads=%d: workers=%d, want %d (effective)", tc.threads, got, eff)
			}
			if got := len(e.rowWorkers.rowProgress); got != encoderMacroblockRows(64) {
				t.Fatalf("Threads=%d: rowProgress=%d, want %d", tc.threads, got, encoderMacroblockRows(64))
			}
		})
	}
}

// TestRowWorkerPoolWaveFrontCoordination spot-checks the atomic
// rowProgress wave-front coordinator standalone. publishRowColumn(r,c)
// must release the row r+1 worker waiting at waitForAboveColumn(r+1, c)
// no later than the publisher's store. Race-checked under -race.
func TestRowWorkerPoolWaveFrontCoordination(t *testing.T) {
	const mbRows = 4
	const mbCols = 16
	pool := newRowWorkerPool(mbRows, mbRows, mbCols)
	if pool == nil {
		t.Fatal("newRowWorkerPool returned nil")
	}
	pool.reset(mbRows)
	for r := range mbRows {
		if got := pool.rowProgress[r].Load(); got != -1 {
			t.Fatalf("row %d: rowProgress=%d after reset, want -1", r, got)
		}
	}
	// Drive a serial wave-front: publish row r col c, then verify
	// row r+1 unblocks at col c.
	for c := range mbCols {
		pool.publishRowColumn(0, c)
		pool.waitForAboveColumn(1, c)
		if got := pool.rowProgress[0].Load(); got < int64(c) {
			t.Fatalf("col %d: rowProgress[0]=%d, want >= %d", c, got, c)
		}
	}
	pool.shutdownPool()
}

func itoaSmall(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
