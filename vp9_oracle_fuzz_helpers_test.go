//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
	"github.com/thesyncim/govpx/internal/testutil"
)

// vp9FuzzByteCursor mirrors oracleRuntimeControlFuzzBytes for the
// VP9 fuzz family. Defined separately so it can live alongside
// govpx_oracle_trace-tagged VP9 fuzzers without dragging in the VP8
// runtime-control machinery.
type vp9FuzzByteCursor struct {
	data []byte
	pos  int
}

func (r *vp9FuzzByteCursor) next() byte {
	if len(r.data) == 0 {
		return 0
	}
	b := r.data[r.pos%len(r.data)]
	r.pos++
	return b
}

func (r *vp9FuzzByteCursor) pick(n int) int {
	if n <= 1 {
		return 0
	}
	return int(r.next()) % n
}

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
		raw = appendVP9YCbCrI420(raw, src)
	}
	ivf, diag, err := coracle.VpxencVP9EncodeI420(raw, width, height, len(sources), extraArgs...)
	if err != nil {
		t.Fatalf("vpxenc-vp9 encode failed: %v\n%s", err, diag)
	}
	return parseVP9IVFFrames(t, ivf)
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
		raw = appendVP9YCbCrI420(raw, src)
	}
	ivf, diag, err := coracle.VpxencVP9FrameFlagsEncodeI420(raw, width, height,
		len(sources), libvpxFlags, extraArgs...)
	if err != nil {
		t.Fatalf("vpxenc-vp9-frameflags encode failed: %v\n%s", err, diag)
	}
	return parseVP9IVFFrames(t, ivf)
}

// parseVP9IVFFrames extracts every IVF frame payload from data.
func parseVP9IVFFrames(t *testing.T, data []byte) [][]byte {
	t.Helper()
	out, err := testutil.IVFFramePayloads(data)
	if err != nil {
		t.Fatalf("IVFFramePayloads: %v", err)
	}
	return out
}

// assertVP9SegmentByteParity compares per-frame VP9 payloads between two
// captures (typically govpx vs libvpx). matchLimit caps how many leading
// frames are asserted strictly: 0 requires the full length, a positive
// value requires only the first matchLimit frames, a negative value logs
// mismatches without asserting. Mirrors assertSegmentByteParity for VP8.
func assertVP9SegmentByteParity(t *testing.T, label string, got, want [][]byte, matchLimit int) {
	t.Helper()
	if len(got) != len(want) {
		if matchLimit < 0 {
			t.Logf("%s: frame count mismatch (logged only): got=%d want=%d", label, len(got), len(want))
		} else {
			t.Errorf("%s: frame count mismatch: got=%d want=%d", label, len(got), len(want))
			if matchLimit == 0 {
				return
			}
		}
	}
	limit := len(got)
	if matchLimit < 0 {
		limit = 0
	} else if matchLimit > 0 && matchLimit < limit {
		limit = matchLimit
	}
	common := min(len(got), len(want))
	for i := 0; i < common; i++ {
		gHash := sha256.Sum256(got[i])
		lHash := sha256.Sum256(want[i])
		if gHash == lHash {
			t.Logf("%s frame %d byte MATCH: len=%d", label, i, len(got[i]))
			continue
		}
		fd := firstVP9PacketDiffForTest(got[i], want[i])
		if i >= limit {
			t.Logf("%s frame %d byte mismatch (not asserted, limit=%d): got_len=%d want_len=%d first_diff=%d got_sha=%s want_sha=%s",
				label, i, limit, len(got[i]), len(want[i]), fd,
				hex.EncodeToString(gHash[:8]), hex.EncodeToString(lHash[:8]))
			continue
		}
		t.Errorf("%s frame %d byte mismatch: got_len=%d want_len=%d first_diff=%d got_sha=%s want_sha=%s",
			label, i, len(got[i]), len(want[i]), fd,
			hex.EncodeToString(gHash[:8]), hex.EncodeToString(lHash[:8]))
	}
}

// newVP9YCbCrFuzzPanning returns a deterministic panning frame suitable for
// fuzz-driven VP9 stream encodes. Mirrors the VP8 encoderValidationPanningFrame
// signature so the fuzz cases can be expressed independent of the *image.YCbCr
// vs Image divergence between VP8 and VP9 surfaces.
func newVP9YCbCrFuzzPanning(width, height, frame int) *image.YCbCr {
	return newVP9PanningYCbCrForRateTest(width, height, frame)
}

// newVP9YCbCrFuzzSources creates `frames` panning sources for a given
// resolution.
func newVP9YCbCrFuzzSources(width, height, frames int) []*image.YCbCr {
	out := make([]*image.YCbCr, frames)
	for i := range out {
		out[i] = newVP9YCbCrFuzzPanning(width, height, i)
	}
	return out
}
