//go:build govpx_oracle_trace

package govpx

import (
	"fmt"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	"strings"
	"testing"
)

func TestVP8OracleEncoderStreamByteParityRuntimeResizeFrameFlags(t *testing.T) {
	vp8test.RequireOracle(t, "encoder runtime-resize byte-parity gate")
	frameFlagsDriver := vp8test.VpxencFrameFlags(t)

	const (
		fps          = 30
		targetKbps   = 700
		framesPerSeg = 4
	)
	cases := []struct {
		name     string
		w1, h1   int
		w2, h2   int
		deadline Deadline
		cpuUsed  int
		rcMode   RateControlMode
		cqLevel  int
		limit    int
	}{
		// libvpx only permits runtime reconfigures up to the initial
		// dimensions, so this true vpx_codec_enc_config_set oracle covers
		// downscale transitions. Public upsize behavior remains covered by
		// the cold-segment resize matrix above.
		{name: "64x64-to-32x32-realtime-cpu0-cbr", w1: 64, h1: 64, w2: 32, h2: 32, deadline: DeadlineRealtime, cpuUsed: 0, rcMode: RateControlCBR},
		{name: "64x64-to-32x32-realtime-cpu-3-cbr", w1: 64, h1: 64, w2: 32, h2: 32, deadline: DeadlineRealtime, cpuUsed: -3, rcMode: RateControlCBR},
		{name: "64x64-to-32x32-realtime-cpu0-vbr", w1: 64, h1: 64, w2: 32, h2: 32, deadline: DeadlineRealtime, cpuUsed: 0, rcMode: RateControlVBR},
		{name: "64x64-to-32x32-realtime-cpu-3-vbr", w1: 64, h1: 64, w2: 32, h2: 32, deadline: DeadlineRealtime, cpuUsed: -3, rcMode: RateControlVBR},
		{name: "64x64-to-32x32-realtime-cpu-3-cq20", w1: 64, h1: 64, w2: 32, h2: 32, deadline: DeadlineRealtime, cpuUsed: -3, rcMode: RateControlCQ, cqLevel: 20},
		{name: "64x64-to-32x32-realtime-cpu-3-q20", w1: 64, h1: 64, w2: 32, h2: 32, deadline: DeadlineRealtime, cpuUsed: -3, rcMode: RateControlQ, cqLevel: 20},
		{name: "64x64-to-32x32-good-cpu0-cbr", w1: 64, h1: 64, w2: 32, h2: 32, deadline: DeadlineGoodQuality, cpuUsed: 0, rcMode: RateControlCBR},
		{name: "64x64-to-32x32-best-cpu0-cbr", w1: 64, h1: 64, w2: 32, h2: 32, deadline: DeadlineBestQuality, cpuUsed: 0, rcMode: RateControlCBR},
		{name: "65x33-to-33x17-realtime-cpu0-cbr", w1: 65, h1: 33, w2: 33, h2: 17, deadline: DeadlineRealtime, cpuUsed: 0, rcMode: RateControlCBR},
		{name: "96x96-to-64x64-good-cpu0-vbr", w1: 96, h1: 96, w2: 64, h2: 64, deadline: DeadlineGoodQuality, cpuUsed: 0, rcMode: RateControlVBR},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seg1 := makePanningSources(tc.w1, tc.h1, framesPerSeg, 0)
			seg2 := makePanningSources(tc.w2, tc.h2, framesPerSeg, framesPerSeg)
			opts := EncoderOptions{
				Width:             tc.w1,
				Height:            tc.h1,
				FPS:               fps,
				RateControlMode:   tc.rcMode,
				TargetBitrateKbps: targetKbps,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				KeyFrameInterval:  999,
				Deadline:          tc.deadline,
				CpuUsed:           tc.cpuUsed,
				CQLevel:           tc.cqLevel,
			}
			sources := append(append([]Image(nil), seg1...), seg2...)
			script := make([]string, len(sources))
			for i := range script {
				script[i] = "-"
			}
			script[framesPerSeg] = fmt.Sprintf("resize:%dx%d", tc.w2, tc.h2)
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, frameFlagsDriver, "runtime-resize-"+tc.name, opts, targetKbps, sources, nil, []string{
				"--control-script=" + strings.Join(script, ","),
			})

			govpxSeg1, govpxSeg2 := encodeWithMidStreamResize(t, opts, tc.w2, tc.h2, seg1, seg2)
			govpxFrames := append(append([][]byte(nil), govpxSeg1...), govpxSeg2...)
			assertSegmentByteParity(t, "runtime-resize-"+tc.name, govpxFrames, libvpxFrames, tc.limit)
		})
	}
}

