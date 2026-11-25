package codec

import (
	"bytes"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"testing"
)

func TestEncoderLoopFilterHeaderMirrorsLibvpxDefaultDeltasAcrossQualities(t *testing.T) {
	tests := []struct {
		name      string
		deadline  Deadline
		wantModes [vp8common.MaxModeLFDeltas]int8
	}{
		{name: "best quality", deadline: DeadlineBestQuality, wantModes: [vp8common.MaxModeLFDeltas]int8{4, -2, 2, 4}},
		{name: "good quality", deadline: DeadlineGoodQuality, wantModes: [vp8common.MaxModeLFDeltas]int8{4, -2, 2, 4}},
		{name: "realtime", deadline: DeadlineRealtime, wantModes: [vp8common.MaxModeLFDeltas]int8{4, -12, 2, 4}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &VP8Encoder{opts: EncoderOptions{Deadline: tt.deadline}}
			header := e.encoderLoopFilterHeader(17, 3)
			if !header.DeltaEnabled || !header.DeltaUpdate {
				t.Fatalf("delta flags = enabled:%t update:%t, want enabled update", header.DeltaEnabled, header.DeltaUpdate)
			}
			if wantRefs := ([vp8common.MaxRefLFDeltas]int8{2, 0, -2, -2}); header.RefDeltas != wantRefs {
				t.Fatalf("ref deltas = %v, want %v", header.RefDeltas, wantRefs)
			}
			if header.ModeDeltas != tt.wantModes {
				t.Fatalf("mode deltas = %v, want %v", header.ModeDeltas, tt.wantModes)
			}
		})
	}

	e := &VP8Encoder{opts: EncoderOptions{Deadline: DeadlineRealtime}}
	if header := e.encoderLoopFilterHeader(0, 3); !header.DeltaEnabled || !header.DeltaUpdate {
		t.Fatalf("zero-level delta flags = enabled:%t update:%t, want enabled update", header.DeltaEnabled, header.DeltaUpdate)
	}
}

func TestComputeLFDeltaUpdateBitResignalsEveryKeyFrame(t *testing.T) {
	e := &VP8Encoder{opts: EncoderOptions{Deadline: DeadlineRealtime}}
	header := e.encoderLoopFilterHeader(17, 0)

	if !e.computeLFDeltaUpdateBit(vp8common.KeyFrame, header.DeltaEnabled, header.RefDeltas, header.ModeDeltas) {
		t.Fatalf("first keyframe LF delta update = false, want true")
	}
	e.updateLastSignaledLFDeltas(header.DeltaEnabled, header.RefDeltas, header.ModeDeltas)

	if !e.computeLFDeltaUpdateBit(vp8common.KeyFrame, header.DeltaEnabled, header.RefDeltas, header.ModeDeltas) {
		t.Fatalf("repeated keyframe LF delta update = false, want true")
	}
	if e.computeLFDeltaUpdateBit(vp8common.InterFrame, header.DeltaEnabled, header.RefDeltas, header.ModeDeltas) {
		t.Fatalf("unchanged inter-frame LF delta update = true, want false")
	}
}

func TestPendingLFDeltaUpdateRestoredAfterDroppedFrame(t *testing.T) {
	var e VP8Encoder

	e.restorePendingLFDeltaUpdateAfterDrop(false)
	if e.pendingLFDeltaUpdate {
		t.Fatalf("pending LF update restored without consumed force bit")
	}
	e.restorePendingLFDeltaUpdateAfterDrop(true)
	if !e.pendingLFDeltaUpdate {
		t.Fatalf("pending LF update = false, want restored for next encoded frame")
	}
	force := e.consumePendingLFDeltaUpdate()
	if !force || e.pendingLFDeltaUpdate {
		t.Fatalf("consume restored LF update failed: force=%t pending=%t", force, e.pendingLFDeltaUpdate)
	}
}

