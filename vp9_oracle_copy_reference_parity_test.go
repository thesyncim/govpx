//go:build govpx_oracle_trace

package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"image"
	"reflect"
	"testing"
)

func TestVP9OracleCopyReferenceFrameParity(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 copy-reference parity gate")
	vp9test.RequireVpxencFrameFlags(t)

	t.Run("refreshed-references", func(t *testing.T) {
		const width, height, frames = 64, 64, 5
		opts := vp9OracleCBROptions(width, height, 650)
		sources := newVP9OracleTransitionSources(width, height, frames)
		script := emptyCopyReferenceScript(len(sources))
		script[1] = "copyref:last+copyref:golden+copyref:altref"
		checks := map[int][]copyReferenceCheck{
			1: {
				{ref: ReferenceLast, name: "last"},
				{ref: ReferenceGolden, name: "golden"},
				{ref: ReferenceAltRef, name: "altref"},
			},
		}

		extraArgs := vp9OracleCBRArgs(650, 600, 400, 500, 0)
		want := captureLibvpxVP9CopyReferenceChecksums(t,
			"vp9-copyref-refresh", sources, nil, script, extraArgs)
		got := captureGovpxVP9CopyReferenceChecksums(t, opts, sources, nil,
			nil, checks)
		assertCopyReferenceChecksumsEqual(t, got, want)
	})

	t.Run("external-set-reference-odd-size", func(t *testing.T) {
		const width, height, frames = 65, 63, 4
		opts := vp9OracleCBROptions(width, height, 650)
		sources := newVP9OracleTransitionSources(width, height, frames)
		script := emptyCopyReferenceScript(len(sources))
		script[1] = "setref:last:panning:8+copyref:last"
		script[2] = "setref:golden:panning:9+copyref:golden"
		script[3] = "setref:altref:panning:10+copyref:altref"
		sets := map[int][]copyReferenceSet{
			1: {{ref: ReferenceLast, name: "last", panningIndex: 8}},
			2: {{ref: ReferenceGolden, name: "golden", panningIndex: 9}},
			3: {{ref: ReferenceAltRef, name: "altref", panningIndex: 10}},
		}
		checks := map[int][]copyReferenceCheck{
			1: {{ref: ReferenceLast, name: "last"}},
			2: {{ref: ReferenceGolden, name: "golden"}},
			3: {{ref: ReferenceAltRef, name: "altref"}},
		}

		extraArgs := vp9OracleCBRArgs(650, 600, 400, 500, 0)
		want := captureLibvpxVP9CopyReferenceChecksums(t,
			"vp9-copyref-setref-odd", sources, nil, script, extraArgs)
		got := captureGovpxVP9CopyReferenceChecksums(t, opts, sources, nil,
			sets, checks)
		assertCopyReferenceChecksumsEqual(t, got, want)
	})

	t.Run("post-inter-refresh-trace", func(t *testing.T) {
		const width, height, frames = 64, 64, 5
		opts := vp9OracleCBROptions(width, height, 650)
		sources := newVP9OracleTransitionSources(width, height, frames)
		script := emptyCopyReferenceScript(len(sources))
		script[3] = "copyref:last+copyref:golden+copyref:altref"
		checks := map[int][]copyReferenceCheck{
			3: {
				{ref: ReferenceLast, name: "last"},
				{ref: ReferenceGolden, name: "golden"},
				{ref: ReferenceAltRef, name: "altref"},
			},
		}

		extraArgs := vp9OracleCBRArgs(650, 600, 400, 500, 0)
		want := captureLibvpxVP9CopyReferenceChecksums(t,
			"vp9-copyref-post-inter", sources, nil, script, extraArgs)
		got := captureGovpxVP9CopyReferenceChecksums(t, opts, sources, nil,
			nil, checks)
		if !reflect.DeepEqual(got, want) {
			t.Logf("VP9 post-inter CopyReferenceFrame trace\n govpx: %s\nlibvpx: %s",
				formatCopyReferenceChecksums(got),
				formatCopyReferenceChecksums(want))
		}
	})
}

func captureLibvpxVP9CopyReferenceChecksums(t *testing.T, name string,
	sources []*image.YCbCr, flags []EncodeFlags, script []string,
	extraArgs []string,
) []copyReferenceChecksum {
	t.Helper()
	if len(flags) > len(sources) {
		t.Fatalf("VP9 copyref flag count = %d, want <= %d",
			len(flags), len(sources))
	}
	logPath := vp9test.VpxencFrameFlagCopyReferenceLog(t, name, sources,
		vp9LibvpxFrameFlags(flags), script, extraArgs...)
	return readCopyReferenceChecksumLog(t, logPath)
}

func captureGovpxVP9CopyReferenceChecksums(t *testing.T,
	opts VP9EncoderOptions, sources []*image.YCbCr, flags []EncodeFlags,
	sets map[int][]copyReferenceSet,
	checks map[int][]copyReferenceCheck,
) []copyReferenceChecksum {
	t.Helper()
	if len(sources) == 0 {
		t.Fatal("empty VP9 copy-reference source")
	}
	if len(flags) > len(sources) {
		t.Fatalf("VP9 copyref flag count = %d, want <= %d",
			len(flags), len(sources))
	}
	width := sources[0].Rect.Dx()
	height := sources[0].Rect.Dy()
	opts.Width = width
	opts.Height = height
	enc, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer enc.Close()

	dstSize, err := vp9AllocatingEncodeBufferSize(width, height)
	if err != nil {
		t.Fatalf("vp9AllocatingEncodeBufferSize: %v", err)
	}
	buf := make([]byte, dstSize)
	out := make([]copyReferenceChecksum, 0)
	for i, src := range sources {
		for _, set := range sets[i] {
			img := encoderValidationPanningFrame(width, height, set.panningIndex)
			if err := enc.SetReferenceFrame(set.ref, img); err != nil {
				t.Fatalf("frame %d SetReferenceFrame(%s): %v",
					i, set.name, err)
			}
		}
		for _, check := range checks[i] {
			dst := testImage(width, height)
			if err := enc.CopyReferenceFrame(check.ref, &dst); err != nil {
				t.Fatalf("frame %d CopyReferenceFrame(%s): %v",
					i, check.name, err)
			}
			out = append(out, copyReferenceImageChecksum(i, check.name, dst))
		}
		var frameFlags EncodeFlags
		if i < len(flags) {
			frameFlags = flags[i]
		}
		if _, err := enc.EncodeIntoWithFlags(src, buf, frameFlags); err != nil {
			t.Fatalf("EncodeIntoWithFlags frame %d: %v", i, err)
		}
	}
	return out
}
