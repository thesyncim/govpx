//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"os"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
	"github.com/thesyncim/govpx/internal/testutil"
)

func TestVP9OracleEncoderStreamByteParityMatrix(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run VP9 stream byte-parity matrix")
	}
	requireVP9VpxencFrameFlagsOracle(t)

	type streamFixture struct {
		name   string
		width  int
		height int
		source func(width, height, frame int) *image.YCbCr
	}
	constant64 := streamFixture{
		name:   "constant-64x64",
		width:  64,
		height: 64,
		source: func(width, height, frame int) *image.YCbCr {
			return newVP9YCbCrForTest(width, height, 128, 128, 128)
		},
	}
	stepped64 := streamFixture{
		name:   "stepped-64x64",
		width:  64,
		height: 64,
		source: func(width, height, frame int) *image.YCbCr {
			return newVP9YCbCrForTest(width, height,
				uint8(96+frame*8), 128, 128)
		},
	}
	panning64 := streamFixture{
		name:   "panning-64x64",
		width:  64,
		height: 64,
		source: newVP9PanningYCbCrForRateTest,
	}
	panning320 := streamFixture{
		name:   "panning-320x180",
		width:  320,
		height: 180,
		source: newVP9PanningYCbCrForRateTest,
	}
	panning720 := streamFixture{
		name:   "panning-1280x720",
		width:  1280,
		height: 720,
		source: newVP9PanningYCbCrForRateTest,
	}
	tiled1024 := streamFixture{
		name:   "panning-1024x64",
		width:  1024,
		height: 64,
		source: newVP9PanningYCbCrForRateTest,
	}

	type streamCase struct {
		name        string
		fixture     streamFixture
		frames      int
		opts        VP9EncoderOptions
		flags       []EncodeFlags
		extraArgs   []string
		exactPrefix int
	}
	cases := []streamCase{
		{
			name:    "fixed-q-constant",
			fixture: constant64,
			frames:  6,
			opts: VP9EncoderOptions{
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			extraArgs: []string{
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--disable-warning-prompt",
			},
			exactPrefix: 6,
		},
		{
			name:    "error-resilient-constant",
			fixture: constant64,
			frames:  6,
			opts: VP9EncoderOptions{
				ErrorResilient: true,
			},
			extraArgs:   []string{"--error-resilient=1"},
			exactPrefix: 6,
		},
		{
			name:    "max-keyframe-interval-2",
			fixture: constant64,
			frames:  6,
			opts: VP9EncoderOptions{
				MaxKeyframeInterval: 2,
			},
			extraArgs:   []string{"--kf-max-dist=2"},
			exactPrefix: 6,
		},
		{
			name:    "force-key-frame1",
			fixture: stepped64,
			frames:  6,
			flags:   vp9OracleFlagAt(6, 1, EncodeForceKeyFrame),
			// The forced keyframe itself is exact; the following inter
			// frames currently expose the reference/rate-state gap.
			exactPrefix: 2,
		},
		{
			name:        "no-update-all",
			fixture:     stepped64,
			frames:      6,
			flags:       vp9OracleRepeatInterFlag(6, vp9NoUpdateRefFlags),
			exactPrefix: 2,
		},
		{
			name:        "no-reference-all",
			fixture:     stepped64,
			frames:      6,
			flags:       vp9OracleRepeatInterFlag(6, EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef),
			exactPrefix: 6,
		},
		{
			name:        "cbr-rate-panning",
			fixture:     panning64,
			frames:      8,
			opts:        vp9OracleCBROptions(64, 64, 700),
			extraArgs:   vp9OracleCBRArgs(700, 600, 400, 500, 0),
			exactPrefix: 1,
		},
		{
			name:    "vbr-rate-panning",
			fixture: panning320,
			frames:  8,
			opts: VP9EncoderOptions{
				RateControlModeSet:  true,
				RateControlMode:     RateControlVBR,
				TargetBitrateKbps:   700,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				MaxKeyframeInterval: 128,
			},
			extraArgs: []string{
				"--end-usage=vbr",
				"--target-bitrate=700",
				"--min-q=4",
				"--max-q=56",
			},
			exactPrefix: 0,
		},
		{
			name:    "cq-rate-panning",
			fixture: panning320,
			frames:  8,
			opts: VP9EncoderOptions{
				RateControlModeSet:  true,
				RateControlMode:     RateControlCQ,
				TargetBitrateKbps:   700,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				CQLevel:             20,
				MaxKeyframeInterval: 128,
			},
			extraArgs: []string{
				"--end-usage=cq",
				"--target-bitrate=700",
				"--min-q=4",
				"--max-q=56",
				"--cq-level=20",
			},
			exactPrefix: 0,
		},
		{
			name:    "q-rate-panning",
			fixture: panning320,
			frames:  8,
			opts: VP9EncoderOptions{
				RateControlModeSet:  true,
				RateControlMode:     RateControlQ,
				TargetBitrateKbps:   700,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				CQLevel:             20,
				MaxKeyframeInterval: 128,
			},
			extraArgs: []string{
				"--end-usage=q",
				"--target-bitrate=700",
				"--min-q=4",
				"--max-q=56",
				"--cq-level=20",
			},
			exactPrefix: 0,
		},
		{
			name:    "cbr-cyclic-aq-panning",
			fixture: panning320,
			frames:  8,
			opts: func() VP9EncoderOptions {
				opts := vp9OracleCBROptions(320, 180, 700)
				opts.AQMode = VP9AQCyclicRefresh
				return opts
			}(),
			extraArgs: append(vp9OracleCBRArgs(700, 600, 400, 500, 0),
				"--aq-mode=3"),
			exactPrefix: 0,
		},
		{
			name:    "vbr-rate-panning-720p",
			fixture: panning720,
			frames:  3,
			opts: VP9EncoderOptions{
				RateControlModeSet:  true,
				RateControlMode:     RateControlVBR,
				TargetBitrateKbps:   2200,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				MaxKeyframeInterval: 128,
			},
			extraArgs: []string{
				"--end-usage=vbr",
				"--target-bitrate=2200",
				"--min-q=4",
				"--max-q=56",
			},
			exactPrefix: 0,
		},
		{
			name:    "tile-columns-from-threads",
			fixture: tiled1024,
			frames:  4,
			opts: func() VP9EncoderOptions {
				opts := VP9EncoderOptions{Threads: 4}
				return opts
			}(),
			extraArgs:   []string{"--tile-columns=2"},
			exactPrefix: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]*image.YCbCr, tc.frames)
			for i := range sources {
				sources[i] = tc.fixture.source(tc.fixture.width,
					tc.fixture.height, i)
			}
			govpxPackets, libvpxPackets := captureVP9StreamParityPackets(t,
				tc.opts, sources, tc.flags, tc.extraArgs)
			matches := 0
			firstMismatch := -1
			for i := range govpxPackets {
				if bytes.Equal(govpxPackets[i], libvpxPackets[i]) {
					matches++
					continue
				}
				if firstMismatch < 0 {
					firstMismatch = i
				}
			}
			t.Logf("VP9 stream byte-parity matrix %s/%s: matches=%d/%d first_mismatch=%d exact_prefix=%d",
				tc.name, tc.fixture.name, matches, len(govpxPackets),
				firstMismatch, tc.exactPrefix)
			t.Logf("VP9 stream byte-parity rows %s:\n%s", tc.name,
				formatVP9StreamParityRows(t, govpxPackets, libvpxPackets))
			for frame := 0; frame < tc.exactPrefix; frame++ {
				if !bytes.Equal(govpxPackets[frame], libvpxPackets[frame]) {
					t.Fatalf("frame %d should be inside exact prefix for %s",
						frame, tc.name)
				}
			}
			newModeByteCase := tc.name == "vbr-rate-panning" ||
				tc.name == "cq-rate-panning" ||
				tc.name == "q-rate-panning" ||
				tc.name == "cbr-cyclic-aq-panning" ||
				tc.name == "vbr-rate-panning-720p"
			if os.Getenv("GOVPX_VP9_STREAM_MATRIX_STRICT") == "1" &&
				!newModeByteCase &&
				matches != len(govpxPackets) {
				t.Fatalf("strict VP9 stream byte parity %s: matches=%d/%d",
					tc.name, matches, len(govpxPackets))
			}
			if os.Getenv("GOVPX_VP9_NEW_MODE_BYTE_STRICT") == "1" &&
				newModeByteCase &&
				matches != len(govpxPackets) {
				t.Fatalf("strict VP9 new-mode byte parity %s/%s: matches=%d/%d",
					tc.name, tc.fixture.name, matches, len(govpxPackets))
			}
		})
	}
}

