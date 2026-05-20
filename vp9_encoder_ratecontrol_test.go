package govpx

import (
	"errors"
	"reflect"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

func TestVP9EncoderExplicitRateControlModesEncode(t *testing.T) {
	const width, height = 64, 64
	const targetKbps = 300
	const wantBitsPerFrame = targetKbps * 1000 / 30
	cases := []struct {
		name    string
		mode    RateControlMode
		cqLevel int
	}{
		{name: "vbr", mode: RateControlVBR},
		{name: "cq", mode: RateControlCQ, cqLevel: 20},
		{name: "q", mode: RateControlQ, cqLevel: 20},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, err := NewVP9Encoder(VP9EncoderOptions{
				Width:              width,
				Height:             height,
				FPS:                30,
				TargetBitrateKbps:  targetKbps,
				RateControlModeSet: true,
				RateControlMode:    tc.mode,
				CQLevel:            tc.cqLevel,
			})
			if err != nil {
				t.Fatalf("NewVP9Encoder: %v", err)
			}
			if !e.rc.enabled || e.rc.mode != tc.mode {
				t.Fatalf("rate control state = enabled:%t mode:%d, want true/%d",
					e.rc.enabled, e.rc.mode, tc.mode)
			}
			if e.rc.dropFrameAllowed || e.rc.dropFramesWaterMark != 0 {
				t.Fatalf("non-CBR drop state = allowed:%t watermark:%d, want disabled",
					e.rc.dropFrameAllowed, e.rc.dropFramesWaterMark)
			}

			dst := make([]byte, 65536)
			dec, err := NewVP9Decoder(VP9DecoderOptions{})
			if err != nil {
				t.Fatalf("NewVP9Decoder: %v", err)
			}
			for frame := range 2 {
				src := newVP9YCbCrForTest(width, height,
					uint8(96+frame*20), 128, 128)
				result, err := e.EncodeIntoWithResult(src, dst)
				if err != nil {
					t.Fatalf("EncodeIntoWithResult frame %d: %v", frame, err)
				}
				if result.Dropped || len(result.Data) == 0 {
					t.Fatalf("frame %d result = dropped:%t bytes:%d, want packet",
						frame, result.Dropped, len(result.Data))
				}
				wantFrameTargetBits := wantBitsPerFrame
				if frame == 0 {
					wantFrameTargetBits = e.rc.onePassVBRKeyFrameTargetBits()
				} else {
					wantFrameTargetBits = e.rc.onePassVBRInterFrameTargetBits(
						1 << vp9LastRefSlot)
				}
				if result.TargetBitrateKbps != targetKbps ||
					result.FrameTargetBits != wantFrameTargetBits {
					t.Fatalf("frame %d rate = kbps:%d target:%d, want %d/%d",
						frame, result.TargetBitrateKbps, result.FrameTargetBits,
						targetKbps, wantFrameTargetBits)
				}
				if frame == 0 && !result.KeyFrame {
					t.Fatal("first explicit rate-control packet is not a keyframe")
				}
				if frame == 1 && result.KeyFrame {
					t.Fatal("second explicit rate-control packet unexpectedly keyframed")
				}
				if err := dec.Decode(result.Data); err != nil {
					t.Fatalf("Decode frame %d: %v", frame, err)
				}
				if _, ok := dec.NextFrame(); !ok {
					t.Fatalf("NextFrame frame %d returned !ok", frame)
				}
			}
		})
	}
}

