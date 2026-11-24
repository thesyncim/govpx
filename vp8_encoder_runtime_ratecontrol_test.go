package govpx

import (
	"errors"
	"reflect"
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vpx/geometry"
)

func TestSetRTCExternalRateControlRemainsEnabledAfterDisable(t *testing.T) {
	rtc := newTestEncoder(t)
	if err := rtc.SetRTCExternalRateControl(true); err != nil {
		t.Fatalf("SetRTCExternalRateControl(true) returned error: %v", err)
	}
	if !rtc.opts.RTCExternalRateControl {
		t.Fatalf("RTCExternalRateControl = false, want true")
	}
	if err := rtc.SetRTCExternalRateControl(false); err != nil {
		t.Fatalf("SetRTCExternalRateControl(false) returned error: %v", err)
	}
	if !rtc.opts.RTCExternalRateControl {
		t.Fatalf("RTCExternalRateControl = false after disable request, want sticky true")
	}
}

func TestSetRealtimeTargetInvalidCQBoundsDoNotMutateState(t *testing.T) {
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
	if err := e.SetRealtimeTarget(RealtimeTarget{MinQuantizer: 30}); !errors.Is(err, ErrInvalidQuantizer) {
		t.Fatalf("SetRealtimeTarget error = %v, want ErrInvalidQuantizer", err)
	}
	if e.opts.MinQuantizer != 4 || e.opts.MaxQuantizer != 56 || e.opts.CQLevel != 24 ||
		e.rc.minQuantizer != vp8common.PublicQuantizerToQIndex(4) ||
		e.rc.maxQuantizer != vp8common.PublicQuantizerToQIndex(56) ||
		e.rc.cqLevel != vp8common.PublicQuantizerToQIndex(24) {
		t.Fatalf("rate control after rejected target = opts:%d/%d/%d rc:%d/%d/%d, want public 4/56/24 mapped to qindex",
			e.opts.MinQuantizer, e.opts.MaxQuantizer, e.opts.CQLevel, e.rc.minQuantizer, e.rc.maxQuantizer, e.rc.cqLevel)
	}
}

func TestSetBitrateKbpsInvalidUpdatesDoNotMutateTarget(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1000,
		MinBitrateKbps:      500,
		MaxBitrateKbps:      1500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}

	if err := e.SetBitrateKbps(499); !errors.Is(err, ErrInvalidBitrate) {
		t.Fatalf("below min error = %v, want ErrInvalidBitrate", err)
	}
	if e.rc.targetBitrateKbps != 1000 {
		t.Fatalf("target after below-min update = %d, want unchanged 1000", e.rc.targetBitrateKbps)
	}
	if err := e.SetBitrateKbps(1501); !errors.Is(err, ErrInvalidBitrate) {
		t.Fatalf("above max error = %v, want ErrInvalidBitrate", err)
	}
	if e.rc.targetBitrateKbps != 1000 {
		t.Fatalf("target after above-max update = %d, want unchanged 1000", e.rc.targetBitrateKbps)
	}
	if err := e.SetBitrateKbps(1200); err != nil {
		t.Fatalf("in-range SetBitrateKbps returned error: %v", err)
	}
	if e.rc.targetBitrateKbps != 1200 {
		t.Fatalf("target after in-range update = %d, want 1200", e.rc.targetBitrateKbps)
	}
}

func TestSetBitrateKbpsPreservesLibvpxZeroBufferLevel(t *testing.T) {
	e := newTestEncoder(t)
	e.rc.bufferLevelBits = 0

	if err := e.SetBitrateKbps(600); err != nil {
		t.Fatalf("SetBitrateKbps returned error: %v", err)
	}

	if e.rc.bufferLevelBits != 0 {
		t.Fatalf("buffer after bitrate change = %d, want libvpx preserved zero", e.rc.bufferLevelBits)
	}
}

