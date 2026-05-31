package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"os"
	"path/filepath"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp9corpus"
)

// TestVP9DecoderThreadingOfficialIVFMatchesSerial extends the VP9 decoder
// conformance lane to the threaded loop-filter path. It compares govpx serial
// decode against Threads=2/4 on every default official VP90 IVF vector.
func TestVP9DecoderThreadingOfficialIVFMatchesSerial(t *testing.T) {
	root, ok := vp9corpus.IVFRoot(t)
	if !ok {
		return
	}
	paths := vp9corpus.FindIVF(t, root, false)
	if len(paths) == 0 {
		t.Fatalf("no VP90 IVF files found under %s", root)
	}
	vp9corpus.AssertIVFMinimum(t, root, paths)

	for _, path := range paths {
		t.Run(testutil.SafeCorpusTestName(root, path), func(t *testing.T) {
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
	root, ok := vp9corpus.Profile0WebMRoot(t)
	if !ok {
		return
	}
	paths := vp9corpus.FindProfile0WebM(t, root)
	vp9corpus.RequireProfile0WebMFiles(t, root, paths)

	for _, path := range paths {
		t.Run(testutil.SafeCorpusTestName(root, path), func(t *testing.T) {
			webm, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile: %v", err)
			}
			packets, err := testutil.ExtractVP9WebMPackets(webm)
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

func TestVP9DecoderThreadingOfficialProfile0TileColumnsUseWorkers(t *testing.T) {
	root, ok := vp9corpus.Profile0WebMRoot(t)
	if !ok {
		return
	}
	paths := vp9corpus.FindProfile0WebM(t, root)
	vp9corpus.RequireProfile0WebMFiles(t, root, paths)

	wanted := map[string]struct{}{
		"vp90-2-08-tile_1x4.webm":                {},
		"vp90-2-08-tile_1x8.webm":                {},
		"vp90-2-08-tile_1x2_frame_parallel.webm": {},
	}
	var tiledPaths []string
	for _, path := range paths {
		if _, ok := wanted[filepath.Base(path)]; ok {
			tiledPaths = append(tiledPaths, path)
		}
	}
	if len(tiledPaths) != len(wanted) {
		t.Fatalf("official VP9 tiled Profile 0 WebM files = %d, want %d",
			len(tiledPaths), len(wanted))
	}

	for _, path := range tiledPaths {
		t.Run(testutil.SafeCorpusTestName(root, path), func(t *testing.T) {
			webm, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile: %v", err)
			}
			packets, err := testutil.ExtractVP9WebMPackets(webm)
			if err != nil {
				t.Fatalf("extract VP9 WebM packets: %v", err)
			}
			if len(packets) == 0 {
				t.Skipf("no VP9 packets in %s", filepath.Base(path))
			}

			for _, threads := range []int{3, 4} {
				d, err := NewVP9Decoder(VP9DecoderOptions{Threads: threads})
				if err != nil {
					t.Fatalf("threaded NewVP9Decoder(threads=%d): %v",
						threads, err)
				}
				usedWorkers := false
				for i, packet := range packets {
					d.vp9TilePool.lastTileJobs = 0
					if err := d.Decode(packet); err != nil {
						t.Fatalf("threaded Decode[%d] threads=%d: %v",
							i, threads, err)
					}
					_, _ = d.NextFrame()
					if d.vp9TilePool.lastTileJobs > 1 {
						usedWorkers = true
					}
				}
				if err := d.Close(); err != nil {
					t.Fatalf("threaded Close(threads=%d): %v",
						threads, err)
				}
				if !usedWorkers {
					t.Fatalf("threaded decoder did not use tile workers for %s with threads=%d",
						filepath.Base(path), threads)
				}
			}
		})
	}
}

func TestVP9DecoderThreadingUsesTileModeWorkers(t *testing.T) {
	key := vp9test.MultiTileStubPacket(t, 1024, 64, 2)
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
	key := vp9test.MultiTileStubPacketWithFrameParallel(t, 1024, 64, 2,
		false)
	inter := vp9InterSkipFrameTilesWithFrameParallelForTest(t, 1024, 64, 2,
		false)

	serial, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("serial NewVP9Decoder: %v", err)
	}
	serialKey := decodeOneFrameForTest(t, "VP9", serial, key)
	serialInter := decodeOneFrameForTest(t, "VP9", serial, inter)

	threaded, err := NewVP9Decoder(VP9DecoderOptions{Threads: 4})
	if err != nil {
		t.Fatalf("threaded NewVP9Decoder: %v", err)
	}
	if threaded.vp9TilePool == nil {
		t.Fatalf("threaded decoder did not initialize VP9 tile worker pool")
	}
	threadedKey := decodeOneFrameForTest(t, "VP9", threaded, key)
	if got := threaded.vp9TilePool.lastTileJobs; got != 4 {
		t.Fatalf("non-frame-parallel key threaded tile jobs = %d, want 4", got)
	}
	if got := threaded.vp9TilePool.lastTileJobKind; got != vp9DecoderTileJobIntra {
		t.Fatalf("non-frame-parallel key threaded tile job kind = %d, want intra", got)
	}
	threadedInter := decodeOneFrameForTest(t, "VP9", threaded, inter)
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
	key := vp9test.MultiTileStubPacket(t, 1024, 64, 2)
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

func assertVP9ThreadedDecodeMatchesSerial(t *testing.T, packets [][]byte, want int) {
	t.Helper()
	serial, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("serial NewVP9Decoder: %v", err)
	}
	serialFrames := decodeFramesForTest(t, "VP9", serial, packets, want)
	if len(serialFrames) == 0 {
		t.Fatalf("serial VP9 decode produced no visible frames from %d packets",
			len(packets))
	}
	if err := serial.Close(); err != nil {
		t.Fatalf("serial Close: %v", err)
	}

	for _, threads := range []int{2, 3, 4} {
		threaded, err := NewVP9Decoder(VP9DecoderOptions{Threads: threads})
		if err != nil {
			t.Fatalf("threaded NewVP9Decoder(threads=%d): %v", threads, err)
		}
		threadedFrames := decodeFramesForTest(t, "VP9", threaded, packets, want)
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

func vp9IVFFramesForThreadingParity(ivf []byte) ([][]byte, error) {
	if !testutil.VP9IVFHeaderLooksValid(ivf) {
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
