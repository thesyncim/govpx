//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle/coracletest"
)

// FuzzVP9EncoderRandomStrides mirrors FuzzEncoderRandomStrides for VP9: callers
// feed VP9 encoder *image.YCbCr values with varying Y/Cb/Cr stride padding, and
// the fuzzer asserts that the encoded VP9 keyframe is byte-identical to the
// libvpx vpxenc-vp9 oracle keyframe encoded from the equivalent tight (no-
// padding) I420 content. A stride-walk bug that reads padding bytes will
// surface as a keyframe SHA-256 mismatch.
//
// Gated by GOVPX_WITH_ORACLE=1 plus a built vpxenc-vp9 binary.
func FuzzVP9EncoderRandomStrides(f *testing.F) {
	coracletest.SkipWithoutOracle(f, "VP9 random-strides fuzz")
	coracletest.VpxencVP9(f)
	// Each seed is (dimBucket, yPadBucket, uPadBucket, vPadBucket, uvAlignBucket).
	seeds := [][]byte{
		{0, 0, 0, 0, 0},  // tight 32x32
		{0, 4, 0, 0, 0},  // 32x32 with Y stride +4
		{1, 0, 2, 2, 0},  // 64x64 with U/V stride +2
		{1, 8, 4, 4, 0},  // 64x64 mixed
		{2, 16, 8, 8, 1}, // 128x128 odd alignment
		{2, 0, 0, 0, 1},  // 128x128 chroma extra +1
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		dim, padded, tight, ok := newVP9StridesFuzzImage(data)
		if !ok {
			return
		}
		opts := VP9EncoderOptions{
			Width:        dim.w,
			Height:       dim.h,
			FPS:          30,
			MinQuantizer: 4,
			MaxQuantizer: 56,
			CQLevel:      32,
			CpuUsed:      8,
			Deadline:     DeadlineRealtime,
		}
		sum := sha256.Sum256(data)
		label := "fuzz-vp9-strides-" + hex.EncodeToString(sum[:4])
		t.Logf("%s w=%d h=%d yStride=%d cStride=%d",
			label, dim.w, dim.h, padded.YStride, padded.CStride)

		govpxFrames := encodeVP9FramesWithGovpx(t, opts, []*image.YCbCr{padded}, nil)
		libvpxFrames := encodeVP9FramesWithLibvpxOracle(t, []*image.YCbCr{tight}, nil)
		assertVP9SegmentByteParity(t, label, govpxFrames, libvpxFrames, 0)
	})
}

type vp9StridesFuzzDim struct {
	w, h int
}

// newVP9StridesFuzzImage builds (padded, tight) VP9 *image.YCbCr pairs where
// the visible content matches but `padded` may include arbitrary per-plane
// stride padding. Both images use deterministic fuzz-bytes-driven content
// via newVP9YCbCrForTest so the libvpx oracle's identical-content premise
// holds.
func newVP9StridesFuzzImage(data []byte) (vp9StridesFuzzDim, *image.YCbCr, *image.YCbCr, bool) {
	r := vp9FuzzByteCursor{data: data}
	if len(data) == 0 {
		return vp9StridesFuzzDim{}, nil, nil, false
	}
	dimPool := [...]vp9StridesFuzzDim{
		{32, 32},
		{64, 64},
		{128, 128},
		{160, 96},
	}
	dim := dimPool[r.pick(len(dimPool))]
	yPad := int(r.next() & 0x1f) // 0..31
	uPad := int(r.next() & 0x0f) // 0..15
	_ = int(r.next() & 0x0f)     // consume the vPad byte to keep seed mapping stable
	uvExtra := int(r.next() & 0x01)

	uvW := (dim.w + 1) >> 1
	uvH := (dim.h + 1) >> 1
	yStride := dim.w + yPad
	cStride := uvW + uPad + uvExtra

	// Build the tight image first via the standard test helper. The padded
	// image reuses the same visible content but lays it into wider strides.
	tight := newVP9YCbCrForTest(dim.w, dim.h, 96, 128, 128)
	padded := &image.YCbCr{
		YStride:        yStride,
		CStride:        cStride,
		SubsampleRatio: image.YCbCrSubsampleRatio420,
		Rect:           tight.Rect,
		Y:              make([]byte, yStride*dim.h),
		Cb:             make([]byte, cStride*uvH),
		Cr:             make([]byte, cStride*uvH),
	}
	// Fill padding bytes with a distinctive pattern so a stride-walk
	// bug that reads them shows up as a divergence rather than silently
	// zeroing out.
	for i := range padded.Y {
		padded.Y[i] = 0xa5
	}
	for i := range padded.Cb {
		padded.Cb[i] = 0x5a
	}
	for i := range padded.Cr {
		padded.Cr[i] = 0xa5
	}
	for y := 0; y < dim.h; y++ {
		copy(padded.Y[y*yStride:y*yStride+dim.w], tight.Y[y*tight.YStride:y*tight.YStride+dim.w])
	}
	for y := 0; y < uvH; y++ {
		copy(padded.Cb[y*cStride:y*cStride+uvW], tight.Cb[y*tight.CStride:y*tight.CStride+uvW])
		copy(padded.Cr[y*cStride:y*cStride+uvW], tight.Cr[y*tight.CStride:y*tight.CStride+uvW])
	}
	return dim, padded, tight, true
}
