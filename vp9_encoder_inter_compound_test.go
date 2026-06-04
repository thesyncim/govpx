package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"testing"
)

// TestVP9EncoderInterDisplayedForceArfZeroMotion pins libvpx's
// ref_frame_sign_bias semantics for an externally-driven FORCE_ALTREF in
// display order. set_ref_sign_bias (vp9/encoder/vp9_encoder.c:4806-4821)
// computes cm->ref_frame_sign_bias[ref] = cur_frame_index <
// ref_buf->frame_index, and set_frame_index (vp9_encoder.c:5029-5038)
// stamps each buffer with current_video_frame + arf_src_offset. On the
// one-pass realtime / externally-flag-driven path arf_src_offset is 0, so a
// FORCE_ALTREF buffer refreshed at an earlier display frame is stamped with
// a *lower* frame_index than the frame referencing it: the ALTREF sign bias
// is therefore 0, not 1. With all three references sharing sign bias 0,
// vp9_setup_compound_reference_mode disallows compound, so the inter frame
// resolves to a single LAST reference.
//
// Oracle-verified: encoding this exact (key, displayed FORCE_ALTREF, inter)
// schedule through vpxenc-vp9-frameflags at --cpu-used=1 emits an inter
// uncompressed header byte 3 of 0x92 (ALTREF ref_frame_sign_bias bit = 0).
// The previous expectation (compound prediction) reflected a non-libvpx
// per-buffer "FORCE_ARF ⇒ sign bias 1" heuristic that has been removed.
func TestVP9EncoderInterDisplayedForceArfZeroMotion(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width: width, Height: height,
		Deadline: DeadlineBestQuality, CpuUsed: 1,
	})
	low := vp9test.NewCompoundAverageYCbCr(width, height, -32)
	mid := vp9test.NewCompoundAverageYCbCr(width, height, 0)
	high := vp9test.NewCompoundAverageYCbCr(width, height, 32)
	key, err := e.Encode(low)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	alt, err := e.EncodeWithFlags(high,
		EncodeForceAltRefFrame|EncodeNoUpdateLast|EncodeNoUpdateGolden|
			EncodeNoReferenceGolden|EncodeNoReferenceAltRef)
	if err != nil {
		t.Fatalf("Encode alt refresh: %v", err)
	}
	inter, err := e.Encode(mid)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	for i, packet := range [][]byte{key, alt, inter} {
		if err := d.Decode(packet); err != nil {
			t.Fatalf("Decode packet %d: %v", i, err)
		}
		if _, ok := d.NextFrame(); !ok {
			t.Fatalf("NextFrame returned !ok after packet %d", i)
		}
	}
	if len(d.miGrid) == 0 {
		t.Fatal("decoder MI grid is empty after inter frame")
	}
	got := d.miGrid[0]
	// libvpx: no compound (ALTREF sign bias 0), single LAST reference.
	if got.RefFrame[1] > vp9dec.IntraFrame {
		t.Fatalf("top-left ref pair = %v, want single reference (no compound)", got.RefFrame)
	}
	if got.RefFrame[0] != vp9dec.LastFrame {
		t.Fatalf("top-left primary ref = %d, want LAST_FRAME", got.RefFrame[0])
	}
	if got.Mode != common.ZeroMv && got.Mode != common.NearestMv && got.Mode != common.NearMv {
		t.Fatalf("top-left mode = %d, want zero-motion inter mode", got.Mode)
	}
}