func TestEncoderLoopFilterHeaderUsesRealtimeSimpleFilterAtHighSpeed(t *testing.T) {
	tests := []struct {
		name     string
		deadline Deadline
		cpuUsed  int
		want     vp8dec.LoopFilterType
	}{
		{name: "realtime positive cpu-used cold auto-speed", deadline: DeadlineRealtime, cpuUsed: 14, want: vp8dec.NormalLoopFilter},
		{name: "realtime explicit speed thirteen", deadline: DeadlineRealtime, cpuUsed: -13, want: vp8dec.NormalLoopFilter},
		{name: "realtime explicit speed fourteen", deadline: DeadlineRealtime, cpuUsed: -14, want: vp8dec.SimpleLoopFilter},
		{name: "realtime explicit speed fifteen", deadline: DeadlineRealtime, cpuUsed: -15, want: vp8dec.SimpleLoopFilter},
		{name: "good quality speed fifteen", deadline: DeadlineGoodQuality, cpuUsed: 15, want: vp8dec.NormalLoopFilter},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &VP8Encoder{opts: EncoderOptions{Deadline: tt.deadline, CpuUsed: tt.cpuUsed}}
			header := e.encoderLoopFilterHeader(17, 3)
			if header.Type != tt.want {
				t.Fatalf("loop filter type = %d, want %d", header.Type, tt.want)
			}
		})
	}
}

func TestEncoderLoopFilterHeaderUsesNormalFilterForRealtimeSpeedFour(t *testing.T) {
	serial := &VP8Encoder{opts: EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: 8}}
	if got := serial.encoderLoopFilterHeader(17, 3).Type; got != vp8dec.NormalLoopFilter {
		t.Fatalf("serial realtime speed=4 loop filter type = %d, want normal", got)
	}

	threaded := &VP8Encoder{
		opts:               EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: 8},
		rowWorkers:         &rowWorkerPool{},
		threadedRowsActive: true,
	}
	if got := threaded.encoderLoopFilterHeader(17, 3).Type; got != vp8dec.NormalLoopFilter {
		t.Fatalf("threaded realtime speed=4 loop filter type = %d, want normal", got)
	}
}

func TestLibvpxMaxLoopFilterLevelCapsHighIntraSections(t *testing.T) {
	e := &VP8Encoder{}
	if got := e.libvpxMaxLoopFilterLevelForFrame(); got != vp8common.MaxLoopFilter {
		t.Fatalf("default max loop filter = %d, want %d", got, vp8common.MaxLoopFilter)
	}
	e.twoPass.sectionIntraRating = 9
	if got, want := e.libvpxMaxLoopFilterLevelForFrame(), vp8common.MaxLoopFilter*3/4; got != want {
		t.Fatalf("high-intra max loop filter = %d, want %d", got, want)
	}
}

func TestEncoderLoopFilterUsesPreviousInterLevelWithLibvpxClamp(t *testing.T) {
	e := &VP8Encoder{
		opts:            EncoderOptions{Sharpness: 3},
		rc:              rateControlState{currentQuantizer: 40},
		loopFilterLevel: 13,
	}
	level, sharpness := e.encoderLoopFilter(vp8common.InterFrame)
	if level != 13 || sharpness != 3 {
		t.Fatalf("inter loop filter = level:%d sharpness:%d, want previous 13 sharpness 3", level, sharpness)
	}

	e.loopFilterLevel = 0
	level, _ = e.encoderLoopFilter(vp8common.InterFrame)
	if level != 5 {
		t.Fatalf("clamped inter loop filter level = %d, want libvpx min q/8 = 5", level)
	}

	level, sharpness = e.encoderLoopFilter(vp8common.KeyFrame)
	if level != 15 || sharpness != 0 {
		t.Fatalf("key loop filter = level:%d sharpness:%d, want q*3/8=15 sharpness 0", level, sharpness)
	}
}

