package govpx

import (
	"fmt"
	"os"
	"testing"
)

// TestDiagR12_D_AutoSpeedTrajectory dumps the per-frame Speed evolution on the
// 720p bench fixture. Off by default; run with GOVPX_DIAG=1.
func TestDiagR12_D_AutoSpeedTrajectory(t *testing.T) {
	if os.Getenv("GOVPX_DIAG") != "1" {
		t.Skip("set GOVPX_DIAG=1 to dump R12-D auto-speed trajectory")
	}
	const W, H, FPS, KBPS, F = 1280, 720, 30, 1500, 30
	enc, err := NewVP8Encoder(EncoderOptions{
		Width: W, Height: H, FPS: FPS,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   KBPS,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		Deadline:            DeadlineRealtime,
		CpuUsed:             8,
		KeyFrameInterval:    FPS,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		UndershootPct:       100,
		OvershootPct:        15,
		Threads:             1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	pkt := make([]byte, W*H*6)
	for i := range F {
		img := diagMakeFrame(W, H, i)
		speedBefore := enc.autoSpeed
		preAvgPick, preAvgEnc := enc.avgPickModeTime, enc.avgEncodeTime
		_, err := enc.EncodeInto(pkt, img, uint64(i)*33000, 33000, 0)
		if err != nil {
			t.Fatal(err)
		}
		postAvgPick, postAvgEnc := enc.avgPickModeTime, enc.avgEncodeTime
		fmt.Printf("frame=%2d  speedBefore=%2d  preAvgPick=%6d  preAvgEnc=%6d  postAvgPick=%6d  postAvgEnc=%6d\n",
			i, speedBefore, preAvgPick, preAvgEnc, postAvgPick, postAvgEnc)
	}
}

// TestDiagR12_D_DumpFixture writes the same synthetic 720p YUV fixture to disk
// (path in env var GOVPX_DIAG_FIXTURE_OUT) so the patched libvpx vpxenc-oracle
// can be driven against the identical pixel stream govpx sees. Off by default.
func TestDiagR12_D_DumpFixture(t *testing.T) {
	out := os.Getenv("GOVPX_DIAG_FIXTURE_OUT")
	if out == "" {
		t.Skip("set GOVPX_DIAG_FIXTURE_OUT to dump 720p YUV fixture")
	}
	const W, H, F = 1280, 720, 30
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for i := range F {
		img := diagMakeFrame(W, H, i)
		if _, err := f.Write(img.Y); err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(img.U); err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(img.V); err != nil {
			t.Fatal(err)
		}
	}
}

func diagMakeFrame(width, height, idx int) Image {
	uvW := (width + 1) >> 1
	uvH := (height + 1) >> 1
	img := Image{
		Width: width, Height: height,
		Y: make([]byte, width*height),
		U: make([]byte, uvW*uvH), V: make([]byte, uvW*uvH),
		YStride: width, UStride: uvW, VStride: uvW,
	}
	for r := range height {
		for c := range width {
			img.Y[r*img.YStride+c] = byte(32 + ((r*3 + c*5 + idx*7) & 191))
		}
	}
	for r := range uvH {
		for c := range uvW {
			img.U[r*img.UStride+c] = byte(96 + ((r*2 + c + idx*3) & 63))
			img.V[r*img.VStride+c] = byte(144 + ((r + c*2 + idx*5) & 63))
		}
	}
	return img
}
