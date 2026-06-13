//go:build govpx_oracle_trace

package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

// TestVP9FullRDUsePartitionSeed0_1_1_0_1Frame1ByteParity is the headline
// milestone pin for the {0,1,1,0,1} long-fixture parity-gap seed (CBR 700 kbps
// kf=30 realtime cpu4, VAR_BASED_PARTITION, one-pass q=145). With the deep
// full-RD use-partition stack enabled, govpx serializes the FULL 256-frame
// fixture and reproduces frames 0..29 byte-identically to the pinned libvpx
// v1.16.0 vpxenc-vp9 oracle — advancing the seed's matched-frame prefix from 1
// (keyframe only) to >= 30 (keyframe + the first twenty-nine inter frames, up to
// the next key frame). This proves the genuine full-RD inter engine GENERALIZES
// across frames: frame 2 references frame 1's now-byte-exact reconstruction, and
// frames 3..29 exercise the GOLDEN refresh cadence, changing q, and the
// accumulated frame-context probability adaptation. The decoder-side
// FrameContext entering each inter frame is byte-identical between the govpx and
// libvpx streams (backward adaptation across 1..29 is correct).
//
// Frame 8 (the prior frontier, SB(0,0) mi(0,4) full-RD intra-vs-NEWMV-LAST) is
// now closed by the x->skip_encode search-context freeze (vp9_encoder_skip_-
// encode_search_ctx.go): when sf->skip_encode_frame is set (computed per frame
// from the previous frame's intra/inter counts, get_skip_encode_frame,
// vp9/encoder/vp9_encodeframe.c:5380-5391) and base_qindex < QIDX_SKIP_THRESH,
// libvpx's RD-search-phase intermediate encode_superblock (output_enabled==0)
// early-returns BEFORE advancing the entropy context (vp9_encodeframe.c:6112-
// 6115), so every leaf in the SB runs its RD search against the SB-ENTRY entropy
// context, not the running committed context. For frame 8 sf->skip_encode_frame
// flips to 1 (qidx=112), so libvpx searched mi(0,4) with left_context=0 and
// NEWMV-LAST mv=(-12,52) won; govpx had threaded mi(0,3)'s committed
// left_context=1 into the search, inflating the NEWMV coefficient cost so DC
// intra won. The freeze decouples the per-leaf search context (frozen) from the
// commit context (still threaded for the bitstream + next SB), matching libvpx
// exactly. Frame 1 (sf->skip_encode_frame==0, qidx=145, previous frame is the
// all-intra keyframe) is unaffected — its leaves still search the running
// threaded context, byte-for-byte as before.
//
// Frame 10 (the prior frontier, SB(0,0) mi(0,4) full-RD intra-vs-NEWMV-LAST) is
// now CLOSED by the intra mode_skip_start / ref_frame_skip_mask gate
// (vp9_rdopt.c:3679-3696,3624). At mi(0,4) frame 10 govpx's intra producer used
// to evaluate H_PRED (this_rd=4845079, dist=4587) and let it beat NEWMV-LAST
// (4883058), but libvpx never evaluates H_PRED there: H_PRED sits at
// vp9_mode_order index 22, past mode_skip_start (= sf->mode_skip_start + 1 = 7
// for cpu4), and once an inter mode (NEWMV-LAST) is the running best at
// midx == mode_skip_start, ref_frame_skip_mask[0] gets the INTRA_FRAME bit
// (LAST_FRAME_MODE_MASK includes (1 << INTRA_FRAME), vp9_rdopt.c:47-48), so
// every intra mode after DC (which alone sits at index 3 < mode_skip_start) is
// suppressed by the ref_frame_skip_mask continue. govpx's standalone intra
// producer was sweeping the whole speed-feature-masked intra set in one shot;
// the fix evaluates each intra Y mode at its own mode_order position under the
// same ref_frame_skip_mask / mode_threshold gates the inter candidates honour,
// so only DC competes once an inter mode wins (DC this_rd=5058519 > 4883058 →
// NEWMV-LAST commits mv=(2,18), byte-exact with libvpx). Closing frame 10 also
// closed frames 11..19, advancing the matched-frame prefix from 10 to 20.
//
// Frame 20 was the NEW first divergence (SB(0,0) mi(1,3) BLOCK_8X8) and is now
// CLOSED. The reference-management is NOT at fault: a private fprintf trace of
// libvpx's per-frame refresh_{last,golden,alt_ref}_frame, ref_frame_map[] and
// get_ref_frame_flags (vp9_encoder.c:3324-3393,4321-4339) confirms govpx and
// libvpx assign IDENTICAL buffers entering frame 20 — GOLDEN holds the frame-10
// recon (refreshed on the baseline_gf_interval=10 CBR cadence at frames 0/10/20,
// vp9_ratectrl.c:2518-2530), ALTREF holds the keyframe (refreshed only at the
// key frame; no ARF on this lag-0 realtime config), LAST holds frame 19. The
// per-ref candidate this_rd values also MATCH libvpx (NEARESTMV-LAST 1746567,
// NEARESTMV-GOLDEN 2031170, NEARESTMV-ALTREF 1631465) — the earlier "swapped
// GOLDEN/ALTREF" reading was wrong. The real divergence: govpx never evaluated
// NEWMV-ALTREF. pickVP9InterMvWithOptions (the MV-search wrapper) drops a (0,0)
// motion-search result (mv == MV{} -> !ok). libvpx's full-RD single-ref NEWMV
// (vp9_rdopt.c:2922-2929) rejects ONLY the INVALID_MV sentinel, so a (0,0) NEWMV
// is a legitimate distinct candidate (it codes a zero MV difference, unlike
// ZEROMV). At mi(1,3) libvpx's NEWMV-ALTREF search returns (0,0) (this_rd
// 1041546); that made best an ALTREF mode at midx == mode_skip_start, so
// ALT_REF_MODE_MASK opened the ALTREF single-ref modes and NEARMV-ALTREF(0,0)
// won (this_rd 1022271). govpx dropped NEWMV-ALTREF=(0,0), so best stayed
// NEWMV-LAST, LAST_FRAME_MODE_MASK masked GOLDEN/ALTREF, and govpx committed
// NEARMV-LAST mv=(16,-2). The fix calls the allow-zero search
// (pickVP9InterMvAllowZero) on the deep full-RD path so the zero NEWMV is scored
// exactly as libvpx; the non-RD leaf keeps the wrapper. This closed frames
// 20..29, advancing the matched-frame prefix 20 -> 30 (frame 30 is the next key
// frame and a fresh, distinct intra-class frontier). vp9_encoder_inter_modes.go
// (evaluateNewMVMode).
//
// The closure required eight libvpx-faithful ports on top of the already-closed
// 64/64 committed-mode decomposition (TestVP9FullRDUsePartitionSeed0_1_1_0_1Frame1):
//
//  1. The writer reads the full-RD-committed tx_size (interDecision.txSize from
//     choose_tx_size_from_rd) instead of re-deriving it with the realtime
//     pickVP9InterTxSize heuristic — libvpx encode_superblock reads mi->tx_size
//     (vp9/encoder/vp9_encodeframe.c:6100).
//  2. The genuine inter super_block_yrd / super_block_uvrd producers score in the
//     TRANSFORM domain (vp9_block_error on coeff/dqcoeff >> shift), matching
//     x->block_tx_domain == 1 for REALTIME speed >= 1 (vp9_encodeframe.c:2041-2048,
//     vp9_speed_features.c:486-489; vp9_rdopt.c:571-600). Y-only is insufficient —
//     the Y and UV producers must both use it or the NEARESTMV-vs-NEARMV tie at
//     mi(0,4) inverts.
//  3. The writer applies the per-Y-tx-block x->zcoeff_blk zero-forcing
//     (vp9/encoder/vp9_encodemb.c:580; decided by vp9_rdopt.c:835 rd1 > rd2) using
//     the per-frame rdmult, the search-time (pre-compressed-header-update) coef
//     probs (inter.selectFc), AND the same transform-domain distortion the RD
//     search used.
//  4. The committed block skip is best_skip2 || best_mode_skippable
//     (vp9_rdopt.c:4149,4173); the writer codes a skip block (no Y/UV residual,
//     recon == predictor) instead of re-deriving skip from the re-quantized
//     residual.
//  5. The inter-frame intra RD producer applies x->skip_encode (set for cpu4 at
//     base_qindex < QIDX_SKIP_THRESH, vp9_rdopt.c:3519): the intra prediction
//     reads its neighbour samples from the SOURCE plane, the inverse transform is
//     not added back into recon, and dist_block scores in the transform domain
//     with the mean_quant_error model term (vp9_encodemb.c:840-934,
//     vp9_rdopt.c:589-600). Without it govpx's intra DC residual at frame-8
//     mi(0,4) inverted (coeff[0] 412 vs 604); with it the intra coeffs and
//     distortion are byte-exact with libvpx. (See vp9InterUseDeepRDIntraSkipEncode.)
//  6. The x->skip_encode SEARCH-context freeze (encode_superblock early-return
//     at vp9_encodeframe.c:6112-6115): the per-leaf RD-search-phase intermediate
//     encode never advances pd->above_context / pd->left_context when
//     sf->skip_encode_frame is armed, so every leaf in the SB searches against
//     the SB-entry entropy context, decoupled from the (still-threaded) commit
//     context. This closed frame 8 (mi(0,4): NEWMV-LAST mv=(-12,52) over DC
//     intra). vp9_encoder_skip_encode_search_ctx.go.
//  7. The intra mode_skip_start / ref_frame_skip_mask gate (vp9_rdopt.c:3624,
//     3679-3696): the standalone larger-block intra producer now evaluates each
//     intra Y mode at its own vp9_mode_order index (DC at 3, TM at 15, H at 22,
//     V at 23, obliques at 24..29) under the same ref_frame_skip_mask /
//     mode_threshold gates the inter candidates honour, instead of sweeping the
//     whole speed-feature-masked intra set unconditionally at the first intra
//     entry. Once an inter mode is the running best at midx == mode_skip_start
//     (= sf->mode_skip_start + 1 = 7 for cpu4), ref_frame_skip_mask[0] gets the
//     INTRA_FRAME bit (LAST/GOLDEN/ALT_FRAME_MODE_MASK each include
//     (1 << INTRA_FRAME)), so every late intra mode is suppressed and only DC
//     (index 3 < mode_skip_start) competes. This closed frame 10 (mi(0,4):
//     NEWMV-LAST mv=(2,18) over a spurious H_PRED win) and frames 11..19,
//     advancing the matched-frame prefix 10 -> 20. For cpu0
//     (sf->mode_skip_start == MAX_MODES) the gate never fires, so all intra
//     modes are still evaluated, exactly as before. vp9_fullrd_inter_intra_sb.go
//     (vp9FullRDInterIntraSBState + per-mode EvalMode) + vp9_encoder_inter_-
//     modes.go (pickVP9FullRDInterReferenceMode intra branch).
//  8. The full-RD NEWMV keeps a (0,0) motion-search result. libvpx's single-ref
//     NEWMV (vp9_rdopt.c:2922-2929) rejects only the INVALID_MV sentinel, so a
//     (0,0) NEWMV is a legitimate distinct candidate. govpx's MV-search wrapper
//     pickVP9InterMvWithOptions drops a zero MV (mv == MV{} -> !ok), which is
//     correct for the non-RD leaf it serves but wrong for the full-RD path:
//     evaluateNewMVMode now calls the allow-zero search (pickVP9InterMvAllowZero)
//     on the vp9InterUseDeepRDRefBestRD branch. This closed frame 20 (mi(1,3):
//     NEWMV-ALTREF=(0,0) makes best an ALTREF mode at mode_skip_start, opening
//     ALT_REF_MODE_MASK so NEARMV-ALTREF(0,0) wins, this_rd 1022271) and frames
//     21..29, advancing the matched-frame prefix 20 -> 30. The non-RD path keeps
//     the wrapper, so the {0,0,0,0,0}/{1,0,0,0,0} non-RD seeds are unaffected.
//     vp9_encoder_inter_modes.go (evaluateNewMVMode).
//
// These ports are now production-default for the scoped VAR_BASED
// use-partition lane; the test still sets the guards explicitly so test order
// cannot hide the path. The seed remains in vp9LongFixtureParityGapSeeds because
// the full 256-frame clip still diverges at the next keyframe frontier.
func TestVP9FullRDUsePartitionSeed0_1_1_0_1Frame1ByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 64, 64

	saved := vp9InterUseDeepRDUsePartition
	savedRBR := vp9InterUseDeepRDRefBestRD
	savedTD := vp9InterUseDeepRDTxDomainDistortion
	savedSE := vp9InterUseDeepRDIntraSkipEncode
	defer func() {
		vp9InterUseDeepRDUsePartition = saved
		vp9InterUseDeepRDRefBestRD = savedRBR
		vp9InterUseDeepRDTxDomainDistortion = savedTD
		vp9InterUseDeepRDIntraSkipEncode = savedSE
	}()
	vp9InterUseDeepRDUsePartition = true
	vp9InterUseDeepRDRefBestRD = true
	vp9InterUseDeepRDTxDomainDistortion = true
	vp9InterUseDeepRDIntraSkipEncode = true

	opts := VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   700,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		MaxKeyframeInterval: 30,
		Deadline:            DeadlineRealtime,
		CpuUsed:             4,
	}
	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	// Encode the FULL {0,1,1,0,1} long fixture (256 panning frames) so the pin
	// asserts the generalized matched-frame prefix, not just the first inter
	// frame. With the deep full-RD use-partition stack the engine reproduces
	// frames 0..29 byte-for-byte; frame 30 is the first divergence (the next key
	// frame, a fresh intra-class frontier — see the header), so the prefix is
	// exactly 30.
	const fixtureFrames = 256
	sources := vp9test.NewPanningSources(width, height, fixtureFrames)
	dst := make([]byte, 1<<20)
	govpxFrames := make([][]byte, 0, len(sources))
	for i := range sources {
		res, err := e.EncodeIntoWithResult(sources[i], dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", i, err)
		}
		govpxFrames = append(govpxFrames, append([]byte(nil), res.Data...))
	}

	// --timebase=1/30 pins vpxenc-vp9's encoder framerate to exactly 30 (see the
	// vp9LongFixtureParityGapSeeds note); the rest mirror the {0,1,1,0,1} bucket.
	extraArgs := []string{
		"--end-usage=cbr",
		"--target-bitrate=700",
		"--cpu-used=4",
		"--kf-min-dist=0",
		"--kf-max-dist=30",
		"--buf-sz=600",
		"--buf-initial-sz=400",
		"--buf-optimal-sz=500",
		"--drop-frame=0",
		"--timebase=1/30",
	}
	libvpxFrames := vp9test.VpxencPackets(t, sources, extraArgs...)

	if len(govpxFrames) < 2 || len(libvpxFrames) < 2 {
		t.Fatalf("need >= 2 frames each, got govpx=%d libvpx=%d",
			len(govpxFrames), len(libvpxFrames))
	}

	// Headline: frame 1 (the first inter frame) is byte-exact.
	g1, l1 := govpxFrames[1], libvpxFrames[1]
	if fd := testutil.FirstByteDiff(g1, l1); fd != -1 || len(g1) != len(l1) {
		t.Fatalf("frame 1 NOT byte-exact: govpx=%d bytes libvpx=%d bytes "+
			"firstByteDiff=%d (want -1). The deep full-RD use-partition "+
			"tx_size/zcoeff_blk/transform-domain-distortion/skip ports regressed.",
			len(g1), len(l1), fd)
	}

	// Frame 0 (keyframe) must also stay byte-exact (it is, independently).
	if fd := testutil.FirstByteDiff(govpxFrames[0], libvpxFrames[0]); fd != -1 {
		t.Fatalf("frame 0 (keyframe) byte mismatch at offset %d", fd)
	}

	// The seed's matched-frame prefix must reach >= 30: frames 0 (keyframe) and
	// 1..29 (inter frames referencing the now-byte-exact reconstructions) all
	// serialize byte-for-byte. Frame 10 closed via the intra mode_skip_start /
	// ref_frame_skip_mask gate (the standalone intra producer now evaluates each
	// intra Y mode at its own vp9_mode_order position under the ref_frame_skip_-
	// mask, so the late intra modes — H_PRED etc. — are suppressed once an inter
	// mode wins, vp9_fullrd_inter_intra_sb.go); that also closed frames 11..19.
	// Frame 20 closed via the NEWMV zero-MV fix (evaluateNewMVMode now calls the
	// allow-zero MV search on the full-RD path, so NEWMV-ALTREF=(0,0) is scored
	// like libvpx vp9_rdopt.c:2922-2929 instead of being dropped by the non-RD
	// wrapper's mv==MV{} guard; that lets best become an ALTREF mode at
	// mode_skip_start, opening ALT_REF_MODE_MASK so NEARMV-ALTREF(0,0) wins — see
	// the header); that also closed frames 21..29. Frame 30 is the new first
	// divergence (the next key frame, a distinct intra-class frontier). Asserting
	// >= 30 (was 20) proves the genuine full-RD inter engine GENERALIZES across
	// the GOLDEN-refresh cadence and the accumulated entropy-context adaptation
	// through the first twenty-nine inter frames, up to the next key frame.
	prefix := testutil.MatchedFramePrefixLength(govpxFrames, libvpxFrames)
	if prefix < 30 {
		t.Fatalf("matched-frame prefix = %d, want >= 30 (frame 0 keyframe + "+
			"frames 1..29 inter all byte-exact)", prefix)
	}
	govpxFrame30Grid := decodeVP9MiGridForOracleTest(t, govpxFrames[30])
	libvpxFrame30Grid := decodeVP9MiGridForOracleTest(t, libvpxFrames[30])
	if len(govpxFrame30Grid) != len(libvpxFrame30Grid) {
		t.Fatalf("frame 30 mi grid len: govpx=%d libvpx=%d",
			len(govpxFrame30Grid), len(libvpxFrame30Grid))
	}
	firstMiDiff := -1
	for i := range govpxFrame30Grid {
		if govpxFrame30Grid[i] != libvpxFrame30Grid[i] {
			firstMiDiff = i
			break
		}
	}
	if firstMiDiff != 6 {
		t.Fatalf("frame 30 first decoded mi diff = %d, want 6 (mi(0,6)); "+
			"mi(0,2) keyframe RD partition replay regressed", firstMiDiff)
	}
	t.Logf("{0,1,1,0,1} full-RD inter engine generalizes; "+
		"matched-frame prefix = %d (frame0=%d bytes frame1=%d bytes)",
		prefix, len(govpxFrames[0]), len(govpxFrames[1]))
}
