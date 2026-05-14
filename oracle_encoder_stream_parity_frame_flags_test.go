//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// frameFlagsForLibvpx mirrors the bit layout of the govpx
// [EncodeFlags] enum so the libvpx-side companion driver
// (`vpxenc-frameflags`) can be fed the same per-frame schedule.
// Both implementations target libvpx's stable
// VPX_EFLAG_FORCE_KF / VP8_EFLAG_NO_REF_* / VP8_EFLAG_NO_UPD_* /
// VPX_EFLAG_FORCE_GF / VPX_EFLAG_FORCE_ARF / VPX_EFLAG_NO_UPD_ENTROPY
// constants (defined in vp8cx.h and vpx_encoder.h).
func frameFlagsForLibvpx(f EncodeFlags) uint32 {
	const (
		libvpxForceKF      = 1 << 0  // VPX_EFLAG_FORCE_KF
		libvpxNoRefLast    = 1 << 16 // VP8_EFLAG_NO_REF_LAST
		libvpxNoRefGF      = 1 << 17 // VP8_EFLAG_NO_REF_GF
		libvpxNoUpdLast    = 1 << 18 // VP8_EFLAG_NO_UPD_LAST
		libvpxForceGF      = 1 << 19 // VP8_EFLAG_FORCE_GF
		libvpxNoUpdEntropy = 1 << 20 // VP8_EFLAG_NO_UPD_ENTROPY
		libvpxNoRefARF     = 1 << 21 // VP8_EFLAG_NO_REF_ARF
		libvpxNoUpdGF      = 1 << 22 // VP8_EFLAG_NO_UPD_GF
		libvpxNoUpdARF     = 1 << 23 // VP8_EFLAG_NO_UPD_ARF
		libvpxForceARF     = 1 << 24 // VP8_EFLAG_FORCE_ARF
	)
	var out uint32
	if f&EncodeForceKeyFrame != 0 {
		out |= libvpxForceKF
	}
	if f&EncodeNoUpdateLast != 0 {
		out |= libvpxNoUpdLast
	}
	if f&EncodeNoUpdateGolden != 0 {
		out |= libvpxNoUpdGF
	}
	if f&EncodeNoUpdateAltRef != 0 {
		out |= libvpxNoUpdARF
	}
	if f&EncodeNoReferenceLast != 0 {
		out |= libvpxNoRefLast
	}
	if f&EncodeNoReferenceGolden != 0 {
		out |= libvpxNoRefGF
	}
	if f&EncodeNoReferenceAltRef != 0 {
		out |= libvpxNoRefARF
	}
	if f&EncodeForceGoldenFrame != 0 {
		out |= libvpxForceGF
	}
	if f&EncodeForceAltRefFrame != 0 {
		out |= libvpxForceARF
	}
	if f&EncodeNoUpdateEntropy != 0 {
		out |= libvpxNoUpdEntropy
	}
	// EncodeInvisibleFrame is a govpx-specific hidden-frame marker
	// that maps to "encode then suppress show_frame"; libvpx does
	// not have a single flag bit for it. The frame-flag driver gets
	// the hidden-frame schedule through --invisible-frames instead.
	return out
}

// findVpxencFrameFlags locates the companion encoder driver,
// preferring the explicit env override and falling back to the
// build dir produced by internal/coracle/build_vpxenc_frameflags.sh.
func findVpxencFrameFlags(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("GOVPX_VPXENC_FRAMEFLAGS"); v != "" {
		if _, err := os.Stat(v); err == nil {
			return v
		}
		t.Fatalf("GOVPX_VPXENC_FRAMEFLAGS=%q not found", v)
	}
	candidates := []string{
		"internal/coracle/build/vpxenc-frameflags",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			abs, err := filepath.Abs(c)
			if err == nil {
				return abs
			}
		}
	}
	t.Skip("vpxenc-frameflags binary not available; set GOVPX_VPXENC_FRAMEFLAGS or run internal/coracle/build_vpxenc_frameflags.sh")
	return ""
}