func TestVP9EncoderExplicitVBRUsesOnePassRateQuantizer(t *testing.T) {
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
	if e.rc.avgFrameQIndexKey != 120 || e.rc.avgFrameQIndexInter != 120 {
		t.Fatalf("initial VBR average q = key:%d inter:%d, want midpoint 120/120",
			e.rc.avgFrameQIndexKey, e.rc.avgFrameQIndexInter)
	}
	dst := make([]byte, 65536)
	key, err := e.EncodeIntoWithResult(newVP9YCbCrForTest(width, height,
		96, 128, 128), dst)
	if err != nil {
		t.Fatalf("Encode key: %v", err)
	}
	if key.InternalQuantizer >= vp9DefaultBaseQIndex {
		t.Fatalf("VBR key qindex = %d, want below public-Q key qindex %d",
			key.InternalQuantizer, vp9DefaultBaseQIndex)
	}
	if key.FrameTargetBits != e.rc.onePassVBRKeyFrameTargetBits() {
		t.Fatalf("VBR key target = %d, want one-pass target %d",
			key.FrameTargetBits, e.rc.onePassVBRKeyFrameTargetBits())
	}
	inter, err := e.EncodeIntoWithResult(newVP9YCbCrForTest(width, height,
		116, 128, 128), dst)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	if inter.InternalQuantizer == vp9DefaultInterBaseQIndex {
		t.Fatalf("VBR inter qindex = %d, still public-Q inter default",
			inter.InternalQuantizer)
	}
}

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
		1<<vp9GoldenRefSlot, macroblocks)
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
		result, err := e.EncodeIntoWithResult(newVP9YCbCrForTest(width,
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
		if _, err := e.EncodeIntoWithResult(newVP9YCbCrForTest(width,
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
	result, err := e.EncodeIntoWithResult(newVP9YCbCrForTest(width,
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
	if _, err := e.EncodeIntoWithResult(newVP9YCbCrForTest(width,
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
	result, err := e.EncodeIntoWithResult(newVP9YCbCrForTest(width,
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

func TestVP9EncoderSetRealtimeTargetUpdatesExplicitVBR(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		TargetBitrateKbps:  300,
		RateControlModeSet: true,
		RateControlMode:    RateControlVBR,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{BitrateKbps: 600, FPS: 60}); err != nil {
		t.Fatalf("SetRealtimeTarget: %v", err)
	}
	if e.opts.TargetBitrateKbps != 600 || e.rc.targetBitrateKbps != 600 ||
		e.rc.bitsPerFrame != 10000 {
		t.Fatalf("rate state after target = opts:%d rc:%d bits:%d, want 600/600/10000",
			e.opts.TargetBitrateKbps, e.rc.targetBitrateKbps, e.rc.bitsPerFrame)
	}
}

func TestVP9EncoderSetRealtimeTargetUpdatesHints(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width:             64,
		Height:            64,
		FPS:               30,
		TargetBitrateKbps: 900,
	})

	if err := e.SetRealtimeTarget(RealtimeTarget{
		BitrateKbps: 1500,
		FPS:         60,
	}); err != nil {
		t.Fatalf("SetRealtimeTarget: %v", err)
	}
	if e.opts.TargetBitrateKbps != 1500 ||
		e.opts.FPS != 60 ||
		e.opts.TimebaseNum != 1 ||
		e.opts.TimebaseDen != 60 {
		t.Fatalf("opts after target = %+v, want bitrate 1500 and 1/60 timebase",
			e.opts)
	}
}

func TestVP9EncoderSetRealtimeTargetResizeForcesKeyFrame(t *testing.T) {
	const (
		w1 = 64
		h1 = 64
		w2 = 96
		h2 = 80
	)
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: w1, Height: h1})
	if _, err := e.Encode(newVP9YCbCrForTest(w1, h1, 72, 128, 128)); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	inter, err := e.Encode(newVP9YCbCrForTest(w1, h1, 92, 128, 128))
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	if h, _ := parseVP9EncoderHeaderForTest(t, inter); h.FrameType != common.InterFrame {
		t.Fatalf("pre-resize frame type = %d, want inter", h.FrameType)
	}
	if !e.refValid[vp9LastRefSlot] || !e.refFrames[vp9LastRefSlot].valid {
		t.Fatal("LAST reference not valid before resize")
	}

	if err := e.SetRealtimeTarget(RealtimeTarget{Width: w2, Height: h2}); err != nil {
		t.Fatalf("SetRealtimeTarget resize: %v", err)
	}
	if e.opts.Width != w2 || e.opts.Height != h2 {
		t.Fatalf("dims after resize = %dx%d, want %dx%d",
			e.opts.Width, e.opts.Height, w2, h2)
	}
	if !e.IsKeyFrameNext() || !e.forceKeyFrame {
		t.Fatal("resize did not force the next VP9 frame to keyframe")
	}
	for slot := range e.refValid {
		if e.refValid[slot] || e.refFrames[slot].valid {
			t.Fatalf("reference slot %d still valid after resize", slot)
		}
	}
	if _, err := e.Encode(newVP9YCbCrForTest(w1, h1, 100, 128, 128)); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("old-size Encode after resize err = %v, want ErrInvalidConfig", err)
	}

	resized, err := e.Encode(newVP9YCbCrForTest(w2, h2, 111, 123, 211))
	if err != nil {
		t.Fatalf("Encode resized keyframe: %v", err)
	}
	h, _ := parseVP9EncoderHeaderForTest(t, resized)
	if h.FrameType != common.KeyFrame || h.Width != w2 || h.Height != h2 {
		t.Fatalf("resized header = type:%d %dx%d, want key %dx%d",
			h.FrameType, h.Width, h.Height, w2, h2)
	}
	if e.forceKeyFrame {
		t.Fatal("forceKeyFrame still set after resized keyframe")
	}
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(resized); err != nil {
		t.Fatalf("Decode resized keyframe: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after resized keyframe")
	}
	assertVP9FilledFrameWithin(t, frame, w2, h2, 111, 123, 211, 1)
}

func TestVP9EncoderSetRealtimeTargetValidationNoMutation(t *testing.T) {
	const width, height = 64, 64
	cases := []struct {
		name   string
		target RealtimeTarget
		want   error
	}{
		{"negative bitrate", RealtimeTarget{BitrateKbps: -1}, ErrInvalidConfig},
		{"one dimension", RealtimeTarget{Width: 96}, ErrInvalidConfig},
		{"too wide", RealtimeTarget{Width: maxVP9Dimension + 1, Height: 64}, ErrInvalidConfig},
		{"explicit frame drop", RealtimeTarget{FrameDrop: RealtimeFrameDropEnabled}, ErrInvalidConfig},
		{"bad quantizer range", RealtimeTarget{MinQuantizer: 50, MaxQuantizer: 20}, ErrInvalidQuantizer},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
			if err := e.SetRealtimeTarget(tc.target); !errors.Is(err, tc.want) {
				t.Fatalf("SetRealtimeTarget error = %v, want %v", err, tc.want)
			}
			if e.opts.Width != width || e.opts.Height != height ||
				e.forceKeyFrame || e.opts.TargetBitrateKbps != 0 {
				t.Fatalf("encoder mutated after reject: opts=%+v forceKeyFrame=%t",
					e.opts, e.forceKeyFrame)
			}
			packet, err := e.Encode(newVP9YCbCrForTest(width, height, 80, 128, 128))
			if err != nil {
				t.Fatalf("Encode after rejected target: %v", err)
			}
			info, err := PeekVP9StreamInfo(packet)
			if err != nil {
				t.Fatalf("PeekVP9StreamInfo: %v", err)
			}
			if !info.KeyFrame || info.Width != width || info.Height != height {
				t.Fatalf("info after rejected target = %+v, want original keyframe", info)
			}
		})
	}
}

func TestVP9EncoderSetRealtimeTargetUpdatesQuantizerBounds(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{MinQuantizer: 20, MaxQuantizer: 20}); err != nil {
		t.Fatalf("SetRealtimeTarget: %v", err)
	}
	if e.opts.MinQuantizer != 20 || e.opts.MaxQuantizer != 20 {
		t.Fatalf("quantizer bounds = %d/%d, want 20/20",
			e.opts.MinQuantizer, e.opts.MaxQuantizer)
	}
	packet, err := e.Encode(newVP9YCbCrForTest(width, height, 80, 128, 128))
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	h, _ := parseVP9EncoderHeaderForTest(t, packet)
	if got, want := int(h.Quant.BaseQindex), vp9PublicQuantizerToQIndex(20); got != want {
		t.Fatalf("BaseQindex = %d, want %d", got, want)
	}
}

func TestVP9EncoderCBRDropBufferUnderrunReturnsDropped(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              width,
		Height:             height,
		FPS:                30,
		TargetBitrateKbps:  1,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		DropFrameAllowed:   true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	src := newVP9YCbCrForTest(width, height, 128, 128, 128)
	dst := make([]byte, 65536)
	key, err := e.EncodeIntoWithResult(src, dst)
	if err != nil {
		t.Fatalf("key EncodeIntoWithResult: %v", err)
	}
	if !key.KeyFrame || key.Dropped || len(key.Data) == 0 {
		t.Fatalf("key result = key:%t dropped:%t data:%d, want encoded keyframe",
			key.KeyFrame, key.Dropped, len(key.Data))
	}

	e.rc.bufferLevelBits = -e.rc.bitsPerFrame - 1
	drainedBuffer := e.rc.bufferLevelBits
	wantBufferAfterRefill := drainedBuffer + e.rc.bitsPerFrame
	inter, err := e.EncodeIntoWithResult(src, dst)
	if err != nil {
		t.Fatalf("inter EncodeIntoWithResult: %v", err)
	}
	if !inter.Dropped || inter.KeyFrame || len(inter.Data) != 0 || inter.SizeBytes != 0 {
		t.Fatalf("inter result = key:%t dropped:%t size:%d data:%d, want dropped inter",
			inter.KeyFrame, inter.Dropped, inter.SizeBytes, len(inter.Data))
	}
	if inter.TargetBitrateKbps != 1 || inter.FrameTargetBits != encoder.FrameOverhead {
		t.Fatalf("inter rate = kbps:%d target:%d, want 1/%d",
			inter.TargetBitrateKbps, inter.FrameTargetBits, encoder.FrameOverhead)
	}
	if inter.BufferLevelBits != wantBufferAfterRefill {
		t.Fatalf("buffer after drop = %d, want %d",
			inter.BufferLevelBits, wantBufferAfterRefill)
	}
}

func TestVP9EncoderCBRSelectsLibvpxQuantizers(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		TargetBitrateKbps:   700,
		RateControlModeSet:  true,
		RateControlMode:     RateControlCBR,
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
	dst := make([]byte, 65536)
	wantQ := [...]int{16, 145, 145, 162}
	for i, want := range wantQ {
		src := newVP9YCbCrForTest(width, height, uint8(96+i*11), 128, 128)
		result, err := e.EncodeIntoWithResult(src, dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", i, err)
		}
		if result.InternalQuantizer != want {
			t.Fatalf("frame %d internal quantizer = %d, want %d",
				i, result.InternalQuantizer, want)
		}
	}
}

// TestVP9EncoderCBRFrameTargetMatchesLibvpx asserts the one-pass CBR
// frame-target formula matches libvpx vp9_calc_iframe_target_size_one_pass_cbr
// (kf_boost ramp uses starting_buffer_level/2 on the very first frame) and
// the inter-frame per-frame bandwidth target on subsequent inter frames.
// Prior to the kf_boost port the keyframe target was hard-coded to the
// per-frame bandwidth, which produced a slightly higher base qindex than the
// libvpx CLI on small frames (libvpx: vp9_ratectrl.c:2205-2232).
func TestVP9EncoderCBRFrameTargetMatchesLibvpx(t *testing.T) {
	const width, height = 64, 64
	const fps = 30
	const bufferInitialSizeMs = 400
	for _, targetKbps := range [...]int{700, 140} {
		e, err := NewVP9Encoder(VP9EncoderOptions{
			Width:               width,
			Height:              height,
			FPS:                 fps,
			TargetBitrateKbps:   targetKbps,
			RateControlModeSet:  true,
			RateControlMode:     RateControlCBR,
			BufferSizeMs:        600,
			BufferInitialSizeMs: bufferInitialSizeMs,
			BufferOptimalSizeMs: 500,
			MinQuantizer:        4,
			MaxQuantizer:        56,
			MaxKeyframeInterval: 128,
		})
		if err != nil {
			t.Fatalf("NewVP9Encoder target %d: %v", targetKbps, err)
		}
		dst := make([]byte, 65536)
		wantKeyTarget := targetKbps * bufferInitialSizeMs / 2
		for i := range 3 {
			want := 0
			if i == 0 {
				// libvpx: vp9_calc_iframe_target_size_one_pass_cbr returns
				// starting_buffer_level/2 on the very first video frame.
				want = wantKeyTarget
			} else {
				want = e.rc.onePassCBRInterFrameTargetBits(
					e.vp9InterRefreshFrameFlags(0))
			}
			src := newVP9YCbCrForTest(width, height, uint8(96+i*11), 128, 128)
			result, err := e.EncodeIntoWithResult(src, dst)
			if err != nil {
				t.Fatalf("EncodeIntoWithResult target %d frame %d: %v",
					targetKbps, i, err)
			}
			if result.FrameTargetBits != want {
				t.Fatalf("target %d frame %d target bits = %d, want %d",
					targetKbps, i, result.FrameTargetBits, want)
			}
		}
	}
}

func TestVP9EncoderCBRDropDoesNotDropKeyOrInvisibleFrame(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              width,
		Height:             height,
		FPS:                30,
		TargetBitrateKbps:  1,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		DropFrameAllowed:   true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	src := newVP9YCbCrForTest(width, height, 128, 128, 128)
	dst := make([]byte, 65536)

	e.rc.bufferLevelBits = -1
	key, err := e.EncodeIntoWithResult(src, dst)
	if err != nil {
		t.Fatalf("key EncodeIntoWithResult: %v", err)
	}
	if !key.KeyFrame || key.Dropped || len(key.Data) == 0 {
		t.Fatalf("key result = key:%t dropped:%t data:%d, want encoded keyframe",
			key.KeyFrame, key.Dropped, len(key.Data))
	}

	e.rc.bufferLevelBits = -1
	hidden, err := e.EncodeIntoWithFlagsResult(src, dst, EncodeInvisibleFrame)
	if err != nil {
		t.Fatalf("hidden EncodeIntoWithFlagsResult: %v", err)
	}
	if hidden.Dropped || hidden.KeyFrame || hidden.ShowFrame || len(hidden.Data) == 0 {
		t.Fatalf("hidden result = key:%t show:%t dropped:%t data:%d, want encoded hidden inter",
			hidden.KeyFrame, hidden.ShowFrame, hidden.Dropped, len(hidden.Data))
	}
}

func TestVP9RateControlDropWatermarkDecimation(t *testing.T) {
	rc := vp9RateControlState{
		enabled:             true,
		mode:                RateControlCBR,
		dropFrameAllowed:    true,
		dropFramesWaterMark: 60,
		bufferOptimalBits:   10000,
		bufferSizeBits:      12000,
		bitsPerFrame:        1000,
	}

	rc.bufferLevelBits = 6000
	reason, drop := rc.testDropInterFrame()
	if drop || reason != vp9DropNone || rc.decimationFactor != 1 || rc.decimationCount != 1 {
		t.Fatalf("first watermark check = reason:%d drop:%t factor:%d count:%d, want arm only",
			reason, drop, rc.decimationFactor, rc.decimationCount)
	}
	reason, drop = rc.testDropInterFrame()
	if !drop || reason != vp9DropWatermarkDecimation || rc.decimationFactor != 1 || rc.decimationCount != 0 {
		t.Fatalf("second watermark check = reason:%d drop:%t factor:%d count:%d, want decimation drop",
			reason, drop, rc.decimationFactor, rc.decimationCount)
	}
	reason, drop = rc.testDropInterFrame()
	if drop || reason != vp9DropNone || rc.decimationFactor != 1 || rc.decimationCount != 1 {
		t.Fatalf("third watermark check = reason:%d drop:%t factor:%d count:%d, want re-arm",
			reason, drop, rc.decimationFactor, rc.decimationCount)
	}

	rc.bufferLevelBits = 7000
	reason, drop = rc.testDropInterFrame()
	if drop || reason != vp9DropNone || rc.decimationFactor != 0 || rc.decimationCount != 0 {
		t.Fatalf("recovered watermark check = reason:%d drop:%t factor:%d count:%d, want reset",
			reason, drop, rc.decimationFactor, rc.decimationCount)
	}
}

func TestVP9RateControlDropNegativeBufferBypassesWatermark(t *testing.T) {
	rc := vp9RateControlState{
		enabled:             true,
		mode:                RateControlCBR,
		dropFrameAllowed:    true,
		dropFramesWaterMark: 60,
		bufferOptimalBits:   10000,
		decimationFactor:    1,
		decimationCount:     1,
		bufferLevelBits:     -1,
	}

	reason, drop := rc.testDropInterFrame()
	if !drop || reason != vp9DropNegativeBuffer {
		t.Fatalf("negative buffer drop = reason:%d drop:%t, want negative-buffer drop",
			reason, drop)
	}
	if rc.decimationFactor != 1 || rc.decimationCount != 1 {
		t.Fatalf("negative buffer changed decimation = factor:%d count:%d, want unchanged 1/1",
			rc.decimationFactor, rc.decimationCount)
	}
}

func TestVP9RateControlPreEncodeRefillPrecedesDropGate(t *testing.T) {
	rc := vp9RateControlState{
		enabled:             true,
		mode:                RateControlCBR,
		dropFrameAllowed:    true,
		dropFramesWaterMark: 60,
		bufferOptimalBits:   10000,
		bufferSizeBits:      12000,
		bitsPerFrame:        1000,
		bufferLevelBits:     -1,
	}

	rc.preEncodeFrame(true)
	reason, drop := rc.testDropInterFrame()
	if drop || reason != vp9DropNone || rc.bufferLevelBits != 999 ||
		rc.decimationFactor != 1 || rc.decimationCount != 1 {
		t.Fatalf("first drop gate = reason:%d drop:%t buffer:%d factor:%d count:%d, want pre-refill arm only",
			reason, drop, rc.bufferLevelBits, rc.decimationFactor,
			rc.decimationCount)
	}
	rc.preEncodeFrame(true)
	reason, drop = rc.testDropInterFrame()
	if !drop || reason != vp9DropWatermarkDecimation ||
		rc.bufferLevelBits != 1999 || rc.decimationFactor != 1 ||
		rc.decimationCount != 0 {
		t.Fatalf("second drop gate = reason:%d drop:%t buffer:%d factor:%d count:%d, want watermark decimation",
			reason, drop, rc.bufferLevelBits, rc.decimationFactor,
			rc.decimationCount)
	}
}

func TestVP9EncoderSetRealtimeTargetFrameDropMode(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		TargetBitrateKbps:  300,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		DropFrameAllowed:   true,
		DropFrameWaterMark: 75,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{BitrateKbps: 600}); err != nil {
		t.Fatalf("bitrate SetRealtimeTarget: %v", err)
	}
	if !e.rc.dropFrameAllowed || !e.opts.DropFrameAllowed {
		t.Fatal("bitrate-only SetRealtimeTarget disabled frame dropping")
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{FrameDrop: RealtimeFrameDropDisabled}); err != nil {
		t.Fatalf("disable FrameDrop: %v", err)
	}
	if e.rc.dropFrameAllowed || e.opts.DropFrameAllowed {
		t.Fatal("FrameDrop disabled did not clear VP9 drop toggle")
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{FrameDrop: RealtimeFrameDropEnabled}); err != nil {
		t.Fatalf("enable FrameDrop: %v", err)
	}
	if !e.rc.dropFrameAllowed || !e.opts.DropFrameAllowed ||
		e.rc.dropFramesWaterMark != 75 {
		t.Fatalf("drop state = allowed:%t opts:%t mark:%d, want true/true/75",
			e.rc.dropFrameAllowed, e.opts.DropFrameAllowed, e.rc.dropFramesWaterMark)
	}
}

