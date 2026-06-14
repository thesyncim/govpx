package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

// withVP9InterDeepRDThisRDScore routes pickVP9InterModeWithOrder's candidate
// score through the GENUINE per-mode this_rd assembly (vp9FullRDInterThisRD)
// instead of the model-RD vp9InterModeScore approximation, for the duration of
// a test. The flag defaults to false (production + the deep-RD partition
// serialization tests stay on the model score); these tests flip it to exercise
// the genuine-RD wire-in end-to-end.
func withVP9InterDeepRDThisRDScore(t *testing.T) {
	t.Helper()
	prev := vp9InterUseDeepRDThisRDScore
	vp9InterUseDeepRDThisRDScore = true
	t.Cleanup(func() { vp9InterUseDeepRDThisRDScore = prev })
}

func withoutVP9ProductionDeepRDSearchPartition(t *testing.T) {
	t.Helper()
	prev := vp9InterUseProductionDeepRDSearchPartition
	vp9InterUseProductionDeepRDSearchPartition = false
	t.Cleanup(func() { vp9InterUseProductionDeepRDSearchPartition = prev })
}

// TestVP9InterDeepRDThisRDScoreWiresThrough proves the genuine per-mode this_rd
// flows into pickVP9InterModeWithOrder's decision when
// vp9InterUseDeepRDThisRDScore is on: a multi-frame inter encode with the flag
// flipped still produces a valid, cleanly-decodable bitstream (the genuine
// Y-RD + UV-RD + skip-pick scoring drives real mode/MV/tx decisions through the
// writer without desync), AND its inter frames differ from the model-score
// production encode (the genuine RD selects different candidates) — so the wire
// is load-bearing, not a no-op.
func TestVP9InterDeepRDThisRDScoreWiresThrough(t *testing.T) {
	const width, height = 64, 64
	encode := func(genuine bool) ([][]byte, error) {
		if genuine {
			withVP9InterDeepRDThisRDScore(t)
		}
		e, err := NewVP9Encoder(VP9EncoderOptions{
			Width: width, Height: height, CpuUsed: -3,
		})
		if err != nil {
			return nil, err
		}
		defer e.Close()
		sources := vp9test.NewPanningSources(width, height, 4)
		var frames [][]byte
		for i := 0; i < 4; i++ {
			pkt, err := e.Encode(sources[i])
			if err != nil {
				return nil, err
			}
			cp := make([]byte, len(pkt))
			copy(cp, pkt)
			frames = append(frames, cp)
		}
		return frames, nil
	}

	// Production (model-score) reference.
	modelFrames, err := encode(false)
	if err != nil {
		t.Fatalf("model-score encode: %v", err)
	}

	// Genuine-RD encode under the flag.
	genuineFrames, err := encode(true)
	if err != nil {
		t.Fatalf("genuine-RD encode: %v", err)
	}

	// The genuine-RD inter frames must decode cleanly (no serialization desync).
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	for i, pkt := range genuineFrames {
		if err := d.Decode(pkt); err != nil {
			t.Fatalf("genuine-RD frame %d decode failed: %v", i, err)
		}
		if _, ok := d.NextFrame(); !ok {
			t.Fatalf("genuine-RD frame %d NextFrame !ok", i)
		}
	}

	// The wire must be load-bearing: at least one inter frame should differ from
	// the model-score encode (frame 0 is the keyframe and is unaffected by the
	// inter scorer, so compare from frame 1).
	differs := false
	for i := 1; i < len(modelFrames) && i < len(genuineFrames); i++ {
		if len(modelFrames[i]) != len(genuineFrames[i]) {
			differs = true
			break
		}
		for j := range modelFrames[i] {
			if modelFrames[i][j] != genuineFrames[i][j] {
				differs = true
				break
			}
		}
		if differs {
			break
		}
	}
	if !differs {
		t.Fatal("genuine per-mode this_rd scoring produced byte-identical inter " +
			"frames to the model-score path; the wire is not load-bearing")
	}
}