func TestVP8OracleEncoderStreamByteParityRuntimeResizePostFrameCrosses(t *testing.T) {
	vp8test.RequireOracle(t, "encoder runtime-resize post-frame byte-parity gate")
	frameFlagsDriver := vp8test.VpxencFrameFlags(t)

	const (
		fps          = 30
		targetKbps   = 700
		framesPerSeg = 4
		w1           = 64
		h1           = 64
		w2           = 32
		h2           = 32
	)
	seg1 := makePanningSources(w1, h1, framesPerSeg, 0)
	seg2 := makePanningSources(w2, h2, framesPerSeg, framesPerSeg)
	sources := append(append([]Image(nil), seg1...), seg2...)
	baseOpts := EncoderOptions{
		Width:             w1,
		Height:            h1,
		FPS:               fps,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  999,
		Deadline:          DeadlineRealtime,
		CpuUsed:           0,
	}

	cases := []struct {
		name     string
		flags    []EncodeFlags
		controls map[int]string
		apply    map[int]func(*testing.T, *VP8Encoder)
		limit    int
	}{
		{
			name:  "force-keyframe-after-resize",
			flags: indexedResizeFlags(len(sources), map[int]EncodeFlags{framesPerSeg + 1: EncodeForceKeyFrame}),
		},
		{
			name:  "force-golden-after-resize",
			flags: indexedResizeFlags(len(sources), map[int]EncodeFlags{framesPerSeg + 1: EncodeForceGoldenFrame}),
		},
		{
			name:  "invisible-inter-after-resize",
			flags: indexedResizeFlags(len(sources), map[int]EncodeFlags{framesPerSeg + 1: EncodeInvisibleFrame}),
		},
		{
			name: "invisible-force-altref-after-resize",
			flags: indexedResizeFlags(len(sources), map[int]EncodeFlags{
				framesPerSeg + 1: EncodeInvisibleFrame | EncodeForceAltRefFrame | EncodeNoUpdateLast | EncodeNoUpdateGolden,
			}),
		},
		{
			name:  "set-reference-last-after-resize",
			flags: indexedResizeFlags(len(sources), map[int]EncodeFlags{framesPerSeg + 1: EncodeNoReferenceGolden | EncodeNoReferenceAltRef}),
			controls: map[int]string{
				framesPerSeg + 1: "setref:last:panning:12",
			},
			apply: map[int]func(*testing.T, *VP8Encoder){
				framesPerSeg + 1: setReferencePanningApply(ReferenceLast, 12, "last"),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			updates := map[int]string{
				framesPerSeg: fmt.Sprintf("resize:%dx%d", w2, h2),
			}
			for frame, update := range tc.controls {
				if frame == framesPerSeg {
					updates[frame] += "+" + update
					continue
				}
				updates[frame] = update
			}
			script := runtimeControlScript(len(sources), updates)
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, frameFlagsDriver, "runtime-resize-post-"+tc.name, baseOpts, targetKbps, sources, tc.flags, []string{
				"--control-script=" + strings.Join(script, ","),
			})
			govpxFrames := encodeWithMidStreamResizeGlobalControls(t, baseOpts, w2, h2, seg1, seg2, tc.flags, tc.apply)
			assertSegmentByteParity(t, "runtime-resize-post-"+tc.name, govpxFrames, libvpxFrames, tc.limit)
		})
	}
}