func TestVP9EncoderSetBitrateKbpsUpdatesRateControl(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:               64,
		Height:              64,
		FPS:                 30,
		TargetBitrateKbps:   300,
		RateControlModeSet:  true,
		RateControlMode:     RateControlCBR,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		TemporalScalability: TemporalScalabilityConfig{
			Enabled: true,
			Mode:    TemporalLayeringTwoLayers,
		},
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	e.rc.bufferLevelBits = maxInt()
	if err := e.SetBitrateKbps(900); err != nil {
		t.Fatalf("SetBitrateKbps: %v", err)
	}
	if e.opts.TargetBitrateKbps != 900 || e.rc.targetBitrateKbps != 900 ||
		e.rc.targetBandwidthBits != 900000 || e.rc.bitsPerFrame != 30000 {
		t.Fatalf("bitrate state = opts:%d rc:%d bandwidth:%d bpf:%d, want 900/900/900000/30000",
			e.opts.TargetBitrateKbps, e.rc.targetBitrateKbps,
			e.rc.targetBandwidthBits, e.rc.bitsPerFrame)
	}
	if e.rc.bufferSizeBits != 540000 || e.rc.bufferInitialBits != 360000 ||
		e.rc.bufferOptimalBits != 450000 || e.rc.bufferLevelBits != 540000 {
		t.Fatalf("buffer bits = size:%d initial:%d optimal:%d level:%d, want 540000/360000/450000/540000",
			e.rc.bufferSizeBits, e.rc.bufferInitialBits,
			e.rc.bufferOptimalBits, e.rc.bufferLevelBits)
	}
	if got := e.opts.TemporalScalability.LayerTargetBitrateKbps; got[0] != 540 || got[1] != 900 {
		t.Fatalf("temporal bitrates after SetBitrateKbps = %v, want [540 900 ...]", got)
	}

	oldRC := e.rc
	oldOpts := e.opts
	oldTemporal := e.temporal
	oldTwoPass := e.twoPass
	if err := e.SetBitrateKbps(0); !errors.Is(err, ErrInvalidBitrate) {
		t.Fatalf("invalid SetBitrateKbps err = %v, want ErrInvalidBitrate", err)
	}
	if e.rc != oldRC || !reflect.DeepEqual(e.opts, oldOpts) ||
		e.temporal != oldTemporal || !reflect.DeepEqual(e.twoPass, oldTwoPass) {
		t.Fatal("invalid SetBitrateKbps mutated encoder state")
	}

	publicQ, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	if err != nil {
		t.Fatalf("NewVP9Encoder public-Q: %v", err)
	}
	if err := publicQ.SetBitrateKbps(900); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("public-Q SetBitrateKbps err = %v, want ErrInvalidConfig", err)
	}
}

