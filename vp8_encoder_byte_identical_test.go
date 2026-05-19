package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"testing"
)

// TestEncoderByteIdenticalHash drives a deterministic 720p realtime cpu-used=8
// encode and prints the SHA-256 of the concatenated packet bytes. The campaign
// in docs/libvpx_performance_gap_plan_2026-05-09.md follow-up commits to
// bit-identical output; this test is the harness that proves it by running
// the same fixture before and after a change and comparing the hash.
//
// Run:
//
//	go test -run TestEncoderByteIdenticalHash -v -count=1 .
//
// Pass -count=N to confirm the hash is stable across runs. Set the environment
// variable GOVPX_BYTEID_FRAMES (default 60) to encode more or fewer frames.
func TestEncoderByteIdenticalHash(t *testing.T) {
	const (
		width  = 1280
		height = 720
	)
	frames := 60
	if v := os.Getenv("GOVPX_BYTEID_FRAMES"); v != "" {
		if n, err := fmt.Sscanf(v, "%d", &frames); err != nil || n != 1 {
			t.Fatalf("GOVPX_BYTEID_FRAMES=%q: %v", v, err)
		}
	}

	e, err := NewVP8Encoder(EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   2500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		DropFrameAllowed:    false,
		Deadline:            DeadlineRealtime,
		CpuUsed:             8,
		KeyFrameInterval:    120,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		Threads:             1,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}

	img := testImage(width, height)
	fillFrame := func(index int) {
		for i := range img.Y {
			img.Y[i] = byte((i*7 + index*13) & 0xFF)
		}
		for i := range img.U {
			img.U[i] = byte(96 + ((i + index*3) & 0x3F))
		}
		for i := range img.V {
			img.V[i] = byte(144 + ((i*2 + index*5) & 0x3F))
		}
	}

	buf := make([]byte, width*height*4)
	h := sha256.New()
	totalBytes := 0
	for i := 0; i < frames; i++ {
		fillFrame(i)
		result, err := e.EncodeInto(buf, img, uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto frame %d: %v", i, err)
		}
		if result.Dropped {
			continue
		}
		h.Write(result.Data)
		totalBytes += result.SizeBytes
	}
	t.Logf("byte-identical hash frames=%d bytes=%d sha256=%s", frames, totalBytes, hex.EncodeToString(h.Sum(nil)))
}
