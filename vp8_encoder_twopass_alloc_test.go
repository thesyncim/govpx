package govpx

import (
	"fmt"
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

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

func TestConvertMacroblockCoefficientsOverwritesActiveSkippedDCBlock(t *testing.T) {
	var src vp8enc.MacroblockCoefficients
	var dst vp8dec.MacroblockTokens
	src.SetBlockEOB(0, 0)
	dst.QCoeff[0][0] = 99
	dst.QCoeff[0][1] = 77
	dst.EOB[0] = 2

	convertMacroblockCoefficients(&src, false, &dst)

	if got := dst.EOB[0]; got != 1 {
		t.Fatalf("EOB[0] = %d, want skipped-DC EOB 1", got)
	}
	if got := dst.QCoeff[0][0]; got != 0 {
		t.Fatalf("QCoeff[0][0] = %d, want active skipped DC overwritten", got)
	}
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
		DropFrameWaterMark:  defaultDropFramesWaterMark,
	}
	temporal := TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayers}

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
		{name: "SetTokenPartitions", fn: func() { _ = e.SetTokenPartitions(int(vp8common.EightPartition)) }},
		{name: "SetSharpness", fn: func() { _ = e.SetSharpness(3) }},
		{name: "SetStaticThreshold", fn: func() { _ = e.SetStaticThreshold(1) }},
		{name: "SetScreenContentMode", fn: func() { _ = e.SetScreenContentMode(1) }},
		{name: "SetRTCExternalRateControl", fn: func() { _ = e.SetRTCExternalRateControl(true) }},
		{name: "SetFrameDropAllowed", fn: func() { _ = e.SetFrameDropAllowed(true) }},
		{name: "SetRealtimeTarget", fn: func() { _ = e.SetRealtimeTarget(RealtimeTarget{FPS: 30}) }},
		{name: "SetTemporalScalability", fn: func() { _ = e.SetTemporalScalability(temporal) }},
		{name: "SetTemporalLayerID", fn: func() { _ = e.SetTemporalLayerID(1) }},
		{name: "SetDeadline", fn: func() { _ = e.SetDeadline(DeadlineRealtime) }},
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

	e.closed = false
	allocs := testing.AllocsPerRun(1000, func() {
		e.closed = false
		_ = e.Close()
	})
	if allocs != 0 {
		t.Fatalf("Close allocs = %v, want 0", allocs)
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

func TestEncodeIntoTemporalSuccessAllocatesZero(t *testing.T) {
	e := newTemporalTestEncoder(t, TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayers})
	dst := make([]byte, 4096)
	src := testImage(16, 16)
	allocs := testing.AllocsPerRun(1000, func() {
		_, _ = e.EncodeInto(dst, src, 0, 1, 0)
	})
	if allocs != 0 {
		t.Fatalf("temporal EncodeInto success allocs = %v, want 0", allocs)
	}
}

