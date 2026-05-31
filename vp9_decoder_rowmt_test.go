package govpx_test

import (
	"errors"
	"runtime"
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9DecoderRowMTMatchesSerial(t *testing.T) {
	packet := vp9test.MultiTileStubPacket(t, 1024, 64, 1)

	serial := vp9DecodeLastVisibleFrameWithOptionsForTest(t,
		govpx.VP9DecoderOptions{Threads: 4}, packet)
	rowMT := vp9DecodeLastVisibleFrameWithOptionsForTest(t,
		govpx.VP9DecoderOptions{Threads: 4, DecoderRowMT: true}, packet)
	assertVP9ImagesEqualForTest(t, serial, rowMT)
}

func TestVP9DecoderSetRowMTValidation(t *testing.T) {
	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	defer d.Close()
	if err := d.SetRowMT(true); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Errorf("single-threaded SetRowMT(true) err = %v, want ErrInvalidConfig", err)
	}
	if err := d.SetLoopFilterOpt(true); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Errorf("single-threaded SetLoopFilterOpt(true) err = %v, want ErrInvalidConfig",
			err)
	}
	if err := d.SetRowMT(false); err != nil {
		t.Errorf("SetRowMT(false) on single-threaded decoder err = %v, want nil",
			err)
	}
	if err := d.SetLoopFilterOpt(false); err != nil {
		t.Errorf("SetLoopFilterOpt(false) on single-threaded decoder err = %v, want nil",
			err)
	}

	threaded, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{Threads: 2})
	if err != nil {
		t.Fatalf("threaded NewVP9Decoder: %v", err)
	}
	if err := threaded.SetRowMT(true); err != nil {
		t.Errorf("threaded SetRowMT(true) err = %v, want nil", err)
	}
	if err := threaded.SetLoopFilterOpt(true); err != nil {
		t.Errorf("threaded SetLoopFilterOpt(true) err = %v, want nil", err)
	}
	if err := threaded.SetRowMT(false); err != nil {
		t.Errorf("threaded SetRowMT(false) err = %v, want nil", err)
	}
	if err := threaded.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := threaded.SetRowMT(true); !errors.Is(err, govpx.ErrClosed) {
		t.Errorf("closed SetRowMT err = %v, want ErrClosed", err)
	}
	if err := threaded.SetLoopFilterOpt(true); !errors.Is(err, govpx.ErrClosed) {
		t.Errorf("closed SetLoopFilterOpt err = %v, want ErrClosed", err)
	}
	var nilDecoder *govpx.VP9Decoder
	if err := nilDecoder.SetRowMT(false); !errors.Is(err, govpx.ErrClosed) {
		t.Errorf("nil SetRowMT err = %v, want ErrClosed", err)
	}
	if err := nilDecoder.SetLoopFilterOpt(false); !errors.Is(err, govpx.ErrClosed) {
		t.Errorf("nil SetLoopFilterOpt err = %v, want ErrClosed", err)
	}
}

func TestVP9DecoderRowMTRuntimeToggleMatchesSerial(t *testing.T) {
	packet := vp9test.MultiTileStubPacket(t, 1024, 64, 1)

	want := vp9DecodeLastVisibleFrameWithOptionsForTest(t,
		govpx.VP9DecoderOptions{Threads: 4}, packet)

	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{Threads: 4})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	defer d.Close()

	for i, enabled := range []bool{true, false, true} {
		if err := d.SetRowMT(enabled); err != nil {
			t.Fatalf("iter %d: SetRowMT(%v): %v", i, enabled, err)
		}
		if err := d.Decode(packet); err != nil {
			t.Fatalf("iter %d: Decode: %v", i, err)
		}
		frame, ok := d.NextFrame()
		if !ok {
			t.Fatalf("iter %d: NextFrame returned !ok", i)
		}
		assertVP9ImagesEqualForTest(t, want, frame)
	}
}

func TestVP9DecoderRowMTSteadyStateAlloc(t *testing.T) {
	packet := vp9test.MultiTileStubPacket(t, 1024, 64, 1)

	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{
		Threads: 4, DecoderRowMT: true,
	})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	defer d.Close()
	if err := d.Decode(packet); err != nil {
		t.Fatalf("warm Decode: %v", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRunsForTest, func() {
		err = d.Decode(packet)
	})
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if allocs != 0 {
		t.Fatalf("row-MT decode steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderRowMTNoGoroutineLeak(t *testing.T) {
	packet := vp9test.MultiTileStubPacket(t, 1024, 64, 1)
	baseline := vp9GoroutineCountForTest()

	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{
		Threads: 4, DecoderRowMT: true,
	})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	for range 3 {
		if err := d.Decode(packet); err != nil {
			t.Fatalf("Decode: %v", err)
		}
		if _, ok := d.NextFrame(); !ok {
			t.Fatal("NextFrame returned !ok")
		}
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if got := vp9GoroutineCountForTest(); got > baseline {
		t.Fatalf("goroutines leaked: baseline=%d after-close=%d", baseline, got)
	}
}

func vp9GoroutineCountForTest() int {
	const samples = 8
	last := runtime.NumGoroutine()
	for range samples {
		runtime.Gosched()
		last = runtime.NumGoroutine()
	}
	return last
}
