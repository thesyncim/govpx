//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"image"
	"os"
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

// FuzzVP9EncoderReferenceControlSequences mirrors
// FuzzEncoderReferenceControlSequences (F8) for VP9: per-frame schedules mix
// EncodeFlags-based reference-update bits (NoUpdateLast, NoUpdateGolden,
// NoUpdateAltRef, NoReferenceLast/Golden/AltRef, ForceGolden/AltRefFrame), and
// the encoded bytes must match the libvpx VP9 vpxenc-vp9-frameflags driver
// driven through the same schedule.
//
// VP9's public SetReferenceFrame/CopyReferenceFrame surface is exercised by
// the dedicated vp9_oracle_copy_reference_parity_test.go family, so this
// fuzzer focuses on the per-frame EncodeFlags permutations the libvpx driver
// also supports. Gated by GOVPX_WITH_ORACLE=1 plus a built
// vpxenc-vp9-frameflags binary.
func FuzzVP9EncoderReferenceControlSequences(f *testing.F) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		f.Skip("set GOVPX_WITH_ORACLE=1 to run VP9 ref-control sequence fuzz")
	}
	requireVP9VpxencFrameFlagsOracleFuzz(f)
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

		govpxFrames := encodeVP9FramesWithGovpx(t, tc.opts, tc.sources, tc.flags)
		libvpxFrames := encodeVP9FramesWithLibvpxFrameFlagsOracle(t, tc.sources, tc.flags, tc.extraArgs)
		assertVP9SegmentByteParity(t, label, govpxFrames, libvpxFrames, 0)
	})
}

type vp9RefControlsFuzzCase struct {
	opts      VP9EncoderOptions
	sources   []*image.YCbCr
	flags     []EncodeFlags
	extraArgs []string
}

// newVP9RefControlsFuzzCase generates a per-frame schedule that mixes the
// EncodeFlags ref-update / no-reference / force-* bits supported by both
// govpx VP9 and the vpxenc-vp9-frameflags driver.
func newVP9RefControlsFuzzCase(data []byte) vp9RefControlsFuzzCase {
	r := vp9FuzzByteCursor{data: data}
	framesPool := [...]int{6, 8, 10}
	frames := framesPool[r.pick(len(framesPool))]
	const (
		width  = 64
		height = 64
	)
	opts := VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     RateControlQ,
		TargetBitrateKbps:   700,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             32,
		MaxKeyframeInterval: 128,
		Deadline:            DeadlineRealtime,
		CpuUsed:             8,
	}
	sources := newVP9YCbCrFuzzSources(width, height, frames)
	flags := make([]EncodeFlags, frames)

	for frame := 1; frame < frames; frame++ {
		switch r.pick(11) {
		case 0:
			// No-op frame.
		case 1, 2:
			flags[frame] |= EncodeNoUpdateLast
		case 3, 4:
			flags[frame] |= EncodeNoUpdateGolden
		case 5, 6:
			flags[frame] |= EncodeNoUpdateAltRef
		case 7:
			flags[frame] |= EncodeNoReferenceLast | EncodeNoUpdateLast
		case 8:
			flags[frame] |= EncodeForceGoldenFrame
		case 9:
			flags[frame] |= EncodeForceAltRefFrame
		case 10:
			flags[frame] |= EncodeNoUpdateEntropy
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
