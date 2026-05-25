//go:build govpx_oracle_trace

package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	"testing"
)

func TestVP8OracleEncoderStreamByteParityROIMap(t *testing.T) {
	vp8test.RequireOracle(t, "ROI byte-parity gate")
	driver := vp8test.VpxencFrameFlags(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 12
		width      = 64
		height     = 64
	)
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationSegmentedFrame(width, height, i)
	}
	opts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  999,
		Deadline:          DeadlineRealtime,
		CpuUsed:           -3,
		Tuning:            TunePSNR,
	}
	govpxFrames := encodeFramesWithGovpxRuntimeControls(t, opts, sources, nil, map[int]func(*testing.T, *VP8Encoder){
		0: func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			mustRuntime(t, "SetROIMap(custom quadrants)", e.SetROIMap(customQuadrantROIMap(width, height)))
		},
	})
	libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, "roi-map-altq-altlf-static-64x64", opts, targetKbps, sources, nil, []string{
		"--roi-map=quadrants",
		"--roi-dq=0,-10,8,-20",
		"--roi-dlf=0,-3,2,5",
		"--roi-static=0,500,0,1200",
	})
	assertSegmentByteParity(t, "roi-map-altq-altlf-static", govpxFrames, libvpxFrames, 0)
}

func TestVP8OracleEncoderStreamByteParityROISimpleDeltaQ(t *testing.T) {
	vp8test.RequireOracle(t, "ROI byte-parity gate")
	driver := vp8test.VpxencFrameFlags(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 12
		width      = 32
		height     = 32
	)
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationSegmentedFrame(width, height, i)
	}
	opts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  999,
		Deadline:          DeadlineRealtime,
		CpuUsed:           -3,
		Tuning:            TunePSNR,
	}
	govpxFrames := encodeFramesWithGovpxRuntimeControls(t, opts, sources, nil, map[int]func(*testing.T, *VP8Encoder){
		0: func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			mustRuntime(t, "SetROIMap(simple checker)", e.SetROIMap(simpleCheckerROIMap(width, height)))
		},
	})
	libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, "roi-map-simple-dq-32x32", opts, targetKbps, sources, nil, []string{
		"--roi-map=checker",
		"--roi-dq=0,-10,0,0",
		"--roi-dlf=0,0,0,0",
		"--roi-static=0,0,0,0",
	})
	assertSegmentByteParity(t, "roi-map-simple-dq", govpxFrames, libvpxFrames, 0)
}

func TestVP8OracleEncoderStreamByteParityROISimpleAxes(t *testing.T) {
	vp8test.RequireOracle(t, "ROI byte-parity gate")
	driver := vp8test.VpxencFrameFlags(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 12
		width      = 32
		height     = 32
	)
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationSegmentedFrame(width, height, i)
	}
	opts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  999,
		Deadline:          DeadlineRealtime,
		CpuUsed:           -3,
		Tuning:            TunePSNR,
	}

	cases := []struct {
		name      string
		roi       func() *ROIMap
		extraArgs []string
		limit     int
	}{
		{
			name: "simple-delta-lf",
			roi: func() *ROIMap {
				roi := roiMapPattern(width, height, "checker")
				roi.DeltaQuantizer = [4]int{}
				roi.DeltaLoopFilter = [4]int{0, -3, 0, 0}
				roi.StaticThreshold = [4]int{}
				return roi
			},
			extraArgs: []string{"--roi-map=checker", "--roi-dq=0,0,0,0", "--roi-dlf=0,-3,0,0", "--roi-static=0,0,0,0"},
		},
		{
			name: "simple-static-threshold",
			roi: func() *ROIMap {
				roi := roiMapPattern(width, height, "checker")
				roi.DeltaQuantizer = [4]int{}
				roi.DeltaLoopFilter = [4]int{}
				roi.StaticThreshold = [4]int{0, 500, 0, 0}
				return roi
			},
			extraArgs: []string{"--roi-map=checker", "--roi-dq=0,0,0,0", "--roi-dlf=0,0,0,0", "--roi-static=0,500,0,0"},
		},
		{
			name: "dq-dlf-no-static",
			roi: func() *ROIMap {
				roi := roiMapPattern(width, height, "checker")
				roi.DeltaQuantizer = [4]int{0, -10, 0, 0}
				roi.DeltaLoopFilter = [4]int{0, -3, 0, 0}
				roi.StaticThreshold = [4]int{}
				return roi
			},
			extraArgs: []string{"--roi-map=checker", "--roi-dq=0,-10,0,0", "--roi-dlf=0,-3,0,0", "--roi-static=0,0,0,0"},
		},
		{
			name: "quadrants-default",
			roi: func() *ROIMap {
				roi := roiMapPattern(width, height, "quadrants")
				roi.DeltaQuantizer = [4]int{}
				roi.DeltaLoopFilter = [4]int{}
				roi.StaticThreshold = [4]int{}
				return roi
			},
			extraArgs: []string{"--roi-map=quadrants", "--roi-dq=0,0,0,0", "--roi-dlf=0,0,0,0", "--roi-static=0,0,0,0"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			govpxFrames := encodeFramesWithGovpxRuntimeControls(t, opts, sources, nil, map[int]func(*testing.T, *VP8Encoder){
				0: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap("+tc.name+")", e.SetROIMap(tc.roi()))
				},
			})
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, "roi-map-"+tc.name+"-32x32", opts, targetKbps, sources, nil, tc.extraArgs)
			assertSegmentByteParity(t, "roi-map-"+tc.name, govpxFrames, libvpxFrames, tc.limit)
		})
	}
}

