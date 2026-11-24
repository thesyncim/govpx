package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9SceneDetectionOnePassHighSourceSADNoLag(t *testing.T) {
	const width, height = 320, 320
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              width,
		Height:             height,
		Deadline:           DeadlineRealtime,
		CpuUsed:            6,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  1000,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	last := vp9test.NewYCbCr(width, height, 0, 128, 128)
	src := vp9test.NewYCbCr(width, height, 255, 128, 128)
	e.vp9CommitLastSource(last, true, false)
	e.rc.framesSinceKey = 3

	e.vp9SceneDetectionOnePass(src, true, height>>3, width>>3)
	if !e.rc.highSourceSAD {
		t.Fatalf("highSourceSAD = false, want true for high no-lag source SAD")
	}
	if !e.rc.highNumBlocksWithMotion {
		t.Fatalf("highNumBlocksWithMotion = false, want true")
	}
	const wantAvg = uint64((64 * 64 * 255) >> 2)
	if got := e.rc.avgSourceSAD[0]; got != wantAvg {
		t.Fatalf("avgSourceSAD[0] = %d, want %d", got, wantAvg)
	}
}

func TestVP9SceneDetectionOnePassZeroTempNoHighSourceSAD(t *testing.T) {
	const width, height = 320, 320
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              width,
		Height:             height,
		Deadline:           DeadlineRealtime,
		CpuUsed:            6,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  1000,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	src := vp9test.NewYCbCr(width, height, 100, 128, 128)
	e.vp9CommitLastSource(src, true, false)
	e.rc.framesSinceKey = 3
	e.rc.avgSourceSAD[0] = 1234

	e.vp9SceneDetectionOnePass(src, true, height>>3, width>>3)
	if e.rc.highSourceSAD {
		t.Fatalf("highSourceSAD = true, want false for zero-temp source SAD")
	}
	if e.rc.highNumBlocksWithMotion {
		t.Fatalf("highNumBlocksWithMotion = true, want false")
	}
	const wantAvg = uint64((3 * 1234) >> 2)
	if got := e.rc.avgSourceSAD[0]; got != wantAvg {
		t.Fatalf("avgSourceSAD[0] = %d, want %d", got, wantAvg)
	}
}
