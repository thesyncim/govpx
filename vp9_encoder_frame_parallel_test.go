package govpx

import (
	"bytes"
	"errors"
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// TestVP9FrameParallelByteParitySerialVsParallel encodes 8 frames in two
// modes and asserts every packet is byte-identical:
//
//	mode A (serial-with-snapshot-restore): single encoder, each inter frame
//	                 is encoded with EncodeNoUpdate{Last,Golden,AltRef,Entropy}
//	                 flags + FrameParallelDecoding=true. Between inter
//	                 frames the encoder's per-frame predictor state
//	                 (prevFrameMvs, prevSegmentMap, ref frames) is restored
//	                 to the post-keyframe snapshot, mirroring the entry
//	                 state every parallel batch member sees.
//	mode B (parallel): FrameParallelEncoderThreads=4, LookaheadFrames=4,
//	                   batch scheduler dispatches frames concurrently.
func TestVP9FrameParallelByteParitySerialVsParallel(t *testing.T) {
	const width, height = 64, 64
	const frameCount = 8

	frames := make([]*image.YCbCr, frameCount)
	for i := range frames {
		frames[i] = vp9test.NewYCbCr(width, height, uint8(80+i*6), 128, 128)
	}

	// Mode A: serial encode with parallel-equivalent per-frame flags AND
	// per-frame state snapshot/restore so every inter frame sees the same
	// entry state the parallel scheduler hands its batch members.
	serialPackets := encodeVP9FrameParallelSerialReference(t, width, height, frames)

	// Mode B: parallel encode through the scheduler.
	parallelPackets := encodeVP9FrameParallelBatched(t, width, height, frames, 4)

	if len(serialPackets) != len(parallelPackets) {
		t.Fatalf("packet count mismatch: serial=%d parallel=%d",
			len(serialPackets), len(parallelPackets))
	}
	for i := range serialPackets {
		if !bytes.Equal(serialPackets[i], parallelPackets[i]) {
			t.Fatalf("packet %d serial vs parallel diverged: serial len=%d parallel len=%d",
				i, len(serialPackets[i]), len(parallelPackets[i]))
		}
	}
}

func encodeVP9FrameParallelSerialReference(t *testing.T, width, height int, frames []*image.YCbCr) [][]byte {
	t.Helper()
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width: width, Height: height,
		FrameParallelDecodingSet: true,
		FrameParallelDecoding:    true,
	})
	if err != nil {
		t.Fatalf("serial NewVP9Encoder: %v", err)
	}
	defer e.Close()
	packets := make([][]byte, 0, len(frames))
	dst := make([]byte, 1<<20)

	// Frame 0 (keyframe): emit and snapshot the post-keyframe state so
	// every subsequent inter frame can encode from the same entry state
	// the parallel batch members would see.
	res, err := e.EncodeIntoWithFlagsResult(frames[0], dst, 0)
	if err != nil {
		t.Fatalf("serial encode keyframe: %v", err)
	}
	packets = append(packets, append([]byte(nil), res.Data...))
	snap := captureVP9PerFrameMvSnapshot(e)

	flags := vp9FrameParallelDispatchFlags(0)
	for i := 1; i < len(frames); i++ {
		res, err := e.EncodeIntoWithFlagsResult(frames[i], dst, flags)
		if err != nil {
			t.Fatalf("serial encode frame %d: %v", i, err)
		}
		packets = append(packets, append([]byte(nil), res.Data...))
		restoreVP9PerFrameMvSnapshot(e, snap)
	}
	return packets
}

// vp9PerFrameMvSnapshot pins the per-frame predictor state that
// encodeVP9FrameIntoWithFlagsResultInternal refreshes unconditionally after
// each encode (not gated by EncodeNoUpdateEntropy). The serial reference
// path restores this snapshot between inter frames so it matches the
// parallel batch entry state.
type vp9PerFrameMvSnapshot struct {
	prevFrameMvs              []vp9dec.MvRef
	prevFrameMvRows           int
	prevFrameMvCols           int
	prevFrameMvsValid         bool
	prevSegmentMap            []uint8
	prevSegmentMapRows        int
	prevSegmentMapCols        int
	prevSegmentMapValid       bool
	prevFrameActiveMapEnabled bool
}

func captureVP9PerFrameMvSnapshot(e *VP9Encoder) vp9PerFrameMvSnapshot {
	var snap vp9PerFrameMvSnapshot
	snap.prevFrameMvs = append([]vp9dec.MvRef(nil), e.prevFrameMvs...)
	snap.prevFrameMvRows = e.prevFrameMvRows
	snap.prevFrameMvCols = e.prevFrameMvCols
	snap.prevFrameMvsValid = e.prevFrameMvsValid
	snap.prevSegmentMap = append([]uint8(nil), e.prevSegmentMap...)
	snap.prevSegmentMapRows = e.prevSegmentMapRows
	snap.prevSegmentMapCols = e.prevSegmentMapCols
	snap.prevSegmentMapValid = e.prevSegmentMapValid
	snap.prevFrameActiveMapEnabled = e.prevFrameActiveMapEnabled
	return snap
}

