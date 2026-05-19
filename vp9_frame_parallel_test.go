package govpx

import (
	"bytes"
	"errors"
	"image"
	"runtime"
	"testing"

	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// TestVP9FrameParallelValidation pins constructor-time gating on the
// FrameParallelEncoderThreads option. Values >= 2 require LookaheadFrames > 0
// and reject AutoAltRef per the encoder-side frame-parallel spec.
func TestVP9FrameParallelValidation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		opts    VP9EncoderOptions
		wantErr error
	}{
		{
			name: "threads_2_without_lookahead_rejected",
			opts: VP9EncoderOptions{
				Width: 64, Height: 64,
				FrameParallelEncoderThreads: 2,
			},
			wantErr: ErrInvalidConfig,
		},
		{
			name: "threads_2_with_lookahead_accepted",
			opts: VP9EncoderOptions{
				Width: 64, Height: 64,
				LookaheadFrames:             4,
				FrameParallelEncoderThreads: 2,
			},
		},
		{
			name: "threads_2_with_auto_altref_rejected",
			opts: VP9EncoderOptions{
				Width: 64, Height: 64,
				LookaheadFrames:             4,
				AutoAltRef:                  true,
				FrameParallelEncoderThreads: 2,
			},
			wantErr: ErrInvalidConfig,
		},
		{
			name: "threads_zero_accepted_serial",
			opts: VP9EncoderOptions{Width: 64, Height: 64},
		},
		{
			name: "threads_one_accepted_serial",
			opts: VP9EncoderOptions{
				Width: 64, Height: 64,
				LookaheadFrames:             4,
				FrameParallelEncoderThreads: 1,
			},
		},
		{
			name: "threads_negative_rejected",
			opts: VP9EncoderOptions{
				Width: 64, Height: 64,
				FrameParallelEncoderThreads: -1,
			},
			wantErr: ErrInvalidConfig,
		},
		{
			name: "threads_above_max_rejected",
			opts: VP9EncoderOptions{
				Width: 64, Height: 64,
				LookaheadFrames:             4,
				FrameParallelEncoderThreads: vp9MaxLookaheadFrames + 1,
			},
			wantErr: ErrInvalidConfig,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, err := NewVP9Encoder(tc.opts)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("NewVP9Encoder err = %v, want %v", err, tc.wantErr)
			}
			if e != nil {
				e.Close()
			}
		})
	}
}

// TestVP9FrameParallelSetFrameParallelEncoderThreads pins the runtime setter
// to mirror the constructor-time validation. It must reject values that
// violate the lookahead / AutoAltRef invariants.
func TestVP9FrameParallelSetFrameParallelEncoderThreads(t *testing.T) {
	t.Run("rejects_threads_2_when_no_lookahead", func(t *testing.T) {
		e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
		if err != nil {
			t.Fatalf("NewVP9Encoder: %v", err)
		}
		defer e.Close()
		if err := e.SetFrameParallelEncoderThreads(2); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("SetFrameParallelEncoderThreads(2) with no lookahead err = %v, want ErrInvalidConfig", err)
		}
		if e.opts.FrameParallelEncoderThreads != 0 {
			t.Fatalf("rejected setter mutated opts: got %d", e.opts.FrameParallelEncoderThreads)
		}
	})
	t.Run("rejects_negative", func(t *testing.T) {
		e, err := NewVP9Encoder(VP9EncoderOptions{
			Width: 64, Height: 64, LookaheadFrames: 4,
		})
		if err != nil {
			t.Fatalf("NewVP9Encoder: %v", err)
		}
		defer e.Close()
		if err := e.SetFrameParallelEncoderThreads(-3); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("SetFrameParallelEncoderThreads(-3) err = %v, want ErrInvalidConfig", err)
		}
	})
	t.Run("accepts_disable_after_enable", func(t *testing.T) {
		e, err := NewVP9Encoder(VP9EncoderOptions{
			Width: 64, Height: 64, LookaheadFrames: 4,
			FrameParallelEncoderThreads: 2,
		})
		if err != nil {
			t.Fatalf("NewVP9Encoder: %v", err)
		}
		defer e.Close()
		if err := e.SetFrameParallelEncoderThreads(0); err != nil {
			t.Fatalf("SetFrameParallelEncoderThreads(0): %v", err)
		}
		if e.opts.FrameParallelEncoderThreads != 0 {
			t.Fatalf("disable setter did not update opts: got %d",
				e.opts.FrameParallelEncoderThreads)
		}
	})
}