func TestVP9OracleEncoderStreamByteParityFrameFlagsMatrix(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run VP9 frame-flag byte-parity matrix")
	}
	requireVP9VpxencFrameFlagsOracle(t)

	const width, height, frames = 64, 64, 6
	type flagCase struct {
		name        string
		flags       []EncodeFlags
		exactPrefix int
	}
	cases := []flagCase{
		{
			name:        "force-key-frame1",
			flags:       vp9OracleFlagAt(frames, 1, EncodeForceKeyFrame),
			exactPrefix: 2,
		},
		{
			name:        "force-key-frame3",
			flags:       vp9OracleFlagAt(frames, 3, EncodeForceKeyFrame),
			exactPrefix: 1,
		},
		{
			name:        "repeat-no-update-last",
			flags:       vp9OracleRepeatInterFlag(frames, EncodeNoUpdateLast),
			exactPrefix: 2,
		},
		{
			name:        "repeat-no-update-golden",
			flags:       vp9OracleRepeatInterFlag(frames, EncodeNoUpdateGolden),
			exactPrefix: 1,
		},
		{
			name:        "repeat-no-update-altref",
			flags:       vp9OracleRepeatInterFlag(frames, EncodeNoUpdateAltRef),
			exactPrefix: 1,
		},
		{
			name:        "repeat-no-update-all",
			flags:       vp9OracleRepeatInterFlag(frames, vp9NoUpdateRefFlags),
			exactPrefix: 2,
		},
		{
			name: "repeat-no-reference-golden-altref",
			flags: vp9OracleRepeatInterFlag(frames,
				EncodeNoReferenceGolden|EncodeNoReferenceAltRef),
			exactPrefix: 2,
		},
		{
			name: "repeat-no-reference-all",
			flags: vp9OracleRepeatInterFlag(frames,
				EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef),
			exactPrefix: 6,
		},
		{
			name:        "repeat-no-update-entropy",
			flags:       vp9OracleRepeatInterFlag(frames, EncodeNoUpdateEntropy),
			exactPrefix: 2,
		},
		{
			name:        "force-ref-refresh-transitions",
			flags:       vp9OracleRefRefreshTransitions(frames),
			exactPrefix: 1,
		},
		{
			name:        "alternating-reference-controls",
			flags:       vp9OracleAlternatingReferenceControls(frames),
			exactPrefix: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := makeVP9SteppedOracleSources(width, height, frames)
			govpxPackets, libvpxPackets := captureVP9StreamParityPackets(t,
				VP9EncoderOptions{}, sources, tc.flags, nil)
			matches, firstMismatch := countVP9ByteParityMatches(govpxPackets,
				libvpxPackets)
			t.Logf("VP9 frame-flag byte-parity matrix %s: matches=%d/%d first_mismatch=%d exact_prefix=%d",
				tc.name, matches, len(govpxPackets), firstMismatch, tc.exactPrefix)
			t.Logf("VP9 frame-flag byte-parity rows %s:\n%s", tc.name,
				formatVP9StreamParityRows(t, govpxPackets, libvpxPackets))
			for frame := 0; frame < tc.exactPrefix; frame++ {
				if !bytes.Equal(govpxPackets[frame], libvpxPackets[frame]) {
					t.Fatalf("frame %d should be inside exact prefix for %s",
						frame, tc.name)
				}
			}
			if os.Getenv("GOVPX_VP9_FLAG_BYTE_MATRIX_STRICT") == "1" &&
				matches != len(govpxPackets) {
				t.Fatalf("strict VP9 frame-flag byte parity %s: matches=%d/%d",
					tc.name, matches, len(govpxPackets))
			}
		})
	}
}

func TestVP9OracleEncoderStreamByteParityControlCrossMatrix(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run VP9 control-cross byte-parity matrix")
	}
	requireVP9VpxencFrameFlagsOracle(t)

	const frames = 6
	type crossCase struct {
		name        string
		width       int
		height      int
		opts        VP9EncoderOptions
		flags       []EncodeFlags
		extraArgs   []string
		exactPrefix int
	}
	cases := []crossCase{
		{
			name:   "fixed-q-no-update-all",
			width:  64,
			height: 64,
			opts: VP9EncoderOptions{
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			flags: vp9OracleRepeatInterFlag(frames, vp9NoUpdateRefFlags),
			extraArgs: []string{
				"--min-q=20",
				"--max-q=20",
			},
			exactPrefix: 1,
		},
		{
			name:        "cbr-force-key-frame3",
			width:       64,
			height:      64,
			opts:        vp9OracleCBROptions(64, 64, 700),
			flags:       vp9OracleFlagAt(frames, 3, EncodeForceKeyFrame),
			extraArgs:   vp9OracleCBRArgs(700, 600, 400, 500, 0),
			exactPrefix: 1,
		},
		{
			name:   "error-resilient-no-update-entropy",
			width:  64,
			height: 64,
			opts: VP9EncoderOptions{
				ErrorResilient: true,
			},
			flags:       vp9OracleRepeatInterFlag(frames, EncodeNoUpdateEntropy),
			extraArgs:   []string{"--error-resilient=1"},
			exactPrefix: 1,
		},
		{
			name:   "cbr-no-reference-all",
			width:  64,
			height: 64,
			opts:   vp9OracleCBROptions(64, 64, 700),
			flags: vp9OracleRepeatInterFlag(frames,
				EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef),
			extraArgs:   vp9OracleCBRArgs(700, 600, 400, 500, 0),
			exactPrefix: 3,
		},
		{
			name:   "tile-columns-ref-refresh",
			width:  1024,
			height: 64,
			opts: VP9EncoderOptions{
				Threads: 4,
			},
			flags:       vp9OracleRefRefreshTransitions(frames),
			extraArgs:   []string{"--tile-columns=2"},
			exactPrefix: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := makeVP9SteppedOracleSources(tc.width, tc.height, frames)
			govpxPackets, libvpxPackets := captureVP9StreamParityPackets(t,
				tc.opts, sources, tc.flags, tc.extraArgs)
			matches, firstMismatch := countVP9ByteParityMatches(govpxPackets,
				libvpxPackets)
			t.Logf("VP9 control-cross byte-parity matrix %s: matches=%d/%d first_mismatch=%d exact_prefix=%d",
				tc.name, matches, len(govpxPackets), firstMismatch, tc.exactPrefix)
			t.Logf("VP9 control-cross byte-parity rows %s:\n%s", tc.name,
				formatVP9StreamParityRows(t, govpxPackets, libvpxPackets))
			for frame := 0; frame < tc.exactPrefix; frame++ {
				if !bytes.Equal(govpxPackets[frame], libvpxPackets[frame]) {
					t.Fatalf("frame %d should be inside exact prefix for %s",
						frame, tc.name)
				}
			}
			if os.Getenv("GOVPX_VP9_CONTROL_CROSS_BYTE_STRICT") == "1" &&
				matches != len(govpxPackets) {
				t.Fatalf("strict VP9 control-cross byte parity %s: matches=%d/%d",
					tc.name, matches, len(govpxPackets))
			}
		})
	}
}