// TestVP9EncoderInterForceArfNewMvTranslated checks the single-reference NewMv
// path for an externally-driven FORCE_ALTREF in display order. As in
// TestVP9EncoderInterDisplayedForceArfZeroMotion, libvpx set_ref_sign_bias
// leaves the ALTREF sign bias at 0 (oracle byte 3 = 0x92), so compound is
// disallowed; the +8px horizontally translated source resolves to a single
// ALTREF NewMv tracking that motion.
func TestVP9EncoderInterForceArfNewMvTranslated(t *testing.T) {
	const width, height = 128, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width: width, Height: height,
		Deadline: DeadlineBestQuality, CpuUsed: 1,
	})
	low := vp9test.NewCompoundPairYCbCr(width, height, false)
	high := vp9test.NewCompoundPairYCbCr(width, height, true)
	mid := shiftedVP9ReferenceYCbCrForTest(
		vp9ImageFromYCbCrForTest(vp9test.AverageYCbCr(low, high)),
		8, 0)
	key, err := e.Encode(low)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	alt, err := e.EncodeWithFlags(high,
		EncodeForceAltRefFrame|EncodeNoUpdateLast|EncodeNoUpdateGolden|
			EncodeNoReferenceGolden|EncodeNoReferenceAltRef)
	if err != nil {
		t.Fatalf("Encode alt refresh: %v", err)
	}
	inter, err := e.Encode(mid)
	if err != nil {
		t.Fatalf("Encode motion inter: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	for i, packet := range [][]byte{key, alt, inter} {
		if err := d.Decode(packet); err != nil {
			t.Fatalf("Decode packet %d: %v", i, err)
		}
		if _, ok := d.NextFrame(); !ok {
			t.Fatalf("NextFrame returned !ok after packet %d", i)
		}
	}
	if len(d.miGrid) == 0 {
		t.Fatal("decoder MI grid is empty after motion frame")
	}
	got := d.miGrid[0]
	// libvpx: no compound (ALTREF sign bias 0); single reference NewMv.
	if got.RefFrame[1] > vp9dec.IntraFrame {
		t.Fatalf("top-left ref pair = %v, want single reference (no compound)", got.RefFrame)
	}
	if got.Mode != common.NewMv {
		t.Fatalf("top-left mode = %d, want NewMv", got.Mode)
	}
	if got.Mv[0].Col < 56 || got.Mv[0].Col > 72 ||
		got.Mv[0].Row < -8 || got.Mv[0].Row > 8 {
		t.Fatalf("top-left MV = %+v, want ref near +8px horizontal motion", got.Mv[0])
	}
}

// TestVP9EncoderInterForceArfNewMvStationary checks the single-reference NewMv
// path for an externally-driven FORCE_ALTREF in display order with a source
// that averages a stationary half with an +8px translated half. libvpx
// set_ref_sign_bias leaves the ALTREF sign bias at 0 (oracle byte 3 = 0x92),
// so compound is disallowed and the block resolves to a single LAST reference
// NewMv near zero (tracking the stationary, lower-cost half).
func TestVP9EncoderInterForceArfNewMvStationary(t *testing.T) {
	const width, height = 128, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width: width, Height: height,
		Deadline: DeadlineBestQuality, CpuUsed: 1,
	})
	low := vp9test.NewCompoundPairYCbCr(width, height, false)
	high := vp9test.NewCompoundPairYCbCr(width, height, true)
	shiftedHigh := shiftedVP9ReferenceYCbCrForTest(vp9ImageFromYCbCrForTest(high), 8, 0)
	mid := vp9test.AverageYCbCr(low, shiftedHigh)
	key, err := e.Encode(low)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	alt, err := e.EncodeWithFlags(high,
		EncodeForceAltRefFrame|EncodeNoUpdateLast|EncodeNoUpdateGolden|
			EncodeNoReferenceGolden|EncodeNoReferenceAltRef)
	if err != nil {
		t.Fatalf("Encode alt refresh: %v", err)
	}
	inter, err := e.Encode(mid)
	if err != nil {
		t.Fatalf("Encode asymmetric motion inter: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	for i, packet := range [][]byte{key, alt, inter} {
		if err := d.Decode(packet); err != nil {
			t.Fatalf("Decode packet %d: %v", i, err)
		}
		if _, ok := d.NextFrame(); !ok {
			t.Fatalf("NextFrame returned !ok after packet %d", i)
		}
	}
	got := d.miGrid[0]
	// libvpx: no compound (ALTREF sign bias 0); single reference NewMv.
	if got.RefFrame[1] > vp9dec.IntraFrame {
		t.Fatalf("top-left ref pair = %v, want single reference (no compound)", got.RefFrame)
	}
	if got.Mode != common.NewMv {
		t.Fatalf("top-left mode = %d, want NewMv", got.Mode)
	}
	if got.Mv[0].Col < -4 || got.Mv[0].Col > 4 ||
		got.Mv[0].Row < -4 || got.Mv[0].Row > 4 {
		t.Fatalf("single-ref MV = %+v, want near zero (stationary half)", got.Mv[0])
	}
}
