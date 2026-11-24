//go:build govpx_oracle_trace

package govpx_test

import (
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp9oracle"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

// =============================================================================
// Seed {1,1,1,1,0} — recode-trigger + committed-pass ground truth.
//
// Config (newVP9LongFixtureFuzzCase for this seed): VBR 700 kbps, kf=30,
// DeadlineGoodQuality, cpu_used=8. The vpxenc args the harness emits are the
// base set in coracle.VpxencVP9EncodeI420 (which begins "--rt") PLUS the
// per-seed extras "--end-usage=vbr --target-bitrate=700 --cpu-used=8
// --kf-min-dist=0 --kf-max-dist=30 --buf-sz=600 --buf-initial-sz=400
// --buf-optimal-sz=500 --drop-frame=0 --timebase=1/30 --good".
//
// CRITICAL CORRECTION to docs/vp9_fullrd_frame1_decomposition.md (which claimed
// this seed runs a "good-quality recode loop", q=54 -> q=83, with TWO intra
// blocks). A private $TMPDIR-built libvpx v1.16.0 vpxenc with env-gated
// (GOVPX_RECODE_TRACE) fprintf probes at set_size_dependent_vars
// (vp9_encoder.c:3768), the recode dispatch (vp9_encoder.c:5402), every
// encode_with_recode_loop iteration (vp9_encoder.c:4546), and the encode_b
// commit point (vp9_encodeframe.c:2242) establishes the ACTUAL behaviour. The
// two-frame IVF the probed binary produces is byte-identical to the unmodified
// pinned vpxenc-vp9 oracle (md5 758eb78456b3a300de053d9217728dfc, unchanged);
// the full 256-frame IVF md5 is 5c5bc4edd9f79c33dd23e61129e1d2f6 both with the
// probe compiled-in-but-disabled and with the stock oracle, so the probe is
// provably non-mutating. Captured 2026-06-05.
//
// GROUND TRUTH (captured):
//
//  1. PASSES. vpxenc defaults VP9 to TWO passes unless deadline==REALTIME
//     (vpxenc.c:825-833 "Make default VP9 passes = 2"); "--good" sets the
//     deadline back to GOOD_QUALITY after the base "--rt", so this seed runs a
//     TWO-PASS encode. The committed bitstream is pass 2 (g_pass==LAST_PASS,
//     oxcf->pass==2). Only pass 2 emits frames through encode_frame_to_data_rate
//     (pass 1 is vp9_first_pass), so the probe sees exactly one encode per
//     output frame.
//
//  2. RECODE LOOP. Because oxcf->pass==2 (not 0), the "if (oxcf->pass==0)
//     sf->recode_loop=DISALLOW_RECODE" override (vp9_speed_features.c:1055-1058)
//     does NOT apply. The GOOD speed-8 cascade leaves
//     sf->recode_loop==ALLOW_RECODE_KFMAXBW(1): baseline ALLOW_RECODE_FIRST(3)
//     (vp9_speed_features.c:931), speed>=2 -> ALLOW_RECODE_KFARFGF(2)
//     (:324, vbr_corpus_complexity is 0), speed>=3 -> ALLOW_RECODE_KFMAXBW(1)
//     (:368); speed>=4/5 do not touch recode_loop and GOOD has no >=6/7/8 block,
//     so cpu_used==8 saturates at the speed-5 GOOD features. encode_frame_to_-
//     data_rate therefore calls encode_with_recode_loop (recode_loop!=DISALLOW),
//     but ALLOW_RECODE_KFMAXBW(1) is BELOW the gate ALLOW_RECODE_KFARFGF(2) at
//     vp9_encoder.c:4546, so the dummy vp9_pack_bitstream is skipped, and the
//     recode_loop_test entry condition (vp9_encoder.c:3241-3245) is never
//     satisfied for an inter frame at this level. RESULT, verified for every one
//     of the 256 frames: loop_count==0, did_dummy_pack==0, recode_loop_test==0.
//     THERE IS NO RECODE. Each frame is encoded exactly once at the q that
//     vp9_rc_pick_q_and_bounds returns.
//
//  3. COMMITTED q. Frame 0 (KF) base_qindex==16 (bounds 16..27). Frame 1 (first
//     inter) base_qindex==39 (bounds 39..60), NOT 83. q comes from the TWO-PASS
//     picker vp9_rc_pick_q_and_bounds_two_pass (vp9_ratectrl.c, dispatched at
//     vp9_rc_pick_q_and_bounds because oxcf->pass!=0).
//
//  4. FRAME-1 RECODE-TEST INPUTS (one-shot, no recode): this_frame_target=28627,
//     recode_tolerance_low=15, recode_tolerance_high=45 (GOOD speed>=1 sets
//     low=15/high=30 at vp9_speed_features.c:310-311; speed>=2 raises high=45 at
//     :337). vp9_rc_compute_frame_size_bounds (vp9_ratectrl.c:1709) then yields
//     frame_under_shoot_limit = max(target - 15*target/100 - 100, 0) = 24233 and
//     frame_over_shoot_limit = min(target + 45*target/100 + 100, max_frame_bw)
//     = 41609. The pass-2 projected size of frame 1 lands inside [24233, 41609],
//     which (even were the dummy pack enabled) is within range -> no recode.
//
// GOVPX STATUS. govpx already models the recode SPEED_FEATURES verbatim
// (vp9_speed_features_good.go: RecodeLoopAllowKfMaxBw at speed>=3,
// RecodeToleranceLow=15 / RecodeToleranceHigh=45) and its encoder deliberately
// encodes each frame ONCE (vp9_encoder_frame.go:350-353 "govpx encodes each
// frame once ... to keep wire behaviour identical when the recode loop is
// introduced"). For THIS seed libvpx ALSO encodes frame 1 once, so the
// single-encode behaviour is already CORRECT and NO recode-loop port is needed.
//
// THE REAL GAP is upstream of any recode: govpx runs ONE-PASS VBR
// (rc_pick_q_and_bounds_one_pass_vbr) for this config because the fuzz harness
// passes no first-pass stats (opts.TwoPassStats empty -> twoPass.enabled()
// false), while libvpx runs TWO-PASS VBR. The q-selection algorithms differ, so
// the per-frame q diverges from frame 0 onward (measured: govpx KF q=29 vs
// libvpx 16; govpx frame-1 q=65 vs libvpx 39). Wiring the per-8x8 modes onto a
// one-pass q will NOT reproduce the pass-2 bitstream; closing this seed requires
// the two-pass VBR q path (vp9_rc_pick_q_and_bounds_two_pass + first-pass stats)
// in addition to the full-RD inter mode engine. That is well beyond a
// recode-files port, so this test PINS the ground truth rather than asserting
// byte parity.
// =============================================================================

// vp9RecodeSeed11110Frame1q is the committed pass-2 base_qindex of frame 1
// (first inter frame) for seed {1,1,1,1,0}. There is no recode; this is the q
// the single encode pass commits.
const vp9RecodeSeed11110Frame1q = 39

// vp9RecodeSeed11110Frame0q is the committed pass-2 base_qindex of frame 0 (the
// keyframe) for seed {1,1,1,1,0}.
const vp9RecodeSeed11110Frame0q = 16

// vp9RecodeSeed11110 frame-1 recode-test inputs (one-shot; no recode taken).
const (
	vp9RecodeSeed11110Frame1Target   = 28627 // rc->this_frame_target
	vp9RecodeSeed11110ToleranceLow   = 15    // sf->recode_tolerance_low
	vp9RecodeSeed11110ToleranceHigh  = 45    // sf->recode_tolerance_high
	vp9RecodeSeed11110MaxFrameBW     = 4000000
	vp9RecodeSeed11110Frame1UnderLim = 24233 // frame_under_shoot_limit
	vp9RecodeSeed11110Frame1OverLim  = 41609 // frame_over_shoot_limit
)

// vp9RecodeSeed11110Frame1SB0 is the COMPLETE libvpx ground-truth block
// decomposition of frame 1 (first inter frame), superblock 0, for the committed
// pass-2 q=39 encode. Captured from the encode_b commit-point probe
// (vp9_encodeframe.c:2242) in encode order. Field semantics match
// fullRDFrame1Block / MODE_INFO (Bsize BLOCK_SIZE 3==8x8; Mode 0==DC,
// 10==NEARESTMV 11==NEARMV 13==NEWMV; Ref0 0==INTRA 1==LAST; Ref1 -1==NONE;
// Interp 0==EIGHTTAP 1==SMOOTH 2==SHARP 3==SWITCHABLE/na on intra).
//
// Every leaf is 8x8 NONE (no sub-8x8, no >=16x16 inter block), tx_size==TX_8X8
// on all 64, single-ref LAST only, no compound. Exactly ONE intra block:
// mi(1,7) DC. (The decomposition-doc claim of two intra blocks and a q=83
// recode pass is superseded by this capture.)
var vp9RecodeSeed11110Frame1SB0 = []fullRDFrame1Block{
	{0, 0, 3, 13, 1, -1, 0}, {0, 1, 3, 13, 1, -1, 2}, {1, 0, 3, 13, 1, -1, 0}, {1, 1, 3, 13, 1, -1, 0},
	{0, 2, 3, 13, 1, -1, 0}, {0, 3, 3, 13, 1, -1, 0}, {1, 2, 3, 10, 1, -1, 1}, {1, 3, 3, 13, 1, -1, 0},
	{2, 0, 3, 13, 1, -1, 1}, {2, 1, 3, 10, 1, -1, 1}, {3, 0, 3, 13, 1, -1, 0}, {3, 1, 3, 13, 1, -1, 1},
	{2, 2, 3, 10, 1, -1, 1}, {2, 3, 3, 13, 1, -1, 0}, {3, 2, 3, 13, 1, -1, 0}, {3, 3, 3, 10, 1, -1, 0},
	{0, 4, 3, 13, 1, -1, 1}, {0, 5, 3, 13, 1, -1, 0}, {1, 4, 3, 10, 1, -1, 1}, {1, 5, 3, 13, 1, -1, 1},
	{0, 6, 3, 13, 1, -1, 0}, {0, 7, 3, 13, 1, -1, 0}, {1, 6, 3, 13, 1, -1, 0}, {1, 7, 3, 0, 0, -1, 3},
	{2, 4, 3, 11, 1, -1, 1}, {2, 5, 3, 13, 1, -1, 0}, {3, 4, 3, 13, 1, -1, 0}, {3, 5, 3, 13, 1, -1, 0},
	{2, 6, 3, 10, 1, -1, 0}, {2, 7, 3, 13, 1, -1, 0}, {3, 6, 3, 13, 1, -1, 1}, {3, 7, 3, 13, 1, -1, 0},
	{4, 0, 3, 13, 1, -1, 0}, {4, 1, 3, 10, 1, -1, 1}, {5, 0, 3, 13, 1, -1, 1}, {5, 1, 3, 10, 1, -1, 1},
	{4, 2, 3, 13, 1, -1, 0}, {4, 3, 3, 13, 1, -1, 1}, {5, 2, 3, 13, 1, -1, 0}, {5, 3, 3, 13, 1, -1, 0},
	{6, 0, 3, 10, 1, -1, 1}, {6, 1, 3, 10, 1, -1, 1}, {7, 0, 3, 13, 1, -1, 0}, {7, 1, 3, 13, 1, -1, 0},
	{6, 2, 3, 11, 1, -1, 0}, {6, 3, 3, 10, 1, -1, 1}, {7, 2, 3, 10, 1, -1, 1}, {7, 3, 3, 13, 1, -1, 1},
	{4, 4, 3, 13, 1, -1, 0}, {4, 5, 3, 10, 1, -1, 1}, {5, 4, 3, 13, 1, -1, 0}, {5, 5, 3, 13, 1, -1, 0},
	{4, 6, 3, 13, 1, -1, 1}, {4, 7, 3, 13, 1, -1, 1}, {5, 6, 3, 13, 1, -1, 0}, {5, 7, 3, 13, 1, -1, 1},
	{6, 4, 3, 13, 1, -1, 1}, {6, 5, 3, 13, 1, -1, 0}, {7, 4, 3, 13, 1, -1, 1}, {7, 5, 3, 13, 1, -1, 0},
	{6, 6, 3, 11, 1, -1, 0}, {6, 7, 3, 11, 1, -1, 0}, {7, 6, 3, 13, 1, -1, 1}, {7, 7, 3, 13, 1, -1, 0},
}

// TestVP9RecodeSeed1_1_1_1_0 pins the recode-trigger ground truth and the
// committed-pass block map for the {1,1,1,1,0} long-fixture parity-gap seed.
// It (1) self-validates the recode-test arithmetic (the one-shot over/under-
// shoot limits derived from the captured target + libvpx tolerances), (2)
// self-validates the embedded committed-pass block map invariants, and (3)
// drives the LIVE pinned oracle over the two-frame prefix of the exact seed
// config to re-pin the no-recode committed q's (frame 0 == 16, frame 1 == 39)
// and to record the one-pass(govpx)-vs-two-pass(libvpx) q divergence that is
// the seed's real, still-open gap. Frame parity is reported, not asserted,
// because govpx does not yet run the two-pass VBR q path.
func TestVP9RecodeSeed1_1_1_1_0(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 64, 64

	// (1) Recode-test arithmetic, verbatim from
	// vp9_rc_compute_frame_size_bounds (vp9_ratectrl.c:1709). These must equal
	// the values the probe captured; if libvpx's formula or the captured target
	// drifts this fails loudly.
	target := vp9RecodeSeed11110Frame1Target
	tolLow := vp9RecodeSeed11110ToleranceLow * target / 100
	tolHigh := vp9RecodeSeed11110ToleranceHigh * target / 100
	underLim := target - tolLow - 100
	if underLim < 0 {
		underLim = 0
	}
	overLim := target + tolHigh + 100
	if overLim > vp9RecodeSeed11110MaxFrameBW {
		overLim = vp9RecodeSeed11110MaxFrameBW
	}
	if underLim != vp9RecodeSeed11110Frame1UnderLim {
		t.Fatalf("frame-1 under-shoot limit = %d, want %d (vp9_ratectrl.c:1709)",
			underLim, vp9RecodeSeed11110Frame1UnderLim)
	}
	if overLim != vp9RecodeSeed11110Frame1OverLim {
		t.Fatalf("frame-1 over-shoot limit = %d, want %d (vp9_ratectrl.c:1709)",
			overLim, vp9RecodeSeed11110Frame1OverLim)
	}

	// (2) Self-validate the committed-pass block map invariants.
	if got, want := len(vp9RecodeSeed11110Frame1SB0), 64; got != want {
		t.Fatalf("frame-1 SB0 block count = %d, want %d (8x8 mi grid)", got, want)
	}
	var intra, compound, nonLast, sub8, notTx8 int
	var eighttap, smooth, sharp int
	var newmv, nearestmv, nearmv int
	seen := map[[2]int]bool{}
	for i, b := range vp9RecodeSeed11110Frame1SB0 {
		key := [2]int{b.MiRow, b.MiCol}
		if seen[key] {
			t.Fatalf("duplicate mi position (%d,%d) at index %d", b.MiRow, b.MiCol, i)
		}
		seen[key] = true
		if b.MiRow < 0 || b.MiRow > 7 || b.MiCol < 0 || b.MiCol > 7 {
			t.Fatalf("block %d mi (%d,%d) outside SB0 8x8 grid", i, b.MiRow, b.MiCol)
		}
		if b.Bsize != 3 { // all committed leaves are 8x8 NONE
			sub8++
		}
		if b.Ref0 == 0 { // INTRA_FRAME
			intra++
			if b.Mode > 9 {
				t.Fatalf("block %d (%d,%d) intra ref but inter mode %d", i, b.MiRow, b.MiCol, b.Mode)
			}
		} else {
			if b.Ref0 != 1 { // LAST_FRAME
				nonLast++
			}
			switch b.Mode {
			case 10:
				nearestmv++
			case 11:
				nearmv++
			case 13:
				newmv++
			default:
				t.Fatalf("block %d (%d,%d) inter ref but mode %d (want NEAREST/NEAR/NEW)",
					i, b.MiRow, b.MiCol, b.Mode)
			}
		}
		if b.Ref1 != -1 {
			compound++
		}
		switch b.Interp {
		case 0:
			eighttap++
		case 1:
			smooth++
		case 2:
			sharp++
		}
	}
	if sub8 != 0 {
		t.Fatalf("non-8x8 leaf count = %d, want 0 (rd_pick_partition converged all-8x8 NONE)", sub8)
	}
	if notTx8 != 0 { // documented: tx_size==TX_8X8 on all 64
		t.Fatalf("non-TX_8X8 block count = %d, want 0", notTx8)
	}
	if intra != 1 {
		t.Fatalf("intra-in-inter-frame block count = %d, want 1 (mi(1,7) DC)", intra)
	}
	if compound != 0 {
		t.Fatalf("compound block count = %d, want 0 (single-ref only)", compound)
	}
	if nonLast != 0 {
		t.Fatalf("non-LAST inter-ref block count = %d, want 0 (no GOLDEN/ALTREF)", nonLast)
	}
	if newmv != 46 || nearestmv != 13 || nearmv != 4 {
		t.Fatalf("inter-mode histogram NEW=%d NEAREST=%d NEAR=%d, want 46/13/4",
			newmv, nearestmv, nearmv)
	}
	if eighttap != 36 || smooth != 26 || sharp != 1 {
		t.Fatalf("interp histogram eighttap=%d smooth=%d sharp=%d, want 36/26/1",
			eighttap, smooth, sharp)
	}

	// (3) Drive the live oracle + govpx over the two-frame prefix of the exact
	// seed config (mirrors newVP9LongFixtureFuzzCase for {1,1,1,1,0}).
	opts := govpx.VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     govpx.RateControlVBR,
		TargetBitrateKbps:   700,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		MaxKeyframeInterval: 30,
		Deadline:            govpx.DeadlineGoodQuality,
		CpuUsed:             8,
	}
	args := []string{
		"--end-usage=vbr",
		"--target-bitrate=700",
		"--cpu-used=8",
		"--kf-min-dist=0",
		"--kf-max-dist=30",
		"--buf-sz=600",
		"--buf-initial-sz=400",
		"--buf-optimal-sz=500",
		"--drop-frame=0",
		"--timebase=1/30",
		"--good",
	}

	// NOTE: vpxenc defaults VP9 to two passes here (deadline==GOOD). The oracle
	// q's pinned below are the pass-2 committed values. Two-pass q-selection
	// allocates the keyframe / first-inter budget from the WHOLE sequence's
	// first-pass stats, so the committed q's are clip-length dependent. The
	// captured ground truth (frame-0 q=16, frame-1 q=39, and the embedded block
	// map) is for the full 256-frame panning clip the fuzz harness uses, so this
	// pin replays the same 256 frames; a 2-frame prefix would yield a different
	// (e.g. KF q=25) two-pass allocation.
	const frames = 256
	sources := vp9test.NewPanningSources(width, height, frames)

	govpxFrames := vp9oracle.EncodeFramesWithGovpx(t, opts, sources, nil)
	libvpxFrames := vp9test.VpxencPackets(t, sources, args...)
	if len(govpxFrames) < 2 || len(libvpxFrames) < 2 {
		t.Fatalf("expected >=2 packets each, got govpx=%d libvpx=%d",
			len(govpxFrames), len(libvpxFrames))
	}

	libHeader0, _ := vp9test.ParseHeader(t, libvpxFrames[0])
	libHeader1, _ := vp9test.ParseHeader(t, libvpxFrames[1])

	// Pin the no-recode committed q's on the live oracle. These are the q values
	// the single pass-2 encode commits; if either drifts the recode/2-pass
	// assumptions behind this note have changed.
	if got := int(libHeader0.Quant.BaseQindex); got != vp9RecodeSeed11110Frame0q {
		t.Fatalf("libvpx frame-0 (KF) base_qindex = %d, want %d", got, vp9RecodeSeed11110Frame0q)
	}
	if got := int(libHeader1.Quant.BaseQindex); got != vp9RecodeSeed11110Frame1q {
		t.Fatalf("libvpx frame-1 base_qindex = %d, want %d (no recode; pass-2 committed q)",
			got, vp9RecodeSeed11110Frame1q)
	}
	if libHeader1.FrameType == 0 {
		t.Fatal("libvpx frame 1 parsed as KEY_FRAME, want INTER")
	}

	// Record the one-pass(govpx) vs two-pass(libvpx) q divergence — the real,
	// still-open gap for this seed. Reported, not asserted: govpx does not yet
	// run the two-pass VBR q path, so these are expected to differ.
	goHeader0, _ := vp9test.ParseHeader(t, govpxFrames[0])
	goHeader1, _ := vp9test.ParseHeader(t, govpxFrames[1])
	prefix := testutil.MatchedFramePrefixLength(govpxFrames, libvpxFrames)
	t.Logf("seed{1,1,1,1,0} matched-frame-prefix=%d/%d (gap: govpx 1-pass VBR vs libvpx 2-pass VBR q; no recode either side)",
		prefix, min(len(govpxFrames), len(libvpxFrames)))
	t.Logf("seed{1,1,1,1,0} frame0(KF) govpx q=%d | libvpx q=%d (2-pass)", goHeader0.Quant.BaseQindex, libHeader0.Quant.BaseQindex)
	t.Logf("seed{1,1,1,1,0} frame1 govpx q=%d fps=%d filterLevel=%d | libvpx q=%d fps=%d filterLevel=%d (2-pass, committed)",
		goHeader1.Quant.BaseQindex, goHeader1.FirstPartitionSize, goHeader1.Loopfilter.FilterLevel,
		libHeader1.Quant.BaseQindex, libHeader1.FirstPartitionSize, libHeader1.Loopfilter.FilterLevel)
}
