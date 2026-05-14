//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
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
			if os.Getenv("GOVPX_VP9_STREAM_MATRIX_STRICT") == "1" &&
				matches != len(govpxPackets) {
				t.Fatalf("strict VP9 stream byte parity %s: matches=%d/%d",
					tc.name, matches, len(govpxPackets))
			}
		})
	}
}

func captureVP9StreamParityPackets(t *testing.T, opts VP9EncoderOptions,
	sources []*image.YCbCr, flags []EncodeFlags, extraArgs []string,
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
