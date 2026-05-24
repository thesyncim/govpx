package govpx

import (
	"errors"
	"testing"
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
