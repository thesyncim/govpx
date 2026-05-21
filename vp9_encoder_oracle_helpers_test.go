//go:build govpx_oracle_trace

package govpx

import (
	"image"
	"testing"
)

// encodeVP9FramesWithGovpx constructs a fresh VP9Encoder, encodes every source
// frame with the supplied flags, and returns copied per-frame VP9 payloads.
func encodeVP9FramesWithGovpx(t testing.TB, opts VP9EncoderOptions,
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

func vp9LibvpxFrameFlags(flags []EncodeFlags) []uint32 {
	if len(flags) == 0 {
		return nil
	}
	out := make([]uint32, len(flags))
	for i, flag := range flags {
		out[i] = vp9FrameFlagsForLibvpx(flag)
	}
	return out
}

func vp9FrameFlagsForLibvpx(f EncodeFlags) uint32 {
	const (
		libvpxForceKF      = 1 << 0
		libvpxNoRefLast    = 1 << 16
		libvpxNoRefGF      = 1 << 17
		libvpxNoUpdLast    = 1 << 18
		libvpxForceGF      = 1 << 19
		libvpxNoUpdEntropy = 1 << 20
		libvpxNoRefARF     = 1 << 21
		libvpxNoUpdGF      = 1 << 22
		libvpxNoUpdARF     = 1 << 23
		libvpxForceARF     = 1 << 24
	)
	var out uint32
	if f&EncodeForceKeyFrame != 0 {
		out |= libvpxForceKF
	}
	if f&EncodeNoReferenceLast != 0 {
		out |= libvpxNoRefLast
	}
	if f&EncodeNoReferenceGolden != 0 {
		out |= libvpxNoRefGF
	}
	if f&EncodeNoUpdateLast != 0 {
		out |= libvpxNoUpdLast
	}
	if f&EncodeForceGoldenFrame != 0 {
		out |= libvpxForceGF
	}
	if f&EncodeNoUpdateEntropy != 0 {
		out |= libvpxNoUpdEntropy
	}
	if f&EncodeNoReferenceAltRef != 0 {
		out |= libvpxNoRefARF
	}
	if f&EncodeNoUpdateGolden != 0 {
		out |= libvpxNoUpdGF
	}
	if f&EncodeNoUpdateAltRef != 0 {
		out |= libvpxNoUpdARF
	}
	if f&EncodeForceAltRefFrame != 0 {
		out |= libvpxForceARF
	}
	return out
}