func TestEncodeIntoRealtimeHighSpeedWritesSimpleLoopFilter(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             32,
		Height:            32,
		FPS:               30,
		TargetBitrateKbps: 300,
		MinQuantizer:      20,
		MaxQuantizer:      20,
		Deadline:          DeadlineRealtime,
		CpuUsed:           -14,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}

	keySource := testImage(32, 32)
	fillImage(keySource, 80, 128, 128)
	key, err := e.EncodeInto(make([]byte, 4096), keySource, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyState := packetState(t, key.Data)
	if keyState.LoopFilter.Type != vp8dec.SimpleLoopFilter {
		t.Fatalf("key loop filter type = %d, want simple", keyState.LoopFilter.Type)
	}

	interSource := testImage(32, 32)
	fillImage(interSource, 82, 128, 128)
	inter, err := e.EncodeInto(make([]byte, 4096), interSource, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.Dropped {
		t.Fatalf("inter frame dropped, want encoded interframe")
	}
	interState := packetState(t, inter.Data)
	if interState.LoopFilter.Type != vp8dec.SimpleLoopFilter {
		t.Fatalf("inter loop filter type = %d, want simple", interState.LoopFilter.Type)
	}
}

func TestLoopFilterUsesFastSearchMirrorsLibvpxAutoFilterSpeedFeature(t *testing.T) {
	tests := []struct {
		name     string
		deadline Deadline
		cpuUsed  int
		want     bool
	}{
		{name: "best quality uses full search", deadline: DeadlineBestQuality, cpuUsed: 8, want: false},
		{name: "good speed four uses full search", deadline: DeadlineGoodQuality, cpuUsed: 4, want: false},
		{name: "good speed five uses fast search", deadline: DeadlineGoodQuality, cpuUsed: 5, want: true},
		{name: "realtime positive cpu-used auto-speed uses full search", deadline: DeadlineRealtime, cpuUsed: 5, want: false},
		{name: "realtime explicit speed two uses full search", deadline: DeadlineRealtime, cpuUsed: -2, want: false},
		{name: "realtime explicit speed three uses fast search", deadline: DeadlineRealtime, cpuUsed: -3, want: true},
		{name: "realtime explicit speed four uses full search", deadline: DeadlineRealtime, cpuUsed: -4, want: false},
		{name: "realtime explicit speed five uses fast search", deadline: DeadlineRealtime, cpuUsed: -5, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &VP8Encoder{opts: EncoderOptions{Deadline: tt.deadline, CpuUsed: tt.cpuUsed}}
			if got := e.loopFilterUsesFastSearch(); got != tt.want {
				t.Fatalf("fast search = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestLoopFilterUsesFastSearchForThreadedRealtimeInterFrames(t *testing.T) {
	serial := &VP8Encoder{opts: EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: 8}}
	if serial.loopFilterUsesFastSearchForFrame() {
		t.Fatalf("serial realtime speed=4 used fast loop-filter search")
	}

	threaded := &VP8Encoder{
		opts:       EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: 8},
		rowWorkers: &rowWorkerPool{},
	}
	if threaded.loopFilterUsesFastSearchForFrame() {
		t.Fatalf("threaded realtime speed=4 used fast loop-filter search")
	}
	fast := &VP8Encoder{opts: EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: -5}}
	if !fast.loopFilterUsesFastSearchForFrame() {
		t.Fatalf("realtime explicit speed=5 did not use fast loop-filter search")
	}
}

func TestLoopFilterUsesFastSearchFollowsAutoSelectSpeed(t *testing.T) {
	// auto_filter consults the same cpi->Speed as every other
	// vp8_set_speed_features gate. Under the pinned auto-select trajectory
	// (Speed clamped at the realtime floor of 4, matching the production
	// vpxenc on the reference host) the realtime cpu_used=8 path keeps the
	// FULL loop-filter search at every frame size; only a genuinely ramped
	// Speed (> 4, or the == 3 special case) flips to the fast picker. The
	// former per-gate "realistic Speed" override forced the fast picker on
	// 720p-class frames while the production vpxenc used the full search --
	// part of the drop-parity divergence pinned by
	// TestVP8RealtimeOverloadDropParity.
	large := &VP8Encoder{
		opts:       EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: 8, Width: 1280, Height: 720},
		frameCount: 2,
		autoSpeed:  4,
	}
	if large.loopFilterUsesFastSearchForFrame() {
		t.Fatalf("large post-cold-start realtime cpu=8 frame at Speed 4 used fast loop-filter search")
	}
	if large.loopFilterUsesFastSearch() {
		t.Fatalf("static loop-filter speed mirror should keep realtime autoSpeed=4 on full search")
	}

	ramped := &VP8Encoder{
		opts:       EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: 8, Width: 1280, Height: 720},
		frameCount: 2,
		autoSpeed:  9,
	}
	if !ramped.loopFilterUsesFastSearchForFrame() {
		t.Fatalf("ramped (autoSpeed=9) realtime cpu=8 frame did not use fast loop-filter search")
	}

	tiny := &VP8Encoder{
		opts:       EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: 8, Width: 640, Height: 360},
		frameCount: 2,
		autoSpeed:  4,
	}
	if tiny.loopFilterUsesFastSearchForFrame() {
		t.Fatalf("tiny realtime cpu=8 frame at Speed 4 used fast loop-filter search")
	}

	first := &VP8Encoder{
		opts:       EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: 8, Width: 1280, Height: 720},
		frameCount: 0,
		autoSpeed:  4,
	}
	if first.loopFilterUsesFastSearchForFrame() {
		t.Fatalf("cold-start realtime cpu=8 frame used fast loop-filter search")
	}
}

