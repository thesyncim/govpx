package govpx

import (
	"bytes"
	"errors"
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

func TestEncodeIntoBufferTooSmall(t *testing.T) {
	e := newTestEncoder(t)

	_, err := e.EncodeInto(nil, testImage(16, 16), 0, 1, 0)
	if !errors.Is(err, ErrBufferTooSmall) {
		t.Fatalf("error = %v, want ErrBufferTooSmall", err)
	}
}

func TestEncodeIntoWritesDecodableKeyFrame(t *testing.T) {
	e := newTestEncoder(t)
	dst := make([]byte, 4096)

	result, err := e.EncodeInto(dst, testImage(16, 16), 22, 3, 0)
	if err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}
	if len(result.Data) == 0 || result.SizeBytes != len(result.Data) || !result.KeyFrame || result.PTS != 22 || result.Duration != 3 {
		t.Fatalf("EncodeResult = %+v, want populated keyframe result", result)
	}
	if e.frameCount != 1 {
		t.Fatalf("frameCount = %d, want 1", e.frameCount)
	}

	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(result.Data); err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("NextFrame returned no frame")
	}
	if frame.Width != 16 || frame.Height != 16 || frame.Y[0] >= 128 {
		t.Fatalf("decoded frame = %dx%d Y0=%d, want 16x16 dark source-directed frame", frame.Width, frame.Height, frame.Y[0])
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

func TestLibvpxInitialLoopFilterLevelUsesBaseQThreeEighths(t *testing.T) {
	tests := []struct {
		qIndex int
		want   int
	}{
		{qIndex: 0, want: 0},
		{qIndex: 6, want: 2},
		{qIndex: 16, want: 6},
		{qIndex: 20, want: 7},
		{qIndex: 127, want: 47},
		{qIndex: 1000, want: 63},
	}
	for _, tt := range tests {
		if got := libvpxInitialLoopFilterLevel(tt.qIndex); got != tt.want {
			t.Fatalf("q=%d loop filter level = %d, want %d", tt.qIndex, got, tt.want)
		}
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

func TestLoopFilterPartialFrameWindowMirrorsLibvpxMiddleSlice(t *testing.T) {
	tests := []struct {
		rows      int
		wantStart int
		wantCount int
	}{
		{rows: 0, wantStart: 0, wantCount: 0},
		{rows: 1, wantStart: 0, wantCount: 1},
		{rows: 2, wantStart: 1, wantCount: 1},
		{rows: 4, wantStart: 2, wantCount: 1},
		{rows: 8, wantStart: 4, wantCount: 1},
		{rows: 16, wantStart: 8, wantCount: 2},
	}
	for _, tt := range tests {
		start, count := loopFilterPartialFrameWindow(tt.rows)
		if start != tt.wantStart || count != tt.wantCount {
			t.Fatalf("rows=%d partial window = %d,%d want %d,%d", tt.rows, start, count, tt.wantStart, tt.wantCount)
		}
	}
}

func TestLoopFilterLumaSSEPartialScoresOnlyMiddleWindow(t *testing.T) {
	src := testImage(64, 64)
	fillImage(src, 20, 128, 128)
	ref := testVP8Frame(t, 64, 64, 20, 128, 128)
	for row := range 16 {
		for col := range 64 {
			ref.Img.Y[row*ref.Img.YStride+col] = 100
		}
	}
	for row := 32; row < 48; row++ {
		for col := range 64 {
			ref.Img.Y[row*ref.Img.YStride+col] = 23
		}
	}

	got := loopFilterLumaSSE(sourceImageFromPublic(src), &ref.Img, 4, 4, true)
	want := 4 * 16 * 16 * 3 * 3
	if got != want {
		t.Fatalf("partial luma SSE = %d, want %d", got, want)
	}
}

func TestCopyLoopFilterPartialLumaCopiesLibvpxStrideWindow(t *testing.T) {
	src := testVP8Frame(t, 64, 128, 10, 128, 128)
	dst := testVP8Frame(t, 64, 128, 99, 128, 128)
	for i := range src.Img.YFull {
		src.Img.YFull[i] = byte(i*17 + 3)
	}
	for i := range dst.Img.YFull {
		dst.Img.YFull[i] = 0
	}

	startRow, rowCount := loopFilterPartialFrameWindow(8)
	copyLoopFilterPartialLuma(&dst.Img, &src.Img, startRow, rowCount)

	startY := startRow*16 - 4
	endY := (startRow + rowCount) * 16
	for y := startY; y < endY; y++ {
		srcOff := src.Img.YOrigin + y*src.Img.YStride
		dstOff := dst.Img.YOrigin + y*dst.Img.YStride
		got := dst.Img.YFull[dstOff : dstOff+dst.Img.YStride]
		want := src.Img.YFull[srcOff : srcOff+src.Img.YStride]
		if !bytes.Equal(got, want) {
			t.Fatalf("copied row %d differs from libvpx y_stride window", y)
		}
	}

	beforeOff := dst.Img.YOrigin + (startY-1)*dst.Img.YStride
	if bytes.Equal(dst.Img.YFull[beforeOff:beforeOff+dst.Img.YStride], src.Img.YFull[src.Img.YOrigin+(startY-1)*src.Img.YStride:src.Img.YOrigin+startY*src.Img.YStride]) {
		t.Fatalf("row before partial window was copied")
	}
}

func TestCopyLoopFilterPartialLumaCopiesLibvpxTopContext(t *testing.T) {
	src := testVP8Frame(t, 16, 16, 10, 128, 128)
	dst := testVP8Frame(t, 16, 16, 99, 128, 128)
	for i := range src.Img.YFull {
		src.Img.YFull[i] = byte(i*17 + 3)
	}
	for i := range dst.Img.YFull {
		dst.Img.YFull[i] = 0
	}

	startRow, rowCount := loopFilterPartialFrameWindow(1)
	copyLoopFilterPartialLuma(&dst.Img, &src.Img, startRow, rowCount)

	top := src.Img.YFull[src.Img.YOrigin : src.Img.YOrigin+src.Img.YStride]
	for y := -4; y < 0; y++ {
		dstOff := dst.Img.YOrigin + y*dst.Img.YStride
		if got := dst.Img.YFull[dstOff : dstOff+dst.Img.YStride]; !bytes.Equal(got, top) {
			t.Fatalf("top context row %d differs from libvpx top-row fill", y)
		}
	}
	for y := range 16 {
		srcOff := src.Img.YOrigin + y*src.Img.YStride
		dstOff := dst.Img.YOrigin + y*dst.Img.YStride
		got := dst.Img.YFull[dstOff : dstOff+dst.Img.YStride]
		want := src.Img.YFull[srcOff : srcOff+src.Img.YStride]
		if !bytes.Equal(got, want) {
			t.Fatalf("visible row %d differs from libvpx y_stride window", y)
		}
	}
	beforeOff := dst.Img.YOrigin - 5*dst.Img.YStride
	if bytes.Equal(dst.Img.YFull[beforeOff:beforeOff+dst.Img.YStride], top) {
		t.Fatalf("row before top context was copied")
	}
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
		fullPartialWindow := loopFilterLumaSSE(srcImg, &e.loopFilterPick.Img, rows, cols, true)
		_ = fullErr
		if partialErr != fullPartialWindow {
			t.Fatalf("level=%d partial SSE = %d, full-frame partial-window SSE = %d", level, partialErr, fullPartialWindow)
		}
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
	got, err := partialCtx.pickFast(24, libvpxMinLoopFilterLevel(ePartial.rc.currentQuantizer))
	if err != nil {
		t.Fatalf("loopFilterPickContext.pickFast returned error: %v", err)
	}

	// Reference: search the same neighborhood as fast search but using the
	// full-frame loop filter and partial-window SSE. Selected level must
	// match exactly.
	eRef := buildEncoder()
	minLevel := libvpxMinLoopFilterLevel(eRef.rc.currentQuantizer)
	maxLevel := libvpxMaxLoopFilterLevel(eRef.rc.currentQuantizer)
	level := clampLoopFilterPickLevel(24, minLevel, maxLevel)
	bestLevel := level
	refCtx := eRef.newLoopFilterPickContext(srcImg, vp8common.InterFrame, 0, rows, cols, required, vp8enc.SegmentationConfig{})
	score := func(lvl int) int {
		refCtx.trialLumaSSE(lvl, false)
		return loopFilterLumaSSE(srcImg, &eRef.loopFilterPick.Img, rows, cols, true)
	}
	bestErr := score(level)
	filtLevel := level - loopFilterSearchStep(level)
	for filtLevel >= minLevel {
		filtErr := score(filtLevel)
		if filtErr < bestErr {
			bestErr = filtErr
			bestLevel = filtLevel
		} else {
			break
		}
		filtLevel -= loopFilterSearchStep(filtLevel)
	}
	filtLevel = level + loopFilterSearchStep(filtLevel)
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
			filtLevel += loopFilterSearchStep(filtLevel)
		}
	}
	want := uint8(clampLoopFilterPickLevel(bestLevel, minLevel, maxLevel))
	if got != want {
		t.Fatalf("fast pick = %d, full-frame baseline = %d", got, want)
	}
}