func TestVP9EncoderSetCQLevelUpdatesPublicQAndRateControl(t *testing.T) {
	publicQ, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	if err != nil {
		t.Fatalf("NewVP9Encoder public-Q: %v", err)
	}
	beforeQ := publicQ.vp9EncoderPublicQModeQIndex(false, false, 1<<vp9LastRefSlot)
	if err := publicQ.SetCQLevel(20); err != nil {
		t.Fatalf("public-Q SetCQLevel: %v", err)
	}
	afterQ := publicQ.vp9EncoderPublicQModeQIndex(false, false, 1<<vp9LastRefSlot)
	if publicQ.opts.CQLevel != 20 || afterQ == beforeQ {
		t.Fatalf("public-Q CQ update = opts:%d q:%d before:%d, want changed level 20",
			publicQ.opts.CQLevel, afterQ, beforeQ)
	}

	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		TargetBitrateKbps:  700,
		RateControlModeSet: true,
		RateControlMode:    RateControlQ,
		MinQuantizer:       4,
		MaxQuantizer:       56,
		CQLevel:            20,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder RC-Q: %v", err)
	}
	if err := e.SetCQLevel(24); err != nil {
		t.Fatalf("RC-Q SetCQLevel: %v", err)
	}
	if e.opts.CQLevel != 24 ||
		e.rc.cqLevel != uint8(vp9PublicQuantizerToQIndex(24)) {
		t.Fatalf("RC-Q CQ update = opts:%d rc:%d, want 24/%d",
			e.opts.CQLevel, e.rc.cqLevel, vp9PublicQuantizerToQIndex(24))
	}

	oldRC := e.rc
	oldOpts := e.opts
	if err := e.SetCQLevel(3); !errors.Is(err, ErrInvalidQuantizer) {
		t.Fatalf("below-min SetCQLevel err = %v, want ErrInvalidQuantizer", err)
	}
	if e.rc != oldRC || !reflect.DeepEqual(e.opts, oldOpts) {
		t.Fatal("invalid SetCQLevel mutated encoder state")
	}
	if err := e.SetCQLevel(0); err != nil {
		t.Fatalf("reset SetCQLevel: %v", err)
	}
	if e.opts.CQLevel != 0 ||
		e.rc.cqLevel != uint8(vp9PublicQuantizerToQIndex(vp9DefaultCQLevel)) {
		t.Fatalf("reset CQ state = opts:%d rc:%d, want 0/%d",
			e.opts.CQLevel, e.rc.cqLevel,
			vp9PublicQuantizerToQIndex(vp9DefaultCQLevel))
	}
}

