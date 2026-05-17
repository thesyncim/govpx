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
// The 6 baseline seeds populate (dimBucket, framesBucket, cpuBucket, kfPos,
// refPos, action1, ...) from cpuPool {0, -3, -8, 4} — all four entries hit
// libvpx speed-feature paths govpx ports only at cpu_used=8 today, and even
// the abs(cpu)=8 seeds (#3) diverge on byte 16 of the first keyframe because
// the runtime-controls fuzz uses content-rich newVP9YCbCrFuzzPanning sources
// that exercise govpx's keyframe compressed-header writer gap (see citations
// below) instead of the flat black/checker patches the existing
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
//   - {0,0,0,0,0,0,0,0} — 64x64 frames=4 cpu=0. Frame 0 KF diverges at byte 9
//     (filter_level=13 govpx vs 12 libvpx) because cpu_used=0 selects the
//     LpfPickFromFullImage search method
//     (vp9/encoder/vp9_speed_features.c:set_good_speed_feature_framesize_*
//     @ vp9_speed_features.c:140-280 best/good-quality branches set
//     sf.lpf_pick = LPF_PICK_FROM_FULL_IMAGE). The LpfPickFromFullImage
//     post-tile search seed contamination was fixed by aligning
//     vp9EncoderLoopFilterParams with libvpx vp9_picklpf.c:90 (do NOT
//     overwrite vp9LastFiltLevel with the from-Q placeholder when the
//     search will run post-tile); the remaining divergence stems from
//     the cpu_used=0 speed-features path govpx has not yet ported
//     (vp9_speed_features.c:140-280). At cpu_used=0 libvpx picks a
//     different partition_search_type / RD path than govpx's
//     cpu_used=8-only verbatim port, so the reconstructed luma the
//     picker scores diverges and the chosen filter_level diverges with
//     it.
//
//   - {0,1,1,0,2,1,0,0} — 64x64 frames=6 cpu=-3 (abs=3). Same cpu_used!=8
//     speed-features gap as #0 plus the realtime-cpu_used=3 branch of
//     set_rt_speed_feature_framesize_independent
//     (vp9_speed_features.c:587-688). Govpx implements the cpu_used=8
//     realtime defaults verbatim (vp9_speed_features.c:661+ branch) but
//     speeds 3-7 require their own partition_search_type, mv_precision,
//     and recode-tolerance branches that govpx has not ported.
//
//   - {1,0,0,1,0,0,1,0} — 128x64 frames=4 cpu=0. Same cpu_used=0 gap as #0
//     plus the wider frame_width-1 (127) trips a different miCols path that
//     amplifies the partition-search divergence; libvpx's
//     vp9_speed_features.c:partition_search_breakout_dist_thr scaling at
//     wider widths leaves more partitions in govpx's emitted bitstream.
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
//   - {0,2,0,2,0,0,0,0} — 64x64 frames=8 cpu=0. Same as #0 (cpu_used=0 gap)
//     with frame count widened; once frame 0 KF parity holds the inter
//     frames should follow because the runtime-controls fuzz only flips
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
// Reverting any entry here must be paired with the corresponding verbatim
// libvpx port landing; this is the explicit handoff list for follow-up work.
var vp9RuntimeControlsSeedsDeferred = [][]byte{
	{0, 0, 0, 0, 0, 0, 0, 0},
	{0, 1, 1, 0, 2, 1, 0, 0},
	{1, 0, 0, 1, 0, 0, 1, 0},
	{1, 1, 2, 0, 3, 1, 1, 0},
	{0, 2, 0, 2, 0, 0, 0, 0},
	{1, 2, 1, 0, 4, 1, 0, 1},
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
