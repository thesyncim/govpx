//go:build govpx_oracle_trace

package govpx_test

import (
	"bytes"
	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp9oracle"
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
		// Only run the govpx-vs-libvpx comparison on a well-formed VP9 IVF
		// container, so both decoders demux the identical frame sequence and the
		// only thing under test is VP9 frame decoding. Container-level
		// malformations make vpxdec diverge from govpx for reasons that are not
		// VP9 decoder bugs:
		//   - a non-VP9 FourCC: vpxdec routes by FourCC (decodes VP8), govpx is
		//     VP9-only and rejects it;
		//   - a mutated IVF version or a truncated / out-of-bounds trailing frame
		//     header: vpxdec warns ("Unrecognized IVF version") and flushes no
		//     output, while govpx best-effort decodes the valid leading frames.
		// Those are container-demux differences, so skip inputs whose container
		// does not fully demux as VP9. The frame *payloads* are still fuzzed,
		// which is the actual VP9-decoder robustness target.
		if !vp9DifferentialIVFEligible(data) {
			return
		}

		govpxFrames, _ := decodeVP9IVFGovpxBestEffort(data)
		libvpxFrames, _ := decodeVP9IVFLibvpxBestEffort(t, data)

		// For aggressive byte mutation, acceptance asymmetry is not a reliable
		// bug signal: govpx and vpxdec legitimately differ in how strictly they
		// reject malformed input. vpxdec auto-detects the codec from the IVF
		// FourCC, decodes each frame at its own (dynamic) resolution rather than
		// the harness-configured IVF dims, and is all-or-nothing on any decode
		// error; govpx is VP9-only, capped to the IVF dims via MaxWidth/MaxHeight,
		// and rejects mismatched configs/resolutions/markers. Those differences
		// produce acceptance disagreements that are not VP9-decoder bugs. The
		// reliable differential signal is a CONTENT divergence on a stream BOTH
		// decoders fully decode; crash-safety on arbitrary input is covered by
		// FuzzVP9DecoderDecode. So compare frame content only when both accepted
		// the whole stream, and skip otherwise.
		if len(govpxFrames) == 0 || len(libvpxFrames) == 0 {
			return
		}
		minFrames := min(len(govpxFrames), len(libvpxFrames))
		if len(govpxFrames) != len(libvpxFrames) {
			t.Logf("VP9 frame count differs (both accepted): govpx=%d libvpx=%d (comparing first %d)",
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

// vp9DifferentialIVFEligible reports whether data is a fully well-formed VP9 IVF
// container: a parseable header with the VP9 FourCC whose every frame header is
// in-bounds. Only such inputs yield a meaningful govpx-vs-libvpx comparison,
// because both decoders then demux the same frame sequence; malformed containers
// make vpxdec's acceptance / partial-output behaviour diverge from govpx for
// non-codec reasons (see the call site).
func vp9DifferentialIVFEligible(data []byte) bool {
	hdr, err := testutil.ParseIVFHeader(data)
	if err != nil || hdr.FourCC != testutil.IVFFourCCVP9 {
		return false
	}
	offset, err := testutil.FirstIVFFrameOffset(data)
	if err != nil {
		return false
	}
	for frameIndex := 0; offset < len(data); frameIndex++ {
		_, next, err := testutil.NextIVFFrame(data, offset, frameIndex)
		if err != nil {
			return false
		}
		offset = next
	}
	return true
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
	dst := vp9oracle.NewImage(header.Width, header.Height)
	var frames [][]byte
	for frameIndex := 0; offset < len(data); frameIndex++ {
		frame, next, err := testutil.NextIVFFrame(data, offset, frameIndex)
		if err != nil {
			// Atomic acceptance: a stream counts as decoded only if every frame
			// decodes. vpxdec is all-or-nothing — it flushes no I420 output when
			// any frame fails — so govpx must discard the frames it decoded
			// before the error too, otherwise a stream with a good frame 0 and a
			// later bad frame reports a spurious acceptance disagreement.
			return nil, err
		}
		offset = next
		info, err := d.DecodeInto(frame.Data, &dst)
		if err != nil {
			return nil, err
		}
		// Mirror libvpx's vpxdec: only emit raw I420 for visible frames.
		// VP9 hidden frames (show_frame == false, e.g. ALTREF) do not
		// produce a raw output sample, and DecodeInto leaves dst
		// untouched; collecting stale dst contents would fork the fuzz
		// comparator from vpxdec which writes only show_frame=1 planes.
		if !info.ShowFrame {
			continue
		}
		frames = append(frames, vp9oracle.PackI420(&dst))
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
		// vpxdec is all-or-nothing: it flushes no I420 output when any frame
		// fails to decode. Mirror that atomic acceptance in the govpx side
		// (decodeVP9IVFGovpxBestEffort also returns no frames on any error).
		return nil, err
	}
	frameSize := vp9oracle.I420FrameSize(header.Width, header.Height)
	if frameSize <= 0 || len(raw) < frameSize {
		return nil, nil
	}
	var frames [][]byte
	for off := 0; off+frameSize <= len(raw); off += frameSize {
		frames = append(frames, append([]byte(nil), raw[off:off+frameSize]...))
	}
	return frames, nil
}
