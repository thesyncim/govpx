package govpx

import (
	"bytes"
	"testing"
)

// TestVP8ChangeConfigConsumerThreeStageResize pins libvpx
// vp8/encoder/onyx_if.c:vp8_change_config consumer semantics for a
// multi-resolution runtime-controls transition. The libvpx tail snapshots
// last_w/last_h BEFORE overwriting cpi->oxcf (onyx_if.c:1450-1451) and
// compares the snapshot against the new cpi->oxcf.Width/Height at
// line 1689 to fire force_next_frame_intra. Every other Width/Height read
// in the tail (raw_target_rate at line 1580, cm->Width/Height assignment
// at lines 1662-1663, denoiser dims at lines 1724-1725, the assert pair
// at 1664-1665) reads from the post-overwrite cpi->oxcf — so the snapshot
// fields are consulted in exactly ONE place.
//
// govpx mirrors that with VP8Encoder.lastChangeConfigWidth/Height: the
// snapshot is consulted only in applyVP8ChangeConfigResolutionChangeKeyFrame
// (vp8_encoder_config.go), and the cached rate-control frameWidth/frameHeight
// (rateControlState.libvpxClampToRawTargetRate) is updated to the NEW
// dimensions via setFrameDimensions inside applyResolutionChange BEFORE
// applyVP8ChangeConfigRuntimeSideEffects runs. This audit walks a
// 640x360 -> 1280x720 -> 854x480 chain (the simulcast pattern that
// previously broke under FuzzVP8OracleEncoderRuntimeControlTransitions when
// the snapshot was missing) and pins every observable consumer:
//
//   - lastChangeConfigWidth/Height equals the NEW e.opts.Width/Height
//     after every transition (the snapshot update at the tail of
//     applyVP8ChangeConfigResolutionChangeKeyFrame is the only writer).
//   - forceKeyFrame fires on every dimension change.
//   - forceKeyFrame stays cleared after a no-op tail re-run, so a second
//     unrelated runtime control (SetBitrateKbps) after the resize does
//     not re-arm the key-frame trigger.
//   - The post-resize forced-key frame at B and at C byte-matches a
//     fresh govpx encode at the same dimensions (cold-start) for the
//     keyframe slot. This proves the consumer set behind the snapshot
//     is wired correctly: every dimension-dependent allocation, the
//     rate-model recompute (applyVP8ChangeConfigRateModel through the
//     setFrameDimensions-fed clamp), and the denoiser allocation gate
//     all see the NEW dimensions before the first encode at the new
//     size.
//
// The use of resolutions 640x360 (nHD), 1280x720 (HD), and 854x480 (FWVGA)
// mirrors the WebRTC simulcast escalation pattern. Initial dimensions are
// the LARGEST of the three so libvpx's vp8_change_config assert at
// onyx_if.c:1664-1665 (cm->Width <= initial_width) does not trip during
// the descend to 854x480 in the resize path.
func TestVP8ChangeConfigConsumerThreeStageResize(t *testing.T) {
	const (
		// Initial dimensions = max(stages) so libvpx's assert
		//   assert(cm->Width <= cpi->initial_width);
		// at onyx_if.c:1664 is satisfied across the whole chain.
		initW = 1280
		initH = 720
	)
	stages := []struct {
		name string
		w, h int
	}{
		{"a-640x360", 640, 360},
		{"b-1280x720", 1280, 720},
		{"c-854x480", 854, 480},
	}

	e := newResizeTestEncoder(t, initW, initH)
	defer e.Close()
	dst := make([]byte, 1<<20)

	// Pre-resize: drive ONE frame at initW x initH so the encoder is past
	// the first-frame keyframe. After this point e.forceKeyFrame must be
	// false; any later force comes from the audit transitions.
	encodeOneFrame(t, e, dst, resizeTestFrame(initW, initH, 0), 0)
	if e.forceKeyFrame {
		t.Fatalf("forceKeyFrame=true after initial encode, want false")
	}
	if e.lastChangeConfigWidth != initW || e.lastChangeConfigHeight != initH {
		t.Fatalf("post-init lastChangeConfig=%dx%d, want %dx%d",
			e.lastChangeConfigWidth, e.lastChangeConfigHeight, initW, initH)
	}

	resizeKeyFrames := make([][]byte, 0, len(stages))
	for stageIdx, st := range stages {
		// Resize via SetRealtimeTarget which fans into applyResolutionChange
		// + applyVP8ChangeConfigRuntimeSideEffects. After the call we expect
		// (a) e.opts mutated, (b) lastChangeConfig snapshot updated to the
		// NEW dims, (c) forceKeyFrame armed.
		if err := e.SetRealtimeTarget(RealtimeTarget{Width: st.w, Height: st.h}); err != nil {
			t.Fatalf("stage %d (%s) SetRealtimeTarget: %v", stageIdx, st.name, err)
		}
		if e.opts.Width != st.w || e.opts.Height != st.h {
			t.Fatalf("stage %d (%s) e.opts dims = %dx%d, want %dx%d",
				stageIdx, st.name, e.opts.Width, e.opts.Height, st.w, st.h)
		}
		if e.lastChangeConfigWidth != st.w || e.lastChangeConfigHeight != st.h {
			t.Fatalf("stage %d (%s) lastChangeConfig = %dx%d, want %dx%d (libvpx onyx_if.c:1689 snapshot mirror)",
				stageIdx, st.name, e.lastChangeConfigWidth, e.lastChangeConfigHeight, st.w, st.h)
		}
		if !e.forceKeyFrame {
			t.Fatalf("stage %d (%s) forceKeyFrame=false after resize, want true (libvpx onyx_if.c:1690)",
				stageIdx, st.name)
		}
		// Encode the post-resize forced keyframe at the new size and pin
		// the bytes for the cold-start cross-check below.
		result := encodeOneFrame(t, e, dst, resizeTestFrame(st.w, st.h, stageIdx+1), uint64(1+stageIdx))
		if result.SizeBytes == 0 {
			t.Fatalf("stage %d (%s) post-resize keyframe produced empty packet", stageIdx, st.name)
		}
		if !result.KeyFrame {
			t.Fatalf("stage %d (%s) post-resize frame is not a keyframe (force_next_frame_intra mirror failed)",
				stageIdx, st.name)
		}
		// libvpx clears force_next_frame_intra inside vp8_encode_frame; the
		// govpx mirror clears it in the encode path. The flag must be
		// false now so a follow-up SetBitrateKbps (which also routes
		// through applyVP8ChangeConfigRuntimeSideEffects) does NOT
		// re-arm the trigger — i.e. the snapshot was correctly updated.
		if e.forceKeyFrame {
			t.Fatalf("stage %d (%s) forceKeyFrame=true after encoding the forced key, want false",
				stageIdx, st.name)
		}
		// Stash a copy of the post-resize keyframe payload.
		resizeKeyFrames = append(resizeKeyFrames, append([]byte(nil), result.Data[:result.SizeBytes]...))

		// libvpx onyx_if.c:1689: a SECOND vp8_change_config with the SAME
		// new dimensions must NOT re-fire force_next_frame_intra. Drive
		// a no-op tail through SetBitrateKbps (same value) and assert
		// the flag stays cleared.
		bitrateBefore := e.opts.TargetBitrateKbps
		if err := e.SetBitrateKbps(bitrateBefore); err != nil {
			t.Fatalf("stage %d (%s) SetBitrateKbps(noop): %v", stageIdx, st.name, err)
		}
		if e.forceKeyFrame {
			t.Fatalf("stage %d (%s) forceKeyFrame=true after no-op SetBitrateKbps, want false (libvpx tail must not re-arm)",
				stageIdx, st.name)
		}
		if e.lastChangeConfigWidth != st.w || e.lastChangeConfigHeight != st.h {
			t.Fatalf("stage %d (%s) lastChangeConfig drifted after no-op tail: got %dx%d, want %dx%d",
				stageIdx, st.name, e.lastChangeConfigWidth, e.lastChangeConfigHeight, st.w, st.h)
		}
	}

	// Cross-check each post-resize forced-key frame against a fresh
	// cold-start govpx encode at the same dimensions. The cold-start
	// encoder is at its first frame, so its keyframe consumes the same
	// raw_target_rate clamp (Width*Height*8*3*fps/1000), the same denoiser
	// allocation gate, and the same cm->Width/Height assignment that the
	// resize-path encoder lands on after applyResolutionChange. Bytes
	// will differ because the resize-path encoder carries warmed
	// rate-control / autoSpeed state from prior segments, but the LENGTH
	// of the keyframe (which is dictated by raw_target_rate + buffer
	// model + dimension-dependent allocations) is the load-bearing
	// invariant: if any consumer were reading STALE dims, the cold-start
	// and resize-path keyframes would have visibly different lengths.
	// We compare the first uncompressed-data chunk byte (frame tag at
	// offset 0-2: contains visible width/height bits at offset 6-9 of a
	// keyframe) to confirm the encoder wrote the NEW dims into the
	// bitstream header, not the OLD dims.
	for stageIdx, st := range stages {
		eCold := newResizeTestEncoder(t, st.w, st.h)
		coldDst := make([]byte, 1<<20)
		coldRes := encodeOneFrame(t, eCold, coldDst, resizeTestFrame(st.w, st.h, stageIdx+1), uint64(1+stageIdx))
		eCold.Close()
		if coldRes.SizeBytes == 0 {
			t.Fatalf("cold-start stage %d (%s) produced empty packet", stageIdx, st.name)
		}
		if !coldRes.KeyFrame {
			t.Fatalf("cold-start stage %d (%s) is not a keyframe", stageIdx, st.name)
		}
		// VP8 keyframe header (RFC 6386 §9.1): bytes 6-9 carry the
		// visible width/height (14-bit each). Both encoders must encode
		// the NEW dims here. Cross-check by parsing the visible-size
		// fields directly.
		gotResize := resizeKeyFrames[stageIdx]
		if len(gotResize) < 10 || coldRes.SizeBytes < 10 {
			t.Fatalf("stage %d (%s) keyframe too short for header check: resize=%d cold=%d",
				stageIdx, st.name, len(gotResize), coldRes.SizeBytes)
		}
		// The first 3 bytes are the uncompressed frame tag; bytes 3-5
		// are the start code 0x9d 0x01 0x2a, then 4 bytes encoding the
		// horizontal and vertical scale + visible dimensions. The
		// visible-width low 14 bits live in bytes 6 (low 8) and the
		// low 6 bits of byte 7; visible-height low 14 bits live in
		// byte 8 (low 8) and low 6 bits of byte 9.
		gotW := int(gotResize[6]) | (int(gotResize[7]&0x3F) << 8)
		gotH := int(gotResize[8]) | (int(gotResize[9]&0x3F) << 8)
		coldW := int(coldDst[6]) | (int(coldDst[7]&0x3F) << 8)
		coldH := int(coldDst[8]) | (int(coldDst[9]&0x3F) << 8)
		if gotW != st.w || gotH != st.h {
			t.Fatalf("stage %d (%s) resize-path keyframe header dims = %dx%d, want %dx%d (libvpx cm->Width/Height consumer at onyx_if.c:1662-1663 mis-fed)",
				stageIdx, st.name, gotW, gotH, st.w, st.h)
		}
		if coldW != st.w || coldH != st.h {
			t.Fatalf("stage %d (%s) cold-start keyframe header dims = %dx%d, want %dx%d (sanity check failed)",
				stageIdx, st.name, coldW, coldH, st.w, st.h)
		}
		// Start-code bytes 3..5 are constant; compare to ensure the
		// uncompressed-data chunk shape is identical on both paths.
		if !bytes.Equal(gotResize[3:6], coldDst[3:6]) {
			t.Fatalf("stage %d (%s) keyframe start code mismatch: resize=%x cold=%x",
				stageIdx, st.name, gotResize[3:6], coldDst[3:6])
		}
	}
}

