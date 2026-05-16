package govpx

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
)

// TestDecoderThreadingPipelinedMatchesSerial verifies that the libvpx-style
// row-pipeline path produces byte-identical output to the serial decoder
// across the libvpx-authored smoke corpus (profiles 0-3, all token
// partition layouts, sharpness, and the error-resilient stream). Pixel
// parity is the hard constraint for R10-G; if any frame's Y/U/V planes
// differ between Threads=0 and Threads>=2 the test fails.
func TestDecoderThreadingPipelinedMatchesSerial(t *testing.T) {
	cases := libvpxAuthoredSmokeCases()
	cases = append(cases, smokeCase{name: "govpx", ivfHex: govpxSmokeIVFHex, checksums: govpxSmokeChecksums[:]})

	for _, threads := range []int{2, 4, 8} {
		for _, tc := range cases {
			name := tc.name + "/threads=" + itoa(threads)
			t.Run(name, func(t *testing.T) {
				frames := mustDecodeSmokeIVFFrames(t, tc.ivfHex, len(tc.checksums))

				// Serial reference.
				serial, err := NewVP8Decoder(DecoderOptions{})
				if err != nil {
					t.Fatalf("serial NewVP8Decoder: %v", err)
				}
				serialFrames := decodePlanes(t, serial, frames, len(tc.checksums))

				// Threaded run.
				threaded, err := NewVP8Decoder(DecoderOptions{Threads: threads})
				if err != nil {
					t.Fatalf("threaded NewVP8Decoder(threads=%d): %v", threads, err)
				}
				threadedFrames := decodePlanes(t, threaded, frames, len(tc.checksums))

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

// TestDecoderThreadingDecodeIntoMatchesSerial mirrors the parity test for
// the DecodeInto path, which copies into a caller-provided destination
// image. This exercises the threaded recon+LF pipeline followed by the
// public DecodeInto copy.
func TestDecoderThreadingDecodeIntoMatchesSerial(t *testing.T) {
	cases := libvpxAuthoredSmokeCases()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			frames := mustDecodeSmokeIVFFrames(t, tc.ivfHex, len(tc.checksums))

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

type capturedFramePlanes struct {
	width  int
	height int
	y      []byte
	u      []byte
	v      []byte
}

// decodePlanes drives Decode/NextFrame across all input frames and returns
// dense (no-stride) copies of the Y/U/V planes for each emitted frame. We
// take dense copies because the decoder reuses its internal frame buffers
// across calls; comparing two sequences therefore needs per-frame snapshots.
func decodePlanes(t testing.TB, d *VP8Decoder, frames [][]byte, want int) []capturedFramePlanes {
	t.Helper()
	out := make([]capturedFramePlanes, 0, want)
	for i, frame := range frames {
		if err := d.Decode(frame); err != nil {
			t.Fatalf("Decode[%d]: %v", i, err)
		}
		img, ok := d.NextFrame()
		if !ok {
			continue
		}
		out = append(out, captureDecodedPlanes(img))
	}
	return out
}

func captureDecodedPlanes(img Image) capturedFramePlanes {
	yWidth := img.Width
	yHeight := img.Height
	uvWidth := (img.Width + 1) >> 1
	uvHeight := (img.Height + 1) >> 1

	y := make([]byte, yWidth*yHeight)
	for row := range yHeight {
		copy(y[row*yWidth:(row+1)*yWidth], img.Y[row*img.YStride:row*img.YStride+yWidth])
	}
	u := make([]byte, uvWidth*uvHeight)
	for row := range uvHeight {
		copy(u[row*uvWidth:(row+1)*uvWidth], img.U[row*img.UStride:row*img.UStride+uvWidth])
	}
	v := make([]byte, uvWidth*uvHeight)
	for row := range uvHeight {
		copy(v[row*uvWidth:(row+1)*uvWidth], img.V[row*img.VStride:row*img.VStride+uvWidth])
	}
	return capturedFramePlanes{width: yWidth, height: yHeight, y: y, u: u, v: v}
}

// TestDecoderThreadingExternalCorpusMatchesSerial walks the external libvpx
// conformance corpus (58 valid VP80 vectors + the four I420
// vp80-03-segmentation-* fixtures) and asserts the threaded decoder
// produces byte-identical Y/U/V planes to the serial decoder for every
// frame. It opt-in runs when GOVPX_TEST_DATA_PATH points at the corpus
// directory; the libvpx oracle is not required (we compare two govpx
// runs against each other, not against libvpx checksums).
func TestDecoderThreadingExternalCorpusMatchesSerial(t *testing.T) {
	root := os.Getenv("GOVPX_TEST_DATA_PATH")
	if root == "" {
		t.Skip("set GOVPX_TEST_DATA_PATH to a VP8 IVF conformance corpus")
	}
	paths := findVP8IVFTestData(t, root)
	if len(paths) == 0 {
		t.Fatalf("no VP8 IVF files found under %s", root)
	}
	for _, path := range paths {
		t.Run(safeIVFTestName(root, path), func(t *testing.T) {
			ivf, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile: %v", err)
			}
			frames, err := ivfFramesForThreadingParity(ivf)
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
			serialFrames := decodePlanes(t, serial, frames, len(frames))
			threadedFrames := decodePlanes(t, threaded, frames, len(frames))
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

// TestVP9DecoderThreadingOfficialIVFMatchesSerial extends the VP9 decoder
// conformance lane to the threaded loop-filter path. It compares govpx serial
// decode against Threads=2/4 on every default official VP90 IVF vector.
func TestVP9DecoderThreadingOfficialIVFMatchesSerial(t *testing.T) {
	root, ok := externalVP9IVFTestDataRoot(t)
	if !ok {
		return
	}
	paths := findVP9IVFTestData(t, root, false)
	if len(paths) == 0 {
		t.Fatalf("no VP90 IVF files found under %s", root)
	}
	assertExternalVP9IVFTestDataMinimum(t, root, paths)

	for _, path := range paths {
		t.Run(safeIVFTestName(root, path), func(t *testing.T) {
			ivf, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile: %v", err)
			}
			frames, err := vp9IVFFramesForThreadingParity(ivf)
			if err != nil {
				t.Fatalf("collect frames: %v", err)
			}
			if len(frames) == 0 {
				t.Skipf("no frames in %s", filepath.Base(path))
			}
			assertVP9ThreadedDecodeMatchesSerial(t, frames, len(frames))
		})
	}
}

// TestVP9DecoderThreadingOfficialProfile0WebMMatchesSerial mirrors the IVF
// threading gate for the curated official VP9 Profile 0 WebM corpus.
func TestVP9DecoderThreadingOfficialProfile0WebMMatchesSerial(t *testing.T) {
	root, ok := externalVP9Profile0WebMTestDataRoot(t)
	if !ok {
		return
	}
	paths := findVP9Profile0WebMTestData(t, root)
	if len(paths) == 0 {
		if os.Getenv("GOVPX_VP9_PROFILE0_WEBM_TEST_DATA_REQUIRED") == "1" ||
			externalVP9Profile0WebMTestMinimum(t, root) > 0 {
			t.Fatalf("no official VP9 Profile 0 WebM files found under %s", root)
		}
		t.Skipf("no official VP9 Profile 0 WebM files found under %s", root)
	}
	assertExternalVP9Profile0WebMTestDataMinimum(t, root, paths)

	for _, path := range paths {
		t.Run(safeIVFTestName(root, path), func(t *testing.T) {
			webm, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile: %v", err)
			}
			packets, err := extractVP9WebMPackets(webm)
			if err != nil {
				t.Fatalf("extract VP9 WebM packets: %v", err)
			}
			if len(packets) == 0 {
				t.Skipf("no VP9 packets in %s", filepath.Base(path))
			}
			assertVP9ThreadedDecodeMatchesSerial(t, packets, len(packets))
		})
	}
}

func TestVP9DecoderThreadingUsesTileModeWorkers(t *testing.T) {
	key := vp9MultiTileStubPacketForTest(t, 1024, 64, 2)
	inter := vp9InterSkipFrameTilesForTest(t, 1024, 64, 2)

	serial, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("serial NewVP9Decoder: %v", err)
	}
	if serial.vp9TilePool != nil {
		t.Fatalf("serial decoder initialized VP9 tile worker pool")
	}
	if err := serial.Decode(key); err != nil {
		t.Fatalf("serial Decode key: %v", err)
	}
	if err := serial.Decode(inter); err != nil {
		t.Fatalf("serial Decode inter: %v", err)
	}

	threaded, err := NewVP9Decoder(VP9DecoderOptions{Threads: 4})
	if err != nil {
		t.Fatalf("threaded NewVP9Decoder: %v", err)
	}
	if threaded.vp9TilePool == nil {
		t.Fatalf("threaded decoder did not initialize VP9 tile worker pool")
	}
	if err := threaded.Decode(key); err != nil {
		t.Fatalf("threaded Decode key: %v", err)
	}
	if got := threaded.vp9TilePool.lastTileJobs; got != 4 {
		t.Fatalf("key threaded tile jobs = %d, want 4", got)
	}
	if got := threaded.vp9TilePool.lastTileJobKind; got != vp9DecoderTileJobIntra {
		t.Fatalf("key threaded tile job kind = %d, want intra", got)
	}
	if err := threaded.Decode(inter); err != nil {
		t.Fatalf("threaded Decode inter: %v", err)
	}
	if got := threaded.vp9TilePool.lastTileJobs; got != 4 {
		t.Fatalf("inter threaded tile jobs = %d, want 4", got)
	}
	if got := threaded.vp9TilePool.lastTileJobKind; got != vp9DecoderTileJobInter {
		t.Fatalf("inter threaded tile job kind = %d, want inter", got)
	}
	if err := threaded.Close(); err != nil {
		t.Fatalf("threaded Close: %v", err)
	}
}

func TestVP9DecoderThreadingNonFrameParallelTileColumnsMatchSerial(t *testing.T) {
	key := vp9MultiTileStubPacketWithFrameParallelForTest(t, 1024, 64, 2,
		false)
	inter := vp9InterSkipFrameTilesWithFrameParallelForTest(t, 1024, 64, 2,
		false)

	serial, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("serial NewVP9Decoder: %v", err)
	}
	serialKey := decodeVP9PacketAndCaptureForThreadingTest(t, serial, key)
	serialInter := decodeVP9PacketAndCaptureForThreadingTest(t, serial, inter)

	threaded, err := NewVP9Decoder(VP9DecoderOptions{Threads: 4})
	if err != nil {
		t.Fatalf("threaded NewVP9Decoder: %v", err)
	}
	if threaded.vp9TilePool == nil {
		t.Fatalf("threaded decoder did not initialize VP9 tile worker pool")
	}
	threadedKey := decodeVP9PacketAndCaptureForThreadingTest(t, threaded, key)
	if got := threaded.vp9TilePool.lastTileJobs; got != 4 {
		t.Fatalf("non-frame-parallel key threaded tile jobs = %d, want 4", got)
	}
	if got := threaded.vp9TilePool.lastTileJobKind; got != vp9DecoderTileJobIntra {
		t.Fatalf("non-frame-parallel key threaded tile job kind = %d, want intra", got)
	}
	threadedInter := decodeVP9PacketAndCaptureForThreadingTest(t, threaded, inter)
	if got := threaded.vp9TilePool.lastTileJobs; got != 4 {
		t.Fatalf("non-frame-parallel inter threaded tile jobs = %d, want 4", got)
	}
	if got := threaded.vp9TilePool.lastTileJobKind; got != vp9DecoderTileJobInter {
		t.Fatalf("non-frame-parallel inter threaded tile job kind = %d, want inter", got)
	}

	if !sameCapturedFramePlanes(serialKey, threadedKey) {
		t.Fatalf("non-frame-parallel tiled key planes diverge")
	}
	if !sameCapturedFramePlanes(serialInter, threadedInter) {
		t.Fatalf("non-frame-parallel tiled inter planes diverge")
	}
	if serial.frameContexts != threaded.frameContexts {
		t.Fatalf("non-frame-parallel threaded decode frame-context adaptation diverged")
	}
	if err := serial.Close(); err != nil {
		t.Fatalf("serial Close: %v", err)
	}
	if err := threaded.Close(); err != nil {
		t.Fatalf("threaded Close: %v", err)
	}
}

func TestVP9DecoderThreadedTileParseSteadyStateAlloc(t *testing.T) {
	key := vp9MultiTileStubPacketForTest(t, 1024, 64, 2)
	inter := vp9InterSkipFrameTilesForTest(t, 1024, 64, 2)
	d, err := NewVP9Decoder(VP9DecoderOptions{Threads: 4})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode key: %v", err)
	}
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode inter: %v", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode inter: %v", err)
	}
	if allocs != 0 {
		t.Fatalf("threaded VP9 tile parse steady state: got %v allocs/op, want 0", allocs)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func decodeVP9PacketAndCaptureForThreadingTest(t testing.TB, d *VP9Decoder,
	packet []byte,
) capturedFramePlanes {
	t.Helper()
	if err := d.Decode(packet); err != nil {
		t.Fatalf("VP9 Decode: %v", err)
	}
	img, ok := d.NextFrame()
	if !ok {
		t.Fatalf("VP9 Decode produced no visible frame")
	}
	return captureDecodedPlanes(img)
}

func assertVP9ThreadedDecodeMatchesSerial(t *testing.T, packets [][]byte, want int) {
	t.Helper()
	serial, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("serial NewVP9Decoder: %v", err)
	}
	serialFrames := decodeVP9Planes(t, serial, packets, want)
	if len(serialFrames) == 0 {
		t.Fatalf("serial VP9 decode produced no visible frames from %d packets",
			len(packets))
	}
	if err := serial.Close(); err != nil {
		t.Fatalf("serial Close: %v", err)
	}

	for _, threads := range []int{2, 4} {
		threaded, err := NewVP9Decoder(VP9DecoderOptions{Threads: threads})
		if err != nil {
			t.Fatalf("threaded NewVP9Decoder(threads=%d): %v", threads, err)
		}
		threadedFrames := decodeVP9Planes(t, threaded, packets, want)
		if err := threaded.Close(); err != nil {
			t.Fatalf("threaded Close(threads=%d): %v", threads, err)
		}
		if len(serialFrames) != len(threadedFrames) {
			t.Fatalf("frame count mismatch: serial=%d threaded=%d threads=%d",
				len(serialFrames), len(threadedFrames), threads)
		}
		for i := range serialFrames {
			if !sameCapturedFramePlanes(serialFrames[i], threadedFrames[i]) {
				t.Fatalf("VP9 frame %d planes diverge with threads=%d", i, threads)
			}
		}
	}
}

func decodeVP9Planes(t testing.TB, d *VP9Decoder, packets [][]byte, want int) []capturedFramePlanes {
	t.Helper()
	out := make([]capturedFramePlanes, 0, want)
	for i, packet := range packets {
		if err := d.Decode(packet); err != nil {
			t.Fatalf("VP9 Decode[%d]: %v", i, err)
		}
		img, ok := d.NextFrame()
		if !ok {
			continue
		}
		out = append(out, captureDecodedPlanes(img))
	}
	return out
}

func sameCapturedFramePlanes(a capturedFramePlanes, b capturedFramePlanes) bool {
	return a.width == b.width &&
		a.height == b.height &&
		bytes.Equal(a.y, b.y) &&
		bytes.Equal(a.u, b.u) &&
		bytes.Equal(a.v, b.v)
}

func vp9IVFFramesForThreadingParity(ivf []byte) ([][]byte, error) {
	if !vp9ExternalIVFHeaderLooksValid(ivf) {
		return nil, testutil.ErrInvalidIVF
	}
	offset := testutil.IVFFileHeaderSize
	var frames [][]byte
	for offset < len(ivf) {
		frame, next, err := testutil.NextIVFFrame(ivf, offset, len(frames))
		if err != nil {
			return nil, err
		}
		frames = append(frames, frame.Data)
		offset = next
	}
	return frames, nil
}

func ivfFramesForThreadingParity(ivf []byte) ([][]byte, error) {
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		return nil, err
	}
	var frames [][]byte
	for offset < len(ivf) {
		frame, next, err := testutil.NextIVFFrame(ivf, offset, len(frames))
		if err != nil {
			return nil, err
		}
		frames = append(frames, frame.Data)
		offset = next
	}
	return frames, nil
}

// BenchmarkDecoderThreading measures the per-frame decode latency for the
// largest VP8 conformance vector available locally and contrasts the
// serial path with Threads=2/4. Skipped when the corpus is not present.
func BenchmarkDecoderThreading(b *testing.B) {
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
		frames, err := ivfFramesForThreadingParity(ivf)
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
