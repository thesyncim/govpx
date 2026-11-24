package govpx

import (
	"github.com/thesyncim/govpx/internal/vpx/geometry"
	"testing"
)

func TestEncodeIntoGFCBRBoostRefreshesGoldenOnInterval(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		GFCBRBoostPct:       100,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
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
	const lastBoostSentinel = 149
	e.rc.lastBoost = lastBoostSentinel
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
		state := packetState(t, inter.Data)
		if frame < refreshFrame {
			if state.Refresh.RefreshGolden {
				t.Fatalf("inter %d refresh golden = true, want false before interval", frame)
			}
			if inter.FrameTargetBits != wantTarget {
				t.Fatalf("inter %d target = %d, want libvpx CBR buffer target %d", frame, inter.FrameTargetBits, wantTarget)
			}
			continue
		}
		if !state.Refresh.RefreshGolden {
			t.Fatalf("inter %d refresh golden = false, want true at GF CBR interval", frame)
		}
		if inter.FrameTargetBits != wantTarget {
			t.Fatalf("inter %d target = %d, want boosted libvpx CBR target %d", frame, inter.FrameTargetBits, wantTarget)
		}
		if e.rc.lastBoost != lastBoostSentinel {
			t.Fatalf("inter %d lastBoost = %d, want fixed-CBR GF refresh to preserve %d", frame, e.rc.lastBoost, lastBoostSentinel)
		}
	}
}

func TestEncodeIntoDefaultCBRRefreshesGoldenOnLibvpxInterval(t *testing.T) {
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
		inter, err := e.EncodeInto(dst, publicImageFromVP8(&e.lastRef.Img), uint64(frame), 1, 0)
		if err != nil {
			t.Fatalf("inter %d EncodeInto returned error: %v", frame, err)
		}
		state := packetState(t, inter.Data)
		if frame < refreshFrame && state.Refresh.RefreshGolden {
			t.Fatalf("inter %d refresh golden = true, want false before interval", frame)
		}
		if frame < refreshFrame && state.Refresh.CopyBufferToAltRef != 0 {
			t.Fatalf("inter %d copy-to-alt = %d, want none before GF refresh", frame, state.Refresh.CopyBufferToAltRef)
		}
		if frame == refreshFrame && !state.Refresh.RefreshGolden {
			t.Fatalf("inter %d refresh golden = false, want default libvpx CBR GF refresh", frame)
		}
		if frame == refreshFrame && state.Refresh.CopyBufferToAltRef != 2 {
			t.Fatalf("inter %d copy-to-alt = %d, want libvpx old-GF-to-ARF copy", frame, state.Refresh.CopyBufferToAltRef)
		}
		if inter.FrameTargetBits != wantRC.frameTargetBits {
			t.Fatalf("inter %d target = %d, want unboosted libvpx CBR target %d", frame, inter.FrameTargetBits, wantRC.frameTargetBits)
		}
	}
}

func TestGFCBRBoostRequiresPriorLastZeroMVMajority(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               32,
		Height:              32,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		GFCBRBoostPct:       100,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	rows := geometry.MacroblockRows(e.opts.Height)
	cols := geometry.MacroblockCols(e.opts.Width)
	e.rc.framesTillGFUpdateDue = 0

	e.lastInterZeroMVCount = rows * cols / 2
	if e.shouldRefreshGoldenFrameCBR(false, false, 0, rows, cols) {
		t.Fatalf("shouldRefreshGoldenFrameCBR = true, want false without LAST/ZEROMV majority")
	}
	e.lastInterZeroMVCount = rows*cols/2 + 1
	if !e.shouldRefreshGoldenFrameCBR(false, false, 0, rows, cols) {
		t.Fatalf("shouldRefreshGoldenFrameCBR = false, want true with LAST/ZEROMV majority")
	}
}

func TestGFCBROpportunityUsesLibvpxCountdown(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             64,
		Height:            64,
		FPS:               30,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: 700,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		Deadline:          DeadlineRealtime,
		KeyFrameInterval:  999,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	rows := geometry.MacroblockRows(e.opts.Height)
	cols := geometry.MacroblockCols(e.opts.Width)
	interval := e.goldenFrameCBRInterval(rows, cols)
	e.rc.framesSinceKeyframe = interval - 1
	e.rc.framesTillGFUpdateDue = 0
	e.lastInterZeroMVCount = rows*cols/2 + 1
	if !e.shouldRefreshGoldenFrameCBR(false, false, 0, rows, cols) {
		t.Fatalf("shouldRefreshGoldenFrameCBR = false, want countdown-driven GF opportunity")
	}
}

func TestGoldenFrameCBRIntervalMirrorsLibvpxCyclicRefreshCadence(t *testing.T) {
	e := newSizedTestEncoder(t, 64, 80)

	if got := e.goldenFrameCBRInterval(geometry.MacroblockRows(e.opts.Height), geometry.MacroblockCols(e.opts.Width)); got != 40 {
		t.Fatalf("GF CBR interval = %d, want libvpx cyclic-refresh cadence clamp 40", got)
	}
}

func TestEncodeIntoGFCBRBoostDisabledForErrorResilient(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		GFCBRBoostPct:       100,
		ErrorResilient:      true,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	dst := make([]byte, 8192)
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	if _, err := e.EncodeInto(dst, src, 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	for frame := 1; frame <= 11; frame++ {
		inter, err := e.EncodeInto(dst, publicImageFromVP8(&e.lastRef.Img), uint64(frame), 1, 0)
		if err != nil {
			t.Fatalf("inter %d EncodeInto returned error: %v", frame, err)
		}
		state := packetState(t, inter.Data)
		if state.Refresh.RefreshGolden {
			t.Fatalf("inter %d refresh golden = true, want disabled for error resilient", frame)
		}
	}
}
