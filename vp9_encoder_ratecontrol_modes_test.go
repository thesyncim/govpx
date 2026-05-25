package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"
)

func TestVP9EncoderExplicitRateControlModesEncode(t *testing.T) {
	const width, height = 64, 64
	const targetKbps = 300
	const wantBitsPerFrame = targetKbps * 1000 / 30
	cases := []struct {
		name    string
		mode    RateControlMode
		cqLevel int
	}{
		{name: "vbr", mode: RateControlVBR},
		{name: "cq", mode: RateControlCQ, cqLevel: 20},
		{name: "q", mode: RateControlQ, cqLevel: 20},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, err := NewVP9Encoder(VP9EncoderOptions{
				Width:              width,
				Height:             height,
				FPS:                30,
				TargetBitrateKbps:  targetKbps,
				RateControlModeSet: true,
				RateControlMode:    tc.mode,
				CQLevel:            tc.cqLevel,
			})
			if err != nil {
				t.Fatalf("NewVP9Encoder: %v", err)
			}
			if !e.rc.enabled || e.rc.mode != tc.mode {
				t.Fatalf("rate control state = enabled:%t mode:%d, want true/%d",
					e.rc.enabled, e.rc.mode, tc.mode)
			}
			if e.rc.dropFrameAllowed || e.rc.dropFramesWaterMark != 0 {
				t.Fatalf("non-CBR drop state = allowed:%t watermark:%d, want disabled",
					e.rc.dropFrameAllowed, e.rc.dropFramesWaterMark)
			}

			dst := make([]byte, 65536)
			dec, err := NewVP9Decoder(VP9DecoderOptions{})
			if err != nil {
				t.Fatalf("NewVP9Decoder: %v", err)
			}
			for frame := range 2 {
				src := vp9test.NewYCbCr(width, height,
					uint8(96+frame*20), 128, 128)
				result, err := e.EncodeIntoWithResult(src, dst)
				if err != nil {
					t.Fatalf("EncodeIntoWithResult frame %d: %v", frame, err)
				}
				if result.Dropped || len(result.Data) == 0 {
					t.Fatalf("frame %d result = dropped:%t bytes:%d, want packet",
						frame, result.Dropped, len(result.Data))
				}
				wantFrameTargetBits := wantBitsPerFrame
				if frame == 0 {
					wantFrameTargetBits = e.rc.onePassVBRKeyFrameTargetBits()
				} else {
					wantFrameTargetBits = e.rc.onePassVBRInterFrameTargetBits(
						1 << vp9LastRefSlot)
				}
				if result.TargetBitrateKbps != targetKbps ||
					result.FrameTargetBits != wantFrameTargetBits {
					t.Fatalf("frame %d rate = kbps:%d target:%d, want %d/%d",
						frame, result.TargetBitrateKbps, result.FrameTargetBits,
						targetKbps, wantFrameTargetBits)
				}
				if frame == 0 && !result.KeyFrame {
					t.Fatal("first explicit rate-control packet is not a keyframe")
				}
				if frame == 1 && result.KeyFrame {
					t.Fatal("second explicit rate-control packet unexpectedly keyframed")
				}
				if err := dec.Decode(result.Data); err != nil {
					t.Fatalf("Decode frame %d: %v", frame, err)
				}
				if _, ok := dec.NextFrame(); !ok {
					t.Fatalf("NextFrame frame %d returned !ok", frame)
				}
			}
		})
	}
}

func TestVP9EncoderExplicitVBRUsesOnePassRateQuantizer(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		TargetBitrateKbps:   700,
		RateControlModeSet:  true,
		RateControlMode:     RateControlVBR,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		MaxKeyframeInterval: 128,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if e.rc.avgFrameQIndexKey != 120 || e.rc.avgFrameQIndexInter != 120 {
		t.Fatalf("initial VBR average q = key:%d inter:%d, want midpoint 120/120",
			e.rc.avgFrameQIndexKey, e.rc.avgFrameQIndexInter)
	}
	dst := make([]byte, 65536)
	key, err := e.EncodeIntoWithResult(vp9test.NewYCbCr(width, height,
		96, 128, 128), dst)
	if err != nil {
		t.Fatalf("Encode key: %v", err)
	}
	if key.InternalQuantizer >= vp9DefaultBaseQIndex {
		t.Fatalf("VBR key qindex = %d, want below public-Q key qindex %d",
			key.InternalQuantizer, vp9DefaultBaseQIndex)
	}
	if key.FrameTargetBits != e.rc.onePassVBRKeyFrameTargetBits() {
		t.Fatalf("VBR key target = %d, want one-pass target %d",
			key.FrameTargetBits, e.rc.onePassVBRKeyFrameTargetBits())
	}
	inter, err := e.EncodeIntoWithResult(vp9test.NewYCbCr(width, height,
		116, 128, 128), dst)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	if inter.InternalQuantizer == vp9DefaultInterBaseQIndex {
		t.Fatalf("VBR inter qindex = %d, still public-Q inter default",
			inter.InternalQuantizer)
	}
}