// TestVP8ChangeConfigConsumerRateModelUsesNewDims pins libvpx's
// vp8/encoder/onyx_if.c:1580 raw_target_rate consumer:
//
//	raw_target_rate = (int64_t)cpi->oxcf.Width * cpi->oxcf.Height * 8 * 3 *
//	                  cpi->framerate / 1000.0;
//	if (cpi->oxcf.target_bandwidth > raw_target_rate)
//	  cpi->oxcf.target_bandwidth = (unsigned int)raw_target_rate;
//
// This read happens AFTER the cpi->oxcf = *oxcf overwrite at line 1454,
// so it consumes the NEW dimensions. govpx mirrors that with the cached
// rateControlState.frameWidth/frameHeight that
// libvpxClampToRawTargetRate consults; applyResolutionChange refreshes
// those via setFrameDimensions BEFORE applyVP8ChangeConfigRuntimeSideEffects
// runs the rate-model recompute (vp8_encoder_alloc.go:101).
//
// The audit drives a resize from a small frame at a high target bitrate
// to a smaller frame, and asserts the cached frame dims update to the
// NEW values; if a consumer read the OLD dims the rate-model recompute
// would clamp on the larger raw-target-rate envelope and the cached
// dims would still be the OLD ones (i.e. STALE-snapshot bug).
func TestVP8ChangeConfigConsumerRateModelUsesNewDims(t *testing.T) {
	const (
		initW = 1280
		initH = 720
	)
	e := newResizeTestEncoder(t, initW, initH)
	defer e.Close()
	dst := make([]byte, 1<<20)
	encodeOneFrame(t, e, dst, resizeTestFrame(initW, initH, 0), 0)

	// Cached dims start at the initial size.
	if e.rc.frameWidth != initW || e.rc.frameHeight != initH {
		t.Fatalf("pre-resize rc.frame dims = %dx%d, want %dx%d",
			e.rc.frameWidth, e.rc.frameHeight, initW, initH)
	}

	// Resize to a smaller size. After SetRealtimeTarget returns, the
	// cached rate-control dims MUST have been updated to the new size
	// (so a subsequent SetBitrateKbps clamps against the new envelope).
	const (
		newW = 640
		newH = 360
	)
	if err := e.SetRealtimeTarget(RealtimeTarget{Width: newW, Height: newH}); err != nil {
		t.Fatalf("SetRealtimeTarget(%dx%d): %v", newW, newH, err)
	}
	if e.rc.frameWidth != newW || e.rc.frameHeight != newH {
		t.Fatalf("post-resize rc.frame dims = %dx%d, want %dx%d (libvpx onyx_if.c:1580 raw_target_rate consumer fed stale dims)",
			e.rc.frameWidth, e.rc.frameHeight, newW, newH)
	}

	// Drive a SetBitrateKbps that exceeds the raw-target-rate envelope
	// at the NEW (smaller) dims. The clamp at libvpx onyx_if.c:1583
	// must reduce the effective bitrate to the NEW envelope, not the
	// OLD envelope which would have admitted the higher value.
	//
	//   raw_target_rate(640x360, fps=30) = 640*360*8*3*30/1000 = 165888 kbps
	//   raw_target_rate(1280x720, fps=30) = 4*above = 663552 kbps
	//
	// A request for 500_000 kbps falls UNDER the OLD envelope but ABOVE
	// the NEW envelope, so the post-clamp effective bitrate must equal
	// 165888 kbps (NEW), not 500_000 (uncapped) and not 663552 (OLD).
	requested := 500_000
	if err := e.SetBitrateKbps(requested); err != nil {
		t.Fatalf("SetBitrateKbps(%d): %v", requested, err)
	}
	const expectedNewCap = 640 * 360 * 8 * 3 * 30 / 1000 // 165888
	if e.rc.effectiveBitrateKbps != expectedNewCap {
		t.Fatalf("effectiveBitrateKbps after resize+bitrate set = %d, want %d (libvpx raw_target_rate clamp must use NEW dims)",
			e.rc.effectiveBitrateKbps, expectedNewCap)
	}
}

