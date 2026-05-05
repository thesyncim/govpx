package libgopx

import (
	"errors"
	"testing"
)

func TestNewVP8EncoderValidation(t *testing.T) {
	_, err := NewVP8Encoder(EncoderOptions{Width: 640, Height: 480, FPS: 30})
	if !errors.Is(err, ErrInvalidBitrate) {
		t.Fatalf("error = %v, want ErrInvalidBitrate", err)
	}

	_, err = NewVP8Encoder(EncoderOptions{Width: 640, Height: 480, FPS: 30, TargetBitrateKbps: 1200, MinQuantizer: 60, MaxQuantizer: 4})
	if !errors.Is(err, ErrInvalidQuantizer) {
		t.Fatalf("error = %v, want ErrInvalidQuantizer", err)
	}

	_, err = NewVP8Encoder(EncoderOptions{Width: 0, Height: 480, FPS: 30, TargetBitrateKbps: 1200})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("error = %v, want ErrInvalidConfig", err)
	}
}

func TestEncoderRateControlBitsPerFrame(t *testing.T) {
	e := newTestEncoder(t)

	if e.rc.bitsPerFrame != 40000 {
		t.Fatalf("bitsPerFrame = %d, want 40000", e.rc.bitsPerFrame)
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{FPS: 60}); err != nil {
		t.Fatalf("SetRealtimeTarget returned error: %v", err)
	}
	if e.rc.bitsPerFrame != 20000 {
		t.Fatalf("bitsPerFrame = %d, want 20000", e.rc.bitsPerFrame)
	}
	if err := e.SetBitrateKbps(600); err != nil {
		t.Fatalf("SetBitrateKbps returned error: %v", err)
	}
	if e.rc.bitsPerFrame != 10000 {
		t.Fatalf("bitsPerFrame = %d, want 10000", e.rc.bitsPerFrame)
	}
}

