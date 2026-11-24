//go:build govpx_oracle_trace

package govpx

import (
	"fmt"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"image"
	"testing"
)

func TestVP9OracleThreadedTileEncodingMatchesLibvpx(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 threaded tile byte parity")
	vp9test.RequireVpxencFrameFlags(t)

	const width, height, defaultFrames = 1280, 720, 2
	type threadedCase struct {
		name   string
		frames int
		opts   VP9EncoderOptions
		flags  []EncodeFlags
		args   []string
		source func(frame int) *image.YCbCr
		before func(*testing.T, *VP9Encoder, int)
	}
	steppedKeyframe := func(frame int) *image.YCbCr {
		return vp9test.NewYCbCr(width, height,
			uint8(96+frame*8), 128, 128)
	}
	activeMapBefore := func(t *testing.T, enc *VP9Encoder, frame int) {
		t.Helper()
		if frame != 1 {
			return
		}
		activeMap, rows, cols := vp9test.ActiveMap(width, height, "checker")
		mustVP9Runtime(t, "SetActiveMap checker",
			enc.SetActiveMap(activeMap, rows, cols))
	}
	cases := []threadedCase{
		{
			name:   "fixed-q",
			frames: 4,
			opts: VP9EncoderOptions{
				Threads:      4,
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			args: []string{
				"--tile-columns=2",
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--disable-warning-prompt",
			},
		},
		{
			name:   "fixed-q-non-neutral-keyframe",
			frames: 1,
			opts: VP9EncoderOptions{
				Threads:      4,
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			args: []string{
				"--tile-columns=2",
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--disable-warning-prompt",
			},
			source: steppedKeyframe,
		},
		{
			name:   "fixed-q-block-checker-keyframe",
			frames: 1,
			opts: VP9EncoderOptions{
				Threads:      4,
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			args: []string{
				"--tile-columns=2",
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--disable-warning-prompt",
			},
			source: func(frame int) *image.YCbCr {
				return vp9test.NewBlockCheckerYCbCr(width, height,
					frame)
			},
		},
		{
			name:   "fixed-q-force-key-stepped",
			frames: 4,
			opts: VP9EncoderOptions{
				Threads:      4,
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			flags: vp9OracleRepeatAllFramesFlag(4, EncodeForceKeyFrame),
			args: []string{
				"--tile-columns=2",
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--disable-warning-prompt",
			},
			source: steppedKeyframe,
		},
		{
			name:   "fixed-q-force-key-block-checker",
			frames: 2,
			opts: VP9EncoderOptions{
				Threads:      4,
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			flags: vp9OracleRepeatAllFramesFlag(2, EncodeForceKeyFrame),
			args: []string{
				"--tile-columns=2",
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--disable-warning-prompt",
			},
			source: func(frame int) *image.YCbCr {
				return vp9test.NewBlockCheckerYCbCr(width, height,
					frame)
			},
		},
		{
			name:   "fixed-q-active-map",
			frames: 2,
			opts: VP9EncoderOptions{
				Threads:      4,
				MinQuantizer: 20,
				MaxQuantizer: 20,
			},
			args: []string{
				"--tile-columns=2",
				"--cq-level=20",
				"--min-q=20",
				"--max-q=20",
				"--control-script=-,active:checker",
				"--disable-warning-prompt",
			},
			before: activeMapBefore,
		},
		{
			name: "vbr",
			opts: VP9EncoderOptions{
				Threads:             4,
				RateControlModeSet:  true,
				RateControlMode:     RateControlVBR,
				TargetBitrateKbps:   2200,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				MaxKeyframeInterval: 128,
			},
			args: []string{
				"--tile-columns=2",
				"--end-usage=vbr",
				"--target-bitrate=2200",
				"--min-q=4",
				"--max-q=56",
				"--disable-warning-prompt",
			},
		},
		{
			name:   "vbr-active-map",
			frames: 2,
			opts: VP9EncoderOptions{
				Threads:             4,
				RateControlModeSet:  true,
				RateControlMode:     RateControlVBR,
				TargetBitrateKbps:   2200,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				MaxKeyframeInterval: 128,
			},
			args: []string{
				"--tile-columns=2",
				"--end-usage=vbr",
				"--target-bitrate=2200",
				"--min-q=4",
				"--max-q=56",
				"--control-script=-,active:checker",
				"--disable-warning-prompt",
			},
			before: activeMapBefore,
		},
		{
			name: "cq",
			opts: VP9EncoderOptions{
				Threads:             4,
				RateControlModeSet:  true,
				RateControlMode:     RateControlCQ,
				TargetBitrateKbps:   2200,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				CQLevel:             20,
				MaxKeyframeInterval: 128,
			},
			args: []string{
				"--tile-columns=2",
				"--end-usage=cq",
				"--target-bitrate=2200",
				"--min-q=4",
				"--max-q=56",
				"--cq-level=20",
				"--disable-warning-prompt",
			},
		},
		{
			name:   "cq-active-map",
			frames: 2,
			opts: VP9EncoderOptions{
				Threads:             4,
				RateControlModeSet:  true,
				RateControlMode:     RateControlCQ,
				TargetBitrateKbps:   2200,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				CQLevel:             20,
				MaxKeyframeInterval: 128,
			},
			args: []string{
				"--tile-columns=2",
				"--end-usage=cq",
				"--target-bitrate=2200",
				"--min-q=4",
				"--max-q=56",
				"--cq-level=20",
				"--control-script=-,active:checker",
				"--disable-warning-prompt",
			},
			before: activeMapBefore,
		},
		{
			name:   "q",
			frames: 4,
			opts: VP9EncoderOptions{
				Threads:             4,
				RateControlModeSet:  true,
				RateControlMode:     RateControlQ,
				TargetBitrateKbps:   2200,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				CQLevel:             20,
				MaxKeyframeInterval: 128,
			},
			args: []string{
				"--tile-columns=2",
				"--end-usage=q",
				"--target-bitrate=2200",
				"--min-q=4",
				"--max-q=56",
				"--cq-level=20",
				"--disable-warning-prompt",
			},
		},
		{
			name:   "q-active-map",
			frames: 2,
			opts: VP9EncoderOptions{
				Threads:             4,
				RateControlModeSet:  true,
				RateControlMode:     RateControlQ,
				TargetBitrateKbps:   2200,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				CQLevel:             20,
				MaxKeyframeInterval: 128,
			},
			args: []string{
				"--tile-columns=2",
				"--end-usage=q",
				"--target-bitrate=2200",
				"--min-q=4",
				"--max-q=56",
				"--cq-level=20",
				"--control-script=-,active:checker",
				"--disable-warning-prompt",
			},
			before: activeMapBefore,
		},
		{
			name:   "error-resilient",
			frames: 4,
			opts: VP9EncoderOptions{
				Threads:        4,
				ErrorResilient: true,
			},
			args: []string{
				"--tile-columns=2",
				"--error-resilient=1",
				"--disable-warning-prompt",
			},
		},
		{
			name:   "row-mt",
			frames: 4,
			opts: VP9EncoderOptions{
				Threads: 4,
				RowMT:   true,
			},
			args: []string{
				"--tile-columns=2",
				"--row-mt=1",
				"--disable-warning-prompt",
			},
		},
		{
			name:   "force-key-frame3",
			frames: 4,
			opts: VP9EncoderOptions{
				Threads: 4,
			},
			flags: vp9OracleFlagAt(4, 3, EncodeForceKeyFrame),
			args: []string{
				"--tile-columns=2",
				"--disable-warning-prompt",
			},
		},
		{
			name:   "no-reference-all",
			frames: 4,
			opts: VP9EncoderOptions{
				Threads: 4,
			},
			flags: vp9OracleRepeatInterFlag(4,
				EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef),
			args: []string{
				"--tile-columns=2",
				"--disable-warning-prompt",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			frames := tc.frames
			if frames == 0 {
				frames = defaultFrames
			}
			sources := make([]*image.YCbCr, frames)
			source := tc.source
			if source == nil {
				source = func(frame int) *image.YCbCr {
					return vp9test.NewYCbCr(width, height, 128, 128, 128)
				}
			}
			for i := range sources {
				sources[i] = source(i)
			}
			govpxPackets, libvpxPackets := captureVP9StreamParityPacketsWithFrameHooks(t,
				tc.opts, sources, tc.flags, tc.args,
				func(enc *VP9Encoder, frame int) {
					resetVP9OracleThreadedTileJobsForTest(enc)
					if tc.before != nil {
						tc.before(t, enc, frame)
					}
				},
				func(enc *VP9Encoder, frame int) {
					assertVP9OracleThreadedTileWriterUsed(t, enc, frame, 4)
					if tc.opts.RowMT {
						if enc.vp9TilePool == nil {
							t.Fatalf("frame %d: VP9 row-MT tile pool was not initialized", frame)
						}
						if got, want := len(enc.vp9TilePool.rowMTSyncs),
							enc.vp9TilePool.workerCount; got != want {
							t.Fatalf("frame %d: VP9 row-MT syncs = %d, want %d",
								frame, got, want)
						}
					}
				})
			if len(govpxPackets) != len(libvpxPackets) {
				t.Fatalf("threaded 720p %s packet count: govpx=%d libvpx=%d",
					tc.name, len(govpxPackets), len(libvpxPackets))
			}
			for frame := range govpxPackets {
				vp9test.AssertPacketByteParity(t,
					fmt.Sprintf("threaded 720p %s frame %d", tc.name, frame),
					govpxPackets[frame], libvpxPackets[frame])
			}
		})
	}
}
