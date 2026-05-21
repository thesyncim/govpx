//go:build govpx_oracle_trace

package govpx

import (
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

// encodeVP9FramesWithGovpx mirrors encodeFramesWithGovpx for VP9: it constructs
// a fresh VP9Encoder, encodes every source frame with the supplied flags, and
// returns the per-frame VP9 packet payloads (copied).
func encodeVP9FramesWithGovpx(t *testing.T, opts VP9EncoderOptions,
	sources []*image.YCbCr, flags []EncodeFlags,
) [][]byte {
	t.Helper()
	if len(sources) == 0 {
		t.Fatalf("encodeVP9FramesWithGovpx: no sources")
	}
	width := sources[0].Rect.Dx()
	height := sources[0].Rect.Dy()
	opts.Width = width
	opts.Height = height
	enc, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer enc.Close()
	dstSize, err := vp9AllocatingEncodeBufferSize(width, height)
	if err != nil {
		t.Fatalf("vp9AllocatingEncodeBufferSize: %v", err)
	}
	dst := make([]byte, dstSize)
	out := make([][]byte, 0, len(sources))
	for i, src := range sources {
		var f EncodeFlags
		if i < len(flags) {
			f = flags[i]
		}
		result, err := enc.EncodeIntoWithFlagsResult(src, dst, f)
		if err != nil {
			t.Fatalf("VP9 EncodeIntoWithFlagsResult frame %d: %v", i, err)
		}
		if result.Dropped {
			out = append(out, nil)
			continue
		}
		out = append(out, append([]byte(nil), result.Data...))
	}
	return out
}

// encodeVP9FramesWithLibvpxOracle runs the VP9 vpxenc-vp9 oracle and returns
// the per-frame IVF payloads. Extra args are appended after the defaults
// (--rt, --cpu-used=8, fixed-q at cq-level 32, no auto-alt-ref, etc.). This
// matches the canonical VpxencVP9EncodeI420 defaults; per-fuzzer cases that
// need different rate-control modes pass their override via extraArgs.
func encodeVP9FramesWithLibvpxOracle(t *testing.T, sources []*image.YCbCr,
	extraArgs []string,
) [][]byte {
	t.Helper()
	if len(sources) == 0 {
		t.Fatalf("encodeVP9FramesWithLibvpxOracle: no sources")
	}
	width := sources[0].Rect.Dx()
	height := sources[0].Rect.Dy()
	var raw []byte
	for _, src := range sources {
		raw = vp9test.AppendI420(raw, src)
	}
	ivf, diag, err := coracle.VpxencVP9EncodeI420(raw, width, height, len(sources), extraArgs...)
	if err != nil {
		t.Fatalf("vpxenc-vp9 encode failed: %v\n%s", err, diag)
	}
	return vp9test.ParseIVFFrames(t, ivf)
}

// encodeVP9FramesWithLibvpxFrameFlagsOracle runs the
// vpxenc-vp9-frameflags helper with a per-frame flag schedule + extraArgs and
// returns per-frame IVF payloads.
func encodeVP9FramesWithLibvpxFrameFlagsOracle(t *testing.T, sources []*image.YCbCr,
	flags []EncodeFlags, extraArgs []string,
) [][]byte {
	t.Helper()
	if len(sources) == 0 {
		t.Fatalf("encodeVP9FramesWithLibvpxFrameFlagsOracle: no sources")
	}
	width := sources[0].Rect.Dx()
	height := sources[0].Rect.Dy()
	libvpxFlags := make([]uint32, len(flags))
	for i, f := range flags {
		libvpxFlags[i] = vp9FrameFlagsForLibvpx(f)
	}
	var raw []byte
	for _, src := range sources {
		raw = vp9test.AppendI420(raw, src)
	}
	ivf, diag, err := coracle.VpxencVP9FrameFlagsEncodeI420(raw, width, height,
		len(sources), libvpxFlags, extraArgs...)
	if err != nil {
		t.Fatalf("vpxenc-vp9-frameflags encode failed: %v\n%s", err, diag)
	}
	return vp9test.ParseIVFFrames(t, ivf)
}
