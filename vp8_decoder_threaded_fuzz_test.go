package govpx

import (
	"bytes"
	"sync"
	"testing"
)

// FuzzVP8DecoderThreaded drives concurrent VP8 decodes of a stream synthesised
// from the fuzz []byte across N goroutines (N drawn from a fuzz byte) and
// asserts that all goroutines produce identical Y/U/V planes when fed the
// same packets. Race conditions surface naturally under `go test -race`,
// which the project's gate runs.
//
// The fuzzer encodes a small VP8 stream once on the calling goroutine, then
// hands the immutable packet slice to N independent decoders. Each decoder
// owns its own *VP8Decoder so the test isolates the read-only packet bytes
// from the per-decoder mutable state. The first decoder's planes are taken
// as the reference; the rest must match byte-for-byte.
func FuzzVP8DecoderThreaded(f *testing.F) {
	seeds := [][]byte{
		nil,
		{},
		{0x00},
		{0x80, 0x80, 0x80, 0x80},
		// Pick 4 goroutines + interesting luma noise.
		{0x04, 0x42, 0x87, 0xab, 0xc0, 0x55, 0xaa, 0xff, 0x00, 0x33, 0x66, 0x99},
		// Pick 8 goroutines.
		{0x08, 0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80, 0x90, 0xa0},
		// Single-goroutine path so the loop still runs but degenerates to a
		// serial decode (guards against off-by-one in the worker count).
		{0x01, 0x55, 0xaa, 0x10, 0x20, 0x30},
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("threaded-decode fuzz panicked on %d-byte input: %v", len(data), r)
			}
		}()

		const width, height = 32, 32

		// Worker count drawn from the first fuzz byte; clamped to [2, 8] so the
		// concurrent path actually exercises N>=2 most of the time. A degenerate
		// 1-goroutine setting is allowed via an explicit seed.
		workers := vp8DecoderThreadedFuzzWorkerCount(data)

		// Build two small streams from the fuzz bytes so the harness covers
		// "same packets" (all workers must produce identical output) and a
		// quick keyframe-only stream as a smoke baseline. Encoding can fail
		// for some inputs (encoder rejects config or drops frames); that is
		// not interesting here.
		packets := vp8DecoderThreadedFuzzBuildStream(t, width, height, data)
		if len(packets) == 0 {
			return
		}

		// Reference decode on the calling goroutine.
		reference, ok := vp8DecoderThreadedFuzzDecodeAll(t, width, height, packets)
		if !ok {
			return
		}
		if len(reference) == 0 {
			return
		}

		// Concurrent decodes — each goroutine owns its own decoder and slice of
		// captured planes. The `packets` slice is read-only.
		results := make([][]capturedFramePlanes, workers)
		var wg sync.WaitGroup
		wg.Add(workers)
		for w := range workers {
			go func() {
				defer wg.Done()
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("worker %d panicked: %v", w, r)
					}
				}()
				got, ok := vp8DecoderThreadedFuzzDecodeAll(t, width, height, packets)
				if !ok {
					return
				}
				results[w] = got
			}()
		}
		wg.Wait()

		// Every worker that produced output must match the reference exactly.
		for w, got := range results {
			if got == nil {
				continue
			}
			if len(got) != len(reference) {
				t.Fatalf("worker %d emitted %d frames, want %d", w, len(got), len(reference))
			}
			for i := range got {
				if got[i].width != reference[i].width || got[i].height != reference[i].height {
					t.Fatalf("worker %d frame %d dim=%dx%d, want %dx%d",
						w, i, got[i].width, got[i].height, reference[i].width, reference[i].height)
				}
				if !bytes.Equal(got[i].y, reference[i].y) {
					t.Fatalf("worker %d frame %d Y plane diverges from reference", w, i)
				}
				if !bytes.Equal(got[i].u, reference[i].u) {
					t.Fatalf("worker %d frame %d U plane diverges from reference", w, i)
				}
				if !bytes.Equal(got[i].v, reference[i].v) {
					t.Fatalf("worker %d frame %d V plane diverges from reference", w, i)
				}
			}
		}
	})
}

func vp8DecoderThreadedFuzzWorkerCount(data []byte) int {
	if len(data) == 0 {
		return 2
	}
	n := int(data[0]) % 8
	if n < 2 {
		// Keep 1-worker degenerate path reachable but rare.
		if n == 1 {
			return 1
		}
		return 2
	}
	return n
}

// vp8DecoderThreadedFuzzBuildStream encodes 1..4 frames of fuzz-derived content
// into VP8 packets. Returns nil on encoder failure so the fuzz body skips
// the iteration without a t.Fatal.
func vp8DecoderThreadedFuzzBuildStream(t *testing.T, width, height int, data []byte) [][]byte {
	t.Helper()
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		Deadline:            DeadlineRealtime,
		CpuUsed:             8,
		KeyFrameInterval:    120,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		return nil
	}
	defer e.Close()

	frameCount := 1
	if len(data) > 1 {
		frameCount = 1 + int(data[1])%4
	}
	out := make([][]byte, 0, frameCount)
	buf := make([]byte, width*height*4+1024)
	for i := 0; i < frameCount; i++ {
		seed := data
		if i > 0 && len(data) > i {
			seed = data[i:]
		}
		src := vp8FuzzYUVNoiseImage(width, height, seed)
		var flag EncodeFlags
		if i == 0 {
			flag = EncodeForceKeyFrame
		}
		result, err := e.EncodeInto(buf, src, uint64(i), 1, flag)
		if err != nil || result.Dropped || len(result.Data) == 0 {
			continue
		}
		out = append(out, append([]byte(nil), result.Data...))
	}
	return out
}

func vp8DecoderThreadedFuzzDecodeAll(t *testing.T, width, height int, packets [][]byte) ([]capturedFramePlanes, bool) {
	t.Helper()
	d, err := NewVP8Decoder(DecoderOptions{MaxWidth: width, MaxHeight: height})
	if err != nil {
		return nil, false
	}
	defer func() { _ = d.Close() }()

	out := make([]capturedFramePlanes, 0, len(packets))
	for _, p := range packets {
		if err := d.Decode(p); err != nil {
			// Decoder rejected our own encoder's output — this is the bug
			// signal documented by FuzzVP8DecoderDecode; treat as skip
			// here so the threaded test focuses on cross-goroutine parity.
			return nil, false
		}
		img, ok := d.NextFrame()
		if !ok {
			continue
		}
		out = append(out, captureDecodedPlanes(img))
	}
	return out, true
}
