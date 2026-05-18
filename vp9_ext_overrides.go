package govpx

// VP9 ext-refresh override resolution, ported verbatim from libvpx v1.16.0:
//
//   - vp9/vp9_cx_iface.c:1393-1398 "Conflicting flags." rejection of
//     VP8_EFLAG_NO_UPD_GF + VP8_EFLAG_FORCE_GF and
//     VP8_EFLAG_NO_UPD_ARF + VP8_EFLAG_FORCE_ARF on the same frame.
//   - vp9/vp9_cx_iface.c:1408 vp9_apply_encoding_flags(cpi, flags) handoff
//     from the codec interface into the encoder body.
//   - vp9/encoder/vp9_encoder.c:6812-6843 vp9_apply_encoding_flags maps the
//     vpx_enc_frame_flags_t bitset onto vp9_use_as_reference /
//     vp9_update_reference / vp9_update_entropy calls.
//   - vp9/encoder/vp9_encoder.c:2954-2959 vp9_update_reference writes the
//     cpi->ext_refresh_{last,golden,alt_ref}_frame fields and arms
//     cpi->ext_refresh_frame_flags_pending.
//   - vp9/encoder/vp9_encoder.c:2997-3001 vp9_update_entropy writes
//     cpi->ext_refresh_frame_context and arms
//     cpi->ext_refresh_frame_context_pending.
//   - vp9/encoder/vp9_encoder.c:2256-2257 vp9_change_config zeroes
//     cpi->ext_refresh_frame_flags_pending and
//     cpi->ext_refresh_frame_context_pending.
//   - vp9/encoder/vp9_encoder.c:4761-4775 set_ext_overrides copies the
//     ext_refresh_frame_* pending state onto the live
//     cm->refresh_frame_context and cpi->refresh_*_frame fields immediately
//     before encode_frame_to_data_rate runs the per-frame encode.
//   - vp9/encoder/vp9_encoder.c:5284 encode_frame_to_data_rate invokes
//     set_ext_overrides as the very first step of the per-frame encode,
//     before the show_existing_frame / sign-bias / loopfilter / pack
//     pipeline.
//
// The port is wired in two stages so the field names match the libvpx
// pipeline and downstream consumers can read the post-override refresh
// mask via the same idiom libvpx uses:
//
//  1. vp9ApplyEncodingFlags translates the EncodeFlags bitset onto the
//     ext_refresh_*_frame state machine (vp9_apply_encoding_flags
//     equivalent). It is invoked from EncodeIntoWithFlagsResult once the
//     caller-supplied flags are normalized.
//  2. setExtOverrides copies the ext_refresh_*_frame pending state onto
//     the per-frame refresh_*_frame fields the bitstream packer reads.
//     It is invoked from encodeVP9FrameIntoWithFlagsResultInternal at the
//     point libvpx calls set_ext_overrides inside encode_frame_to_data_rate.

// vp9ExtRefreshState mirrors the libvpx VP9_COMP ext_refresh_* fields
// (vp9_encoder.h:654-660):
//
//	int ext_refresh_frame_flags_pending;
//	int ext_refresh_last_frame;
//	int ext_refresh_golden_frame;
//	int ext_refresh_alt_ref_frame;
//
//	int ext_refresh_frame_context_pending;
//	int ext_refresh_frame_context;
//
// and the post-override refresh_*_frame fields
// (vp9_encoder.h:650-652):
//
//	int refresh_last_frame;
//	int refresh_golden_frame;
//	int refresh_alt_ref_frame;
//
// govpx pipes the ext_refresh_* state through this struct so the
// vp9_apply_encoding_flags → set_ext_overrides handoff lives in one
// named place and the consumer (the per-frame RefreshFrameFlags
// computation) reads it back the same way libvpx reads cpi->refresh_*_frame.
type vp9ExtRefreshState struct {
	// ext_refresh_frame_flags_pending (vp9_encoder.h:654)
	flagsPending bool
	// ext_refresh_last_frame (vp9_encoder.h:655)
	last bool
	// ext_refresh_golden_frame (vp9_encoder.h:656)
	golden bool
	// ext_refresh_alt_ref_frame (vp9_encoder.h:657)
	altRef bool

	// ext_refresh_frame_context_pending (vp9_encoder.h:659)
	contextPending bool
	// ext_refresh_frame_context (vp9_encoder.h:660)
	context bool

	// refresh_last_frame (vp9_encoder.h:650), latched by set_ext_overrides.
	refreshLast bool
	// refresh_golden_frame (vp9_encoder.h:651), latched by set_ext_overrides.
	refreshGolden bool
	// refresh_alt_ref_frame (vp9_encoder.h:652), latched by set_ext_overrides.
	refreshAltRef bool
	// refresh_*_frame latched valid (set_ext_overrides ran since last reset)
	refreshLatched bool

	// refresh_frame_context (cm->refresh_frame_context default 1), copied
	// by set_ext_overrides only when contextPending is armed.
	refreshFrameContext        bool
	refreshFrameContextLatched bool
}

