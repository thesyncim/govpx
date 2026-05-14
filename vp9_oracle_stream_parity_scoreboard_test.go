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
			exactPrefix: 1,
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
			if os.Getenv("GOVPX_VP9_STREAM_MATRIX_STRICT") == "1" &&
				matches != len(govpxPackets) {
				t.Fatalf("strict VP9 stream byte parity %s: matches=%d/%d",
					tc.name, matches, len(govpxPackets))
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
			exactPrefix: 1,
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
			exactPrefix: 1,
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

func makeVP9SteppedOracleSources(width, height, frames int) []*image.YCbCr {
	sources := make([]*image.YCbCr, frames)
	for i := range sources {
		sources[i] = newVP9YCbCrForTest(width, height, uint8(96+i*8), 128, 128)
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

func formatVP9StreamParityRows(t *testing.T, govpxPackets, libvpxPackets [][]byte) string {
	t.Helper()
	var b bytes.Buffer
	fmt.Fprintln(&b, "frame,match,govpx_bytes,libvpx_bytes,govpx_q,libvpx_q,govpx_refresh,libvpx_refresh,govpx_first_part,libvpx_first_part")
	for i := range govpxPackets {
		govpxHeader, _ := parseVP9EncoderHeaderForTest(t, govpxPackets[i])
		libvpxHeader, _ := parseVP9EncoderHeaderForTest(t, libvpxPackets[i])
		fmt.Fprintf(&b, "%d,%t,%d,%d,%d,%d,%#x,%#x,%d,%d\n",
			i, bytes.Equal(govpxPackets[i], libvpxPackets[i]),
			len(govpxPackets[i]), len(libvpxPackets[i]),
			govpxHeader.Quant.BaseQindex, libvpxHeader.Quant.BaseQindex,
			govpxHeader.RefreshFrameFlags, libvpxHeader.RefreshFrameFlags,
			govpxHeader.FirstPartitionSize, libvpxHeader.FirstPartitionSize)
	}
	return b.String()
}