func TestVP9EncoderSetAQModeSwitchesModeAtomically(t *testing.T) {
	const width, height = 64, 64
	// Use a CBR rate-control config so variance-AQ stays wired —
	// the AQ path is suppressed under pure-Q / fixed-Q because the
	// rate controller cannot absorb the per-segment qindex swings.
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              width,
		Height:             height,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  500,
		MinQuantizer:       4,
		MaxQuantizer:       56,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder public-Q: %v", err)
	}
	if err := e.SetAQMode(VP9AQVariance); err != nil {
		t.Fatalf("SetAQMode variance: %v", err)
	}
	if e.opts.AQMode != VP9AQVariance || e.cyclicAQ.Enabled {
		t.Fatalf("variance AQ state = mode:%d cyclic:%t, want variance/false",
			e.opts.AQMode, e.cyclicAQ.Enabled)
	}
	packet, err := e.Encode(newVP9YCbCrForTest(width, height, 128, 128, 128))
	if err != nil {
		t.Fatalf("Encode variance AQ key: %v", err)
	}
	header, _ := parseVP9EncoderHeaderForTest(t, packet)
	if !header.Seg.Enabled || !header.Seg.UpdateMap || !header.Seg.UpdateData {
		t.Fatalf("runtime variance AQ segmentation = enabled:%t updateMap:%t updateData:%t, want true/true/true",
			header.Seg.Enabled, header.Seg.UpdateMap, header.Seg.UpdateData)
	}

	oldOpts := e.opts
	oldCyclic := e.cyclicAQ
	if err := e.SetAQMode(VP9AQNone); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("post-start SetAQMode none err = %v, want ErrInvalidConfig", err)
	}
	if !reflect.DeepEqual(e.opts, oldOpts) ||
		!reflect.DeepEqual(e.cyclicAQ, oldCyclic) {
		t.Fatal("post-start SetAQMode mutated encoder state")
	}

	cbr, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              width,
		Height:             height,
		FPS:                30,
		TargetBitrateKbps:  300,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		MinQuantizer:       4,
		MaxQuantizer:       56,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder CBR: %v", err)
	}
	if err := cbr.SetAQMode(VP9AQCyclicRefresh); err != nil {
		t.Fatalf("SetAQMode cyclic refresh: %v", err)
	}
	if cbr.opts.AQMode != VP9AQCyclicRefresh || !cbr.cyclicAQ.Enabled ||
		len(cbr.cyclicAQ.SegMap) != 64 {
		t.Fatalf("cyclic AQ state = mode:%d enabled:%t map:%d, want cyclic/true/64",
			cbr.opts.AQMode, cbr.cyclicAQ.Enabled, len(cbr.cyclicAQ.SegMap))
	}
	disabled, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              width,
		Height:             height,
		FPS:                30,
		TargetBitrateKbps:  300,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		MinQuantizer:       4,
		MaxQuantizer:       56,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder disabled CBR: %v", err)
	}
	if err := disabled.SetAQMode(VP9AQCyclicRefresh); err != nil {
		t.Fatalf("disabled SetAQMode cyclic refresh: %v", err)
	}
	if err := disabled.SetAQMode(VP9AQNone); err != nil {
		t.Fatalf("disabled SetAQMode none: %v", err)
	}
	if disabled.opts.AQMode != VP9AQNone || disabled.cyclicAQ.Enabled ||
		disabled.cyclicAQ.MIRows != 0 || disabled.cyclicAQ.MICols != 0 {
		t.Fatalf("pre-start disabled AQ state = mode:%d enabled:%t rows:%d cols:%d, want none/false/0/0",
			disabled.opts.AQMode, disabled.cyclicAQ.Enabled,
			disabled.cyclicAQ.MIRows, disabled.cyclicAQ.MICols)
	}
	invalidComplexity, err := NewVP9Encoder(VP9EncoderOptions{
		Width:        width,
		Height:       height,
		MinQuantizer: 20,
		MaxQuantizer: 20,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder invalid complexity: %v", err)
	}
	if err := invalidComplexity.SetAQMode(VP9AQComplexity); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("invalid SetAQMode complexity err = %v, want ErrInvalidConfig", err)
	}
	if invalidComplexity.opts.AQMode != VP9AQNone {
		t.Fatal("invalid SetAQMode complexity mutated encoder state")
	}
	dst := make([]byte, 65536)
	keyN, err := cbr.EncodeInto(newVP9YCbCrForTest(width, height, 96, 128, 128), dst)
	if err != nil {
		t.Fatalf("Encode cyclic AQ key: %v", err)
	}
	keyPacket := append([]byte(nil), dst[:keyN]...)
	interN, err := cbr.EncodeInto(newVP9YCbCrForTest(width, height, 116, 128, 128), dst)
	if err != nil {
		t.Fatalf("Encode cyclic AQ inter: %v", err)
	}
	interPacket := append([]byte(nil), dst[:interN]...)
	keyHeader, _ := parseVP9EncoderHeaderForTest(t, keyPacket)
	var br vp9dec.BitReader
	br.Init(interPacket)
	interHeader, err := vp9dec.ReadUncompressedHeader(&br, &keyHeader,
		func(uint8) (uint32, uint32) { return width, height })
	if err != nil {
		t.Fatalf("ReadUncompressedHeader cyclic AQ inter: %v", err)
	}
	if !interHeader.Seg.Enabled || !interHeader.Seg.UpdateMap ||
		!interHeader.Seg.UpdateData {
		t.Fatalf("runtime cyclic AQ segmentation = enabled:%t updateMap:%t updateData:%t, want true/true/true",
			interHeader.Seg.Enabled, interHeader.Seg.UpdateMap,
			interHeader.Seg.UpdateData)
	}
	if err := disabled.SetAQMode(VP9AQMode(99)); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("invalid SetAQMode enum err = %v, want ErrInvalidConfig", err)
	}
	if disabled.opts.AQMode != VP9AQNone || disabled.cyclicAQ.Enabled {
		t.Fatal("invalid SetAQMode enum mutated encoder state")
	}
}