func TestVP8OracleEncoderStreamByteParityROIMapOddDimensions(t *testing.T) {
	vp8test.RequireOracle(t, "ROI byte-parity gate")
	driver := vp8test.VpxencFrameFlags(t)

	const (
		fps           = 30
		targetKbps    = 700
		frames        = 10
		defaultWidth  = 65
		defaultHeight = 33
	)

	cases := []struct {
		name                     string
		pattern                  string
		width                    int
		height                   int
		limit                    int
		tokenPartitions          int
		errorResilient           bool
		errorResilientPartitions bool
		extraArgs                []string
	}{
		{name: "checker", pattern: "checker"},
		{name: "left1", pattern: "left1"},
		{name: "border1", pattern: "border1"},
		{name: "border1-er2-token4", pattern: "border1", tokenPartitions: 2, errorResilientPartitions: true, extraArgs: []string{"--error-resilient=2", "--token-parts=2"}},
		{name: "checker-er3-token8", pattern: "checker", tokenPartitions: 3, errorResilient: true, errorResilientPartitions: true, extraArgs: []string{"--error-resilient=3", "--token-parts=3"}},
		{name: "border1-17x17", pattern: "border1", width: 17, height: 17},
		{name: "checker-33x65", pattern: "checker", width: 33, height: 65},
		{name: "left1-31x48-token4", pattern: "left1", width: 31, height: 48, tokenPartitions: 2, extraArgs: []string{"--token-parts=2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			width := tc.width
			if width == 0 {
				width = defaultWidth
			}
			height := tc.height
			if height == 0 {
				height = defaultHeight
			}
			sources := make([]Image, frames)
			for i := range sources {
				sources[i] = encoderValidationSegmentedFrame(width, height, i)
			}
			opts := EncoderOptions{
				Width:             width,
				Height:            height,
				FPS:               fps,
				RateControlMode:   RateControlCBR,
				TargetBitrateKbps: targetKbps,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				KeyFrameInterval:  999,
				Deadline:          DeadlineRealtime,
				CpuUsed:           -3,
				Tuning:            TunePSNR,
			}
			opts.TokenPartitions = tc.tokenPartitions
			opts.ErrorResilient = tc.errorResilient
			opts.ErrorResilientPartitions = tc.errorResilientPartitions
			govpxFrames := encodeFramesWithGovpxRuntimeControls(t, opts, sources, nil, map[int]func(*testing.T, *VP8Encoder){
				0: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap("+tc.pattern+")", e.SetROIMap(roiMapPattern(width, height, tc.pattern)))
				},
			})
			extraArgs := []string{"--roi-map=" + tc.pattern}
			extraArgs = append(extraArgs, tc.extraArgs...)
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, "roi-map-odd-"+tc.name, opts, targetKbps, sources, nil, extraArgs)
			assertSegmentByteParity(t, "roi-map-odd-"+tc.name, govpxFrames, libvpxFrames, tc.limit)
		})
	}
}

