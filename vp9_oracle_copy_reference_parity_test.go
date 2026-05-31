//go:build govpx_oracle_trace

package govpx_test

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"reflect"
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9oracle"
)

func TestVP9OracleCopyReferenceFrameParity(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 copy-reference parity gate")
	vp9test.RequireVpxencFrameFlags(t)

	t.Run("refreshed-references", func(t *testing.T) {
		const width, height, frames = 64, 64, 5
		opts := vp9oracle.CBROptions(width, height, 650)
		sources := vp9oracle.TransitionSources(width, height, frames)
		script := vp9oracle.EmptyCopyReferenceScript(len(sources))
		script[1] = "copyref:last+copyref:golden+copyref:altref"
		checks := map[int][]vp9oracle.CopyReferenceCheck{
			1: {
				{Ref: govpx.ReferenceLast, Name: "last"},
				{Ref: govpx.ReferenceGolden, Name: "golden"},
				{Ref: govpx.ReferenceAltRef, Name: "altref"},
			},
		}

		extraArgs := vp9oracle.CBRArgs(650, 600, 400, 500, 0)
		want := vp9oracle.CaptureLibvpxCopyReferenceChecksums(t,
			"vp9-copyref-refresh", sources, nil, script, extraArgs)
		got := vp9oracle.CaptureGovpxCopyReferenceChecksums(t, opts, sources, nil,
			nil, checks)
		vp9oracle.AssertCopyReferenceChecksumsEqual(t, got, want)
	})

	t.Run("external-set-reference-odd-size", func(t *testing.T) {
		const width, height, frames = 65, 63, 4
		opts := vp9oracle.CBROptions(width, height, 650)
		sources := vp9oracle.TransitionSources(width, height, frames)
		script := vp9oracle.EmptyCopyReferenceScript(len(sources))
		script[1] = "setref:last:panning:8+copyref:last"
		script[2] = "setref:golden:panning:9+copyref:golden"
		script[3] = "setref:altref:panning:10+copyref:altref"
		sets := map[int][]vp9oracle.CopyReferenceSet{
			1: {{Ref: govpx.ReferenceLast, Name: "last", PanningIndex: 8}},
			2: {{Ref: govpx.ReferenceGolden, Name: "golden", PanningIndex: 9}},
			3: {{Ref: govpx.ReferenceAltRef, Name: "altref", PanningIndex: 10}},
		}
		checks := map[int][]vp9oracle.CopyReferenceCheck{
			1: {{Ref: govpx.ReferenceLast, Name: "last"}},
			2: {{Ref: govpx.ReferenceGolden, Name: "golden"}},
			3: {{Ref: govpx.ReferenceAltRef, Name: "altref"}},
		}

		extraArgs := vp9oracle.CBRArgs(650, 600, 400, 500, 0)
		want := vp9oracle.CaptureLibvpxCopyReferenceChecksums(t,
			"vp9-copyref-setref-odd", sources, nil, script, extraArgs)
		got := vp9oracle.CaptureGovpxCopyReferenceChecksums(t, opts, sources, nil,
			sets, checks)
		vp9oracle.AssertCopyReferenceChecksumsEqual(t, got, want)
	})

	t.Run("post-inter-refresh-trace", func(t *testing.T) {
		const width, height, frames = 64, 64, 5
		opts := vp9oracle.CBROptions(width, height, 650)
		sources := vp9oracle.TransitionSources(width, height, frames)
		script := vp9oracle.EmptyCopyReferenceScript(len(sources))
		script[3] = "copyref:last+copyref:golden+copyref:altref"
		checks := map[int][]vp9oracle.CopyReferenceCheck{
			3: {
				{Ref: govpx.ReferenceLast, Name: "last"},
				{Ref: govpx.ReferenceGolden, Name: "golden"},
				{Ref: govpx.ReferenceAltRef, Name: "altref"},
			},
		}

		extraArgs := vp9oracle.CBRArgs(650, 600, 400, 500, 0)
		want := vp9oracle.CaptureLibvpxCopyReferenceChecksums(t,
			"vp9-copyref-post-inter", sources, nil, script, extraArgs)
		got := vp9oracle.CaptureGovpxCopyReferenceChecksums(t, opts, sources, nil,
			nil, checks)
		if !reflect.DeepEqual(got, want) {
			t.Logf("VP9 post-inter CopyReferenceFrame trace\n govpx: %s\nlibvpx: %s",
				vp9oracle.FormatCopyReferenceChecksums(got),
				vp9oracle.FormatCopyReferenceChecksums(want))
		}
	})
}
