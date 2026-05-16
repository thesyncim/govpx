//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

// FuzzEncoderRandomStrides closes plan-§3 F6 / G7 from the VP8
// byte-exactness tracker: callers feed govpx Image values with
// varying Y/U/V stride padding, and the fuzzer asserts that the
// encoded VP8 keyframe is byte-identical to the libvpx oracle
// keyframe encoded from the equivalent tight (no-padding) I420
// content. A stride-walk bug that reads padding bytes will surface
// as a keyframe SHA-256 mismatch.
//
// The libvpx oracle side ingests I420 from a YUV file, which is
// always tight (visible content, no stride padding). The govpx
// side receives the SAME visible content but laid out with
// fuzz-generated YStride / UStride / VStride padding values. If
// the encoder correctly walks strides, the two outputs match.
func FuzzEncoderRandomStrides(f *testing.F) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		f.Skip("set GOVPX_WITH_ORACLE=1 to run random-strides fuzz")
	}
	// Each seed is (dimBucket, yPadBucket, uPadBucket, vPadBucket,
	// uAlignBucket, vAlignBucket).
	seeds := [][]byte{
		{0, 0, 0, 0, 0, 0}, // tight 16x16
		{0, 4, 0, 0, 0, 0}, // 16x16 with Y stride +4
		{1, 0, 2, 2, 0, 0}, // 32x16 with U/V stride +2
		{2, 8, 4, 4, 0, 0}, // 48x48 with mixed padding
		{3, 16, 8, 8, 0, 0},
		{4, 0, 0, 0, 1, 1}, // 64x64 odd alignment
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		vpxencOracle := findVpxencOracle(t)

		dim, padded, tight := newStridesFuzzImage(data)
		if dim.w == 0 {
			return
		}
		opts := EncoderOptions{
			Width:             dim.w,
			Height:            dim.h,
			FPS:               30,
			RateControlMode:   RateControlCBR,
			TargetBitrateKbps: 700,
			MinQuantizer:      4,
			MaxQuantizer:      56,
			KeyFrameInterval:  999,
			Deadline:          DeadlineRealtime,
			CpuUsed:           strictByteParityCPUUsed(DeadlineRealtime, -3),
			Tuning:            TunePSNR,
		}
		sum := sha256.Sum256(data)
		label := "fuzz-strides-" + hex.EncodeToString(sum[:4])
		t.Logf("%s w=%d h=%d yStride=%d uStride=%d vStride=%d",
			label, dim.w, dim.h, padded.YStride, padded.UStride, padded.VStride)

		// Govpx encodes the padded image; libvpx oracle encodes the
		// tight one. Both must produce the same VP8 packet bytes.
		govpxFrames := encodeFramesWithGovpx(t, opts, []Image{padded})
		libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, label, opts, opts.TargetBitrateKbps, []Image{tight}, libvpxEndUsageArgs(nil))
		assertSegmentByteParity(t, label, govpxFrames, libvpxFrames, 0)
	})
}

type stridesFuzzDim struct {
	w, h int
}

// newStridesFuzzImage constructs (padded, tight) image pairs where
// the visible content matches but `padded` may include arbitrary
// per-plane stride padding. Both images use deterministic
// fuzz-bytes-driven content via encoderValidationPanningFrame for
// determinism so the libvpx oracle's identical-content premise
// holds.
func newStridesFuzzImage(data []byte) (stridesFuzzDim, Image, Image) {
	r := vp9FuzzByteReader{data: data}
	if r.remaining() == 0 {
		return stridesFuzzDim{}, Image{}, Image{}
	}
	dimPool := [...]stridesFuzzDim{
		{16, 16},
		{32, 16},
		{48, 48},
		{64, 64},
		{96, 96},
	}
	dim := dimPool[int(r.next())%len(dimPool)]
	yPad := int(r.next() & 0x1f)   // 0..31
	uPad := int(r.next() & 0x0f)   // 0..15
	vPad := int(r.next() & 0x0f)   // 0..15
	uExtra := int(r.next() & 0x01) // odd alignment
	vExtra := int(r.next() & 0x01)

	uvW := (dim.w + 1) >> 1
	uvH := (dim.h + 1) >> 1
	yStride := dim.w + yPad
	uStride := uvW + uPad + uExtra
	vStride := uvW + vPad + vExtra

	// Base image is the deterministic panning frame at the target
	// dimension. The libvpx oracle side uses this verbatim (tight
	// strides); the govpx side reuses the visible content but lays
	// it out into a wider stride.
	tight := encoderValidationPanningFrame(dim.w, dim.h, 0)

	padded := Image{
		Width:   dim.w,
		Height:  dim.h,
		Y:       make([]byte, yStride*dim.h),
		U:       make([]byte, uStride*uvH),
		V:       make([]byte, vStride*uvH),
		YStride: yStride,
		UStride: uStride,
		VStride: vStride,
	}
	// Fill padding bytes with a distinctive pattern so a stride-walk
	// bug that reads them shows up as a divergence rather than
	// silently zeroing out.
	for i := range padded.Y {
		padded.Y[i] = 0xa5
	}
	for i := range padded.U {
		padded.U[i] = 0x5a
	}
	for i := range padded.V {
		padded.V[i] = 0xa5
	}
	for y := 0; y < dim.h; y++ {
		copy(padded.Y[y*yStride:y*yStride+dim.w], tight.Y[y*tight.YStride:y*tight.YStride+dim.w])
	}
	for y := 0; y < uvH; y++ {
		copy(padded.U[y*uStride:y*uStride+uvW], tight.U[y*tight.UStride:y*tight.UStride+uvW])
		copy(padded.V[y*vStride:y*vStride+uvW], tight.V[y*tight.VStride:y*tight.VStride+uvW])
	}
	return dim, padded, tight
}
