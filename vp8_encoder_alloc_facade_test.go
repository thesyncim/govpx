package govpx_test

import (
	"fmt"
	"testing"

	"github.com/thesyncim/govpx"
)

func TestVP8EncoderPublicHotPathAllocatesZero(t *testing.T) {
	e := newVP8FacadeEncoder(t)
	dst := make([]byte, 1)
	src := newVP8FacadeImage(16, 16)
	cfg := govpx.RateControlConfig{
		Mode:                govpx.RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		UndershootPct:       100,
		OvershootPct:        100,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		DropFrameAllowed:    true,
		DropFrameWaterMark:  50,
	}
	temporal := govpx.TemporalScalabilityConfig{Enabled: true, Mode: govpx.TemporalLayeringTwoLayers}

	tests := []struct {
		name string
		fn   func()
	}{
		{name: "EncodeInto", fn: func() { _, _ = e.EncodeInto(dst, src, 0, 1, 0) }},
		{name: "SetBitrateKbps", fn: func() { _ = e.SetBitrateKbps(1200) }},
		{name: "SetRateControl", fn: func() { _ = e.SetRateControl(cfg) }},
		{name: "SetCQLevel", fn: func() { _ = e.SetCQLevel(10) }},
		{name: "SetMaxIntraBitratePct", fn: func() { _ = e.SetMaxIntraBitratePct(200) }},
		{name: "SetGFCBRBoostPct", fn: func() { _ = e.SetGFCBRBoostPct(100) }},
		{name: "SetRateControlBuffer", fn: func() { _ = e.SetRateControlBuffer(600, 400, 500) }},
		{name: "SetTokenPartitions", fn: func() { _ = e.SetTokenPartitions(3) }},
		{name: "SetErrorResilient", fn: func() { _ = e.SetErrorResilient(true, true) }},
		{name: "SetSharpness", fn: func() { _ = e.SetSharpness(3) }},
		{name: "SetStaticThreshold", fn: func() { _ = e.SetStaticThreshold(1) }},
		{name: "SetScreenContentMode", fn: func() { _ = e.SetScreenContentMode(1) }},
		{name: "SetRTCExternalRateControl", fn: func() { _ = e.SetRTCExternalRateControl(true) }},
		{name: "SetFrameDropAllowed", fn: func() { _ = e.SetFrameDropAllowed(true) }},
		{name: "SetRealtimeTarget", fn: func() { _ = e.SetRealtimeTarget(govpx.RealtimeTarget{FPS: 30}) }},
		{name: "SetTemporalScalability", fn: func() { _ = e.SetTemporalScalability(temporal) }},
		{name: "SetTemporalLayerID", fn: func() { _ = e.SetTemporalLayerID(1) }},
		{name: "SetDeadline", fn: func() { _ = e.SetDeadline(govpx.DeadlineRealtime) }},
		{name: "SetCPUUsed", fn: func() { _ = e.SetCPUUsed(8) }},
		{name: "SetKeyFrameInterval", fn: func() { _ = e.SetKeyFrameInterval(120) }},
		{name: "SetAdaptiveKeyFrames", fn: func() { _ = e.SetAdaptiveKeyFrames(true) }},
		{name: "SetNoiseSensitivity", fn: func() { _ = e.SetNoiseSensitivity(2) }},
		{name: "SetARNR", fn: func() { _ = e.SetARNR(3, 4, 3) }},
		{name: "SetTwoPassStats", fn: func() { _ = e.SetTwoPassStats(nil) }},
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

func TestVP8EncoderEncodeIntoSuccessAllocatesZero(t *testing.T) {
	e := newVP8FacadeEncoder(t)
	dst := make([]byte, 4096)
	src := newVP8FacadeImage(16, 16)
	allocs := testing.AllocsPerRun(1000, func() {
		_, _ = e.EncodeInto(dst, src, 0, 1, 0)
	})
	if allocs != 0 {
		t.Fatalf("EncodeInto success allocs = %v, want 0", allocs)
	}
}

func TestVP8EncoderTemporalEncodeIntoSuccessAllocatesZero(t *testing.T) {
	e := newTemporalVP8FacadeEncoder(t, govpx.TemporalScalabilityConfig{
		Enabled: true,
		Mode:    govpx.TemporalLayeringTwoLayers,
	})
	dst := make([]byte, 4096)
	src := newVP8FacadeImage(16, 16)
	allocs := testing.AllocsPerRun(1000, func() {
		_, _ = e.EncodeInto(dst, src, 0, 1, 0)
	})
	if allocs != 0 {
		t.Fatalf("temporal EncodeInto success allocs = %v, want 0", allocs)
	}
}

func TestVP8EncoderMultiSizeInterFrameAllocatesZero(t *testing.T) {
	cases := []struct {
		name string
		w, h int
	}{
		{"64x64", 64, 64},
		{"128x128", 128, 128},
		{"320x240", 320, 240},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newSizedVP8FacadeEncoder(t, tc.w, tc.h)
			defer e.Close()
			if err := e.SetKeyFrameInterval(0); err != nil {
				t.Fatalf("SetKeyFrameInterval returned error: %v", err)
			}
			src := newVP8FacadeImage(tc.w, tc.h)
			fillVP8FacadeImage(src, 220, 90, 170)
			dst := make([]byte, tc.w*tc.h*6+4096)
			if _, err := e.EncodeInto(dst, src, 0, 1, 0); err != nil {
				t.Fatalf("key EncodeInto returned error: %v", err)
			}
			for i := range 4 {
				if _, err := e.EncodeInto(dst, src, uint64(i+1), 1, 0); err != nil {
					t.Fatalf("warmup EncodeInto returned error: %v", err)
				}
			}
			pts := uint64(64)
			allocs := testing.AllocsPerRun(64, func() {
				_, _ = e.EncodeInto(dst, src, pts, 1, 0)
				pts++
			})
			if allocs != 0 {
				t.Fatalf("inter-frame EncodeInto allocs = %v at %s, want 0", allocs, tc.name)
			}
		})
	}
}

