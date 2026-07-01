//go:build govpx_phase_stats

package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestVP9EncoderInterMvPartSkipAvoidsFullPelSAD(t *testing.T) {
	const (
		width  = 128
		height = 64
	)
	var stats EncoderPhaseStats
	opts := VP9EncoderOptions{Width: width, Height: height}
	opts.PhaseStats = &stats
	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	keySrc := vp9test.NewMotionYCbCr(width, height)
	if _, err := e.Encode(keySrc); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	if !e.refFrames[0].valid {
		t.Fatal("LAST reference was not refreshed by keyframe")
	}

	stats.Reset()
	interSrc := shiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 0, 0)
	inter := &vp9InterEncodeState{
		img:     interSrc,
		ref:     &e.refFrames[0],
		allowHP: true,
	}
	e.sf.Mv.SubpelForceStop = FullPel
	seed := vp9dec.MV{Col: 8}

	got, _, ok := e.pickVP9InterMvAllowZero(inter, 8, 16,
		0, 0, common.Block64x64, vp9dec.LastFrame,
		vp9InterMvSearchOptions{
			seed:      seed,
			seedValid: true,
			useMvPart: true,
		})
	if !ok {
		t.Fatal("mv-part skip returned !ok")
	}
	if got != seed {
		t.Fatalf("mv-part skip = %+v, want seed %+v", got, seed)
	}
	if stats.VP9FullPelSearchSkipMVPart != 1 {
		t.Fatalf("skip-mvpart searches = %d, want 1", stats.VP9FullPelSearchSkipMVPart)
	}
	if stats.FullPelSADCalls != 0 || stats.FullPelSADCandidates != 0 ||
		stats.FullPelBatchCalls != 0 {
		t.Fatalf("full-pel SAD stats = %+v, want no SAD work", stats)
	}
}
