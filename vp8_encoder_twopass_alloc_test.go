package govpx

import "testing"

func TestCollectFirstPassStatsAndTwoPassSceneCut(t *testing.T) {
	const (
		width  = 256
		height = 256
	)
	firstPass, err := NewVP8Encoder(EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 1200,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  120,
	})
	if err != nil {
		t.Fatalf("first-pass NewVP8Encoder returned error: %v", err)
	}
	frames := make([]Image, 12)
	stats := make([]FirstPassFrameStats, len(frames))
	fillScene := func(img Image, base int) {
		for y := 0; y < img.Height; y++ {
			for x := 0; x < img.Width; x++ {
				img.Y[y*img.YStride+x] = byte(base + ((x*17 + y*31 + x*y*3) & 63))
			}
		}
		for i := range img.U {
			img.U[i] = 90
			img.V[i] = 170
		}
	}
	for i := range frames {
		frames[i] = testImage(width, height)
		if i < 5 {
			fillScene(frames[i], 20)
		} else {
			fillScene(frames[i], 150)
		}
		stats[i], err = firstPass.CollectFirstPassStats(frames[i], uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("CollectFirstPassStats %d returned error: %v", i, err)
		}
	}
	if !libvpxTestCandidateKeyFrame(stats, 5) {
		t.Fatalf("first-pass stats did not satisfy libvpx candidate keyframe test at scene cut: prev=%+v cut=%+v next=%+v", stats[4], stats[5], stats[6])
	}

	e, err := NewVP8Encoder(EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 1200,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  120,
		TwoPassStats:      stats,
		TwoPassMinPct:     50,
		TwoPassMaxPct:     200,
	})
	if err != nil {
		t.Fatalf("second-pass NewVP8Encoder returned error: %v", err)
	}
	dst := make([]byte, 512*1024)
	var result EncodeResult
	for i, frame := range frames[:6] {
		result, err = e.EncodeInto(dst, frame, uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto frame %d returned error: %v", i, err)
		}
	}
	if !result.KeyFrame || !result.SceneCut || result.PTS != 5 || result.TwoPassFrameTargetBits == 0 {
		t.Fatalf("scene-cut result = key:%t scene:%t pts:%d target:%d, want two-pass scene-cut keyframe", result.KeyFrame, result.SceneCut, result.PTS, result.TwoPassFrameTargetBits)
	}
}

func TestEncoderCloseAllocatesZero(t *testing.T) {
	e := newTestEncoder(t)
	e.closed = false
	allocs := testing.AllocsPerRun(1000, func() {
		e.closed = false
		_ = e.Close()
	})
	if allocs != 0 {
		t.Fatalf("Close allocs = %v, want 0", allocs)
	}
}
