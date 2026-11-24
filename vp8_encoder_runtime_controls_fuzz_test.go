package govpx_test

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil"
)

// FuzzVP8EncoderRuntimeControls mirrors FuzzVP9EncoderRuntimeControls on the
// non-oracle VP8 surface. It picks a bounded sequence of runtime Set* method
// invocations from fuzzed bytes and replays them against a live encoder,
// interleaved with EncodeInto calls so each control hits the in-flight path.
// Errors must be returned, not raised, and the encoder must remain usable
// after every rejected control.
//
// This is the NON-oracle variant: it never compares against libvpx, so it
// builds and runs without the govpx_oracle_trace tag. It focuses on
// panic-freedom and consistent-state-after-bad-arg invariants.
func FuzzVP8EncoderRuntimeControls(f *testing.F) {
	seeds := [][]byte{
		{0x00, 0x01, 0x02},
		{0xff, 0x00, 0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80, 0x90, 0xa0, 0xb0, 0xc0},
		{0x10, 0x20, 0x30},
		{0x05, 0xff, 0xff, 0x01, 0x02},
		{0x07, 0x80, 0x00, 0x40, 0x40, 0x06, 0x02},
		// Op-byte sweep: each setter hit once.
		{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22},
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

		const width, height = 64, 64
		e, err := govpx.NewVP8Encoder(govpx.EncoderOptions{
			Width:               width,
			Height:              height,
			FPS:                 30,
			RateControlMode:     govpx.RateControlCBR,
			TargetBitrateKbps:   500,
			MinQuantizer:        4,
			MaxQuantizer:        56,
			Deadline:            govpx.DeadlineRealtime,
			CpuUsed:             8,
			KeyFrameInterval:    120,
			BufferSizeMs:        600,
			BufferInitialSizeMs: 400,
			BufferOptimalSizeMs: 500,
		})
		if err != nil {
			t.Fatalf("NewVP8Encoder: %v", err)
		}

		dst := make([]byte, width*height*4+1024)
		img := newVP8FacadeImage(width, height)
		for i := range img.Y {
			img.Y[i] = byte((i * 7) & 0xFF)
		}
		for i := range img.U {
			img.U[i] = 128
		}
		for i := range img.V {
			img.V[i] = 128
		}

		r := testutil.NewByteCursor(data)
		// Warm the encoder with one encode first so runtime controls hit
		// the in-flight path rather than the cold-start branches.
		if _, err := e.EncodeInto(dst, img, 0, 1, 0); err != nil {
			assertVP8FuzzRuntimeControlError(t, err)
		}
		const maxControls = 24
		for i := range maxControls {
			if r.Remaining() == 0 {
				break
			}
			applyVP8FuzzRuntimeControl(t, e, &r, img)
			if r.Remaining() == 0 {
				break
			}
			if _, err := e.EncodeInto(dst, img, uint64(i+1), 1, 0); err != nil {
				assertVP8FuzzRuntimeControlError(t, err)
			}
		}
	})
}

