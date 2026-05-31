package govpx

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9EncoderPostEncodeDropRequiresCBR(t *testing.T) {
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
	if err := e.SetPostEncodeDrop(true); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetPostEncodeDrop(true) on VBR err = %v, want ErrInvalidConfig",
			err)
	}
}

func TestVP9EncoderPostEncodeDropUpdatesCBRState(t *testing.T) {
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
	if err := e.SetPostEncodeDrop(true); err != nil {
		t.Fatalf("SetPostEncodeDrop: %v", err)
	}
	if !e.opts.PostEncodeDrop || !e.rc.postEncodeDrop {
		t.Fatalf("opts=%v rc=%v, want both true",
			e.opts.PostEncodeDrop, e.rc.postEncodeDrop)
	}
}

func TestVP9EncoderOptionsRejectPostEncodeDropOutsideCBR(t *testing.T) {
	if _, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		RateControlModeSet: true,
		RateControlMode:    RateControlVBR,
		TargetBitrateKbps:  600,
		PostEncodeDrop:     true,
	}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
}

func TestVP9CBRPostEncodeDropTriggersOnProjectedUnderflow(t *testing.T) {
	rc := &vp9RateControlState{
		enabled:         true,
		mode:            RateControlCBR,
		postEncodeDrop:  true,
		bufferLevelBits: 1000,
		bitsPerFrame:    4000,
		worstQuality:    200,
	}
	if !rc.shouldPostEncodeDrop(false, true, 100, 6000) {
		t.Fatalf("shouldPostEncodeDrop on buffer underflow = false, want true")
	}
}

func TestVP9CBRPostEncodeDropSkipsWithoutUnderflow(t *testing.T) {
	rc := &vp9RateControlState{
		enabled:         true,
		mode:            RateControlCBR,
		postEncodeDrop:  true,
		bufferLevelBits: 1000,
		bitsPerFrame:    4000,
		worstQuality:    200,
	}
	if rc.shouldPostEncodeDrop(false, true, 100, 4999) {
		t.Fatalf("shouldPostEncodeDrop with non-negative projected buffer = true, want false")
	}
}

func TestVP9CBRPostEncodeDropSkipsKeyFramesDisabledControlAndWorstQ(t *testing.T) {
	rc := &vp9RateControlState{
		enabled:         true,
		mode:            RateControlCBR,
		postEncodeDrop:  true,
		bufferLevelBits: 1000,
		bitsPerFrame:    4000,
		worstQuality:    200,
	}
	encoded := 6000
	if rc.shouldPostEncodeDrop(true, true, 100, encoded) {
		t.Fatalf("shouldPostEncodeDrop on key frame = true, want false")
	}
	if rc.shouldPostEncodeDrop(false, true, 200, encoded) {
		t.Fatalf("shouldPostEncodeDrop at worst q = true, want false")
	}
	rc.postEncodeDrop = false
	if rc.shouldPostEncodeDrop(false, true, 100, encoded) {
		t.Fatalf("shouldPostEncodeDrop disabled = true, want false")
	}
}

func TestVP9EncoderCBRPostEncodeDropBookkeepingMatchesLibvpx(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:               64,
		Height:              64,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   100,
		BufferSizeMs:        500,
		BufferInitialSizeMs: 200,
		BufferOptimalSizeMs: 250,
		DropFrameAllowed:    true,
		DropFrameWaterMark:  80,
		PostEncodeDrop:      true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	if !e.rc.postEncodeDrop {
		t.Fatalf("rc.postEncodeDrop = false, want true")
	}
	e.rc.bufferLevelBits = 1234
	e.rc.bitsPerFrame = 4000
	e.rc.framesToKey = 5
	e.rc.highSourceSAD = true
	if !e.rc.shouldPostEncodeDrop(false, true, 100, 6000) {
		t.Fatalf("shouldPostEncodeDrop in underflow = false, want true")
	}
	beforeBuffer := e.rc.bufferLevelBits
	e.rc.postEncodeDropFrame(100)
	if e.rc.bufferLevelBits != beforeBuffer {
		t.Fatalf("bufferLevelBits = %d, want unchanged %d",
			e.rc.bufferLevelBits, beforeBuffer)
	}
	if e.rc.lastQInter != 100 {
		t.Fatalf("lastQInter = %d, want dropped frame qindex", e.rc.lastQInter)
	}
	if e.rc.avgFrameQIndexInter != e.rc.worstQuality {
		t.Fatalf("avgFrameQIndexInter = %d, want worstQuality %d",
			e.rc.avgFrameQIndexInter, e.rc.worstQuality)
	}
	if !e.rc.forceMaxQ {
		t.Fatalf("forceMaxQ = false, want true")
	}
	if !e.rc.lastPostEncodeDroppedSceneChange {
		t.Fatalf("lastPostEncodeDroppedSceneChange = false, want true")
	}
	if e.rc.framesToKey != 4 {
		t.Fatalf("framesToKey = %d, want 4", e.rc.framesToKey)
	}
}

