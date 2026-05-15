package govpx

import (
	"testing"

	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

// TestSuppressInterFrameCopyBuffersOnAltRefEdgesClearsCopyBuffer mirrors
// libvpx vp8/encoder/onyx_if.c update_reference_frames asserts:
//
//   - assert(!cm->copy_buffer_to_arf) when cm->refresh_alt_ref_frame is
//     set (hidden ARF frames are populating the ARF buffer themselves).
//   - source-alt-ref overlay frames are not suppressed; libvpx still copies
//     old GOLDEN to ALTREF when the overlay refreshes GOLDEN.
func TestSuppressInterFrameCopyBuffersOnAltRefEdgesClearsCopyBuffer(t *testing.T) {
	t.Run("hidden ARF clears CopyBufferToAltRef", func(t *testing.T) {
		cfg := vp8enc.InterFrameStateConfig{
			RefreshAltRef:      true,
			CopyBufferToAltRef: 2,
			CopyBufferToGolden: 0,
		}
		suppressInterFrameCopyBuffersOnAltRefEdges(&cfg, false)
		if cfg.CopyBufferToAltRef != 0 {
			t.Fatalf("CopyBufferToAltRef = %d, want 0 for hidden ARF (libvpx assert(!cm->copy_buffer_to_arf))", cfg.CopyBufferToAltRef)
		}
	})

	t.Run("deferred show-frame after hidden ARF preserves copy fields", func(t *testing.T) {
		cfg := vp8enc.InterFrameStateConfig{
			CopyBufferToAltRef: 2,
			CopyBufferToGolden: 1,
		}
		suppressInterFrameCopyBuffersOnAltRefEdges(&cfg, true)
		if cfg.CopyBufferToAltRef != 2 {
			t.Fatalf("CopyBufferToAltRef = %d, want 2 preserved for is_src_frame_alt_ref show-frame", cfg.CopyBufferToAltRef)
		}
		if cfg.CopyBufferToGolden != 1 {
			t.Fatalf("CopyBufferToGolden = %d, want 1 preserved for is_src_frame_alt_ref show-frame", cfg.CopyBufferToGolden)
		}
	})

	t.Run("nil cfg is a no-op", func(t *testing.T) {
		// Defensive guard: callers should not crash if cfg is nil.
		suppressInterFrameCopyBuffersOnAltRefEdges(nil, true)
	})

	t.Run("plain inter-frame keeps copy fields", func(t *testing.T) {
		cfg := vp8enc.InterFrameStateConfig{
			RefreshGolden:      true,
			CopyBufferToAltRef: 2,
		}
		suppressInterFrameCopyBuffersOnAltRefEdges(&cfg, false)
		if cfg.CopyBufferToAltRef != 2 {
			t.Fatalf("CopyBufferToAltRef = %d, want 2 preserved for libvpx CBR golden refresh", cfg.CopyBufferToAltRef)
		}
	})
}

// TestEncodeInterFrameAttemptSuppressesAltRefCopyBufferOnHiddenARF drives a
// small auto-ARF-shaped sequence through the inter-frame encode path and
// verifies that hidden ARF frames have CopyBufferToAltRef == 0, mirroring
// libvpx update_reference_frames's assert(!cm->copy_buffer_to_arf).
func TestEncodeInterFrameAttemptSuppressesAltRefCopyBufferOnHiddenARF(t *testing.T) {
	e := newTestEncoder(t)
	e.opts.ErrorResilient = false

	// Seed the auto-ARF schedule the way libvpx onyx_if.c does just
	// before emitting the hidden ARF: source_alt_ref_pending becomes
	// true and the alt-ref source PTS is recorded for is_src_frame_alt_ref.
	const altRefPTS uint64 = 4
	e.scheduleAltRefSource(altRefPTS, 0)
	e.currentSourcePTS = altRefPTS

	dst := make([]byte, 1<<15)
	rows := encoderMacroblockRows(e.opts.Height)
	cols := encoderMacroblockCols(e.opts.Width)
	required := rows * cols

	// Hidden ARF: invisible frame that forces an ARF refresh.
	flags := EncodeForceAltRefFrame | EncodeInvisibleFrame
	attempt, err := e.encodeInterFrameAttempt(dst, sourceImageFromImage(testImage(e.opts.Width, e.opts.Height)), rows, cols, required, flags, false, false, false, false, e.rc.currentQuantizer, true, false)
	if err != nil {
		t.Fatalf("encodeInterFrameAttempt(hidden ARF) returned error: %v", err)
	}
	if !attempt.Config.RefreshAltRef {
		t.Fatalf("RefreshAltRef = false, want true for hidden ARF frame")
	}
	if attempt.Config.CopyBufferToAltRef != 0 {
		t.Fatalf("CopyBufferToAltRef = %d, want 0 for hidden ARF (libvpx assert(!cm->copy_buffer_to_arf))", attempt.Config.CopyBufferToAltRef)
	}
	if attempt.Config.CopyBufferToGolden != 0 {
		t.Fatalf("CopyBufferToGolden = %d, want 0 for hidden ARF (libvpx never sets copy_buffer_to_gf)", attempt.Config.CopyBufferToGolden)
	}
}

// TestEncodeInterFrameAttemptPreservesAltRefCopyOnDeferredShowFrame verifies
// that when the current source matches the previously scheduled alt-ref
// source (libvpx is_src_frame_alt_ref=1), a CBR golden refresh still copies
// the old GOLDEN buffer to ALTREF before refreshing GOLDEN.
func TestEncodeInterFrameAttemptPreservesAltRefCopyOnDeferredShowFrame(t *testing.T) {
	e := newTestEncoder(t)
	e.opts.ErrorResilient = false

	const altRefPTS uint64 = 7
	e.scheduleAltRefSource(altRefPTS, 0)
	// Walk through the libvpx update_alt_ref_frame_stats lifecycle: after
	// emitting the hidden ARF the encoder marks source_alt_ref_active
	// and arms the next source as the deferred show frame.
	e.sourceAltRefActive = true
	e.sourceAltRefPending = false
	// Deferred show frame: same PTS as the scheduled ARF source.
	e.currentSourcePTS = altRefPTS

	dst := make([]byte, 1<<15)
	rows := encoderMacroblockRows(e.opts.Height)
	cols := encoderMacroblockCols(e.opts.Width)
	required := rows * cols

	// Drive a CBR golden refresh on the deferred show frame so that the
	// pre-existing CopyBufferToAltRef=2 path fires just like libvpx.
	attempt, err := e.encodeInterFrameAttempt(dst, sourceImageFromImage(testImage(e.opts.Width, e.opts.Height)), rows, cols, required, 0, false, true, false, false, e.rc.currentQuantizer, true, false)
	if err != nil {
		t.Fatalf("encodeInterFrameAttempt(deferred show) returned error: %v", err)
	}
	if attempt.Config.CopyBufferToAltRef != 2 {
		t.Fatalf("CopyBufferToAltRef = %d, want 2 on is_src_frame_alt_ref deferred show frame", attempt.Config.CopyBufferToAltRef)
	}
	if attempt.Config.CopyBufferToGolden != 0 {
		t.Fatalf("CopyBufferToGolden = %d, want 0 because the frame refreshes GOLDEN directly", attempt.Config.CopyBufferToGolden)
	}
}
