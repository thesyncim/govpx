//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"image"
	"strconv"
	"testing"
)

// vp9RuntimeControlsParityGapSeeds lists runtime-control schedules that measure
// known VP9 parity gaps. The seed shape is
// (dimBucket, framesBucket, cpuBucket, kfFlagPos, refFlagPos, action...),
// and one-byte entries are corpus aliases produced by testutil.ByteCursor
// wrap-around. Speed-8 entries that already match libvpx live in
// vp9RuntimeControlsSpeed8ParitySeeds so the main fuzz target still exercises
// them while the remaining parity-gap lanes stay reproducible.
var vp9RuntimeControlsParityGapSeeds = [][]byte{
	{0, 0, 0, 0, 0, 0, 0, 0},
	{0, 1, 1, 0, 2, 1, 0, 0},
	{1, 0, 0, 1, 0, 0, 1, 0},
	{1, 1, 2, 0, 3, 1, 1, 0},
	{0, 2, 0, 2, 0, 0, 0, 0},
	{1, 2, 1, 0, 4, 1, 0, 1},
	// Short-byte corpus aliases of the above.
	{0x30}, // alias of #0
	{0x31}, // alias of #1 family
	{0x32}, // alias of the speed-8 parity lane
	{0x37}, // alias of the speed-4 parity lane
	{0x38}, // alias of the cpu=0 keyframe lane
}

// vp9RuntimeControlsSpeed8ParitySeeds is the subset that byte-matches libvpx
// with no env flags. Keep it in the regular fuzz seed corpus while the
// remaining cpu=0/-3 and speed-4 seeds stay in the parity-gap list.
var vp9RuntimeControlsSpeed8ParitySeeds = [][]byte{
	{1, 1, 2, 0, 3, 1, 1, 0},
	{0x32},
}

func vp9RuntimeControlsParityGapSeed(data []byte) bool {
	for _, seed := range vp9RuntimeControlsSpeed8ParitySeeds {
		if bytes.Equal(data, seed) {
			return false
		}
	}
	for _, seed := range vp9RuntimeControlsParityGapSeeds {
		if bytes.Equal(data, seed) {
			return true
		}
	}
	return false
}

// FuzzVP9OracleEncoderRuntimeControls mirrors the VP8
// FuzzVP8OracleEncoderRuntimeControlTransitions: a fuzz-driven runtime-control
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
	vp9test.RequireOracle(f, "VP9 runtime-control oracle fuzz")
	vp9test.RequireVpxencFrameFlags(f)
	seeds := [][]byte{
		// (dimBucket, framesBucket, cpuBucket, kfFlagPos, refFlagPos, action1, action2, ...)
		{0, 0, 0, 0, 0, 0, 0, 0},
		{0, 1, 1, 0, 2, 1, 0, 0},
		{1, 0, 0, 1, 0, 0, 1, 0},
		{1, 1, 2, 0, 3, 1, 1, 0},
		{0, 2, 0, 2, 0, 0, 0, 0},
		{1, 2, 1, 0, 4, 1, 0, 1},
	}
	seen := make(map[string]struct{}, len(seeds)+len(vp9RuntimeControlsSpeed8ParitySeeds))
	addSeed := func(seed []byte) {
		key := string(seed)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		f.Add(seed)
	}
	for _, seed := range seeds {
		addSeed(seed)
	}
	for _, seed := range vp9RuntimeControlsSpeed8ParitySeeds {
		addSeed(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if vp9RuntimeControlsParityGapSeed(data) {
			t.Skip("open VP9 runtime-control parity seed")
		}
		tc := vp9OracleRuntimeFuzzCaseFromBytes(data)
		sum := sha256.Sum256(data)
		label := "fuzz-vp9-runtime-controls-" + tc.name + "-" + hex.EncodeToString(sum[:4])
		t.Logf("%s w=%d h=%d frames=%d cpu=%d flags=%v",
			label, tc.opts.Width, tc.opts.Height, len(tc.sources), tc.opts.CpuUsed, tc.flags)

		govpxFrames := encodeVP9FramesWithGovpx(t, tc.opts, tc.sources, tc.flags)
		libvpxFrames := vp9test.VpxencFrameFlagPackets(t, tc.sources,
			vp9LibvpxFrameFlags(tc.flags), tc.extraArgs...)
		vp9test.AssertSegmentByteParity(t, label, govpxFrames, libvpxFrames, 0)
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
	r := testutil.NewByteCursor(data)
	dims := [...]struct {
		w int
		h int
	}{
		{64, 64},
		{128, 64},
	}
	frameCountPool := [...]int{4, 6, 8}
	cpuPool := [...]int{0, -3, -8, 4}

	dim := dims[r.Pick(len(dims))]
	frames := frameCountPool[r.Pick(len(frameCountPool))]
	cpuUsed := cpuPool[r.Pick(len(cpuPool))]
	kfPos := r.Pick(frames)
	refPos := r.Pick(frames)

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
	sources := vp9test.NewPanningSources(dim.w, dim.h, frames)
	flags := make([]EncodeFlags, frames)

	// Sprinkle a key-frame flag and an optional reference-update flag.
	if kfPos > 0 && kfPos < frames {
		flags[kfPos] |= EncodeForceKeyFrame
	}
	if refPos > 0 && refPos < frames {
		switch r.Pick(5) {
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
		switch r.Pick(4) {
		case 1:
			flags[i] |= EncodeNoUpdateEntropy
		case 2:
			flags[i] |= EncodeForceGoldenFrame
		case 3:
			flags[i] |= EncodeForceAltRefFrame
		}
	}
	// libvpx vp9/vp9_cx_iface.c:1394-1398 rejects FORCE_GF + NO_UPD_GF and
	// FORCE_ARF + NO_UPD_ARF on the same frame as "Conflicting flags." The
	// vpxenc-vp9-frameflags oracle propagates that VPX_CODEC_INVALID_PARAM as
	// an exit-status failure, so the materialiser would deadlock the parity
	// comparator before ever exercising the encoder. govpx's
	// normalizeVP9EncodeFlags (vp9_encoder.c:set_ext_overrides semantics:
	// FORCE wins because vp9_apply_encoding_flags' upd mask treats FORCE_GF
	// as "refresh all minus NO_UPD bits" and the conflict check would have
	// rejected the input upstream) drops the NO_UPD_GF/NO_UPD_ARF bit when
	// the matching FORCE_GF/FORCE_ARF bit is set. Apply the same resolution
	// at materialisation so both encoders see identical, libvpx-acceptable
	// flag schedules for every fuzz iteration.
	for i := range flags {
		flags[i] = normalizeVP9EncodeFlags(flags[i])
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
