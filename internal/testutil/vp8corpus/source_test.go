package vp8corpus

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVP8SourceCorpusRootAndLimitsUseEnvironment(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOVPX_ENCODER_TEST_DATA_PATH", dir)
	root, ok := SourceRoot(t)
	if !ok || root != dir {
		t.Fatalf("SourceRoot = %q, %t; want %q, true", root, ok, dir)
	}

	t.Setenv("GOVPX_ENCODER_TEST_DATA_FRAMES", "4")
	if got := SourceFrameLimit(t); got != 4 {
		t.Fatalf("SourceFrameLimit = %d, want 4", got)
	}
	t.Setenv("GOVPX_ENCODER_TEST_DATA_LIMIT", "1")
	if got := sourceLimit(t); got != 1 {
		t.Fatalf("sourceLimit = %d, want 1", got)
	}
}

func TestVP8SourceCorpusFindSourcesFiltersAndLimits(t *testing.T) {
	dir := t.TempDir()
	y4m := filepath.Join(dir, "a.y4m")
	yuv := filepath.Join(dir, "b_4x2.yuv")
	txt := filepath.Join(dir, "note.txt")
	for _, path := range []string{y4m, yuv, txt} {
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatalf("WriteFile %s returned error: %v", path, err)
		}
	}

	t.Setenv("GOVPX_ENCODER_TEST_DATA_LIMIT", "1")
	paths := FindSources(t, dir)
	if len(paths) != 1 || paths[0] != y4m {
		t.Fatalf("FindSources = %v, want [%s]", paths, y4m)
	}
}

func TestVP8SourceCorpusReadsY4MClip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tiny.y4m")
	frameSize := 4*2 + 2*(2*1)
	data := []byte("YUV4MPEG2 W4 H2 F30:1 Ip A0:0 C420jpeg\nFRAME\n")
	for i := range frameSize {
		data = append(data, byte(i))
	}
	data = append(data, []byte("FRAME\n")...)
	for i := range frameSize {
		data = append(data, byte(i+20))
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	clip, ok := ReadSourceClip(t, path, 2)
	if !ok {
		t.Fatalf("ReadSourceClip ok=false, want true")
	}
	if clip.Width != 4 || clip.Height != 2 || clip.FPS != 30 || len(clip.Frames) != 2 {
		t.Fatalf("clip = %dx%d fps=%d frames=%d, want 4x2 fps=30 frames=2", clip.Width, clip.Height, clip.FPS, len(clip.Frames))
	}
	frame := clip.Frames[1]
	if frame.Y[0] != 20 || frame.Cb[0] != 28 || frame.Cr[0] != 30 {
		t.Fatalf("frame 1 samples YUV=%d/%d/%d, want 20/28/30", frame.Y[0], frame.Cb[0], frame.Cr[0])
	}
}

func TestVP8SourceCorpusReadsRawI420Clip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tiny_4x2.yuv")
	frameSize := 4*2 + 2*(2*1)
	data := make([]byte, frameSize)
	for i := range data {
		data[i] = byte(i + 3)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	clip, ok := ReadSourceClip(t, path, 2)
	if !ok {
		t.Fatalf("ReadSourceClip ok=false, want true")
	}
	if clip.Width != 4 || clip.Height != 2 || clip.FPS != 30 || len(clip.Frames) != 1 {
		t.Fatalf("clip = %dx%d fps=%d frames=%d, want 4x2 fps=30 frames=1", clip.Width, clip.Height, clip.FPS, len(clip.Frames))
	}
	frame := clip.Frames[0]
	if frame.Y[0] != 3 || frame.Cb[0] != 11 || frame.Cr[0] != 13 {
		t.Fatalf("frame samples YUV=%d/%d/%d, want 3/11/13", frame.Y[0], frame.Cb[0], frame.Cr[0])
	}
}

func TestVP8SourceTargetKbpsFloor(t *testing.T) {
	if got := SourceTargetKbps(16, 16, 30); got != 100 {
		t.Fatalf("SourceTargetKbps small = %d, want 100", got)
	}
	if got := SourceTargetKbps(320, 240, 30); got != 1920 {
		t.Fatalf("SourceTargetKbps 320x240 = %d, want 1920", got)
	}
}
