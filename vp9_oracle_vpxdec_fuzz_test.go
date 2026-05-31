//go:build govpx_oracle_trace

package govpx_test

import (
	"bytes"
	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"image"
	"testing"
)

// FuzzVP9DecoderAgainstLibvpx mirrors FuzzVP8DecoderAgainstLibvpx (F4) for VP9:
// the govpx VP9 decoder is fed bytes mutated from a real libvpx VP9-encoded IVF
// corpus, and the same bytes are fed to libvpx's vpxdec-vp9. The two decoders
// must agree — either both accept and produce identical I420 per frame, or both
// reject. Asymmetric outcomes are bugs.
//
// Gated by GOVPX_WITH_ORACLE=1 and the vpxdec-vp9 + vpxenc-vp9 binaries. The
// seed corpus is built lazily from vpxenc-vp9 so it always reflects whatever
// the current oracle emits for a small synthetic clip.
func FuzzVP9DecoderAgainstLibvpx(f *testing.F) {
	vp9test.RequireOracle(f, "VP9 decoder-vs-libvpx fuzz")
	vp9test.RequireVpxdec(f)
	vp9test.RequireVpxenc(f)

	// Build a 4-frame VP9 IVF seed using vpxenc-vp9 so the corpus
	// always exercises whatever the current oracle emits.
	seed := vp9FuzzSeedIVF(f, 64, 64, 4)
	f.Add(seed)
	if len(seed) >= 32 {
		f.Add(append([]byte(nil), seed[:32]...))
	}
	if len(seed) >= 2 {
		f.Add(append([]byte(nil), seed[:len(seed)/2]...))
	}
	f.Add([]byte{})
	f.Add(make([]byte, 16))

	f.Fuzz(func(t *testing.T, data []byte) {
		govpxFrames, govpxErr := decodeVP9IVFGovpxBestEffort(data)
		libvpxFrames, libvpxErr := decodeVP9IVFLibvpxBestEffort(t, data)

		if (len(govpxFrames) > 0) != (len(libvpxFrames) > 0) {
			t.Errorf("VP9 acceptance disagreement: govpx_frames=%d libvpx_frames=%d govpx_err=%v libvpx_err=%v",
				len(govpxFrames), len(libvpxFrames), govpxErr, libvpxErr)
			return
		}
		if len(govpxFrames) == 0 {
			return
		}
		minFrames := min(len(govpxFrames), len(libvpxFrames))
		if len(govpxFrames) != len(libvpxFrames) {
			t.Logf("VP9 frame count partial accept: govpx=%d libvpx=%d (comparing first %d)",
				len(govpxFrames), len(libvpxFrames), minFrames)
		}
		for i := 0; i < minFrames; i++ {
			if !bytes.Equal(govpxFrames[i], libvpxFrames[i]) {
				diff := testutil.FirstByteDiff(govpxFrames[i], libvpxFrames[i])
				t.Errorf("VP9 frame %d I420 byte mismatch: govpx_len=%d libvpx_len=%d first_diff=%d",
					i, len(govpxFrames[i]), len(libvpxFrames[i]), diff)
			}
		}
	})
}

// vp9FuzzSeedIVF returns a VP9 IVF stream produced by vpxenc-vp9 for use as a
// fuzz seed. The clip uses a constant grey image so the corpus is
// deterministic across machines.
func vp9FuzzSeedIVF(f *testing.F, width, height, frames int) []byte {
	f.Helper()
	srcs := make([]*image.YCbCr, frames)
	for i := range srcs {
		srcs[i] = vp9test.NewYCbCr(width, height, 128, 128, 128)
	}
	return vp9test.VpxencIVF(f, srcs)
}

// decodeVP9IVFGovpxBestEffort parses data as an IVF container and feeds each
// VP9 packet to a govpx VP9 decoder, returning the per-frame concatenated I420
// planes for every frame the decoder accepted.
func decodeVP9IVFGovpxBestEffort(data []byte) ([][]byte, error) {
	header, err := testutil.ParseIVFHeader(data)
	if err != nil {
		return nil, err
	}
	offset, err := testutil.FirstIVFFrameOffset(data)
	if err != nil {
		return nil, err
	}
	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{
		MaxWidth:  header.Width,
		MaxHeight: header.Height,
	})
	if err != nil {
		return nil, err
	}
	dst := newVP9OracleImage(header.Width, header.Height)
	var frames [][]byte
	for frameIndex := 0; offset < len(data); frameIndex++ {
		frame, next, err := testutil.NextIVFFrame(data, offset, frameIndex)
		if err != nil {
			return frames, err
		}
		offset = next
		info, err := d.DecodeInto(frame.Data, &dst)
		if err != nil {
			return frames, err
		}
		// Mirror libvpx's vpxdec: only emit raw I420 for visible frames.
		// VP9 hidden frames (show_frame == false, e.g. ALTREF) do not
		// produce a raw output sample, and DecodeInto leaves dst
		// untouched; collecting stale dst contents would fork the fuzz
		// comparator from vpxdec which writes only show_frame=1 planes.
		if !info.ShowFrame {
			continue
		}
		frames = append(frames, packVP9OracleI420(&dst))
	}
	return frames, nil
}

// decodeVP9IVFLibvpxBestEffort writes data to a temp file and runs vpxdec-vp9
// via the coracle wrapper, returning per-frame concatenated I420 planes for
// every frame vpxdec emitted.
func decodeVP9IVFLibvpxBestEffort(t *testing.T, data []byte) ([][]byte, error) {
	t.Helper()
	header, headerErr := testutil.ParseIVFHeader(data)
	if headerErr != nil {
		// vpxdec also won't produce frames from an unparseable IVF header;
		// the outcome is "no frames" regardless.
		_, _ = vp9test.VpxdecI420Result(data) // probe to surface vpxdec error if any.
		return nil, headerErr
	}
	raw, err := vp9test.VpxdecI420Result(data)
	if err != nil {
		// vpxdec may have written some frames before erroring.
		return nil, err
	}
	frameSize := vp9OracleI420FrameSize(header.Width, header.Height)
	if frameSize <= 0 || len(raw) < frameSize {
		return nil, nil
	}
	var frames [][]byte
	for off := 0; off+frameSize <= len(raw); off += frameSize {
		frames = append(frames, append([]byte(nil), raw[off:off+frameSize]...))
	}
	return frames, nil
}
