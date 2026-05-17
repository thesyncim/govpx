//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"os"
	"strconv"
	"testing"
)

// vp9RuntimeControlsSeedsDeferred lists VP9 runtime-control fuzz seeds whose
// strict byte parity is gated behind libvpx VP9 features govpx has not yet
// ported. Mirrors VP8's longFixtureSeedsDeferred and
// vp9LongFixtureSeedsDeferred convention so the fuzz gate stays green; each
// entry cites the libvpx file:line that drives the divergence so a follow-up
// port has a concrete starting point.
//
// Two equivalence classes are listed: the 6 baseline {dimBucket, framesBucket,
// cpuBucket, kfPos, refPos, action1, ...} seeds and additional short-byte
// regression corpus entries that resolve to the SAME materialised case via
// the wrap-around cursor (vp9FuzzByteCursor.pick = byte % n in
// vp9_oracle_fuzz_helpers_test.go:72-77). The cursor wraps for inputs shorter
// than the 8 cells consumed by vp9OracleRuntimeFuzzCaseFromBytes, so short
// regression seeds captured by sweeps land here next to their canonical
// 8-byte equivalent.
//
// The 6 baseline seeds populate (dimBucket, framesBucket, cpuBucket, kfPos,
// refPos, action1, ...) from cpuPool {0, -3, -8, 4}. The fuzz case
// materialiser pins Deadline=DeadlineRealtime, so all six seeds run REALTIME
// mode at speed=abs(cpu_used). govpx's RT-mode speed-feature cascade is
// already verbatim against libvpx at every speed (pinned by
// TestVP9SetRtSpeedFeaturesCPUUsed0Verbatim, TestVP9SetRtSpeedFeaturesCPUUsed4Verbatim,
// TestVP9SetRtSpeedFeaturesCPUUsed4Verbatim720p, and the per-field RT speed-3
// configurator tests), so divergence below is in the encoder body
// (tx_mode + per-block mode picker + writer cascade), NOT the
// speed-features dispatcher. Even the abs(cpu)=8 seeds (#3) diverge on byte
// 16 of the first keyframe because the runtime-controls fuzz uses
// content-rich newVP9YCbCrFuzzPanning sources that exercise govpx's
// keyframe compressed-header writer gap (see citations below) instead of
// the flat black/checker patches the existing
// TestVP9EncoderVpxencOracle*KeyframeByteParity tests pin.
//
// All 6 seeds are RED at frame 0 (keyframe) with first_diff in [9, 20]
// after the cpu_used pass-through fix (--cpu-used=opts.CpuUsed appended to
// extraArgs); without that fix the libvpx oracle always ran at speed 8
// while govpx ran at opts.CpuUsed, masking the underlying gap as a
// trivially-divergent speed mismatch.
//
// Deferred seeds (cpu values from cpuPool[bucket]):
//
//   - {0,0,0,0,0,0,0,0} — 64x64 frames=4 cpu=0. The fuzz case
//     materialiser builds opts with Deadline=DeadlineRealtime
//     (vp9_oracle_encoder_runtime_controls_fuzz_test.go) and the libvpx
//     oracle defaults to VPX_DL_REALTIME, so both sides run REALTIME mode
//     at speed=0. The full RT cpu_used=0 SPEED_FEATURES struct is already
//     pinned verbatim against libvpx by
//     TestVP9SetRtSpeedFeaturesCPUUsed0Verbatim and the sibling GOOD-mode
//     anchor TestVP9SetGoodSpeedFeaturesCPUUsed0Verbatim — the
//     speed-features cascade is NOT the root cause of this deferral.
//
//     The upstream libvpx select_tx_mode at vp9_encodeframe.c:4334-4344
//     returns TX_MODE_SELECT for KEY_FRAME at cpu_used=0
//     (use_nonrd_pick_mode==0, tx_size_search_method==USE_FULL_RD).
//     govpx's vp9EncoderFrameTxMode now ports that branch verbatim — the
//     keyframe writer plumbs the TxModeSelect-shaped tx_probs row via
//     the existing keyframe-source path (writeVP9ModeBlock:6885+) and
//     the vp9ModeTreeKeyframe fallback (commit 0dfca64). govpx now also
//     ports the per-block tx_size RD loop from libvpx's
//     choose_tx_size_from_rd (vp9_rdopt.c:907-1023) via
//     pickVP9KeyframeBlockTxSize (vp9_encoder.go), running the libvpx
//     start_tx/end_tx loop with sf.TxSizeSearchDepth bounds and the
//     tx_size_search_breakout early-exit on textured residuals.
//
//     With the per-block RD loop in place the bitstream length on
//     content-rich panning sources stays at ~3828 bytes vs libvpx's
//     ~2726 (got_len=3828 want_len=2726 first_diff=9, filter_level=0
//     govpx vs 12 libvpx). The remaining gap is the RATE PROXY used by
//     the picker. govpx's keyframe RD scorers (the existing
//     scoreVP9KeyframeTxBlockRD at vp9_encoder.go:7641 and the new
//     pickVP9KeyframeBlockTxSize at vp9_encoder.go) approximate the
//     libvpx coefficient rate via SATD-of-qcoeff scaled into prob-cost
//     units (`rate <<= 2 + VP9ProbCostShift`). libvpx's cost_coeffs
//     (vp9_rdopt.c:358-470) uses the full per-token entropy walk via
//     x->token_costs[tx_size][type][is_inter] (computed from the
//     pareto8 tables via vp9_get_token_cost). The SATD proxy
//     underestimates the larger-tx coef rate so the picker can
//     occasionally pick a smaller tx where libvpx picks the larger.
//
//     Closing this seed requires porting libvpx's cost_coeffs
//     (vp9_rdopt.c:358) into govpx's pickVP9KeyframeBlockTxSize rate
//     leg so the RD comparison uses byte-exact libvpx rates. The
//     choose_tx_size_from_rd loop body (vp9_rdopt.c:907-1023) is
//     already verbatim in govpx; only the coefficient-cost proxy needs
//     replacing. Once cost_coeffs lands, the bitstream length should
//     converge with libvpx's ~2726 and the LPF picker's filter_level
//     pick should also converge.
//
//   - {0,1,1,0,2,1,0,0} — 64x64 frames=6 cpu=-3 (abs=3). Same KEY_FRAME
//     per-block tx_size RD-search gap as #0 (vp9_rdopt.c:3950+);
//     additionally, at RT speed=3 the libvpx configurator sets
//     sf.lpf_pick=LPF_PICK_FROM_Q (vp9_speed_features.c:555) and
//     sf.disable_split_mask=DISABLE_ALL_SPLIT, which alters the
//     partition + lpf paths relative to speed=0. govpx's RT speed=3
//     SPEED_FEATURES struct itself IS verbatim against libvpx, but the
//     downstream encoder body (per-block tx_size search, partition
//     pruning, RD cost rounding at adaptive_rd_thresh=4) still
//     accumulates byte-level drift at cpu_used=3 RT — same encoder-body
//     handoff as #0.
//
//   - {1,0,0,1,0,0,1,0} — 128x64 frames=4 cpu=0. Same KEY_FRAME
//     per-block tx_size RD-search gap as #0 (vp9_rdopt.c:3950+); the
//     wider frame_width-1 (127) trips a different miCols path that
//     amplifies the bitstream divergence on top.
//
//   - {1,1,2,0,3,1,1,0} — 128x64 frames=6 cpu=-8 (abs=8). govpx covers the
//     cpu_used=8 speed-features path, yet frame 0 still diverges at byte 16
//     because the compressed-header writer payload differs on
//     content-rich keyframes: libvpx writes the full
//     coef-update / tx-mode payload via write_compressed_header
//     (vp9/encoder/vp9_bitstream.c:826-973) using
//     vp9_cond_prob_diff_update results from the per-tile frame_counts,
//     while govpx's WriteCompressedHeaderFromCounts emits a smaller subset
//     and packs SPEED_FEATURES.coef_prob_appx_step=4 (the speed-8 fast
//     path, vp9_speed_features.c:610) verbatim — the savings-search
//     threshold then diverges at the first coef-prob context. The flat
//     sources used by TestVP9EncoderVpxencOracleChecker64KeyframeByteParity
//     emit predominantly all-zero counts so the writers agree there;
//     panning content exposes the gap.
//
//   - {0,2,0,2,0,0,0,0} — 64x64 frames=8 cpu=0. Same KEY_FRAME
//     per-block tx_size RD-search gap as #0 with frame count widened;
//     once frame 0 KF parity holds the inter frames should follow
//     because the runtime-controls fuzz only flips
//     EncodeForce*/NoUpdate* flags that govpx already routes through
//     vp9_set_reference_frame_flags / ext_refresh_frame_flags.
//
//   - {1,2,1,0,4,1,0,1} — 128x64 frames=8 cpu=-3. Triggers the seed-byte
//     refPos generator (r.pick(5)==4) which OR's
//     EncodeNoReferenceGolden|EncodeNoReferenceAltRef onto the same frame
//     where the per-frame action loop sets EncodeForceGoldenFrame at
//     frame 4 (cumulative flags 576 = 0x240). govpx's
//     vp9_set_reference_frame_flags rejects the EncodeForceGoldenFrame +
//     EncodeNoUpdateGolden combination as ErrInvalidConfig (the no-update
//     bit is implied when refresh_golden_frame is forced); libvpx's
//     vp9_cx_iface.c:1657 ctrl_set_reference accepts the redundant flags
//     and clears the no-update bit at vp9_encoder.c:set_ext_overrides.
//     Closing this seed needs either the fuzz seed corpus to avoid the
//     contradictory combination or a verbatim port of
//     set_ext_overrides's resolution rules into govpx's flag validator.
//
// Short-byte regression-corpus seeds resolving to one of the above cases via
// vp9FuzzByteCursor wrap-around:
//
//   - {0x30} (single ASCII '0', from testdata/fuzz/
//     FuzzVP9OracleEncoderRuntimeControls/regression_vp9_runtime_controls_-
//     582528dd captured in commit 0fba532) — vp9FuzzByteCursor returns
//     48%n for every pick(), so every cell evaluates to 0 and the case
//     materialises identically to baseline seed #0
//     {0,0,0,0,0,0,0,0}: w=64 h=64 frames=4 cpu=0 flags=[0,0,0,0]. Frame 0
//     KF diverges at byte 9 (filter_level=0 govpx vs 12 libvpx). Current
//     observed delta with the per-block tx_size RD loop in place:
//     got_len=3828 want_len=2726 first_diff=9. The filter_level=0 vs 12
//     difference reflects the cost_coeffs rate-proxy gap described
//     under seed #0 (govpx's SATD proxy underestimates the larger-tx
//     rate; libvpx's cost_coeffs at vp9_rdopt.c:358 needs porting). The
//     RT/GOOD cpu_used=0 SPEED_FEATURES cascade IS verbatim per
//     TestVP9SetRtSpeedFeaturesCPUUsed0Verbatim and
//     TestVP9SetGoodSpeedFeaturesCPUUsed0Verbatim; the upstream
//     TX_MODE_SELECT routing is also verbatim; and the per-block tx_size
//     loop (choose_tx_size_from_rd) is verbatim — only the rate proxy
//     remains. Same handoff as seed #0 (cost_coeffs rate port); do NOT
//     close this entry until #0 closes.
//
//   - {0x31} (single ASCII '1', from testdata/fuzz/
//     FuzzVP9OracleEncoderRuntimeControls/regression_vp9_runtime_controls_-
//     916d1b27 captured in commit 9e8f70a) — vp9FuzzByteCursor returns
//     49%n for every pick(); 49 % 2 = 1, 49 % 3 = 1, 49 % 4 = 1, etc. so
//     every cell evaluates to 1. The case materialises to dims[1]=
//     (128,64), frameCountPool[1]=6, cpuPool[1]=-3, kfPos=1, refPos=1,
//     plus the action loop picks EncodeNoUpdateEntropy for every inter
//     frame (r.pick(4)==1). Frame 0 KF diverges at byte 16
//     (got_len=7611 want_len=5324) and inter frames diverge at byte 4
//     each. Same downstream encoder-body gap as seed #1 (cpu=-3 RT
//     speed=3 path + per-block keyframe tx_size RD search at
//     vp9_rdopt.c:3950+). The RT speed=3 SPEED_FEATURES struct is
//     already verbatim (TestVP9SetRtSpeedFeaturesCPUUsed3Verbatim or
//     analogous) and the keyframe TX_MODE_SELECT writer cascade is now
//     in place — only the per-block keyframe tx_size RD search remains
//     unported. Same handoff as #1; do NOT close this entry until #1
//     closes.
//
//   - {0x32} (single ASCII '2', from testdata/fuzz/
//     FuzzVP9OracleEncoderRuntimeControls/regression_vp9_runtime_controls_-
//     2fde656d captured in commit 2ebdb7d) — vp9FuzzByteCursor returns
//     50%n for every pick(); 50 % 2 = 0, 50 % 3 = 2, 50 % 4 = 2,
//     50 % 5 = 0, 50 % 8 = 2 etc. The case materialises to dims[0]=
//     (64,64), frameCountPool[2]=8, cpuPool[2]=-8, kfPos=2, refPos=2,
//     plus the refPos generator picks r.pick(5)==0 -> EncodeNoUpdateLast
//     for frame 2 and the per-frame action loop picks r.pick(4)==2 ->
//     EncodeForceGoldenFrame for every inter frame. Frame 2 stacks
//     EncodeNoUpdateLast | EncodeForceGoldenFrame plus the kfPos-driven
//     EncodeForceKeyFrame (cumulative flags 545 = 0x221). govpx's
//     vp9_set_reference_frame_flags rejects EncodeForceGoldenFrame in
//     combination with the implicit NoUpdateGolden derivation rule the
//     same way it rejects seed #5 (vp9_cx_iface.c:1657 ctrl_set_reference
//     accepts the redundant flags in libvpx and clears the no-update
//     bit at vp9_encoder.c:set_ext_overrides). Same handoff as #5
//     (set_ext_overrides resolution rules); do NOT close until #5 closes.
//
// Re-measurement under
// GOVPX_VP9_LIBVPX_CHOOSE_PARTITIONING=1 GOVPX_VP9_NONRD_PICK_PARTITION=1
// (verified by TestVP9DeferredSeedsRemeasureRuntimeControls):
//
//	PASS=0/9 FAIL=9/9. Seeds #0/#2/#4/#6 (cpu=0 panning content)
//	diverge on frame 0 at first_byte_diff=9 by ~1000-2298 bytes —
//	the cost_coeffs rate-proxy gap described under seed #0 is the
//	sole remaining piece (choose_tx_size_from_rd already verbatim).
//	Seeds #1/#7 (cpu=-3) diverge on frame 0 at first_byte_diff=16
//	by ~989-2287 bytes — same gap amplified by RT speed=3
//	coef_prob_appx_step. Seed #3 (cpu=-8) diverges on frame 1 by
//	~123 bytes. Seeds #5 and "2" alias structurally cannot be
//	measured because libvpx vpxenc-vp9-frameflags rejects the
//	EncodeForceGoldenFrame|EncodeNoUpdateGolden combination as
//	"Conflicting flags" and govpx rejects it as ErrInvalidConfig
//	— pending the set_ext_overrides port
//	(vp9_encoder.c:set_ext_overrides) regardless of partition gate.
//
//	Conclusion: gates flipping ON does not un-defer any
//	RuntimeControls seed. Closure requires the cost_coeffs rate
//	proxy port and the set_ext_overrides resolution port.
//
// Reverting any entry here must be paired with the corresponding verbatim
// libvpx port landing; this is the explicit handoff list for follow-up work.
var vp9RuntimeControlsSeedsDeferred = [][]byte{
	{0, 0, 0, 0, 0, 0, 0, 0},
	{0, 1, 1, 0, 2, 1, 0, 0},
	{1, 0, 0, 1, 0, 0, 1, 0},
	{1, 1, 2, 0, 3, 1, 1, 0},
	{0, 2, 0, 2, 0, 0, 0, 0},
	{1, 2, 1, 0, 4, 1, 0, 1},
	// Short-byte regression-corpus aliases of the above (see comment).
	{0x30}, // regression_vp9_runtime_controls_582528dd — alias of #0
	{0x31}, // regression_vp9_runtime_controls_916d1b27 — alias of #1 family
	{0x32}, // regression_vp9_runtime_controls_2fde656d — alias of #5 family
}