func TestSetRateControlBufferUpdatesVP8BufferModel(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               64,
		Height:              64,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   300,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	e.rc.bufferLevelBits = 100000
	if err := e.SetRateControlBuffer(200, 100, 150); err != nil {
		t.Fatalf("SetRateControlBuffer returned error: %v", err)
	}
	if e.opts.BufferSizeMs != 200 || e.opts.BufferInitialSizeMs != 100 ||
		e.opts.BufferOptimalSizeMs != 150 {
		t.Fatalf("buffer opts = %d/%d/%d, want 200/100/150",
			e.opts.BufferSizeMs, e.opts.BufferInitialSizeMs, e.opts.BufferOptimalSizeMs)
	}
	if e.rc.bufferSizeBits != 60000 || e.rc.bufferInitialBits != 30000 ||
		e.rc.bufferOptimalBits != 45000 || e.rc.bufferLevelBits != 60000 {
		t.Fatalf("buffer bits = size:%d initial:%d optimal:%d level:%d, want 60000/30000/45000/60000",
			e.rc.bufferSizeBits, e.rc.bufferInitialBits, e.rc.bufferOptimalBits, e.rc.bufferLevelBits)
	}

	oldRC := e.rc
	oldOpts := e.opts
	if err := e.SetRateControlBuffer(0, 100, 150); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("invalid SetRateControlBuffer error = %v, want ErrInvalidConfig", err)
	}
	if e.rc != oldRC || !reflect.DeepEqual(e.opts, oldOpts) {
		t.Fatal("invalid SetRateControlBuffer mutated encoder state")
	}
}

func TestSetRateControlBufferRejectsVP8VBR(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             64,
		Height:            64,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 300,
		MinQuantizer:      4,
		MaxQuantizer:      56,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	if err := e.SetRateControlBuffer(200, 100, 150); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetRateControlBuffer(VBR) error = %v, want ErrInvalidConfig", err)
	}
}

func TestSetErrorResilientUpdatesVP8LossResilienceBits(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             64,
		Height:            64,
		FPS:               30,
		Deadline:          DeadlineRealtime,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: 300,
		MinQuantizer:      4,
		MaxQuantizer:      56,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	defer e.Close()

	rows := geometry.MacroblockRows(e.opts.Height)
	cols := geometry.MacroblockCols(e.opts.Width)
	cbrGFInterval := e.goldenFrameCBRInterval(rows, cols)
	if cbrGFInterval == libvpxDefaultGFInterval {
		t.Fatalf("test setup CBR GF interval = default %d; choose dimensions that exercise the CBR branch", cbrGFInterval)
	}

	if err := e.SetErrorResilient(false, true); err != nil {
		t.Fatalf("SetErrorResilient(partitions) returned error: %v", err)
	}
	if e.opts.ErrorResilient || !e.opts.ErrorResilientPartitions {
		t.Fatalf("error resilient opts = default:%t partitions:%t, want false/true",
			e.opts.ErrorResilient, e.opts.ErrorResilientPartitions)
	}
	if !e.twoPass.errorResilient {
		t.Fatal("two-pass errorResilient = false, want true for partitions-only bitmask")
	}
	if e.rc.baselineGFInterval != libvpxDefaultGFInterval {
		t.Fatalf("partitions-only baselineGFInterval = %d, want default %d",
			e.rc.baselineGFInterval, libvpxDefaultGFInterval)
	}

	if err := e.SetErrorResilient(false, false); err != nil {
		t.Fatalf("SetErrorResilient(off) returned error: %v", err)
	}
	if e.opts.ErrorResilient || e.opts.ErrorResilientPartitions || e.twoPass.errorResilient {
		t.Fatalf("error resilient state after disable = opts:%t/%t twopass:%t, want all false",
			e.opts.ErrorResilient, e.opts.ErrorResilientPartitions, e.twoPass.errorResilient)
	}
	if e.rc.baselineGFInterval != cbrGFInterval {
		t.Fatalf("disabled baselineGFInterval = %d, want realtime CBR interval %d",
			e.rc.baselineGFInterval, cbrGFInterval)
	}
}

func TestSetErrorResilientDoesNotRecomputeCyclicRefreshBirthCohort(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             64,
		Height:            64,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 300,
		MinQuantizer:      4,
		MaxQuantizer:      56,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	defer e.Close()
	if e.cyclicRefreshModeEnabled(false) {
		t.Fatal("VBR-born encoder started with cyclic refresh enabled")
	}
	if err := e.SetErrorResilient(true, true); err != nil {
		t.Fatalf("SetErrorResilient returned error: %v", err)
	}
	if !e.opts.ErrorResilient || !e.opts.ErrorResilientPartitions {
		t.Fatalf("error resilient opts = %t/%t, want true/true",
			e.opts.ErrorResilient, e.opts.ErrorResilientPartitions)
	}
	if e.cyclicRefreshModeEnabled(false) {
		t.Fatal("runtime SetErrorResilient recomputed cyclic refresh for VBR-born encoder")
	}
}

