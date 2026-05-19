package govpx

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/decoder"
)

// TestSetScalingModeValidationAndForceKey mirrors libvpx's
// vp8e_set_scalemode contract (vp8/vp8_cx_iface.c:1295-1316): valid mode
// pairs from VP8E_NORMAL..VP8E_ONETWO are accepted, out-of-range pairs
// are rejected, and a successful call forces the next frame to be a key
// frame (libvpx sets next_frame_flag |= FRAMEFLAGS_KEY).
func TestSetScalingModeValidationAndForceKey(t *testing.T) {
	e := newTestEncoder(t)

	// libvpx rejects out-of-range modes via vp8_set_internal_size's
	// (horiz_mode <= VP8E_ONETWO) check; mirror with ErrInvalidConfig.
	for _, bad := range []ScalingMode{-1, ScalingMode(4), ScalingMode(7)} {
		if err := e.SetScalingMode(bad, ScalingNormal); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("SetScalingMode(%v, Normal) error = %v, want ErrInvalidConfig", bad, err)
		}
		if err := e.SetScalingMode(ScalingNormal, bad); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("SetScalingMode(Normal, %v) error = %v, want ErrInvalidConfig", bad, err)
		}
	}

	for _, mode := range []ScalingMode{ScalingNormal, ScalingFourFive, ScalingThreeFive, ScalingOneTwo} {
		e.forceKeyFrame = false
		if err := e.SetScalingMode(mode, mode); err != nil {
			t.Fatalf("SetScalingMode(%v, %v) error = %v", mode, mode, err)
		}
		if !e.forceKeyFrame {
			t.Fatalf("SetScalingMode(%v, %v) did not set forceKeyFrame", mode, mode)
		}
		if e.horizScale != uint8(mode) || e.vertScale != uint8(mode) {
			t.Fatalf("SetScalingMode(%v, %v) state = (%d, %d), want (%d, %d)",
				mode, mode, e.horizScale, e.vertScale, mode, mode)
		}
	}
}

// TestSetScalingModeRejectsClosedEncoder asserts the setter fails closed
// on a nil or post-Close encoder, matching libvpx's invalid-context
// rejection.
func TestSetScalingModeRejectsClosedEncoder(t *testing.T) {
	var nilEnc *VP8Encoder
	if err := nilEnc.SetScalingMode(ScalingNormal, ScalingNormal); !errors.Is(err, ErrClosed) {
		t.Fatalf("nil encoder SetScalingMode error = %v, want ErrClosed", err)
	}

	e := newTestEncoder(t)
	e.Close()
	if err := e.SetScalingMode(ScalingOneTwo, ScalingOneTwo); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed encoder SetScalingMode error = %v, want ErrClosed", err)
	}
}

// TestSetScalingModeKeyframeCarriesScaleBits asserts that after
// SetScalingMode, the next emitted key frame's uncompressed-data chunk
// carries the configured 2-bit horizontal and vertical scale fields per
// RFC 6386 §9.1. The decoder is the authoritative reader.
func TestSetScalingModeKeyframeCarriesScaleBits(t *testing.T) {
	cases := []struct {
		horiz, vert         ScalingMode
		wantHoriz, wantVert int
	}{
		{ScalingNormal, ScalingNormal, 0, 0},
		{ScalingFourFive, ScalingFourFive, 1, 1},
		{ScalingThreeFive, ScalingThreeFive, 2, 2},
		{ScalingOneTwo, ScalingOneTwo, 3, 3},
		{ScalingFourFive, ScalingOneTwo, 1, 3},
		{ScalingNormal, ScalingThreeFive, 0, 2},
	}
	for _, tc := range cases {
		e := newTestEncoder(t)
		if err := e.SetScalingMode(tc.horiz, tc.vert); err != nil {
			t.Fatalf("SetScalingMode(%v, %v) error = %v", tc.horiz, tc.vert, err)
		}
		src := testImage(16, 16)
		dst := make([]byte, 8192)
		result, err := e.EncodeInto(dst, src, 0, 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto(%v, %v) error = %v", tc.horiz, tc.vert, err)
		}
		if !result.KeyFrame {
			t.Fatalf("after SetScalingMode(%v, %v) first frame not a key frame", tc.horiz, tc.vert)
		}
		header, err := decoder.ParseFrameHeader(result.Data)
		if err != nil {
			t.Fatalf("ParseFrameHeader error = %v", err)
		}
		if header.HorizScale != tc.wantHoriz {
			t.Fatalf("horiz scale bits = %d, want %d", header.HorizScale, tc.wantHoriz)
		}
		if header.VertScale != tc.wantVert {
			t.Fatalf("vert scale bits = %d, want %d", header.VertScale, tc.wantVert)
		}
		e.Close()
	}
}