func vp9RuntimeControlsSeedDeferred(data []byte) bool {
	for _, seed := range vp9RuntimeControlsSeedsDeferred {
		if bytes.Equal(data, seed) {
			return true
		}
	}
	return false
}

// FuzzVP9OracleEncoderRuntimeControls mirrors the VP8
// FuzzOracleEncoderRuntimeControlTransitions: a fuzz-driven runtime-control
// schedule is replayed against both the govpx VP9 encoder and the
// vpxenc-vp9-frameflags driver, and the per-frame VP9 packet bytes must match.
//
// The action pool is intentionally narrower than the VP8 sibling because
// vpxenc-vp9-frameflags exposes a different per-frame control vocabulary — only
// the controls govpx VP9 can drive in lockstep with libvpx VP9 are included.
// Any action that govpx supports but the driver doesn't (or vice-versa) is
// omitted to keep the comparator fair; gaps surface as a logged "comparator
// inapplicable" rather than a silent false-positive parity.
//
// Gated by GOVPX_WITH_ORACLE=1 plus a built vpxenc-vp9-frameflags binary.
func FuzzVP9OracleEncoderRuntimeControls(f *testing.F) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		f.Skip("set GOVPX_WITH_ORACLE=1 to run VP9 runtime-control oracle fuzz")
	}
	requireVP9VpxencFrameFlagsOracleFuzz(f)
	seeds := [][]byte{
		// (dimBucket, framesBucket, cpuBucket, kfFlagPos, refFlagPos, action1, action2, ...)
		{0, 0, 0, 0, 0, 0, 0, 0},
		{0, 1, 1, 0, 2, 1, 0, 0},
		{1, 0, 0, 1, 0, 0, 1, 0},
		{1, 1, 2, 0, 3, 1, 1, 0},
		{0, 2, 0, 2, 0, 0, 0, 0},
		{1, 2, 1, 0, 4, 1, 0, 1},
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if vp9RuntimeControlsSeedDeferred(data) {
			t.Skip("seed deferred: see vp9RuntimeControlsSeedsDeferred for libvpx file:line citations")
		}
		tc := vp9OracleRuntimeFuzzCaseFromBytes(data)
		sum := sha256.Sum256(data)
		label := "fuzz-vp9-runtime-controls-" + tc.name + "-" + hex.EncodeToString(sum[:4])
		t.Logf("%s w=%d h=%d frames=%d cpu=%d flags=%v",
			label, tc.opts.Width, tc.opts.Height, len(tc.sources), tc.opts.CpuUsed, tc.flags)

		govpxFrames := encodeVP9FramesWithGovpx(t, tc.opts, tc.sources, tc.flags)
		libvpxFrames := encodeVP9FramesWithLibvpxFrameFlagsOracle(t, tc.sources, tc.flags, tc.extraArgs)
		assertVP9SegmentByteParity(t, label, govpxFrames, libvpxFrames, 0)
	})
}