func TestVP8EncoderMultiResolutionAllocatesZero(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping multi-resolution alloc sweep in -short")
	}
	resolutions := []struct {
		name string
		w, h int
	}{
		{"320x240", 320, 240},
		{"640x480", 640, 480},
		{"1280x720", 1280, 720},
		{"1920x1080", 1920, 1080},
	}
	cpuBands := []int{0, 3, 5, 8, 15}
	for _, rc := range resolutions {
		for _, cpu := range cpuBands {
			t.Run(fmt.Sprintf("%s/cpu=%d", rc.name, cpu), func(t *testing.T) {
				e := newSizedVP8FacadeEncoder(t, rc.w, rc.h)
				defer e.Close()
				if err := e.SetCPUUsed(cpu); err != nil {
					t.Fatalf("SetCPUUsed(%d) returned error: %v", cpu, err)
				}
				if err := e.SetKeyFrameInterval(0); err != nil {
					t.Fatalf("SetKeyFrameInterval returned error: %v", err)
				}
				const frames = 6
				srcs := make([]govpx.Image, frames)
				for i := range srcs {
					srcs[i] = vp8FacadeAllocFrame(rc.w, rc.h, i)
				}
				dst := make([]byte, rc.w*rc.h*6+4096)
				if _, err := e.EncodeInto(dst, srcs[0], 0, 1, 0); err != nil {
					t.Fatalf("key EncodeInto returned error: %v", err)
				}
				for i := 1; i < frames; i++ {
					if _, err := e.EncodeInto(dst, srcs[i], uint64(i), 1, 0); err != nil {
						t.Fatalf("warmup inter EncodeInto returned error: %v", err)
					}
				}
				pts := uint64(frames)
				idx := 0
				allocs := testing.AllocsPerRun(20, func() {
					_, _ = e.EncodeInto(dst, srcs[idx%frames], pts, 1, 0)
					idx++
					pts++
				})
				if allocs != 0 {
					t.Fatalf("inter-frame EncodeInto allocs = %v at %s cpu=%d, want 0", allocs, rc.name, cpu)
				}
			})
		}
	}
}

