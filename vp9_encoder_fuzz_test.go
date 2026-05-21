package govpx

import (
	"errors"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
)

// FuzzVP9EncoderOptions feeds arbitrary bytes to a deterministic
// VP9EncoderOptions decoder and asserts NewVP9Encoder either returns a usable
// encoder or one of the documented sentinel errors. The harness never accepts
// a panic.
func FuzzVP9EncoderOptions(f *testing.F) {
	seeds := [][]byte{
		nil,
		{},
		{0x00},
		// Plausible 64x64 CBR config.
		{0x00, 0x40, 0x40, 0x1e, 0x00, 0x00, 0x05, 0xdc, 0x04, 0x38, 0x20, 0x00, 0x00, 0x00, 0x00, 0x00},
		// Out-of-range width/height to drive normalize-options errors.
		{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
		// Negative-looking values to push validator paths.
		{0x80, 0x00, 0x80, 0x00, 0x80, 0x00, 0xff, 0xff, 0xff, 0xff},
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("NewVP9Encoder panicked on %d-byte input: %v", len(data), r)
			}
		}()
		opts := vp9EncoderOptionsFromFuzz(data)
		e, err := NewVP9Encoder(opts)
		if err != nil {
			assertVP9FuzzEncoderConstructError(t, err)
			return
		}
		if e == nil {
			t.Fatal("NewVP9Encoder returned nil encoder without error")
		}
		// Single-frame encode to make sure a freshly-constructed
		// encoder does not panic on a valid input. Output is
		// discarded.
		img := vp9test.NewYCbCr(opts.Width, opts.Height, 128, 128, 128)
		size, err := vp9AllocatingEncodeBufferSize(opts.Width, opts.Height)
		if err != nil {
			return
		}
		dst := make([]byte, size)
		if _, err := e.EncodeIntoWithResult(img, dst); err != nil {
			assertVP9FuzzEncoderRuntimeError(t, err)
		}
	})
}

// FuzzVP9EncoderRuntimeControls picks a sequence of runtime Set* mutations
// from fuzzed bytes and replays them against a live encoder. Errors must
// be returned, not raised, and the encoder must remain usable after every
// rejected control.
func FuzzVP9EncoderRuntimeControls(f *testing.F) {
	seeds := [][]byte{
		{0x00, 0x01, 0x02},
		{0xff, 0x00, 0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80, 0x90, 0xa0, 0xb0, 0xc0},
		{0x10, 0x20, 0x30},
		{0x05, 0xff, 0xff, 0x01, 0x02},
		{0x07, 0x80, 0x00, 0x40, 0x40, 0x06, 0x02},
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("runtime-control fuzz panicked on %d-byte input: %v", len(data), r)
			}
		}()

		e, err := NewVP9Encoder(VP9EncoderOptions{
			Width:               64,
			Height:              64,
			FPS:                 30,
			RateControlModeSet:  true,
			RateControlMode:     RateControlCBR,
			TargetBitrateKbps:   500,
			BufferSizeMs:        600,
			BufferInitialSizeMs: 400,
			BufferOptimalSizeMs: 500,
			MinQuantizer:        4,
			MaxQuantizer:        56,
			MaxKeyframeInterval: 128,
		})
		if err != nil {
			t.Fatalf("NewVP9Encoder: %v", err)
		}
		size, err := vp9AllocatingEncodeBufferSize(64, 64)
		if err != nil {
			t.Fatalf("vp9AllocatingEncodeBufferSize: %v", err)
		}
		dst := make([]byte, size)
		img := vp9test.NewYCbCr(64, 64, 96, 128, 128)

		r := testutil.NewByteCursor(data)
		// Encode one frame first to warm the encoder so the runtime
		// controls hit the in-flight path rather than the not-yet-
		// initialised path.
		if _, err := e.EncodeIntoWithResult(img, dst); err != nil {
			assertVP9FuzzEncoderRuntimeError(t, err)
		}
		// Drive a small bounded sequence of runtime controls. We
		// don't want a fuzz input that's effectively unbounded so
		// cap iterations.
		const maxControls = 24
		for range maxControls {
			if r.Remaining() == 0 {
				break
			}
			applyVP9FuzzRuntimeControl(t, e, &r)
			if r.Remaining() == 0 {
				break
			}
			if _, err := e.EncodeIntoWithResult(img, dst); err != nil {
				assertVP9FuzzEncoderRuntimeError(t, err)
			}
		}
	})
}

