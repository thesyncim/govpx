package govpx

import (
	"errors"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vpx/geometry"
	"testing"
)

func TestSetRateControlQAcceptsCQLevelWithoutCQFloor(t *testing.T) {
	e := newTestEncoder(t)
	err := e.SetRateControl(RateControlConfig{
		Mode:                RateControlQ,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             28,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("SetRateControl returned error: %v", err)
	}
	if e.rc.mode != RateControlQ || e.rc.cqLevel != vp8common.PublicQuantizerToQIndex(28) {
		t.Fatalf("Q mode state = mode:%d cq:%d, want RateControlQ / qindex %d", e.rc.mode, e.rc.cqLevel, vp8common.PublicQuantizerToQIndex(28))
	}
	if e.rc.currentQuantizer >= e.rc.cqLevel {
		t.Fatalf("Q current quantizer = %d, want below CQ qindex %d to prove no CQ floor", e.rc.currentQuantizer, e.rc.cqLevel)
	}
}

func TestSetCQLevelRejectedUpdatesPreserveCQState(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCQ,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             24,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	if err := e.SetCQLevel(64); !errors.Is(err, ErrInvalidQuantizer) {
		t.Fatalf("out-of-range SetCQLevel error = %v, want ErrInvalidQuantizer", err)
	}
	if err := e.SetCQLevel(3); !errors.Is(err, ErrInvalidQuantizer) {
		t.Fatalf("below-min SetCQLevel error = %v, want ErrInvalidQuantizer", err)
	}
	if e.rc.cqLevel != vp8common.PublicQuantizerToQIndex(24) {
		t.Fatalf("CQ level after rejected updates = %d, want qindex %d", e.rc.cqLevel, vp8common.PublicQuantizerToQIndex(24))
	}
}

func TestSetCQLevelValidationAppliesToRateControlQ(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlQ,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             24,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	if err := e.SetCQLevel(3); !errors.Is(err, ErrInvalidQuantizer) {
		t.Fatalf("below-min Q SetCQLevel error = %v, want ErrInvalidQuantizer", err)
	}
	if err := e.SetCQLevel(40); err != nil {
		t.Fatalf("Q SetCQLevel returned error: %v", err)
	}
	if e.rc.cqLevel != vp8common.PublicQuantizerToQIndex(40) {
		t.Fatalf("Q cqLevel = %d, want qindex %d", e.rc.cqLevel, vp8common.PublicQuantizerToQIndex(40))
	}
	if e.rc.currentQuantizer >= e.rc.cqLevel {
		t.Fatalf("Q current quantizer = %d, want no reset to CQ qindex %d", e.rc.currentQuantizer, e.rc.cqLevel)
	}
}

func TestSetGFCBRBoostPctAffectsGoldenRefreshTarget(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	if err := e.SetGFCBRBoostPct(50); err != nil {
		t.Fatalf("SetGFCBRBoostPct returned error: %v", err)
	}
	dst := make([]byte, 8192)
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	if _, err := e.EncodeInto(dst, src, 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	rows := geometry.MacroblockRows(e.opts.Height)
	cols := geometry.MacroblockCols(e.opts.Width)
	refreshFrame := e.rc.framesTillGFUpdateDue + 1
	cbrInterval := e.goldenFrameCBRInterval(rows, cols)
	for frame := 1; frame <= refreshFrame; frame++ {
		wantRC := e.rc
		if frame == refreshFrame {
			wantRC.framesTillGFUpdateDue = cbrInterval
			wantRC.currentGFInterval = cbrInterval
		}
		wantRC.beginFrame(false)
		wantTarget := wantRC.frameTargetBits
		if frame == refreshFrame {
			wantTarget = boostedFrameTargetBits(wantTarget, e.rc.gfCBRBoostPct)
		}
		inter, err := e.EncodeInto(dst, publicImageFromVP8(&e.lastRef.Img), uint64(frame), 1, 0)
		if err != nil {
			t.Fatalf("inter %d EncodeInto returned error: %v", frame, err)
		}
		if frame == refreshFrame && inter.FrameTargetBits != wantTarget {
			t.Fatalf("boosted target = %d, want libvpx CBR target %d", inter.FrameTargetBits, wantTarget)
		}
	}
}
