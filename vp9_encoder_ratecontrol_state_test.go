package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"testing"
)

func TestVP9RateControlVBRGoldenUsesGFARFCorrectionFactor(t *testing.T) {
	var rc vp9RateControlState
	if err := rc.applyOptions(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		TargetBitrateKbps:  700,
		RateControlModeSet: true,
		RateControlMode:    RateControlVBR,
		MinQuantizer:       4,
		MaxQuantizer:       56,
	}, vp9TimingStateFromOptions(VP9EncoderOptions{FPS: 30})); err != nil {
		t.Fatalf("applyOptions: %v", err)
	}
	const qindex = 96
	macroblocks := encoder.MacroblockCount((64+7)>>3, (64+7)>>3)
	actualBits := encoder.EstimatedBitsAtQ(false, qindex, macroblocks, 1) * 2
	rc.updateRateCorrectionFactor(actualBits, qindex, false,
		1<<vp9GoldenRefSlot, macroblocks, nil, encoder.RateFactorInterNormal)
	if rc.rateCorrectionFactors[encoder.RateFactorGFARFStd] <= 1 {
		t.Fatalf("GF/ARF correction factor = %.3f, want updated above 1",
			rc.rateCorrectionFactors[encoder.RateFactorGFARFStd])
	}
	if rc.rateCorrectionFactors[encoder.RateFactorInterNormal] != 1 {
		t.Fatalf("INTER_NORMAL correction factor = %.3f, want unchanged",
			rc.rateCorrectionFactors[encoder.RateFactorInterNormal])
	}
}

func TestVP9RateControlBoostedRefreshUpdatesLastBoostedQIndex(t *testing.T) {
	rc := vp9RateControlState{lastBoostedQIndex: 40}
	rc.updateQHistory(80, false, 1<<vp9GoldenRefSlot, true)
	if got := rc.lastBoostedQIndex; got != 80 {
		t.Fatalf("last boosted q after golden refresh = %d, want 80", got)
	}
}