func TestVP8OracleEncoderStreamByteParityROIMapPatterns(t *testing.T) {
	vp8test.RequireOracle(t, "ROI byte-parity gate")
	driver := vp8test.VpxencFrameFlags(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 12
		width      = 64
		height     = 64
	)
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationSegmentedFrame(width, height, i)
	}
	opts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  999,
		Deadline:          DeadlineRealtime,
		CpuUsed:           -3,
		Tuning:            TunePSNR,
	}

	cases := []struct {
		name                     string
		pattern                  string
		limit                    int
		tokenPartitions          int
		threads                  int
		noiseSensitivity         int
		screenContentMode        int
		sharpness                int
		tuning                   Tuning
		tuningSet                bool
		errorResilient           bool
		errorResilientPartitions bool
		extraArgs                []string
	}{
		{name: "checker", pattern: "checker", limit: 0},
		{name: "left1", pattern: "left1", limit: 0},
		{name: "border1", pattern: "border1", limit: 0},
		{name: "off", pattern: "off", limit: 0},
		{name: "checker-token-parts4", pattern: "checker", tokenPartitions: 2, extraArgs: []string{"--token-parts=2"}},
		{name: "left1-threads2", pattern: "left1", threads: 2, extraArgs: []string{"--threads=2"}},
		{name: "checker-noise3", pattern: "checker", noiseSensitivity: 3, extraArgs: []string{"--noise-sensitivity=3"}},
		{name: "border1-noise6", pattern: "border1", noiseSensitivity: 6, extraArgs: []string{"--noise-sensitivity=6"}},
		{name: "checker-screen-content2", pattern: "checker", screenContentMode: 2, extraArgs: []string{"--screen-content-mode=2"}},
		{name: "border1-sharpness4", pattern: "border1", sharpness: 4, extraArgs: []string{"--sharpness=4"}},
		{name: "left1-tune-ssim", pattern: "left1", tuning: TuneSSIM, tuningSet: true, extraArgs: []string{"--tune=ssim"}},
		{name: "border1-screen-content2-sharpness4", pattern: "border1", screenContentMode: 2, sharpness: 4, extraArgs: []string{"--screen-content-mode=2", "--sharpness=4"}},
		{name: "border1-er3-token-parts4", pattern: "border1", tokenPartitions: 2, errorResilient: true, errorResilientPartitions: true, extraArgs: []string{"--error-resilient=3", "--token-parts=2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			apply := map[int]func(*testing.T, *VP8Encoder){
				0: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap("+tc.pattern+")", e.SetROIMap(roiMapPattern(width, height, tc.pattern)))
				},
			}
			caseOpts := opts
			caseOpts.TokenPartitions = tc.tokenPartitions
			caseOpts.Threads = tc.threads
			caseOpts.NoiseSensitivity = tc.noiseSensitivity
			caseOpts.ScreenContentMode = tc.screenContentMode
			caseOpts.Sharpness = tc.sharpness
			if tc.tuningSet {
				caseOpts.Tuning = tc.tuning
			}
			caseOpts.ErrorResilient = tc.errorResilient
			caseOpts.ErrorResilientPartitions = tc.errorResilientPartitions
			govpxFrames := encodeFramesWithGovpxRuntimeControls(t, caseOpts, sources, nil, apply)
			extraArgs := []string{"--roi-map=" + tc.pattern}
			extraArgs = append(extraArgs, tc.extraArgs...)
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, "roi-map-"+tc.name+"-64x64", caseOpts, targetKbps, sources, nil, extraArgs)
			assertSegmentByteParity(t, "roi-map-"+tc.name, govpxFrames, libvpxFrames, tc.limit)
		})
	}
}