func TestSetRateControlPreservesLibvpxZeroBufferLevel(t *testing.T) {
	e := newTestEncoder(t)
	e.rc.bufferLevelBits = 0

	err := e.SetRateControl(RateControlConfig{
		Mode:                RateControlCBR,
		TargetBitrateKbps:   900,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("SetRateControl returned error: %v", err)
	}

	if e.rc.bufferLevelBits != 0 {
		t.Fatalf("buffer after rate-control change = %d, want libvpx preserved zero", e.rc.bufferLevelBits)
	}
}

func TestCodecControlsRunVP8ChangeConfigSideEffectsForZeroValues(t *testing.T) {
	e := newTestEncoder(t)
	wantKbps := e.rc.libvpxClampToRawTargetRate(e.rc.targetBitrateKbps, e.rc.libvpxRateControlTiming())
	wantTargetBits := wantKbps * 1000
	wantBitsPerFrame := computeBitsPerFrame(wantTargetBits, e.rc.libvpxRateControlTiming())
	wantMaxBuffer := libvpxVP8BufferBits(e.rc.bufferSizeMs, wantTargetBits)

	assertChangeConfig := func(name string, fn func() error) {
		t.Helper()
		e.rc.targetBandwidthBits = 1
		e.rc.bitsPerFrame = 1
		e.rc.bufferLevelBits = maxInt()
		e.autoSpeed = 13
		if err := fn(); err != nil {
			t.Fatalf("%s returned error: %v", name, err)
		}
		if e.rc.targetBandwidthBits != wantTargetBits || e.rc.bitsPerFrame != wantBitsPerFrame {
			t.Fatalf("%s rate model = target:%d bpf:%d, want %d/%d",
				name, e.rc.targetBandwidthBits, e.rc.bitsPerFrame, wantTargetBits, wantBitsPerFrame)
		}
		if e.rc.bufferLevelBits != wantMaxBuffer || e.rc.maximumBufferBits != wantMaxBuffer {
			t.Fatalf("%s buffer = level:%d max:%d, want %d",
				name, e.rc.bufferLevelBits, e.rc.maximumBufferBits, wantMaxBuffer)
		}
		if e.autoSpeed != e.opts.CpuUsed {
			t.Fatalf("%s autoSpeed = %d, want vp8_change_config reset %d",
				name, e.autoSpeed, e.opts.CpuUsed)
		}
	}

	assertChangeConfig("SetNoiseSensitivity(0)", func() error {
		return e.SetNoiseSensitivity(0)
	})
	assertChangeConfig("SetErrorResilient(false, false)", func() error {
		return e.SetErrorResilient(false, false)
	})
	assertChangeConfig("VP8E_SET_ARNR_MAXFRAMES(0)", func() error {
		return e.setARNRMaxFrames(0)
	})
	assertChangeConfig("VP8E_SET_ARNR_STRENGTH(0)", func() error {
		return e.setARNRStrength(0)
	})
	assertChangeConfig("VP8E_SET_ARNR_TYPE(1)", func() error {
		return e.setARNRType(1)
	})
}

func TestSetRateControlPreservesLibvpxAdaptiveState(t *testing.T) {
	e := newTestEncoder(t)
	e.rc.decimationFactor = 2
	e.rc.decimationCount = 1
	e.rc.frameTargetBits = 12345
	e.rc.avgFrameQuantizer = 43
	e.rc.normalInterQuantizerTotal = 129
	e.rc.normalInterFrames = 3
	e.rc.normalInterAvgQuantizer = 43
	e.rc.rateCorrectionFactor = 1.75
	e.rc.keyFrameCorrectionFactor = 2.25
	e.rc.goldenCorrectionFactor = 1.5
	e.rc.totalActualBits = 123456
	e.rc.rollingActualBits = 2100
	e.rc.rollingTargetBits = 2200
	e.rc.longRollingActualBits = 2300
	e.rc.longRollingTargetBits = 2400

	err := e.SetRateControl(RateControlConfig{
		Mode:                RateControlCBR,
		TargetBitrateKbps:   900,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		DropFrameAllowed:    true,
		DropFrameWaterMark:  60,
	})
	if err != nil {
		t.Fatalf("SetRateControl returned error: %v", err)
	}

	if e.rc.decimationFactor != 2 || e.rc.decimationCount != 1 {
		t.Fatalf("decimation state = factor:%d count:%d, want 2/1", e.rc.decimationFactor, e.rc.decimationCount)
	}
	if e.rc.frameTargetBits != 12345 {
		t.Fatalf("frameTargetBits = %d, want libvpx preserved stale target 12345", e.rc.frameTargetBits)
	}
	if e.rc.avgFrameQuantizer != 43 || e.rc.normalInterQuantizerTotal != 129 || e.rc.normalInterFrames != 3 || e.rc.normalInterAvgQuantizer != 43 {
		t.Fatalf("quantizer history = avg:%d total:%d frames:%d normal:%d, want 43/129/3/43",
			e.rc.avgFrameQuantizer, e.rc.normalInterQuantizerTotal, e.rc.normalInterFrames, e.rc.normalInterAvgQuantizer)
	}
	if e.rc.rateCorrectionFactor != 1.75 || e.rc.keyFrameCorrectionFactor != 2.25 || e.rc.goldenCorrectionFactor != 1.5 {
		t.Fatalf("correction factors = %g/%g/%g, want 1.75/2.25/1.5",
			e.rc.rateCorrectionFactor, e.rc.keyFrameCorrectionFactor, e.rc.goldenCorrectionFactor)
	}
	if e.rc.totalActualBits != 123456 {
		t.Fatalf("totalActualBits = %d, want 123456", e.rc.totalActualBits)
	}
	if e.rc.rollingActualBits != 2100 || e.rc.rollingTargetBits != 2200 || e.rc.longRollingActualBits != 2300 || e.rc.longRollingTargetBits != 2400 {
		t.Fatalf("rolling bits = short:%d/%d long:%d/%d, want 2100/2200 and 2300/2400",
			e.rc.rollingActualBits, e.rc.rollingTargetBits, e.rc.longRollingActualBits, e.rc.longRollingTargetBits)
	}
}

func TestSetRateControlBundledFPSLeavesLibvpxFramerate(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             64,
		Height:            64,
		FPS:               30,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: 300,
		MinQuantizer:      4,
		MaxQuantizer:      56,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	defer e.Close()
	wantOutFPS := e.rc.outputFrameRate

	cfg := RateControlConfig{
		Mode:                RateControlCBR,
		TargetBitrateKbps:   700,
		MinQuantizer:        4,
		MaxQuantizer:        48,
		BufferSizeMs:        e.rc.bufferSizeMs,
		BufferInitialSizeMs: e.rc.bufferInitialSizeMs,
		BufferOptimalSizeMs: e.rc.bufferOptimalSizeMs,
		DropFrameWaterMark:  60,
	}
	if err := e.SetRateControl(cfg); err != nil {
		t.Fatalf("SetRateControl(bundle): %v", err)
	}
	// Mirror fuzz/runtime enc_config_set: store new g_timebase only.
	e.opts.FPS = 15
	e.opts.TimebaseNum = 1
	e.opts.TimebaseDen = 15
	e.timing = timingFromEncoderOptions(e.opts)

	if e.rc.outputFrameRate != wantOutFPS {
		t.Fatalf("outputFrameRate = %d, want libvpx-preserved %d after enc_cfg fps token", e.rc.outputFrameRate, wantOutFPS)
	}
	if got := computeBitsPerFrame(e.rc.targetBandwidthBits, e.timing); got == e.rc.bitsPerFrame {
		t.Fatalf("bitsPerFrame = %d matches g_timebase-only bpf %d, want cpi->framerate bpf", e.rc.bitsPerFrame, got)
	}
	if got := computeBitsPerFrame(e.rc.targetBandwidthBits, e.rc.libvpxRateControlTiming()); got != e.rc.bitsPerFrame {
		t.Fatalf("bitsPerFrame = %d, want %d from outputFrameRate %d", e.rc.bitsPerFrame, got, wantOutFPS)
	}
}

// TestSetRateControlPinsLibvpxCyclicRefreshMode asserts that the cyclic
// refresh mode flag tracks libvpx's vp8_create_compressor gate: it is
// computed once at construction and never recomputed by
// vpx_codec_enc_config_set. A CBR-born encoder therefore keeps cyclic
// refresh after switching to VBR/CQ/Q, and a VBR-born encoder never gains
// it after switching to CBR. This matches libvpx pack_bitstream output:
// the VBR→CBR / CBR→VBR runtime-transition byte-parity oracles show
// segmentation_enabled tracks the construction-time mode, not the live
// RC mode.