func TestVP9OracleRuntimeControlByteParityScoreboard(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run VP9 runtime-control byte-parity scoreboard")
	}
	requireVP9VpxencFrameFlagsOracle(t)

	const width, height, frames = 64, 64, 10
	type runtimeCase struct {
		name      string
		opts      VP9EncoderOptions
		flags     []EncodeFlags
		before    func(*testing.T, *VP9Encoder, int)
		extraArgs []string
	}
	baseOpts := func(targetKbps int) VP9EncoderOptions {
		return vp9OracleCBROptions(width, height, targetKbps)
	}
	cases := []runtimeCase{
		{
			name: "bitrate-only-two-step",
			opts: baseOpts(700),
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetRealtimeTarget bitrate 300",
						enc.SetRealtimeTarget(RealtimeTarget{BitrateKbps: 300}))
				case 7:
					mustVP9Runtime(t, "SetRealtimeTarget bitrate 900",
						enc.SetRealtimeTarget(RealtimeTarget{BitrateKbps: 900}))
				}
			},
			extraArgs: append(vp9OracleCBRArgs(700, 600, 400, 500, 0),
				"--target-bitrate-schedule=3:300,7:900"),
		},
		{
			name: "q-band-only-two-step",
			opts: baseOpts(700),
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetRealtimeTarget q band 10-50",
						enc.SetRealtimeTarget(RealtimeTarget{
							MinQuantizer: 10,
							MaxQuantizer: 50,
						}))
				case 7:
					mustVP9Runtime(t, "SetRealtimeTarget q band 4-56",
						enc.SetRealtimeTarget(RealtimeTarget{
							MinQuantizer: 4,
							MaxQuantizer: 56,
						}))
				}
			},
			extraArgs: append(vp9OracleCBRArgs(700, 600, 400, 500, 0),
				"--min-q-schedule=3:10,7:4",
				"--max-q-schedule=3:50,7:56"),
		},
		{
			name: "fixed-q-runtime-window",
			opts: baseOpts(700),
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetRealtimeTarget fixed q 20",
						enc.SetRealtimeTarget(RealtimeTarget{
							MinQuantizer: 20,
							MaxQuantizer: 20,
						}))
				case 7:
					mustVP9Runtime(t, "SetRealtimeTarget q band 4-56",
						enc.SetRealtimeTarget(RealtimeTarget{
							MinQuantizer: 4,
							MaxQuantizer: 56,
						}))
				}
			},
			extraArgs: append(vp9OracleCBRArgs(700, 600, 400, 500, 0),
				"--min-q-schedule=3:20,7:4",
				"--max-q-schedule=3:20,7:56"),
		},
		{
			name: "fps-only-two-step",
			opts: baseOpts(700),
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetRealtimeTarget fps 15",
						enc.SetRealtimeTarget(RealtimeTarget{FPS: 15}))
				case 7:
					mustVP9Runtime(t, "SetRealtimeTarget fps 30",
						enc.SetRealtimeTarget(RealtimeTarget{FPS: 30}))
				}
			},
			extraArgs: append(vp9OracleCBRArgs(700, 600, 400, 500, 0),
				"--fps-schedule=3:15,7:30"),
		},
		{
			name: "buffer-model-two-step",
			opts: baseOpts(700),
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetRateControlBuffer tight",
						enc.SetRateControlBuffer(400, 300, 350))
				case 7:
					mustVP9Runtime(t, "SetRateControlBuffer restore",
						enc.SetRateControlBuffer(600, 400, 500))
				}
			},
			extraArgs: append(vp9OracleCBRArgs(700, 600, 400, 500, 0),
				"--buf-sz-schedule=3:400,7:600",
				"--buf-initial-sz-schedule=3:300,7:400",
				"--buf-optimal-sz-schedule=3:350,7:500"),
		},
		{
			name: "combined-bitrate-fps-q",
			opts: baseOpts(700),
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetRealtimeTarget combined low",
						enc.SetRealtimeTarget(RealtimeTarget{
							BitrateKbps:  300,
							FPS:          15,
							MinQuantizer: 10,
							MaxQuantizer: 50,
						}))
				case 7:
					mustVP9Runtime(t, "SetRealtimeTarget combined restore",
						enc.SetRealtimeTarget(RealtimeTarget{
							BitrateKbps:  700,
							FPS:          30,
							MinQuantizer: 4,
							MaxQuantizer: 56,
						}))
				}
			},
			extraArgs: append(vp9OracleCBRArgs(700, 600, 400, 500, 0),
				"--target-bitrate-schedule=3:300,7:700",
				"--fps-schedule=3:15,7:30",
				"--min-q-schedule=3:10,7:4",
				"--max-q-schedule=3:50,7:56"),
		},
		{
			name:  "bitrate-with-force-key",
			opts:  baseOpts(700),
			flags: vp9OracleFlagAt(frames, 4, EncodeForceKeyFrame),
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetRealtimeTarget bitrate 300",
						enc.SetRealtimeTarget(RealtimeTarget{BitrateKbps: 300}))
				case 7:
					mustVP9Runtime(t, "SetRealtimeTarget bitrate 700",
						enc.SetRealtimeTarget(RealtimeTarget{BitrateKbps: 700}))
				}
			},
			extraArgs: append(vp9OracleCBRArgs(700, 600, 400, 500, 0),
				"--target-bitrate-schedule=3:300,7:700"),
		},
		{
			name:  "fixed-q-with-no-update-all",
			opts:  baseOpts(700),
			flags: vp9OracleRepeatInterFlag(frames, vp9NoUpdateRefFlags),
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetRealtimeTarget fixed q 20",
						enc.SetRealtimeTarget(RealtimeTarget{
							MinQuantizer: 20,
							MaxQuantizer: 20,
						}))
				case 7:
					mustVP9Runtime(t, "SetRealtimeTarget q band 4-56",
						enc.SetRealtimeTarget(RealtimeTarget{
							MinQuantizer: 4,
							MaxQuantizer: 56,
						}))
				}
			},
			extraArgs: append(vp9OracleCBRArgs(700, 600, 400, 500, 0),
				"--min-q-schedule=3:20,7:4",
				"--max-q-schedule=3:20,7:56"),
		},
		{
			name: "buffer-with-no-reference-all",
			opts: baseOpts(700),
			flags: vp9OracleRepeatInterFlag(frames,
				EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef),
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetRateControlBuffer tight",
						enc.SetRateControlBuffer(400, 300, 350))
				case 7:
					mustVP9Runtime(t, "SetRateControlBuffer restore",
						enc.SetRateControlBuffer(600, 400, 500))
				}
			},
			extraArgs: append(vp9OracleCBRArgs(700, 600, 400, 500, 0),
				"--buf-sz-schedule=3:400,7:600",
				"--buf-initial-sz-schedule=3:300,7:400",
				"--buf-optimal-sz-schedule=3:350,7:500"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := newVP9OracleTransitionSources(width, height, frames)
			govpxPackets, libvpxPackets := captureVP9StreamParityPacketsWithHooks(t,
				tc.opts, sources, tc.flags, tc.extraArgs,
				func(enc *VP9Encoder, frame int) {
					tc.before(t, enc, frame)
				})
			matches, firstMismatch := countVP9ByteParityMatches(govpxPackets,
				libvpxPackets)
			t.Logf("VP9 runtime-control byte-parity scoreboard %s: matches=%d/%d first_mismatch=%d",
				tc.name, matches, len(govpxPackets), firstMismatch)
			t.Logf("VP9 runtime-control byte-parity rows %s:\n%s",
				tc.name, formatVP9StreamParityRows(t, govpxPackets, libvpxPackets))
			if os.Getenv("GOVPX_VP9_RUNTIME_BYTE_STRICT") == "1" &&
				matches != len(govpxPackets) {
				t.Fatalf("strict VP9 runtime-control byte parity %s: matches=%d/%d",
					tc.name, matches, len(govpxPackets))
			}
		})
	}
}

func TestVP9OracleRuntimeResizeByteParityScoreboard(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run VP9 runtime-resize byte-parity scoreboard")
	}
	requireVP9VpxencFrameFlagsOracle(t)

	type resizeCase struct {
		name          string
		initialWidth  int
		initialHeight int
		nextWidth     int
		nextHeight    int
		resizeFrame   int
	}
	cases := []resizeCase{
		{name: "up-64x64-to-96x80", initialWidth: 64, initialHeight: 64, nextWidth: 96, nextHeight: 80, resizeFrame: 2},
		{name: "down-96x80-to-64x64", initialWidth: 96, initialHeight: 80, nextWidth: 64, nextHeight: 64, resizeFrame: 2},
		{name: "odd-65x63-to-81x79", initialWidth: 65, initialHeight: 63, nextWidth: 81, nextHeight: 79, resizeFrame: 2},
	}
	extraArgs := []string{"--cq-level=32", "--min-q=32", "--max-q=32"}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const frames = 5
			sources := makeVP9RuntimeResizeSources(tc.initialWidth,
				tc.initialHeight, tc.nextWidth, tc.nextHeight,
				tc.resizeFrame, frames)
			opts := VP9EncoderOptions{
				Width:        tc.initialWidth,
				Height:       tc.initialHeight,
				MinQuantizer: 32,
				MaxQuantizer: 32,
			}
			before := func(enc *VP9Encoder, frame int) {
				if frame != tc.resizeFrame {
					return
				}
				mustVP9Runtime(t, "SetRealtimeTarget resize",
					enc.SetRealtimeTarget(RealtimeTarget{
						Width:  tc.nextWidth,
						Height: tc.nextHeight,
					}))
			}
			govpxRows, govpxPackets := captureGovpxVP9VariablePacketRows(t,
				opts, sources, nil, before)
			libvpxRows, libvpxPackets := captureLibvpxVP9VariablePacketRows(t,
				sources, nil, nil, extraArgs)
			stats := compareVP9OracleTransitionRows(t, govpxRows, libvpxRows)
			matches, firstMismatch := countVP9ByteParityMatches(govpxPackets,
				libvpxPackets)
			t.Logf("VP9 runtime resize byte-parity scoreboard %s: matches=%d/%d first_mismatch=%d stats=%s",
				tc.name, matches, len(govpxPackets), firstMismatch, stats)
			t.Logf("VP9 runtime resize rate rows %s:\n%s", tc.name,
				formatVP9RateScoreboardRows(govpxRows, libvpxRows))
			t.Logf("VP9 runtime resize byte rows %s:\n%s", tc.name,
				formatVP9StreamParityRows(t, govpxPackets, libvpxPackets))
			if os.Getenv("GOVPX_VP9_RUNTIME_RESIZE_STRICT") == "1" &&
				(stats.hasMismatch() || matches != len(govpxPackets)) {
				t.Fatalf("strict VP9 runtime resize parity %s: matches=%d/%d stats=%s",
					tc.name, matches, len(govpxPackets), stats)
			}
		})
	}
}

func TestVP9OracleInvisibleKeyFrameByteParityScoreboard(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run VP9 invisible-frame byte-parity scoreboard")
	}
	requireVP9VpxencFrameFlagsOracle(t)

	const width, height = 64, 64
	sources := []*image.YCbCr{
		newVP9YCbCrForTest(width, height, 96, 128, 128),
	}
	flags := []EncodeFlags{EncodeInvisibleFrame}
	govpxRows, govpxPackets := captureGovpxVP9VariablePacketRows(t,
		VP9EncoderOptions{Width: width, Height: height, MinQuantizer: 32, MaxQuantizer: 32},
		sources, flags, nil)
	libvpxRows, libvpxPackets := captureLibvpxVP9VariablePacketRows(t,
		sources, flags, []bool{true},
		[]string{"--cq-level=32", "--min-q=32", "--max-q=32"})
	stats := compareVP9OracleTransitionRows(t, govpxRows, libvpxRows)
	matches, firstMismatch := countVP9ByteParityMatches(govpxPackets,
		libvpxPackets)
	t.Logf("VP9 invisible keyframe byte-parity scoreboard: matches=%d/%d first_mismatch=%d stats=%s",
		matches, len(govpxPackets), firstMismatch, stats)
	t.Logf("VP9 invisible keyframe rate rows:\n%s",
		formatVP9RateScoreboardRows(govpxRows, libvpxRows))
	t.Logf("VP9 invisible keyframe byte rows:\n%s",
		formatVP9StreamParityRows(t, govpxPackets, libvpxPackets))
	if os.Getenv("GOVPX_VP9_INVISIBLE_KEY_STRICT") == "1" &&
		(stats.hasMismatch() || matches != len(govpxPackets)) {
		t.Fatalf("strict VP9 invisible keyframe parity: matches=%d/%d stats=%s",
			matches, len(govpxPackets), stats)
	}
}