func TestPickLoopFilterLevelFastMatchesFullFrameBaseline(t *testing.T) {
	const width, height = 64, 128
	rows := (height + 15) / 16
	cols := (width + 15) / 16
	required := rows * cols

	src := testImage(width, height)
	for r := range height {
		for c := range width {
			src.Y[r*src.YStride+c] = byte(40 + (r*7+c*11)%160)
			src.U[(r/2)*src.UStride+(c/2)] = 128
			src.V[(r/2)*src.VStride+(c/2)] = 128
		}
	}

	buildEncoder := func() *VP8Encoder {
		e := newSizedTestEncoder(t, width, height)
		for r := 0; r < e.analysis.Img.CodedHeight; r++ {
			for c := 0; c < e.analysis.Img.CodedWidth; c++ {
				e.analysis.Img.Y[r*e.analysis.Img.YStride+c] = byte(50 + (r*5+c*9)%180)
			}
		}
		for i := range e.analysis.Img.U {
			e.analysis.Img.U[i] = 128
		}
		for i := range e.analysis.Img.V {
			e.analysis.Img.V[i] = 128
		}
		if len(e.reconstructModes) < required {
			e.reconstructModes = make([]vp8dec.MacroblockMode, required)
		}
		for i := range required {
			e.reconstructModes[i] = vp8dec.MacroblockMode{
				Mode:     vp8common.DCPred,
				UVMode:   vp8common.DCPred,
				RefFrame: vp8common.LastFrame,
			}
		}
		e.rc.currentQuantizer = 60
		return e
	}

	srcImg := sourceImageFromPublic(src)
	ePartial := buildEncoder()
	partialCtx := ePartial.newLoopFilterPickContext(srcImg, vp8common.InterFrame, 0, rows, cols, required, vp8enc.SegmentationConfig{})
	got, err := partialCtx.pickFast(24, vp8enc.LibvpxMinLoopFilterLevel(ePartial.rc.currentQuantizer))
	if err != nil {
		t.Fatalf("loopFilterPickContext.pickFast returned error: %v", err)
	}

	// Reference: search the same neighborhood as fast search but using the
	// full-frame loop filter and partial-window SSE. Selected level must
	// match exactly.
	eRef := buildEncoder()
	minLevel := vp8enc.LibvpxMinLoopFilterLevel(eRef.rc.currentQuantizer)
	maxLevel := vp8enc.LibvpxMaxLoopFilterLevel(eRef.rc.currentQuantizer)
	level := vp8enc.ClampLoopFilterPickLevel(24, minLevel, maxLevel)
	bestLevel := level
	refCtx := eRef.newLoopFilterPickContext(srcImg, vp8common.InterFrame, 0, rows, cols, required, vp8enc.SegmentationConfig{})
	score := func(lvl int) int {
		refCtx.trialLumaSSE(lvl, false)
		return vp8enc.LoopFilterLumaSSE(srcImg, &eRef.loopFilterPick.Img, rows, cols, true)
	}
	bestErr := score(level)
	filtLevel := level - vp8enc.LoopFilterSearchStep(level)
	for filtLevel >= minLevel {
		filtErr := score(filtLevel)
		if filtErr < bestErr {
			bestErr = filtErr
			bestLevel = filtLevel
		} else {
			break
		}
		filtLevel -= vp8enc.LoopFilterSearchStep(filtLevel)
	}
	filtLevel = level + vp8enc.LoopFilterSearchStep(filtLevel)
	if bestLevel == level {
		bestErr -= bestErr >> 10
		for filtLevel < maxLevel {
			filtErr := score(filtLevel)
			if filtErr < bestErr {
				bestErr = filtErr - (filtErr >> 10)
				bestLevel = filtLevel
			} else {
				break
			}
			filtLevel += vp8enc.LoopFilterSearchStep(filtLevel)
		}
	}
	want := uint8(vp8enc.ClampLoopFilterPickLevel(bestLevel, minLevel, maxLevel))
	if got != want {
		t.Fatalf("fast pick = %d, full-frame baseline = %d", got, want)
	}
}

