package govpx

import (
	"errors"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"testing"
)

func TestEncodeIntoAltRefSignBiasFollowsLibvpxSourceAltRefActive(t *testing.T) {
	// AutoAltRef mirrors libvpx oxcf.play_alternate; without it the
	// vp8/encoder/onyx_if.c:4724-4732 dispatcher routes refresh_alt_ref_frame=1
	// through update_golden_frame_stats (which leaves source_alt_ref_active=0),
	// so the ALTREF sign-bias activation seen here only fires when AutoAltRef
	// is set. The frame-flags byte-parity oracle covers the no-AutoAltRef path.
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		Deadline:            DeadlineRealtime,
		CpuUsed:             8,
		KeyFrameInterval:    120,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		AutoAltRef:          true,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	keySrc := testImage(16, 16)
	altSrc := testImage(16, 16)
	interSrc := testImage(16, 16)
	fillImage(keySrc, 220, 90, 170)
	fillImage(altSrc, 40, 91, 171)
	fillImage(interSrc, 60, 92, 172)
	dst := make([]byte, 4096)

	if _, err := e.EncodeInto(dst, keySrc, 0, 1, EncodeForceKeyFrame); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	altRefresh, err := e.EncodeInto(dst, altSrc, 1, 1, EncodeInvisibleFrame|EncodeForceAltRefFrame|EncodeNoUpdateLast|EncodeNoUpdateGolden)
	if err != nil {
		t.Fatalf("alt refresh EncodeInto returned error: %v", err)
	}
	altState := packetState(t, altRefresh.Data)
	if altState.Refresh.AltRefSignBias {
		t.Fatalf("alt-refresh frame AltRefSignBias = true, want false before update_alt_ref_frame_stats activates ALTREF")
	}
	if !e.sourceAltRefActive {
		t.Fatalf("sourceAltRefActive = false after ALTREF refresh, want true")
	}
	if len(altRefresh.Data) == 0 {
		t.Fatalf("alt refresh wrote no packet data")
	}

	inter, err := e.EncodeInto(dst, interSrc, 2, 1, 0)
	if err != nil {
		t.Fatalf("post-altref inter EncodeInto returned error: %v", err)
	}
	interState := packetState(t, inter.Data)
	if !interState.Refresh.AltRefSignBias || interState.Refresh.GoldenSignBias {
		t.Fatalf("post-altref sign bias = golden:%v alt:%v, want golden:false alt:true", interState.Refresh.GoldenSignBias, interState.Refresh.AltRefSignBias)
	}

	// FORCE_GF in isolation maps (via libvpx vp8e_set_frame_flags
	// upd-mask) to refresh_last=refresh_golden=refresh_alt_ref=1, which
	// re-routes the post-encode dispatcher to update_alt_ref_frame_stats
	// (play_alternate=AutoAltRef=true on this encoder) and keeps
	// sourceAltRefActive=true. To exercise the libvpx "GOLDEN refresh
	// while ALTREF active" branch (update_golden_frame_stats with
	// refresh_golden_frame=1 and refresh_alt_ref_frame=0), the user has
	// to opt out of the ALTREF half of the FORCE_GF mask with
	// EncodeNoUpdateAltRef.
	golden, err := e.EncodeInto(dst, interSrc, 3, 1, EncodeForceGoldenFrame|EncodeNoUpdateAltRef)
	if err != nil {
		t.Fatalf("golden refresh EncodeInto returned error: %v", err)
	}
	goldenState := packetState(t, golden.Data)
	if !goldenState.Refresh.AltRefSignBias {
		t.Fatalf("golden-refresh frame AltRefSignBias = false, want true while ALTREF was active for this frame")
	}
	if e.sourceAltRefActive {
		t.Fatalf("sourceAltRefActive = true after GOLDEN refresh, want false")
	}
}