func TestVP9OracleRuntimeDropToggleByteParityScoreboard(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run VP9 runtime-drop byte-parity scoreboard")
	}
	requireVP9VpxencFrameFlagsOracle(t)

	const width, height, frames = 64, 64, 24
	type runtimeDropCase struct {
		name      string
		opts      VP9EncoderOptions
		before    func(*testing.T, *VP9Encoder, int)
		extraArgs []string
		wantDrop  bool
	}
	dropOpts := func(targetKbps int) VP9EncoderOptions {
		opts := vp9OracleCBROptions(width, height, targetKbps)
		opts.BufferSizeMs = 400
		opts.BufferInitialSizeMs = 300
		opts.BufferOptimalSizeMs = 350
		opts.DropFrameWaterMark = 60
		return opts
	}
	cases := []runtimeDropCase{
		{
			name: "drop-frame-toggle",
			opts: dropOpts(120),
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					mustVP9Runtime(t, "SetRealtimeTarget drop enabled",
						enc.SetRealtimeTarget(RealtimeTarget{
							FrameDrop: RealtimeFrameDropEnabled,
						}))
				case 14:
					mustVP9Runtime(t, "SetRealtimeTarget drop disabled",
						enc.SetRealtimeTarget(RealtimeTarget{
							FrameDrop: RealtimeFrameDropDisabled,
						}))
				}
			},
			extraArgs: append(vp9OracleCBRArgs(120, 400, 300, 350, 0),
				"--drop-frame-schedule=3:60,14:0"),
			wantDrop: true,
		},
		{
			name: "fixed-q-drop-frame-toggle",
			opts: func() VP9EncoderOptions {
				opts := dropOpts(140)
				opts.MinQuantizer = 20
				opts.MaxQuantizer = 20
				return opts
			}(),
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 2:
					mustVP9Runtime(t, "SetRealtimeTarget fixed-q drop enabled",
						enc.SetRealtimeTarget(RealtimeTarget{
							FrameDrop: RealtimeFrameDropEnabled,
						}))
				case 14:
					mustVP9Runtime(t, "SetRealtimeTarget fixed-q drop disabled",
						enc.SetRealtimeTarget(RealtimeTarget{
							FrameDrop: RealtimeFrameDropDisabled,
						}))
				}
			},
			extraArgs: append(vp9OracleCBRArgs(140, 400, 300, 350, 0),
				"--min-q=20", "--max-q=20",
				"--drop-frame-schedule=2:60,14:0"),
			wantDrop: true,
		},
		{
			name: "fixed-q-window-under-drop-pressure",
			opts: func() VP9EncoderOptions {
				opts := dropOpts(140)
				opts.DropFrameAllowed = true
				return opts
			}(),
			before: func(t *testing.T, enc *VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 4:
					mustVP9Runtime(t, "SetRealtimeTarget fixed q under drop",
						enc.SetRealtimeTarget(RealtimeTarget{
							MinQuantizer: 20,
							MaxQuantizer: 20,
						}))
				case 14:
					mustVP9Runtime(t, "SetRealtimeTarget q band restore after drop",
						enc.SetRealtimeTarget(RealtimeTarget{
							MinQuantizer: 4,
							MaxQuantizer: 56,
						}))
				}
			},
			extraArgs: append(vp9OracleCBRArgs(140, 400, 300, 350, 60),
				"--min-q-schedule=4:20,14:4",
				"--max-q-schedule=4:20,14:56"),
			wantDrop: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := newVP9OracleTransitionSources(width, height, frames)
			govpxRows, govpxPackets, libvpxRows, libvpxPackets :=
				captureVP9StreamParityPacketRowsWithHooks(t, tc.opts,
					sources, nil, tc.extraArgs,
					func(enc *VP9Encoder, frame int) {
						tc.before(t, enc, frame)
					})
			stats := compareVP9OracleTransitionRows(t, govpxRows, libvpxRows)
			matches, packetMatches, dropMatches, firstMismatch :=
				countVP9ByteParityMatchesWithDrops(t, govpxRows, govpxPackets,
					libvpxRows, libvpxPackets)
			govpxDrops := vp9DroppedFrameIndices(govpxRows)
			libvpxDrops := vp9DroppedFrameIndices(libvpxRows)
			t.Logf("VP9 runtime-drop byte-parity scoreboard %s: rows=%d matches=%d packet_matches=%d drop_matches=%d first_mismatch=%d govpx_drops=%v libvpx_drops=%v transition=%s",
				tc.name, len(govpxRows), matches, packetMatches, dropMatches,
				firstMismatch, govpxDrops, libvpxDrops, stats)
			t.Logf("VP9 runtime-drop byte-parity rows %s:\n%s", tc.name,
				formatVP9DropAwareStreamParityRows(t, govpxRows, govpxPackets,
					libvpxRows, libvpxPackets))
			if tc.wantDrop && (len(govpxDrops) == 0 || len(libvpxDrops) == 0) {
				t.Fatalf("drop fixture %s did not drop on both sides: govpx=%v libvpx=%v",
					tc.name, govpxDrops, libvpxDrops)
			}
			if os.Getenv("GOVPX_VP9_RUNTIME_DROP_BYTE_STRICT") == "1" &&
				(matches != len(govpxRows) || stats.hasMismatch()) {
				t.Fatalf("strict VP9 runtime-drop mismatch %s: matches=%d/%d stats=%s",
					tc.name, matches, len(govpxRows), stats)
			}
		})
	}
}

func TestVP9OracleTemporalPatternByteParityScoreboard(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run VP9 temporal byte-parity scoreboard")
	}
	requireVP9VpxencFrameFlagsOracle(t)

	const width, height, frames, targetKbps = 64, 64, 16, 700
	cases := []struct {
		name string
		mode TemporalLayeringMode
	}{
		{name: "two-layer", mode: TemporalLayeringTwoLayers},
		{name: "three-layer-default", mode: TemporalLayeringThreeLayers},
		{name: "three-layer-no-inter-layer-prediction", mode: TemporalLayeringThreeLayersNoInterLayerPrediction},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pattern, ok := temporalLayeringPattern(tc.mode)
			if !ok {
				t.Fatalf("temporalLayeringPattern(%d) failed", tc.mode)
			}
			opts := vp9OracleCBROptions(width, height, targetKbps)
			opts.TemporalScalability = vp9OracleTemporalConfig(tc.mode,
				targetKbps)
			sources := newVP9OracleTransitionSources(width, height, frames)
			flags := vp9OracleTemporalPatternFlags(pattern, frames)
			extraArgs := append(vp9OracleCBRArgs(targetKbps, 600, 400, 500, 0),
				vp9OracleTemporalArgs(t, tc.mode, targetKbps)...)
			govpxPackets, libvpxPackets := captureVP9StreamParityPackets(t,
				opts, sources, flags, extraArgs)
			matches, firstMismatch := countVP9ByteParityMatches(govpxPackets,
				libvpxPackets)
			t.Logf("VP9 temporal byte-parity scoreboard %s: matches=%d/%d first_mismatch=%d",
				tc.name, matches, len(govpxPackets), firstMismatch)
			t.Logf("VP9 temporal byte-parity rows %s:\n%s", tc.name,
				formatVP9StreamParityRows(t, govpxPackets, libvpxPackets))
			if os.Getenv("GOVPX_VP9_TEMPORAL_BYTE_STRICT") == "1" &&
				matches != len(govpxPackets) {
				t.Fatalf("strict VP9 temporal byte parity %s: matches=%d/%d",
					tc.name, matches, len(govpxPackets))
			}
		})
	}
}

func TestVP9OracleEncoderStreamByteParityLookaheadFlushBursts(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run VP9 lookahead flush byte-parity scoreboard")
	}
	requireVP9VpxencOracle(t)

	const width, height = 64, 64
	type flushCase struct {
		name        string
		lag         int
		frames      int
		flushAfter  []int
		exactPrefix int
	}
	cases := []flushCase{
		{
			name:        "lag1-mid-flush",
			lag:         1,
			frames:      5,
			flushAfter:  []int{2},
			exactPrefix: 5,
		},
		{
			name:        "lag2-two-bursts",
			lag:         2,
			frames:      6,
			flushAfter:  []int{2, 4},
			exactPrefix: 6,
		},
		{
			name:        "lag4-early-drain",
			lag:         4,
			frames:      8,
			flushAfter:  []int{3},
			exactPrefix: 8,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := makeVP9SteppedOracleSources(width, height, tc.frames)
			govpxPackets := captureVP9LookaheadPacketsWithFlushesForOracleTest(t,
				VP9EncoderOptions{LookaheadFrames: tc.lag}, sources, tc.flushAfter)
			libvpxPackets := captureVP9VpxencPacketsForOracleTest(t, sources,
				fmt.Sprintf("--lag-in-frames=%d", tc.lag), "--auto-alt-ref=0")
			if len(govpxPackets) != len(libvpxPackets) {
				t.Fatalf("VP9 lookahead flush packets: govpx=%d libvpx=%d",
					len(govpxPackets), len(libvpxPackets))
			}
			matches, firstMismatch := countVP9ByteParityMatches(govpxPackets,
				libvpxPackets)
			t.Logf("VP9 lookahead flush byte-parity scoreboard %s: matches=%d/%d first_mismatch=%d exact_prefix=%d",
				tc.name, matches, len(govpxPackets), firstMismatch, tc.exactPrefix)
			t.Logf("VP9 lookahead flush byte-parity rows %s:\n%s", tc.name,
				formatVP9StreamParityRows(t, govpxPackets, libvpxPackets))
			for frame := 0; frame < tc.exactPrefix; frame++ {
				if !bytes.Equal(govpxPackets[frame], libvpxPackets[frame]) {
					t.Fatalf("frame %d should be inside exact prefix for %s",
						frame, tc.name)
				}
			}
			if os.Getenv("GOVPX_VP9_LOOKAHEAD_FLUSH_STRICT") == "1" &&
				matches != len(govpxPackets) {
				t.Fatalf("strict VP9 lookahead flush byte parity %s: matches=%d/%d",
					tc.name, matches, len(govpxPackets))
			}
		})
	}
}