// vp9EncoderOptionsFromFuzz pulls a structured VP9EncoderOptions value out of
// fuzz bytes. The mapping is intentionally bounded so the validator path is
// reachable for almost every input.
func vp9EncoderOptionsFromFuzz(data []byte) VP9EncoderOptions {
	r := testutil.NewByteCursor(data)
	width := 16 + int(r.Next()%64)*4  // 16..268, multiples of 4
	height := 16 + int(r.Next()%64)*4 // same
	fps := 1 + int(r.Next()%60)       // 1..60
	cpuUsed := int8(r.Next()%19) - 9  // -9..9
	mode := []RateControlMode{
		RateControlCBR, RateControlVBR, RateControlCQ, RateControlQ,
	}[r.Next()%4]
	target := 50 + int(r.U16LE()%3950) // 50..4000
	cq := int(r.Next() % 64)           // 0..63
	minQ := int(r.Next() % 64)
	rem := 64 - minQ
	if rem <= 0 {
		rem = 1
	}
	maxQ := minQ + int(r.Next())%rem
	noise := int8(r.Next() % 7)  // 0..6
	sharp := r.Next() % 8        // 0..7
	threads := int(r.Next() % 8) // 0..7
	tileRows := int8(r.Next() % 4)
	deadline := []Deadline{
		DeadlineRealtime, DeadlineGoodQuality, DeadlineBestQuality,
	}[r.Next()%3]
	colorSpace := VP9ColorSpace(r.Next() % 8)
	colorRange := VP9ColorRange(r.Next() % 2)
	dlf := VP9DisableLoopfilter(r.Next() % 3)
	aq := VP9AQMode(r.Next() % 7)
	return VP9EncoderOptions{
		Width:                    width,
		Height:                   height,
		FPS:                      fps,
		Threads:                  threads,
		Log2TileRows:             tileRows,
		Deadline:                 deadline,
		CpuUsed:                  cpuUsed,
		NoiseSensitivity:         noise,
		Sharpness:                sharp,
		ScreenContentMode:        int8(r.Next() % 3),
		TargetBitrateKbps:        target,
		RateControlModeSet:       true,
		RateControlMode:          mode,
		BufferSizeMs:             600,
		BufferInitialSizeMs:      400,
		BufferOptimalSizeMs:      500,
		MinQuantizer:             minQ,
		MaxQuantizer:             maxQ,
		CQLevel:                  cq,
		MinKeyframeInterval:      int(r.Next() % 32),
		MaxKeyframeInterval:      int(r.Next()%240) + 1,
		AQMode:                   aq,
		ColorSpace:               colorSpace,
		ColorRange:               colorRange,
		DisableLoopfilter:        dlf,
		AdaptiveKeyFrames:        r.Next()&1 == 1,
		ErrorResilient:           r.Next()&1 == 1,
		FrameParallelDecoding:    r.Next()&1 == 1,
		FrameParallelDecodingSet: r.Next()&1 == 1,
		Lossless:                 r.Next()&1 == 1,
		MinBitrateKbps:           int(r.U16LE() % 1000),
		MaxBitrateKbps:           int(r.U16LE() % 5000),
		UndershootPct:            int(r.Next() % 101),
		OvershootPct:             int(r.Next() % 101),
		MaxIntraBitratePct:       int(r.U16LE() % 1000),
		MaxInterBitratePct:       int(r.U16LE() % 1000),
		DeltaQUV:                 int(int8(r.Next())%16) - 8,
	}
}

