package govpx

import "testing"

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