// applyVP8FuzzRuntimeControl invokes one of the encoder's Set* methods chosen
// by the next fuzz byte. Every method must surface bad arguments as a
// returned error; panics are caught by the f.Fuzz wrapper.
func applyVP8FuzzRuntimeControl(t *testing.T, e *govpx.VP8Encoder, r *testutil.ByteCursor, src govpx.Image) {
	t.Helper()
	const numSetters = 23
	pick := int(r.Next()) % numSetters
	var err error
	switch pick {
	case 0:
		err = e.SetBitrateKbps(int(r.U16LE()))
	case 1:
		err = e.SetRateControl(govpx.RateControlConfig{
			Mode: []govpx.RateControlMode{
				govpx.RateControlCBR,
				govpx.RateControlVBR,
				govpx.RateControlCQ,
				govpx.RateControlQ,
			}[r.Next()%4],
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
		err = e.SetMaxIntraBitratePct(int(r.U16LE() % 2000))
	case 4:
		err = e.SetGFCBRBoostPct(int(r.U16LE() % 2000))
	case 5:
		err = e.SetTokenPartitions(int(r.Next() % 4))
	case 6:
		err = e.SetSharpness(int(r.Next() % 8))
	case 7:
		err = e.SetStaticThreshold(int(r.U16LE() % 1024))
	case 8:
		err = e.SetScreenContentMode(int(r.Next() % 4))
	case 9:
		err = e.SetRTCExternalRateControl(r.Next()&1 == 1)
	case 10:
		err = e.SetFrameDropAllowed(r.Next()&1 == 1)
	case 11:
		err = e.SetDeadline([]govpx.Deadline{
			govpx.DeadlineRealtime,
			govpx.DeadlineGoodQuality,
			govpx.DeadlineBestQuality,
		}[r.Next()%3])
	case 12:
		err = e.SetCPUUsed(int(int8(r.Next()%33)) - 16)
	case 13:
		err = e.SetTuning(govpx.Tuning(r.Next() % 4))
	case 14:
		err = e.SetKeyFrameInterval(int(r.Next()))
	case 15:
		err = e.SetAdaptiveKeyFrames(r.Next()&1 == 1)
	case 16:
		err = e.SetNoiseSensitivity(int(r.Next() % 8))
	case 17:
		err = e.SetARNR(int(r.Next()%17), int(r.Next()%8), int(r.Next()%4))
	case 18:
		// Pick one of the three valid reference selectors so the parity
		// path is reached; bit selector with multiple bits set must be
		// rejected by SetReferenceFrame.
		ref := []govpx.ReferenceFrame{
			govpx.ReferenceLast,
			govpx.ReferenceGolden,
			govpx.ReferenceAltRef,
			govpx.ReferenceFrame(r.Next()),
		}[r.Next()%4]
		err = e.SetReferenceFrame(ref, src)
	case 19:
		err = e.SetRealtimeTarget(govpx.RealtimeTarget{
			BitrateKbps:  int(r.U16LE() % 4000),
			MinQuantizer: int(r.Next() % 64),
			MaxQuantizer: int(r.Next() % 64),
		})
	case 20:
		rows := int(r.Next()%32) + 1
		cols := int(r.Next()%32) + 1
		amap := make([]uint8, rows*cols)
		for i := range amap {
			amap[i] = r.Next() & 1
		}
		err = e.SetActiveMap(amap, rows, cols)
	case 21:
		err = e.SetTemporalScalability(govpx.TemporalScalabilityConfig{
			Enabled:                r.Next()&1 == 1,
			Mode:                   govpx.TemporalLayeringMode(r.Next() % 5),
			LayerTargetBitrateKbps: [govpx.MaxTemporalLayers]int{200, 400, 800, 1200, 1600},
		})
	case 22:
		err = e.SetTemporalLayerID(int(r.Next() % 5))
	}
	if err != nil {
		assertVP8FuzzRuntimeControlError(t, err)
	}
}

// assertVP8FuzzRuntimeControlError pins the set of errors a runtime Set* call
// or a frame encode may return for arbitrary inputs. Anything else means
// the encoder leaked an internal sentinel or panicked in disguise.
func assertVP8FuzzRuntimeControlError(t *testing.T, err error) {
	t.Helper()
	switch {
	case errors.Is(err, govpx.ErrInvalidConfig):
	case errors.Is(err, govpx.ErrInvalidBitrate):
	case errors.Is(err, govpx.ErrInvalidQuantizer):
	case errors.Is(err, govpx.ErrBufferTooSmall):
	case errors.Is(err, govpx.ErrFrameNotReady):
	case errors.Is(err, govpx.ErrClosed):
	default:
		t.Fatalf("VP8 runtime call returned unexpected error: %v", err)
	}
}