func TestVP9EncoderSetLossless(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetLossless(true); err != nil {
		t.Fatalf("SetLossless(true): %v", err)
	}
	if !e.opts.Lossless {
		t.Fatal("SetLossless(true) did not update encoder options")
	}
	src := newVP9CheckerYCbCrForTest(64, 64, 0, 255, 80, 192)
	packet, err := e.Encode(src)
	if err != nil {
		t.Fatalf("lossless Encode: %v", err)
	}
	h, _ := parseVP9EncoderHeaderForTest(t, packet)
	if h.Quant.BaseQindex != 0 || !h.Quant.Lossless {
		t.Fatalf("lossless header q/lossless = %d/%v, want 0/true",
			h.Quant.BaseQindex, h.Quant.Lossless)
	}

	if err := e.SetLossless(false); err != nil {
		t.Fatalf("SetLossless(false): %v", err)
	}
	e.ForceKeyFrame()
	packet, err = e.Encode(src)
	if err != nil {
		t.Fatalf("non-lossless Encode: %v", err)
	}
	h, _ = parseVP9EncoderHeaderForTest(t, packet)
	if h.Quant.Lossless {
		t.Fatal("SetLossless(false) left lossless header enabled")
	}

	invalid, err := NewVP9Encoder(VP9EncoderOptions{
		Width:     64,
		Height:    64,
		Quantizer: 1,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder invalid toggle fixture: %v", err)
	}
	before := invalid.opts
	if err := invalid.SetLossless(true); !errors.Is(err, ErrInvalidQuantizer) {
		t.Fatalf("SetLossless invalid err = %v, want ErrInvalidQuantizer", err)
	}
	if !reflect.DeepEqual(invalid.opts, before) {
		t.Fatal("invalid SetLossless mutated encoder options")
	}
}

func TestVP9EncoderSetRateControlSwitchesModeAtomically(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetRateControl(RateControlConfig{
		Mode:                RateControlCQ,
		TargetBitrateKbps:   700,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             20,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	}); err != nil {
		t.Fatalf("SetRateControl(CQ): %v", err)
	}
	if !e.opts.RateControlModeSet || e.opts.RateControlMode != RateControlCQ ||
		!e.rc.enabled || e.rc.mode != RateControlCQ ||
		e.opts.TargetBitrateKbps != 700 || e.rc.bitsPerFrame != 23333 ||
		e.rc.cqLevel != uint8(vp9PublicQuantizerToQIndex(20)) {
		t.Fatalf("CQ rate control state = opts:%+v rc:%+v, want enabled CQ 700kbps cq20",
			e.opts, e.rc)
	}
	dst := make([]byte, 65536)
	result, err := e.EncodeIntoWithResult(
		newVP9YCbCrForTest(64, 64, 96, 128, 128), dst)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult after SetRateControl: %v", err)
	}
	if result.TargetBitrateKbps != 700 || result.Dropped || len(result.Data) == 0 {
		t.Fatalf("post-SetRateControl result = kbps:%d dropped:%t bytes:%d, want 700 encoded",
			result.TargetBitrateKbps, result.Dropped, len(result.Data))
	}

	oldRC := e.rc
	oldOpts := e.opts
	oldTwoPass := e.twoPass
	if err := e.SetRateControl(RateControlConfig{
		Mode:              RateControlVBR,
		TargetBitrateKbps: 700,
		DropFrameAllowed:  true,
	}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("invalid drop SetRateControl err = %v, want ErrInvalidConfig", err)
	}
	if e.rc != oldRC || !reflect.DeepEqual(e.opts, oldOpts) ||
		!reflect.DeepEqual(e.twoPass, oldTwoPass) {
		t.Fatal("invalid SetRateControl mutated encoder state")
	}
	if err := e.SetRateControl(RateControlConfig{
		Mode:              RateControlVBR,
		TargetBitrateKbps: 700,
		MinBitrateKbps:    900,
	}); !errors.Is(err, ErrInvalidBitrate) {
		t.Fatalf("min>target SetRateControl err = %v, want ErrInvalidBitrate", err)
	}
	if e.rc != oldRC || !reflect.DeepEqual(e.opts, oldOpts) ||
		!reflect.DeepEqual(e.twoPass, oldTwoPass) {
		t.Fatal("invalid-min SetRateControl mutated encoder state")
	}
	if err := e.SetRateControl(RateControlConfig{
		Mode:              RateControlVBR,
		TargetBitrateKbps: 700,
		UndershootPct:     500,
	}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("out-of-range undershoot SetRateControl err = %v, want ErrInvalidConfig", err)
	}
	if e.rc != oldRC || !reflect.DeepEqual(e.opts, oldOpts) ||
		!reflect.DeepEqual(e.twoPass, oldTwoPass) {
		t.Fatal("invalid-undershoot SetRateControl mutated encoder state")
	}
}

