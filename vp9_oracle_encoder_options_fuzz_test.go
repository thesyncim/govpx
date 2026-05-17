//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"crypto/sha256"
	"image"
	"os"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
	"github.com/thesyncim/govpx/internal/testutil"
)

// FuzzVP9OracleEncoderOptions complements FuzzVP9EncoderOptions (which only
// asserts no-panic + sentinel-error contracts on NewVP9Encoder) by adding the
// libvpx keyframe-byte-parity comparator that the VP8 sibling
// FuzzVP8EncoderOptions already enforces. For each fuzz iteration:
//
//   - Govpx rejects → documented sentinel error or contract bug (logged via
//     assertVP9FuzzEncoderConstructError).
//   - Govpx accepts, vpxenc-vp9 CLI rejects this shape → comparator
//     inapplicable, logged-only (mirrors FuzzVP8EncoderOptions). The fuzz
//     iteration keeps going.
//   - Both accept → keyframe bytes must SHA-256 match. Mismatch is t.Errorf
//     so divergences land as seed regressions under testdata/fuzz/.
//
// Gated by GOVPX_WITH_ORACLE=1 plus a built vpxenc-vp9 binary. Without the
// binary the fuzzer t.Skips cleanly so plain `go test` runs are green.
func FuzzVP9OracleEncoderOptions(f *testing.F) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		f.Skip("set GOVPX_WITH_ORACLE=1 to run VP9 option-validation oracle fuzz")
	}
	// Seeds mirror FuzzVP9EncoderOptions shape but biased toward configs
	// the libvpx CLI accepts so the comparator path actually fires.
	seeds := [][]byte{
		nil,
		{},
		{0x00},
		// Plausible 64×64 CBR config.
		{0x00, 0x40, 0x40, 0x1e, 0x00, 0x00, 0x05, 0xdc, 0x04, 0x38, 0x20, 0x00, 0x00, 0x00, 0x00, 0x00},
		// 32×32 lossless.
		{0x04, 0x20, 0x20, 0x1e, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		// Out-of-range/all-0xff to push validator.
		{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
		// All-zeros default-construction.
		{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("NewVP9Encoder panicked on %d-byte input: %v", len(data), r)
			}
		}()
		opts := vp9EncoderOptionsFromFuzz(data)
		e, err := NewVP9Encoder(opts)
		if err != nil {
			assertVP9FuzzEncoderConstructError(t, err)
			return
		}
		if e == nil {
			t.Fatal("NewVP9Encoder returned nil encoder without error")
		}
		src := newVP9YCbCrForTest(opts.Width, opts.Height, 128, 128, 128)
		size, err := vp9AllocatingEncodeBufferSize(opts.Width, opts.Height)
		if err != nil {
			return
		}
		dst := make([]byte, size)
		result, err := e.EncodeIntoWithResult(src, dst)
		if err != nil {
			assertVP9FuzzEncoderRuntimeError(t, err)
			return
		}
		if len(result.Data) == 0 {
			return
		}
		libvpxKey := tryVP9LibvpxKeyFrameBytes(t, opts, src)
		if len(libvpxKey) == 0 {
			t.Logf("vpxenc-vp9 rejected fuzzed config (comparator inapplicable, logged-only)")
			return
		}
		gHash := sha256.Sum256(result.Data)
		lHash := sha256.Sum256(libvpxKey)
		if gHash != lHash {
			t.Errorf("keyframe byte mismatch under fuzzed options: govpx_len=%d vpxenc_len=%d first_diff=%d",
				len(result.Data), len(libvpxKey),
				firstVP9PacketDiffForTest(result.Data, libvpxKey))
		}
		_ = bytes.Equal // keep import in case future tightening drops first_diff log.
	})
}

// tryVP9LibvpxKeyFrameBytes runs vpxenc-vp9 for one keyframe at the fuzzed
// options and returns the keyframe IVF payload, or nil if the CLI rejects the
// shape / the binary is unbuilt. Matches the VP8 sibling tryLibvpxKeyFrameBytes
// but maps to the vpxenc-vp9 default arg set.
func tryVP9LibvpxKeyFrameBytes(t *testing.T, opts VP9EncoderOptions, src *image.YCbCr) []byte {
	t.Helper()
	if _, err := coracle.VpxencVP9Path(); err != nil {
		return nil
	}
	raw := appendVP9YCbCrI420(nil, src)
	// vpxenc-vp9 defaults pin --rt --cpu-used=8 --end-usage=q. Override
	// extraArgs that vary by config so the comparator stays informative
	// rather than always rejecting on noisy parameters.
	var extra []string
	switch opts.RateControlMode {
	case RateControlCBR:
		extra = append(extra, "--end-usage=cbr",
			"--target-bitrate=700")
	case RateControlVBR:
		extra = append(extra, "--end-usage=vbr",
			"--target-bitrate=700")
	case RateControlCQ:
		extra = append(extra, "--end-usage=cq")
	}
	ivf, _, err := coracle.VpxencVP9EncodeI420(raw, opts.Width, opts.Height, 1, extra...)
	if err != nil || len(ivf) == 0 {
		return nil
	}
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		return nil
	}
	first, _, err := testutil.NextIVFFrame(ivf, offset, 0)
	if err != nil {
		return nil
	}
	return append([]byte(nil), first.Data...)
}