// applyVP9FuzzRuntimeControl invokes one of the encoder's Set* methods chosen
// by the next fuzz byte. Every method must surface bad arguments as a
// returned error; panics are caught by the f.Fuzz wrapper.
func applyVP9FuzzRuntimeControl(t *testing.T, e *VP9Encoder, r *testutil.ByteCursor) {
	t.Helper()
	const numSetters = 40
	pick := int(r.Next()) % numSetters
	var err error
	switch pick {
	case 0:
		err = e.SetBitrateKbps(50 + int(r.U16LE()%3950))
	case 1:
		err = e.SetRateControl(RateControlConfig{
			Mode:                []RateControlMode{RateControlCBR, RateControlVBR, RateControlCQ, RateControlQ}[r.Next()%4],
			TargetBitrateKbps:   50 + int(r.U16LE()%3950),
			MinQuantizer:        int(r.Next() % 64),
			MaxQuantizer:        int(r.Next() % 64),
			CQLevel:             int(r.Next() % 64),
			UndershootPct:       int(r.Next() % 101),
			OvershootPct:        int(r.Next() % 101),
			BufferSizeMs:        100 + int(r.U16LE()%9000),
			BufferInitialSizeMs: 100 + int(r.U16LE()%9000),
			BufferOptimalSizeMs: 100 + int(r.U16LE()%9000),
		})
	case 2:
		err = e.SetCQLevel(int(r.Next() % 64))
	case 3:
		err = e.SetAQMode(VP9AQMode(r.Next() % 8))
	case 4:
		err = e.SetLossless(r.Next()&1 == 1)
	case 5:
		err = e.SetFrameParallelDecoding(r.Next()&1 == 1)
	case 6:
		rows := int(r.Next()%32) + 1
		cols := int(r.Next()%32) + 1
		amap := make([]uint8, rows*cols)
		for i := range amap {
			amap[i] = r.Next() & 1
		}
		err = e.SetActiveMap(amap, rows, cols)
	case 7:
		err = e.SetDeadline([]Deadline{DeadlineRealtime, DeadlineGoodQuality, DeadlineBestQuality}[r.Next()%3])
	case 8:
		err = e.SetCPUUsed(int(int8(r.Next()%19)) - 9)
	case 9:
		err = e.SetTuning(Tuning(r.Next() % 4))
	case 10:
		err = e.SetRowMT(r.Next()&1 == 1)
	case 11:
		err = e.SetScreenContentMode(int(r.Next() % 4))
	case 12:
		err = e.SetNoiseSensitivity(int(r.Next() % 8))
	case 13:
		err = e.SetSharpness(r.Next() % 8)
	case 14:
		err = e.SetStaticThreshold(int(r.U16LE() % 1024))
	case 15:
		err = e.SetKeyFrameInterval(int(r.Next()))
	case 16:
		minF := int(r.Next() % 32)
		maxF := minF + int(r.Next()%240)
		err = e.SetKeyFrameIntervalRange(minF, maxF)
	case 17:
		err = e.SetAdaptiveKeyFrames(r.Next()&1 == 1)
	case 18:
		err = e.SetRTCExternalRateControl(r.Next()&1 == 1)
	case 19:
		err = e.SetColorSpace(VP9ColorSpace(r.Next() % 8))
	case 20:
		err = e.SetColorRange(VP9ColorRange(r.Next() % 2))
	case 21:
		err = e.SetRenderSize(int(r.U16LE()%2048)+1, int(r.U16LE()%2048)+1)
	case 22:
		levels := []int{255, 0, 10, 11, 20, 21, 30, 31, 40, 41, 50, 51, 52, 60, 61, 62, 99}
		err = e.SetTargetLevel(levels[int(r.Next())%len(levels)])
	case 23:
		err = e.SetDisableLoopfilter(VP9DisableLoopfilter(r.Next() % 4))
	case 24:
		err = e.SetDeltaQUV(int(int8(r.Next())%32) - 16)
	case 25:
		err = e.SetMaxInterBitratePct(int(r.U16LE() % 2000))
	case 26:
		err = e.SetMinGFInterval(int(r.Next()))
	case 27:
		err = e.SetMaxGFInterval(int(r.Next()))
	case 28:
		err = e.SetFramePeriodicBoost(r.Next()&1 == 1)
	case 29:
		err = e.SetAltRefAQ(r.Next()&1 == 1)
	case 30:
		err = e.SetPostEncodeDrop(r.Next()&1 == 1)
	case 31:
		err = e.SetDisableOvershootMaxQCBR(r.Next()&1 == 1)
	case 32:
		err = e.SetNextFrameQIndex(int(r.Next()))
	case 33:
		err = e.SetFrameDropAllowed(r.Next()&1 == 1)
	case 34:
		err = e.SetRateControlBuffer(int(r.U16LE()%9000)+100, int(r.U16LE()%9000)+100, int(r.U16LE()%9000)+100)
	case 35:
		err = e.SetARNR(int(r.Next()%17), int(r.Next()%8), int(r.Next()%4))
	case 36:
		err = e.SetRealtimeTarget(RealtimeTarget{
			BitrateKbps:  int(r.U16LE() % 4000),
			MinQuantizer: int(r.Next() % 64),
			MaxQuantizer: int(r.Next() % 64),
		})
	case 37:
		layers := int(r.Next()%4) + 1
		err = e.SetTemporalScalability(TemporalScalabilityConfig{
			Enabled:                layers > 1,
			Mode:                   TemporalLayeringMode(r.Next() % 5),
			LayerTargetBitrateKbps: [MaxTemporalLayers]int{200, 400, 800, 1200, 1600},
		})
		_ = layers
	case 38:
		err = e.SetMaxIntraBitratePct(int(r.U16LE() % 2000))
	case 39:
		err = e.SetGFCBRBoostPct(int(r.U16LE() % 2000))
	}
	if err != nil {
		assertVP9FuzzEncoderRuntimeError(t, err)
	}
}

// assertVP9FuzzEncoderConstructError pins the set of errors NewVP9Encoder may
// return for arbitrary inputs.
func assertVP9FuzzEncoderConstructError(t *testing.T, err error) {
	t.Helper()
	switch {
	case errors.Is(err, ErrInvalidConfig):
	case errors.Is(err, ErrInvalidBitrate):
	case errors.Is(err, ErrInvalidQuantizer):
	default:
		t.Fatalf("NewVP9Encoder returned unexpected error: %v", err)
	}
}

// assertVP9FuzzEncoderRuntimeError pins the set of errors a runtime Set* call
// (or a frame encode) may return for arbitrary inputs. Anything else is a
// regression because the encoder must surface every bad runtime argument as
// a typed error, not a panic or sentinel-leak.
func assertVP9FuzzEncoderRuntimeError(t *testing.T, err error) {
	t.Helper()
	switch {
	case errors.Is(err, ErrInvalidConfig):
	case errors.Is(err, ErrInvalidBitrate):
	case errors.Is(err, ErrInvalidQuantizer):
	case errors.Is(err, ErrBufferTooSmall):
	case errors.Is(err, ErrFrameNotReady):
	case errors.Is(err, ErrClosed):
	default:
		t.Fatalf("VP9 runtime call returned unexpected error: %v", err)
	}
}
