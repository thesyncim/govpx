//go:build govpx_oracle_trace

package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"
)

func TestVP9OracleRuntimeDropToggleByteParity(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 runtime-drop byte-parity scoreboard")
	vp9test.RequireVpxencFrameFlags(t)

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
			stats := vp9test.CompareTransitionRows(t, govpxRows, libvpxRows, vp9OracleLibvpxFrameFlags)
			matches, packetMatches, dropMatches, firstMismatch :=
				vp9test.CountByteParityMatchesWithDrops(t, govpxRows, govpxPackets,
					libvpxRows, libvpxPackets)
			govpxDrops := vp9test.DroppedFrameIndices(govpxRows)
			libvpxDrops := vp9test.DroppedFrameIndices(libvpxRows)
			t.Logf("VP9 runtime-drop byte-parity scoreboard %s: rows=%d matches=%d packet_matches=%d drop_matches=%d first_mismatch=%d govpx_drops=%v libvpx_drops=%v transition=%s",
				tc.name, len(govpxRows), matches, packetMatches, dropMatches,
				firstMismatch, govpxDrops, libvpxDrops, stats)
			t.Logf("VP9 runtime-drop byte-parity rows %s:\n%s", tc.name,
				vp9test.FormatDropAwareStreamParityRows(t, govpxRows, govpxPackets,
					libvpxRows, libvpxPackets))
			if tc.wantDrop && (len(govpxDrops) == 0 || len(libvpxDrops) == 0) {
				t.Fatalf("drop fixture %s did not drop on both sides: govpx=%v libvpx=%v",
					tc.name, govpxDrops, libvpxDrops)
			}
			if vp9test.StrictEnv("GOVPX_VP9_RUNTIME_DROP_BYTE_STRICT") &&
				(matches != len(govpxRows) || stats.HasMismatch()) {
				t.Fatalf("strict VP9 runtime-drop mismatch %s: matches=%d/%d stats=%s",
					tc.name, matches, len(govpxRows), stats)
			}
		})
	}
}
