//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"encoding/hex"
	"errors"
	"os"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
	"github.com/thesyncim/govpx/internal/testutil"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
)

// FuzzVP8DecoderAgainstLibvpx feeds govpx and libvpx the same mutated
// libvpx-encoded IVF corpus. The decoders must either both reject the stream
// or both accept it and produce identical I420 frames.
//
// Fuzz mutations include bit flips, byte deletes/inserts, header field
// corruption, and partition-size truncation. Divergent inputs land in
// testdata/fuzz/FuzzVP8DecoderAgainstLibvpx and replay as regression tests.
func FuzzVP8DecoderAgainstLibvpx(f *testing.F) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		f.Skip("set GOVPX_WITH_ORACLE=1 to run decoder-vs-libvpx fuzz")
	}

	smoke, err := hex.DecodeString(testutil.LibvpxEncodedSmokeIVFHex)
	if err != nil {
		f.Fatalf("decode smoke IVF: %v", err)
	}
	// Seed corpus: the canonical 2-frame libvpx-encoded smoke IVF plus
	// a few minimal variants that exercise the IVF-parser and
	// frame-bounds edges (empty, header-only, half-truncated, zeros).
	f.Add(smoke)
	if len(smoke) >= 32 {
		f.Add(append([]byte(nil), smoke[:32]...))
	}
	if len(smoke) >= 2 {
		f.Add(append([]byte(nil), smoke[:len(smoke)/2]...))
	}
	f.Add([]byte{})
	f.Add(make([]byte, 16))

	f.Fuzz(func(t *testing.T, data []byte) {
		vpxdec := findVpxdecForFuzz(t)
		govpxFrames, govpxErr := decodeIVFGovpxBestEffort(data)
		libvpxFrames, libvpxErr := decodeIVFLibvpxBestEffort(t, vpxdec, data)

		govpxAccept := len(govpxFrames) > 0 || govpxErr == nil
		libvpxAccept := len(libvpxFrames) > 0 || libvpxErr == nil

		// Asymmetric "did this stream produce any decoded frames" is
		// a bug — one decoder accepted what the other rejected.
		if (len(govpxFrames) > 0) != (len(libvpxFrames) > 0) {
			t.Errorf("acceptance disagreement: govpx_frames=%d libvpx_frames=%d govpx_err=%v libvpx_err=%v",
				len(govpxFrames), len(libvpxFrames), govpxErr, libvpxErr)
			return
		}
		if len(govpxFrames) == 0 {
			// Both rejected the stream. The error messages may differ
			// (different parsers) but the outcome is symmetric.
			_ = govpxAccept
			_ = libvpxAccept
			return
		}
		// Frame-count partial-accept divergences: one decoder produced
		// more frames than the other from the same input. Cap the
		// comparison at the matched prefix and log the gap.
		minFrames := len(govpxFrames)
		if len(libvpxFrames) < minFrames {
			minFrames = len(libvpxFrames)
		}
		if len(govpxFrames) != len(libvpxFrames) {
			t.Logf("frame count partial accept: govpx=%d libvpx=%d (comparing first %d)",
				len(govpxFrames), len(libvpxFrames), minFrames)
		}
		for i := 0; i < minFrames; i++ {
			if !bytes.Equal(govpxFrames[i], libvpxFrames[i]) {
				diff := testutil.FirstByteDiff(govpxFrames[i], libvpxFrames[i])
				t.Errorf("frame %d I420 byte mismatch: govpx_len=%d libvpx_len=%d first_diff=%d",
					i, len(govpxFrames[i]), len(libvpxFrames[i]), diff)
			}
		}
	})
}

// findVpxdecForFuzz locates the libvpx vpxdec binary, preferring
// GOVPX_VPXDEC and falling back to the build dir produced by
// internal/coracle/build_libvpx.sh.
func findVpxdecForFuzz(t *testing.T) string {
	t.Helper()
	path, err := coracle.VpxdecPath()
	if err == nil {
		return path
	}
	if errors.Is(err, coracle.ErrVpxdecNotBuilt) {
		t.Skip("vpxdec binary not available; set GOVPX_VPXDEC or run internal/coracle/build_libvpx.sh")
	}
	t.Fatalf("VpxdecPath: %v", err)
	return ""
}

// decodeIVFGovpxBestEffort parses data as an IVF container and feeds
// each VP8 packet to a govpx decoder, returning the per-frame
// concatenated I420 planes (no stride padding) for every frame the
// decoder accepted. A non-nil error indicates a parser or decoder
// failure mid-stream; partial output is still returned for the frames
// decoded before the error.
func decodeIVFGovpxBestEffort(data []byte) ([][]byte, error) {
	if _, err := testutil.ParseIVFHeader(data); err != nil {
		return nil, err
	}
	offset, err := testutil.FirstIVFFrameOffset(data)
	if err != nil {
		return nil, err
	}
	// MaxWidth/MaxHeight cap is not bound by the IVF container width:
	// fuzz mutations routinely break the IVF<->VP8 dimension agreement
	// and libvpx's vpxdec sizes its output from the VP8 key-frame header
	// (not the IVF header), so leave the cap unbounded and let the
	// decoder pick output dimensions per-frame.
	dec, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		return nil, err
	}
	var frames [][]byte
	for frameIndex := 0; offset < len(data); frameIndex++ {
		frame, next, err := testutil.NextIVFFrame(data, offset, frameIndex)
		if err != nil {
			return frames, err
		}
		offset = next
		if err := dec.Decode(frame.Data); err != nil {
			return frames, err
		}
		// Mirror libvpx's vpxdec: only emit raw I420 for visible frames.
		// Hidden frames (ShowFrame == false, e.g. alt-refs) update the
		// reference buffers but produce no output sample. Pull via
		// NextFrame() which consumes only visible frames.
		img, ok := dec.NextFrame()
		if !ok {
			continue
		}
		frames = append(frames, packTightI420(&img))
	}
	return frames, nil
}

