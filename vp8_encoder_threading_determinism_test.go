package govpx

import (
	"bytes"
	"runtime"
	"testing"
)

// TestEncoderThreadsProducesIdenticalBitstream pins the byte-for-byte
// invariant the parity reports depend on at Threads=1: this must
// stay byte-identical to the historical serial macroblock loop forever.
// Threads=0 is normalised to 1 by the option validator so it lands on
// the same path. Threads>=2 may diverge once the row-threaded
// macroblock pipeline lands (libvpx itself produces a different
// bitstream when ethreading is enabled, since the MV predictor's
// last-coded-MV cache and the entropy probabilities update at a
// different cadence under threading); deterministic-at-fixed-N parity
// is checked by TestEncoderThreadsProducesDeterministicAtFixedN below.
func TestEncoderThreadsProducesIdenticalBitstream(t *testing.T) {
	const (
		width  = 64
		height = 48
		frames = 4
	)
	// Threads=0 (validator normalises to 1) and Threads=1 must remain
	// byte-identical to each other and to the canonical Threads=1
	// baseline. This is the regression gate for the zero-cost serial
	// path.
	zeroCostThreadCounts := []int{0, 1}

	makeFrame := func(index int) Image {
		img := testImage(width, height)
		for i := range img.Y {
			img.Y[i] = byte((i*7 + index*13) & 0xFF)
		}
		for i := range img.U {
			img.U[i] = byte(96 + ((i + index*3) & 0x3F))
		}
		for i := range img.V {
			img.V[i] = byte(144 + ((i*2 + index*5) & 0x3F))
		}
		return img
	}

	encode := func(t *testing.T, threads int) [][]byte {
		t.Helper()
		e, err := NewVP8Encoder(EncoderOptions{
			Width:             width,
			Height:            height,
			FPS:               30,
			RateControlMode:   RateControlCBR,
			TargetBitrateKbps: 1200,
			MinQuantizer:      4,
			MaxQuantizer:      56,
			DropFrameAllowed:  false,
			Deadline:          DeadlineRealtime,
			// Pin realtime Speed. Positive cpu_used enables wall-clock
			// autoSpeed, which can legitimately pick different speeds between
			// repeated runs and obscure the threading determinism invariant.
			CpuUsed:             -8,
			KeyFrameInterval:    120,
			ErrorResilient:      true,
			BufferSizeMs:        600,
			BufferInitialSizeMs: 400,
			BufferOptimalSizeMs: 500,
			Threads:             threads,
		})
		if err != nil {
			t.Fatalf("NewVP8Encoder Threads=%d returned error: %v", threads, err)
		}
		packets := make([][]byte, 0, frames)
		buf := make([]byte, max(8192, width*height*4))
		for i := range frames {
			res, err := e.EncodeInto(buf, makeFrame(i), uint64(i), 1, 0)
			if err != nil {
				t.Fatalf("EncodeInto Threads=%d frame %d: %v", threads, i, err)
			}
			if res.Dropped {
				t.Fatalf("EncodeInto Threads=%d frame %d unexpectedly dropped", threads, i)
			}
			packets = append(packets, append([]byte(nil), res.Data...))
		}
		return packets
	}

	baseline := encode(t, 1)
	for _, threads := range zeroCostThreadCounts {
		t.Run("threads_"+itoaSmall(threads), func(t *testing.T) {
			got := encode(t, threads)
			if len(got) != len(baseline) {
				t.Fatalf("threads=%d produced %d packets, baseline=%d", threads, len(got), len(baseline))
			}
			for i := range got {
				if !bytes.Equal(got[i], baseline[i]) {
					t.Fatalf("threads=%d frame %d bitstream diverges from Threads=1 baseline (%d vs %d bytes)", threads, i, len(got[i]), len(baseline[i]))
				}
			}
		})
	}
}