func TestEncodeIntoInvisibleFrameUpdatesReferenceWithoutOutput(t *testing.T) {
	e := newTestEncoder(t)
	src := testImage(16, 16)
	fillImage(src, 220, 90, 170)
	invisiblePacket := make([]byte, 4096)

	invisible, err := e.EncodeInto(invisiblePacket, src, 0, 1, EncodeInvisibleFrame)
	if err != nil {
		t.Fatalf("invisible EncodeInto returned error: %v", err)
	}
	info, err := PeekVP8StreamInfo(invisible.Data)
	if err != nil {
		t.Fatalf("PeekVP8StreamInfo returned error: %v", err)
	}
	if !invisible.KeyFrame || !info.KeyFrame || info.ShowFrame {
		t.Fatalf("invisible result/header = %+v/%+v, want invisible keyframe", invisible, info)
	}

	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(invisible.Data); err != nil {
		t.Fatalf("Decode invisible returned error: %v", err)
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatalf("NextFrame returned invisible frame")
	}

	visiblePacket := make([]byte, 4096)
	visible, err := e.EncodeInto(visiblePacket, publicImageFromVP8(&e.lastRef.Img), 1, 1, 0)
	if err != nil {
		t.Fatalf("visible EncodeInto returned error: %v", err)
	}
	if visible.KeyFrame {
		t.Fatalf("visible KeyFrame = true, want interframe after invisible keyframe reference update")
	}
	if err := d.Decode(visible.Data); err != nil {
		t.Fatalf("Decode visible returned error: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("NextFrame returned no visible frame")
	}
	assertImagesEqual(t, "visible after invisible", publicImageFromVP8(&e.current.Img), frame)
}

func TestEncodeIntoSharpnessAppliesLoopFilterToReferences(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               32,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        20,
		MaxQuantizer:        20,
		Sharpness:           3,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	first := testImage(32, 16)
	fillImage(first, 220, 90, 170)
	for row := 0; row < first.Height; row++ {
		for col := 16; col < first.Width; col++ {
			first.Y[row*first.YStride+col] = 40
		}
	}
	keyPacket := make([]byte, 8192)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyState := parseEncoderStateHeader(t, key.Data)
	if keyState.LoopFilter.Level != 9 || keyState.LoopFilter.SharpnessLevel != 0 {
		t.Fatalf("key loop filter = %+v, want level 9 sharpness 0", keyState.LoopFilter)
	}
	keyFrame := decodeSingleFrame(t, key.Data)
	assertImagesEqual(t, "filtered key current", keyFrame, publicImageFromVP8(&e.current.Img))

	second := testImage(32, 16)
	fillImage(second, 40, 90, 170)
	for row := 0; row < second.Height; row++ {
		for col := 16; col < second.Width; col++ {
			second.Y[row*second.YStride+col] = 220
		}
	}
	interPacket := make([]byte, 8192)
	inter, err := e.EncodeInto(interPacket, second, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want interframe")
	}
	interState := parseEncoderStateHeader(t, inter.Data)
	if interState.LoopFilter.Level != 9 || interState.LoopFilter.SharpnessLevel != 3 {
		t.Fatalf("inter loop filter = %+v, want level 9 sharpness 3", interState.LoopFilter)
	}
	decoded := decodeFrameSequence(t, key.Data, inter.Data)
	assertImagesEqual(t, "filtered inter current", decoded[1], publicImageFromVP8(&e.current.Img))
	assertImagesEqual(t, "filtered inter last", decoded[1], publicImageFromVP8(&e.lastRef.Img))
}

func TestEncodeIntoDefaultSharpnessStillAppliesLoopFilter(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               32,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        20,
		MaxQuantizer:        20,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	src := testImage(32, 16)
	fillImage(src, 220, 90, 170)
	for row := 0; row < src.Height; row++ {
		for col := 16; col < src.Width; col++ {
			src.Y[row*src.YStride+col] = 40
		}
	}

	result, err := e.EncodeInto(make([]byte, 8192), src, 0, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}
	state := parseEncoderStateHeader(t, result.Data)
	if state.LoopFilter.Level != 9 || state.LoopFilter.SharpnessLevel != 0 {
		t.Fatalf("loop filter = %+v, want level 9 sharpness 0", state.LoopFilter)
	}
	decoded := decodeSingleFrame(t, result.Data)
	assertImagesEqual(t, "default filtered current", decoded, publicImageFromVP8(&e.current.Img))
}

// BenchmarkVP8LoopFilterPickTrial720p pins the production-geometry cost of one
// loop-filter picker trial at the canonical 1280x720 realtime spot, split by
// sub-phase. The full-trial shape (full-plane luma copy + full-frame luma
// filter + full-frame SSE) is what realtime cpu-used=8 actually runs on the
// canonical 720p fixture: pinned auto-Speed 4 selects libvpx's
// vp8cx_pick_filter_level (sf->auto_filter=1), not the partial fast picker.
//
// 2026-07-16 diagnosis pin: the connected 480-frame phase numbers
// (copy 19.98us / filter 323.5us / sse 33.0us per trial) match this
// cache-warm benchmark (copy ~18.5us / filter ~330us / sse ~56us), and the
// V16 inner-edge kernel's in-situ per-call cost from the CPU profile (33ns)
// matches its hot-buffer microbench. Any future re-appearance of a
// wall-vs-CPU gap in the lf-pick phase should be adjudicated against these
// numbers before assuming memory stalls.
func BenchmarkVP8LoopFilterPickTrial720p(b *testing.B) {
	const width, height = 1280, 720
	rows := (height + 15) / 16
	cols := (width + 15) / 16
	required := rows * cols

	src := testImage(width, height)
	for r := range height {
		for c := range width {
			src.Y[r*src.YStride+c] = byte(40 + (r*7+c*11)%160)
		}
	}
	e := newSizedTestEncoder(b, width, height)
	for r := 0; r < e.analysis.Img.CodedHeight; r++ {
		for c := 0; c < e.analysis.Img.CodedWidth; c++ {
			e.analysis.Img.Y[r*e.analysis.Img.YStride+c] = byte(50 + (r*5+c*9)%180)
		}
	}
	if len(e.reconstructModes) < required {
		e.reconstructModes = make([]vp8dec.MacroblockMode, required)
	}
	for i := range required {
		e.reconstructModes[i] = vp8dec.MacroblockMode{
			Mode:     vp8common.NearestMV,
			UVMode:   vp8common.DCPred,
			RefFrame: vp8common.LastFrame,
		}
	}
	srcImg := sourceImageFromPublic(src)
	ctx := e.newLoopFilterPickContext(srcImg, vp8common.InterFrame, 0, rows, cols, required, vp8enc.SegmentationConfig{})

	startRow, rowCount := vp8enc.LoopFilterPartialFrameWindow(rows)

	b.Run("full_trial", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			ctx.trialLumaSSEFull(40)
		}
	})
	b.Run("full_copy_only", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			vp8common.CopyImageLuma(&e.loopFilterPick.Img, &e.analysis.Img)
		}
	})
	b.Run("full_filter_only", func(b *testing.B) {
		vp8common.CopyImageLuma(&e.loopFilterPick.Img, &e.analysis.Img)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			vp8dec.ApplyLoopFilterFullLumaConfiguredUnchecked(&e.loopFilterPick.Img, rows, cols, ctx.modes, ctx.frameType, ctx.filterType, 40, ctx.fullFrameConfig, &e.loopInfo)
		}
	})
	b.Run("full_sse_only", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			vp8enc.LoopFilterLumaSSE(ctx.src, &e.loopFilterPick.Img, rows, cols, false)
		}
	})
	// The partial fast-picker shape (speed==3 || speed>4) for reference; the
	// canonical 720p realtime cpu8 spot does not exercise it because pinned
	// auto-Speed holds at 4 there.
	b.Run("partial_trial", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			ctx.trialLumaSSEPartial(40)
		}
	})
	b.Run("partial_filter_only", func(b *testing.B) {
		vp8enc.CopyLoopFilterPartialLuma(&e.loopFilterPick.Img, &e.analysis.Img, startRow, rowCount)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			vp8dec.ApplyLoopFilterPartialConfiguredUnchecked(&e.loopFilterPick.Img, rows, cols, ctx.modes, ctx.frameType, ctx.filterType, 40, ctx.fastFrameConfig, &e.loopInfo, startRow, rowCount)
		}
	})
}