func TestVP9OracleEncoderStreamByteParityAutoAltRefVisibilityScoreboard(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run VP9 auto-alt-ref visibility scoreboard")
	}
	requireVP9VpxencFrameFlagsOracle(t)

	const width, height, frames, lag = 64, 64, 16, 4
	sources := makeVP9SteppedOracleSources(width, height, frames)
	govpxRows, govpxPackets := captureGovpxVP9AutoAltRefPacketRowsForOracleTest(t,
		VP9EncoderOptions{
			LookaheadFrames: lag,
			AutoAltRef:      true,
			ARNRMaxFrames:   7,
			ARNRStrength:    3,
			ARNRType:        3,
		}, sources)
	libvpxRows, libvpxPackets := captureLibvpxVP9AutoAltRefPacketRowsForOracleTest(t,
		sources,
		"--deadline=good",
		"--cpu-used=4",
		"--end-usage=vbr",
		"--target-bitrate=300",
		fmt.Sprintf("--lag-in-frames=%d", lag),
		"--auto-alt-ref=1",
		"--arnr-maxframes=7",
		"--arnr-strength=3",
		"--arnr-type=3")
	govpxHidden := countVP9HiddenRows(govpxRows)
	libvpxHidden := countVP9HiddenRows(libvpxRows)
	limit := len(govpxPackets)
	if len(libvpxPackets) < limit {
		limit = len(libvpxPackets)
	}
	matches := 0
	firstMismatch := -1
	for i := 0; i < limit; i++ {
		if bytes.Equal(govpxPackets[i], libvpxPackets[i]) {
			matches++
			continue
		}
		if firstMismatch < 0 {
			firstMismatch = i
		}
	}
	t.Logf("VP9 auto-alt-ref visibility scoreboard: govpx_packets=%d libvpx_packets=%d compare=%d matches=%d first_mismatch=%d govpx_hidden=%d libvpx_hidden=%d govpx_altref_refresh=%d libvpx_altref_refresh=%d",
		len(govpxPackets), len(libvpxPackets), limit, matches, firstMismatch,
		govpxHidden, libvpxHidden, countVP9AltRefRefreshRows(govpxRows),
		countVP9AltRefRefreshRows(libvpxRows))
	t.Logf("VP9 auto-alt-ref visibility rows:\n%s",
		formatVP9AutoAltRefVisibilityRows(govpxRows, libvpxRows))
	if govpxHidden == 0 {
		t.Fatal("govpx emitted no hidden auto-alt-ref packet")
	}
	if libvpxHidden == 0 {
		t.Log("libvpx emitted no hidden auto-alt-ref packet for this one-pass scoreboard fixture")
	}
	if os.Getenv("GOVPX_VP9_AUTO_ALT_REF_STRICT") == "1" &&
		(len(govpxPackets) != len(libvpxPackets) || matches != len(govpxPackets)) {
		t.Fatalf("strict VP9 auto-alt-ref byte parity: matches=%d/%d libvpx_packets=%d",
			matches, len(govpxPackets), len(libvpxPackets))
	}
}

func TestVP9OracleEncoderStreamByteParityAutoAltRefARNRMatrix(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run VP9 auto-alt-ref ARNR byte-parity matrix")
	}
	requireVP9VpxencFrameFlagsOracle(t)

	type autoAltRefCase struct {
		name      string
		width     int
		height    int
		frames    int
		lag       int
		targetKbs int
		source    func(width, height, frame int) *image.YCbCr
		arnrType  int
	}
	cases := []autoAltRefCase{
		{
			name:      "stepped-64x64-centered",
			width:     64,
			height:    64,
			frames:    16,
			lag:       4,
			targetKbs: 300,
			source: func(width, height, frame int) *image.YCbCr {
				return newVP9YCbCrForTest(width, height,
					uint8(96+frame*8), 128, 128)
			},
			arnrType: 3,
		},
		{
			name:      "panning-320x180-backward",
			width:     320,
			height:    180,
			frames:    12,
			lag:       4,
			targetKbs: 900,
			source:    newVP9PanningYCbCrForRateTest,
			arnrType:  1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]*image.YCbCr, tc.frames)
			for i := range sources {
				sources[i] = tc.source(tc.width, tc.height, i)
			}
			govpxRows, govpxPackets := captureGovpxVP9AutoAltRefPacketRowsForOracleTest(t,
				VP9EncoderOptions{
					LookaheadFrames: tc.lag,
					AutoAltRef:      true,
					ARNRMaxFrames:   7,
					ARNRStrength:    3,
					ARNRType:        tc.arnrType,
				}, sources)
			libvpxRows, libvpxPackets := captureLibvpxVP9AutoAltRefPacketRowsForOracleTest(t,
				sources,
				"--deadline=good",
				"--cpu-used=4",
				"--end-usage=vbr",
				"--target-bitrate="+fmt.Sprintf("%d", tc.targetKbs),
				fmt.Sprintf("--lag-in-frames=%d", tc.lag),
				"--auto-alt-ref=1",
				"--arnr-maxframes=7",
				"--arnr-strength=3",
				fmt.Sprintf("--arnr-type=%d", tc.arnrType))
			limit := len(govpxPackets)
			if len(libvpxPackets) < limit {
				limit = len(libvpxPackets)
			}
			matches := 0
			firstMismatch := -1
			for i := 0; i < limit; i++ {
				if bytes.Equal(govpxPackets[i], libvpxPackets[i]) {
					matches++
					continue
				}
				if firstMismatch < 0 {
					firstMismatch = i
				}
			}
			t.Logf("VP9 auto-alt-ref ARNR byte-parity matrix %s: govpx_packets=%d libvpx_packets=%d compare=%d matches=%d first_mismatch=%d govpx_hidden=%d libvpx_hidden=%d",
				tc.name, len(govpxPackets), len(libvpxPackets), limit, matches,
				firstMismatch, countVP9HiddenRows(govpxRows),
				countVP9HiddenRows(libvpxRows))
			t.Logf("VP9 auto-alt-ref ARNR rows %s:\n%s", tc.name,
				formatVP9AutoAltRefVisibilityRows(govpxRows, libvpxRows))
			if countVP9HiddenRows(govpxRows) == 0 {
				t.Fatalf("govpx emitted no hidden auto-alt-ref packet for %s",
					tc.name)
			}
			if os.Getenv("GOVPX_VP9_AUTO_ALT_REF_ARNR_BYTE_STRICT") == "1" &&
				(len(govpxPackets) != len(libvpxPackets) ||
					matches != len(govpxPackets)) {
				t.Fatalf("strict VP9 auto-alt-ref ARNR byte parity %s: matches=%d/%d libvpx_packets=%d",
					tc.name, matches, len(govpxPackets), len(libvpxPackets))
			}
		})
	}
}

func makeVP9SteppedOracleSources(width, height, frames int) []*image.YCbCr {
	sources := make([]*image.YCbCr, frames)
	for i := range sources {
		sources[i] = newVP9YCbCrForTest(width, height, uint8(96+i*8), 128, 128)
	}
	return sources
}

func makeVP9RuntimeResizeSources(w0, h0, w1, h1, resizeFrame, frames int) []*image.YCbCr {
	sources := make([]*image.YCbCr, frames)
	for i := range sources {
		width, height := w0, h0
		if i >= resizeFrame {
			width, height = w1, h1
		}
		sources[i] = newVP9PanningYCbCrForRateTest(width, height, i)
	}
	return sources
}

func countVP9ByteParityMatches(govpxPackets, libvpxPackets [][]byte) (matches int, firstMismatch int) {
	firstMismatch = -1
	for i := range govpxPackets {
		if bytes.Equal(govpxPackets[i], libvpxPackets[i]) {
			matches++
			continue
		}
		if firstMismatch < 0 {
			firstMismatch = i
		}
	}
	return matches, firstMismatch
}

func countVP9ByteParityMatchesWithDrops(t *testing.T,
	govpxRows []vp9RateScoreboardRow, govpxPackets [][]byte,
	libvpxRows []vp9RateScoreboardRow, libvpxPackets [][]byte,
) (matches int, packetMatches int, dropMatches int, firstMismatch int) {
	t.Helper()
	if len(govpxRows) != len(libvpxRows) ||
		len(govpxPackets) != len(govpxRows) ||
		len(libvpxPackets) != len(libvpxRows) {
		t.Fatalf("VP9 drop-aware parity row/packet count mismatch: govpx_rows=%d govpx_packets=%d libvpx_rows=%d libvpx_packets=%d",
			len(govpxRows), len(govpxPackets), len(libvpxRows),
			len(libvpxPackets))
	}
	firstMismatch = -1
	for i := range govpxRows {
		gDrop := govpxRows[i].Dropped
		lDrop := libvpxRows[i].Dropped
		switch {
		case gDrop && lDrop:
			matches++
			dropMatches++
		case gDrop || lDrop:
			if firstMismatch < 0 {
				firstMismatch = i
			}
		case len(govpxPackets[i]) != 0 && bytes.Equal(govpxPackets[i], libvpxPackets[i]):
			matches++
			packetMatches++
		default:
			if firstMismatch < 0 {
				firstMismatch = i
			}
		}
	}
	return matches, packetMatches, dropMatches, firstMismatch
}

