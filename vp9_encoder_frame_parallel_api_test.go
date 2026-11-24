package govpx_test

import (
	"errors"
	"runtime"
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9FrameParallelValidation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		opts    govpx.VP9EncoderOptions
		wantErr error
	}{
		{
			name: "threads_2_without_lookahead_rejected",
			opts: govpx.VP9EncoderOptions{
				Width: 64, Height: 64,
				FrameParallelEncoderThreads: 2,
			},
			wantErr: govpx.ErrInvalidConfig,
		},
		{
			name: "threads_2_with_lookahead_accepted",
			opts: govpx.VP9EncoderOptions{
				Width: 64, Height: 64,
				LookaheadFrames:             4,
				FrameParallelEncoderThreads: 2,
			},
		},
		{
			name: "threads_2_with_auto_altref_rejected",
			opts: govpx.VP9EncoderOptions{
				Width: 64, Height: 64,
				LookaheadFrames:             4,
				AutoAltRef:                  true,
				FrameParallelEncoderThreads: 2,
			},
			wantErr: govpx.ErrInvalidConfig,
		},
		{
			name: "threads_zero_accepted_serial",
			opts: govpx.VP9EncoderOptions{Width: 64, Height: 64},
		},
		{
			name: "threads_one_accepted_serial",
			opts: govpx.VP9EncoderOptions{
				Width: 64, Height: 64,
				LookaheadFrames:             4,
				FrameParallelEncoderThreads: 1,
			},
		},
		{
			name: "threads_negative_rejected",
			opts: govpx.VP9EncoderOptions{
				Width: 64, Height: 64,
				FrameParallelEncoderThreads: -1,
			},
			wantErr: govpx.ErrInvalidConfig,
		},
		{
			name: "threads_above_max_rejected",
			opts: govpx.VP9EncoderOptions{
				Width: 64, Height: 64,
				LookaheadFrames:             4,
				FrameParallelEncoderThreads: 26,
			},
			wantErr: govpx.ErrInvalidConfig,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, err := govpx.NewVP9Encoder(tc.opts)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("NewVP9Encoder err = %v, want %v", err, tc.wantErr)
			}
			if e != nil {
				e.Close()
			}
		})
	}
}

func TestVP9FrameParallelSetFrameParallelEncoderThreads(t *testing.T) {
	t.Run("rejects_threads_2_when_no_lookahead", func(t *testing.T) {
		e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 64, Height: 64})
		if err != nil {
			t.Fatalf("NewVP9Encoder: %v", err)
		}
		defer e.Close()
		if err := e.SetFrameParallelEncoderThreads(2); !errors.Is(err, govpx.ErrInvalidConfig) {
			t.Fatalf("SetFrameParallelEncoderThreads(2) with no lookahead err = %v, want ErrInvalidConfig", err)
		}
	})
	t.Run("rejects_negative", func(t *testing.T) {
		e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
			Width: 64, Height: 64, LookaheadFrames: 4,
		})
		if err != nil {
			t.Fatalf("NewVP9Encoder: %v", err)
		}
		defer e.Close()
		if err := e.SetFrameParallelEncoderThreads(-3); !errors.Is(err, govpx.ErrInvalidConfig) {
			t.Fatalf("SetFrameParallelEncoderThreads(-3) err = %v, want ErrInvalidConfig", err)
		}
	})
	t.Run("accepts_disable_after_enable", func(t *testing.T) {
		e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
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
	})
}

func TestVP9FrameParallelErrFrameNotReadySemantics(t *testing.T) {
	const width, height = 64, 64
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width: width, Height: height,
		LookaheadFrames:             4,
		FrameParallelEncoderThreads: 4,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	dst := make([]byte, 1<<20)
	for i := range 3 {
		src := vp9test.NewYCbCr(width, height, uint8(96+i*8), 128, 128)
		_, err := e.EncodeIntoWithResult(src, dst)
		if !errors.Is(err, govpx.ErrFrameNotReady) {
			t.Fatalf("frame %d expected ErrFrameNotReady, got %v", i, err)
		}
	}
	src := vp9test.NewYCbCr(width, height, 96+3*8, 128, 128)
	res, err := e.EncodeIntoWithResult(src, dst)
	if err != nil {
		t.Fatalf("frame 3 keyframe trigger: %v", err)
	}
	if len(res.Data) == 0 {
		t.Fatalf("frame 3 keyframe trigger produced empty packet")
	}
	if !res.KeyFrame {
		t.Fatalf("frame 3 trigger expected KeyFrame=true, got %+v", res)
	}

	gotBatchFirst := false
	for i := 4; i < 8; i++ {
		src := vp9test.NewYCbCr(width, height, uint8(96+i*8), 128, 128)
		res, err := e.EncodeIntoWithResult(src, dst)
		if errors.Is(err, govpx.ErrFrameNotReady) {
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
	for {
		_, err := e.FlushIntoWithResult(dst)
		if errors.Is(err, govpx.ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("drain: %v", err)
		}
	}
}

func TestVP9FrameParallelGoroutineLeak(t *testing.T) {
	const width, height = 64, 64
	runtime.GC()
	baseline := runtime.NumGoroutine()
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width: width, Height: height,
		LookaheadFrames:             4,
		FrameParallelEncoderThreads: 4,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dst := make([]byte, 1<<20)
	for i := range 4 {
		src := vp9test.NewYCbCr(width, height, uint8(96+i*8), 128, 128)
		_, _ = e.EncodeIntoWithResult(src, dst)
	}
	for {
		_, err := e.FlushIntoWithResult(dst)
		if errors.Is(err, govpx.ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("FlushIntoWithResult: %v", err)
		}
	}
	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	runtime.GC()
	const tolerance = 4
	if final := runtime.NumGoroutine(); final > baseline+tolerance {
		t.Fatalf("goroutine leak: baseline=%d final=%d", baseline, final)
	}
}