func TestLoopFilterTrialLumaSSELevelZeroUsesLibvpxTrialFilter(t *testing.T) {
	const width, height = 64, 128
	rows := (height + 15) / 16
	cols := (width + 15) / 16
	required := rows * cols

	src := testImage(width, height)
	for r := range height {
		for c := range width {
			src.Y[r*src.YStride+c] = byte(33 + (r*13+c*3)%170)
			src.U[(r/2)*src.UStride+(c/2)] = 128
			src.V[(r/2)*src.VStride+(c/2)] = 128
		}
	}

	e := newSizedTestEncoder(t, width, height)
	for r := 0; r < e.analysis.Img.CodedHeight; r++ {
		for c := 0; c < e.analysis.Img.CodedWidth; c++ {
			e.analysis.Img.Y[r*e.analysis.Img.YStride+c] = byte(57 + (r*5+c*11)%160)
		}
	}
	for i := range e.loopFilterPick.Img.Y {
		e.loopFilterPick.Img.Y[i] = 201
	}
	for i := range required {
		e.reconstructModes[i] = vp8dec.MacroblockMode{
			RefFrame: vp8common.IntraFrame,
			Mode:     vp8common.DCPred,
			UVMode:   vp8common.DCPred,
		}
	}
	srcImg := sourceImageFromPublic(src)
	ctx := e.newLoopFilterPickContext(srcImg, vp8common.InterFrame, 0, rows, cols, required, vp8enc.SegmentationConfig{})
	for _, partial := range []bool{false, true} {
		for i := range e.loopFilterPick.Img.Y {
			e.loopFilterPick.Img.Y[i] = 201
		}
		scratchBefore := append([]byte(nil), e.loopFilterPick.Img.Y...)
		got := ctx.trialLumaSSE(0, partial)
		if got <= 0 {
			t.Fatalf("level zero trial partial=%t SSE = %d, want scored trial", partial, got)
		}
		if bytes.Equal(e.loopFilterPick.Img.Y, scratchBefore) {
			t.Fatalf("level zero trial partial=%t left loop-filter scratch buffer untouched", partial)
		}
	}
}

