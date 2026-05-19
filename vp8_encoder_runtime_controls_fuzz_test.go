package govpx

import (
	"encoding/binary"
	"errors"
	"testing"
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
		e, err := NewVP8Encoder(EncoderOptions{
			Width:               width,
			Height:              height,
			FPS:                 30,
			RateControlMode:     RateControlCBR,
			TargetBitrateKbps:   500,
			MinQuantizer:        4,
			MaxQuantizer:        56,
			Deadline:            DeadlineRealtime,
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
		img := testImage(width, height)
		for i := range img.Y {
			img.Y[i] = byte((i * 7) & 0xFF)
		}
		for i := range img.U {
			img.U[i] = 128
		}
		for i := range img.V {
			img.V[i] = 128
		}

		r := vp8FuzzByteReader{data: data}
		// Warm the encoder with one encode first so runtime controls hit
		// the in-flight path rather than the cold-start branches.
		if _, err := e.EncodeInto(dst, img, 0, 1, 0); err != nil {
			assertVP8FuzzRuntimeControlError(t, err)
		}
		const maxControls = 24
		for i := range maxControls {
			if r.remaining() == 0 {
				break
			}
			applyVP8FuzzRuntimeControl(t, e, &r, img)
			if r.remaining() == 0 {
				break
			}
			if _, err := e.EncodeInto(dst, img, uint64(i+1), 1, 0); err != nil {
				assertVP8FuzzRuntimeControlError(t, err)
			}
		}
	})
}

// vp8FuzzByteReader is a small deterministic byte stream consumer used by the
// VP8 runtime-controls fuzz. It treats the input as a circular buffer so very
// short fuzz inputs still exercise every Set* path.
type vp8FuzzByteReader struct {
	data []byte
	pos  int
}

func (r *vp8FuzzByteReader) remaining() int {
	return len(r.data) - r.pos
}

func (r *vp8FuzzByteReader) next() byte {
	if len(r.data) == 0 {
		return 0
	}
	b := r.data[r.pos%len(r.data)]
	r.pos++
	return b
}

func (r *vp8FuzzByteReader) nextU16() uint16 {
	lo := r.next()
	hi := r.next()
	return binary.LittleEndian.Uint16([]byte{lo, hi})
}

// applyVP8FuzzRuntimeControl invokes one of the encoder's Set* methods chosen
// by the next fuzz byte. Every method must surface bad arguments as a
// returned error; panics are caught by the f.Fuzz wrapper.
func applyVP8FuzzRuntimeControl(t *testing.T, e *VP8Encoder, r *vp8FuzzByteReader, src Image) {
	t.Helper()
	const numSetters = 23
	pick := int(r.next()) % numSetters
	var err error
	switch pick {
	case 0:
		err = e.SetBitrateKbps(int(r.nextU16()))
	case 1:
		err = e.SetRateControl(RateControlConfig{
			Mode:                []RateControlMode{RateControlCBR, RateControlVBR, RateControlCQ, RateControlQ}[r.next()%4],
			TargetBitrateKbps:   50 + int(r.nextU16()%3950),
			MinQuantizer:        int(r.next() % 64),
			MaxQuantizer:        int(r.next() % 64),
			CQLevel:             int(r.next() % 64),
			UndershootPct:       int(r.next() % 101),
			OvershootPct:        int(r.next() % 101),
			BufferSizeMs:        100 + int(r.nextU16()%9000),
			BufferInitialSizeMs: 100 + int(r.nextU16()%9000),
			BufferOptimalSizeMs: 100 + int(r.nextU16()%9000),
		})
	case 2:
		err = e.SetCQLevel(int(r.next() % 64))
	case 3:
		err = e.SetMaxIntraBitratePct(int(r.nextU16() % 2000))
	case 4:
		err = e.SetGFCBRBoostPct(int(r.nextU16() % 2000))
	case 5:
		err = e.SetTokenPartitions(int(r.next() % 4))
	case 6:
		err = e.SetSharpness(int(r.next() % 8))
	case 7:
		err = e.SetStaticThreshold(int(r.nextU16() % 1024))
	case 8:
		err = e.SetScreenContentMode(int(r.next() % 4))
	case 9:
		err = e.SetRTCExternalRateControl(r.next()&1 == 1)
	case 10:
		err = e.SetFrameDropAllowed(r.next()&1 == 1)
	case 11:
		err = e.SetDeadline([]Deadline{DeadlineRealtime, DeadlineGoodQuality, DeadlineBestQuality}[r.next()%3])
	case 12:
		err = e.SetCPUUsed(int(int8(r.next()%33)) - 16)
	case 13:
		err = e.SetTuning(Tuning(r.next() % 4))
	case 14:
		err = e.SetKeyFrameInterval(int(r.next()))
	case 15:
		err = e.SetAdaptiveKeyFrames(r.next()&1 == 1)
	case 16:
		err = e.SetNoiseSensitivity(int(r.next() % 8))
	case 17:
		err = e.SetARNR(int(r.next()%17), int(r.next()%8), int(r.next()%4))
	case 18:
		// Pick one of the three valid reference selectors so the parity
		// path is reached; bit selector with multiple bits set must be
		// rejected by SetReferenceFrame.
		ref := []ReferenceFrame{ReferenceLast, ReferenceGolden, ReferenceAltRef,
			ReferenceFrame(r.next())}[r.next()%4]
		err = e.SetReferenceFrame(ref, src)
	case 19:
		err = e.SetRealtimeTarget(RealtimeTarget{
			BitrateKbps:  int(r.nextU16() % 4000),
			MinQuantizer: int(r.next() % 64),
			MaxQuantizer: int(r.next() % 64),
		})
	case 20:
		rows := int(r.next()%32) + 1
		cols := int(r.next()%32) + 1
		amap := make([]uint8, rows*cols)
		for i := range amap {
			amap[i] = r.next() & 1
		}
		err = e.SetActiveMap(amap, rows, cols)
	case 21:
		err = e.SetTemporalScalability(TemporalScalabilityConfig{
			Enabled:                r.next()&1 == 1,
			Mode:                   TemporalLayeringMode(r.next() % 5),
			LayerTargetBitrateKbps: [MaxTemporalLayers]int{200, 400, 800, 1200, 1600},
		})
	case 22:
		err = e.SetTemporalLayerID(int(r.next() % 5))
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
	case errors.Is(err, ErrInvalidConfig):
	case errors.Is(err, ErrInvalidBitrate):
	case errors.Is(err, ErrInvalidQuantizer):
	case errors.Is(err, ErrBufferTooSmall):
	case errors.Is(err, ErrFrameNotReady):
	case errors.Is(err, ErrClosed):
	default:
		t.Fatalf("VP8 runtime call returned unexpected error: %v", err)
	}
}