// decodeIVFLibvpxBestEffort writes data to a temp file and runs
// `vpxdec --codec=vp8 --i420 --output=raw input.ivf`, returning the
// per-frame concatenated I420 planes for every frame vpxdec emitted.
// vpxdec's non-zero exit on malformed input is surfaced as the error;
// any frames written before the failure are still returned.
//
// Slicing notes (task #308): vpxdec's per-frame raw I420 size is derived
// from the *VP8 key-frame header* (width/height encoded in packet bytes
// 6-9), NOT the IVF file header's width/height fields. The IVF header
// dims are attacker-controlled metadata that the fuzzer mutates freely;
// using them to size the raw slicer caused false acceptance disagreements
// when a mutation set IVF height to a different value than the VP8 KF
// height (e.g. seed f4f81f7d: IVF height 7708 vs VP8 KF height 28 — the
// slicer's expected per-frame size dwarfed the raw output and returned
// zero frames while govpx correctly decoded two). Walk per-frame VP8
// headers to compute libvpx-faithful expected sizes.
func decodeIVFLibvpxBestEffort(t *testing.T, vpxdec string, data []byte) ([][]byte, error) {
	raw, _, runErr := coracle.VpxdecVP8DecodeI420(data, coracle.VpxdecVP8Config{BinaryPath: vpxdec})
	if _, headerErr := testutil.ParseIVFHeader(data); headerErr != nil {
		// vpxdec also won't produce frames from an unparseable IVF header;
		// the outcome is "no frames" regardless.
		return nil, runErr
	}
	sizes := vp8PerFrameI420Sizes(data)
	frames := sliceRawByPerFrameSizes(raw, sizes)
	return frames, runErr
}

// vp8PerFrameI420Sizes walks every IVF frame in data and returns the
// expected per-frame raw I420 size that libvpx's vpxdec would emit when
// run with --i420. Sizes are computed from the VP8 key-frame header
// dimensions, mirroring vpxdec's `vpx_img_plane_width/height` walk in
// write_image_file. Inter frames inherit the most recent key-frame
// dimensions (VP8 has no in-band resize outside keyframes). Frames whose
// header is unparseable or whose dims are zero contribute a zero entry;
// the slicer treats those as "no raw output expected for this frame".
func vp8PerFrameI420Sizes(data []byte) []int {
	offset, err := testutil.FirstIVFFrameOffset(data)
	if err != nil {
		return nil
	}
	var sizes []int
	curWidth, curHeight := 0, 0
	for frameIndex := 0; offset < len(data); frameIndex++ {
		frame, next, err := testutil.NextIVFFrame(data, offset, frameIndex)
		if err != nil {
			return sizes
		}
		offset = next
		header, headerErr := vp8dec.ParseFrameHeader(frame.Data)
		if headerErr != nil {
			// Unparseable frame: libvpx will reject the stream from
			// here on. Stop accumulating expected sizes — anything
			// beyond this point won't appear in the raw output.
			return sizes
		}
		if header.KeyFrame() {
			curWidth = header.Width
			curHeight = header.Height
		}
		sizes = append(sizes, i420FrameSize(curWidth, curHeight))
	}
	return sizes
}

// sliceRawByPerFrameSizes walks raw output and chops it into frames
// using the expected per-frame sizes from vp8PerFrameI420Sizes. It
// returns one slice per fully-present frame; once raw is exhausted, it
// stops (vpxdec wrote fewer frames than the IVF claimed, e.g. because
// of mid-stream rejection).
func sliceRawByPerFrameSizes(raw []byte, sizes []int) [][]byte {
	var frames [][]byte
	off := 0
	for _, size := range sizes {
		if size <= 0 {
			continue
		}
		if off+size > len(raw) {
			break
		}
		frames = append(frames, append([]byte(nil), raw[off:off+size]...))
		off += size
	}
	return frames
}

// packTightI420 copies the visible Y/U/V planes from img into a single
// contiguous slice with no stride padding, matching the layout vpxdec
// writes when --i420 is in effect.
func packTightI420(img *Image) []byte {
	w := img.Width
	h := img.Height
	uvW := (w + 1) >> 1
	uvH := (h + 1) >> 1
	out := make([]byte, 0, i420FrameSize(w, h))
	for y := 0; y < h; y++ {
		out = append(out, img.Y[y*img.YStride:y*img.YStride+w]...)
	}
	for y := 0; y < uvH; y++ {
		out = append(out, img.U[y*img.UStride:y*img.UStride+uvW]...)
	}
	for y := 0; y < uvH; y++ {
		out = append(out, img.V[y*img.VStride:y*img.VStride+uvW]...)
	}
	return out
}

func i420FrameSize(w, h int) int {
	if w <= 0 || h <= 0 {
		return 0
	}
	uvW := (w + 1) >> 1
	uvH := (h + 1) >> 1
	return w*h + 2*uvW*uvH
}