// TestSignBiasEvolutionMatchesLibvpxAcrossGFAndARF drives a 12-frame sequence
// with AutoAltRef enabled so the encoder produces a key frame, several inter
// frames, a hidden ARF refresh, the matching deferred show frame, more inter
// frames, and a forced GOLDEN refresh. For each emitted packet it parses the
// (golden_sign_bias, altref_sign_bias) header bits and asserts they match the
// libvpx evolution rule out of vp8/encoder/onyx_if.c:
//
//   - GOLDEN sign bias is always 0: update_golden_frame_stats never flips
//     ref_frame_sign_bias[GOLDEN_FRAME].
//   - ALTREF sign bias at frame N equals cpi->source_alt_ref_active as seen
//     ENTERING frame N. update_alt_ref_frame_stats sets source_alt_ref_active
//     AFTER the hidden ARF refresh, so the refresh frame itself encodes the
//     prior bias (false). The first show frame after the hidden ARF then
//     encodes (false, true). update_golden_frame_stats clears the active
//     flag on a GOLDEN refresh ONLY if no ARF is pending; the GOLDEN refresh
//     frame still encodes the prior bias because the clear runs AFTER pack.
//
// The expected per-packet tuple is derived by replaying libvpx's two stat
// updates against each packet's RefreshAltRef / RefreshGolden bits, so any
// drift between govpx's interFrameSignBias() / updateGoldenFrameStats() and
// libvpx's update_alt_ref_frame_stats / update_golden_frame_stats surfaces
// here as a per-frame tuple mismatch with the failing frame index pinned.

