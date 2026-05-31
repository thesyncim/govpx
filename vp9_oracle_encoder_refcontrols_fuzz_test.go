//go:build govpx_oracle_trace

package govpx_test

import (
	"crypto/sha256"
	"encoding/hex"
	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp9oracle"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"image"
	"testing"
)

// vp9RefControlParitySeeds pins reference-control schedules that exercise
// per-frame update, no-reference, and force-reference flags against libvpx.
// Keep these seeds stable so strict byte parity failures point at behavior,
// not corpus churn.
var vp9RefControlParitySeeds = [][]byte{
	{0, 0, 0, 0, 0, 0, 0, 0},
	{0, 1, 0, 2, 0, 3, 0, 0},
	{1, 2, 3, 4, 5, 6, 0, 0},
	{0, 4, 0, 5, 0, 6, 0, 7},
	{0xff, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
	{0, 7, 0, 8, 0, 9, 0, 10},
	[]byte("0"),
	[]byte("1"),
	[]byte("2"),
	[]byte("7"),
}

// FuzzVP9EncoderReferenceControlSequences checks VP9 per-frame schedules that mix
// govpx.EncodeFlags-based reference-update bits (NoUpdateLast, NoUpdateGolden,
// NoUpdateAltRef, NoReferenceLast/Golden/AltRef, ForceGolden/AltRefFrame), and
// the encoded bytes must match the libvpx VP9 vpxenc-vp9-frameflags driver
// driven through the same schedule.
//
// VP9's public SetReferenceFrame/CopyReferenceFrame surface is exercised by
// the dedicated vp9_oracle_copy_reference_parity_test.go family, so this
// fuzzer focuses on the per-frame govpx.EncodeFlags permutations the libvpx
// driver also supports. Gated by GOVPX_WITH_ORACLE=1 plus a built
// vpxenc-vp9-frameflags binary.
func FuzzVP9EncoderReferenceControlSequences(f *testing.F) {
	vp9test.RequireOracle(f, "VP9 ref-control sequence fuzz")
	vp9test.RequireVpxencFrameFlags(f)
	seeds := [][]byte{
		{0, 0, 0, 0, 0, 0, 0, 0},
		{0, 1, 0, 2, 0, 3, 0, 0},
		{1, 2, 3, 4, 5, 6, 0, 0},
		{0, 4, 0, 5, 0, 6, 0, 7},
		{0xff, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		{0, 7, 0, 8, 0, 9, 0, 10},
	}
	seen := make(map[string]struct{}, len(seeds)+len(vp9RefControlParitySeeds))
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
	for _, seed := range vp9RefControlParitySeeds {
		addSeed(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		tc := newVP9RefControlsFuzzCase(data)
		sum := sha256.Sum256(data)
		label := "fuzz-vp9-refctrl-" + hex.EncodeToString(sum[:4])
		t.Logf("%s frames=%d flags=%v", label, len(tc.sources), tc.flags)

		govpxFrames := vp9oracle.EncodeFramesWithGovpx(t, tc.opts, tc.sources, tc.flags)
		libvpxFrames := vp9test.VpxencFrameFlagPackets(t, tc.sources,
			vp9oracle.LibvpxFrameFlags(tc.flags), tc.extraArgs...)
		vp9test.AssertSegmentByteParity(t, label, govpxFrames, libvpxFrames, 0)
	})
}

type vp9RefControlsFuzzCase struct {
	opts      govpx.VP9EncoderOptions
	sources   []*image.YCbCr
	flags     []govpx.EncodeFlags
	extraArgs []string
}

// newVP9RefControlsFuzzCase generates a per-frame schedule that mixes the
// govpx.EncodeFlags ref-update / no-reference / force-* bits supported by both
// govpx VP9 and the vpxenc-vp9-frameflags driver.
func newVP9RefControlsFuzzCase(data []byte) vp9RefControlsFuzzCase {
	r := testutil.NewByteCursor(data)
	framesPool := [...]int{6, 8, 10}
	frames := framesPool[r.Pick(len(framesPool))]
	const (
		width  = 64
		height = 64
	)
	opts := govpx.VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     govpx.RateControlQ,
		TargetBitrateKbps:   700,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             32,
		MaxKeyframeInterval: 128,
		Deadline:            govpx.DeadlineRealtime,
		CpuUsed:             8,
	}
	sources := vp9test.NewPanningSources(width, height, frames)
	flags := make([]govpx.EncodeFlags, frames)

	for frame := 1; frame < frames; frame++ {
		switch r.Pick(11) {
		case 0:
			// No-op frame.
		case 1, 2:
			flags[frame] |= govpx.EncodeNoUpdateLast
		case 3, 4:
			flags[frame] |= govpx.EncodeNoUpdateGolden
		case 5, 6:
			flags[frame] |= govpx.EncodeNoUpdateAltRef
		case 7:
			flags[frame] |= govpx.EncodeNoReferenceLast | govpx.EncodeNoUpdateLast
		case 8:
			flags[frame] |= govpx.EncodeForceGoldenFrame
		case 9:
			flags[frame] |= govpx.EncodeForceAltRefFrame
		case 10:
			flags[frame] |= govpx.EncodeNoUpdateEntropy
		}
	}

	extraArgs := []string{
		"--cq-level=32",
		"--min-q=4",
		"--max-q=56",
		"--end-usage=q",
	}
	return vp9RefControlsFuzzCase{
		opts:      opts,
		sources:   sources,
		flags:     flags,
		extraArgs: extraArgs,
	}
}
