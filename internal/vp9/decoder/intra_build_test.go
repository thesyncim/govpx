package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vp9/dsp"
)

// TestBuildIntraPredictorsDc128 picks the DC128 variant when both
// neighbors are absent; output must be all 128.
func TestBuildIntraPredictorsDc128(t *testing.T) {
	dst := make([]uint8, 4*4)
	BuildIntraPredictors(BuildIntraPredictorsArgs{
		Dst:         dst,
		DstStride:   4,
		Mode:        common.DcPred,
		TxSize:      common.Tx4x4,
		FrameWidth:  16,
		FrameHeight: 16,
	})
	for i, v := range dst {
		if v != 128 {
			t.Errorf("dst[%d]=%d want 128", i, v)
		}
	}
}

// TestBuildIntraPredictorsVMatchesDsp drives the V predictor against
// a hand-built above-row and checks against a direct DSP-kernel call.
// The DSP kernel expects a corner-prefixed slice (above[0]=corner,
// above[1..bs]=row pixels); BuildIntraPredictors hands the kernel
// exactly that shape, so the direct comparison passes a matching
// 5-byte slice.
func TestBuildIntraPredictorsVMatchesDsp(t *testing.T) {
	row := []uint8{10, 20, 30, 40}
	corner := uint8(99)

	dst1 := make([]uint8, 4*4)
	BuildIntraPredictors(BuildIntraPredictorsArgs{
		Dst:           dst1,
		DstStride:     4,
		Mode:          common.VPred,
		TxSize:        common.Tx4x4,
		Edges:         IntraEdgeRefs{Above: row, AboveLeft: corner},
		UpAvailable:   true,
		LeftAvailable: true,
		FrameWidth:    16,
		FrameHeight:   16,
	})

	dst2 := make([]uint8, 4*4)
	dsp.VpxVPredictor4x4(dst2, 4, append([]uint8{corner}, row...), nil)
	for i := range dst1 {
		if dst1[i] != dst2[i] {
			t.Errorf("dst1[%d]=%d dst2[%d]=%d", i, dst1[i], i, dst2[i])
		}
	}
}

// TestBuildIntraPredictorsHMatchesDsp drives the H predictor through
// a corner-prefixed dummy above slice.
func TestBuildIntraPredictorsHMatchesDsp(t *testing.T) {
	left := []uint8{5, 6, 7, 8}
	dst1 := make([]uint8, 4*4)
	BuildIntraPredictors(BuildIntraPredictorsArgs{
		Dst:           dst1,
		DstStride:     4,
		Mode:          common.HPred,
		TxSize:        common.Tx4x4,
		Edges:         IntraEdgeRefs{Left: left},
		LeftAvailable: true,
		FrameWidth:    16,
		FrameHeight:   16,
	})
	dst2 := make([]uint8, 4*4)
	dsp.VpxHPredictor4x4(dst2, 4, []uint8{0}, left)
	for i := range dst1 {
		if dst1[i] != dst2[i] {
			t.Errorf("dst1[%d]=%d dst2[%d]=%d", i, dst1[i], i, dst2[i])
		}
	}
}

// TestBuildIntraPredictorsLeftMissingFills129 confirms the absent-
// left-column path stamps 129 across the left buffer.
func TestBuildIntraPredictorsLeftMissingFills129(t *testing.T) {
	dst1 := make([]uint8, 4*4)
	BuildIntraPredictors(BuildIntraPredictorsArgs{
		Dst:           dst1,
		DstStride:     4,
		Mode:          common.HPred,
		TxSize:        common.Tx4x4,
		LeftAvailable: false,
		FrameWidth:    16,
		FrameHeight:   16,
	})
	want := make([]uint8, 4*4)
	dsp.VpxHPredictor4x4(want, 4, []uint8{0}, []uint8{129, 129, 129, 129})
	for i := range dst1 {
		if dst1[i] != want[i] {
			t.Errorf("dst1[%d]=%d want %d", i, dst1[i], want[i])
		}
	}
}

// TestBuildIntraPredictorsDcLeftOnly: only left neighbor present →
// DC kernel reads from left column alone.
func TestBuildIntraPredictorsDcLeftOnly(t *testing.T) {
	left := []uint8{100, 100, 100, 100}
	dst1 := make([]uint8, 4*4)
	BuildIntraPredictors(BuildIntraPredictorsArgs{
		Dst:           dst1,
		DstStride:     4,
		Mode:          common.DcPred,
		TxSize:        common.Tx4x4,
		Edges:         IntraEdgeRefs{Left: left},
		LeftAvailable: true,
		FrameWidth:    16,
		FrameHeight:   16,
	})
	want := make([]uint8, 4*4)
	dsp.VpxDcLeftPredictor4x4(want, 4, []uint8{0}, left)
	for i := range dst1 {
		if dst1[i] != want[i] {
			t.Errorf("dst1[%d]=%d want %d", i, dst1[i], want[i])
		}
	}
}

// TestBuildIntraPredictorsBottomEdgeExtend: with Y0+bs > FrameHeight
// the left column gets edge-replicated from the last in-frame row.
func TestBuildIntraPredictorsBottomEdgeExtend(t *testing.T) {
	// Y0 = 2, bs = 4, FrameHeight = 4 → extend_bottom = 2; rows 2 and
	// 3 of leftCol replicate left[1].
	left := []uint8{42, 99, 0, 0}
	dst1 := make([]uint8, 4*4)
	BuildIntraPredictors(BuildIntraPredictorsArgs{
		Dst:            dst1,
		DstStride:      4,
		Mode:           common.HPred,
		TxSize:         common.Tx4x4,
		Edges:          IntraEdgeRefs{Left: left},
		LeftAvailable:  true,
		FrameWidth:     16,
		FrameHeight:    4,
		Y0:             2,
		MbToBottomEdge: -1,
	})
	want := make([]uint8, 4*4)
	dsp.VpxHPredictor4x4(want, 4, []uint8{0}, []uint8{42, 99, 99, 99})
	for i := range dst1 {
		if dst1[i] != want[i] {
			t.Errorf("dst1[%d]=%d want %d", i, dst1[i], want[i])
		}
	}
}