func TestSignBiasEvolutionMatchesLibvpxAcrossGFAndARF(t *testing.T) {
	e := newAutoAltRefTestEncoder(t)
	const frameCount = 12
	const width = 32
	const height = 32
	dst := make([]byte, 1<<16)
	type emittedFrame struct {
		index      int
		pts        uint64
		key        bool
		show       bool
		refresh    vp8dec.RefreshHeader
		forcedGold bool
	}
	emitted := make([]emittedFrame, 0, frameCount+8)
	pushPacket := func(idx int, pts uint64, data []byte, forcedGold bool) {
		t.Helper()
		hdr, err := vp8dec.ParseFrameHeader(data)
		if err != nil {
			t.Fatalf("ParseFrameHeader frame %d (pts=%d): %v", idx, pts, err)
		}
		state := parseEncoderStateHeader(t, data)
		emitted = append(emitted, emittedFrame{
			index:      idx,
			pts:        pts,
			key:        hdr.KeyFrame(),
			show:       hdr.ShowFrame,
			refresh:    state.Refresh,
			forcedGold: forcedGold,
		})
	}
	// Drive frameCount source frames; force a GOLDEN refresh on frame 10 so
	// the evolution covers a forced GF refresh AFTER the auto-ARF has
	// activated (libvpx's "GOLDEN refresh while ALTREF active" branch). The
	// auto-ARF driver's hidden ARF and matching deferred show frame are
	// scheduled naturally by the lookahead during the early frames.
	for i := range frameCount {
		img := movingBarTestImage(width, height, i)
		var flags EncodeFlags
		forced := false
		if i == 10 {
			// FORCE_GF alone runs through libvpx's upd-mask and sets
			// refresh_alt_ref=1 too, which would re-route the post-encode
			// dispatcher away from update_golden_frame_stats. Opt out of
			// the ALTREF half of the mask so the libvpx "GOLDEN refresh
			// while ALTREF active" branch (update_golden_frame_stats
			// with refresh_golden=1, refresh_alt_ref=0) actually fires.
			flags = EncodeForceGoldenFrame | EncodeNoUpdateAltRef
			forced = true
		}
		result, err := e.EncodeInto(dst, img, uint64(i)*1000, 1000, flags)
		if err != nil {
			if errors.Is(err, ErrFrameNotReady) {
				continue
			}
			t.Fatalf("EncodeInto frame %d: %v", i, err)
		}
		if result.Dropped || len(result.Data) == 0 {
			continue
		}
		pushPacket(i, result.PTS, append([]byte(nil), result.Data...), forced)
	}
	for {
		result, err := e.FlushInto(dst)
		if err != nil {
			if errors.Is(err, ErrFrameNotReady) {
				break
			}
			t.Fatalf("FlushInto: %v", err)
		}
		if result.Dropped || len(result.Data) == 0 {
			continue
		}
		pushPacket(-1, result.PTS, append([]byte(nil), result.Data...), false)
	}
	if len(emitted) == 0 {
		t.Fatalf("no packets emitted")
	}
	// The test only buys parity coverage if at least one hidden ARF, one
	// deferred show frame, and one forced GOLDEN refresh actually fire in
	// the captured stream.
	hiddenSeen := false
	deferredShowSeen := false
	goldenRefreshSeen := false
	for i, p := range emitted {
		if !p.key && !p.show && p.refresh.RefreshAltRef {
			hiddenSeen = true
			// The deferred show frame is the next visible non-key packet.
			for j := i + 1; j < len(emitted); j++ {
				if !emitted[j].key && emitted[j].show {
					deferredShowSeen = true
					break
				}
			}
		}
		if !p.key && p.refresh.RefreshGolden && !p.refresh.RefreshAltRef {
			goldenRefreshSeen = true
		}
	}
	if !hiddenSeen {
		t.Fatalf("expected at least one hidden ARF in the captured stream; got %d packets", len(emitted))
	}
	if !deferredShowSeen {
		t.Fatalf("expected at least one deferred show frame after the hidden ARF")
	}
	if !goldenRefreshSeen {
		t.Fatalf("expected at least one GOLDEN refresh in the captured stream")
	}
	// Replay libvpx's per-frame sign-bias derivation against each packet.
	// State entering frame N is (active, pending). For each packet:
	//   1. Expected bias = (false, active) — the libvpx onyx_if.c
	//      pre-pack write at line 3397-3401 reads source_alt_ref_active
	//      and never flips GOLDEN.
	//   2. Update active/pending using update_alt_ref_frame_stats /
	//      update_golden_frame_stats semantics for the refresh bits in the
	//      packet (and reset to (false,false) on a key frame).
	active := false
	pending := false
	for i, p := range emitted {
		var wantGolden, wantAltRef bool
		if p.key {
			// Key frame's RefreshHeader has no sign-bias bits, so the
			// decoder leaves them as the zero value. After the key
			// frame libvpx clears source_alt_ref_active /
			// source_alt_ref_pending in resetGoldenFrameStats.
			wantGolden = false
			wantAltRef = false
		} else {
			wantGolden = false
			wantAltRef = active
		}
		gotGolden := p.refresh.GoldenSignBias
		gotAltRef := p.refresh.AltRefSignBias
		if gotGolden != wantGolden || gotAltRef != wantAltRef {
			t.Fatalf("packet %d (src=%d pts=%d key=%v show=%v refLast=%v refGold=%v refARF=%v forcedGold=%v) sign-bias = (golden=%v, altref=%v), want (golden=%v, altref=%v); state entering frame: active=%v pending=%v",
				i, p.index, p.pts, p.key, p.show,
				p.refresh.RefreshLast, p.refresh.RefreshGolden, p.refresh.RefreshAltRef,
				p.forcedGold,
				gotGolden, gotAltRef, wantGolden, wantAltRef,
				active, pending)
		}
		// Advance (active, pending) using the libvpx update rules.
		if p.key {
			active = false
			pending = false
			continue
		}
		if p.refresh.RefreshAltRef {
			// update_alt_ref_frame_stats: clears pending, sets active.
			active = true
			pending = false
			continue
		}
		if p.refresh.RefreshGolden {
			// update_golden_frame_stats: when no ARF is pending the
			// active flag clears; when one is pending it stays.
			if !pending {
				active = false
			}
		}
		// Non-refresh inter frames leave (active, pending) unchanged for
		// the purposes of the sign-bias derivation. govpx's auto-ARF
		// driver may set pending later via scheduleAltRefSource, but
		// pending alone never affects ref_frame_sign_bias[ALTREF_FRAME];
		// only update_alt_ref_frame_stats does, and that runs on a
		// hidden ARF commit.
		_ = pending
	}
}
