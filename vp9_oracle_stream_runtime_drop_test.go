//go:build govpx_oracle_trace

package govpx_test

import (
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9oracle"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9OracleRuntimeDropToggleByteParity(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 runtime-drop byte-parity trace")
	vp9test.RequireVpxencFrameFlags(t)

	const width, height, frames = 64, 64, 24
	type runtimeDropCase struct {
		name      string
		opts      govpx.VP9EncoderOptions
		before    func(*testing.T, *govpx.VP9Encoder, int)
		extraArgs []string
		wantDrop  bool
	}
	dropOpts := func(targetKbps int) govpx.VP9EncoderOptions {
		opts := vp9oracle.CBROptions(width, height, targetKbps)
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
			before: func(t *testing.T, enc *govpx.VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 3:
					if err := enc.SetRealtimeTarget(govpx.RealtimeTarget{
						FrameDrop: govpx.RealtimeFrameDropEnabled,
					}); err != nil {
						t.Fatalf("SetRealtimeTarget drop enabled: %v", err)
					}
				case 14:
					if err := enc.SetRealtimeTarget(govpx.RealtimeTarget{
						FrameDrop: govpx.RealtimeFrameDropDisabled,
					}); err != nil {
						t.Fatalf("SetRealtimeTarget drop disabled: %v", err)
					}
				}
			},
			extraArgs: append(vp9oracle.CBRArgs(120, 400, 300, 350, 0),
				"--drop-frame-schedule=3:60,14:0"),
			wantDrop: true,
		},
		{
			name: "fixed-q-drop-frame-toggle",
			opts: func() govpx.VP9EncoderOptions {
				opts := dropOpts(140)
				opts.MinQuantizer = 20
				opts.MaxQuantizer = 20
				return opts
			}(),
			before: func(t *testing.T, enc *govpx.VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 2:
					if err := enc.SetRealtimeTarget(govpx.RealtimeTarget{
						FrameDrop: govpx.RealtimeFrameDropEnabled,
					}); err != nil {
						t.Fatalf("SetRealtimeTarget fixed-q drop enabled: %v", err)
					}
				case 14:
					if err := enc.SetRealtimeTarget(govpx.RealtimeTarget{
						FrameDrop: govpx.RealtimeFrameDropDisabled,
					}); err != nil {
						t.Fatalf("SetRealtimeTarget fixed-q drop disabled: %v", err)
					}
				}
			},
			extraArgs: append(vp9oracle.CBRArgs(140, 400, 300, 350, 0),
				"--min-q=20", "--max-q=20",
				"--drop-frame-schedule=2:60,14:0"),
			wantDrop: true,
		},
		{
			name: "fixed-q-window-under-drop-pressure",
			opts: func() govpx.VP9EncoderOptions {
				opts := dropOpts(140)
				opts.DropFrameAllowed = true
				return opts
			}(),
			before: func(t *testing.T, enc *govpx.VP9Encoder, frame int) {
				t.Helper()
				switch frame {
				case 4:
					if err := enc.SetRealtimeTarget(govpx.RealtimeTarget{
						MinQuantizer: 20,
						MaxQuantizer: 20,
					}); err != nil {
						t.Fatalf("SetRealtimeTarget fixed q under drop: %v", err)
					}
				case 14:
					if err := enc.SetRealtimeTarget(govpx.RealtimeTarget{
						MinQuantizer: 4,
						MaxQuantizer: 56,
					}); err != nil {
						t.Fatalf("SetRealtimeTarget q band restore after drop: %v", err)
					}
				}
			},
			extraArgs: append(vp9oracle.CBRArgs(140, 400, 300, 350, 60),
				"--min-q-schedule=4:20,14:4",
				"--max-q-schedule=4:20,14:56"),
			wantDrop: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := vp9oracle.TransitionSources(width, height, frames)
			govpxRows, govpxPackets, libvpxRows, libvpxPackets :=
				vp9oracle.CaptureStreamParityPacketRowsWithHooks(t, tc.opts,
					sources, nil, tc.extraArgs,
					func(enc *govpx.VP9Encoder, frame int) {
						tc.before(t, enc, frame)
					})
			stats := vp9test.CompareTransitionRows(t, govpxRows, libvpxRows, vp9oracle.RateTraceFlagMapper)
			matches, packetMatches, dropMatches, firstMismatch :=
				vp9test.CountByteParityMatchesWithDrops(t, govpxRows, govpxPackets,
					libvpxRows, libvpxPackets)
			govpxDrops := vp9test.DroppedFrameIndices(govpxRows)
			libvpxDrops := vp9test.DroppedFrameIndices(libvpxRows)
			t.Logf("VP9 runtime-drop byte-parity trace %s: rows=%d matches=%d packet_matches=%d drop_matches=%d first_mismatch=%d govpx_drops=%v libvpx_drops=%v transition=%s",
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
