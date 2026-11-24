package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestVP9SegmentMapCountStampsPredictedForFollowingContext(t *testing.T) {
	e := &VP9Encoder{
		miGrid:              make([]vp9dec.NeighborMi, 2),
		prevSegmentMap:      []uint8{1, 1},
		prevSegmentMapRows:  1,
		prevSegmentMapCols:  2,
		prevSegmentMapValid: true,
	}
	for i := range e.miGrid {
		e.miGrid[i] = vp9dec.NeighborMi{
			SbType:    common.Block8x8,
			SegmentID: 1,
		}
	}
	tile := vp9dec.TileBounds{MiColStart: 0, MiColEnd: 2}
	var noPredCounts [vp9dec.MaxSegments]uint32
	var temporalCounts [vp9dec.PredictionProbs][2]uint32
	var tUnpredCounts [vp9dec.MaxSegments]uint32

	e.countVP9SegmentMapBlock(1, 2, tile, 0, 0, true,
		&noPredCounts, &temporalCounts, &tUnpredCounts)
	e.countVP9SegmentMapBlock(1, 2, tile, 0, 1, true,
		&noPredCounts, &temporalCounts, &tUnpredCounts)

	if e.miGrid[0].SegIDPredicted != 1 {
		t.Fatalf("left block SegIDPredicted = %d, want stamped predicted flag",
			e.miGrid[0].SegIDPredicted)
	}
	if temporalCounts[1][1] != 1 {
		t.Fatalf("temporalCounts[ctx=1][pred=1] = %d, want following block to see left prediction flag",
			temporalCounts[1][1])
	}
	if temporalCounts[0][1] != 1 {
		t.Fatalf("temporalCounts[ctx=0][pred=1] = %d, want first block in empty context",
			temporalCounts[0][1])
	}
}

func TestVP9EncoderBlockSegmentIDInterUsesBinaryPredFlag(t *testing.T) {
	e := &VP9Encoder{
		opts: VP9EncoderOptions{
			Segmentation: VP9SegmentationOptions{
				Enabled:   true,
				SegmentID: 3,
			},
		},
		prevSegmentMap:      []uint8{3},
		prevSegmentMapRows:  1,
		prevSegmentMapCols:  1,
		prevSegmentMapValid: true,
	}
	seg := &vp9dec.SegmentationParams{
		Enabled:        true,
		UpdateMap:      true,
		TemporalUpdate: false,
	}
	segID, predicted := e.vp9EncoderBlockSegmentID(seg, 1, 1, 0, 0,
		common.Block8x8, false, nil, &vp9InterEncodeState{})
	if segID != 3 {
		t.Fatalf("segID = %d, want static segment 3", segID)
	}
	if predicted != 0 {
		t.Fatalf("predicted = %d, want binary 0 when temporal_update=0 on inter", predicted)
	}
}

func TestVP9EncoderBlockSegmentIDKeyframeCarriesSegmentLiteral(t *testing.T) {
	e := &VP9Encoder{
		opts: VP9EncoderOptions{
			Segmentation: VP9SegmentationOptions{
				Enabled:   true,
				SegmentID: 4,
			},
		},
	}
	seg := &vp9dec.SegmentationParams{
		Enabled:        true,
		UpdateMap:      true,
		TemporalUpdate: false,
	}
	segID, predicted := e.vp9EncoderBlockSegmentID(seg, 1, 1, 0, 0,
		common.Block8x8, false, nil, nil)
	if segID != 4 {
		t.Fatalf("segID = %d, want static segment 4", segID)
	}
	if predicted != segID {
		t.Fatalf("predicted = %d, want segment literal %d on keyframe path", predicted, segID)
	}
}