func TestVP9EncoderSetRateControlRebuildsTwoPassPlan(t *testing.T) {
	stats := finalizedVP9TwoPassTestStats(100, 200)
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		TargetBitrateKbps:  300,
		RateControlModeSet: true,
		RateControlMode:    RateControlVBR,
		TwoPassStats:       stats,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetRateControl(RateControlConfig{
		Mode:              RateControlVBR,
		TargetBitrateKbps: 600,
	}); err != nil {
		t.Fatalf("SetRateControl two-pass VBR: %v", err)
	}
	if !e.twoPass.enabled() || e.twoPass.bitsLeft != 40000 ||
		e.twoPass.frameIndex != 0 || e.rc.bitsPerFrame != 20000 {
		t.Fatalf("two-pass state after SetRateControl = enabled:%t bitsLeft:%d frame:%d bpf:%d, want true/40000/0/20000",
			e.twoPass.enabled(), e.twoPass.bitsLeft, e.twoPass.frameIndex,
			e.rc.bitsPerFrame)
	}
	if err := e.SetRateControl(RateControlConfig{
		Mode:              RateControlCBR,
		TargetBitrateKbps: 600,
	}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetRateControl CBR with existing two-pass stats err = %v, want ErrInvalidConfig", err)
	}
}

func TestVP9EncoderSetRateControlBufferUpdatesBufferModel(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:               64,
		Height:              64,
		FPS:                 30,
		TargetBitrateKbps:   300,
		RateControlModeSet:  true,
		RateControlMode:     RateControlCBR,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	e.rc.bufferLevelBits = 100000
	if err := e.SetRateControlBuffer(200, 100, 150); err != nil {
		t.Fatalf("SetRateControlBuffer: %v", err)
	}
	if e.opts.BufferSizeMs != 200 || e.opts.BufferInitialSizeMs != 100 ||
		e.opts.BufferOptimalSizeMs != 150 {
		t.Fatalf("buffer opts = %d/%d/%d, want 200/100/150",
			e.opts.BufferSizeMs, e.opts.BufferInitialSizeMs,
			e.opts.BufferOptimalSizeMs)
	}
	if e.rc.bufferSizeBits != 60000 || e.rc.bufferInitialBits != 30000 ||
		e.rc.bufferOptimalBits != 45000 || e.rc.bufferLevelBits != 60000 {
		t.Fatalf("buffer bits = size:%d initial:%d optimal:%d level:%d, want 60000/30000/45000/60000",
			e.rc.bufferSizeBits, e.rc.bufferInitialBits,
			e.rc.bufferOptimalBits, e.rc.bufferLevelBits)
	}

	oldRC := e.rc
	oldOpts := e.opts
	if err := e.SetRateControlBuffer(0, 100, 150); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("invalid SetRateControlBuffer err = %v, want ErrInvalidConfig", err)
	}
	if e.rc != oldRC || !reflect.DeepEqual(e.opts, oldOpts) {
		t.Fatal("invalid SetRateControlBuffer mutated encoder state")
	}
}

func TestVP9EncoderSetRateControlBufferRequiresCBR(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetRateControlBuffer(200, 100, 150); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetRateControlBuffer without CBR err = %v, want ErrInvalidConfig", err)
	}
}
