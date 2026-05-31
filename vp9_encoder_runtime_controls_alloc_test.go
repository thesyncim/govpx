package govpx_test

import (
	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9oracle"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"
)

// TestVP9EncoderRuntimeControlsAllocationGate pins the steady-state
// allocation profile of the runtime Set* surface. After warmup, calling each
// covered setter must not allocate. This complements the trace/byte
// parity check by guarding regressions in the runtime control hot path.
func TestVP9EncoderRuntimeControlsAllocationGate(t *testing.T) {
	const width, height = 64, 64

	makeEncoder := func(t *testing.T) *govpx.VP9Encoder {
		t.Helper()
		opts := vp9oracle.CBROptions(width, height, 600)
		e, err := govpx.NewVP9Encoder(opts)
		if err != nil {
			t.Fatalf("NewVP9Encoder: %v", err)
		}
		size, err := vp9oracle.EncodeBufferSize(width, height)
		if err != nil {
			t.Fatalf("EncodeBufferSize: %v", err)
		}
		dst := make([]byte, size)
		img := vp9test.NewPanningYCbCr(width, height, 0)
		if _, err := e.EncodeIntoWithResult(img, dst); err != nil {
			t.Fatalf("EncodeIntoWithResult warm: %v", err)
		}
		return e
	}

	allocCases := []struct {
		name string
		call func(t *testing.T, e *govpx.VP9Encoder)
	}{
		{"SetBitrateKbps", func(t *testing.T, e *govpx.VP9Encoder) {
			if err := e.SetBitrateKbps(700); err != nil {
				t.Fatalf("SetBitrateKbps: %v", err)
			}
		}},
		{"SetRealtimeTargetBitrate", func(t *testing.T, e *govpx.VP9Encoder) {
			if err := e.SetRealtimeTarget(govpx.RealtimeTarget{BitrateKbps: 700}); err != nil {
				t.Fatalf("SetRealtimeTarget bitrate: %v", err)
			}
		}},
		{"SetRealtimeTargetFPS", func(t *testing.T, e *govpx.VP9Encoder) {
			if err := e.SetRealtimeTarget(govpx.RealtimeTarget{FPS: 30}); err != nil {
				t.Fatalf("SetRealtimeTarget FPS: %v", err)
			}
		}},
		{"SetRateControlBuffer", func(t *testing.T, e *govpx.VP9Encoder) {
			if err := e.SetRateControlBuffer(0, 0, 0); err != nil {
				t.Fatalf("SetRateControlBuffer: %v", err)
			}
		}},
		{"SetCQLevel", func(t *testing.T, e *govpx.VP9Encoder) {
			if err := e.SetCQLevel(30); err != nil {
				t.Fatalf("SetCQLevel: %v", err)
			}
		}},
		{"SetCPUUsed", func(t *testing.T, e *govpx.VP9Encoder) {
			if err := e.SetCPUUsed(4); err != nil {
				t.Fatalf("SetCPUUsed: %v", err)
			}
		}},
		{"SetSharpness", func(t *testing.T, e *govpx.VP9Encoder) {
			if err := e.SetSharpness(4); err != nil {
				t.Fatalf("SetSharpness: %v", err)
			}
		}},
		{"SetStaticThreshold", func(t *testing.T, e *govpx.VP9Encoder) {
			if err := e.SetStaticThreshold(200); err != nil {
				t.Fatalf("SetStaticThreshold: %v", err)
			}
		}},
		{"SetMinGFInterval", func(t *testing.T, e *govpx.VP9Encoder) {
			if err := e.SetMinGFInterval(8); err != nil {
				t.Fatalf("SetMinGFInterval: %v", err)
			}
		}},
		{"SetMaxGFInterval", func(t *testing.T, e *govpx.VP9Encoder) {
			if err := e.SetMaxGFInterval(16); err != nil {
				t.Fatalf("SetMaxGFInterval: %v", err)
			}
		}},
		{"SetMaxInterBitratePct", func(t *testing.T, e *govpx.VP9Encoder) {
			if err := e.SetMaxInterBitratePct(200); err != nil {
				t.Fatalf("SetMaxInterBitratePct: %v", err)
			}
		}},
		{"SetMaxIntraBitratePct", func(t *testing.T, e *govpx.VP9Encoder) {
			if err := e.SetMaxIntraBitratePct(200); err != nil {
				t.Fatalf("SetMaxIntraBitratePct: %v", err)
			}
		}},
		{"SetGFCBRBoostPct", func(t *testing.T, e *govpx.VP9Encoder) {
			if err := e.SetGFCBRBoostPct(50); err != nil {
				t.Fatalf("SetGFCBRBoostPct: %v", err)
			}
		}},
		{"SetFramePeriodicBoost", func(t *testing.T, e *govpx.VP9Encoder) {
			if err := e.SetFramePeriodicBoost(true); err != nil {
				t.Fatalf("SetFramePeriodicBoost: %v", err)
			}
		}},
		{"SetAltRefAQ", func(t *testing.T, e *govpx.VP9Encoder) {
			if err := e.SetAltRefAQ(true); err != nil {
				t.Fatalf("SetAltRefAQ: %v", err)
			}
		}},
		{"SetPostEncodeDrop", func(t *testing.T, e *govpx.VP9Encoder) {
			if err := e.SetPostEncodeDrop(true); err != nil {
				t.Fatalf("SetPostEncodeDrop: %v", err)
			}
		}},
		{"SetDisableOvershootMaxQCBR", func(t *testing.T, e *govpx.VP9Encoder) {
			if err := e.SetDisableOvershootMaxQCBR(true); err != nil {
				t.Fatalf("SetDisableOvershootMaxQCBR: %v", err)
			}
		}},
		{"SetNextFrameQIndex", func(t *testing.T, e *govpx.VP9Encoder) {
			if err := e.SetNextFrameQIndex(128); err != nil {
				t.Fatalf("SetNextFrameQIndex: %v", err)
			}
		}},
		{"SetDeltaQUV", func(t *testing.T, e *govpx.VP9Encoder) {
			if err := e.SetDeltaQUV(4); err != nil {
				t.Fatalf("SetDeltaQUV: %v", err)
			}
		}},
		{"SetDisableLoopfilter", func(t *testing.T, e *govpx.VP9Encoder) {
			if err := e.SetDisableLoopfilter(govpx.VP9LoopfilterDisableInter); err != nil {
				t.Fatalf("SetDisableLoopfilter: %v", err)
			}
		}},
		{"SetFrameParallelDecoding", func(t *testing.T, e *govpx.VP9Encoder) {
			if err := e.SetFrameParallelDecoding(false); err != nil {
				t.Fatalf("SetFrameParallelDecoding: %v", err)
			}
		}},
		{"SetRTCExternalRateControl", func(t *testing.T, e *govpx.VP9Encoder) {
			if err := e.SetRTCExternalRateControl(true); err != nil {
				t.Fatalf("SetRTCExternalRateControl: %v", err)
			}
		}},
		{"SetColorSpace", func(t *testing.T, e *govpx.VP9Encoder) {
			if err := e.SetColorSpace(govpx.VP9ColorSpace(4)); err != nil {
				t.Fatalf("SetColorSpace: %v", err)
			}
		}},
		{"SetColorRange", func(t *testing.T, e *govpx.VP9Encoder) {
			if err := e.SetColorRange(govpx.VP9ColorRangeFull); err != nil {
				t.Fatalf("SetColorRange: %v", err)
			}
		}},
	}

	for _, ac := range allocCases {
		ac := ac
		t.Run(ac.name, func(t *testing.T) {
			e := makeEncoder(t)
			ac.call(t, e)
			allocs := testing.AllocsPerRun(50, func() {
				ac.call(t, e)
			})
			if allocs != 0 {
				t.Errorf("steady-state allocations for %s = %v, want 0",
					ac.name, allocs)
			}
		})
	}
}