// vp9ApplyEncodingFlagsError mirrors libvpx vp9/vp9_cx_iface.c:1393-1398:
//
//	if (((flags & VP8_EFLAG_NO_UPD_GF) && (flags & VP8_EFLAG_FORCE_GF)) ||
//	    ((flags & VP8_EFLAG_NO_UPD_ARF) && (flags & VP8_EFLAG_FORCE_ARF))) {
//	  ctx->base.err_detail = "Conflicting flags.";
//	  return VPX_CODEC_INVALID_PARAM;
//	}
//
// Callers that hold the libvpx interface contract (the runtime-controls
// fuzz oracle, vpxenc-vp9-frameflags) reject the same way, so govpx
// surfaces this as ErrInvalidConfig. Returning the error here lets callers
// run normalizeVP9EncodeFlags ahead of the encoder body to pre-resolve the
// conflict (matching the fuzz-materialiser convention at
// vp9_oracle_encoder_runtime_controls_fuzz_test.go:508-522).
func vp9ApplyEncodingFlagsError(flags EncodeFlags) error {
	if flags&EncodeForceGoldenFrame != 0 && flags&EncodeNoUpdateGolden != 0 {
		return ErrInvalidConfig
	}
	if flags&EncodeForceAltRefFrame != 0 && flags&EncodeNoUpdateAltRef != 0 {
		return ErrInvalidConfig
	}
	return nil
}

// vp9ApplyEncodingFlags mirrors libvpx vp9/encoder/vp9_encoder.c:6812-6843
// vp9_apply_encoding_flags. The libvpx body:
//
//	if (flags & (NO_REF_LAST | NO_REF_GF | NO_REF_ARF)) {
//	  int ref = 7;
//	  if (flags & NO_REF_LAST) ref ^= VP9_LAST_FLAG;
//	  if (flags & NO_REF_GF)   ref ^= VP9_GOLD_FLAG;
//	  if (flags & NO_REF_ARF)  ref ^= VP9_ALT_FLAG;
//	  vp9_use_as_reference(cpi, ref);
//	}
//	if (flags & (NO_UPD_LAST | NO_UPD_GF | NO_UPD_ARF | FORCE_GF | FORCE_ARF)) {
//	  int upd = 7;
//	  if (flags & NO_UPD_LAST) upd ^= VP9_LAST_FLAG;
//	  if (flags & NO_UPD_GF)   upd ^= VP9_GOLD_FLAG;
//	  if (flags & NO_UPD_ARF)  upd ^= VP9_ALT_FLAG;
//	  vp9_update_reference(cpi, upd);
//	}
//	if (flags & VP8_EFLAG_NO_UPD_ENTROPY) {
//	  vp9_update_entropy(cpi, 0);
//	}
//
// govpx writes the equivalent ext_refresh_* state. The downstream
// vp9_use_as_reference reference-frame-flag mask is already handled by
// vp9InterReferenceMask in the existing encoder body; only the
// refresh-side state is plumbed here because that's what set_ext_overrides
// reads in libvpx.
func (e *VP9Encoder) vp9ApplyEncodingFlags(flags EncodeFlags) {
	if e == nil {
		return
	}
	// vp9_encoder.c:6826-6838: enter the refresh arm when any
	// NO_UPD_{LAST,GF,ARF} or FORCE_{GF,ARF} bit is set.
	if flags&vp9ExternalRefreshCtlFlags != 0 {
		// vp9_encoder.c:6829: int upd = 7;
		const (
			vp9LastBit = 1 << vp9LastRefSlot   // VP9_LAST_FLAG = 1
			vp9GoldBit = 1 << vp9GoldenRefSlot // VP9_GOLD_FLAG = 2
			vp9AltBit  = 1 << vp9AltRefSlot    // VP9_ALT_FLAG  = 4
		)
		upd := vp9LastBit | vp9GoldBit | vp9AltBit
		if flags&EncodeNoUpdateLast != 0 {
			upd ^= vp9LastBit
		}
		if flags&EncodeNoUpdateGolden != 0 {
			upd ^= vp9GoldBit
		}
		if flags&EncodeNoUpdateAltRef != 0 {
			upd ^= vp9AltBit
		}
		// vp9_encoder.c:2954-2959: vp9_update_reference writes the
		// ext_refresh_*_frame fields from the (LAST|GOLD|ALT) mask and
		// arms ext_refresh_frame_flags_pending.
		e.extRefresh.last = upd&vp9LastBit != 0
		e.extRefresh.golden = upd&vp9GoldBit != 0
		e.extRefresh.altRef = upd&vp9AltBit != 0
		e.extRefresh.flagsPending = true
	}
	// vp9_encoder.c:6840-6842: VP8_EFLAG_NO_UPD_ENTROPY arms
	// vp9_update_entropy(cpi, 0) i.e. cm->refresh_frame_context = 0
	// (do NOT propagate the per-frame entropy update).
	if flags&EncodeNoUpdateEntropy != 0 {
		// vp9_encoder.c:2997-3001 vp9_update_entropy.
		e.extRefresh.context = false
		e.extRefresh.contextPending = true
	}
}