func TestVP8EncoderMultiTokenPartitionAllocatesZero(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping multi-token-partition alloc sweep in -short")
	}
	cases := []struct {
		name      string
		partition int
	}{
		{"1part", 0},
		{"2parts", 1},
		{"4parts", 2},
		{"8parts", 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newSizedVP8FacadeEncoder(t, 320, 240)
			defer e.Close()
			if err := e.SetTokenPartitions(tc.partition); err != nil {
				t.Fatalf("SetTokenPartitions(%d): %v", tc.partition, err)
			}
			if err := e.SetKeyFrameInterval(0); err != nil {
				t.Fatalf("SetKeyFrameInterval: %v", err)
			}
			src := newVP8FacadeImage(320, 240)
			fillVP8FacadeImage(src, 220, 90, 170)
			dst := make([]byte, 320*240*6+4096)
			if _, err := e.EncodeInto(dst, src, 0, 1, 0); err != nil {
				t.Fatalf("key EncodeInto: %v", err)
			}
			for i := 1; i <= 6; i++ {
				if _, err := e.EncodeInto(dst, src, uint64(i), 1, 0); err != nil {
					t.Fatalf("warmup EncodeInto: %v", err)
				}
			}
			pts := uint64(7)
			allocs := testing.AllocsPerRun(20, func() {
				_, _ = e.EncodeInto(dst, src, pts, 1, 0)
				pts++
			})
			if allocs != 0 {
				t.Fatalf("EncodeInto allocs/op = %v at TokenPartitions=%d, want 0", allocs, tc.partition)
			}
		})
	}
}

func BenchmarkVP8EncoderEncodeInto(b *testing.B) {
	resolutions := []struct {
		name string
		w, h int
	}{
		{"320x240", 320, 240},
		{"640x480", 640, 480},
		{"1280x720", 1280, 720},
		{"1920x1080", 1920, 1080},
	}
	for _, rc := range resolutions {
		b.Run(rc.name, func(b *testing.B) {
			e := newSizedVP8FacadeEncoder(b, rc.w, rc.h)
			defer e.Close()
			if err := e.SetKeyFrameInterval(0); err != nil {
				b.Fatalf("SetKeyFrameInterval returned error: %v", err)
			}
			const cycle = 6
			srcs := make([]govpx.Image, cycle)
			for i := range srcs {
				srcs[i] = vp8FacadeAllocFrame(rc.w, rc.h, i)
			}
			dst := make([]byte, rc.w*rc.h*6+4096)
			if _, err := e.EncodeInto(dst, srcs[0], 0, 1, 0); err != nil {
				b.Fatalf("key EncodeInto returned error: %v", err)
			}
			for i := 1; i < cycle; i++ {
				if _, err := e.EncodeInto(dst, srcs[i], uint64(i), 1, 0); err != nil {
					b.Fatalf("warmup EncodeInto returned error: %v", err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			pts := uint64(cycle)
			for i := 0; i < b.N; i++ {
				if _, err := e.EncodeInto(dst, srcs[i%cycle], pts, 1, 0); err != nil {
					b.Fatalf("steady-state EncodeInto returned error: %v", err)
				}
				pts++
			}
		})
	}
}

func newSizedVP8FacadeEncoder(t testing.TB, width int, height int) *govpx.VP8Encoder {
	t.Helper()
	e, err := govpx.NewVP8Encoder(govpx.EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlMode:     govpx.RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		DropFrameAllowed:    true,
		DropFrameWaterMark:  50,
		Deadline:            govpx.DeadlineRealtime,
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

func newTemporalVP8FacadeEncoder(t testing.TB, temporal govpx.TemporalScalabilityConfig) *govpx.VP8Encoder {
	t.Helper()
	e, err := govpx.NewVP8Encoder(govpx.EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     govpx.RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		DropFrameAllowed:    true,
		DropFrameWaterMark:  50,
		Deadline:            govpx.DeadlineRealtime,
		CpuUsed:             8,
		KeyFrameInterval:    120,
		ErrorResilient:      true,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		TemporalScalability: temporal,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	return e
}

func vp8FacadeAllocFrame(width int, height int, index int) govpx.Image {
	img := newVP8FacadeImage(width, height)
	for row := range height {
		for col := range width {
			img.Y[row*img.YStride+col] = byte(32 + ((row*3 + col*5 + index*7) & 191))
		}
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for row := range uvHeight {
		for col := range uvWidth {
			img.U[row*img.UStride+col] = byte(96 + ((row*2 + col + index*3) & 63))
			img.V[row*img.VStride+col] = byte(144 + ((row + col*2 + index*5) & 63))
		}
	}
	return img
}