// TestOracleEncoderStreamByteParityFrameFlags exercises per-frame
// VP8 flag scheduling — the EncodeForceKeyFrame /
// EncodeNoUpdateLast / EncodeNoUpdateGolden / EncodeNoUpdateAltRef /
// EncodeNoReferenceLast / EncodeNoReferenceGolden /
// EncodeNoReferenceAltRef / EncodeForceGoldenFrame /
// EncodeForceAltRefFrame / EncodeNoUpdateEntropy axes that the
// stock vpxenc binary cannot drive. The libvpx-side reference is the
// companion vpxenc-frameflags driver, which translates the same
// per-frame schedule into vpx_codec_encode flags.
//
// Cases that diverge are pinned with `limit:` so the gap is visible
// in the per-frame "byte mismatch (not asserted, ...)" log lines
// without regressing the strict gate.
func TestOracleEncoderStreamByteParityFrameFlags(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder stream byte-parity gate")
	}
	driver := findVpxencFrameFlags(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 8
	)

	type fixture struct {
		name   string
		w, h   int
		source func(w, h, i int) Image
	}
	panning16 := fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}
	panning32 := fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}
	panning48 := fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}
	panning64 := fixture{name: "panning-64x64", w: 64, h: 64, source: encoderValidationPanningFrame}
	panningOdd := fixture{name: "panning-65x33", w: 65, h: 33, source: encoderValidationPanningFrame}

	cases := []struct {
		name                     string
		deadline                 Deadline
		cpuUsed                  int
		fx                       fixture
		limit                    int
		flags                    []EncodeFlags // per-frame; missing indices default to 0.
		rcMode                   RateControlMode
		rcModeSet                bool
		cqLevel                  int
		disableKf                bool
		tokenParts               int
		errorResilient           bool
		errorResilientPartitions bool
		extraArgs                []string // appended to libvpx driver argv.
	}{
		// Force-keyframe at frame 3. Both implementations must emit
		// a keyframe at index 3; the surrounding frames must
		// byte-match too.
		{name: "force-kf-frame3-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, 0, 0, EncodeForceKeyFrame}},
		{name: "force-kf-frame3-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, flags: []EncodeFlags{0, 0, 0, EncodeForceKeyFrame}},
		{name: "force-kf-frame3-realtime-cpu0-48x48", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning48, flags: []EncodeFlags{0, 0, 0, EncodeForceKeyFrame}},
		{name: "force-kf-frame3-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, flags: []EncodeFlags{0, 0, 0, EncodeForceKeyFrame}},
		// Force-keyframe at frame 1 (immediately after the initial key).
		{name: "force-kf-frame1-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, EncodeForceKeyFrame}},
		{name: "force-kf-frame1-realtime-cpu-3-32x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, flags: []EncodeFlags{0, EncodeForceKeyFrame}},
		// Force-keyframe on every frame (the "all keyframes by flag" axis
		// — orthogonal to the existing `kfInterval=1` axis which uses
		// kf-min/max-dist instead of the per-frame flag).
		{name: "force-kf-every-frame-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{EncodeForceKeyFrame, EncodeForceKeyFrame, EncodeForceKeyFrame, EncodeForceKeyFrame, EncodeForceKeyFrame, EncodeForceKeyFrame, EncodeForceKeyFrame, EncodeForceKeyFrame}},
		// Explicit force-key requests still win when automatic
		// keyframes are disabled from construction.
		{name: "disable-kf-force-kf-frame3-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, disableKf: true, flags: []EncodeFlags{0, 0, 0, EncodeForceKeyFrame}, extraArgs: []string{"--kf-disabled"}},
		// Hidden packets clear VP8 show_frame while keeping the encoded
		// reference update byte-identical to libvpx.
		{name: "invisible-keyframe-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{EncodeInvisibleFrame}},
		{name: "invisible-inter-frame1-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, EncodeInvisibleFrame}},
		{name: "invisible-inter-run-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, EncodeInvisibleFrame, EncodeInvisibleFrame, 0, EncodeInvisibleFrame}},
		{name: "invisible-no-upd-entropy-no-ref-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, EncodeInvisibleFrame | EncodeNoUpdateEntropy | EncodeNoUpdateLast | EncodeNoReferenceGolden | EncodeNoReferenceAltRef}},
		{name: "invisible-force-gf-arf-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, 0, EncodeInvisibleFrame | EncodeForceGoldenFrame, 0, EncodeInvisibleFrame | EncodeForceAltRefFrame}},
		{name: "invisible-force-kf-frame3-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, 0, 0, EncodeInvisibleFrame | EncodeForceKeyFrame}},
		{name: "invisible-no-upd-all-token8-er3-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, tokenParts: 3, errorResilient: true, errorResilientPartitions: true, extraArgs: []string{"--error-resilient=3", "--token-parts=3"}, flags: repeatFlag(frames-1, EncodeInvisibleFrame|EncodeNoUpdateLast|EncodeNoUpdateGolden|EncodeNoUpdateAltRef)},
		{name: "invisible-no-upd-entropy-no-upd-all-token4-realtime-cpu-3-32x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, tokenParts: 2, extraArgs: []string{"--token-parts=2"}, flags: repeatFlag(frames-1, EncodeInvisibleFrame|EncodeNoUpdateEntropy|EncodeNoUpdateLast|EncodeNoUpdateGolden|EncodeNoUpdateAltRef)},
		{name: "invisible-force-gf-arf-no-upd-entropy-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, flags: []EncodeFlags{0, 0, EncodeInvisibleFrame | EncodeForceGoldenFrame | EncodeNoUpdateEntropy, 0, EncodeInvisibleFrame | EncodeForceAltRefFrame | EncodeNoUpdateEntropy}},

		// EncodeNoUpdateLast on every inter frame — exercises the
		// "freeze LAST" pattern used by WebRTC scalability layers.
		// libvpx vp8_cx_iface vp8e_set_frame_flags routes any of
		// {NO_UPD_LAST, NO_UPD_GF, NO_UPD_ARF, FORCE_GF, FORCE_ARF}
		// through vp8_update_reference with an inverted "upd" mask
		// (start at all-three, XOR off each NO_UPD_*); govpx mirrors
		// the same mask via libvpxExternalRefreshMask so the
		// downstream refresh / sourceAltRefActive bookkeeping lines
		// up on the next frame.
		{name: "no-upd-last-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, EncodeNoUpdateLast, EncodeNoUpdateLast, EncodeNoUpdateLast, EncodeNoUpdateLast, EncodeNoUpdateLast, EncodeNoUpdateLast, EncodeNoUpdateLast}},
		{name: "no-upd-last-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, flags: []EncodeFlags{0, EncodeNoUpdateLast, EncodeNoUpdateLast, EncodeNoUpdateLast, EncodeNoUpdateLast, EncodeNoUpdateLast, EncodeNoUpdateLast, EncodeNoUpdateLast}},
		// EncodeNoUpdateGolden / EncodeNoUpdateAltRef on every inter
		// frame. Together with the existing temporal SVC scoreboard
		// tests these pin the per-flag refresh accounting through
		// the libvpx upd-mask semantics described above.
		{name: "no-upd-gf-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: repeatFlag(frames-1, EncodeNoUpdateGolden)},
		{name: "no-upd-arf-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: repeatFlag(frames-1, EncodeNoUpdateAltRef)},
		{name: "no-upd-gf-arf-realtime-cpu-3-32x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, flags: repeatFlag(frames-1, EncodeNoUpdateGolden|EncodeNoUpdateAltRef)},
		{name: "no-upd-all-every-inter-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: repeatFlag(frames-1, EncodeNoUpdateLast|EncodeNoUpdateGolden|EncodeNoUpdateAltRef)},

		// EncodeNoReferenceGolden / EncodeNoReferenceAltRef — drop
		// the GF/ARF reference from the inter prediction pool. The
		// picker must fall back to LAST-only, which is what WebRTC
		// scalability uses to prevent cross-layer dependencies.
		{name: "no-ref-last-every-inter-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: repeatFlag(frames-1, EncodeNoReferenceLast)},
		{name: "no-ref-last-every3-realtime-cpu-3-32x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, flags: everyNFlag(frames, 3, EncodeNoReferenceLast)},
		{name: "no-ref-gf-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: repeatFlag(frames-1, EncodeNoReferenceGolden)},
		{name: "no-ref-arf-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: repeatFlag(frames-1, EncodeNoReferenceAltRef)},
		{name: "no-ref-gf-arf-realtime-cpu-3-32x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, flags: repeatFlag(frames-1, EncodeNoReferenceGolden|EncodeNoReferenceAltRef)},
		{name: "no-ref-all-every-inter-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: repeatFlag(frames-1, EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef)},

		// Combined no-update-Last+no-reference-Golden, the canonical
		// "base temporal layer" pattern from libvpx's
		// vpx_temporal_svc_encoder mode 1.
		{name: "no-upd-last-no-ref-gf-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: repeatFlag(frames-1, EncodeNoUpdateLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef)},
		{name: "no-ref-last-no-upd-gf-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: repeatFlag(frames-1, EncodeNoReferenceLast|EncodeNoUpdateGolden)},
		{name: "no-ref-last-no-upd-arf-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: repeatFlag(frames-1, EncodeNoReferenceLast|EncodeNoUpdateAltRef)},
		{name: "no-ref-gf-no-upd-last-realtime-cpu-3-32x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, flags: repeatFlag(frames-1, EncodeNoReferenceGolden|EncodeNoUpdateLast)},
		{name: "no-ref-arf-no-upd-gf-realtime-cpu-3-32x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, flags: repeatFlag(frames-1, EncodeNoReferenceAltRef|EncodeNoUpdateGolden)},

		// EncodeForceGoldenFrame / EncodeForceAltRefFrame at a
		// specific frame, mirroring the "manual GF/ARF refresh"
		// pattern. libvpx's upd-mask interpretation here is the
		// surprising part: with no NO_UPD_* bits set the mask stays
		// at 7 so refresh_last_frame, refresh_golden_frame and
		// refresh_alt_ref_frame ALL flip to 1 on the forced frame
		// (independent of which of FORCE_GF / FORCE_ARF the user
		// requested). libvpxExternalRefreshMask reproduces that.
		{name: "force-gf-frame4-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, 0, 0, 0, EncodeForceGoldenFrame}},
		{name: "force-gf-frame4-realtime-cpu-3-32x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, flags: []EncodeFlags{0, 0, 0, 0, EncodeForceGoldenFrame}},
		{name: "force-gf-no-upd-last-frame4-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, 0, 0, 0, EncodeForceGoldenFrame | EncodeNoUpdateLast}},
		{name: "force-gf-every3-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, flags: everyNFlag(frames, 3, EncodeForceGoldenFrame)},
		{name: "force-arf-frame4-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, 0, 0, 0, EncodeForceAltRefFrame}},
		{name: "force-arf-no-upd-last-gf-frame4-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, 0, 0, 0, EncodeForceAltRefFrame | EncodeNoUpdateLast | EncodeNoUpdateGolden}},
		{name: "force-arf-every3-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, flags: everyNFlag(frames, 3, EncodeForceAltRefFrame)},
		{name: "force-gf-arf-same-frame-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, 0, 0, 0, EncodeForceGoldenFrame | EncodeForceAltRefFrame}},
		{name: "force-gf-no-upd-entropy-frame4-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, 0, 0, 0, EncodeForceGoldenFrame | EncodeNoUpdateEntropy}},
		{name: "force-arf-no-upd-entropy-frame4-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, 0, 0, 0, EncodeForceAltRefFrame | EncodeNoUpdateEntropy}},

		// EncodeNoUpdateEntropy on every inter frame — keeps the
		// reference entropy adaptation state frozen.
		{name: "no-upd-entropy-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: repeatFlag(frames-1, EncodeNoUpdateEntropy)},
		{name: "no-upd-entropy-realtime-cpu-3-32x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, flags: repeatFlag(frames-1, EncodeNoUpdateEntropy)},
		{name: "no-upd-entropy-realtime-cpu0-32x32-4partitions", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, tokenParts: 2, extraArgs: []string{"--token-parts=2"}, flags: repeatFlag(frames-1, EncodeNoUpdateEntropy)},
		{name: "force-kf-no-upd-last-entropy-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, 0, 0, EncodeForceKeyFrame | EncodeNoUpdateLast | EncodeNoUpdateEntropy}},
		{name: "no-upd-entropy-no-upd-all-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: repeatFlag(frames-1, EncodeNoUpdateEntropy|EncodeNoUpdateLast|EncodeNoUpdateGolden|EncodeNoUpdateAltRef)},
		{name: "no-upd-all-er2-token8-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, tokenParts: 3, errorResilientPartitions: true, extraArgs: []string{"--error-resilient=2", "--token-parts=3"}, flags: repeatFlag(frames-1, EncodeNoUpdateLast|EncodeNoUpdateGolden|EncodeNoUpdateAltRef)},
		{name: "no-upd-entropy-no-upd-all-er3-token8-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, tokenParts: 3, errorResilient: true, errorResilientPartitions: true, extraArgs: []string{"--error-resilient=3", "--token-parts=3"}, flags: repeatFlag(frames-1, EncodeNoUpdateEntropy|EncodeNoUpdateLast|EncodeNoUpdateGolden|EncodeNoUpdateAltRef)},
		{name: "no-upd-entropy-no-upd-all-er3-token8-realtime-cpu0-65x33", deadline: DeadlineRealtime, cpuUsed: 0, fx: panningOdd, tokenParts: 3, errorResilient: true, errorResilientPartitions: true, extraArgs: []string{"--error-resilient=3", "--token-parts=3"}, flags: repeatFlag(frames-1, EncodeNoUpdateEntropy|EncodeNoUpdateLast|EncodeNoUpdateGolden|EncodeNoUpdateAltRef)},
		{name: "invisible-no-ref-all-odd-dims-realtime-cpu-3-65x33", deadline: DeadlineRealtime, cpuUsed: -3, fx: panningOdd, flags: repeatFlag(frames-1, EncodeInvisibleFrame|EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef)},
		{name: "no-upd-last-no-ref-gf-odd-dims-realtime-cpu0-65x33", deadline: DeadlineRealtime, cpuUsed: 0, fx: panningOdd, flags: repeatFlag(frames-1, EncodeNoUpdateLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef)},
		{name: "no-ref-gf-no-upd-last-token4-odd-dims-realtime-cpu0-65x33", deadline: DeadlineRealtime, cpuUsed: 0, fx: panningOdd, tokenParts: 2, extraArgs: []string{"--token-parts=2"}, flags: repeatFlag(frames-1, EncodeNoReferenceGolden|EncodeNoUpdateLast)},
		{name: "force-gf-arf-token4-odd-dims-realtime-cpu0-65x33", deadline: DeadlineRealtime, cpuUsed: 0, fx: panningOdd, tokenParts: 2, extraArgs: []string{"--token-parts=2"}, flags: []EncodeFlags{0, 0, EncodeForceGoldenFrame, 0, EncodeForceAltRefFrame}},

		// Force-KF + no-update-GF/ARF (the layer-0 "I-frame anchor"
		// pattern used by 3-layer SVC mode 4 in libvpx's example).
		{name: "force-kf-no-upd-gf-arf-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{EncodeForceKeyFrame | EncodeNoUpdateGolden | EncodeNoUpdateAltRef}},
		{name: "force-kf-frame3-realtime-vbr-cpu-3-16x16", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, rcMode: RateControlVBR, rcModeSet: true, extraArgs: []string{"--end-usage=vbr"}, flags: []EncodeFlags{0, 0, 0, EncodeForceKeyFrame}},
		{name: "force-kf-frame3-realtime-q-cpu-3-16x16-q20", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, rcMode: RateControlQ, rcModeSet: true, cqLevel: 20, extraArgs: []string{"--end-usage=q", "--cq-level=20"}, flags: []EncodeFlags{0, 0, 0, EncodeForceKeyFrame}},

		// Token-partitions=2 / 4 crossed with per-frame flags to
		// confirm the partitioned writer also honors per-frame refs.
		{name: "force-kf-frame3-realtime-cpu0-32x32-4partitions", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, tokenParts: 2, extraArgs: []string{"--token-parts=2"}, flags: []EncodeFlags{0, 0, 0, EncodeForceKeyFrame}},
		{name: "no-upd-last-realtime-cpu0-32x32-4partitions", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, tokenParts: 2, extraArgs: []string{"--token-parts=2"}, flags: repeatFlag(frames-1, EncodeNoUpdateLast)},

		// Good-quality deadline + per-frame flags to widen the
		// picker's mode-decision coverage.
		{name: "force-kf-frame3-good-quality-cpu4-32x32", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: panning32, flags: []EncodeFlags{0, 0, 0, EncodeForceKeyFrame}},
		{name: "no-upd-last-good-quality-cpu4-16x16", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: panning16, flags: repeatFlag(frames-1, EncodeNoUpdateLast)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]Image, frames)
			for i := range sources {
				sources[i] = tc.fx.source(tc.fx.w, tc.fx.h, i)
			}
			rcMode := tc.rcMode
			if !tc.rcModeSet {
				rcMode = RateControlCBR
			}
			opts := EncoderOptions{
				Width:                    tc.fx.w,
				Height:                   tc.fx.h,
				FPS:                      fps,
				RateControlMode:          rcMode,
				TargetBitrateKbps:        targetKbps,
				MinQuantizer:             4,
				MaxQuantizer:             56,
				CQLevel:                  tc.cqLevel,
				KeyFrameInterval:         999,
				Deadline:                 tc.deadline,
				CpuUsed:                  tc.cpuUsed,
				Tuning:                   TunePSNR,
				TokenPartitions:          tc.tokenParts,
				ErrorResilient:           tc.errorResilient,
				ErrorResilientPartitions: tc.errorResilientPartitions,
			}
			if tc.disableKf {
				opts.KeyFrameInterval = 0
			}

			govpxFrames := encodeFramesWithGovpxFrameFlags(t, opts, sources, tc.flags)
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, tc.name, opts, targetKbps, sources, tc.flags, tc.extraArgs)

			if len(govpxFrames) != len(libvpxFrames) {
				if tc.limit < 0 {
					t.Logf("frame count mismatch (not asserted, known gap): govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
					return
				}
				t.Fatalf("frame count mismatch: govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
			}

			limit := len(govpxFrames)
			switch {
			case tc.limit < 0:
				limit = 0
			case tc.limit > 0 && tc.limit < limit:
				limit = tc.limit
			}
			for i := 0; i < len(govpxFrames); i++ {
				gHash := sha256.Sum256(govpxFrames[i])
				lHash := sha256.Sum256(libvpxFrames[i])
				gFP, gIsKey := parseVP8FramePartitionSizes(govpxFrames[i])
				lFP, lIsKey := parseVP8FramePartitionSizes(libvpxFrames[i])
				if gHash == lHash {
					t.Logf("frame %d byte MATCH: len=%d first_part=%d keyframe=%t", i, len(govpxFrames[i]), gFP, gIsKey)
					continue
				}
				firstDiff := firstByteDiff(govpxFrames[i], libvpxFrames[i])
				firstNonTagDiff := firstByteDiff(govpxFrames[i][3:], libvpxFrames[i][3:])
				if firstNonTagDiff >= 0 {
					firstNonTagDiff += 3
				}
				if i >= limit {
					t.Logf("frame %d byte mismatch (not asserted, limit=%d): govpx_len=%d libvpx_len=%d first_diff=%d non_tag_diff=%d govpx_first_part=%d libvpx_first_part=%d",
						i, limit, len(govpxFrames[i]), len(libvpxFrames[i]), firstDiff, firstNonTagDiff, gFP, lFP)
					continue
				}
				t.Errorf("frame %d byte mismatch: govpx_len=%d libvpx_len=%d first_diff=%d govpx_first_part=%d libvpx_first_part=%d govpx_keyframe=%t libvpx_keyframe=%t govpx_sha=%s libvpx_sha=%s",
					i, len(govpxFrames[i]), len(libvpxFrames[i]), firstDiff,
					gFP, lFP, gIsKey, lIsKey,
					hex.EncodeToString(gHash[:8]), hex.EncodeToString(lHash[:8]))
			}
		})
	}
}

func TestOracleEncoderStreamByteParityForceKeyFrameAPI(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run force-key API byte-parity gate")
	}
	driver := findVpxencFrameFlags(t)

	const (
		fps        = 30
		targetKbps = 700
		width      = 32
		height     = 16
	)

	cases := []struct {
		name            string
		frames          int
		lookaheadFrames int
		forceFrames     map[int]bool
		mutate          func(*EncoderOptions)
		buildFlags      func(int, map[int]bool) []EncodeFlags
		apiFlags        func([]EncodeFlags) []EncodeFlags
		setup           func(*testing.T, *VP8Encoder)
		runtimeApply    map[int]func(*testing.T, *VP8Encoder)
		controlScript   []string
		extraArgs       []string
		matchLimit      int
	}{
		{
			name:        "no-lookahead-frame1-and4",
			frames:      8,
			forceFrames: map[int]bool{1: true, 4: true},
		},
		{
			name:            "lookahead2-frame1-and4",
			frames:          8,
			lookaheadFrames: 2,
			forceFrames:     map[int]bool{1: true, 4: true},
			extraArgs:       []string{"--lag-in-frames=2"},
		},
		{
			name:            "lookahead4-frame4-and-flush",
			frames:          10,
			lookaheadFrames: 4,
			forceFrames:     map[int]bool{4: true, 9: true},
			extraArgs:       []string{"--lag-in-frames=4"},
		},
		{
			name:            "lookahead4-auto-alt-ref-frame4-and-flush",
			frames:          10,
			lookaheadFrames: 4,
			forceFrames:     map[int]bool{4: true, 9: true},
			mutate: func(opts *EncoderOptions) {
				opts.AutoAltRef = true
			},
			extraArgs: []string{"--lag-in-frames=4", "--auto-alt-ref=1"},
		},
		{
			name:        "active-map-checker-frame1-and4",
			frames:      8,
			forceFrames: map[int]bool{1: true, 4: true},
			setup:       activeMapApply("checker"),
			extraArgs:   []string{"--active-map=checker"},
		},
		{
			name:        "roi-map-border-frame1-and4",
			frames:      8,
			forceFrames: map[int]bool{1: true, 4: true},
			setup:       roiMapApply("border1"),
			extraArgs:   []string{"--roi-map=border1"},
		},
		{
			name:        "temporal-two-layer-frame1-and4",
			frames:      8,
			forceFrames: map[int]bool{1: true, 4: true},
			mutate: func(opts *EncoderOptions) {
				opts.TemporalScalability = TemporalScalabilityConfig{
					Enabled:                true,
					Mode:                   TemporalLayeringTwoLayers,
					LayerTargetBitrateKbps: [MaxTemporalLayers]int{420, targetKbps},
				}
			},
			buildFlags: forceKeyTemporalTwoLayerFlags,
			extraArgs: []string{
				"--temporal-layers=2",
				"--temporal-bitrates=420,700",
				"--temporal-decimators=2,1",
				"--temporal-periodicity=2",
				"--temporal-layer-ids=0,1",
			},
		},
		{
			name:        "drop-frame-enabled-frame1-and4",
			frames:      8,
			forceFrames: map[int]bool{1: true, 4: true},
			mutate: func(opts *EncoderOptions) {
				opts.DropFrameAllowed = true
				opts.DropFrameWaterMark = 60
			},
			extraArgs: []string{"--drop-frame=60"},
		},
		{
			name:        "keyframes-disabled-frame1-and4",
			frames:      8,
			forceFrames: map[int]bool{1: true, 4: true},
			mutate: func(opts *EncoderOptions) {
				opts.KeyFrameInterval = 0
			},
			extraArgs: []string{"--kf-disabled"},
		},
		{
			name:        "no-update-entropy-frame1-and4",
			frames:      8,
			forceFrames: map[int]bool{1: true, 4: true},
			buildFlags: func(frames int, forceFrames map[int]bool) []EncodeFlags {
				flags := repeatFlag(frames-1, EncodeNoUpdateEntropy)
				for frame := range forceFrames {
					if frame >= 0 && frame < len(flags) {
						flags[frame] |= EncodeForceKeyFrame
					}
				}
				return flags
			},
			apiFlags: forceKeyAPIEncodeFlags,
		},
		{
			name:        "no-update-last-no-ref-gf-arf-frame1-and4",
			frames:      8,
			forceFrames: map[int]bool{1: true, 4: true},
			buildFlags: func(frames int, forceFrames map[int]bool) []EncodeFlags {
				flags := repeatFlag(frames-1, EncodeNoUpdateLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef)
				for frame := range forceFrames {
					if frame >= 0 && frame < len(flags) {
						flags[frame] |= EncodeForceKeyFrame
					}
				}
				return flags
			},
			apiFlags: forceKeyAPIEncodeFlags,
		},
		{
			name:        "force-golden-altref-frame1-and4",
			frames:      8,
			forceFrames: map[int]bool{1: true, 4: true},
			buildFlags: func(frames int, forceFrames map[int]bool) []EncodeFlags {
				flags := make([]EncodeFlags, frames)
				for frame := range forceFrames {
					if frame >= 0 && frame < len(flags) {
						flags[frame] = EncodeForceKeyFrame | EncodeForceGoldenFrame | EncodeForceAltRefFrame
					}
				}
				return flags
			},
			apiFlags: forceKeyAPIEncodeFlags,
		},
		{
			name:        "rtc-external-frame1-and4",
			frames:      8,
			forceFrames: map[int]bool{1: true, 4: true},
			mutate: func(opts *EncoderOptions) {
				opts.RTCExternalRateControl = true
			},
			extraArgs: []string{"--rtc-external=1"},
		},
		{
			name:        "set-reference-before-force-keyframe",
			frames:      8,
			forceFrames: map[int]bool{3: true, 6: true},
			runtimeApply: map[int]func(*testing.T, *VP8Encoder){
				2: setReferencePanningApply(ReferenceLast, 8, "last"),
				5: setReferencePanningApply(ReferenceGolden, 9, "golden"),
			},
			controlScript: runtimeControlScript(8, map[int]string{
				2: "setref:last:panning:8",
				5: "setref:golden:panning:9",
			}),
		},
		{
			name:        "rtc-external-set-reference-before-force-keyframe",
			frames:      8,
			forceFrames: map[int]bool{3: true, 6: true},
			runtimeApply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
				},
				2: setReferencePanningApply(ReferenceLast, 8, "last"),
				5: setReferencePanningApply(ReferenceGolden, 9, "golden"),
			},
			controlScript: runtimeControlScript(8, map[int]string{
				1: "rtc:1",
				2: "setref:last:panning:8",
				5: "setref:golden:panning:9",
			}),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]Image, tc.frames)
			for i := range sources {
				sources[i] = encoderValidationPanningFrame(width, height, i)
			}
			opts := EncoderOptions{
				Width:             width,
				Height:            height,
				FPS:               fps,
				RateControlMode:   RateControlCBR,
				TargetBitrateKbps: targetKbps,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				KeyFrameInterval:  999,
				Deadline:          DeadlineRealtime,
				CpuUsed:           0,
				Tuning:            TunePSNR,
				LookaheadFrames:   tc.lookaheadFrames,
			}
			if tc.mutate != nil {
				tc.mutate(&opts)
			}
			flags := make([]EncodeFlags, tc.frames)
			if tc.buildFlags != nil {
				flags = tc.buildFlags(tc.frames, tc.forceFrames)
			} else {
				for frame := range tc.forceFrames {
					flags[frame] = EncodeForceKeyFrame
				}
			}

			apiFlags := make([]EncodeFlags, len(flags))
			if tc.apiFlags != nil {
				apiFlags = tc.apiFlags(flags)
			}
			govpxFrames := encodeFramesWithGovpxForceKeyScheduleFlagsSetupAndApply(t, opts, sources, tc.forceFrames, apiFlags, tc.setup, tc.runtimeApply)
			extraArgs := append([]string(nil), tc.extraArgs...)
			if tc.controlScript != nil {
				extraArgs = append(extraArgs, "--control-script="+strings.Join(tc.controlScript, ","))
			}
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, "force-key-api-"+tc.name, opts, opts.TargetBitrateKbps, sources, flags, extraArgs)
			assertSegmentByteParity(t, "force-key-api", govpxFrames, libvpxFrames, tc.matchLimit)
		})
	}
}

func forceKeyTemporalTwoLayerFlags(frames int, forceFrames map[int]bool) []EncodeFlags {
	flags := temporalTwoLayerFlags(frames)
	for frame := range forceFrames {
		if frame >= 0 && frame < len(flags) {
			flags[frame] |= EncodeForceKeyFrame
		}
	}
	return flags
}

func forceKeyAPIEncodeFlags(flags []EncodeFlags) []EncodeFlags {
	out := make([]EncodeFlags, len(flags))
	for i, flag := range flags {
		out[i] = flag &^ EncodeForceKeyFrame
	}
	return out
}

func encodeFramesWithGovpxForceKeySchedule(t *testing.T, opts EncoderOptions, sources []Image, forceFrames map[int]bool) [][]byte {
	t.Helper()
	return encodeFramesWithGovpxForceKeyScheduleAndSetup(t, opts, sources, forceFrames, nil)
}

func encodeFramesWithGovpxForceKeyScheduleAndSetup(t *testing.T, opts EncoderOptions, sources []Image, forceFrames map[int]bool, setup func(*testing.T, *VP8Encoder)) [][]byte {
	t.Helper()
	return encodeFramesWithGovpxForceKeyScheduleFlagsAndSetup(t, opts, sources, forceFrames, nil, setup)
}

func encodeFramesWithGovpxForceKeyScheduleFlagsAndSetup(t *testing.T, opts EncoderOptions, sources []Image, forceFrames map[int]bool, flags []EncodeFlags, setup func(*testing.T, *VP8Encoder)) [][]byte {
	t.Helper()
	return encodeFramesWithGovpxForceKeyScheduleFlagsSetupAndApply(t, opts, sources, forceFrames, flags, setup, nil)
}

func encodeFramesWithGovpxForceKeyScheduleFlagsSetupAndApply(t *testing.T, opts EncoderOptions, sources []Image, forceFrames map[int]bool, flags []EncodeFlags, setup func(*testing.T, *VP8Encoder), apply map[int]func(*testing.T, *VP8Encoder)) [][]byte {
	t.Helper()
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	defer enc.Close()
	if setup != nil {
		setup(t, enc)
	}
	buf := make([]byte, opts.Width*opts.Height*4+4096)
	out := make([][]byte, 0, len(sources))
	for i, src := range sources {
		if fn := apply[i]; fn != nil {
			fn(t, enc)
		}
		if forceFrames[i] {
			enc.ForceKeyFrame()
		}
		var frameFlags EncodeFlags
		if i < len(flags) {
			frameFlags = flags[i]
		}
		result, err := enc.EncodeInto(buf, src, uint64(i), 1, frameFlags)
		if errors.Is(err, ErrFrameNotReady) {
			continue
		}
		if err != nil {
			t.Fatalf("EncodeInto frame %d: %v", i, err)
		}
		if result.Dropped {
			t.Fatalf("frame %d dropped, want full stream", i)
		}
		out = append(out, append([]byte(nil), result.Data...))
	}
	for {
		result, err := enc.FlushInto(buf)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("FlushInto: %v", err)
		}
		if result.Dropped {
			t.Fatalf("flush packet dropped, want full stream")
		}
		out = append(out, append([]byte(nil), result.Data...))
	}
	return out
}

// repeatFlag returns a slice of length 1+n with index 0 set to 0
// (initial keyframe receives no flag) and indices 1..n set to f.
func repeatFlag(n int, f EncodeFlags) []EncodeFlags {
	out := make([]EncodeFlags, n+1)
	for i := 1; i <= n; i++ {
		out[i] = f
	}
	return out
}

// everyNFlag returns a per-frame schedule of length frames, skipping the
// initial keyframe and setting f on every n-th inter frame.
func everyNFlag(frames int, n int, f EncodeFlags) []EncodeFlags {
	out := make([]EncodeFlags, frames)
	if n <= 0 {
		return out
	}
	for i := n; i < frames; i += n {
		out[i] = f
	}
	return out
}

func encodeFramesWithGovpxFrameFlags(t *testing.T, opts EncoderOptions, sources []Image, flags []EncodeFlags) [][]byte {
	t.Helper()
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	buf := make([]byte, opts.Width*opts.Height*4+4096)
	out := make([][]byte, 0, len(sources))
	for i, src := range sources {
		var f EncodeFlags
		if i < len(flags) {
			f = flags[i]
		}
		result, err := enc.EncodeInto(buf, src, uint64(i), 1, f)
		if errors.Is(err, ErrFrameNotReady) {
			continue
		}
		if err != nil {
			t.Fatalf("EncodeInto frame %d: %v", i, err)
		}
		if result.Dropped {
			t.Fatalf("frame %d dropped, want full stream", i)
		}
		out = append(out, append([]byte(nil), result.Data...))
	}
	for {
		result, err := enc.FlushInto(buf)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("FlushInto: %v", err)
		}
		if result.Dropped {
			t.Fatalf("flush packet dropped, want full stream")
		}
		out = append(out, append([]byte(nil), result.Data...))
	}
	return out
}

func encodeFramesWithFrameFlagsDriver(t *testing.T, driver, name string, opts EncoderOptions, targetKbps int, sources []Image, flags []EncodeFlags, extraArgs []string) [][]byte {
	t.Helper()
	dir := t.TempDir()
	yuvPath := filepath.Join(dir, name+".yuv")
	ivfPath := filepath.Join(dir, name+".ivf")
	writeEncoderValidationI420(t, yuvPath, sources)

	deadlineArg := "good"
	switch opts.Deadline {
	case DeadlineBestQuality:
		deadlineArg = "best"
	case DeadlineRealtime:
		deadlineArg = "rt"
	}
	endUsage := "cbr"
	switch opts.RateControlMode {
	case RateControlVBR:
		endUsage = "vbr"
	case RateControlCQ:
		endUsage = "cq"
	case RateControlQ:
		endUsage = "q"
	}
	flagsCSV := make([]string, len(sources))
	invisibleCSV := make([]string, len(sources))
	haveInvisible := false
	for i := range flagsCSV {
		var f EncodeFlags
		if i < len(flags) {
			f = flags[i]
		}
		flagsCSV[i] = strconv.FormatUint(uint64(frameFlagsForLibvpx(f)), 10)
		if f&EncodeInvisibleFrame != 0 {
			invisibleCSV[i] = "1"
			haveInvisible = true
		} else {
			invisibleCSV[i] = "0"
		}
	}

	args := []string{
		"--infile=" + yuvPath,
		"--outfile=" + ivfPath,
		"--width=" + strconv.Itoa(opts.Width),
		"--height=" + strconv.Itoa(opts.Height),
		"--fps-num=" + strconv.Itoa(opts.FPS),
		"--fps-den=1",
		"--frames=" + strconv.Itoa(len(sources)),
		"--target-bitrate=" + strconv.Itoa(targetKbps),
		"--min-q=" + strconv.Itoa(opts.MinQuantizer),
		"--max-q=" + strconv.Itoa(opts.MaxQuantizer),
		"--kf-min-dist=999",
		"--kf-max-dist=999",
		"--deadline=" + deadlineArg,
		"--cpu-used=" + strconv.Itoa(opts.CpuUsed),
		"--end-usage=" + endUsage,
		"--auto-alt-ref=0",
		"--token-parts=" + strconv.Itoa(opts.TokenPartitions),
		"--frame-flags=" + strings.Join(flagsCSV, ","),
	}
	if haveInvisible {
		args = append(args, "--invisible-frames="+strings.Join(invisibleCSV, ","))
	}
	if opts.CQLevel > 0 {
		args = append(args, "--cq-level="+strconv.Itoa(opts.CQLevel))
	}
	args = append(args, extraArgs...)
	cmd := exec.Command(driver, args...)
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("vpxenc-frameflags failed: %v\n%s", err, out)
	}
	data, err := os.ReadFile(ivfPath)
	if err != nil {
		t.Fatalf("read %s: %v", ivfPath, err)
	}
	return parseIVFFramePayloads(t, data)
}
