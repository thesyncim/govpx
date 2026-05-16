package govpx

import (
	"image"
	"runtime"
	"testing"
	"time"
)

// TestVP9WorkerPoolNoGoroutineLeakOnClose spawns several VP9 encoders and
// decoders configured with Threads > 1 (so each instance arms the tile
// worker pool and decoder helper pools), immediately tears them down via
// Close(), and asserts the goroutine count returns to the pre-spawn
// baseline. Regression cover for the worker-pool leak that surfaced
// when callers (BD-rate harness, bench helpers) constructed encoders and
// dropped them on the floor without calling Close(), pinning helper
// goroutines for the lifetime of the test process and slowing every
// subsequent test.
func TestVP9WorkerPoolNoGoroutineLeakOnClose(t *testing.T) {
	t.Parallel()

	// Drive the runtime to settle: any deferred GC sweeps or finalizer
	// goroutines spawned by prior tests should drain before we snapshot.
	runtime.GC()
	settleGoroutineCount(t)
	baseline := runtime.NumGoroutine()

	const iterations = 8
	for i := range iterations {
		// Plain tile-threaded encoder.
		enc, err := NewVP9Encoder(VP9EncoderOptions{
			Width:   128,
			Height:  64,
			FPS:     30,
			Threads: 4,
		})
		if err != nil {
			t.Fatalf("NewVP9Encoder[%d]: %v", i, err)
		}
		// Push one frame so the tile worker pool gets engaged at least once
		// before shutdown, mirroring the real-world dispatch ordering.
		img := image.NewYCbCr(image.Rect(0, 0, 128, 64),
			image.YCbCrSubsampleRatio420)
		if _, err := enc.Encode(img); err != nil {
			enc.Close()
			t.Fatalf("Encode[%d]: %v", i, err)
		}
		if err := enc.Close(); err != nil {
			t.Fatalf("Close[%d]: %v", i, err)
		}
		// Double-Close is allowed and must not leak / panic.
		if err := enc.Close(); err != ErrClosed {
			t.Fatalf("double Close[%d] err = %v, want ErrClosed", i, err)
		}

		// RowMT-threaded encoder so the per-tile-column row worker pools
		// also have to be torn down by Close.
		rowEnc, err := NewVP9Encoder(VP9EncoderOptions{
			Width:   128,
			Height:  64,
			FPS:     30,
			Threads: 4,
			RowMT:   true,
		})
		if err != nil {
			t.Fatalf("NewVP9Encoder(rowMT)[%d]: %v", i, err)
		}
		if _, err := rowEnc.Encode(img); err != nil {
			rowEnc.Close()
			t.Fatalf("rowMT Encode[%d]: %v", i, err)
		}
		if err := rowEnc.Close(); err != nil {
			t.Fatalf("rowMT Close[%d]: %v", i, err)
		}

		dec, err := NewVP9Decoder(VP9DecoderOptions{Threads: 4})
		if err != nil {
			t.Fatalf("NewVP9Decoder[%d]: %v", i, err)
		}
		if err := dec.Close(); err != nil {
			t.Fatalf("Decoder Close[%d]: %v", i, err)
		}
		if err := dec.Close(); err != ErrClosed {
			t.Fatalf("double decoder Close[%d] err = %v, want ErrClosed", i, err)
		}
	}

	// Give the runtime a moment to reap exited goroutines.
	settleGoroutineCount(t)
	final := runtime.NumGoroutine()

	// Allow a small slack for test-runtime scheduler chatter; the leak we
	// regressed against was hundreds of goroutines per iteration so even
	// a generous slack catches it.
	const slack = 8
	if final > baseline+slack {
		t.Fatalf("goroutine leak: baseline=%d final=%d (slack=%d)",
			baseline, final, slack)
	}
}

// settleGoroutineCount waits up to a few hundred milliseconds for the
// goroutine count to stop changing so spurious scheduler chatter does
// not flake the leak check.
func settleGoroutineCount(t *testing.T) {
	t.Helper()
	prev := runtime.NumGoroutine()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		cur := runtime.NumGoroutine()
		if cur == prev {
			return
		}
		prev = cur
	}
}