func TestLoopFilterTrialLumaSSEPartialMatchesFullFrameWindow(t *testing.T) {
	const width, height = 64, 128
	rows := (height + 15) / 16
	cols := (width + 15) / 16
	required := rows * cols

	src := testImage(width, height)
	fillImage(src, 96, 128, 128)
	for r := range height {
		for c := range width {
			src.Y[r*src.YStride+c] = byte(40 + (r*7+c*11)%160)
		}
	}

	e := newSizedTestEncoder(t, width, height)
	e.threadedRowsActive = true
	// Seed the analysis buffer with reconstructed-like values that differ
	// macroblock-by-macroblock so the loop filter actually has work to do.
	for r := 0; r < e.analysis.Img.CodedHeight; r++ {
		for c := 0; c < e.analysis.Img.CodedWidth; c++ {
			e.analysis.Img.Y[r*e.analysis.Img.YStride+c] = byte(50 + (r*5+c*9)%180)
		}
	}
	for i := range e.analysis.Img.U {
		e.analysis.Img.U[i] = 128
	}
	for i := range e.analysis.Img.V {
		e.analysis.Img.V[i] = 128
	}
	if len(e.reconstructModes) < required {
		e.reconstructModes = make([]vp8dec.MacroblockMode, required)
	}
	for i := range required {
		e.reconstructModes[i] = vp8dec.MacroblockMode{
			Mode:     vp8common.DCPred,
			UVMode:   vp8common.DCPred,
			RefFrame: vp8common.LastFrame,
		}
	}

	srcImg := sourceImageFromPublic(src)
	ctx := e.newLoopFilterPickContext(srcImg, vp8common.InterFrame, 0, rows, cols, required, vp8enc.SegmentationConfig{})
	for _, level := range []int{8, 24, 48} {
		partialErr := ctx.trialLumaSSE(level, true)
		fullErr := ctx.trialLumaSSE(level, false)
		// The full path computes SSE over the whole frame; recompute the
		// partial-window SSE on the buffer left behind by the full filter so
		// we can compare against the partial path.
		fullPartialWindow := vp8enc.LoopFilterLumaSSE(srcImg, &e.loopFilterPick.Img, rows, cols, true)
		_ = fullErr
		if partialErr != fullPartialWindow {
			t.Fatalf("level=%d partial SSE = %d, full-frame partial-window SSE = %d", level, partialErr, fullPartialWindow)
		}
	}
}