func TestSetRateControlValidation(t *testing.T) {
	e := newTestEncoder(t)

	err := e.SetRateControl(RateControlConfig{
		Mode:                RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        56,
		MaxQuantizer:        4,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if !errors.Is(err, ErrInvalidQuantizer) {
		t.Fatalf("error = %v, want ErrInvalidQuantizer", err)
	}
}

func TestSetRealtimeTargetRejectsResolutionChange(t *testing.T) {
	e := newTestEncoder(t)

	if err := e.SetRealtimeTarget(RealtimeTarget{Width: 32, Height: 16}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("larger resolution error = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{Width: 8, Height: 16}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("smaller resolution error = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{Width: 16, Height: 16}); err != nil {
		t.Fatalf("same resolution returned error: %v", err)
	}
}

func TestForceKeyFrameIsConsumedByNextEncodeAttempt(t *testing.T) {
	e := newTestEncoder(t)
	e.frameCount = 7
	e.ForceKeyFrame()

	result, err := e.EncodeInto(make([]byte, 4096), testImage(16, 16), 1, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}
	if !result.KeyFrame {
		t.Fatalf("KeyFrame = false, want true")
	}
	if e.forceKeyFrame {
		t.Fatalf("forceKeyFrame = true, want false")
	}
}

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

func TestEncodeIntoUsesSourcePixels(t *testing.T) {
	darkEncoder := newTestEncoder(t)
	brightEncoder := newTestEncoder(t)
	dark := testImage(16, 16)
	bright := testImage(16, 16)
	fillImage(bright, 220, 128, 128)
	dstDark := make([]byte, 4096)
	dstBright := make([]byte, 4096)

	darkResult, err := darkEncoder.EncodeInto(dstDark, dark, 0, 1, 0)
	if err != nil {
		t.Fatalf("dark EncodeInto returned error: %v", err)
	}
	brightResult, err := brightEncoder.EncodeInto(dstBright, bright, 0, 1, 0)
	if err != nil {
		t.Fatalf("bright EncodeInto returned error: %v", err)
	}

	darkFrame := decodeSingleFrame(t, darkResult.Data)
	brightFrame := decodeSingleFrame(t, brightResult.Data)
	if brightFrame.Y[0] <= darkFrame.Y[0] {
		t.Fatalf("decoded Y0 dark/bright = %d/%d, want bright greater", darkFrame.Y[0], brightFrame.Y[0])
	}
}

func TestEncodeIntoReconstructsReferencesLikeDecoder(t *testing.T) {
	e := newSizedTestEncoder(t, 32, 16)
	src := testImage(32, 16)
	fillImage(src, 220, 90, 170)
	for row := 0; row < src.Height; row++ {
		for col := 16; col < src.Width; col++ {
			src.Y[row*src.YStride+col] = 40
		}
	}
	dst := make([]byte, 8192)

	result, err := e.EncodeInto(dst, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}
	decoded := decodeSingleFrame(t, result.Data)

	assertImagesEqual(t, "current", decoded, publicImageFromVP8(&e.current.Img))
	assertImagesEqual(t, "last", decoded, publicImageFromVP8(&e.lastRef.Img))
	assertImagesEqual(t, "golden", decoded, publicImageFromVP8(&e.goldenRef.Img))
	assertImagesEqual(t, "alt", decoded, publicImageFromVP8(&e.altRef.Img))
}

func TestEncoderHotPathAllocs(t *testing.T) {
	e := newTestEncoder(t)
	dst := make([]byte, 1)
	src := testImage(16, 16)
	cfg := RateControlConfig{
		Mode:                RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		UndershootPct:       100,
		OvershootPct:        100,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		DropFrameAllowed:    true,
	}

	tests := []struct {
		name string
		fn   func()
	}{
		{name: "EncodeInto", fn: func() { _, _ = e.EncodeInto(dst, src, 0, 1, 0) }},
		{name: "SetBitrateKbps", fn: func() { _ = e.SetBitrateKbps(1200) }},
		{name: "SetRateControl", fn: func() { _ = e.SetRateControl(cfg) }},
		{name: "SetRealtimeTarget", fn: func() { _ = e.SetRealtimeTarget(RealtimeTarget{FPS: 30}) }},
		{name: "ForceKeyFrame", fn: func() { e.ForceKeyFrame() }},
		{name: "Reset", fn: func() { e.Reset() }},
	}

	for _, tt := range tests {
		allocs := testing.AllocsPerRun(1000, tt.fn)
		if allocs != 0 {
			t.Fatalf("%s allocs = %v, want 0", tt.name, allocs)
		}
	}
}

func TestEncodeIntoSuccessAllocatesZero(t *testing.T) {
	e := newTestEncoder(t)
	dst := make([]byte, 4096)
	src := testImage(16, 16)
	allocs := testing.AllocsPerRun(1000, func() {
		_, _ = e.EncodeInto(dst, src, 0, 1, 0)
	})
	if allocs != 0 {
		t.Fatalf("EncodeInto success allocs = %v, want 0", allocs)
	}
}

func newTestEncoder(t *testing.T) *VP8Encoder {
	t.Helper()
	return newSizedTestEncoder(t, 16, 16)
}

func newSizedTestEncoder(t *testing.T, width int, height int) *VP8Encoder {
	t.Helper()
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		DropFrameAllowed:    true,
		Deadline:            DeadlineRealtime,
		CpuUsed:             8,
		KeyFrameInterval:    120,
		ErrorResilient:      true,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	return e
}

func testImage(width int, height int) Image {
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	return Image{
		Width:   width,
		Height:  height,
		Y:       make([]byte, width*height),
		U:       make([]byte, uvWidth*uvHeight),
		V:       make([]byte, uvWidth*uvHeight),
		YStride: width,
		UStride: uvWidth,
		VStride: uvWidth,
	}
}

func fillImage(img Image, y byte, u byte, v byte) {
	for i := range img.Y {
		img.Y[i] = y
	}
	for i := range img.U {
		img.U[i] = u
	}
	for i := range img.V {
		img.V[i] = v
	}
}

func decodeSingleFrame(t *testing.T, packet []byte) Image {
	t.Helper()
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("NextFrame returned no frame")
	}
	return frame
}

func assertImagesEqual(t *testing.T, name string, want Image, got Image) {
	t.Helper()
	if got.Width != want.Width || got.Height != want.Height {
		t.Fatalf("%s dimensions = %dx%d, want %dx%d", name, got.Width, got.Height, want.Width, want.Height)
	}
	assertPlaneEqual(t, name+" Y", want.Y, want.YStride, got.Y, got.YStride, want.Width, want.Height)
	uvWidth := (want.Width + 1) >> 1
	uvHeight := (want.Height + 1) >> 1
	assertPlaneEqual(t, name+" U", want.U, want.UStride, got.U, got.UStride, uvWidth, uvHeight)
	assertPlaneEqual(t, name+" V", want.V, want.VStride, got.V, got.VStride, uvWidth, uvHeight)
}

func assertPlaneEqual(t *testing.T, name string, want []byte, wantStride int, got []byte, gotStride int, width int, height int) {
	t.Helper()
	for row := 0; row < height; row++ {
		wantRow := want[row*wantStride : row*wantStride+width]
		gotRow := got[row*gotStride : row*gotStride+width]
		for col := 0; col < width; col++ {
			if gotRow[col] != wantRow[col] {
				t.Fatalf("%s[%d,%d] = %d, want %d", name, row, col, gotRow[col], wantRow[col])
			}
		}
	}
}
