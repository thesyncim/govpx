//go:build govpx_oracle_trace

package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	"strconv"
	"strings"
	"testing"
)

func TestVP8OracleEncoderStreamByteParityBufferActualDrops(t *testing.T) {
	vp8test.RequireOracle(t, "dropped-frame byte-parity gate")
	driver := vp8test.VpxencFrameFlags(t)

	const (
		fps    = 30
		frames = 30
		width  = 64
		height = 64
	)
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(width, height, i)
	}

	cases := []struct {
		name                     string
		targetKbps               int
		drop                     int
		rtcExternal              bool
		tokenPartitions          int
		errorResilient           bool
		errorResilientPartitions bool
		activeMap                string
		roiMap                   string
		segmentedSource          bool
		limit                    int
	}{
		{name: "drop-frame90-low-bitrate25-tight-buffer-frames30", targetKbps: 25, drop: 90},
		{name: "drop-frame60-low-bitrate50-buffer-200-100-150-frames30", targetKbps: 50, drop: 60},
		{name: "rtc-external-drop-low-bitrate-tight-buffer-frames30", targetKbps: 50, drop: 60, rtcExternal: true},
		{name: "er3-token8-drop-low-bitrate-tight-buffer-frames30", targetKbps: 50, drop: 60, errorResilient: true, errorResilientPartitions: true, tokenPartitions: 3},
		{name: "active-checker-drop-low-bitrate-tight-buffer-frames30", targetKbps: 50, drop: 60, activeMap: "checker"},
		{name: "roi-border1-drop-low-bitrate-tight-buffer-frames30", targetKbps: 50, drop: 60, roiMap: "border1", segmentedSource: true},
		{name: "rtc-external-active-checker-drop-low-bitrate-tight-buffer-frames30", targetKbps: 50, drop: 60, rtcExternal: true, activeMap: "checker"},
		{name: "rtc-external-roi-border1-drop-low-bitrate-tight-buffer-frames30", targetKbps: 50, drop: 60, rtcExternal: true, roiMap: "border1", segmentedSource: true},
		{name: "active-roi-drop-low-bitrate-tight-buffer-frames30", targetKbps: 50, drop: 60, activeMap: "checker", roiMap: "border1", segmentedSource: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			caseSources := sources
			if tc.segmentedSource {
				caseSources = make([]Image, frames)
				for i := range caseSources {
					caseSources[i] = encoderValidationSegmentedFrame(width, height, i)
				}
			}
			opts := EncoderOptions{
				Width:                    width,
				Height:                   height,
				FPS:                      fps,
				RateControlMode:          RateControlCBR,
				TargetBitrateKbps:        tc.targetKbps,
				MinQuantizer:             4,
				MaxQuantizer:             56,
				KeyFrameInterval:         999,
				Deadline:                 DeadlineRealtime,
				CpuUsed:                  -3,
				Tuning:                   TunePSNR,
				BufferSizeMs:             200,
				BufferInitialSizeMs:      100,
				BufferOptimalSizeMs:      150,
				DropFrameAllowed:         true,
				DropFrameWaterMark:       tc.drop,
				RTCExternalRateControl:   tc.rtcExternal,
				TokenPartitions:          tc.tokenPartitions,
				ErrorResilient:           tc.errorResilient,
				ErrorResilientPartitions: tc.errorResilientPartitions,
			}
			var govpxFrames [][]byte
			if tc.activeMap != "" || tc.roiMap != "" {
				apply := map[int]func(*testing.T, *VP8Encoder){}
				if tc.activeMap != "" {
					apply[0] = activeMapApply(tc.activeMap)
				}
				if tc.roiMap != "" {
					prior := apply[0]
					apply[0] = func(t *testing.T, e *VP8Encoder) {
						t.Helper()
						if prior != nil {
							prior(t, e)
						}
						roiMapApply(tc.roiMap)(t, e)
					}
				}
				govpxFrames = encodeFramesWithGovpxRuntimeControls(t, opts, caseSources, nil, apply)
			} else {
				govpxFrames = encodeFramesWithGovpx(t, opts, caseSources)
			}
			extraArgs := []string{
				"--target-bitrate=" + strconv.Itoa(tc.targetKbps),
				"--buf-sz=200",
				"--buf-initial-sz=100",
				"--buf-optimal-sz=150",
				"--drop-frame=" + strconv.Itoa(tc.drop),
			}
			if tc.rtcExternal {
				extraArgs = append(extraArgs, "--rtc-external=1")
			}
			if tc.errorResilient {
				value := "1"
				if tc.errorResilientPartitions {
					value = "3"
				}
				extraArgs = append(extraArgs, "--error-resilient="+value)
			}
			if tc.tokenPartitions > 0 {
				extraArgs = append(extraArgs, "--token-parts="+strconv.Itoa(tc.tokenPartitions))
			}
			if tc.activeMap != "" {
				extraArgs = append(extraArgs, "--active-map="+tc.activeMap)
			}
			if tc.roiMap != "" {
				extraArgs = append(extraArgs, "--roi-map="+tc.roiMap)
			}
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, tc.name, opts, tc.targetKbps, caseSources, nil, extraArgs)
			assertSegmentByteParity(t, tc.name, govpxFrames, libvpxFrames, tc.limit)
		})
	}
}

