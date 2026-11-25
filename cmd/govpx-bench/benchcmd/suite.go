package benchcmd

import (
	"errors"
	"fmt"
	"math"
)

type encodeSuiteCase struct {
	name        string
	width       int
	height      int
	frames      int
	fps         int
	bitrateKbps int
	mode        string
	content     string
	noDrop      bool
}

func runEncodeSuite(base benchConfig, suiteName string, runs int) (suiteReport, error) {
	if base.Decode {
		return suiteReport{}, errors.New("-suite is only supported for encode benchmarks")
	}
	libvpxPath := suiteLibvpxReferencePath(base)
	if libvpxPath == "" {
		if benchCodec(base) == codecVP9 {
			return suiteReport{}, errors.New("-suite requires a libvpx vpxenc-vp9 reference; keep -auto-libvpx enabled or pass -libvpx-vpxenc-vp9")
		}
		return suiteReport{}, errors.New("-suite requires a libvpx vpxenc reference; keep -auto-libvpx enabled or pass -libvpx-vpxenc")
	}
	if runs <= 0 {
		runs = 1
	}
	cases, err := encodeSuiteCases(suiteName)
	if err != nil {
		return suiteReport{}, err
	}
	selector := "median (alternating order)"
	if runs == 1 {
		selector = "single-run"
	}
	report := suiteReport{
		Name:         suiteName,
		Runs:         runs,
		Selector:     selector,
		LibvpxVpxenc: libvpxPath,
		PhaseTiming:  base.PhaseTiming,
		QualitySkip:  base.SkipQuality,
		Cases:        make([]suiteCaseReport, 0, len(cases)),
	}
	nsProduct := 1.0
	fpsProduct := 1.0
	ratioCount := 0
	for _, tc := range cases {
		selected, err := runEncodeSuiteCase(base, tc, runs)
		if err != nil {
			return suiteReport{}, fmt.Errorf("%s: %w", tc.name, err)
		}
		report.Cases = append(report.Cases, suiteCaseReport{Name: tc.name, Report: selected})
		if selected.Comparison != nil && selected.Comparison.NSPerFrameRatio > 0 && selected.Comparison.EncodeFPSRatio > 0 {
			nsProduct *= selected.Comparison.NSPerFrameRatio
			fpsProduct *= selected.Comparison.EncodeFPSRatio
			ratioCount++
		}
	}
	if ratioCount > 0 {
		inv := 1 / float64(ratioCount)
		report.GeomeanNSGap = math.Pow(nsProduct, inv)
		report.GeomeanFPSGap = math.Pow(fpsProduct, inv)
	}
	return report, nil
}

func suiteLibvpxReferencePath(cfg benchConfig) string {
	if benchCodec(cfg) == codecVP9 {
		return vp9LibvpxEncoderPath(cfg)
	}
	return cfg.LibvpxVpxenc
}

func runEncodeSuiteCase(base benchConfig, tc encodeSuiteCase, runs int) (benchReport, error) {
	cfg := base
	cfg.Width = tc.width
	cfg.Height = tc.height
	cfg.Frames = tc.frames
	cfg.FPS = tc.fps
	cfg.BitrateKbps = tc.bitrateKbps
	cfg.Mode = tc.mode
	cfg.Decode = false
	if tc.content != "" && cfg.InputPath == "" && (cfg.Content == "" || cfg.Content == "ramp") {
		cfg.Content = tc.content
	}
	if tc.noDrop && !cfg.DropFrameSet {
		cfg.DropFrameSet = true
		cfg.DropFrame = 0
	}
	// Pin VP8 realtime speed when -cpu-used was not explicitly set by the
	// caller, avoiding libvpx's non-deterministic adaptive CPU-speed ramp-up.
	if benchCodec(cfg) == codecVP8 && cfg.Mode == "realtime" && !cfg.CpuUsedSet && cfg.CpuUsed > 0 {
		cfg.CpuUsed = -cfg.CpuUsed
	}
	result, err := runRepeatedEncodeBenchmark(cfg, runs)
	if err != nil {
		return benchReport{}, err
	}
	if result.Reference == nil || result.Comparison == nil {
		return benchReport{}, errors.New("missing libvpx reference report")
	}
	return result, nil
}