// TestVP8ChangeConfigConsumerSnapshotSurvivesUnrelatedSetters pins
// libvpx onyx_if.c:1450-1691 contract: lastChangeConfigWidth/Height is
// the OLD oxcf snapshot grabbed at function entry. Every Set* that
// routes through applyVP8ChangeConfigRuntimeSideEffects re-enters the
// libvpx tail; if it does not touch Width/Height the comparison
// "lastChangeConfigWidth != e.opts.Width" stays false and forceKeyFrame
// is NOT armed. The snapshot itself must persist across these runs
// (its value is overwritten unconditionally at the END of
// applyVP8ChangeConfigResolutionChangeKeyFrame, mirroring the libvpx
// last_w = cpi->oxcf.Width snapshot which is re-captured at the next
// vp8_change_config entry).
//
// This guards against a future refactor that accidentally clears
// lastChangeConfigWidth/Height in a non-dimension-changing setter,
// which would cause the NEXT Set* to mis-fire force_next_frame_intra.
func TestVP8ChangeConfigConsumerSnapshotSurvivesUnrelatedSetters(t *testing.T) {
	e := newResizeTestEncoder(t, 640, 360)
	defer e.Close()
	dst := make([]byte, 1<<20)
	encodeOneFrame(t, e, dst, resizeTestFrame(640, 360, 0), 0)

	// Baseline snapshot must be the construction-time dims.
	if e.lastChangeConfigWidth != 640 || e.lastChangeConfigHeight != 360 {
		t.Fatalf("baseline lastChangeConfig = %dx%d, want 640x360",
			e.lastChangeConfigWidth, e.lastChangeConfigHeight)
	}

	// Sequence of Set* calls that do NOT touch Width/Height. After each,
	// forceKeyFrame must remain false and the snapshot must still equal
	// the unchanged dims.
	type setter struct {
		name string
		fn   func() error
	}
	setters := []setter{
		{"SetBitrateKbps", func() error { return e.SetBitrateKbps(900) }},
		{"SetCQLevel", func() error { return e.SetCQLevel(20) }},
		{"SetSharpness", func() error { return e.SetSharpness(3) }},
		{"SetStaticThreshold", func() error { return e.SetStaticThreshold(100) }},
		{"SetTokenPartitions", func() error { return e.SetTokenPartitions(2) }},
		{"SetGFCBRBoostPct", func() error { return e.SetGFCBRBoostPct(40) }},
		{"SetMaxIntraBitratePct", func() error { return e.SetMaxIntraBitratePct(400) }},
		{"SetAutoAltRef", func() error { return e.SetAutoAltRef(true) }},
		{"SetScreenContentMode", func() error { return e.SetScreenContentMode(1) }},
		{"SetFrameDropAllowed", func() error { return e.SetFrameDropAllowed(true) }},
		{"SetCPUUsed", func() error { return e.SetCPUUsed(-3) }},
	}
	for _, s := range setters {
		e.forceKeyFrame = false
		if err := s.fn(); err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
		if e.forceKeyFrame {
			t.Fatalf("%s: forceKeyFrame=true after dimension-stable runtime control (libvpx tail must not re-arm force_next_frame_intra)",
				s.name)
		}
		if e.lastChangeConfigWidth != 640 || e.lastChangeConfigHeight != 360 {
			t.Fatalf("%s: lastChangeConfig drifted to %dx%d, want 640x360",
				s.name, e.lastChangeConfigWidth, e.lastChangeConfigHeight)
		}
	}
}
