package govpx

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
)

// TestVP8DecoderThreadingPipelinedMatchesSerial verifies that the libvpx-style
// row-pipeline path produces byte-identical output to the serial decoder
// across the libvpx-authored smoke corpus (profiles 0-3, all token
// partition layouts, sharpness, and the error-resilient stream). Pixel
// parity is the hard constraint for R10-G; if any frame's Y/U/V planes
// differ between Threads=0 and Threads>=2 the test fails.
func TestVP8DecoderThreadingPipelinedMatchesSerial(t *testing.T) {
	cases := libvpxAuthoredDecodeCases()
	cases = append(cases, decodeFixtureCase{name: "govpx", ivfHex: govpxBaselineIVFHex, checksums: govpxBaselineChecksums[:]})

	for _, threads := range []int{2, 4, 8} {
		for _, tc := range cases {
			name := tc.name + "/threads=" + itoa(threads)
			t.Run(name, func(t *testing.T) {
				frames := mustDecodeIVFFrames(t, tc.ivfHex, len(tc.checksums))

				// Serial reference.
				serial, err := NewVP8Decoder(DecoderOptions{})
				if err != nil {
					t.Fatalf("serial NewVP8Decoder: %v", err)
				}
				serialFrames := decodeFramesForTest(t, "VP8", serial, frames,
					len(tc.checksums))

				// Threaded run.
				threaded, err := NewVP8Decoder(DecoderOptions{Threads: threads})
				if err != nil {
					t.Fatalf("threaded NewVP8Decoder(threads=%d): %v", threads, err)
				}
				threadedFrames := decodeFramesForTest(t, "VP8", threaded, frames,
					len(tc.checksums))

				if len(serialFrames) != len(threadedFrames) {
					t.Fatalf("frame count mismatch: serial=%d threaded=%d", len(serialFrames), len(threadedFrames))
				}
				for i := range serialFrames {
					s := serialFrames[i]
					th := threadedFrames[i]
					if s.width != th.width || s.height != th.height {
						t.Fatalf("frame %d dim mismatch serial=%dx%d threaded=%dx%d", i, s.width, s.height, th.width, th.height)
					}
					if !bytes.Equal(s.y, th.y) {
						t.Fatalf("frame %d Y plane diverges (threads=%d)", i, threads)
					}
					if !bytes.Equal(s.u, th.u) {
						t.Fatalf("frame %d U plane diverges (threads=%d)", i, threads)
					}
					if !bytes.Equal(s.v, th.v) {
						t.Fatalf("frame %d V plane diverges (threads=%d)", i, threads)
					}
				}

				// Sanity: threaded path must still match libvpx
				// checksums.
				for i, want := range tc.checksums {
					got := checksumFrame(i, want.KeyFrame, want.ShowFrame, Image{
						Width:  threadedFrames[i].width,
						Height: threadedFrames[i].height,
						Y:      threadedFrames[i].y, YStride: threadedFrames[i].width,
						U: threadedFrames[i].u, UStride: (threadedFrames[i].width + 1) >> 1,
						V: threadedFrames[i].v, VStride: (threadedFrames[i].width + 1) >> 1,
					})
					if !testutil.SameFrameChecksum(got, want) {
						t.Fatalf("threaded frame %d checksum mismatch (threads=%d)\nlibvpx:  %s\ngovpx: %s", i, threads, formatChecksum(want), formatChecksum(got))
					}
				}
			})
		}
	}
}

// TestVP8DecoderThreadingDecodeIntoMatchesSerial mirrors the parity test for
// the DecodeInto path, which copies into a caller-provided destination
// image. This exercises the threaded recon+LF pipeline followed by the
// public DecodeInto copy.
func TestVP8DecoderThreadingDecodeIntoMatchesSerial(t *testing.T) {
	cases := libvpxAuthoredDecodeCases()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			frames := mustDecodeIVFFrames(t, tc.ivfHex, len(tc.checksums))

			serial, err := NewVP8Decoder(DecoderOptions{})
			if err != nil {
				t.Fatalf("serial NewVP8Decoder: %v", err)
			}
			threaded, err := NewVP8Decoder(DecoderOptions{Threads: 4})
			if err != nil {
				t.Fatalf("threaded NewVP8Decoder: %v", err)
			}

			width, height := tc.checksums[0].Width, tc.checksums[0].Height
			dstSerial := testImage(width, height)
			dstThreaded := testImage(width, height)

			for i := range frames {
				if _, err := serial.DecodeInto(frames[i], &dstSerial); err != nil {
					t.Fatalf("serial DecodeInto[%d]: %v", i, err)
				}
				if _, err := threaded.DecodeInto(frames[i], &dstThreaded); err != nil {
					t.Fatalf("threaded DecodeInto[%d]: %v", i, err)
				}
				if !bytes.Equal(dstSerial.Y, dstThreaded.Y) {
					t.Fatalf("DecodeInto frame %d Y plane diverges", i)
				}
				if !bytes.Equal(dstSerial.U, dstThreaded.U) {
					t.Fatalf("DecodeInto frame %d U plane diverges", i)
				}
				if !bytes.Equal(dstSerial.V, dstThreaded.V) {
					t.Fatalf("DecodeInto frame %d V plane diverges", i)
				}
			}
		})
	}
}