func captureVP9LookaheadPacketsWithFlushesForOracleTest(t *testing.T,
	opts VP9EncoderOptions, sources []*image.YCbCr, flushAfter []int,
) [][]byte {
	t.Helper()
	if len(sources) == 0 {
		t.Fatal("empty VP9 lookahead flush source")
	}
	width := sources[0].Rect.Dx()
	height := sources[0].Rect.Dy()
	for i, src := range sources {
		if src.Rect.Dx() != width || src.Rect.Dy() != height {
			t.Fatalf("source %d dimension mismatch: got %dx%d want %dx%d",
				i, src.Rect.Dx(), src.Rect.Dy(), width, height)
		}
	}
	opts.Width = width
	opts.Height = height
	enc, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dstSize, err := vp9AllocatingEncodeBufferSize(width, height)
	if err != nil {
		t.Fatalf("vp9AllocatingEncodeBufferSize: %v", err)
	}
	dst := make([]byte, dstSize)
	flushSet := vp9OracleFlushIndexSet(flushAfter)
	packets := make([][]byte, 0, len(sources))
	for i, src := range sources {
		result, err := enc.EncodeIntoWithResult(src, dst)
		if errors.Is(err, ErrFrameNotReady) {
			// Keep filling the lookahead queue.
		} else if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", i, err)
		} else {
			if result.Dropped {
				t.Fatalf("EncodeIntoWithResult frame %d unexpectedly dropped", i)
			}
			packets = append(packets, append([]byte(nil), result.Data...))
		}
		if flushSet[i] {
			packets = append(packets,
				drainVP9LookaheadFlushForOracleTest(t, enc, dst)...)
		}
	}
	packets = append(packets, drainVP9LookaheadFlushForOracleTest(t, enc, dst)...)
	if len(packets) != len(sources) {
		t.Fatalf("VP9 lookahead flush packets = %d, want %d",
			len(packets), len(sources))
	}
	return packets
}

func drainVP9LookaheadFlushForOracleTest(t *testing.T, enc *VP9Encoder, dst []byte) [][]byte {
	t.Helper()
	var packets [][]byte
	for {
		result, err := enc.FlushIntoWithResult(dst)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("FlushIntoWithResult: %v", err)
		}
		if result.Dropped {
			t.Fatal("FlushIntoWithResult unexpectedly dropped")
		}
		packets = append(packets, append([]byte(nil), result.Data...))
	}
	return packets
}

func captureGovpxVP9AutoAltRefPacketRowsForOracleTest(t *testing.T,
	opts VP9EncoderOptions, sources []*image.YCbCr,
) ([]vp9RateScoreboardRow, [][]byte) {
	t.Helper()
	if len(sources) == 0 {
		t.Fatal("empty VP9 auto-alt-ref source")
	}
	opts.Width = sources[0].Rect.Dx()
	opts.Height = sources[0].Rect.Dy()
	enc, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	var trace bytes.Buffer
	enc.SetVP9OracleTraceWriter(&trace)
	dstSize, err := vp9AllocatingEncodeBufferSize(opts.Width, opts.Height)
	if err != nil {
		t.Fatalf("vp9AllocatingEncodeBufferSize: %v", err)
	}
	dst := make([]byte, dstSize)
	packets := make([][]byte, 0, len(sources)+1)
	for i, src := range sources {
		result, err := enc.EncodeIntoWithResult(src, dst)
		if errors.Is(err, ErrFrameNotReady) {
			continue
		}
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", i, err)
		}
		if result.Dropped {
			t.Fatalf("EncodeIntoWithResult frame %d unexpectedly dropped", i)
		}
		packets = append(packets, append([]byte(nil), result.Data...))
	}
	for {
		result, err := enc.FlushIntoWithResult(dst)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("FlushIntoWithResult: %v", err)
		}
		if result.Dropped {
			t.Fatal("FlushIntoWithResult unexpectedly dropped")
		}
		packets = append(packets, append([]byte(nil), result.Data...))
	}
	rows := parseVP9RateScoreboardRows(t, trace.Bytes())
	if len(rows) != len(packets) {
		t.Fatalf("govpx auto-alt-ref trace rows = %d, packets = %d",
			len(rows), len(packets))
	}
	for i := range rows {
		enrichVP9RateScoreboardRowFromPacket(t, &rows[i], packets[i])
	}
	return rows, packets
}

func captureLibvpxVP9AutoAltRefPacketRowsForOracleTest(t *testing.T,
	sources []*image.YCbCr, extraArgs ...string,
) ([]vp9RateScoreboardRow, [][]byte) {
	t.Helper()
	if len(sources) == 0 {
		t.Fatal("empty VP9 libvpx auto-alt-ref source")
	}
	width := sources[0].Rect.Dx()
	height := sources[0].Rect.Dy()
	var raw []byte
	for i, src := range sources {
		if src.Rect.Dx() != width || src.Rect.Dy() != height {
			t.Fatalf("source %d dimension mismatch: got %dx%d want %dx%d",
				i, src.Rect.Dx(), src.Rect.Dy(), width, height)
		}
		raw = appendVP9YCbCrI420(raw, src)
	}
	ivf, trace, diag, err := coracle.VpxencVP9FrameFlagsTraceI420(raw, width,
		height, len(sources), nil, extraArgs...)
	if err != nil {
		t.Fatalf("VpxencVP9FrameFlagsTraceI420 failed: %v\n%s", err, diag)
	}
	rows := parseVP9RateScoreboardRows(t, trace)
	wantPackets := 0
	for _, row := range rows {
		if !row.Dropped {
			wantPackets++
		}
	}
	gotPackets, err := testutil.CountIVFFrames(ivf)
	if err != nil {
		t.Fatalf("CountIVFFrames: %v", err)
	}
	if gotPackets != wantPackets {
		t.Fatalf("libvpx auto-alt-ref IVF packets = %d, want %d",
			gotPackets, wantPackets)
	}
	packets := make([][]byte, len(rows))
	if wantPackets == 0 {
		return rows, packets
	}
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset: %v", err)
	}
	packetIndex := 0
	for i := range rows {
		if rows[i].Dropped {
			continue
		}
		var frame testutil.IVFFrame
		frame, offset, err = testutil.NextIVFFrame(ivf, offset, packetIndex)
		if err != nil {
			t.Fatalf("NextIVFFrame[%d]: %v", packetIndex, err)
		}
		packets[i] = append([]byte(nil), frame.Data...)
		enrichVP9RateScoreboardRowFromPacket(t, &rows[i], packets[i])
		packetIndex++
	}
	return rows, packets
}

func countVP9HiddenRows(rows []vp9RateScoreboardRow) int {
	count := 0
	for _, row := range rows {
		if !row.Dropped && !row.ShowFrame {
			count++
		}
	}
	return count
}

func countVP9AltRefRefreshRows(rows []vp9RateScoreboardRow) int {
	count := 0
	for _, row := range rows {
		if !row.Dropped && !row.KeyFrame &&
			row.RefreshFrameFlags&(1<<vp9AltRefSlot) != 0 {
			count++
		}
	}
	return count
}

func formatVP9AutoAltRefVisibilityRows(govpxRows, libvpxRows []vp9RateScoreboardRow) string {
	var b bytes.Buffer
	fmt.Fprintln(&b, "packet,govpx_frame,libvpx_frame,govpx_show,libvpx_show,govpx_key,libvpx_key,govpx_refresh,libvpx_refresh,govpx_q,libvpx_q,govpx_bytes,libvpx_bytes,govpx_first_part,libvpx_first_part")
	limit := len(govpxRows)
	if len(libvpxRows) > limit {
		limit = len(libvpxRows)
	}
	for i := 0; i < limit; i++ {
		g, gok := vp9ScoreboardRowAt(govpxRows, i)
		l, lok := vp9ScoreboardRowAt(libvpxRows, i)
		fmt.Fprintf(&b, "%d,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s\n",
			i,
			vp9OptionalInt(gok, g.FrameIndex),
			vp9OptionalInt(lok, l.FrameIndex),
			vp9OptionalBool(gok, g.ShowFrame),
			vp9OptionalBool(lok, l.ShowFrame),
			vp9OptionalBool(gok, g.KeyFrame),
			vp9OptionalBool(lok, l.KeyFrame),
			vp9OptionalHex(gok, g.RefreshFrameFlags),
			vp9OptionalHex(lok, l.RefreshFrameFlags),
			vp9OptionalInt(gok, g.BaseQIndex),
			vp9OptionalInt(lok, l.BaseQIndex),
			vp9OptionalInt(gok, g.SizeBytes),
			vp9OptionalInt(lok, l.SizeBytes),
			vp9OptionalInt(gok, g.FirstPartitionSize),
			vp9OptionalInt(lok, l.FirstPartitionSize))
	}
	return b.String()
}

func vp9ScoreboardRowAt(rows []vp9RateScoreboardRow, i int) (vp9RateScoreboardRow, bool) {
	if i < 0 || i >= len(rows) {
		return vp9RateScoreboardRow{}, false
	}
	return rows[i], true
}

func vp9OptionalInt(ok bool, v int) string {
	if !ok {
		return "-"
	}
	return fmt.Sprintf("%d", v)
}

func vp9OptionalBool(ok bool, v bool) string {
	if !ok {
		return "-"
	}
	return fmt.Sprintf("%t", v)
}

func vp9OptionalHex(ok bool, v uint8) string {
	if !ok {
		return "-"
	}
	return fmt.Sprintf("%#x", v)
}