func TestLoopFilterTrialLumaSSEStatsMatchesNoStatsPath(t *testing.T) {
	const width, height = 64, 64
	rows := (height + 15) / 16
	cols := (width + 15) / 16
	required := rows * cols

	src := testImage(width, height)
	for r := range height {
		for c := range width {
			src.Y[r*src.YStride+c] = byte(48 + (r*3+c*13)%144)
			src.U[(r/2)*src.UStride+(c/2)] = 128
			src.V[(r/2)*src.VStride+(c/2)] = 128
		}
	}

	e := newSizedTestEncoder(t, width, height)
	for r := 0; r < e.analysis.Img.CodedHeight; r++ {
		for c := 0; c < e.analysis.Img.CodedWidth; c++ {
			e.analysis.Img.Y[r*e.analysis.Img.YStride+c] = byte(64 + (r*5+c*7)%128)
		}
	}
	for i := range e.analysis.Img.U {
		e.analysis.Img.U[i] = 128
	}
	for i := range e.analysis.Img.V {
		e.analysis.Img.V[i] = 128
	}
	for i := range required {
		e.reconstructModes[i] = vp8dec.MacroblockMode{
			Mode:     vp8common.DCPred,
			UVMode:   vp8common.DCPred,
			RefFrame: vp8common.LastFrame,
		}
	}

	ctx := e.newLoopFilterPickContext(sourceImageFromPublic(src), vp8common.InterFrame, 0, rows, cols, required, vp8enc.SegmentationConfig{})
	var stats EncoderPhaseStats
	if got, want := ctx.trialLumaSSEPartialStats(24, &stats), ctx.trialLumaSSEPartial(24); got != want {
		t.Fatalf("partial stats SSE = %d, no-stats SSE = %d", got, want)
	}
	if got, want := ctx.trialLumaSSEFullStats(24, &stats), ctx.trialLumaSSEFull(24); got != want {
		t.Fatalf("full stats SSE = %d, no-stats SSE = %d", got, want)
	}
	if stats.LoopFilterTrials != 2 {
		t.Fatalf("LoopFilterTrials = %d, want 2", stats.LoopFilterTrials)
	}
}

func TestLoopFilterPickNoStatsAllocatesZero(t *testing.T) {
	const width, height = 64, 64
	rows := (height + 15) / 16
	cols := (width + 15) / 16
	required := rows * cols

	src := testImage(width, height)
	for r := range height {
		for c := range width {
			src.Y[r*src.YStride+c] = byte(48 + (r*3+c*13)%144)
			src.U[(r/2)*src.UStride+(c/2)] = 128
			src.V[(r/2)*src.VStride+(c/2)] = 128
		}
	}

	e := newSizedTestEncoder(t, width, height)
	e.rc.currentQuantizer = 48
	for r := 0; r < e.analysis.Img.CodedHeight; r++ {
		for c := 0; c < e.analysis.Img.CodedWidth; c++ {
			e.analysis.Img.Y[r*e.analysis.Img.YStride+c] = byte(64 + (r*5+c*7)%128)
		}
	}
	for i := range e.analysis.Img.U {
		e.analysis.Img.U[i] = 128
	}
	for i := range e.analysis.Img.V {
		e.analysis.Img.V[i] = 128
	}
	for i := range required {
		e.reconstructModes[i] = vp8dec.MacroblockMode{
			Mode:     vp8common.DCPred,
			UVMode:   vp8common.DCPred,
			RefFrame: vp8common.LastFrame,
		}
	}

	ctx := e.newLoopFilterPickContext(sourceImageFromPublic(src), vp8common.InterFrame, 0, rows, cols, required, vp8enc.SegmentationConfig{})
	minLevel := vp8enc.LibvpxMinLoopFilterLevel(e.rc.currentQuantizer)
	allocs := testing.AllocsPerRun(50, func() {
		if _, err := ctx.pickFastNoStats(24, minLevel); err != nil {
			t.Fatalf("pickFastNoStats: %v", err)
		}
		if _, err := ctx.pickFullNoStats(24, minLevel); err != nil {
			t.Fatalf("pickFullNoStats: %v", err)
		}
	})
	if allocs != 0 {
		t.Fatalf("loop-filter no-stats pick allocs = %f, want 0", allocs)
	}
}