// TestEncodeIntoMultiSizeInterFrameAllocatesZero guards the per-frame
// reconstruction scratch pool added in parity-close-r10-d-allocs. The
// reconstruction builder used to allocate a fresh
// []vp8enc.TokenContextPlanes of length cols every frame, which the 16x16
// fixture happens to mask because cols==1 and the tiny slice can sit on the
// caller's stack. Anything wider (>=64x64) faithfully exercises the heap
// allocator and traps regressions in the per-row above-token scratch.
func TestEncodeIntoMultiSizeInterFrameAllocatesZero(t *testing.T) {
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
			e := newSizedTestEncoder(t, tc.w, tc.h)
			defer e.Close()
			if err := e.SetKeyFrameInterval(0); err != nil {
				t.Fatalf("SetKeyFrameInterval returned error: %v", err)
			}
			src := testImage(tc.w, tc.h)
			fillImage(src, 220, 90, 170)
			dst := make([]byte, tc.w*tc.h*6+4096)
			if _, err := e.EncodeInto(dst, src, 0, 1, 0); err != nil {
				t.Fatalf("key EncodeInto returned error: %v", err)
			}
			// Warm any one-shot lazy state so AllocsPerRun's first iteration
			// does not double-count construction allocations.
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

// TestEncodeIntoMultiResolutionAllocatesZero is the parity-close-r15-d
// regression guard for steady-state zero allocations across the full
// resolution + cpu_used matrix exercised by govpx-bench. The earlier
// TestEncodeIntoMultiSizeInterFrameAllocatesZero capped at 320x240 and only
// exercised CpuUsed=8; this covers 320x240/640x480/1280x720/1920x1080 against
// CpuUsed in {0,3,5,8,15} (Speed bands feeding libvpx_auto_select_speed plus
// the static "RT highest speed" 15 ceiling). The frames are spatial-temporal
// gradients matching cmd/govpx-bench's makeBenchmarkFrame, exercising the
// inter-mode picker, encoder match path, and rate control loops the
// flat-fill fixture above does not.
func TestEncodeIntoMultiResolutionAllocatesZero(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping multi-resolution alloc sweep in -short")
	}
	type resCase struct {
		name string
		w, h int
	}
	resolutions := []resCase{
		{"320x240", 320, 240},
		{"640x480", 640, 480},
		{"1280x720", 1280, 720},
		{"1920x1080", 1920, 1080},
	}
	cpuBands := []int{0, 3, 5, 8, 15}
	for _, rc := range resolutions {
		for _, cpu := range cpuBands {
			t.Run(fmt.Sprintf("%s/cpu=%d", rc.name, cpu), func(t *testing.T) {
				e := newSizedTestEncoder(t, rc.w, rc.h)
				defer e.Close()
				if err := e.SetCPUUsed(cpu); err != nil {
					t.Fatalf("SetCPUUsed(%d) returned error: %v", cpu, err)
				}
				if err := e.SetKeyFrameInterval(0); err != nil {
					t.Fatalf("SetKeyFrameInterval returned error: %v", err)
				}
				const frames = 6
				srcs := make([]Image, frames)
				for i := range srcs {
					srcs[i] = makeMultiResAllocFrame(rc.w, rc.h, i)
				}
				dst := make([]byte, rc.w*rc.h*6+4096)
				// Encode the keyframe + a few inter frames so that any
				// lazily-initialised per-frame state (recode scratch, segment
				// buffers, inter-mode bookkeeping) is warm before
				// AllocsPerRun starts counting.
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

// makeMultiResAllocFrame mirrors cmd/govpx-bench/main.go::makeBenchmarkFrame
// so the alloc regression guard touches the same picker / rate-control paths
// as the bench harness that originally exposed the per-frame allocations.
// Keeping this helper local to vp8_encoder_test.go avoids importing the bench
// package and the resulting test-only dependency cycle.
func makeMultiResAllocFrame(width int, height int, index int) Image {
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	img := Image{
		Width:   width,
		Height:  height,
		Y:       make([]byte, width*height),
		U:       make([]byte, uvWidth*uvHeight),
		V:       make([]byte, uvWidth*uvHeight),
		YStride: width,
		UStride: uvWidth,
		VStride: uvWidth,
	}
	for row := range height {
		for col := range width {
			img.Y[row*img.YStride+col] = byte(32 + ((row*3 + col*5 + index*7) & 191))
		}
	}
	for row := range uvHeight {
		for col := range uvWidth {
			img.U[row*img.UStride+col] = byte(96 + ((row*2 + col + index*3) & 63))
			img.V[row*img.VStride+col] = byte(144 + ((row + col*2 + index*5) & 63))
		}
	}
	return img
}

// TestEncodeIntoMultiTokenPartitionAllocatesZero locks in the
// parity-close-r15-d-v2 partition-buffer pool (PartitionScratch on
// VP8Encoder). Multi-token-partition encodes (libvpx --token-parts=N>0)
// previously allocated N+1 objects per frame: one byte buffer per
// partition plus the closure passed into writePartitionedTokenPayload.
// The new prepare/finalize split routes through e.partScratch, so the
// steady-state alloc count is 0 across all four supported token-partition
// modes (1/2/4/8 partitions = TokenPartitions in {0,1,2,3}).
func TestEncodeIntoMultiTokenPartitionAllocatesZero(t *testing.T) {
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
			e := newSizedTestEncoder(t, 320, 240)
			defer e.Close()
			if err := e.SetTokenPartitions(tc.partition); err != nil {
				t.Fatalf("SetTokenPartitions(%d): %v", tc.partition, err)
			}
			if err := e.SetKeyFrameInterval(0); err != nil {
				t.Fatalf("SetKeyFrameInterval: %v", err)
			}
			src := testImage(320, 240)
			fillImage(src, 220, 90, 170)
			dst := make([]byte, 320*240*6+4096)
			if _, err := e.EncodeInto(dst, src, 0, 1, 0); err != nil {
				t.Fatalf("key EncodeInto: %v", err)
			}
			// Warm the partition scratch + any other lazy state.
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

// BenchmarkEncodeInto is the parity-close-r15-d alloc-tracking sweep across
// the same resolutions that TestEncodeIntoMultiResolutionAllocatesZero
// guards. Run with `-benchmem -count=10 -benchtime=200x` to confirm
// allocs/op == 0 at all sizes after the encoder is warm. Each subtest covers
// a single resolution with CpuUsed=8 (the bench-harness default); the
// AllocsPerRun test above sweeps the CpuUsed band.
func BenchmarkEncodeInto(b *testing.B) {
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
			e := newSizedTestEncoder(b, rc.w, rc.h)
			defer e.Close()
			if err := e.SetKeyFrameInterval(0); err != nil {
				b.Fatalf("SetKeyFrameInterval returned error: %v", err)
			}
			const cycle = 6
			srcs := make([]Image, cycle)
			for i := range srcs {
				srcs[i] = makeMultiResAllocFrame(rc.w, rc.h, i)
			}
			dst := make([]byte, rc.w*rc.h*6+4096)
			// Warm the encoder so the steady-state hot path is what the
			// benchmark measures (matches govpx-bench which also reads
			// MemStats only after a warm pre-pass).
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