func TestVP8OracleEncoderStreamByteParityBufferActualDropControlCrosses(t *testing.T) {
	vp8test.RequireOracle(t, "dropped-frame control-cross byte-parity gate")
	driver := vp8test.VpxencFrameFlags(t)

	const (
		fps        = 30
		frames     = 30
		width      = 64
		height     = 64
		lowBitrate = 50
	)
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(width, height, i)
	}

	baseLowDropOpts := func() EncoderOptions {
		return EncoderOptions{
			Width:               width,
			Height:              height,
			FPS:                 fps,
			RateControlMode:     RateControlCBR,
			TargetBitrateKbps:   lowBitrate,
			MinQuantizer:        4,
			MaxQuantizer:        56,
			KeyFrameInterval:    999,
			Deadline:            DeadlineRealtime,
			CpuUsed:             -3,
			Tuning:              TunePSNR,
			BufferSizeMs:        200,
			BufferInitialSizeMs: 100,
			BufferOptimalSizeMs: 150,
			DropFrameAllowed:    true,
			DropFrameWaterMark:  60,
		}
	}
	baseDropArgs := func(targetKbps int) []string {
		return []string{
			"--target-bitrate=" + strconv.Itoa(targetKbps),
			"--buf-sz=200",
			"--buf-initial-sz=100",
			"--buf-optimal-sz=150",
			"--drop-frame=60",
		}
	}

	type dropCross struct {
		name       string
		opts       EncoderOptions
		flags      []EncodeFlags
		script     []string
		apply      map[int]func(*testing.T, *VP8Encoder)
		extraArgs  []string
		matchLimit int
	}

	cases := []dropCross{
		{
			name: "temporal-two-layer-drop-low-bitrate-tight-buffer",
			opts: func() EncoderOptions {
				opts := baseLowDropOpts()
				opts.TemporalScalability = runtimeTemporalConfig(TemporalLayeringTwoLayers, lowBitrate)
				return opts
			}(),
			flags:     temporalScalabilityReconfigureFlags(frames, TemporalLayeringTwoLayers, 0),
			script:    runtimeTemporalLayerIDScript(frames, TemporalLayeringTwoLayers),
			extraArgs: append(baseDropArgs(lowBitrate), runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, lowBitrate)...),
		},
		{
			name: "temporal-three-layer-drop-low-bitrate-tight-buffer",
			opts: func() EncoderOptions {
				opts := baseLowDropOpts()
				opts.TemporalScalability = runtimeTemporalConfig(TemporalLayeringThreeLayers, lowBitrate)
				return opts
			}(),
			flags:     temporalScalabilityReconfigureFlags(frames, TemporalLayeringThreeLayers, 0),
			script:    runtimeTemporalLayerIDScript(frames, TemporalLayeringThreeLayers),
			extraArgs: append(baseDropArgs(lowBitrate), runtimeTemporalExtraArgs(TemporalLayeringThreeLayers, lowBitrate)...),
		},
		{
			name: "invisible-drop-low-bitrate-tight-buffer",
			opts: baseLowDropOpts(),
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				2: EncodeInvisibleFrame,
				5: EncodeInvisibleFrame | EncodeForceAltRefFrame | EncodeNoUpdateLast | EncodeNoUpdateGolden,
			}),
			extraArgs: baseDropArgs(lowBitrate),
		},
		{
			name: "invisible-altref-drop-low-bitrate-tight-buffer-long",
			opts: baseLowDropOpts(),
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				2:  EncodeInvisibleFrame,
				5:  EncodeInvisibleFrame | EncodeForceAltRefFrame | EncodeNoUpdateLast | EncodeNoUpdateGolden,
				11: EncodeInvisibleFrame,
				14: EncodeInvisibleFrame | EncodeForceAltRefFrame | EncodeNoUpdateLast | EncodeNoUpdateGolden,
			}),
			extraArgs: baseDropArgs(lowBitrate),
		},
		{
			name: "runtime-drop-enable-disable-low-bitrate",
			opts: EncoderOptions{
				Width:             width,
				Height:            height,
				FPS:               fps,
				RateControlMode:   RateControlCBR,
				TargetBitrateKbps: 700,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				KeyFrameInterval:  999,
				Deadline:          DeadlineRealtime,
				CpuUsed:           -3,
				Tuning:            TunePSNR,
			},
			script: runtimeControlScript(frames, map[int]string{
				1:  "bitrate:50+minq:4+maxq:56+undershoot:100+overshoot:100+bufsz:200+bufinit:100+bufopt:150+drop:60",
				22: "bitrate:700+minq:4+maxq:56+undershoot:100+overshoot:100+bufsz:6000+bufinit:4000+bufopt:5000+drop:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRateControl(drop on)", e.SetRateControl(RateControlConfig{
						Mode:                RateControlCBR,
						TargetBitrateKbps:   lowBitrate,
						MinQuantizer:        4,
						MaxQuantizer:        56,
						UndershootPct:       100,
						OvershootPct:        100,
						BufferSizeMs:        200,
						BufferInitialSizeMs: 100,
						BufferOptimalSizeMs: 150,
						DropFrameAllowed:    true,
						DropFrameWaterMark:  60,
					}))
				},
				22: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRateControl(drop off)", e.SetRateControl(RateControlConfig{
						Mode:                RateControlCBR,
						TargetBitrateKbps:   700,
						MinQuantizer:        4,
						MaxQuantizer:        56,
						UndershootPct:       100,
						OvershootPct:        100,
						BufferSizeMs:        6000,
						BufferInitialSizeMs: 4000,
						BufferOptimalSizeMs: 5000,
					}))
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			govpxFrames := encodeFramesWithGovpxRuntimeControls(t, tc.opts, sources, tc.flags, tc.apply)
			extraArgs := append([]string(nil), tc.extraArgs...)
			if tc.script != nil {
				extraArgs = append(extraArgs, "--control-script="+strings.Join(tc.script, ","))
			}
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, "actual-drop-cross-"+tc.name, tc.opts, tc.opts.TargetBitrateKbps, sources, tc.flags, extraArgs)
			assertSegmentByteParity(t, "actual-drop-cross-"+tc.name, govpxFrames, libvpxFrames, tc.matchLimit)
		})
	}
}