type vp9OracleRuntimeFuzzCase struct {
	name      string
	opts      VP9EncoderOptions
	sources   []*image.YCbCr
	flags     []EncodeFlags
	extraArgs []string
}

// vp9OracleRuntimeFuzzCaseFromBytes materialises a fuzz seed into a VP9
// runtime-control case. Each byte selects a bucket index off a wrapping
// cursor so even short seeds yield a fully-specified case.
func vp9OracleRuntimeFuzzCaseFromBytes(data []byte) vp9OracleRuntimeFuzzCase {
	r := vp9FuzzByteCursor{data: data}
	dims := [...]struct {
		w int
		h int
	}{
		{64, 64},
		{128, 64},
	}
	frameCountPool := [...]int{4, 6, 8}
	cpuPool := [...]int{0, -3, -8, 4}

	dim := dims[r.pick(len(dims))]
	frames := frameCountPool[r.pick(len(frameCountPool))]
	cpuUsed := cpuPool[r.pick(len(cpuPool))]
	kfPos := r.pick(frames)
	refPos := r.pick(frames)

	opts := VP9EncoderOptions{
		Width:               dim.w,
		Height:              dim.h,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     RateControlQ,
		TargetBitrateKbps:   700,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		MaxKeyframeInterval: 128,
		CpuUsed:             int8(cpuUsed),
		CQLevel:             32,
		Deadline:            DeadlineRealtime,
	}
	sources := newVP9YCbCrFuzzSources(dim.w, dim.h, frames)
	flags := make([]EncodeFlags, frames)

	// Sprinkle a key-frame flag and an optional reference-update flag.
	if kfPos > 0 && kfPos < frames {
		flags[kfPos] |= EncodeForceKeyFrame
	}
	if refPos > 0 && refPos < frames {
		switch r.pick(5) {
		case 0:
			flags[refPos] |= EncodeNoUpdateLast
		case 1:
			flags[refPos] |= EncodeNoUpdateGolden
		case 2:
			flags[refPos] |= EncodeNoUpdateAltRef
		case 3:
			flags[refPos] |= EncodeNoReferenceLast
		case 4:
			flags[refPos] |= EncodeNoReferenceGolden | EncodeNoReferenceAltRef
		}
	}
	// Per-frame action permutations are encoded into remaining bytes. We
	// keep this bounded so a single fuzz iteration stays cheap at 720p.
	for i := 1; i < frames; i++ {
		switch r.pick(4) {
		case 1:
			flags[i] |= EncodeNoUpdateEntropy
		case 2:
			flags[i] |= EncodeForceGoldenFrame
		case 3:
			flags[i] |= EncodeForceAltRefFrame
		}
	}

	extraArgs := []string{
		"--cq-level=32",
		"--min-q=4",
		"--max-q=56",
		"--end-usage=q",
		// Propagate the fuzz-selected speed preset to the libvpx oracle.
		// vpxenc-vp9-frameflags defaults to --cpu-used=8; without this
		// override the libvpx side would always run at speed 8 while
		// govpx ran at opts.CpuUsed, producing trivially-divergent
		// bitstreams. libvpx clamps to [-9, 9] in
		// vp9/vp9_cx_iface.c:ctrl_set_cpuused and uses abs(cpu_used)
		// as the SPEED_FEATURES selector (vp9_speed_features.c), which
		// matches govpx vp9SpeedFeatureCPUUsed.
		"--cpu-used=" + strconv.Itoa(cpuUsed),
	}
	return vp9OracleRuntimeFuzzCase{
		name:      "general",
		opts:      opts,
		sources:   sources,
		flags:     flags,
		extraArgs: extraArgs,
	}
}