func restoreVP9PerFrameMvSnapshot(e *VP9Encoder, snap vp9PerFrameMvSnapshot) {
	if cap(e.prevFrameMvs) < len(snap.prevFrameMvs) {
		e.prevFrameMvs = make([]vp9dec.MvRef, len(snap.prevFrameMvs))
	} else {
		e.prevFrameMvs = e.prevFrameMvs[:len(snap.prevFrameMvs)]
	}
	copy(e.prevFrameMvs, snap.prevFrameMvs)
	e.prevFrameMvRows = snap.prevFrameMvRows
	e.prevFrameMvCols = snap.prevFrameMvCols
	e.prevFrameMvsValid = snap.prevFrameMvsValid
	if cap(e.prevSegmentMap) < len(snap.prevSegmentMap) {
		e.prevSegmentMap = make([]uint8, len(snap.prevSegmentMap))
	} else {
		e.prevSegmentMap = e.prevSegmentMap[:len(snap.prevSegmentMap)]
	}
	copy(e.prevSegmentMap, snap.prevSegmentMap)
	e.prevSegmentMapRows = snap.prevSegmentMapRows
	e.prevSegmentMapCols = snap.prevSegmentMapCols
	e.prevSegmentMapValid = snap.prevSegmentMapValid
	e.prevFrameActiveMapEnabled = snap.prevFrameActiveMapEnabled
}

func encodeVP9FrameParallelBatched(t *testing.T, width, height int, frames []*image.YCbCr, threads int) [][]byte {
	t.Helper()
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width: width, Height: height,
		LookaheadFrames:             threads,
		FrameParallelEncoderThreads: threads,
	})
	if err != nil {
		t.Fatalf("parallel NewVP9Encoder: %v", err)
	}
	defer e.Close()
	packets := make([][]byte, 0, len(frames))
	dst := make([]byte, 1<<20)
	for i, src := range frames {
		res, err := e.EncodeIntoWithResult(src, dst)
		if err == nil {
			packets = append(packets, append([]byte(nil), res.Data...))
			continue
		}
		if !errors.Is(err, ErrFrameNotReady) {
			t.Fatalf("parallel encode frame %d: %v", i, err)
		}
	}
	for {
		res, err := e.FlushIntoWithResult(dst)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("parallel flush: %v", err)
		}
		packets = append(packets, append([]byte(nil), res.Data...))
	}
	return packets
}

// BenchmarkVP9FrameParallelSerialVs720pBatch4 runs 16 720p frames through the
// encoder twice — once serial, once with FrameParallelEncoderThreads=4 — so
// the regression harness can compare wall-clock encode time as new SIMD or
// scheduling work lands. The benchmark only emits encoded output through the
// scheduler-driven path on the second run; both runs use otherwise identical
// realtime-mode options.
func BenchmarkVP9FrameParallelSerialVs720pBatch4(b *testing.B) {
	const width, height = 1280, 720
	const frameCount = 16

	frames := make([]*image.YCbCr, frameCount)
	for i := range frames {
		frames[i] = vp9test.NewYCbCr(width, height, uint8(80+i*4), 128, 128)
	}

	b.Run("serial", func(b *testing.B) {
		b.ReportAllocs()
		for n := 0; n < b.N; n++ {
			e, err := NewVP9Encoder(VP9EncoderOptions{
				Width: width, Height: height,
				FrameParallelDecodingSet: true,
				FrameParallelDecoding:    true,
			})
			if err != nil {
				b.Fatalf("NewVP9Encoder: %v", err)
			}
			dst := make([]byte, 1<<22)
			for i, src := range frames {
				flags := EncodeFlags(0)
				if i > 0 {
					flags = vp9FrameParallelDispatchFlags(0)
				}
				if _, err := e.EncodeIntoWithFlagsResult(src, dst, flags); err != nil {
					b.Fatalf("serial encode %d: %v", i, err)
				}
			}
			e.Close()
		}
	})

	b.Run("parallel_threads4", func(b *testing.B) {
		b.ReportAllocs()
		for n := 0; n < b.N; n++ {
			e, err := NewVP9Encoder(VP9EncoderOptions{
				Width: width, Height: height,
				LookaheadFrames:             4,
				FrameParallelEncoderThreads: 4,
			})
			if err != nil {
				b.Fatalf("NewVP9Encoder: %v", err)
			}
			dst := make([]byte, 1<<22)
			for i, src := range frames {
				_, err := e.EncodeIntoWithResult(src, dst)
				if err != nil && !errors.Is(err, ErrFrameNotReady) {
					b.Fatalf("parallel encode %d: %v", i, err)
				}
			}
			for {
				_, err := e.FlushIntoWithResult(dst)
				if errors.Is(err, ErrFrameNotReady) {
					break
				}
				if err != nil {
					b.Fatalf("parallel flush: %v", err)
				}
			}
			e.Close()
		}
	})
}