func encodeSuiteCases(name string) ([]encodeSuiteCase, error) {
	switch name {
	case "standard":
		// Equal-work, pinned-speed suite on panning content with
		// frame dropping disabled so both encoders encode 100% of frames.
		return []encodeSuiteCase{
			{name: "rt-720p-2m-60f", width: 1280, height: 720, frames: 60, fps: 30, bitrateKbps: 2000, mode: "realtime", content: "panning", noDrop: true},
			{name: "good-1080p-8m-60f", width: 1920, height: 1080, frames: 60, fps: 30, bitrateKbps: 8000, mode: "good", content: "panning", noDrop: true},
		}, nil
	case "quick":
		return []encodeSuiteCase{
			{name: "rt-720p-2m-30f", width: 1280, height: 720, frames: 30, fps: 30, bitrateKbps: 2000, mode: "realtime"},
			{name: "good-1080p-8m-30f", width: 1920, height: 1080, frames: 30, fps: 30, bitrateKbps: 8000, mode: "good"},
		}, nil
	case "vp8":
		return []encodeSuiteCase{
			{name: "rt-360p-600k-120f", width: 640, height: 360, frames: 120, fps: 30, bitrateKbps: 600, mode: "realtime"},
			{name: "rt-720p-1500k-120f", width: 1280, height: 720, frames: 120, fps: 30, bitrateKbps: 1500, mode: "realtime"},
			{name: "rt-1080p-4m-60f", width: 1920, height: 1080, frames: 60, fps: 30, bitrateKbps: 4000, mode: "realtime"},
			{name: "good-720p-4m-60f", width: 1280, height: 720, frames: 60, fps: 30, bitrateKbps: 4000, mode: "good"},
			{name: "good-1080p-8m-60f", width: 1920, height: 1080, frames: 60, fps: 30, bitrateKbps: 8000, mode: "good"},
			{name: "good-1440p-12m-30f", width: 2560, height: 1440, frames: 30, fps: 30, bitrateKbps: 12000, mode: "good"},
		}, nil
	case "webrtc":
		// Realtime streaming budgets across mobile-ish to broadcaster
		// resolutions; mirrors the WebRTC simulcast ladder rates from
		// the project's stream-parity trace.
		return []encodeSuiteCase{
			{name: "rt-180p-150k-60f", width: 320, height: 180, frames: 60, fps: 30, bitrateKbps: 150, mode: "realtime"},
			{name: "rt-360p-500k-60f", width: 640, height: 360, frames: 60, fps: 30, bitrateKbps: 500, mode: "realtime"},
			{name: "rt-720p-1500k-60f", width: 1280, height: 720, frames: 60, fps: 30, bitrateKbps: 1500, mode: "realtime"},
			{name: "rt-1080p-2500k-60f", width: 1920, height: 1080, frames: 60, fps: 30, bitrateKbps: 2500, mode: "realtime"},
		}, nil
	case "vod":
		// VoD-style good-quality bitrates that the libvpx good-deadline
		// settings hit comfortably; useful for PSNR/SSIM tracking.
		return []encodeSuiteCase{
			{name: "good-480p-1000k-60f", width: 854, height: 480, frames: 60, fps: 30, bitrateKbps: 1000, mode: "good"},
			{name: "good-720p-2500k-60f", width: 1280, height: 720, frames: 60, fps: 30, bitrateKbps: 2500, mode: "good"},
			{name: "good-1080p-5000k-60f", width: 1920, height: 1080, frames: 60, fps: 30, bitrateKbps: 5000, mode: "good"},
			{name: "good-1440p-10000k-30f", width: 2560, height: 1440, frames: 30, fps: 30, bitrateKbps: 10000, mode: "good"},
		}, nil
	case "stress":
		// Pushes the encoder past comfortable rate regions to surface
		// rate-control / motion-search hot paths.
		return []encodeSuiteCase{
			{name: "rt-1080p-300k-60f", width: 1920, height: 1080, frames: 60, fps: 30, bitrateKbps: 300, mode: "realtime"},
			{name: "rt-1080p-8000k-60f", width: 1920, height: 1080, frames: 60, fps: 30, bitrateKbps: 8000, mode: "realtime"},
			{name: "good-2160p-15000k-30f", width: 3840, height: 2160, frames: 30, fps: 30, bitrateKbps: 15000, mode: "good"},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported suite %q (want standard, quick, vp8, webrtc, vod, or stress)", name)
	}
}
