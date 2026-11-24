//go:build govpx_oracle_trace

package govpx_test

import (
	"fmt"
	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9oracle"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"image"
	"testing"
)

func TestVP9OracleRealtimeNewModeMatchesLibvpx(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 realtime new-mode byte parity")
	vp9test.RequireVpxencFrameFlags(t)

	type pinnedCase struct {
		name   string
		width  int
		height int
		frames int
		opts   govpx.VP9EncoderOptions
		args   []string
	}
	rateOptions := func(mode govpx.RateControlMode, targetKbps int) govpx.VP9EncoderOptions {
		opts := govpx.VP9EncoderOptions{
			RateControlModeSet:  true,
			RateControlMode:     mode,
			TargetBitrateKbps:   targetKbps,
			MinQuantizer:        4,
			MaxQuantizer:        56,
			MaxKeyframeInterval: 128,
		}
		if mode == govpx.RateControlCQ || mode == govpx.RateControlQ {
			opts.CQLevel = 20
		}
		return opts
	}
	rateArgs := func(mode govpx.RateControlMode, targetKbps int) []string {
		endUsage := "vbr"
		if mode == govpx.RateControlCQ {
			endUsage = "cq"
		} else if mode == govpx.RateControlQ {
			endUsage = "q"
		}
		args := []string{
			"--end-usage=" + endUsage,
			fmt.Sprintf("--target-bitrate=%d", targetKbps),
			"--min-q=4",
			"--max-q=56",
		}
		if mode == govpx.RateControlCQ || mode == govpx.RateControlQ {
			args = append(args, "--cq-level=20")
		}
		return args
	}
	cases := []pinnedCase{
		{name: "vbr-64x64", width: 64, height: 64, frames: 4,
			opts: rateOptions(govpx.RateControlVBR, 700),
			args: rateArgs(govpx.RateControlVBR, 700)},
		{name: "vbr-320x180", width: 320, height: 180, frames: 4,
			opts: rateOptions(govpx.RateControlVBR, 700),
			args: rateArgs(govpx.RateControlVBR, 700)},
		{name: "vbr-1280x720", width: 1280, height: 720, frames: 2,
			opts: rateOptions(govpx.RateControlVBR, 2200),
			args: rateArgs(govpx.RateControlVBR, 2200)},
		{name: "cq-64x64", width: 64, height: 64, frames: 4,
			opts: rateOptions(govpx.RateControlCQ, 700),
			args: rateArgs(govpx.RateControlCQ, 700)},
		{name: "cq-320x180", width: 320, height: 180, frames: 4,
			opts: rateOptions(govpx.RateControlCQ, 700),
			args: rateArgs(govpx.RateControlCQ, 700)},
		{name: "cq-1280x720", width: 1280, height: 720, frames: 2,
			opts: rateOptions(govpx.RateControlCQ, 2200),
			args: rateArgs(govpx.RateControlCQ, 2200)},
		{name: "q-64x64", width: 64, height: 64, frames: 4,
			opts: rateOptions(govpx.RateControlQ, 700),
			args: rateArgs(govpx.RateControlQ, 700)},
		{name: "q-320x180", width: 320, height: 180, frames: 4,
			opts: rateOptions(govpx.RateControlQ, 700),
			args: rateArgs(govpx.RateControlQ, 700)},
		{name: "q-1280x720", width: 1280, height: 720, frames: 2,
			opts: rateOptions(govpx.RateControlQ, 2200),
			args: rateArgs(govpx.RateControlQ, 2200)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]*image.YCbCr, tc.frames)
			for i := range sources {
				sources[i] = vp9test.NewYCbCr(tc.width, tc.height, 128, 128, 128)
			}
			_, govpxPackets, _, libvpxPackets :=
				vp9oracle.CaptureStreamParityPacketRowsWithHooks(t,
					tc.opts, sources, nil, tc.args, nil)
			if len(govpxPackets) != len(libvpxPackets) {
				t.Fatalf("VP9 new-mode %s packet count: govpx=%d libvpx=%d",
					tc.name, len(govpxPackets), len(libvpxPackets))
			}
			for frame := range govpxPackets {
				vp9test.AssertPacketByteParity(t,
					fmt.Sprintf("VP9 new-mode %s frame %d", tc.name, frame),
					govpxPackets[frame], libvpxPackets[frame])
			}
		})
	}
}
