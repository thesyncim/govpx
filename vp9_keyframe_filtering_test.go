package govpx

import (
	"bytes"
	"errors"
	"testing"
)

// TestVP9KeyFrameFilteringChangesKeyframeBytes pins the libvpx wiring of
// VP9E_SET_KEY_FRAME_FILTERING (vp9/vp9_cx_iface.c:974) → ctx->extra_cfg.
// enable_keyframe_filtering → cpi->oxcf.enable_keyframe_filtering →
// vp9/encoder/vp9_encoder.c:6347-6364 vp9_temporal_filter(cpi, -1).
//
// When the gates pass, the encoder runs a forward-only temporal filter
// over the keyframe source against the lookahead window before encoding;
// the keyframe bytes diverge from a raw-source encode under the same
// configuration.  This test compares EnableKeyFrameFiltering=true vs
// false on a mixed-motion 8-frame lookahead and asserts the keyframe
// byte payload differs.
func TestVP9KeyFrameFilteringChangesKeyframeBytes(t *testing.T) {
	const w, h = 64, 64
	encode := func(enableKF bool) []byte {
		opts := VP9EncoderOptions{
			Width:                   w,
			Height:                  h,
			FPS:                     30,
			LookaheadFrames:         8,
			AutoAltRef:              true,
			ARNRMaxFrames:           7,
			ARNRStrength:            3,
			ARNRType:                3,
			Deadline:                DeadlineGoodQuality,
			CpuUsed:                 1,
			RateControlModeSet:      true,
			RateControlMode:         RateControlQ,
			TargetBitrateKbps:       1000,
			CQLevel:                 32,
			MaxQuantizer:            63,
			EnableKeyFrameFiltering: enableKF,
		}
		enc, err := NewVP9Encoder(opts)
		if err != nil {
			t.Fatalf("NewVP9Encoder(enableKF=%v): %v", enableKF, err)
		}
		seq := newVP9TPLPanningSequence(w, h, 16)
		buf := make([]byte, 64*1024)
		var keyframeBytes []byte
		drain := func(res VP9EncodeResult) {
			if res.KeyFrame && len(keyframeBytes) == 0 {
				keyframeBytes = append(keyframeBytes, res.Data...)
			}
		}
		for i := range 16 {
			res, err := enc.encodeVP9LookaheadIntoWithFlagsResult(seq[i%len(seq)], buf, 0)
			switch {
			case err == nil:
				drain(res)
			case errors.Is(err, ErrFrameNotReady):
			default:
				t.Fatalf("encode %d: %v", i, err)
			}
		}
		for {
			res, err := enc.FlushIntoWithResult(buf)
			if errors.Is(err, ErrFrameNotReady) {
				break
			}
			if err != nil {
				t.Fatalf("flush: %v", err)
			}
			drain(res)
		}
		return keyframeBytes
	}
	off := encode(false)
	on := encode(true)
	if len(off) == 0 || len(on) == 0 {
		t.Fatalf("missing keyframe bytes off=%d on=%d", len(off), len(on))
	}
	if bytes.Equal(off, on) {
		t.Fatalf("EnableKeyFrameFiltering had no effect on keyframe bytes (size=%d both)",
			len(off))
	}
}

// TestVP9KeyFrameFilteringSetterRoundTrip pins the runtime control wiring:
// SetEnableKeyFrameFiltering toggles e.opts.EnableKeyFrameFiltering and
// allocates the ARNR scratch when both ARNR config and the toggle are
// active.  Mirrors libvpx's ctrl_set_keyframe_filtering
// (vp9/vp9_cx_iface.c:974) which writes through update_extra_cfg.
func TestVP9KeyFrameFilteringSetterRoundTrip(t *testing.T) {
	enc, err := NewVP9Encoder(VP9EncoderOptions{
		Width:           64,
		Height:          64,
		FPS:             30,
		LookaheadFrames: 8,
		AutoAltRef:      true,
		ARNRMaxFrames:   7,
		ARNRStrength:    3,
		ARNRType:        3,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if enc.opts.EnableKeyFrameFiltering {
		t.Fatalf("EnableKeyFrameFiltering default = true, want false")
	}
	if err := enc.SetEnableKeyFrameFiltering(true); err != nil {
		t.Fatalf("SetEnableKeyFrameFiltering(true): %v", err)
	}
	if !enc.opts.EnableKeyFrameFiltering {
		t.Fatalf("SetEnableKeyFrameFiltering(true) did not flip the option")
	}
	if len(enc.vp9ARNRScratch.Y) == 0 {
		t.Fatalf("ARNR scratch not allocated after SetEnableKeyFrameFiltering(true)")
	}
	if err := enc.SetEnableKeyFrameFiltering(false); err != nil {
		t.Fatalf("SetEnableKeyFrameFiltering(false): %v", err)
	}
	if enc.opts.EnableKeyFrameFiltering {
		t.Fatalf("SetEnableKeyFrameFiltering(false) did not clear the option")
	}
}

// TestVP9KeyFrameFilteringGateRespectsLibvpxPreconditions verifies the
// gate helper trips on every libvpx-listed precondition
// (vp9_encoder.c:6347-6353).  Each subtest flips one precondition off and
// asserts the gate returns false.
func TestVP9KeyFrameFilteringGateRespectsLibvpxPreconditions(t *testing.T) {
	base := VP9EncoderOptions{
		Width:                   64,
		Height:                  64,
		FPS:                     30,
		LookaheadFrames:         8,
		AutoAltRef:              true,
		ARNRMaxFrames:           7,
		ARNRStrength:            3,
		ARNRType:                3,
		Deadline:                DeadlineGoodQuality,
		CpuUsed:                 1,
		EnableKeyFrameFiltering: true,
	}
	for _, tc := range []struct {
		name string
		mut  func(*VP9EncoderOptions)
	}{
		{
			name: "Realtime deadline disables",
			mut:  func(o *VP9EncoderOptions) { o.Deadline = DeadlineRealtime },
		},
		{
			name: "Lossless disables",
			mut:  func(o *VP9EncoderOptions) { o.Lossless = true },
		},
		{
			name: "ARNRMaxFrames<=1 disables",
			mut:  func(o *VP9EncoderOptions) { o.ARNRMaxFrames = 1 },
		},
		{
			name: "ARNRStrength==0 disables",
			mut:  func(o *VP9EncoderOptions) { o.ARNRStrength = 0 },
		},
		{
			name: "CpuUsed>=2 disables",
			mut:  func(o *VP9EncoderOptions) { o.CpuUsed = 2 },
		},
		{
			name: "EnableKeyFrameFiltering==false disables",
			mut:  func(o *VP9EncoderOptions) { o.EnableKeyFrameFiltering = false },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := base
			tc.mut(&opts)
			enc, err := NewVP9Encoder(opts)
			if err != nil {
				t.Fatalf("NewVP9Encoder: %v", err)
			}
			if enc.vp9KeyFrameFilteringActive() {
				t.Fatalf("gate returned true with %s", tc.name)
			}
		})
	}
	// Sanity: with the base config (all preconditions satisfied) the gate
	// is active.
	enc, err := NewVP9Encoder(base)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if !enc.vp9KeyFrameFilteringActive() {
		t.Fatalf("gate returned false with the base config (all preconditions hold)")
	}
}
