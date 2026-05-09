package govpx

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unsafe"
)

// TestEncoderResetMatchesColdStart pins the contract that VP8Encoder.Reset()
// returns the encoder to a NewVP8Encoder-equivalent cold-start state, with
// the same per-MB scratch buffers re-zeroed and every rate-control / picker
// scalar restored to its constructor seed.
//
// Regression for parity-close-r15-e: prior to R15-E, Reset() left a hand-
// curated subset of fields untouched (rc.kfOverspendBits, the inter-RD
// threshold-cache snapshots, the per-reference probabilities, etc.). The
// bench harness's warmup pass (encode-then-Reset before the timed pass)
// inherited those warmed values into the timed run, driving govpx 7% kbps
// below stock libvpx at 320x240 with no visible quality difference. The
// fresh-vs-reset state diff under realtime-CBR is the load-bearing signal.
func TestEncoderResetMatchesColdStart(t *testing.T) {
	const (
		W, H, FPS, KBPS, F = 320, 240, 30, 1200, 30
	)
	mkOpts := func() EncoderOptions {
		return EncoderOptions{
			Width: W, Height: H, FPS: FPS,
			RateControlMode:                RateControlCBR,
			TargetBitrateKbps:              KBPS,
			MinQuantizer:                   4,
			MaxQuantizer:                   56,
			Deadline:                       DeadlineRealtime,
			CpuUsed:                        8,
			KeyFrameInterval:               FPS,
			BufferSizeMs:                   600,
			BufferInitialSizeMs:            400,
			BufferOptimalSizeMs:            500,
			UndershootPct:                  100,
			OvershootPct:                   15,
			Threads:                        1,
			AutoSpeedGoOverheadCalibration: true,
		}
	}
	encA, err := NewVP8Encoder(mkOpts())
	if err != nil {
		t.Fatal(err)
	}
	defer encA.Close()
	encB, err := NewVP8Encoder(mkOpts())
	if err != nil {
		t.Fatal(err)
	}
	defer encB.Close()

	pkt := make([]byte, W*H*6)
	for i := range F {
		img := resetParityFrame(W, H, i)
		if _, err := encB.EncodeInto(pkt, img, uint64(i), 1, 0); err != nil {
			t.Fatal(err)
		}
	}
	encB.Reset()

	if diffs := encoderStateDiffs(encA, encB); len(diffs) > 0 {
		t.Errorf("Reset() leaked %d field(s) from a warm encoder:\n%s",
			len(diffs), strings.Join(diffs, "\n"))
	}
}

// TestEncoderResetCBRBytesMatchColdStart pins the bench-relevant contract:
// after a warmup pass + Reset, the next 30-frame CBR encode produces a
// byte-stream byte-identical to a cold-start encoder fed the same input.
// Without R15-E's Reset overhaul this drifted ~7% lower at 320x240.
func TestEncoderResetCBRBytesMatchColdStart(t *testing.T) {
	const (
		W, H, FPS, KBPS, F = 320, 240, 30, 1200, 30
	)
	mkOpts := func() EncoderOptions {
		return EncoderOptions{
			Width: W, Height: H, FPS: FPS,
			RateControlMode:                RateControlCBR,
			TargetBitrateKbps:              KBPS,
			MinQuantizer:                   4,
			MaxQuantizer:                   56,
			Deadline:                       DeadlineRealtime,
			CpuUsed:                        8,
			KeyFrameInterval:               FPS,
			BufferSizeMs:                   600,
			BufferInitialSizeMs:            400,
			BufferOptimalSizeMs:            500,
			UndershootPct:                  100,
			OvershootPct:                   15,
			Threads:                        1,
			AutoSpeedGoOverheadCalibration: true,
		}
	}
	encA, err := NewVP8Encoder(mkOpts())
	if err != nil {
		t.Fatal(err)
	}
	defer encA.Close()
	encB, err := NewVP8Encoder(mkOpts())
	if err != nil {
		t.Fatal(err)
	}
	defer encB.Close()

	pkt := make([]byte, W*H*6)
	for i := range F {
		img := resetParityFrame(W, H, i)
		if _, err := encB.EncodeInto(pkt, img, uint64(i), 1, 0); err != nil {
			t.Fatal(err)
		}
	}
	encB.Reset()

	pkt1 := make([]byte, W*H*6)
	pkt2 := make([]byte, W*H*6)
	for i := range F {
		img := resetParityFrame(W, H, i)
		rA, err := encA.EncodeInto(pkt1, img, uint64(i), 1, 0)
		if err != nil {
			t.Fatal(err)
		}
		rB, err := encB.EncodeInto(pkt2, img, uint64(i), 1, 0)
		if err != nil {
			t.Fatal(err)
		}
		if rA.SizeBytes != rB.SizeBytes {
			t.Errorf("frame %d size: cold=%d reset=%d", i, rA.SizeBytes, rB.SizeBytes)
			continue
		}
		if rA.Quantizer != rB.Quantizer {
			t.Errorf("frame %d Q: cold=%d reset=%d", i, rA.Quantizer, rB.Quantizer)
		}
	}
}

func resetParityFrame(width, height, idx int) Image {
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

// encoderStateDiffs returns a list of "field=fresh:reset" diff descriptors
// for every encoder field where a freshly constructed encoder a differs from
// the post-Reset encoder b. Frame-buffer slabs and the lookahead ring are
// skipped (their backing arrays grow during use; content is cleared
// elsewhere).
func encoderStateDiffs(a, b *VP8Encoder) []string {
	va := reflect.ValueOf(a).Elem()
	vb := reflect.ValueOf(b).Elem()
	ty := va.Type()
	var diffs []string
	for i := 0; i < va.NumField(); i++ {
		fname := ty.Field(i).Name
		switch fname {
		case "current", "analysis", "lastRef", "goldenRef", "altRef", "preprocess",
			"loopFilterPick", "arnrScratch", "arnrLastSource",
			"firstPassLastRef", "firstPassGoldenRef", "firstPassLastSource", "firstPassNewRef",
			"lookahead":
			continue
		}
		fa := va.Field(i)
		fb := vb.Field(i)
		fa2 := reflect.NewAt(fa.Type(), unsafe.Pointer(fa.UnsafeAddr())).Elem()
		fb2 := reflect.NewAt(fb.Type(), unsafe.Pointer(fb.UnsafeAddr())).Elem()
		if reflect.DeepEqual(fa2.Interface(), fb2.Interface()) {
			continue
		}
		if fname == "rc" {
			tya := fa2.Type()
			for j := 0; j < fa2.NumField(); j++ {
				sub := tya.Field(j).Name
				sa := fa2.Field(j)
				sb := fb2.Field(j)
				sa2 := reflect.NewAt(sa.Type(), unsafe.Pointer(sa.UnsafeAddr())).Elem()
				sb2 := reflect.NewAt(sb.Type(), unsafe.Pointer(sb.UnsafeAddr())).Elem()
				if !reflect.DeepEqual(sa2.Interface(), sb2.Interface()) {
					diffs = append(diffs, fmt.Sprintf("rc.%s: cold=%v reset=%v",
						sub, abridge(sa2.Interface()), abridge(sb2.Interface())))
				}
			}
			continue
		}
		diffs = append(diffs, fmt.Sprintf("%s: cold=%v reset=%v",
			fname, abridge(fa2.Interface()), abridge(fb2.Interface())))
	}
	return diffs
}

func abridge(v any) string {
	s := fmt.Sprintf("%v", v)
	if len(s) > 80 {
		return s[:80] + "..."
	}
	return s
}