// TestVP8DecoderThreadingExternalCorpusMatchesSerial walks the external libvpx
// conformance corpus (58 valid VP80 vectors + the four I420
// vp80-03-segmentation-* fixtures) and asserts the threaded decoder
// produces byte-identical Y/U/V planes to the serial decoder for every
// frame. It opt-in runs when GOVPX_TEST_DATA_PATH points at the corpus
// directory; the libvpx oracle is not required (we compare two govpx
// runs against each other, not against libvpx checksums).
func TestVP8DecoderThreadingExternalCorpusMatchesSerial(t *testing.T) {
	root := os.Getenv("GOVPX_TEST_DATA_PATH")
	if root == "" {
		t.Skip("set GOVPX_TEST_DATA_PATH to a VP8 IVF conformance corpus")
	}
	paths := findVP8IVFTestData(t, root)
	if len(paths) == 0 {
		t.Fatalf("no VP8 IVF files found under %s", root)
	}
	for _, path := range paths {
		t.Run(testutil.SafeCorpusTestName(root, path), func(t *testing.T) {
			ivf, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile: %v", err)
			}
			frames, err := testutil.IVFFramePayloadViews(ivf)
			if err != nil {
				t.Fatalf("collect frames: %v", err)
			}
			if len(frames) == 0 {
				t.Skipf("no frames in %s", filepath.Base(path))
			}

			serial, err := NewVP8Decoder(DecoderOptions{})
			if err != nil {
				t.Fatalf("serial NewVP8Decoder: %v", err)
			}
			threaded, err := NewVP8Decoder(DecoderOptions{Threads: 4})
			if err != nil {
				t.Fatalf("threaded NewVP8Decoder: %v", err)
			}
			serialFrames := decodeFramesForTest(t, "VP8", serial, frames,
				len(frames))
			threadedFrames := decodeFramesForTest(t, "VP8", threaded, frames,
				len(frames))
			if len(serialFrames) != len(threadedFrames) {
				t.Fatalf("frame count mismatch: serial=%d threaded=%d", len(serialFrames), len(threadedFrames))
			}
			for i := range serialFrames {
				if !bytes.Equal(serialFrames[i].y, threadedFrames[i].y) ||
					!bytes.Equal(serialFrames[i].u, threadedFrames[i].u) ||
					!bytes.Equal(serialFrames[i].v, threadedFrames[i].v) {
					t.Fatalf("frame %d planes diverge for %s", i, filepath.Base(path))
				}
			}
		})
	}
}

// BenchmarkVP8DecoderThreading measures the per-frame decode latency for the
// largest VP8 conformance vector available locally and contrasts the
// serial path with Threads=2/4. Skipped when the corpus is not present.
func BenchmarkVP8DecoderThreading(b *testing.B) {
	root := os.Getenv("GOVPX_TEST_DATA_PATH")
	if root == "" {
		root = "internal/coracle/build/test-data/vp8"
	}
	if _, err := os.Stat(root); err != nil {
		b.Skip("VP8 conformance corpus not available")
	}
	candidates := []string{
		"vp80-01-intra-1411.ivf",
		"vp80-00-comprehensive-014.ivf",
		"vp80-00-comprehensive-015.ivf",
		"vp80-03-segmentation-04.ivf",   // 1280x720
		"vp80-00-comprehensive-008.ivf", // 1432x888
		"vp80-05-sharpness-1443.ivf",    // 1920x96
	}
	for _, name := range candidates {
		path := filepath.Join(root, name)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		ivf, err := os.ReadFile(path)
		if err != nil {
			b.Logf("ReadFile %s: %v", name, err)
			continue
		}
		frames, err := testutil.IVFFramePayloadViews(ivf)
		if err != nil {
			b.Logf("collect frames %s: %v", name, err)
			continue
		}
		for _, threads := range []int{0, 1, 2, 4, 8} {
			b.Run(name+"/threads="+itoa(threads), func(b *testing.B) {
				d, err := NewVP8Decoder(DecoderOptions{Threads: threads})
				if err != nil {
					b.Fatalf("NewVP8Decoder: %v", err)
				}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					d.Reset()
					for j := range frames {
						if err := d.Decode(frames[j]); err != nil {
							b.Fatalf("Decode[%d]: %v", j, err)
						}
						_, _ = d.NextFrame()
					}
				}
			})
		}
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	negative := v < 0
	if negative {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if negative {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
