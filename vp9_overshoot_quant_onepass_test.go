package govpx

import (
	"errors"
	"testing"
)

func TestVP9SetDisableOvershootMaxQCBRRejectsWithoutCBR(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		RateControlModeSet: true,
		RateControlMode:    RateControlVBR,
		TargetBitrateKbps:  600,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetDisableOvershootMaxQCBR(true); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetDisableOvershootMaxQCBR(true) on VBR err = %v, want ErrInvalidConfig",
			err)
	}
}

func TestVP9SetDisableOvershootMaxQCBRAppliesInCBR(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  600,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetDisableOvershootMaxQCBR(true); err != nil {
		t.Fatalf("SetDisableOvershootMaxQCBR: %v", err)
	}
	if !e.opts.DisableOvershootMaxQCBR || !e.rc.disableOvershootMaxQCBR {
		t.Fatalf("opts=%v rc=%v, want both true",
			e.opts.DisableOvershootMaxQCBR, e.rc.disableOvershootMaxQCBR)
	}
	ctx := e.vp9PerFrameSpeedContext(vp9PerFrameSpeedContextArgs{})
	if !ctx.disableOvershootMaxqCbr {
		t.Fatal("speed-feature context did not inherit DisableOvershootMaxQCBR")
	}
}

func TestVP9DisableOvershootMaxQCBRSuppressesWorstQClamp(t *testing.T) {
	mkRC := func(disable bool) *vp9RateControlState {
		return &vp9RateControlState{
			enabled:                 true,
			mode:                    RateControlCBR,
			bestQuality:             16,
			worstQuality:            200,
			avgFrameQIndexKey:       100,
			avgFrameQIndexInter:     100,
			bufferOptimalBits:       100000,
			bufferSizeBits:          150000,
			bufferLevelBits:         1000, // below critical (12500)
			disableOvershootMaxQCBR: disable,
		}
	}
	baseline := mkRC(false).cbrActiveWorstQuantizer(false, 10)
	suppressed := mkRC(true).cbrActiveWorstQuantizer(false, 10)
	if baseline != 200 {
		t.Fatalf("baseline active worst = %d, want 200", baseline)
	}
	if suppressed >= baseline {
		t.Fatalf("suppressed active worst = %d, want < %d", suppressed,
			baseline)
	}
}

func TestVP9EncoderRejectsDisableOvershootMaxQCBROutsideCBR(t *testing.T) {
	if _, err := NewVP9Encoder(VP9EncoderOptions{
		Width:                   64,
		Height:                  64,
		FPS:                     30,
		RateControlModeSet:      true,
		RateControlMode:         RateControlVBR,
		TargetBitrateKbps:       600,
		DisableOvershootMaxQCBR: true,
	}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
}

func TestVP9SetNextFrameQIndexRejectsOutOfRange(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:  64,
		Height: 64,
		FPS:    30,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetNextFrameQIndex(-1); !errors.Is(err, ErrInvalidQuantizer) {
		t.Fatalf("SetNextFrameQIndex(-1) err = %v, want ErrInvalidQuantizer",
			err)
	}
	if err := e.SetNextFrameQIndex(256); !errors.Is(err, ErrInvalidQuantizer) {
		t.Fatalf("SetNextFrameQIndex(256) err = %v, want ErrInvalidQuantizer",
			err)
	}
}

func TestVP9SetNextFrameQIndexRejectsWithCyclicRefreshAQ(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  600,
		AQMode:             VP9AQCyclicRefresh,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetNextFrameQIndex(80); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetNextFrameQIndex with cyclic refresh err = %v, want ErrInvalidConfig",
			err)
	}
}

func TestVP9SetNextFrameQIndexRejectsWithPerceptualAQ(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:  64,
		Height: 64,
		FPS:    30,
		AQMode: VP9AQPerceptual,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetNextFrameQIndex(80); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetNextFrameQIndex with perceptual AQ err = %v, want ErrInvalidConfig",
			err)
	}
}

func TestVP9SetNextFrameQIndexAppliesAndConsumes(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:  64,
		Height: 64,
		FPS:    30,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetNextFrameQIndex(123); err != nil {
		t.Fatalf("SetNextFrameQIndex: %v", err)
	}
	if !e.opts.NextFrameQIndexSet || e.opts.NextFrameQIndex != 123 ||
		!e.rc.nextFrameQIndexSet || int(e.rc.nextFrameQIndex) != 123 {
		t.Fatalf("opts(%v,%d) rc(%v,%d), want set/123 across both",
			e.opts.NextFrameQIndexSet, e.opts.NextFrameQIndex,
			e.rc.nextFrameQIndexSet, int(e.rc.nextFrameQIndex))
	}
	qindex := e.vp9EncoderFrameQIndex(false, false, 0, 64)
	if qindex != 123 {
		t.Fatalf("qindex = %d, want 123", qindex)
	}
	// Consumed: next call falls back to public-Q.
	if e.rc.nextFrameQIndexSet || e.opts.NextFrameQIndexSet {
		t.Fatalf("override not cleared: rc=%v opts=%v",
			e.rc.nextFrameQIndexSet, e.opts.NextFrameQIndexSet)
	}
}

func TestVP9NextFrameQIndexAppliedFromOptions(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		NextFrameQIndexSet: true,
		NextFrameQIndex:    77,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if !e.rc.nextFrameQIndexSet || int(e.rc.nextFrameQIndex) != 77 {
		t.Fatalf("rc=(%v,%d), want (true,77)",
			e.rc.nextFrameQIndexSet, int(e.rc.nextFrameQIndex))
	}
}

func TestVP9NextFrameQIndexValidationRejectsSpuriousValue(t *testing.T) {
	// NextFrameQIndex non-zero without NextFrameQIndexSet is invalid.
	if _, err := NewVP9Encoder(VP9EncoderOptions{
		Width:           64,
		Height:          64,
		FPS:             30,
		NextFrameQIndex: 50,
	}); !errors.Is(err, ErrInvalidQuantizer) {
		t.Fatalf("err = %v, want ErrInvalidQuantizer", err)
	}
}
