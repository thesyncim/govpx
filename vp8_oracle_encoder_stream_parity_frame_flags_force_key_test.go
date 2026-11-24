//go:build govpx_oracle_trace

package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	"strings"
	"testing"
)

func TestVP8OracleEncoderStreamByteParityForceKeyFrameAPI(t *testing.T) {
	vp8test.RequireOracle(t, "force-key API byte-parity gate")
	driver := vp8test.VpxencFrameFlags(t)

	const (
		fps        = 30
		targetKbps = 700
		width      = 32
		height     = 16
	)

	cases := []struct {
		name            string
		frames          int
		lookaheadFrames int
		forceFrames     map[int]bool
		mutate          func(*EncoderOptions)
		buildFlags      func(int, map[int]bool) []EncodeFlags
		apiFlags        func([]EncodeFlags) []EncodeFlags
		setup           func(*testing.T, *VP8Encoder)
		runtimeApply    map[int]func(*testing.T, *VP8Encoder)
		controlScript   []string
		extraArgs       []string
		matchLimit      int
	}{
		{
			name:        "no-lookahead-frame1-and4",
			frames:      8,
			forceFrames: map[int]bool{1: true, 4: true},
		},
		{
			name:            "lookahead2-frame1-and4",
			frames:          8,
			lookaheadFrames: 2,
			forceFrames:     map[int]bool{1: true, 4: true},
			extraArgs:       []string{"--lag-in-frames=2"},
		},
		{
			name:            "lookahead4-frame4-and-flush",
			frames:          10,
			lookaheadFrames: 4,
			forceFrames:     map[int]bool{4: true, 9: true},
			extraArgs:       []string{"--lag-in-frames=4"},
		},
		{
			name:            "lookahead4-auto-alt-ref-frame4-and-flush",
			frames:          10,
			lookaheadFrames: 4,
			forceFrames:     map[int]bool{4: true, 9: true},
			mutate: func(opts *EncoderOptions) {
				opts.AutoAltRef = true
			},
			extraArgs: []string{"--lag-in-frames=4", "--auto-alt-ref=1"},
		},
		{
			name:        "active-map-checker-frame1-and4",
			frames:      8,
			forceFrames: map[int]bool{1: true, 4: true},
			setup:       activeMapApply("checker"),
			extraArgs:   []string{"--active-map=checker"},
		},
		{
			name:        "roi-map-border-frame1-and4",
			frames:      8,
			forceFrames: map[int]bool{1: true, 4: true},
			setup:       roiMapApply("border1"),
			extraArgs:   []string{"--roi-map=border1"},
		},
		{
			name:        "temporal-two-layer-frame1-and4",
			frames:      8,
			forceFrames: map[int]bool{1: true, 4: true},
			mutate: func(opts *EncoderOptions) {
				opts.TemporalScalability = TemporalScalabilityConfig{
					Enabled:                true,
					Mode:                   TemporalLayeringTwoLayers,
					LayerTargetBitrateKbps: [MaxTemporalLayers]int{420, targetKbps},
				}
			},
			buildFlags: forceKeyTemporalTwoLayerFlags,
			extraArgs: []string{
				"--temporal-layers=2",
				"--temporal-bitrates=420,700",
				"--temporal-decimators=2,1",
				"--temporal-periodicity=2",
				"--temporal-layer-ids=0,1",
			},
		},
		{
			name:        "drop-frame-enabled-frame1-and4",
			frames:      8,
			forceFrames: map[int]bool{1: true, 4: true},
			mutate: func(opts *EncoderOptions) {
				opts.DropFrameAllowed = true
				opts.DropFrameWaterMark = 60
			},
			extraArgs: []string{"--drop-frame=60"},
		},
		{
			name:        "keyframes-disabled-frame1-and4",
			frames:      8,
			forceFrames: map[int]bool{1: true, 4: true},
			mutate: func(opts *EncoderOptions) {
				opts.KeyFrameInterval = 0
			},
			extraArgs: []string{"--kf-disabled"},
		},
		{
			name:        "no-update-entropy-frame1-and4",
			frames:      8,
			forceFrames: map[int]bool{1: true, 4: true},
			buildFlags: func(frames int, forceFrames map[int]bool) []EncodeFlags {
				flags := repeatFlag(frames-1, EncodeNoUpdateEntropy)
				for frame := range forceFrames {
					if frame >= 0 && frame < len(flags) {
						flags[frame] |= EncodeForceKeyFrame
					}
				}
				return flags
			},
			apiFlags: forceKeyAPIEncodeFlags,
		},
		{
			name:        "no-update-last-no-ref-gf-arf-frame1-and4",
			frames:      8,
			forceFrames: map[int]bool{1: true, 4: true},
			buildFlags: func(frames int, forceFrames map[int]bool) []EncodeFlags {
				flags := repeatFlag(frames-1, EncodeNoUpdateLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef)
				for frame := range forceFrames {
					if frame >= 0 && frame < len(flags) {
						flags[frame] |= EncodeForceKeyFrame
					}
				}
				return flags
			},
			apiFlags: forceKeyAPIEncodeFlags,
		},
		{
			name:        "force-golden-altref-frame1-and4",
			frames:      8,
			forceFrames: map[int]bool{1: true, 4: true},
			buildFlags: func(frames int, forceFrames map[int]bool) []EncodeFlags {
				flags := make([]EncodeFlags, frames)
				for frame := range forceFrames {
					if frame >= 0 && frame < len(flags) {
						flags[frame] = EncodeForceKeyFrame | EncodeForceGoldenFrame | EncodeForceAltRefFrame
					}
				}
				return flags
			},
			apiFlags: forceKeyAPIEncodeFlags,
		},
		{
			name:        "rtc-external-frame1-and4",
			frames:      8,
			forceFrames: map[int]bool{1: true, 4: true},
			mutate: func(opts *EncoderOptions) {
				opts.RTCExternalRateControl = true
			},
			extraArgs: []string{"--rtc-external=1"},
		},
		{
			name:        "set-reference-before-force-keyframe",
			frames:      8,
			forceFrames: map[int]bool{3: true, 6: true},
			runtimeApply: map[int]func(*testing.T, *VP8Encoder){
				2: setReferencePanningApply(ReferenceLast, 8, "last"),
				5: setReferencePanningApply(ReferenceGolden, 9, "golden"),
			},
			controlScript: runtimeControlScript(8, map[int]string{
				2: "setref:last:panning:8",
				5: "setref:golden:panning:9",
			}),
		},
		{
			name:        "rtc-external-set-reference-before-force-keyframe",
			frames:      8,
			forceFrames: map[int]bool{3: true, 6: true},
			runtimeApply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
				},
				2: setReferencePanningApply(ReferenceLast, 8, "last"),
				5: setReferencePanningApply(ReferenceGolden, 9, "golden"),
			},
			controlScript: runtimeControlScript(8, map[int]string{
				1: "rtc:1",
				2: "setref:last:panning:8",
				5: "setref:golden:panning:9",
			}),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]Image, tc.frames)
			for i := range sources {
				sources[i] = encoderValidationPanningFrame(width, height, i)
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
				CpuUsed:           0,
				Tuning:            TunePSNR,
				LookaheadFrames:   tc.lookaheadFrames,
			}
			if tc.mutate != nil {
				tc.mutate(&opts)
			}
			flags := make([]EncodeFlags, tc.frames)
			if tc.buildFlags != nil {
				flags = tc.buildFlags(tc.frames, tc.forceFrames)
			} else {
				for frame := range tc.forceFrames {
					flags[frame] = EncodeForceKeyFrame
				}
			}

			apiFlags := make([]EncodeFlags, len(flags))
			if tc.apiFlags != nil {
				apiFlags = tc.apiFlags(flags)
			}
			govpxFrames := encodeFramesWithGovpxForceKeyScheduleFlagsSetupAndApply(t, opts, sources, tc.forceFrames, apiFlags, tc.setup, tc.runtimeApply)
			extraArgs := append([]string(nil), tc.extraArgs...)
			if tc.controlScript != nil {
				extraArgs = append(extraArgs, "--control-script="+strings.Join(tc.controlScript, ","))
			}
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, "force-key-api-"+tc.name, opts, opts.TargetBitrateKbps, sources, flags, extraArgs)
			assertSegmentByteParity(t, "force-key-api", govpxFrames, libvpxFrames, tc.matchLimit)
		})
	}
}
