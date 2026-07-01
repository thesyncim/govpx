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

func TestVP9EncoderFullPelSADSourceBreakdown(t *testing.T) {
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
	interSrc := shiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 24, 0)
	inter := &vp9InterEncodeState{
		img:     interSrc,
		ref:     &e.refFrames[0],
		allowHP: true,
	}
	e.sf.Mv.SearchMethod = SearchMethodFastDiamond
	e.sf.Mv.SubpelForceStop = FullPel

	if _, _, ok := e.pickVP9InterMvAllowZero(inter, 8, 16,
		0, 0, common.Block64x64, vp9dec.LastFrame,
		vp9InterMvSearchOptions{
			seed:      vp9dec.MV{Col: 8},
			seedValid: true,
		}); !ok {
		t.Fatal("seeded full-pel search returned !ok")
	}
	sourceCalls := stats.VP9FullPelSADZeroCalls +
		stats.VP9FullPelSADSeedCalls +
		stats.VP9FullPelSADHintCalls +
		stats.VP9FullPelSADPatternCalls +
		stats.VP9FullPelSADFullRDCalls +
		stats.VP9FullPelSADOtherCalls
	if sourceCalls != stats.FullPelSADCalls {
		t.Fatalf("source calls = %d, total calls = %d", sourceCalls,
			stats.FullPelSADCalls)
	}
	sourceCandidates := stats.VP9FullPelSADZeroCandidates +
		stats.VP9FullPelSADSeedCandidates +
		stats.VP9FullPelSADHintCandidates +
		stats.VP9FullPelSADPatternCandidates +
		stats.VP9FullPelSADFullRDCandidates +
		stats.VP9FullPelSADOtherCandidates
	if sourceCandidates != stats.FullPelSADCandidates {
		t.Fatalf("source candidates = %d, total candidates = %d",
			sourceCandidates, stats.FullPelSADCandidates)
	}
	if stats.VP9FullPelSADZeroCalls != 0 {
		t.Fatalf("zero-source calls = %d, want 0 for seeded search",
			stats.VP9FullPelSADZeroCalls)
	}
	if stats.VP9FullPelSADSeedCalls != 1 {
		t.Fatalf("seed-source calls = %d, want 1",
			stats.VP9FullPelSADSeedCalls)
	}
	if stats.VP9FullPelSADPatternCalls == 0 {
		t.Fatal("pattern-source calls = 0, want motion-search candidates")
	}
	if stats.VP9FullPelSADOtherCalls != 0 {
		t.Fatalf("other-source calls = %d, want 0",
			stats.VP9FullPelSADOtherCalls)
	}

	stats.Reset()
	if _, _, ok := e.pickVP9InterMvAllowZero(inter, 8, 16,
		0, 0, common.Block64x64, vp9dec.LastFrame,
		vp9InterMvSearchOptions{}); !ok {
		t.Fatal("unseeded full-pel search returned !ok")
	}
	if stats.VP9FullPelSADZeroCalls != 1 {
		t.Fatalf("unseeded zero-source calls = %d, want 1",
			stats.VP9FullPelSADZeroCalls)
	}
	if stats.VP9FullPelSADSeedCalls != 0 {
		t.Fatalf("unseeded seed-source calls = %d, want 0",
			stats.VP9FullPelSADSeedCalls)
	}
}