func TestVP9EncoderCBRPostEncodeDropForcesNextFrameToWorstQ(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  600,
		PostEncodeDrop:     true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	e.rc.forceMaxQ = true
	qindex := e.vp9EncoderFrameQIndex(false, false, 0, 1<<vp9LastRefSlot, 64)
	if qindex != int(e.rc.worstQuality) {
		t.Fatalf("qindex = %d, want worstQuality %d", qindex, e.rc.worstQuality)
	}
	if e.rc.forceMaxQ {
		t.Fatalf("forceMaxQ remained set after q pick")
	}
}

func TestVP9EncoderCBRPostEncodeDropCarriesSceneChangeToNextFrame(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  600,
		PostEncodeDrop:     true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	e.rc.lastPostEncodeDroppedSceneChange = true
	e.sf.UseSourceSad = 1
	e.vp9CarryPostEncodeDroppedSceneChange()
	if !e.rc.highSourceSAD {
		t.Fatalf("highSourceSAD = false, want carried scene-change signal")
	}
	if e.sf.UseSourceSad != 0 {
		t.Fatalf("UseSourceSad = %d, want disabled after post-drop scene change",
			e.sf.UseSourceSad)
	}
	if e.rc.lastPostEncodeDroppedSceneChange {
		t.Fatalf("lastPostEncodeDroppedSceneChange remained set")
	}
}

func TestVP9EncoderCBRPostEncodeDropSkipsReferenceAndContextCommit(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:               64,
		Height:              64,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   120,
		BufferSizeMs:        100,
		BufferInitialSizeMs: 10,
		BufferOptimalSizeMs: 20,
		Quantizer:           10,
		PostEncodeDrop:      true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	packet := make([]byte, 1<<20)
	key, err := e.EncodeIntoWithResult(vp9test.NewPanningYCbCr(64, 64, 0), packet)
	if err != nil {
		t.Fatalf("key EncodeInto: %v", err)
	}
	if key.Dropped || !key.KeyFrame || key.SizeBytes == 0 {
		t.Fatalf("key result = %+v, want coded key frame", key)
	}

	beforeFC := e.fc
	beforeContexts := e.frameContexts
	beforeHeaderType := e.lastVP9HeaderFrameType
	beforeHeaderValid := e.lastVP9HeaderValid
	beforeTxMode := e.prevFrameTxMode

	e.rc.bufferLevelBits = -e.rc.bitsPerFrame + 1
	inter, err := e.EncodeIntoWithResult(vp9test.NewPanningYCbCr(64, 64, 1), packet)
	if err != nil {
		t.Fatalf("inter EncodeInto: %v", err)
	}
	if !inter.Dropped || inter.SizeBytes != 0 || len(inter.Data) != 0 {
		t.Fatalf("inter result = %+v, want post-encode dropped frame", inter)
	}
	if inter.RefreshFrameFlags != 0 {
		t.Fatalf("RefreshFrameFlags = 0x%x, want 0 after drop", inter.RefreshFrameFlags)
	}
	if e.fc != beforeFC || e.frameContexts != beforeContexts {
		t.Fatalf("frame context committed on post-encode drop")
	}
	if e.lastVP9HeaderValid != beforeHeaderValid ||
		e.lastVP9HeaderFrameType != beforeHeaderType ||
		e.prevFrameTxMode != beforeTxMode {
		t.Fatalf("header/tx state committed on post-encode drop")
	}
	if !e.lastFrameDropped {
		t.Fatalf("lastFrameDropped = false, want true")
	}
}