func TestThreadedKeyFrameReferencesMatchDecoder(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             64,
		Height:            64,
		FPS:               30,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: 700,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		Deadline:          DeadlineRealtime,
		CpuUsed:           8,
		KeyFrameInterval:  999,
		Threads:           2,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	src := encoderValidationPanningFrame(64, 64, 0)
	dst := make([]byte, 64*64*4)
	result, err := e.EncodeInto(dst, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}
	if result.Dropped {
		t.Fatalf("EncodeInto unexpectedly dropped frame")
	}

	decoded := decodeSingleFrame(t, result.Data)
	assertImagesEqual(t, "threaded keyframe current", decoded, publicImageFromVP8(&e.current.Img))
	assertImagesEqual(t, "threaded keyframe last", decoded, publicImageFromVP8(&e.lastRef.Img))
}

// TestEncoderThreadsProducesDeterministicAtFixedN verifies the encoder
// produces a byte-stable bitstream at every fixed Threads value: two
// runs with identical inputs and identical Threads must yield identical
// packets. The bitstream may differ across Threads values (libvpx
// allows this once ethreading turns on), but at any given fixed N the
// encoder must be deterministic. This is the regression gate for the
// row-threaded pipeline once it ships.
func TestEncoderThreadsProducesDeterministicAtFixedN(t *testing.T) {
	const (
		width  = 64
		height = 48
		frames = 4
	)
	threadCounts := []int{1, 2, 4, 8}
	if n := runtime.NumCPU(); n > 8 && n != 1 {
		threadCounts = append(threadCounts, n)
	}

	makeFrame := func(index int) Image {
		img := testImage(width, height)
		for i := range img.Y {
			img.Y[i] = byte((i*11 + index*17) & 0xFF)
		}
		for i := range img.U {
			img.U[i] = byte(112 + ((i + index*5) & 0x3F))
		}
		for i := range img.V {
			img.V[i] = byte(128 + ((i*3 + index*7) & 0x3F))
		}
		return img
	}

	encode := func(t *testing.T, threads int) [][]byte {
		t.Helper()
		e, err := NewVP8Encoder(EncoderOptions{
			Width:             width,
			Height:            height,
			FPS:               30,
			RateControlMode:   RateControlCBR,
			TargetBitrateKbps: 1200,
			MinQuantizer:      4,
			MaxQuantizer:      56,
			DropFrameAllowed:  false,
			Deadline:          DeadlineRealtime,
			// Pin realtime Speed. Positive cpu_used enables wall-clock
			// autoSpeed, which is intentionally timing-sensitive; this test is
			// about fixed-N row-threading determinism.
			CpuUsed:             -8,
			KeyFrameInterval:    120,
			ErrorResilient:      true,
			BufferSizeMs:        600,
			BufferInitialSizeMs: 400,
			BufferOptimalSizeMs: 500,
			Threads:             threads,
		})
		if err != nil {
			t.Fatalf("NewVP8Encoder Threads=%d returned error: %v", threads, err)
		}
		packets := make([][]byte, 0, frames)
		buf := make([]byte, max(8192, width*height*4))
		for i := range frames {
			res, err := e.EncodeInto(buf, makeFrame(i), uint64(i), 1, 0)
			if err != nil {
				t.Fatalf("EncodeInto Threads=%d frame %d: %v", threads, i, err)
			}
			if res.Dropped {
				t.Fatalf("EncodeInto Threads=%d frame %d unexpectedly dropped", threads, i)
			}
			packets = append(packets, append([]byte(nil), res.Data...))
		}
		return packets
	}

	for _, threads := range threadCounts {
		t.Run("threads_"+itoaSmall(threads), func(t *testing.T) {
			runA := encode(t, threads)
			runB := encode(t, threads)
			if len(runA) != len(runB) {
				t.Fatalf("threads=%d run A produced %d packets, run B=%d", threads, len(runA), len(runB))
			}
			for i := range runA {
				if !bytes.Equal(runA[i], runB[i]) {
					t.Fatalf("threads=%d frame %d not deterministic across runs (%d vs %d bytes)", threads, i, len(runA[i]), len(runB[i]))
				}
			}
		})
	}
}