// TestVP9FrameParallelErrFrameNotReadySemantics pins the lookahead state
// machine when FrameParallelEncoderThreads=4 and LookaheadFrames=4. The first
// frame in the stream is a keyframe; the scheduler routes it through the
// serial path. After enough inter frames accumulate to fill a parallel batch,
// the next EncodeIntoWithFlagsResult call dispatches the batch and returns
// the first inter frame's packet; subsequent calls drain the staged batch in
// display order via FlushInto.
func TestVP9FrameParallelErrFrameNotReadySemantics(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width: width, Height: height,
		LookaheadFrames:             4,
		FrameParallelEncoderThreads: 4,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	dst := make([]byte, 1<<20)
	// Push enough frames to fill the lookahead and trigger the first
	// (serial) encode of the keyframe. The first LookaheadFrames-1 pushes
	// return ErrFrameNotReady; the LookaheadFrames-th push triggers the
	// keyframe emit through the serial path.
	for i := range 3 {
		src := newVP9YCbCrForTest(width, height, uint8(96+i*8), 128, 128)
		_, err := e.EncodeIntoWithResult(src, dst)
		if !errors.Is(err, ErrFrameNotReady) {
			t.Fatalf("frame %d expected ErrFrameNotReady, got %v", i, err)
		}
	}
	src := newVP9YCbCrForTest(width, height, 96+3*8, 128, 128)
	res, err := e.EncodeIntoWithResult(src, dst)
	if err != nil {
		t.Fatalf("frame 3 (keyframe trigger): %v", err)
	}
	if len(res.Data) == 0 {
		t.Fatalf("frame 3 keyframe trigger produced empty packet")
	}
	if !res.KeyFrame {
		t.Fatalf("frame 3 trigger expected KeyFrame=true, got %+v", res)
	}
	// Push more frames so the scheduler can build an inter-frame batch.
	// Inter-frame batch fires when the lookahead is full and the head is
	// not a keyframe.
	gotBatchFirst := false
	for i := 4; i < 8; i++ {
		src := newVP9YCbCrForTest(width, height, uint8(96+i*8), 128, 128)
		res, err := e.EncodeIntoWithResult(src, dst)
		if errors.Is(err, ErrFrameNotReady) {
			continue
		}
		if err != nil {
			t.Fatalf("inter frame %d encode: %v", i, err)
		}
		if res.KeyFrame {
			t.Fatalf("frame %d unexpectedly emitted as keyframe inside parallel batch", i)
		}
		gotBatchFirst = true
	}
	if !gotBatchFirst {
		t.Fatalf("never received a parallel batch packet from EncodeIntoWithResult")
	}
	// Drain remaining staged batch packets.
	for {
		_, err := e.FlushIntoWithResult(dst)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("drain: %v", err)
		}
	}
}

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
		frames[i] = newVP9YCbCrForTest(width, height, uint8(80+i*6), 128, 128)
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
		frames[i] = newVP9YCbCrForTest(width, height, uint8(80+i*4), 128, 128)
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

// TestVP9FrameParallelGoroutineLeak gates that the scheduler does not leak
// goroutines across a parallel batch + flush. Each batch spawns N-1 helper
// goroutines that must rejoin before the batch retires.
func TestVP9FrameParallelGoroutineLeak(t *testing.T) {
	const width, height = 64, 64
	runtime.GC()
	baseline := runtime.NumGoroutine()
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width: width, Height: height,
		LookaheadFrames:             4,
		FrameParallelEncoderThreads: 4,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dst := make([]byte, 1<<20)
	for i := range 4 {
		src := newVP9YCbCrForTest(width, height, uint8(96+i*8), 128, 128)
		_, _ = e.EncodeIntoWithResult(src, dst)
	}
	for {
		_, err := e.FlushIntoWithResult(dst)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("FlushIntoWithResult: %v", err)
		}
	}
	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Allow runtime time to descend through the goroutine table after Close.
	runtime.GC()
	// Some tolerance: the test framework may have spawned goroutines unrelated
	// to the encoder. Tighten this once the test surfaces a real leak.
	const tolerance = 4
	if final := runtime.NumGoroutine(); final > baseline+tolerance {
		t.Fatalf("goroutine leak: baseline=%d final=%d", baseline, final)
	}
}