func vp9OracleFlushIndexSet(indexes []int) map[int]bool {
	set := make(map[int]bool, len(indexes))
	for _, index := range indexes {
		set[index] = true
	}
	return set
}

func captureVP9VpxencPacketsForOracleTest(t *testing.T,
	sources []*image.YCbCr, extraArgs ...string,
) [][]byte {
	t.Helper()
	if len(sources) == 0 {
		t.Fatal("empty VP9 vpxenc source")
	}
	width := sources[0].Rect.Dx()
	height := sources[0].Rect.Dy()
	for i, src := range sources {
		if src.Rect.Dx() != width || src.Rect.Dy() != height {
			t.Fatalf("source %d dimension mismatch: got %dx%d want %dx%d",
				i, src.Rect.Dx(), src.Rect.Dy(), width, height)
		}
	}
	var raw []byte
	for _, src := range sources {
		raw = appendVP9YCbCrI420(raw, src)
	}
	ivf, diag, err := coracle.VpxencVP9EncodeI420(raw, width, height,
		len(sources), extraArgs...)
	if err != nil {
		t.Fatalf("vpxenc-vp9 encode failed: %v\n%s", err, diag)
	}
	count, err := testutil.CountIVFFrames(ivf)
	if err != nil {
		t.Fatalf("CountIVFFrames: %v", err)
	}
	if count != len(sources) {
		t.Fatalf("IVF frame count = %d, want %d", count, len(sources))
	}
	packets := make([][]byte, len(sources))
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset: %v", err)
	}
	for i := range packets {
		var frame testutil.IVFFrame
		frame, offset, err = testutil.NextIVFFrame(ivf, offset, i)
		if err != nil {
			t.Fatalf("NextIVFFrame[%d]: %v", i, err)
		}
		packets[i] = append([]byte(nil), frame.Data...)
	}
	return packets
}

func captureVP9StreamParityPackets(t *testing.T, opts VP9EncoderOptions,
	sources []*image.YCbCr, flags []EncodeFlags, extraArgs []string,
) ([][]byte, [][]byte) {
	t.Helper()
	return captureVP9StreamParityPacketsWithHooks(t, opts, sources, flags,
		extraArgs, nil)
}

func captureVP9StreamParityPacketsWithHooks(t *testing.T, opts VP9EncoderOptions,
	sources []*image.YCbCr, flags []EncodeFlags, extraArgs []string,
	beforeFrame func(*VP9Encoder, int),
) ([][]byte, [][]byte) {
	t.Helper()
	if len(sources) == 0 {
		t.Fatal("empty VP9 stream parity source")
	}
	if len(flags) > len(sources) {
		t.Fatalf("VP9 stream parity flag count = %d, want <= %d",
			len(flags), len(sources))
	}
	width := sources[0].Rect.Dx()
	height := sources[0].Rect.Dy()
	for i, src := range sources {
		if src.Rect.Dx() != width || src.Rect.Dy() != height {
			t.Fatalf("source %d dimension mismatch: got %dx%d want %dx%d",
				i, src.Rect.Dx(), src.Rect.Dy(), width, height)
		}
	}

	opts.Width = width
	opts.Height = height
	enc, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dstSize, err := vp9AllocatingEncodeBufferSize(width, height)
	if err != nil {
		t.Fatalf("vp9AllocatingEncodeBufferSize: %v", err)
	}
	dst := make([]byte, dstSize)
	govpxPackets := make([][]byte, len(sources))
	for i, src := range sources {
		if beforeFrame != nil {
			beforeFrame(enc, i)
		}
		var f EncodeFlags
		if i < len(flags) {
			f = flags[i]
		}
		if f&EncodeInvisibleFrame != 0 {
			t.Fatalf("frame %d uses EncodeInvisibleFrame, which has no VP9 libvpx flag bit", i)
		}
		result, err := enc.EncodeIntoWithFlagsResult(src, dst, f)
		if err != nil {
			t.Fatalf("EncodeIntoWithFlagsResult frame %d: %v", i, err)
		}
		if result.Dropped {
			t.Fatalf("EncodeIntoWithFlagsResult frame %d unexpectedly dropped", i)
		}
		govpxPackets[i] = append([]byte(nil), result.Data...)
	}

	libvpxFlags := make([]uint32, len(flags))
	for i, f := range flags {
		libvpxFlags[i] = vp9FrameFlagsForLibvpx(f)
	}
	var raw []byte
	for _, src := range sources {
		raw = appendVP9YCbCrI420(raw, src)
	}
	ivf, diag, err := coracle.VpxencVP9FrameFlagsEncodeI420(raw, width,
		height, len(sources), libvpxFlags, extraArgs...)
	if err != nil {
		t.Fatalf("vpxenc-vp9-frameflags encode failed: %v\n%s", err, diag)
	}
	count, err := testutil.CountIVFFrames(ivf)
	if err != nil {
		t.Fatalf("CountIVFFrames: %v", err)
	}
	if count != len(sources) {
		t.Fatalf("IVF frame count = %d, want %d", count, len(sources))
	}
	libvpxPackets := make([][]byte, len(sources))
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset: %v", err)
	}
	for i := range libvpxPackets {
		var frame testutil.IVFFrame
		frame, offset, err = testutil.NextIVFFrame(ivf, offset, i)
		if err != nil {
			t.Fatalf("NextIVFFrame[%d]: %v", i, err)
		}
		libvpxPackets[i] = append([]byte(nil), frame.Data...)
	}
	return govpxPackets, libvpxPackets
}

func captureVP9StreamParityPacketRowsWithHooks(t *testing.T,
	opts VP9EncoderOptions, sources []*image.YCbCr, flags []EncodeFlags,
	extraArgs []string, beforeFrame func(*VP9Encoder, int),
) ([]vp9RateScoreboardRow, [][]byte, []vp9RateScoreboardRow, [][]byte) {
	t.Helper()
	govpxRows, govpxPackets := captureGovpxVP9StreamParityPacketRowsWithHooks(t,
		opts, sources, flags, beforeFrame)
	libvpxRows, libvpxPackets := captureLibvpxVP9StreamParityPacketRows(t,
		sources, flags, extraArgs)
	return govpxRows, govpxPackets, libvpxRows, libvpxPackets
}

func captureGovpxVP9StreamParityPacketRowsWithHooks(t *testing.T,
	opts VP9EncoderOptions, sources []*image.YCbCr, flags []EncodeFlags,
	beforeFrame func(*VP9Encoder, int),
) ([]vp9RateScoreboardRow, [][]byte) {
	t.Helper()
	if len(sources) == 0 {
		t.Fatal("empty VP9 stream parity source")
	}
	if len(flags) > len(sources) {
		t.Fatalf("VP9 stream parity flag count = %d, want <= %d",
			len(flags), len(sources))
	}
	width := sources[0].Rect.Dx()
	height := sources[0].Rect.Dy()
	for i, src := range sources {
		if src.Rect.Dx() != width || src.Rect.Dy() != height {
			t.Fatalf("source %d dimension mismatch: got %dx%d want %dx%d",
				i, src.Rect.Dx(), src.Rect.Dy(), width, height)
		}
	}
	opts.Width = width
	opts.Height = height
	enc, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	var trace bytes.Buffer
	enc.SetVP9OracleTraceWriter(&trace)
	dstSize, err := vp9AllocatingEncodeBufferSize(width, height)
	if err != nil {
		t.Fatalf("vp9AllocatingEncodeBufferSize: %v", err)
	}
	dst := make([]byte, dstSize)
	packets := make([][]byte, len(sources))
	for i, src := range sources {
		if beforeFrame != nil {
			beforeFrame(enc, i)
		}
		var f EncodeFlags
		if i < len(flags) {
			f = flags[i]
		}
		if f&EncodeInvisibleFrame != 0 {
			t.Fatalf("frame %d uses EncodeInvisibleFrame, which has no VP9 libvpx flag bit", i)
		}
		result, err := enc.EncodeIntoWithFlagsResult(src, dst, f)
		if err != nil {
			t.Fatalf("EncodeIntoWithFlagsResult frame %d: %v", i, err)
		}
		if result.Dropped {
			continue
		}
		packets[i] = append([]byte(nil), result.Data...)
	}
	rows := parseVP9RateScoreboardRows(t, trace.Bytes())
	if len(rows) != len(sources) {
		t.Fatalf("govpx VP9 trace rows = %d, want %d", len(rows), len(sources))
	}
	for i := range rows {
		if rows[i].Dropped {
			continue
		}
		if len(packets[i]) == 0 {
			t.Fatalf("govpx VP9 row %d was not dropped but has no packet", i)
		}
		enrichVP9RateScoreboardRowFromPacket(t, &rows[i], packets[i])
	}
	return rows, packets
}

