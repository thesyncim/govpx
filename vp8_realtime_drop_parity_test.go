package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// makeVP8DropParityFrame reproduces cmd/govpx-bench/benchcmd
// makeBenchmarkFrame byte-for-byte: a synthetic high-entropy ramp whose
// per-frame pixel deltas defeat ZEROMV/skip, overloading the 720p CBR
// budget so the drop-frame/decimation path engages.
func makeVP8DropParityFrame(width, height, index int) Image {
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	img := Image{
		Width:   width,
		Height:  height,
		Y:       make([]byte, width*height),
		U:       make([]byte, uvWidth*uvHeight),
		V:       make([]byte, uvWidth*uvHeight),
		YStride: width,
		UStride: uvWidth,
		VStride: uvWidth,
	}
	for row := range height {
		for col := range width {
			img.Y[row*img.YStride+col] = byte(32 + ((row*3 + col*5 + index*7) & 191))
		}
	}
	for row := range uvHeight {
		for col := range uvWidth {
			img.U[row*img.UStride+col] = byte(96 + ((row*2 + col + index*3) & 63))
			img.V[row*img.VStride+col] = byte(144 + ((row + col*2 + index*5) & 63))
		}
	}
	return img
}

// TestVP8RealtimeOverloadDropParity pins the 720p realtime cpu_used=8 CBR
// overload fixture (the exact cmd/govpx-bench configuration:
// -codec=vp8 -width=1280 -height=720 -frames=120 -fps=30 -bitrate=1500
// -mode=realtime -cpu-used=8) byte-for-byte, including its frame-drop
// pattern.
//
// Ground truth (2026-07-01 drop-parity audit): libvpx v1.16.0 vpxenc with
// the bench parity flags (--rt --cpu-used=8 --drop-frame=30 --end-usage=cbr
// --target-bitrate=1500 --buf-sz=1000 --buf-initial-sz=500 --buf-optimal-sz=600
// --undershoot-pct=100 --overshoot-pct=15 --min-q=2 --max-q=56
// --kf-min-dist=3000 --kf-max-dist=3000 --max-intra-rate=900
// --noise-sensitivity=4 --static-thresh=1 --threads=1 --token-parts=0
// --timebase=1/30) on this fixture encodes 94 frames and drops 26; all 94
// emitted packets were verified BYTE-IDENTICAL to govpx's output after the
// fix, so the SHA-256 below pins libvpx's own bytes, not merely govpx's.
//
// Before the fix govpx encoded 52 / dropped 68 on the same input: the
// retired "libvpx-realistic cpu_used+1" speed-feature overrides (HEX
// search, no iterative sub-pel, no improved_mv_pred at >= 1500 MBs) and
// the 2*budget-2 keyframe timing pin ran Speed-9 search features while
// the production vpxenc stayed at the auto-select floor of Speed 4,
// inflating interframes ~1.9x at max Q; the drained buffer model then
// cascaded through vp8_check_drop_buffer (vp8/encoder/onyx_if.c:3216)
// into 2.6x the drops. See the drop-parity audit note in
// vp8_encoder_config.go.
func TestVP8RealtimeOverloadDropParity(t *testing.T) {
	const (
		width  = 1280
		height = 720
		frames = 120
	)
	// Byte-verified libvpx v1.16.0 ground truth for this fixture.
	const (
		wantEncoded = 94
		wantDropped = 26
		wantPattern = "EEEEEDEDEDEDEEEEEEEEEEEEEEEDEDEDEEEEEEEEEEEEEEEDEDEDEEEEEEEEEDEDEDEDEDEEEEEEEEEDEDEDEDEDEDEEEEEEEEEDEDEDEDEDEEEEEEEEEEEE"
		wantSHA256  = "464d9e9f2456d0450a090c05c8e02e168e1e12713e909ef3692260e45df8a2f5"
	)
	// First packets: keyframe then the early inter ramp. These match the
	// per-frame byte counts reported by vpxenc on the identical input.
	wantHeadSizes := []int{73765, 6499, 8284, 8834, 8069, 11042, 8537, 9913}

	opts := EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1500,
		MinQuantizer:        2,
		MaxQuantizer:        56,
		Deadline:            DeadlineRealtime,
		CpuUsed:             8,
		KeyFrameInterval:    3000,
		BufferSizeMs:        1000,
		BufferInitialSizeMs: 500,
		BufferOptimalSizeMs: 600,
		UndershootPct:       100,
		OvershootPct:        15,
		MaxIntraBitratePct:  900,
		DropFrameAllowed:    true,
		DropFrameWaterMark:  30,
		NoiseSensitivity:    4,
		StaticThreshold:     1,
		Threads:             1,
		TokenPartitions:     0,
	}
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatal(err)
	}
	packet := make([]byte, width*height*6)
	encoded := 0
	dropped := 0
	pattern := make([]byte, 0, frames)
	sizes := make([]int, 0, frames)
	digest := sha256.New()
	for i := range frames {
		frame := makeVP8DropParityFrame(width, height, i)
		res, err := enc.EncodeInto(packet, frame, uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if res.Dropped {
			dropped++
			pattern = append(pattern, 'D')
			continue
		}
		encoded++
		pattern = append(pattern, 'E')
		sizes = append(sizes, res.SizeBytes)
		digest.Write(res.Data)
	}
	if encoded != wantEncoded || dropped != wantDropped {
		t.Errorf("encoded/dropped = %d/%d, want %d/%d (libvpx vpxenc parity)", encoded, dropped, wantEncoded, wantDropped)
	}
	if got := string(pattern); got != wantPattern {
		t.Errorf("drop pattern mismatch\n got: %s\nwant: %s", got, wantPattern)
	}
	for i, want := range wantHeadSizes {
		if i >= len(sizes) {
			t.Fatalf("only %d packets emitted, want at least %d", len(sizes), len(wantHeadSizes))
		}
		if sizes[i] != want {
			t.Errorf("packet %d size = %d, want %d (vpxenc byte count)", i, sizes[i], want)
		}
	}
	if got := hex.EncodeToString(digest.Sum(nil)); got != wantSHA256 {
		t.Errorf("stream sha256 = %s, want %s (byte-verified against libvpx vpxenc)", got, wantSHA256)
	}
}
