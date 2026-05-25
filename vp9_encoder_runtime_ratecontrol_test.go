package govpx

import (
	"errors"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"reflect"
	"testing"
)

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
	packet, err := e.Encode(vp9test.NewYCbCr(width, height, 128, 128, 128))
	if err != nil {
		t.Fatalf("Encode variance AQ key: %v", err)
	}
	header, _ := vp9test.ParseHeader(t, packet)
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
	keyN, err := cbr.EncodeInto(vp9test.NewYCbCr(width, height, 96, 128, 128), dst)
	if err != nil {
		t.Fatalf("Encode cyclic AQ key: %v", err)
	}
	keyPacket := append([]byte(nil), dst[:keyN]...)
	interN, err := cbr.EncodeInto(vp9test.NewYCbCr(width, height, 116, 128, 128), dst)
	if err != nil {
		t.Fatalf("Encode cyclic AQ inter: %v", err)
	}
	interPacket := append([]byte(nil), dst[:interN]...)
	keyHeader, _ := vp9test.ParseHeader(t, keyPacket)
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
	src := vp9test.NewCheckerYCbCr(64, 64, 0, 255, 80, 192)
	packet, err := e.Encode(src)
	if err != nil {
		t.Fatalf("lossless Encode: %v", err)
	}
	h, _ := vp9test.ParseHeader(t, packet)
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
	h, _ = vp9test.ParseHeader(t, packet)
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
		e.rc.cqLevel != uint8(encoder.PublicQuantizerToQIndex(20)) {
		t.Fatalf("CQ rate control state = opts:%+v rc:%+v, want enabled CQ 700kbps cq20",
			e.opts, e.rc)
	}
	dst := make([]byte, 65536)
	result, err := e.EncodeIntoWithResult(
		vp9test.NewYCbCr(64, 64, 96, 128, 128), dst)
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