func captureGovpxVP9VariablePacketRows(t *testing.T,
	opts VP9EncoderOptions, sources []*image.YCbCr, flags []EncodeFlags,
	beforeFrame func(*VP9Encoder, int),
) ([]vp9RateScoreboardRow, [][]byte) {
	t.Helper()
	if len(sources) == 0 {
		t.Fatal("empty VP9 variable-size stream source")
	}
	if len(flags) > len(sources) {
		t.Fatalf("VP9 variable-size flag count = %d, want <= %d",
			len(flags), len(sources))
	}
	opts.Width = sources[0].Rect.Dx()
	opts.Height = sources[0].Rect.Dy()
	enc, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	var trace bytes.Buffer
	enc.SetVP9OracleTraceWriter(&trace)
	packets := make([][]byte, len(sources))
	for i, src := range sources {
		if beforeFrame != nil {
			beforeFrame(enc, i)
		}
		var f EncodeFlags
		if i < len(flags) {
			f = flags[i]
		}
		dstSize, err := vp9AllocatingEncodeBufferSize(src.Rect.Dx(), src.Rect.Dy())
		if err != nil {
			t.Fatalf("vp9AllocatingEncodeBufferSize frame %d: %v", i, err)
		}
		dst := make([]byte, dstSize)
		result, err := enc.EncodeIntoWithFlagsResult(src, dst, f)
		if err != nil {
			t.Fatalf("EncodeIntoWithFlagsResult frame %d: %v", i, err)
		}
		if result.Dropped {
			t.Fatalf("EncodeIntoWithFlagsResult frame %d unexpectedly dropped", i)
		}
		packets[i] = append([]byte(nil), result.Data...)
	}
	rows := parseVP9RateScoreboardRows(t, trace.Bytes())
	if len(rows) != len(sources) {
		t.Fatalf("govpx VP9 variable trace rows = %d, want %d", len(rows), len(sources))
	}
	for i := range rows {
		enrichVP9RateScoreboardRowFromPacket(t, &rows[i], packets[i])
	}
	return rows, packets
}

func captureLibvpxVP9StreamParityPacketRows(t *testing.T,
	sources []*image.YCbCr, flags []EncodeFlags, extraArgs []string,
) ([]vp9RateScoreboardRow, [][]byte) {
	t.Helper()
	if len(sources) == 0 {
		t.Fatal("empty VP9 libvpx stream parity source")
	}
	if len(flags) > len(sources) {
		t.Fatalf("VP9 libvpx stream parity flag count = %d, want <= %d",
			len(flags), len(sources))
	}
	width := sources[0].Rect.Dx()
	height := sources[0].Rect.Dy()
	for i, src := range sources {
		if src.Rect.Dx() != width || src.Rect.Dy() != height {
			t.Fatalf("source %d dimension mismatch: got %dx%d want %dx%d",
				i, src.Rect.Dx(), src.Rect.Dy(), width, height)
		}
	}
	libvpxFlags := make([]uint32, len(flags))
	for i, f := range flags {
		libvpxFlags[i] = vp9FrameFlagsForLibvpx(f)
	}
	var raw []byte
	for _, src := range sources {
		raw = appendVP9YCbCrI420(raw, src)
	}
	ivf, trace, diag, err := coracle.VpxencVP9FrameFlagsTraceI420(raw, width,
		height, len(sources), libvpxFlags, extraArgs...)
	if err != nil {
		t.Fatalf("vpxenc-vp9-frameflags trace failed: %v\n%s", err, diag)
	}
	rows := parseVP9RateScoreboardRows(t, trace)
	if len(rows) != len(sources) {
		t.Fatalf("libvpx VP9 trace rows = %d, want %d", len(rows), len(sources))
	}
	packets := make([][]byte, len(rows))
	wantPackets := 0
	for i := range rows {
		if !rows[i].Dropped {
			wantPackets++
		}
	}
	gotPackets, err := testutil.CountIVFFrames(ivf)
	if err != nil {
		t.Fatalf("CountIVFFrames: %v", err)
	}
	if gotPackets != wantPackets {
		t.Fatalf("libvpx VP9 IVF packets = %d, want %d", gotPackets, wantPackets)
	}
	if wantPackets == 0 {
		return rows, packets
	}
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset: %v", err)
	}
	for i := range rows {
		if rows[i].Dropped {
			continue
		}
		var frame testutil.IVFFrame
		frame, offset, err = testutil.NextIVFFrame(ivf, offset, i)
		if err != nil {
			t.Fatalf("NextIVFFrame[%d]: %v", i, err)
		}
		packets[i] = append([]byte(nil), frame.Data...)
		enrichVP9RateScoreboardRowFromPacket(t, &rows[i], packets[i])
	}
	return rows, packets
}

func captureLibvpxVP9VariablePacketRows(t *testing.T,
	sources []*image.YCbCr, flags []EncodeFlags, invisible []bool,
	extraArgs []string,
) ([]vp9RateScoreboardRow, [][]byte) {
	t.Helper()
	if len(sources) == 0 {
		t.Fatal("empty VP9 libvpx variable-size stream source")
	}
	if len(flags) > len(sources) {
		t.Fatalf("VP9 libvpx variable-size flag count = %d, want <= %d",
			len(flags), len(sources))
	}
	frameSizes := make([]coracle.VpxencVP9FrameSize, len(sources))
	var raw []byte
	for i, src := range sources {
		frameSizes[i] = coracle.VpxencVP9FrameSize{
			Width:  src.Rect.Dx(),
			Height: src.Rect.Dy(),
		}
		raw = appendVP9YCbCrI420(raw, src)
	}
	libvpxFlags := make([]uint32, len(flags))
	for i, f := range flags {
		libvpxFlags[i] = vp9FrameFlagsForLibvpx(f)
	}
	ivf, trace, diag, err := coracle.VpxencVP9FrameFlagsTraceI420WithFrameSizes(
		raw, frameSizes, libvpxFlags, invisible, extraArgs...)
	if err != nil {
		t.Fatalf("vpxenc-vp9-frameflags variable trace failed: %v\n%s", err, diag)
	}
	rows := parseVP9RateScoreboardRows(t, trace)
	if len(rows) != len(sources) {
		t.Fatalf("libvpx VP9 variable trace rows = %d, want %d", len(rows), len(sources))
	}
	packets := make([][]byte, len(rows))
	gotPackets, err := testutil.CountIVFFrames(ivf)
	if err != nil {
		t.Fatalf("CountIVFFrames: %v", err)
	}
	if gotPackets != len(sources) {
		t.Fatalf("libvpx VP9 variable IVF packets = %d, want %d", gotPackets, len(sources))
	}
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset: %v", err)
	}
	for i := range rows {
		var frame testutil.IVFFrame
		frame, offset, err = testutil.NextIVFFrame(ivf, offset, i)
		if err != nil {
			t.Fatalf("NextIVFFrame[%d]: %v", i, err)
		}
		packets[i] = append([]byte(nil), frame.Data...)
		enrichVP9RateScoreboardRowFromPacket(t, &rows[i], packets[i])
	}
	return rows, packets
}

func formatVP9StreamParityRows(t *testing.T, govpxPackets, libvpxPackets [][]byte) string {
	t.Helper()
	var b bytes.Buffer
	fmt.Fprintln(&b, "frame,match,first_diff,govpx_bytes,libvpx_bytes,govpx_q,libvpx_q,govpx_refresh,libvpx_refresh,govpx_first_part,libvpx_first_part")
	for i := range govpxPackets {
		govpxHeader, _ := parseVP9EncoderHeaderForTest(t, govpxPackets[i])
		libvpxHeader, _ := parseVP9EncoderHeaderForTest(t, libvpxPackets[i])
		fmt.Fprintf(&b, "%d,%t,%d,%d,%d,%d,%d,%#x,%#x,%d,%d\n",
			i, bytes.Equal(govpxPackets[i], libvpxPackets[i]),
			firstVP9PacketDiffForTest(govpxPackets[i], libvpxPackets[i]),
			len(govpxPackets[i]), len(libvpxPackets[i]),
			govpxHeader.Quant.BaseQindex, libvpxHeader.Quant.BaseQindex,
			govpxHeader.RefreshFrameFlags, libvpxHeader.RefreshFrameFlags,
			govpxHeader.FirstPartitionSize, libvpxHeader.FirstPartitionSize)
	}
	return b.String()
}

func formatVP9DropAwareStreamParityRows(t *testing.T,
	govpxRows []vp9RateScoreboardRow, govpxPackets [][]byte,
	libvpxRows []vp9RateScoreboardRow, libvpxPackets [][]byte,
) string {
	t.Helper()
	var b bytes.Buffer
	fmt.Fprintln(&b, "frame,row_match,packet_match,first_diff,govpx_drop,libvpx_drop,govpx_bytes,libvpx_bytes,govpx_q,libvpx_q,govpx_target,libvpx_target,govpx_buffer,libvpx_buffer,govpx_refresh,libvpx_refresh,govpx_first_part,libvpx_first_part")
	for i := range govpxRows {
		g := govpxRows[i]
		l := libvpxRows[i]
		packetMatch := false
		if g.Dropped && l.Dropped {
			packetMatch = true
		} else if !g.Dropped && !l.Dropped {
			packetMatch = bytes.Equal(govpxPackets[i], libvpxPackets[i])
		}
		rowMatch := g.Dropped == l.Dropped &&
			g.BaseQIndex == l.BaseQIndex &&
			g.FrameTargetBits == l.FrameTargetBits &&
			g.BufferLevelBits == l.BufferLevelBits &&
			g.RefreshFrameFlags == l.RefreshFrameFlags
		fmt.Fprintf(&b, "%d,%t,%t,%d,%t,%t,%d,%d,%d,%d,%d,%d,%d,%d,%#x,%#x,%d,%d\n",
			g.FrameIndex, rowMatch, packetMatch,
			firstVP9PacketDiffForTest(govpxPackets[i], libvpxPackets[i]),
			g.Dropped, l.Dropped,
			len(govpxPackets[i]), len(libvpxPackets[i]), g.BaseQIndex,
			l.BaseQIndex, g.FrameTargetBits, l.FrameTargetBits,
			g.BufferLevelBits, l.BufferLevelBits, g.RefreshFrameFlags,
			l.RefreshFrameFlags, g.FirstPartitionSize, l.FirstPartitionSize)
	}
	return b.String()
}