func TestVP9RateControlAltRefDisabledLeavesFramesSinceGolden(t *testing.T) {
	tests := []struct {
		name          string
		refreshFlags  uint8
		showFrame     bool
		altRefEnabled bool
		start         uint16
		want          uint16
	}{
		{
			name:         "forced alt refresh with altref disabled leaves counter",
			refreshFlags: 1 << vp9AltRefSlot,
			showFrame:    true,
			start:        3,
			want:         3,
		},
		{
			name:          "alt refresh with altref enabled resets counter",
			refreshFlags:  1 << vp9AltRefSlot,
			showFrame:     true,
			altRefEnabled: true,
			start:         3,
			want:          0,
		},
		{
			name:         "golden refresh resets counter",
			refreshFlags: 1 << vp9GoldenRefSlot,
			showFrame:    true,
			start:        3,
			want:         0,
		},
		{
			name:      "ordinary shown inter increments counter",
			showFrame: true,
			start:     3,
			want:      4,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc := vp9RateControlState{framesSinceGolden: tt.start}
			rc.updateQHistoryWithAltRef(80, false, tt.refreshFlags,
				tt.showFrame, tt.altRefEnabled)
			if got := rc.framesSinceGolden; got != tt.want {
				t.Fatalf("framesSinceGolden = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestVP9EncoderOnePassVBRGoldenRefreshCadence(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		TargetBitrateKbps:   700,
		RateControlModeSet:  true,
		RateControlMode:     RateControlVBR,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		MaxKeyframeInterval: 128,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dst := make([]byte, 65536)
	for frame := 0; frame <= 10; frame++ {
		result, err := e.EncodeIntoWithResult(vp9test.NewYCbCr(width,
			height, uint8(96+frame*3), 128, 128), dst)
		if err != nil {
			t.Fatalf("Encode frame %d: %v", frame, err)
		}
		switch frame {
		case 0:
			if result.RefreshFrameFlags != 0xff {
				t.Fatalf("key refresh flags = %#x, want all refs",
					result.RefreshFrameFlags)
			}
		case 10:
			want := uint8(1<<vp9LastRefSlot | 1<<vp9GoldenRefSlot)
			if result.RefreshFrameFlags != want {
				t.Fatalf("frame 10 refresh flags = %#x, want one-pass GF %#x",
					result.RefreshFrameFlags, want)
			}
			if result.FrameTargetBits <= e.rc.bitsPerFrame {
				t.Fatalf("frame 10 target = %d, want boosted above %d",
					result.FrameTargetBits, e.rc.bitsPerFrame)
			}
		default:
			if result.RefreshFrameFlags != 1<<vp9LastRefSlot {
				t.Fatalf("frame %d refresh flags = %#x, want LAST only",
					frame, result.RefreshFrameFlags)
			}
		}
	}
}

func TestVP9SetRateControlCBRToVBRSeedsGoldenCadence(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   700,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dst := make([]byte, 65536)
	for frame := range 3 {
		if _, err := e.EncodeIntoWithResult(vp9test.NewYCbCr(width,
			height, uint8(96+frame*3), 128, 128), dst); err != nil {
			t.Fatalf("Encode CBR frame %d: %v", frame, err)
		}
	}
	if err := e.SetRateControl(RateControlConfig{
		Mode:                RateControlVBR,
		TargetBitrateKbps:   700,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        6000,
		BufferInitialSizeMs: 4000,
		BufferOptimalSizeMs: 5000,
	}); err != nil {
		t.Fatalf("SetRateControl(VBR): %v", err)
	}
	if e.rc.framesTillGF == 0 {
		t.Fatal("CBR->VBR runtime transition left golden cadence immediately due")
	}
	result, err := e.EncodeIntoWithResult(vp9test.NewYCbCr(width,
		height, 112, 128, 128), dst)
	if err != nil {
		t.Fatalf("Encode post-transition frame: %v", err)
	}
	if result.RefreshFrameFlags != 1<<vp9LastRefSlot {
		t.Fatalf("post-transition refresh flags = %#x, want LAST only",
			result.RefreshFrameFlags)
	}
}

func TestVP9SetRateControlPreservesOnePassGoldenCadence(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     RateControlCQ,
		TargetBitrateKbps:   700,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             20,
		MaxKeyframeInterval: 128,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dst := make([]byte, 65536)
	if _, err := e.EncodeIntoWithResult(vp9test.NewYCbCr(width,
		height, 96, 128, 128), dst); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	wantFramesTillGF := e.rc.framesTillGF
	if wantFramesTillGF == 0 {
		t.Fatal("initial CQ keyframe left golden cadence immediately due")
	}
	if err := e.SetRateControl(RateControlConfig{
		Mode:                RateControlQ,
		TargetBitrateKbps:   700,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             20,
		BufferSizeMs:        6000,
		BufferInitialSizeMs: 4000,
		BufferOptimalSizeMs: 5000,
	}); err != nil {
		t.Fatalf("SetRateControl(Q): %v", err)
	}
	if e.rc.framesTillGF != wantFramesTillGF {
		t.Fatalf("framesTillGF after CQ->Q = %d, want preserved %d",
			e.rc.framesTillGF, wantFramesTillGF)
	}
	result, err := e.EncodeIntoWithResult(vp9test.NewYCbCr(width,
		height, 104, 128, 128), dst)
	if err != nil {
		t.Fatalf("Encode post-transition frame: %v", err)
	}
	if result.RefreshFrameFlags != 1<<vp9LastRefSlot {
		t.Fatalf("post-transition refresh flags = %#x, want LAST only",
			result.RefreshFrameFlags)
	}
}

func TestVP9SetRateControlPreservesLibvpxAdaptiveState(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   700,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		DropFrameAllowed:    true,
		DropFrameWaterMark:  60,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	e.rc.bufferLevelBits = 0
	e.rc.decimationFactor = 2
	e.rc.decimationCount = 1
	e.rc.avgFrameQIndexKey = 41
	e.rc.avgFrameQIndexInter = 47
	e.rc.lastQKey = 33
	e.rc.lastQInter = 55
	e.rc.lastBoostedQIndex = 43
	e.rc.totalActualBits = 123456
	e.rc.totalTargetBits = 654321
	for i := range e.rc.rateCorrectionFactors {
		e.rc.rateCorrectionFactors[i] = 1.25 + float64(i)/4
		e.rc.dampedAdjustment[i] = i&1 == 1
	}
	e.rc.q1Frame = 31
	e.rc.q2Frame = 39
	e.rc.rc1Frame = -7
	e.rc.rc2Frame = 9
	e.rc.framesSinceKey = 77
	e.rc.framesTillGF = 3
	wantFactors := e.rc.rateCorrectionFactors
	wantDamped := e.rc.dampedAdjustment

	if err := e.SetRateControl(RateControlConfig{
		Mode:                RateControlCBR,
		TargetBitrateKbps:   900,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		DropFrameAllowed:    true,
		DropFrameWaterMark:  60,
	}); err != nil {
		t.Fatalf("SetRateControl(CBR): %v", err)
	}
	if e.rc.bufferLevelBits != 0 {
		t.Fatalf("buffer after rate-control change = %d, want preserved zero",
			e.rc.bufferLevelBits)
	}
	if e.rc.decimationFactor != 2 || e.rc.decimationCount != 1 {
		t.Fatalf("decimation state = factor:%d count:%d, want 2/1",
			e.rc.decimationFactor, e.rc.decimationCount)
	}
	if e.rc.avgFrameQIndexKey != 41 || e.rc.avgFrameQIndexInter != 47 ||
		e.rc.lastQKey != 33 || e.rc.lastQInter != 55 ||
		e.rc.lastBoostedQIndex != 43 {
		t.Fatalf("quantizer history = key:%d inter:%d last:%d/%d boosted:%d, want 41/47/33/55/43",
			e.rc.avgFrameQIndexKey, e.rc.avgFrameQIndexInter,
			e.rc.lastQKey, e.rc.lastQInter, e.rc.lastBoostedQIndex)
	}
	if e.rc.totalActualBits != 123456 || e.rc.totalTargetBits != 654321 {
		t.Fatalf("totals = actual:%d target:%d, want 123456/654321",
			e.rc.totalActualBits, e.rc.totalTargetBits)
	}
	if e.rc.rateCorrectionFactors != wantFactors ||
		e.rc.dampedAdjustment != wantDamped {
		t.Fatalf("rate correction state was not preserved")
	}
	if e.rc.q1Frame != 31 || e.rc.q2Frame != 39 ||
		e.rc.rc1Frame != -7 || e.rc.rc2Frame != 9 {
		t.Fatalf("recode history = q:%d/%d rc:%d/%d, want 31/39 -7/9",
			e.rc.q1Frame, e.rc.q2Frame, e.rc.rc1Frame, e.rc.rc2Frame)
	}
	if e.rc.framesSinceKey != 77 || e.rc.framesTillGF != 3 {
		t.Fatalf("frame cadence = since-key:%d till-gf:%d, want 77/3",
			e.rc.framesSinceKey, e.rc.framesTillGF)
	}
}