// setExtOverrides mirrors libvpx vp9/encoder/vp9_encoder.c:4761-4775
// set_ext_overrides verbatim:
//
//	if (cpi->ext_refresh_frame_context_pending) {
//	  cpi->common.refresh_frame_context = cpi->ext_refresh_frame_context;
//	  cpi->ext_refresh_frame_context_pending = 0;
//	}
//	if (cpi->ext_refresh_frame_flags_pending) {
//	  cpi->refresh_last_frame = cpi->ext_refresh_last_frame;
//	  cpi->refresh_golden_frame = cpi->ext_refresh_golden_frame;
//	  cpi->refresh_alt_ref_frame = cpi->ext_refresh_alt_ref_frame;
//	}
//
// Note the libvpx body does NOT zero ext_refresh_frame_flags_pending after
// the copy (only ext_refresh_frame_context_pending is cleared). The flags
// pending counter is cleared later, after the encode commits, at
// vp9_encoder.c:5567 "cpi->ext_refresh_frame_flags_pending = 0;".
// govpx mirrors this two-step clear: setExtOverrides latches the values
// for the bitstream packer to read, and the per-frame commit point
// (vp9CommitExtOverridesAfterEncode) clears flagsPending.
func (e *VP9Encoder) setExtOverrides() {
	if e == nil {
		return
	}
	if e.extRefresh.contextPending {
		e.extRefresh.refreshFrameContext = e.extRefresh.context
		e.extRefresh.refreshFrameContextLatched = true
		e.extRefresh.contextPending = false
	}
	if e.extRefresh.flagsPending {
		e.extRefresh.refreshLast = e.extRefresh.last
		e.extRefresh.refreshGolden = e.extRefresh.golden
		e.extRefresh.refreshAltRef = e.extRefresh.altRef
		e.extRefresh.refreshLatched = true
	}
}

// vp9CommitExtOverridesAfterEncode clears the ext_refresh_frame_flags_pending
// counter once the per-frame encode commits, mirroring libvpx
// vp9/encoder/vp9_encoder.c:5567 inside encode_frame_to_data_rate's tail:
//
//	cpi->ext_refresh_frame_flags_pending = 0;
//
// This is the libvpx convention: set_ext_overrides reads pending → live,
// and the very tail of the per-frame encode clears the pending bit so the
// next frame defaults to the encoder-internal refresh decision unless the
// caller arms it again via vp9_apply_encoding_flags.
func (e *VP9Encoder) vp9CommitExtOverridesAfterEncode() {
	if e == nil {
		return
	}
	e.extRefresh.flagsPending = false
	e.extRefresh.refreshLatched = false
	e.extRefresh.refreshFrameContextLatched = false
}

// vp9ExtOverrideRefreshMask returns the RefreshFrameFlags bitmask
// (1<<vp9LastRefSlot | 1<<vp9GoldenRefSlot | 1<<vp9AltRefSlot) that the
// libvpx bitstream packer would write after set_ext_overrides has latched
// the ext_refresh_*_frame fields onto cpi->refresh_{last,golden,alt_ref}_frame
// (vp9/encoder/vp9_bitstream.c reads the post-override refresh state when
// emitting the inter uncompressed header). Returns ok=false when no
// override is pending so the caller can fall back to the encoder-internal
// refresh decision (one-pass VBR golden refresh, temporal SVC scoreboard,
// CBR rate-control golden refresh) the same way libvpx does: the libvpx
// encoder body initialises refresh_*_frame from those internal sources
// BEFORE set_ext_overrides copies the user override on top.
func (e *VP9Encoder) vp9ExtOverrideRefreshMask() (uint8, bool) {
	if e == nil || !e.extRefresh.refreshLatched {
		return 0, false
	}
	var mask uint8
	if e.extRefresh.refreshLast {
		mask |= 1 << vp9LastRefSlot
	}
	if e.extRefresh.refreshGolden {
		mask |= 1 << vp9GoldenRefSlot
	}
	if e.extRefresh.refreshAltRef {
		mask |= 1 << vp9AltRefSlot
	}
	return mask, true
}

// vp9ExtOverrideRefreshFrameContext returns the post-set_ext_overrides
// cm->refresh_frame_context value when the caller armed it via
// VP8_EFLAG_NO_UPD_ENTROPY (vp9_apply_encoding_flags →
// vp9_update_entropy). Returns ok=false when no override is pending; the
// caller falls back to the existing per-frame entropy-update decision.
func (e *VP9Encoder) vp9ExtOverrideRefreshFrameContext() (bool, bool) {
	if e == nil || !e.extRefresh.refreshFrameContextLatched {
		return false, false
	}
	return e.extRefresh.refreshFrameContext, true
}
